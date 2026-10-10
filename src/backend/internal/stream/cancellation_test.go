package stream

import (
	"context"
	"crypto/tls"
	"errors"
	"honey-forge/internal/contract"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func TestReadCancellation(t *testing.T) {
	cancelReady := make(chan context.CancelFunc, 1)
	readDone := make(chan error, 1)
	r := gin.New()
	r.Use(contract.Middleware())
	r.GET("/api/stream", func(c *gin.Context) {
		contract.SetPrincipal(c, contract.Principal{Role: contract.Admin})
		socket, err := Upgrade(c, contract.FrontendStream)
		if err != nil {
			readDone <- err
			return
		}
		defer socket.Close()
		ctx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()
		cancelReady <- cancel
		_, err = socket.Read(ctx)
		readDone <- err
	})
	server := httptest.NewTLSServer(r)
	defer server.Close()
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client, _, err := dialer.Dial("wss"+strings.TrimPrefix(server.URL, "https")+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cancel := <-cancelReady
	cancel()
	if err := <-readDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("context not propagated: %v", err)
	}
}
