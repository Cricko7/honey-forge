package frontendws

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/internal/stream"
	"honey-forge/modules/auth"
)

type fakeSessions struct {
	principal     contract.Principal
	err           error
	calls         atomic.Int64
	blockDelivery bool
}

func (f *fakeSessions) ResolveSession(ctx context.Context, cookie string) (auth.ResolvedSession, error) {
	if f.calls.Add(1) > 1 && f.blockDelivery {
		<-ctx.Done()
		return auth.ResolvedSession{}, ctx.Err()
	}
	return auth.ResolvedSession{View: auth.SessionView{User: auth.User{ID: string(f.principal.UserID), OrganizationID: string(f.principal.OrganizationID), Role: string(f.principal.Role)}}}, f.err
}

func frontendServer(t *testing.T, j journal, s sessions) (*httptest.Server, *Handler, context.CancelFunc) {
	t.Helper()
	codec, err := contract.NewCursorCodec([]byte("integration-cursor-key-32-bytes!"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := contract.NewBrowserPolicy([]string{"https://operator.example"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	h := NewHandler(j, s, codec, policy, slog.New(slog.NewTextHandler(io.Discard, nil)), ctx)
	router := gin.New()
	router.Use(contract.Middleware())
	h.Register(router)
	server := httptest.NewTLSServer(router)
	t.Cleanup(func() { cancel(); h.Wait(); server.Close() })
	return server, h, cancel
}
func frontendDial(t *testing.T, server *httptest.Server) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	d := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Subprotocols: []string{"dashboard-stream.v1"}}
	return d.Dial("wss"+strings.TrimPrefix(server.URL, "https")+stream.FrontendPath, http.Header{"Origin": []string{"https://operator.example"}, "Cookie": []string{"__Host-session=opaque"}})
}

func TestHandshakeAuthenticationFailures(t *testing.T) {
	for _, tt := range []struct {
		name                       string
		role                       contract.Role
		sessionError, journalError error
		status                     int
		code                       string
	}{
		{"invalid cookie", contract.Admin, auth.ErrUnauthorized, nil, 401, "unauthenticated"},
		{"agent role", contract.Agent, nil, nil, 403, "forbidden"},
		{"session database unavailable", contract.Admin, auth.ErrUnavailable, nil, 503, "database_unavailable"},
		{"journal unavailable", contract.Admin, nil, auth.ErrUnavailable, 503, "database_unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &fakeSessions{principal: contract.Principal{UserID: contract.NewID(), OrganizationID: contract.NewID(), Role: tt.role}, err: tt.sessionError}
			server, _, _ := frontendServer(t, &fakeJournal{err: tt.journalError}, s)
			c, res, err := frontendDial(t, server)
			if c != nil {
				c.Close()
				t.Fatal("unexpected upgrade")
			}
			if err == nil || res == nil {
				t.Fatal("missing HTTP error")
			}
			defer res.Body.Close()
			b, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tt.status || !strings.Contains(string(b), tt.code) {
				t.Fatalf("status %d body %s", res.StatusCode, b)
			}
		})
	}
}

func TestSlowConsumerClosesWithoutBlockingJournal(t *testing.T) {
	principal := contract.Principal{UserID: contract.NewID(), OrganizationID: contract.NewID(), Role: contract.Admin}
	s := &fakeSessions{principal: principal, blockDelivery: true}
	changes := make([]Change, 1002)
	for i := range changes {
		changes[i] = Change{Sequence: int64(i + 1), Type: "profile.changed", OccurredAt: time.Now(), Data: json.RawMessage(`{}`)}
	}
	j := &largeJournal{changes: changes}
	server, h, _ := frontendServer(t, j, s)
	token, err := stream.EncodeCursor(h.cursors, principal.OrganizationID, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := frontendDial(t, server)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.WriteJSON(map[string]any{"message_id": contract.NewID(), "type": "stream.subscribe", "payload": map[string]any{"after": token}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, err = c.ReadMessage()
	var closed *websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != 4410 || closed.Text != "slow_consumer" {
		t.Fatalf("close %v", err)
	}
}

type largeJournal struct{ changes []Change }

func (j *largeJournal) Boundary(context.Context) (int64, int64, error) {
	return int64(len(j.changes)), 0, nil
}
func (j *largeJournal) Changes(ctx context.Context, after, end int64) ([]Change, error) {
	last := min(after+100, end)
	return j.changes[after:last], nil
}

func TestFirstSubscribeTimeout(t *testing.T) {
	s := &fakeSessions{principal: contract.Principal{UserID: contract.NewID(), OrganizationID: contract.NewID(), Role: contract.Viewer}}
	server, _, _ := frontendServer(t, &fakeJournal{}, s)
	c, _, err := frontendDial(t, server)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SetReadDeadline(time.Now().Add(7 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, err = c.ReadMessage()
	var closed *websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != 4408 {
		t.Fatalf("close %v", err)
	}
}
