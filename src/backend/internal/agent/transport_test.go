package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the agent's actual dial path: no HTTP request (and thus no bearer
// token) may reach a peer until TLS authentication succeeds.
func TestSessionTLSAuthentication(t *testing.T) {
	for _, tt := range []struct {
		name       string
		untrusted  bool
		wrongName  bool
		expired    bool
		maxVersion uint16
		plaintext  bool
		accepted   bool
	}{
		{name: "trusted TLS 1.2", maxVersion: tls.VersionTLS12, accepted: true},
		{name: "trusted TLS 1.3", maxVersion: tls.VersionTLS13, accepted: true},
		{name: "untrusted certificate", untrusted: true},
		{name: "wrong server name", wrongName: true},
		{name: "expired certificate", expired: true},
		{name: "TLS 1.1", maxVersion: tls.VersionTLS11},
		{name: "plaintext peer", plaintext: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			observed := make(chan struct {
				token   string
				version uint16
			}, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				version := uint16(0)
				if r.TLS != nil {
					version = r.TLS.Version
				}

				observed <- struct {
					token   string
					version uint16
				}{r.Header.Get("Authorization"), version}
				w.WriteHeader(http.StatusUnauthorized)
			}))
			certificate, leaf := sessionTestCertificate(t, tt.expired)
			server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS10, MaxVersion: tt.maxVersion}
			if tt.plaintext {
				server.Start()
			} else {
				server.StartTLS()
			}

			defer server.Close()

			roots := x509.NewCertPool()
			if !tt.untrusted {
				roots.AddCert(leaf)
			}

			config := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			if tt.wrongName {
				config.ServerName = "other.example"
			}

			journal, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "tls-test", 1<<20)
			if err != nil {
				t.Fatal(err)
			}

			defer journal.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			err = session(ctx, Options{URL: "wss://" + server.Listener.Addr().String() + "/assets/stream", Token: "tls-test-private-token", TLS: config}, journal, NewService(journal, nil), nil, nil)
			if tt.accepted {
				if !errors.Is(err, ErrRevoked) {
					t.Fatalf("trusted TLS must reach the authenticated HTTP boundary: %v", err)
				}

				select {
				case request := <-observed:
					if request.token != "Bearer tls-test-private-token" || request.version != tt.maxVersion {
						t.Fatalf("unexpected authenticated request: %+v", request)
					}
				default:
					t.Fatal("trusted peer did not receive request")
				}

				return
			}

			if err == nil || errors.Is(err, ErrRevoked) || !strings.Contains(err.Error(), "connect backend") {
				t.Fatalf("unsafe peer must fail before HTTP authentication: %v", err)
			}

			select {
			case <-observed:
				t.Fatal("unsafe peer received an HTTP request with agent credentials")
			default:
			}
		})
	}
}

func sessionTestCertificate(t *testing.T, expired bool) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    now.Add(-2 * time.Hour),
		NotAfter:     now.Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if expired {
		leaf.NotAfter = now.Add(-time.Hour)
	}

	raw, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}

	return tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key}, parsed
}
