package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
)

func TestSessionWelcomeBoundary(t *testing.T) {
	for _, tt := range []struct {
		name     string
		protocol bool
		kind     int
		want     string
	}{
		{"text welcome", true, websocket.TextMessage, ""},
		{"binary welcome", true, websocket.BinaryMessage, "unsupported backend frame"},
		{"missing negotiated protocol", false, websocket.TextMessage, "invalid backend subprotocol"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			trapID := string(contract.NewID())
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				if tt.protocol {
					upgrader.Subprotocols = []string{"resource-stream.v1"}
				}

				conn, err := upgrader.Upgrade(w, r, nil)
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
					if tt.protocol {
						t.Error(err)
					}

					return
				}

				raw, err := json.Marshal(map[string]any{
					"message_id": contract.NewID(),
					"reply_to":   hello.MessageID,
					"type":       "agent.welcome",
					"payload":    map[string]any{"trap_id": trapID, "heartbeat_interval_seconds": 5},
				})
				if err != nil {
					t.Error(err)
					return
				}

				if err := conn.WriteMessage(tt.kind, raw); err != nil {
					t.Error(err)
					return
				}

				if err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4401, "revoked"), time.Now().Add(time.Second)); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()

			journal, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), trapID, 1<<20)
			if err != nil {
				t.Fatal(err)
			}

			defer journal.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			config := server.Client().Transport.(*http.Transport).TLSClientConfig
			err = session(ctx, Options{
				URL:    "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream",
				TrapID: trapID,
				Token:  "test-token",
				TLS:    config,
			}, journal, NewService(journal, nil), &Local{}, nil)
			if tt.want == "" {
				if !errors.Is(err, ErrRevoked) {
					t.Fatalf("valid welcome did not enter the session: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
		})
	}
}
