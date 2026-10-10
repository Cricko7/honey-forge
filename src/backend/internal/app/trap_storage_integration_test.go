//go:build integration

package app

import (
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
)

func TestTrapStorageRejectsOrphanCommands(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "storage@example.test", auth.OrganizationInput{Mode: "create", Name: "Storage"})
	_, err := r.pool.Exec(t.Context(), `INSERT INTO commands(id,organization_id,trap_id,request_id,action,params,status,created_at,expires_at) VALUES($1,$2,$3,$4,'stop','{}','queued',clock_timestamp(),clock_timestamp()+interval '24 hours')`, string(contract.NewID()), a.view.Organization.ID, string(contract.NewID()), string(contract.NewID()))
	if err == nil {
		t.Fatal("orphan command persisted")
	}
}
