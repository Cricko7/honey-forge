package agent

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalTLSRejectsOtherAgentCertificate(t *testing.T) {
	config, certificate, err := newLocalTLS()
	if err != nil {
		t.Fatal(err)
	}

	_, otherCertificate, err := newLocalTLS()
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer((&Local{Token: "private"}).Handler(t.Context()))
	server.TLS = config
	server.StartTLS()
	defer server.Close()

	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/trap-stream"
	client, err := DialLocal(t.Context(), endpoint, "private", certificate)
	if err != nil {
		t.Fatalf("trusted local certificate rejected: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	if client, err := DialLocal(t.Context(), endpoint, "private", otherCertificate); err == nil {
		client.Close()
		t.Fatal("certificate from another agent accepted")
	}
}

func TestLocalClientRequiresWSSLoopbackAndTrust(t *testing.T) {
	_, certificate, err := newLocalTLS()
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name, address string
		cert          []byte
	}{
		{"plaintext", "ws://127.0.0.1:1234/trap-stream", certificate},
		{"non-loopback", "wss://192.0.2.1:1234/trap-stream", certificate},
		{"credentials in URL", "wss://token@127.0.0.1:1234/trap-stream", certificate},
		{"wrong path", "wss://127.0.0.1:1234/other", certificate},
		{"no trust", "wss://127.0.0.1:1234/trap-stream", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := localClientTLS(tt.address, tt.cert); err == nil {
				t.Fatal("unsafe local transport accepted")
			}
		})
	}
}
