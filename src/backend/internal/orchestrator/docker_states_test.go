package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestStatesUsesTwoDockerCallsForManyTraps(t *testing.T) {
	ids := make([]string, 20)
	names := make([]string, 20)
	var rows []string
	for i := range ids {
		ids[i] = fmt.Sprintf("11111111-1111-4111-8111-%012d", i)
		names[i] = serviceName(ids[i])
		rows = append(rows, fmt.Sprintf(`%s {"com.nodemetrics.instance":"%s","com.nodemetrics.generation":"1","com.nodemetrics.ports":"2222"}`, names[i], ids[i]))
	}

	calls := 0
	d := DockerCLI{Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		calls++
		if args[1] == "ls" {
			return []byte(strings.Join(names, "\n")), nil
		}

		return []byte(strings.Join(rows, "\n")), nil
	}}
	states, err := d.States(t.Context(), ids)
	if err != nil || len(states) != 20 || calls != 2 {
		t.Fatalf("states=%d calls=%d err=%v", len(states), calls, err)
	}

	for _, id := range ids {
		if !states[id].Exists || states[id].Generation != 1 {
			t.Fatal("lost service state")
		}
	}

	calls = 0
	states, err = d.States(t.Context(), nil)
	if err != nil || len(states) != 0 || calls != 0 {
		t.Fatal("empty installation must not poll Docker services")
	}
}

func TestStatesRejectsWrongOwner(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	d := DockerCLI{Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		if args[1] == "ls" {
			return []byte(serviceName(id)), nil
		}

		return []byte(serviceName(id) + ` {"com.nodemetrics.instance":"another","com.nodemetrics.generation":"1"}`), nil
	}}
	if _, err := d.States(t.Context(), []string{id}); err == nil {
		t.Fatal("another owner's service was accepted")
	}
}
