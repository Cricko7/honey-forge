package events

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"honey-forge/modules/agentws"
)

func TestKafkaConnectionUsesAuthenticatedTLS(t *testing.T) {
	for _, tt := range []struct {
		name    string
		trusted bool
	}{
		{"trusted broker", true}, {"untrusted broker", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			template := httptest.NewTLSServer(http.NotFoundHandler())
			template.Close()
			listener, err := tls.Listen("tcp", "127.0.0.1:0", template.TLS)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			handshake := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					handshake <- err
					return
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					handshake <- err
					return
				}
				handshake <- conn.(*tls.Conn).Handshake()
			}()
			config := template.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			if !tt.trusted {
				config.RootCAs = x509.NewCertPool()
			}
			publisher, err := NewKafkaPublisher([]string{listener.Addr().String()}, "tls-test", config)
			if err != nil {
				t.Fatal(err)
			}
			defer publisher.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			published := make(chan error, 1)
			go func() { published <- publisher.Publish(ctx, agentws.Identity{}, "batch", nil) }()
			select {
			case err := <-handshake:
				if (err == nil) != tt.trusted {
					t.Fatalf("Kafka TLS handshake=%v, want trusted=%v", err, tt.trusted)
				}
			case <-ctx.Done():
				t.Fatal("Kafka did not attempt TLS handshake")
			}
			cancel()
			if err := <-published; !errors.Is(err, context.Canceled) {
				t.Fatalf("unacknowledged publish should respect cancellation: %v", err)
			}
		})
	}
}
