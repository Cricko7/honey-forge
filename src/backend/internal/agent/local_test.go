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
	"honey-forge/internal/platform/traffic"
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
	config, certificate, err := newLocalTLS()
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(local.Handler(t.Context()))
	server.TLS = config
	server.StartTLS()
	defer server.Close()

	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/trap-stream"
	clientTLS, err := localClientTLS(endpoint, certificate)
	if err != nil {
		t.Fatal(err)
	}

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

			conn, response, err := (&websocket.Dialer{TLSClientConfig: clientTLS}).DialContext(t.Context(), endpoint, headers)
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

	client, err := DialLocal(t.Context(), endpoint, "local-token", certificate)
	if err != nil {
		t.Fatal(err)
	}

	defer client.Close()

	frame := traffic.Frame{
		ConnectionID:  string(contract.NewID()),
		Sequence:      1,
		Kind:          "data",
		RemoteAddress: "127.0.0.1:1234",
		LocalAddress:  "127.0.0.1:2222",
		Data:          []byte{0, 255, 1},
	}
	for n, kind := range []string{"open", "data", "close"} {
		copy := frame
		copy.Sequence, copy.Kind = int64(n+1), kind
		if kind != "data" {
			copy.Data = nil
		}

		client.Mirror(copy)
		select {
		case got := <-local.Traffic:
			if string(got.Data) != string(copy.Data) || got.ConnectionID != copy.ConnectionID || got.Kind != kind {
				t.Fatal("wire copy changed")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing local traffic copy", kind)
		}
	}

	if len(j.Pending()) != 0 {
		t.Fatal("raw traffic entered telemetry")
	}

	e := events.AgentEvent{
		EventID:         string(contract.NewID()),
		EventType:       "tcp.connection_opened",
		TypeID:          "tcp-banner",
		TypeVersion:     1,
		ProfileRevision: 1,
		OccurredAt:      time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:       string(contract.NewID()),
		SessionSequence: 1,
		Source:          events.Source{IP: "127.0.0.1", Port: 12345},
		Destination:     events.Destination{Protocol: "tcp", Port: 2222},
		Data:            json.RawMessage(`{"listener_name":"demo"}`),
	}
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

func TestLocalRejectsPlaintextBeforeAuthentication(t *testing.T) {
	local := &Local{Token: "private"}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/trap-stream", nil)
	request.Header.Set("Authorization", "Bearer private")
	response := httptest.NewRecorder()
	local.Handler(t.Context()).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("plaintext local transport accepted: %d", response.Code)
	}
}
