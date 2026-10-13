//go:build integration

package app

import (
	"encoding/json"
	"fmt"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	commandrepo "honey-forge/modules/commands/repository"
	commandservice "honey-forge/modules/commands/service"
	"honey-forge/modules/traps"
)

func TestDeleteOfflineTrapClosesCommands(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprintf("running=%v", running), func(t *testing.T) {
			r := openIntegrationRuntime(t)
			a := registerOperator(t, r, "delete@example.test", auth.OrganizationInput{Mode: "create", Name: "Deletion"})
			_, trap, _ := createIntegrationTrap(t, r, a)
			ctx, identity, connection, _ := connectIntegrationAgent(t, r, a, trap)
			command := decodeIntegration[commands.Command](t, sendOperator(t, r, "POST", "/api/traps/"+trap.ID+"/commands", integrationJSON(t, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "apply_config", Params: json.RawMessage(`{"profile_revision":1}`)}), a, 201))
			if running {
				agent := commandservice.NewAgent(commandrepo.New(r.pool, traps.NewRepository(r.pool)), CommandResultCheck(r.Catalog))
				dispatch, err := agent.Claim(ctx, identity.OrganizationID, trap.ID)
				if err != nil || dispatch == nil {
					t.Fatalf("claim: %v", err)
				}
			}
			if err := r.Agents.Offline(ctx, identity, connection); err != nil {
				t.Fatal(err)
			}
			req := operatorRequest(t, "DELETE", "/api/traps/"+trap.ID, "", a)
			req.Header.Set("X-Expected-Revision", "2")
			if response := serveOperator(r.Router, req); response.Code != 412 {
				t.Fatalf("wrong revision: %d %s", response.Code, response.Body.String())
			}
			var status string
			if err := r.pool.QueryRow(t.Context(), `SELECT status FROM commands WHERE id=$1`, command.ID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != string(command.Status) && !running || running && status != "running" {
				t.Fatalf("failed deletion changed command: %s", status)
			}
			var beforeSequence int64
			if err := r.pool.QueryRow(t.Context(), `SELECT COALESCE(max(sequence),0) FROM mutation_changes WHERE organization_id=$1`, identity.OrganizationID).Scan(&beforeSequence); err != nil {
				t.Fatal(err)
			}
			req = operatorRequest(t, "DELETE", "/api/traps/"+trap.ID, "", a)
			req.Header.Set("X-Expected-Revision", "1")
			if response := serveOperator(r.Router, req); response.Code != 204 {
				t.Fatalf("delete: %d %s", response.Code, response.Body.String())
			}
			var code string
			if err := r.pool.QueryRow(t.Context(), `SELECT status,error->>'code' FROM commands WHERE id=$1`, command.ID).Scan(&status, &code); err != nil {
				t.Fatal(err)
			}
			if status != "failed" || code != "trap_deleted" {
				t.Fatalf("command=%s code=%s", status, code)
			}
			var revoked bool
			if err := r.pool.QueryRow(t.Context(), `SELECT deleted_at IS NOT NULL AND token_hash IS NULL AND active_command_id IS NULL FROM traps WHERE id=$1`, trap.ID).Scan(&revoked); err != nil {
				t.Fatal(err)
			}
			if !revoked {
				t.Fatal("trap credentials and command not revoked")
			}
			var journaled bool
			if err := r.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM mutation_changes WHERE resource_id=$1 AND type='command.changed' AND sequence>$2)`, command.ID, beforeSequence).Scan(&journaled); err != nil {
				t.Fatal(err)
			}
			if !journaled {
				t.Fatal("closed command not published")
			}
			agent := commandservice.NewAgent(commandrepo.New(r.pool, traps.NewRepository(r.pool)), CommandResultCheck(r.Catalog))
			if _, err := agent.Claim(ctx, identity.OrganizationID, trap.ID); err == nil {
				t.Fatal("deleted agent can still claim commands")
			}
			req = operatorRequest(t, "DELETE", "/api/traps/"+trap.ID, "", a)
			req.Header.Set("X-Expected-Revision", "1")
			if response := serveOperator(r.Router, req); response.Code != 404 {
				t.Fatalf("repeated deletion: %d", response.Code)
			}
		})
	}
}
