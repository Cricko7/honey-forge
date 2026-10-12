package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
)

func TestSessionRetiresExpiredResultAndKeepsJournalOutcome(t *testing.T) {
	trapID := string(contract.NewID())
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), trapID, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	s := NewService(j, &fakeRunner{})
	stop := configCommand("stop", nil)
	if _, err := s.Execute(t.Context(), stop); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{Subprotocols: []string{"resource-stream.v1"}}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Error(err)
			return
		}
		var hello contract.Envelope
		if err := conn.ReadJSON(&hello); err != nil {
			t.Error(err)
			return
		}
		reply := func(kind string, replyTo *contract.ID, body any) error {
			return conn.WriteJSON(map[string]any{"message_id": contract.NewID(), "reply_to": replyTo, "type": kind, "payload": body})
		}
		if err := reply("agent.welcome", &hello.MessageID, map[string]any{"trap_id": trapID, "heartbeat_interval_seconds": 5}); err != nil {
			t.Error(err)
			return
		}
		var result contract.Envelope
		if err := conn.ReadJSON(&result); err != nil {
			t.Error(err)
			return
		}
		if result.Type != "command.result" {
			t.Errorf("unexpected message %s", result.Type)
			return
		}
		if err := reply("error", &result.MessageID, map[string]any{"error": map[string]string{"code": "command_expired", "message": "Command expired"}}); err != nil {
			t.Error(err)
			return
		}
		// A later valid message must still be processed on the same connection.
		if err := reply("command.dispatch", nil, configCommand("stop", nil)); err != nil {
			t.Error(err)
			return
		}
		for range 2 {
			var next contract.Envelope
			if err := conn.ReadJSON(&next); err != nil {
				t.Error(err)
				return
			}
		}
		if err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4401, "revoked"), time.Now().Add(time.Second)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	err = session(ctx, Options{URL: "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream", TrapID: trapID, Token: "test-token", TLS: server.Client().Transport.(*http.Transport).TLSClientConfig}, j, s, &Local{}, nil)
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("expiry interrupted session: %v", err)
	}
	state := j.LoadState()
	if state.Awaiting[stop.CommandID] || state.Results[stop.CommandID].Status != commands.Succeeded {
		t.Fatalf("expired delivery was not retired with its outcome retained: %+v", state)
	}
}
