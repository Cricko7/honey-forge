//go:build integration

package app

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/internal/stream"
	"honey-forge/modules/auth"
	"honey-forge/modules/events"
	"honey-forge/modules/traps"
)

func dialFrontend(t *testing.T, server *httptest.Server, a operatorSession) *websocket.Conn {
	t.Helper()
	d := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Subprotocols: []string{"dashboard-stream.v1"}}
	h := http.Header{"Origin": []string{integrationOrigin}, "Cookie": []string{"__Host-session=" + a.cookie}}
	c, res, err := d.Dial("wss"+strings.TrimPrefix(server.URL, "https")+stream.FrontendPath, h)
	if err != nil {
		if res != nil {
			defer res.Body.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Log(err)
		}
	})
	if c.Subprotocol() != "dashboard-stream.v1" {
		t.Fatal("wrong protocol")
	}
	return c
}
func subscribeFrontend(t *testing.T, c *websocket.Conn, after any) contract.ID {
	t.Helper()
	id := contract.NewID()
	if err := c.WriteJSON(map[string]any{"message_id": id, "type": "stream.subscribe", "payload": map[string]any{"after": after}}); err != nil {
		t.Fatal(err)
	}
	return id
}
func readFrontend(t *testing.T, c *websocket.Conn, kind string) contract.Envelope {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var e contract.Envelope
	if err := c.ReadJSON(&e); err != nil {
		t.Fatal(err)
	}
	if e.Type != kind {
		t.Fatalf("type %s want %s", e.Type, kind)
	}
	return e
}
func frontendCursor(t *testing.T, e contract.Envelope) string {
	t.Helper()
	var cursor string
	if err := json.Unmarshal(e.Payload["cursor"], &cursor); err != nil {
		t.Fatal(err)
	}
	return cursor
}

func TestFrontendRESTReplayLiveReconnect(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "frontend@example.test", auth.OrganizationInput{Mode: "create", Name: "Frontend"})
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", a, 200))
	_, trap, _ := createIntegrationTrap(t, r, a)
	path := "/api/traps/" + trap.ID
	patched := decodeIntegration[traps.Trap](t, trapMutation(t, r, a, "PATCH", path, `{"name":"Second state"}`, 1, 200))
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	c := dialFrontend(t, server, a)
	requestID := subscribeFrontend(t, c, page.StreamCursor)
	readFrontend(t, c, "profile.changed")
	readFrontend(t, c, "audit.created")
	for _, want := range []traps.Trap{trap, patched} {
		e := readFrontend(t, c, "trap.changed")
		var data struct {
			Trap traps.Trap `json:"trap"`
		}
		if err := json.Unmarshal(e.Payload["data"], &data); err != nil {
			t.Fatal(err)
		}
		if data.Trap.StateVersion != want.StateVersion || data.Trap.Name != want.Name {
			t.Fatalf("historical DTO %+v want %+v", data.Trap, want)
		}
		if e.ReplyTo != nil {
			t.Fatal("notification has reply_to")
		}
		raw := string(e.Payload["data"])
		for _, secret := range []string{"token_hash", "applied_configuration", "connection_id", "generation", "initial_trap"} {
			if strings.Contains(raw, secret) {
				t.Fatalf("storage field leaked %s", secret)
			}
		}
	}
	ready := readFrontend(t, c, "stream.ready")
	if ready.ReplyTo == nil || *ready.ReplyTo != requestID || string(ready.Payload["replayed"]) != "true" {
		t.Fatal("ready correlation/replayed")
	}
	foreign := registerOperator(t, r, "foreign-stream@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	createIntegrationTrap(t, r, foreign) // These commits must never enter this stream.
	latest := decodeIntegration[traps.Trap](t, trapMutation(t, r, a, "PATCH", path, `{"name":"Live state"}`, 2, 200))
	live := readFrontend(t, c, "trap.changed")
	var payload struct {
		Trap traps.Trap `json:"trap"`
	}
	if err := json.Unmarshal(live.Payload["data"], &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Trap.ID != trap.ID || payload.Trap.StateVersion != latest.StateVersion {
		t.Fatal("live state missing")
	}
	cursor := frontendCursor(t, live)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	trapMutation(t, r, a, "DELETE", path, "", latest.Revision, 204)
	// A fresh session in the same organization resumes a previous session's token.
	login := sessionFromResponse(t, sendOperator(t, r, "POST", "/api/sessions", integrationJSON(t, auth.LoginRequest{Email: a.view.User.Email, Password: integrationPassword}), operatorSession{}, 201))
	resumed := dialFrontend(t, server, login)
	subscribeFrontend(t, resumed, cursor)
	deleted := readFrontend(t, resumed, "trap.deleted")
	if !strings.Contains(string(deleted.Payload["data"]), trap.ID) {
		t.Fatal("tombstone missing")
	}
	readFrontend(t, resumed, "stream.ready")
	sendOperator(t, r, "GET", path, "", login, 404)
}

func TestFrontendInvalidCursorAndSubscribe(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "cursor-owner@example.test", auth.OrganizationInput{Mode: "create", Name: "Owner"})
	b := registerOperator(t, r, "cursor-foreign@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", b, 200))
	expired, err := stream.EncodeCursor(r.Cursors, contract.ID(a.view.Organization.ID), 0, time.Now().Add(-25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	for _, tt := range []struct {
		name    string
		payload map[string]any
		code    string
	}{
		{"foreign", map[string]any{"after": page.StreamCursor}, "invalid_cursor"},
		{"expired", map[string]any{"after": expired}, "cursor_expired"},
		{"malformed", map[string]any{"after": "invalid"}, "invalid_cursor"},
		{"missing after", map[string]any{}, "invalid_message"},
		{"wrong after", map[string]any{"after": 1}, "invalid_message"},
		{"organization injection", map[string]any{"after": nil, "organization_id": b.view.Organization.ID}, "invalid_message"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := dialFrontend(t, server, a)
			id := contract.NewID()
			if err := c.WriteJSON(map[string]any{"message_id": id, "type": "stream.subscribe", "payload": tt.payload}); err != nil {
				t.Fatal(err)
			}
			e := readFrontend(t, c, "error")
			var api contract.Error
			if err := json.Unmarshal(e.Payload["error"], &api); err != nil {
				t.Fatal(err)
			}
			if api.Code != tt.code || e.ReplyTo == nil || *e.ReplyTo != id {
				t.Fatalf("error %s reply %v", api.Code, e.ReplyTo)
			}
			_, _, err := c.ReadMessage()
			var closed *websocket.CloseError
			if !errors.As(err, &closed) || closed.Code != 4400 || closed.Text != tt.code {
				t.Fatalf("close %v", err)
			}
		})
	}
}

