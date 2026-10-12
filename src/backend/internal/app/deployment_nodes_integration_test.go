//go:build integration

package app

import (
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func TestDeploymentNodesOwnershipAvailabilityAndPorts(t *testing.T) {
	r := openIntegrationRuntime(t)
	r.Traps.WithAutoDeploy(true)
	a := registerOperator(t, r, "nodes-a@example.test", auth.OrganizationInput{Mode: "create", Name: "A"})
	b := registerOperator(t, r, "nodes-b@example.test", auth.OrganizationInput{Mode: "create", Name: "B"})
	join := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", a, 200))
	viewer := registerOperator(t, r, "nodes-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: join.Code})
	nodes := []string{"aaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbb", "ccccccccccccccccccccccccc"}
	for i, id := range nodes {
		org := a.view.Organization.ID
		if i == 2 {
			org = b.view.Organization.ID
		}

		if _, err := r.pool.Exec(t.Context(), `INSERT INTO deployment_nodes VALUES($1,$2,'company','10.0.0.2',true,clock_timestamp())`, id, org); err != nil {
			t.Fatal(err)
		}
	}

	page := decodeIntegration[struct {
		Items []traps.DeploymentNode `json:"items"`
	}](t, sendOperator(t, r, "GET", "/api/traps/deployment-nodes", "", a, 200))
	if len(page.Items) != 2 {
		t.Fatalf("foreign node exposed: %+v", page)
	}

	p := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, catalogProfileRequest(t, r, a)), a, 201))
	request := traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Company trap", ProfileID: p.ID, DeploymentNodeID: nodes[0]}
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), viewer, 403)
	created := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), a, 201))
	if created.DeploymentNodeID != nodes[0] {
		t.Fatalf("placement lost: %+v", created)
	}

	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), a, 200)
	request.DeploymentNodeID = nodes[1]
	requireOperatorError(t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), a, 409), "idempotency_conflict")
	request.RequestID = string(contract.NewID())
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), a, 201)
	request.RequestID = string(contract.NewID())
	requireOperatorError(t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), a, 409), "trap_port_conflict")
	request.DeploymentNodeID = nodes[2]
	requireOperatorError(t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), a, 409), "deployment_node_unavailable")
	request.DeploymentNodeID = "not-a-node"
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), a, 422)
	if _, err := r.pool.Exec(t.Context(), `UPDATE deployment_nodes SET observed_at=clock_timestamp()-interval '1 minute' WHERE id=$1`, nodes[0]); err != nil {
		t.Fatal(err)
	}

	request.DeploymentNodeID = nodes[0]
	requireOperatorError(t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, request), a, 409), "deployment_node_unavailable")
	read := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", "/api/traps/"+created.ID, "", a, 200))
	if read.DeploymentNodeID != nodes[0] {
		t.Fatalf("stored placement lost: %+v", read)
	}
}
