//go:build integration

package app

import (
	"net/http/httptest"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
)

func TestTrapDeleteWaitsForIngestionReservation(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "ingestrace@example.test", auth.OrganizationInput{Mode: "create", Name: "Ingestion"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	connectIntegrationAgent(t, r, a, trap)
	tx, err := r.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), `SELECT id FROM traps WHERE id=$1 FOR UPDATE`, trap.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO trap_ingestions(trap_id,batch_id) VALUES($1,$2)`, trap.ID, string(contract.NewID())); err != nil {
		t.Fatal(err)
	}
	req := operatorRequest(t, "DELETE", "/api/traps/"+trap.ID, "", a)
	req.Header.Set("X-Expected-Revision", "1")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); r.Router.ServeHTTP(response, req) }()
	// Observe the database lock, not a scheduling delay, before releasing ingress.
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	waiting := false
	for !waiting {
		if err := r.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE a.wait_event_type='Lock' AND a.pid IN (SELECT pid FROM pg_locks WHERE relation='traps'::regclass))`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-poll.C:
		case <-timeout.C:
			t.Fatal("DELETE did not reach trap lock")
		}
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-done
	if response.Code != 409 {
		t.Fatalf("DELETE raced ingestion: %d %s", response.Code, response.Body.String())
	}
}
