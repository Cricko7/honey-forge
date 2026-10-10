//go:build integration

package repository

import (
	"errors"
	"testing"

	profilescore "honey-forge/modules/profiles"

	"github.com/jackc/pgx/v5"
)

func TestPostgresProfileBindingRace(t *testing.T) {
	pool := profilePool(t)
	if _, e := pool.Exec(t.Context(), `INSERT INTO catalog_versions(type_id,type_version,entry) VALUES('demo',1,'{"type_id":"demo","type_version":1}')`); e != nil {
		t.Fatal(e)
	}

	s := integrationService(NewRepository(pool))

	created, e := s.Create(t.Context(), admin, request())
	if e != nil {
		t.Fatal(e)
	}

	bound := make(chan struct{})
	commitBinding := make(chan struct{})
	bindingResult := make(chan error, 1)
	go func() {
		bindingResult <- pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
			if _, e := ReadProfileForBinding(t.Context(), tx, orgID, created.Profile.ID); e != nil {
				return e
			}

			if _, e := tx.Exec(t.Context(), `INSERT INTO traps(id,organization_id,profile_id,type_id,type_version,interaction_level,name,description,created_at,updated_at,initial_trap) VALUES($1,$2,$3,'demo',1,'low','Binding','',clock_timestamp(),clock_timestamp(),'{}')`, newID(), orgID, created.Profile.ID); e != nil {
				return e
			}

			close(bound)
			<-commitBinding
			return nil
		})
	}()
	select {
	case <-bound:
	case e := <-bindingResult:
		t.Fatal(e)
	}

	deleted := make(chan error, 1)
	go func() {
		deleted <- s.Delete(t.Context(), admin, created.Profile.ID, profilescore.ETag(created.Profile))
	}()
	close(commitBinding)
	if e := <-bindingResult; e != nil {
		t.Fatal(e)
	}

	if e := <-deleted; !errors.Is(e, profilescore.ErrInUse) {
		t.Fatalf("binding/delete race: %v", e)
	}

	if _, e := pool.Exec(t.Context(), `UPDATE traps SET deleted_at=clock_timestamp()`); e != nil {
		t.Fatal(e)
	}

	if e := s.Delete(t.Context(), admin, created.Profile.ID, profilescore.ETag(created.Profile)); e != nil {
		t.Fatal("tombstone blocks delete", e)
	}

	e = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		_, e := ReadProfileForBinding(t.Context(), tx, orgID, created.Profile.ID)
		return e
	})
	if !errors.Is(e, profilescore.ErrNotFound) {
		t.Fatalf("binding deleted profile: %v", e)
	}
}
