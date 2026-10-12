//go:build integration

package app

import (
	"context"
	"encoding/json"
	"testing"

	"honey-forge/internal/orchestrator"
	"honey-forge/modules/auth"
	"honey-forge/modules/traps"
)

func TestDeploymentNodesRefreshPublishesOnlyAssignedWorkers(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "refresh@example.test", auth.OrganizationInput{Mode: "create", Name: "Company"})
	metadata := []map[string]any{{
		"ID":          "aaaaaaaaaaaaaaaaaaaaaaaaa",
		"Spec":        map[string]any{"Role": "worker", "Availability": "active", "Labels": map[string]string{"honeyforge.organization": a.view.Organization.ID}},
		"Description": map[string]string{"Hostname": "company"},
		"Status":      map[string]string{"State": "ready", "Addr": "10.0.0.2"},
	}}
	docker := orchestrator.DockerCLI{Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		if args[1] == "ls" {
			return []byte("aaaaaaaaaaaaaaaaaaaaaaaaa"), nil
		}

		return json.Marshal(metadata)
	}}
	store := orchestrator.PGStore{Pool: r.pool}
	if err := store.SyncNodes(t.Context(), docker); err != nil {
		t.Fatal(err)
	}

	type nodePage struct {
		Items []traps.DeploymentNode `json:"items"`
	}
	page := decodeIntegration[nodePage](t, sendOperator(t, r, "GET", "/api/traps/deployment-nodes", "", a, 200))
	if len(page.Items) != 1 || !page.Items[0].Ready || page.Items[0].Address != "10.0.0.2" {
		t.Fatalf("node refresh failed: %+v", page)
	}

	metadata[0]["Spec"].(map[string]any)["Availability"] = "drain"
	if err := store.SyncNodes(t.Context(), docker); err != nil {
		t.Fatal(err)
	}

	page = decodeIntegration[nodePage](t, sendOperator(t, r, "GET", "/api/traps/deployment-nodes", "", a, 200))
	if page.Items[0].Ready {
		t.Fatal("drained node is ready")
	}

	metadata = nil
	if err := store.SyncNodes(t.Context(), docker); err != nil {
		t.Fatal(err)
	}

	page = decodeIntegration[nodePage](t, sendOperator(t, r, "GET", "/api/traps/deployment-nodes", "", a, 200))
	if page.Items[0].Ready {
		t.Fatal("removed node is ready")
	}
}
