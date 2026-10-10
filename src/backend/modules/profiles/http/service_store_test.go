package http

import (
	"context"
	"encoding/json"
	profilecore "honey-forge/modules/profiles"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5"
	"honey-forge/modules/auth"
)

type fakeKey struct {
	hash    [32]byte
	initial profilecore.Profile
}

type fakeStore struct {
	mu        sync.Mutex
	profiles  map[string]profilecore.Profile
	keys      map[string]fakeKey
	snapshots map[string]profilecore.Profile
	writes    int
}

func (f *fakeStore) Create(ctx context.Context, a auth.AuthContext, key string, hash [32]byte, build func() (profilecore.Profile, error)) (profilecore.CreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return profilecore.CreateResult{}, err
	}

	key = a.OrganizationID + key
	if k, ok := f.keys[key]; ok {
		if k.hash != hash {
			return profilecore.CreateResult{}, profilecore.ErrIdempotencyConflict
		}

		p, ok := f.profiles[k.initial.ID]
		if !ok {
			return profilecore.CreateResult{}, profilecore.ErrRequestUsed
		}

		return profilecore.CreateResult{Profile: clone(k.initial), Replayed: true, Current: p.Revision == 1}, nil
	}

	p, e := build()
	if e != nil {
		return profilecore.CreateResult{}, e
	}

	p.OrganizationID = a.OrganizationID
	p.Sequence = int64(len(f.keys) + 1)
	f.profiles[p.ID] = clone(p)
	f.keys[key] = fakeKey{hash, clone(p)}
	f.snapshots[profilecore.ETag(p)] = clone(p)
	f.writes++
	return profilecore.CreateResult{Profile: clone(p), Current: true}, nil
}

func (f *fakeStore) ReadProfile(ctx context.Context, org, id string) (profilecore.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return profilecore.Profile{}, e
	}

	p, ok := f.profiles[id]
	if !ok || p.OrganizationID != org {
		return profilecore.Profile{}, profilecore.ErrNotFound
	}

	return clone(p), nil
}

func (f *fakeStore) Update(ctx context.Context, a auth.AuthContext, id string, update func(profilecore.Profile) (profilecore.Profile, []string, error)) (profilecore.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return profilecore.Profile{}, e
	}

	p, ok := f.profiles[id]
	if !ok || p.OrganizationID != a.OrganizationID {
		return profilecore.Profile{}, profilecore.ErrNotFound
	}

	n, fields, e := update(clone(p))
	if e != nil {
		return profilecore.Profile{}, e
	}

	if len(fields) > 0 {
		f.profiles[id] = clone(n)
		f.snapshots[profilecore.ETag(n)] = clone(n)
		f.writes++
	}

	return clone(n), nil
}

func (f *fakeStore) Delete(ctx context.Context, a auth.AuthContext, id string, check func(profilecore.Profile, pgx.Tx) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}

	p, ok := f.profiles[id]
	if !ok || p.OrganizationID != a.OrganizationID {
		return profilecore.ErrNotFound
	}

	if e := check(clone(p), nil); e != nil {
		return e
	}

	delete(f.profiles, id)
	f.writes++
	return nil
}

func (f *fakeStore) ReadSnapshot(ctx context.Context, org, id string, revision int32) (profilecore.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return profilecore.Snapshot{}, e
	}

	p, ok := f.snapshots[profilecore.ETag(profilecore.Profile{ID: id, Revision: revision})]
	if !ok || p.OrganizationID != org {
		return profilecore.Snapshot{}, profilecore.ErrNotFound
	}

	return profilecore.SnapshotOf(clone(p)), nil
}

func (f *fakeStore) List(ctx context.Context, org string, q profilecore.ListQuery) ([]profilecore.Profile, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, 0, e
	}

	if q.Boundary == 0 {
		q.Boundary = int64(len(f.keys))
	}

	items := []profilecore.Profile{}
	for _, p := range f.profiles {
		if p.OrganizationID != org || p.Sequence > q.Boundary || q.TypeID != "" && p.TypeID != q.TypeID {
			continue
		}

		if q.AfterID != "" && (p.CreatedAt.After(q.AfterTime) || p.CreatedAt.Equal(q.AfterTime) && p.ID >= q.AfterID) {
			continue
		}

		items = append(items, clone(p))
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}

		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items[:min(len(items), q.Limit+1)], q.Boundary, nil
}

func mustJSON(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}

	return string(b)
}

func clone(p profilecore.Profile) profilecore.Profile {
	p.Config = profilecore.CloneObject(p.Config)
	p.SecretFieldsSet = append([]string{}, p.SecretFieldsSet...)
	return p
}
