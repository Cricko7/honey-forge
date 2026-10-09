//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Cricko7/honey-forge/src/backend/modules/auth"
	"github.com/jackc/pgx/v5"
)

func TestPostgresProfilePaginationAndAuditRollback(t *testing.T) {
	pool := profilePool(t)
	s, _ := testService()
	repo := NewRepository(pool)
	s.store = repo
	s.deps.HasLiveBindings = CheckLiveBindings
	for range 4 {
		req := request()
		req.RequestID = newID()
		if _, e := s.Create(t.Context(), admin, req); e != nil {
			t.Fatal(e)
		}
	}

	page, e := s.List(t.Context(), admin, ListOptions{Limit: 2})
	if e != nil || page.NextCursor == nil {
		t.Fatalf("first page: %#v %v", page, e)
	}

	req := request()
	if _, e := s.Create(t.Context(), admin, req); e != nil {
		t.Fatal(e)
	}

	second, e := s.List(t.Context(), admin, ListOptions{Limit: 2, Cursor: *page.NextCursor})
	if e != nil || len(second.Items) != 2 || second.NextCursor != nil {
		t.Fatalf("boundary: %#v %v", second, e)
	}

	p := page.Items[0]
	repo.writers.AppendAudit = func(context.Context, pgx.Tx, auth.AuthContext, string, string, int32, []string) (string, error) {
		return "", errors.New("audit unavailable")
	}

	if _, e := s.Patch(t.Context(), admin, p.ID, ETag(p), PatchRequest{Name: ptr("Failed")}); e == nil {
		t.Fatal("audit failure accepted")
	}

	if e := s.Delete(t.Context(), admin, p.ID, ETag(p)); e == nil {
		t.Fatal("delete audit failure accepted")
	}

	after, e := s.ReadProfile(t.Context(), admin, p.ID)
	if e != nil || after.Revision != p.Revision || after.Name != p.Name {
		t.Fatalf("partial mutation: %#v %v", after, e)
	}

	var snapshots, audits int
	if e := pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM profile_revisions),(SELECT count(*) FROM profile_audit)`).Scan(&snapshots, &audits); e != nil {
		t.Fatal(e)
	}

	if snapshots != 5 || audits != 5 {
		t.Fatalf("partial audit/snapshot: %d/%d", snapshots, audits)
	}
}
