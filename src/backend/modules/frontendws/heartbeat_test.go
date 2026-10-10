package frontendws

import (
	"errors"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
)

func TestPingRequiresPongWithinTenSeconds(t *testing.T) {
	s := &fakeSessions{principal: contract.Principal{UserID: contract.NewID(), OrganizationID: contract.NewID(), Role: contract.Viewer}}
	server, _, _ := frontendServer(t, &fakeJournal{}, s)
	c, _, err := frontendDial(t, server)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pings := 0
	c.SetPingHandler(func(string) error { pings++; return nil }) // Intentionally omit pong.
	if err := c.WriteJSON(map[string]any{"message_id": contract.NewID(), "type": "stream.subscribe", "payload": map[string]any{"after": nil}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var e contract.Envelope
	if err := c.ReadJSON(&e); err != nil || e.Type != "stream.ready" {
		t.Fatalf("ready %s, %v", e.Type, err)
	}
	started := time.Now()
	_, _, err = c.ReadMessage()
	var closed *websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != 4408 || closed.Text != "timeout" {
		t.Fatalf("close %v", err)
	}
	if pings != 1 || time.Since(started) < 24*time.Second {
		t.Fatalf("ping count %d, closed too early", pings)
	}
	if s.calls.Load() < 3 {
		t.Fatal("session not verified on ping")
	}
}
