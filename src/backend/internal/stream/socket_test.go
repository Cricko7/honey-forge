package stream

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
)

func TestTransport(t *testing.T) {
	for _, tt := range []struct {
		name  string
		kind  int
		data  string
		close int
	}{{"valid", websocket.TextMessage, `{"message_id":"11111111-1111-4111-8111-111111111111","type":"hello","payload":{}}`, 0}, {"binary", websocket.BinaryMessage, "x", 1003}, {"size", websocket.TextMessage, strings.Repeat("x", contract.MaxBodyBytes+1), 1009}} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(contract.Middleware())
			r.GET("/api/stream", func(c *gin.Context) {
				contract.SetPrincipal(c, contract.Principal{Role: contract.Admin})
				socket, err := Upgrade(c, contract.FrontendStream)
				if err != nil {
					return
				}
				defer socket.Close()
				message, err := socket.Read(c.Request.Context())
				if err == nil {
					if err := socket.Reply(c.Request.Context(), message, "hello.ok", map[string]any{}); err != nil {
						t.Error(err)
					}
				}
			})
			server := httptest.NewTLSServer(r)
			defer server.Close()
			dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, EnableCompression: true, Subprotocols: []string{"dashboard-stream.v1"}}
			client, response, err := dialer.Dial("wss"+strings.TrimPrefix(server.URL, "https")+"/api/stream", http.Header{"Origin": []string{server.URL}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if response.Header.Get("X-Request-ID") == "" || response.Header.Get("Sec-WebSocket-Extensions") != "" {
				t.Fatal("handshake headers")
			}
			if err := client.WriteMessage(tt.kind, []byte(tt.data)); err != nil {
				t.Fatal(err)
			}
			_, b, err := client.ReadMessage()
			if tt.close != 0 {
				var closed *websocket.CloseError
				if !errors.As(err, &closed) || closed.Code != tt.close {
					t.Fatalf("close %v", err)
				}
			} else if err != nil || !strings.Contains(string(b), `"reply_to":"11111111-1111-4111-8111-111111111111"`) {
				t.Fatalf("reply %s %v", b, err)
			}
		})
	}
}

func TestFailedHandshake(t *testing.T) {
	r := gin.New()
	r.Use(contract.Middleware())
	r.GET("/api/stream", func(c *gin.Context) {
		_, err := Upgrade(c, contract.FrontendStream)
		if err == nil {
			t.Error("anonymous handshake accepted")
		}
	})
	s := httptest.NewTLSServer(r)
	defer s.Close()
	client := s.Client()
	response, err := client.Get(s.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 401 || response.Header.Get("X-Request-ID") == "" {
		t.Fatal("unsafe handshake")
	}
}
