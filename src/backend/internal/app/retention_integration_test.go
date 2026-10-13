//go:build integration

package app

import (
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
)

func TestHistoryRetentionPreservesCurrentStateAndStreamFloor(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "retention@example.test", auth.OrganizationInput{Mode: "create", Name: "Retention"})
	org := a.view.Organization.ID
	ctx := t.Context()
	for _, query := range []string{
		`UPDATE auth_audit SET occurred_at=now()-interval '2 days' WHERE organization_id=$1`,
		`UPDATE auth_changes SET occurred_at=now()-interval '2 days' WHERE organization_id=$1`,
		`UPDATE mutation_changes SET created_at=now()-interval '2 days' WHERE organization_id=$1`,
	} {
		if _, err := r.pool.Exec(ctx, query, org); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO mutation_changes(organization_id,sequence,type,resource_id,metadata,data) SELECT organization_id,sequence+1,'audit.created',$1,'{}','{}' FROM organization_changes WHERE organization_id=$2`, a.view.User.ID, org); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE organization_changes SET sequence=sequence+1 WHERE organization_id=$1`, org); err != nil {
		t.Fatal(err)
	}
	if err := cleanHistory(ctx, r.pool, time.Now(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	var audits, changes, sessions int
	var floor, sequence int64
	if err := r.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM auth_audit WHERE organization_id=$1),(SELECT count(*) FROM mutation_changes WHERE organization_id=$1),(SELECT count(*) FROM operator_sessions WHERE user_id=$2),stream_floor,sequence FROM organization_changes WHERE organization_id=$1`, org, a.view.User.ID).Scan(&audits, &changes, &sessions, &floor, &sequence); err != nil {
		t.Fatal(err)
	}
	if audits != 0 || changes != 1 || sessions != 1 || floor != sequence-1 {
		t.Fatalf("audits=%d changes=%d sessions=%d floor=%d sequence=%d", audits, changes, sessions, floor, sequence)
	}
	if err := cleanHistory(ctx, r.pool, time.Now(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	sendOperator(t, r, "GET", "/api/traps", "", a, 200)
}

func TestHistoryRetentionKeepsActiveCommandsAndUnfinishedIngestion(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "retention-traps@example.test", auth.OrganizationInput{Mode: "create", Name: "Retention"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx := t.Context()
	for _, age := range []string{"3 days", "1 hour"} {
		if _, err := r.pool.Exec(ctx, `INSERT INTO trap_events(event_id,trap_id,organization_id,session_id,session_sequence,occurred_at,received_at,fingerprint,envelope,event_data) VALUES($1,$2,$3,$4,1,now()-$5::interval,now()-$5::interval,decode(repeat('00',32),'hex'),'{}','{}')`, string(contract.NewID()), trap.ID, a.view.Organization.ID, string(contract.NewID()), age); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"failed", "queued"} {
		if _, err := r.pool.Exec(ctx, `INSERT INTO commands(id,organization_id,trap_id,request_id,action,params,status,created_at,expires_at,finished_at,error) VALUES($1,$2,$3,$4,'stop','{}',$5,now()-interval '3 days',CASE WHEN $5='queued' THEN now()+interval '1 hour' ELSE now()-interval '2 days' END,CASE WHEN $5='failed' THEN now()-interval '2 days' END,CASE WHEN $5='failed' THEN '{}'::jsonb END)`, string(contract.NewID()), a.view.Organization.ID, trap.ID, string(contract.NewID()), status); err != nil {
			t.Fatal(err)
		}
	}
	for _, finished := range []bool{true, false} {
		batch := string(contract.NewID())
		if _, err := r.pool.Exec(ctx, `INSERT INTO trap_ingestions(trap_id,batch_id,finished,fingerprint) VALUES($1,$2,$3,decode(repeat('00',32),'hex'))`, trap.ID, batch, finished); err != nil {
			t.Fatal(err)
		}
		if _, err := r.pool.Exec(ctx, `INSERT INTO trap_event_batches(trap_id,batch_id,fingerprint,stored_at) VALUES($1,$2,decode(repeat('00',32),'hex'),now()-interval '3 days')`, trap.ID, batch); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanHistory(ctx, r.pool, time.Now(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	var events, commands, ingestions, batches int
	if err := r.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM trap_events),(SELECT count(*) FROM commands WHERE status='queued'),(SELECT count(*) FROM trap_ingestions),(SELECT count(*) FROM trap_event_batches)`).Scan(&events, &commands, &ingestions, &batches); err != nil {
		t.Fatal(err)
	}
	if events != 1 || commands != 1 || ingestions != 1 || batches != 1 {
		t.Fatalf("events=%d active=%d ingestion=%d batches=%d", events, commands, ingestions, batches)
	}
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM commands WHERE status='failed'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old commands %d %v", count, err)
	}
	sendOperator(t, r, "GET", "/api/traps/"+trap.ID, "", a, 200)
}
