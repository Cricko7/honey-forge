package orchestrator

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"honey-forge/modules/profiles"
)

type fakeStore struct {
	targets  []Target
	issued   []string
	advanced []string
}

func (f *fakeStore) List(context.Context) ([]Target, error) { return f.targets, nil }
func (f *fakeStore) Issue(_ context.Context, target Target) (string, int64, error) {
	f.issued = append(f.issued, target.ID)
	for i := range f.targets {
		if f.targets[i].ID == target.ID {
			f.targets[i].Generation++
		}
	}
	return "token", target.Generation + 1, nil
}
func (f *fakeStore) Advance(_ context.Context, target Target) error {
	f.advanced = append(f.advanced, target.ID)
	return nil
}

type fakeDocker struct {
	current  map[string]ServiceState
	deployed []string
	removed  []string
	failID   string
}

func (f *fakeDocker) Current(_ context.Context, id string) (ServiceState, error) {
	return f.current[id], nil
}
func (f *fakeDocker) Remove(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	delete(f.current, id)
	return nil
}
func (f *fakeDocker) Deploy(_ context.Context, target Target, token string, generation int64, ports []int) error {
	if target.ID == f.failID {
		return errors.New("unavailable")
	}
	f.deployed = append(f.deployed, target.ID)
	f.current[target.ID] = ServiceState{Exists: true, Generation: generation, Ports: ports}
	return nil
}

func TestReconcilePlacesEveryPendingTrapAndReusesCurrentService(t *testing.T) {
	store := &fakeStore{targets: []Target{
		{ID: "one", Snapshot: profiles.Snapshot{TypeID: "tcp-banner", TypeVersion: 1}},
		{ID: "two", Snapshot: profiles.Snapshot{TypeID: "redis-emulator", TypeVersion: 1}},
	}}
	docker := &fakeDocker{current: map[string]ServiceState{}}
	c := Controller{Store: store, Docker: docker, Ports: func(context.Context, profiles.Snapshot) ([]int, error) { return []int{2222}, nil }}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(docker.deployed, []string{"one", "two"}) || len(store.issued) != 2 || !reflect.DeepEqual(store.advanced, []string{"one", "two"}) {
		t.Fatalf("deployed=%v issued=%v", docker.deployed, store.issued)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(docker.deployed) != 2 || len(store.issued) != 2 {
		t.Fatalf("repeated deployment: deployed=%v issued=%v", docker.deployed, store.issued)
	}
}

func TestReconcileRedeploysChangedPortsAndRemovesDeletedTrap(t *testing.T) {
	store := &fakeStore{targets: []Target{{ID: "changed", Generation: 1}, {ID: "deleted", Deleted: true}}}
	docker := &fakeDocker{current: map[string]ServiceState{
		"changed": {Exists: true, Generation: 1, Ports: []int{2222}},
		"deleted": {Exists: true, Generation: 1, Ports: []int{6380}},
	}}
	c := Controller{Store: store, Docker: docker, Ports: func(context.Context, profiles.Snapshot) ([]int, error) { return []int{2223}, nil }}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(docker.removed, []string{"changed", "deleted"}) || !reflect.DeepEqual(docker.deployed, []string{"changed"}) {
		t.Fatalf("removed=%v deployed=%v", docker.removed, docker.deployed)
	}
}

func TestReconcileContinuesAfterOneDeploymentFails(t *testing.T) {
	store := &fakeStore{targets: []Target{{ID: "bad"}, {ID: "good"}}}
	docker := &fakeDocker{current: map[string]ServiceState{}, failID: "bad"}
	c := Controller{Store: store, Docker: docker, Ports: func(context.Context, profiles.Snapshot) ([]int, error) { return []int{2222}, nil }}
	if err := c.Reconcile(t.Context()); err == nil || !reflect.DeepEqual(docker.deployed, []string{"good"}) {
		t.Fatalf("error=%v deployed=%v", err, docker.deployed)
	}
}
