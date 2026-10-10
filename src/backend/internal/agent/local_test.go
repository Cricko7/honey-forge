package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
)

func TestLocalWebSocketAuthenticationAndCommit(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	local, err := NewLocal(j, "local-token")
	if err != nil {
		t.Fatal(err)
	}
	local.Configure(demoSnapshot())
	server := httptest.NewServer(local.Handler(t.Context()))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/trap-stream"
	for _, tc := range []struct {
		name, token, origin string
		status              int
	}{{"missing token", "", "", 401}, {"wrong token", "wrong", "", 401}, {"browser origin", "local-token", "https://attacker.example", 403}} {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			headers.Set("Authorization", "Bearer "+tc.token)
			if tc.origin != "" {
				headers.Set("Origin", tc.origin)
			}
			conn, response, err := websocket.DefaultDialer.DialContext(t.Context(), endpoint, headers)
			if conn != nil {
				conn.Close()
			}
			if response != nil {
				defer response.Body.Close()
			}
			if err == nil || response == nil || response.StatusCode != tc.status {
				t.Fatalf("status %v error %v", response, err)
			}
		})
	}
	client, err := DialLocal(t.Context(), endpoint, "local-token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	e := events.AgentEvent{EventID: string(contract.NewID()), EventType: "tcp.connection_opened", TypeID: "tcp-banner", TypeVersion: 1, ProfileRevision: 1, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), SessionID: string(contract.NewID()), SessionSequence: 1, Source: events.Source{IP: "127.0.0.1", Port: 12345}, Destination: events.Destination{Protocol: "tcp", Port: 2222}, Data: json.RawMessage(`{"listener_name":"demo"}`)}
	if err := client.Emit(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if len(j.Pending()) != 1 {
		t.Fatal("acknowledged event absent from journal")
	}
	e.EventID = string(contract.NewID())
	e.ProfileRevision = 2
	if err := client.Emit(context.Background(), e); err == nil {
		t.Fatal("accepted unissued revision")
	}
	if len(j.Pending()) != 1 {
		t.Fatal("invalid event was queued")
	}
}
