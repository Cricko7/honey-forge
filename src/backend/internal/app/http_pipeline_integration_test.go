//go:build integration

package app

import (
	"encoding/json"
	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
	"testing"
)

func TestHTTPPipelineSnapshotFencesInstalledConfigurationAndAgentIdentity(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "pipeline@example.test", auth.OrganizationInput{Mode: "create", Name: "Pipeline"})
	config := json.RawMessage(`{"services":[{"name":"web","port":32089,"entry":"web"}],"blocks":[{"id":"web","kind":"input","config":{"name":"web","port":32089,"method":"POST","path":"/pipeline"},"outputs":{"out":"reply"}},{"id":"reply","kind":"response","config":{"status":200,"format":"json","body":"{}","headers":"{}"},"outputs":{}}],"limits":{"max_steps":512,"timeout_ms":10000,"max_body_bytes":65536},"management":{"heartbeat_interval_seconds":10,"telemetry_flush_interval_ms":500}}`)
	var object profiles.Object
	if err := json.Unmarshal(config, &object); err != nil {
		t.Fatal(err)
	}
	profile := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, profiles.CreateRequest{RequestID: string(contract.NewID()), Name: "Pipeline", TypeID: "custom-http", TypeVersion: 1, Config: object}), a, 201))
	trap := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Pipeline", ProfileID: profile.ID}), a, 201))
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	if _, err := r.Agents.PipelineSnapshot(ctx, identity, connection); err == nil {
		t.Fatal("unconfigured stopped trap accepted")
	}
	var revision int32 = 1
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	runtime.RuntimeState = "running"
	executeIntegrationCommand(t, r, a, ctx, identity, "start", runtime)
	if snapshot, err := r.Agents.PipelineSnapshot(ctx, identity, connection); err != nil || snapshot.TypeID != "custom-http" {
		t.Fatalf("installed snapshot: %+v %v", snapshot, err)
	}
	for _, tc := range []struct {
		name   string
		change func()
	}{
		{"wrong connection", func() { connection = string(contract.NewID()) }},
		{"wrong generation", func() { identity.CredentialGeneration++ }},
		{"wrong organization", func() { identity.OrganizationID = string(contract.NewID()) }},
		{"wrong trap", func() { identity.TrapID = string(contract.NewID()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			savedIdentity, savedConnection := identity, connection
			tc.change()
			defer func() { identity, connection = savedIdentity, savedConnection }()
			if _, err := r.Agents.PipelineSnapshot(ctx, identity, connection); err == nil {
				t.Fatal("identity fence bypassed")
			}
		})
	}
	runtime.RuntimeState = "stopped"
	executeIntegrationCommand(t, r, a, ctx, identity, "stop", runtime)
	if _, err := r.Agents.PipelineSnapshot(ctx, identity, connection); err == nil {
		t.Fatal("stopped target accepted")
	}
}
