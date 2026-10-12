package tlsconfig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCA(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(48 * time.Hour),
	}
	raw, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
	for file, data := range map[string][]byte{certFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), keyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})} {
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	return certFile, keyFile
}

func TestAgentClientPinningAndKeyRotation(t *testing.T) {
	ca, caKey := testCA(t)
	cert, key, _, err := IssueClient(ca, caKey, "trap-one")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certFile, keyFile, pinsFile := filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"), filepath.Join(dir, "pins")
	for file, data := range map[string][]byte{certFile: cert, keyFile: key} {
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	roots, err := clientRoots(ca)
	if err != nil {
		t.Fatal(err)
	}

	server.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()

	leaf := server.Certificate()
	// Trust remains mandatory in addition to the pin.
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw}), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(pinsFile, []byte(SPKIPin(leaf)), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := AgentClient(ca, certFile, keyFile, pinsFile)
	if err != nil {
		t.Fatal(err)
	}

	transport := &http.Transport{TLSClientConfig: config, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	response.Body.Close()
	if err := os.WriteFile(pinsFile, []byte("sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="), 0600); err != nil {
		t.Fatal(err)
	}

	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("wrong pin accepted")
	}

	if err := os.WriteFile(pinsFile, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := AgentClient(ca, certFile, keyFile, pinsFile); err == nil {
		t.Fatal("invalid pins accepted")
	}

	if err := os.WriteFile(pinsFile, []byte(SPKIPin(leaf)), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(keyFile, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}

	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("invalid rotated key accepted")
	}
}

func TestAgentIdentityRejectsUntrustedForwarding(t *testing.T) {
	ca, key := testCA(t)
	cert, _, _, err := IssueClient(ca, key, "trap-one")
	if err != nil {
		t.Fatal(err)
	}

	block, _ := pem.Decode(cert)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	issuer, err := tls.LoadX509KeyPair(ca, key)
	if err != nil {
		t.Fatal(err)
	}

	issuerCert, err := x509.ParseCertificate(issuer.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}

	expired := *leaf
	expired.NotBefore = time.Now().Add(-time.Hour)
	expired.NotAfter = time.Now().Add(-time.Minute)
	expiredDER, err := x509.CreateCertificate(rand.Reader, &expired, issuerCert, leaf.PublicKey, issuer.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	expiredPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: expiredDER})
	for _, tt := range []struct {
		name, peer, header string
		valid              bool
	}{
		{"direct", "trap-one", "", true},
		{"trusted proxy", ResourceProxyName, url.QueryEscape(string(cert)), true},
		{"spoofed header", "", url.QueryEscape(string(cert)), false},
		{"missing proxy leaf", ResourceProxyName, "", false},
		{"bad forwarded leaf", ResourceProxyName, "invalid", false},
		{"expired forwarded leaf", ResourceProxyName, url.QueryEscape(string(expiredPEM)), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "https://example/assets/stream", nil)
			if tt.peer != "" {
				peer := *leaf
				peer.Subject.CommonName = tt.peer
				request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{&peer}}}
			}

			request.Header.Set("X-Agent-Certificate", tt.header)
			identity, err := AgentIdentity(request, ca)
			if (err == nil) != tt.valid || tt.valid && identity != "trap-one" {
				t.Fatalf("identity=%q err=%v", identity, err)
			}
		})
	}
}

func TestAgentServerEnforcesMTLSAndReloadsCertificates(t *testing.T) {
	ca, caKey := testCA(t)
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer fixture.Close()

	certificate := fixture.TLS.Certificates[0]
	private, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	serverCert, serverKey, pins := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"), filepath.Join(dir, "pins")
	for file, data := range map[string][]byte{
		serverCert: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}),
		serverKey:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}),
		pins:       []byte(SPKIPin(fixture.Certificate())),
	} {
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	config, err := AgentServer(serverCert, serverKey, ca)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := AgentIdentity(r, ca)
		if err != nil || id != "trap-one" {
			w.WriteHeader(200)
			return
		}

		w.WriteHeader(204)
	}))
	server.TLS = config
	server.StartTLS()
	defer server.Close()

	clientCert, clientKey, _, err := IssueClient(ca, caKey, "trap-one")
	if err != nil {
		t.Fatal(err)
	}

	certFile, keyFile := filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key")
	writePair := func(cert, key []byte) {
		t.Helper()
		if err := os.WriteFile(certFile, cert, 0600); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(keyFile, key, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writePair(clientCert, clientKey)
	clientTLS, err := AgentClient(serverCert, certFile, keyFile, pins)
	if err != nil {
		t.Fatal(err)
	}

	transport := &http.Transport{TLSClientConfig: clientTLS, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	check := func(want int) {
		t.Helper()
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}

		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("status=%d want=%d", response.StatusCode, want)
		}
	}
	check(204)
	// Rotation uses a new key; existing client config rereads the key pair.
	rotatedCert, rotatedKey, _, err := IssueClient(ca, caKey, "trap-one")
	if err != nil {
		t.Fatal(err)
	}

	if string(rotatedKey) == string(clientKey) {
		t.Fatal("rotation reused private key")
	}

	writePair(rotatedCert, rotatedKey)
	check(204)
	otherCA, otherKey := testCA(t)
	untrustedCert, untrustedKey, _, err := IssueClient(otherCA, otherKey, "trap-one")
	if err != nil {
		t.Fatal(err)
	}

	writePair(untrustedCert, untrustedKey)
	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("untrusted client accepted")
	}

	writePair(clientCert, clientKey)
	// Anonymous browser traffic is still allowed, including spoofed headers.
	transport.TLSClientConfig = &tls.Config{RootCAs: clientTLS.RootCAs, MinVersion: tls.VersionTLS12}
	check(200)
	// A broken API key rotation must fail instead of serving stale credentials.
	if err := os.WriteFile(serverKey, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}

	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("broken server key rotation accepted")
	}
}
