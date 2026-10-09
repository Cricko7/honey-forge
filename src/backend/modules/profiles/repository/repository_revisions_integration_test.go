//go:build integration

package repository

import (
	"errors"
	profilescore "honey-forge/src/backend/modules/profiles"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPostgresProfileBindingRace(t *testing.T) {
	pool := profilePool(t)
	if _, e := pool.Exec(t.Context(), `CREATE TABLE traps(id uuid PRIMARY KEY,organization_id uuid NOT NULL,profile_id uuid NOT NULL,deleted_at timestamptz,FOREIGN KEY(organization_id,profile_id) REFERENCES profiles(organization_id,id))`); e != nil {
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

			if _, e := tx.Exec(t.Context(), `INSERT INTO traps(id,organization_id,profile_id)VALUES($1,$2,$3)`, newID(), orgID, created.Profile.ID); e != nil {
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
