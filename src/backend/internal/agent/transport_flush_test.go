package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"honey-forge/internal/contract"
	"honey-forge/modules/events"
)

func TestSessionDrainsAcknowledgedBacklogWithoutFlushDelay(t *testing.T) {
	const trapID = "11111111-1111-4111-8111-111111111111"
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), trapID, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	for range 16 {
		if _, err := journal.Enqueue(events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), EventType: "http.block", Data: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}

	observed := make(chan error, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{Subprotocols: []string{"resource-stream.v1"}}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			observed <- err
			return
		}
		defer conn.Close()

		reply := func(message contract.Envelope, kind string, body any) error {
			return conn.WriteJSON(map[string]any{"message_id": contract.NewID(), "reply_to": message.MessageID, "type": kind, "payload": body})
		}
		var hello contract.Envelope
		if err := conn.ReadJSON(&hello); err != nil {
			observed <- err
			return
		}
		if err := reply(hello, "agent.welcome", map[string]any{"trap_id": trapID, "heartbeat_interval_seconds": 5}); err != nil {
			observed <- err
			return
		}

		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			observed <- err
			return
		}

		for index := range 16 {
			var message contract.Envelope
			if err := conn.ReadJSON(&message); err != nil {
				observed <- fmt.Errorf("backlog stalled after %d events: %w", index, err)
				return
			}
			if index == 0 {
				if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					observed <- err
					return
				}
			}

			var batch Batch
			if message.Type != "telemetry.batch" || payload(message, &batch) != nil {
				observed <- fmt.Errorf("unexpected backlog message %s", message.Type)
				return
			}
			if err := reply(message, "telemetry.ack", map[string]any{"batch_id": batch.BatchID, "acknowledged_event_ids": []string{batch.Events[0].EventID}, "stored_at": time.Now().UTC()}); err != nil {
				observed <- err
				return
			}
		}

		observed <- nil
	}))
	certificate, leaf := sessionTestCertificate(t, false)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()

	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	local, err := NewLocal(journal, "token")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- session(ctx, Options{URL: "wss://" + server.Listener.Addr().String(), TrapID: trapID, Token: "token", TLS: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, journal, NewService(journal, nil), local, nil)
	}()
	if err := <-observed; err != nil {
		t.Error(err)
	}

	cancel()
	<-done
}