func TestFrontendRevocationAndShutdown(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "revocation@example.test", auth.OrganizationInput{Mode: "create", Name: "Revoke"})
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	c := dialFrontend(t, server, a)
	subscribeFrontend(t, c, nil)
	readFrontend(t, c, "stream.ready")
	sendOperator(t, r, "DELETE", "/api/session", "", a, 204)
	// Trigger delivery without waiting for ping; it must authenticate again first.
	login := sessionFromResponse(t, sendOperator(t, r, "POST", "/api/sessions", integrationJSON(t, auth.LoginRequest{Email: a.view.User.Email, Password: integrationPassword}), operatorSession{}, 201))
	createIntegrationTrap(t, r, login)
	_, _, err := c.ReadMessage()
	var closed *websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != 4401 {
		t.Fatalf("revocation close %v", err)
	}
	c2 := dialFrontend(t, server, login)
	subscribeFrontend(t, c2, nil)
	readFrontend(t, c2, "stream.ready")
	r.stop()
	_, _, err = c2.ReadMessage()
	if !errors.As(err, &closed) || closed.Code != 1013 {
		t.Fatalf("shutdown close %v", err)
	}
}

func TestFrontendEventSummaryAfterCommit(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "frontend-events@example.test", auth.OrganizationInput{Mode: "create", Name: "Events"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", a, 200))
	event := attackEvent(time.Now().UTC())
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, event)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	c := dialFrontend(t, server, a)
	subscribeFrontend(t, c, page.StreamCursor)
	e := readFrontend(t, c, "event.created")
	var data struct {
		Event events.EventSummary `json:"event"`
	}
	if err := json.Unmarshal(e.Payload["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data.Event.EventID != event.EventID || strings.Contains(string(e.Payload["data"]), "listener_name") {
		t.Fatal("summary invalid or event data leaked")
	}
	readFrontend(t, c, "stream.ready")
	sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", a, 200)
}
