//go:build integration

package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/catalog"
	"honey-forge/modules/commands"
	"honey-forge/modules/events"
)

func TestFrontendReplaySurvivesRuntimeRestart(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "restart-stream@example.test", auth.OrganizationInput{Mode: "create", Name: "Restart"})
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", a, 200))
	_, trap, _ := createIntegrationTrap(t, r, a)
	databaseURL := r.pool.Config().ConnString()
	r.Close()
	restarted, err := Open(t.Context(), Config{DatabaseURL: databaseURL, CursorKey: []byte("integration-cursor-key-32-bytes!"), BrowserOrigins: []string{integrationOrigin}, MigrationsPath: "../../../../migrations", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	server := httptest.NewTLSServer(restarted.Router)
	defer server.Close()
	c := dialFrontend(t, server, a)
	subscribeFrontend(t, c, page.StreamCursor)
	readFrontend(t, c, "profile.changed")
	readFrontend(t, c, "audit.created")
	e := readFrontend(t, c, "trap.changed")
	if !strings.Contains(string(e.Payload["data"]), trap.ID) {
		t.Fatal("durable trap change missing")
	}
	readFrontend(t, c, "stream.ready")
}

func TestFrontendCommandReplayKeepsStatusSnapshots(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "command-stream@example.test", auth.OrganizationInput{Mode: "create", Name: "Commands"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, _, runtime := connectIntegrationAgent(t, r, a, trap)
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", a, 200))
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	command := executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	c := dialFrontend(t, server, a)
	subscribeFrontend(t, c, page.StreamCursor)
	var statuses []commands.Status
	for {
		if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var e contract.Envelope
		if err := c.ReadJSON(&e); err != nil {
			t.Fatal(err)
		}
		if e.Type == "stream.ready" {
			break
		}
		if e.Type != "command.changed" {
			continue
		}
		var data struct {
			TrapID  string           `json:"trap_id"`
			Command commands.Command `json:"command"`
		}
		if err := json.Unmarshal(e.Payload["data"], &data); err != nil {
			t.Fatal(err)
		}
		if data.TrapID != trap.ID || data.Command.ID != command.ID {
			t.Fatal("command owner/id")
		}
		for _, secret := range []string{"configuration", "lease_id", "lease_expires_at", "organization_id"} {
			if strings.Contains(string(e.Payload["data"]), secret) {
				t.Fatalf("private field %s", secret)
			}
		}
		statuses = append(statuses, data.Command.Status)
	}
	if !reflect.DeepEqual(statuses, []commands.Status{commands.Queued, commands.Running, commands.Succeeded}) {
		t.Fatalf("replay statuses %v", statuses)
	}
}

func TestFrontendCatalogInvalidationIsDurableAndIdempotent(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "catalog-stream@example.test", auth.OrganizationInput{Mode: "create", Name: "Catalog"})
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", a, 200))
	defs := catalog.BuiltinDefinitions()
	next := defs[0]
	next.Entry.TypeVersion = 2
	updated, err := catalog.NewService(append(defs, next), r.Cursors)
	if err != nil {
		t.Fatal(err)
	}
	repo := catalog.NewRepository(r.pool)
	for range 2 {
		if err := repo.Install(t.Context(), updated); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	c := dialFrontend(t, server, a)
	subscribeFrontend(t, c, page.StreamCursor)
	e := readFrontend(t, c, "catalog.changed")
	var data struct {
		ETag string `json:"etag"`
	}
	if err := json.Unmarshal(e.Payload["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data.ETag == "" {
		t.Fatal("missing etag")
	}
	readFrontend(t, c, "stream.ready") // No duplicate invalidation for the same ETag.
	var count int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM mutation_changes WHERE organization_id=$1 AND type='catalog.changed'`, a.view.Organization.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("notifications %d, error %v", count, err)
	}
}
