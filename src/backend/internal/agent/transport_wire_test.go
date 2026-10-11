package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"honey-forge/internal/contract"
)

func TestWireSendPadding(t *testing.T) {
	telemetry := map[string]any{"batch_id": string(contract.NewID()), "events": []any{map[string]any{"event_id": string(contract.NewID()), "data": "unchanged"}}}
	messages := []struct {
		kind    string
		payload map[string]any
	}{
		{"agent.heartbeat", map[string]any{"runtime": "unchanged"}},
		{"telemetry.batch", telemetry},
		{"telemetry.batch", telemetry},
	}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(rw, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		w := wire{conn: conn}
		for _, message := range messages {
			if _, err := w.send(message.kind, message.payload); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	defer server.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for _, expected := range messages {
		kind, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind != websocket.TextMessage {
			t.Fatal("expected text frame")
		}
		padding := len(raw) - len(bytes.TrimRight(raw, " "))
		if padding < 64 || padding > 1024 {
			t.Fatalf("padding = %d, want 64..1024", padding)
		}
		message, err := contract.DecodeEnvelope(raw)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(expected.payload)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(message.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if message.Type != expected.kind || !bytes.Equal(want, got) {
			t.Fatalf("changed envelope: %+v", message)
		}
	}
}

func TestPadMessageLimit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		size      int
		wantError bool
	}{
		{"normal", 100, false},
		{"one byte left", contract.MaxBodyBytes - 1, false},
		{"at limit", contract.MaxBodyBytes, false},
		{"over limit", contract.MaxBodyBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := append([]byte(`{"data":"`), bytes.Repeat([]byte("x"), tc.size-11)...)
			raw = append(raw, []byte(`"}`)...)
			padded, err := padMessage(raw)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if tc.wantError {
				return
			}
			if len(padded) > contract.MaxBodyBytes || !json.Valid(padded) || !bytes.Equal(bytes.TrimRight(padded, " "), raw) {
				t.Fatal("padding changed JSON or exceeded message limit")
			}
		})
	}
}
