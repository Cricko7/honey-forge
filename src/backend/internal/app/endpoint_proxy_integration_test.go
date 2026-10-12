//go:build integration

package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/tlsconfig"
	"honey-forge/modules/auth"
)

// Exercise the checked-in nginx configuration, verified TLS on both hops,
// hostname isolation, anonymous assets, and an authenticated WebSocket upgrade.
func TestTelemetryProxyIntegration(t *testing.T) {
	if os.Getenv("TEST_NGINX_PROXY") != "1" {
		t.Skip("TEST_NGINX_PROXY=1 and Linux Docker required")
	}

	directory := t.TempDir()
	clientCA, clientCAKey := proxyClientCA(t, directory)
	r := openIntegrationRuntimeWithOptions(t, nil, nil, "", clientCA)
	operator := registerOperator(t, r, "proxy@example.test", auth.OrganizationInput{Mode: "create", Name: "Proxy"})
	_, trap, _ := createIntegrationTrap(t, r, operator)
	credentials := decodeIntegration[struct {
		Token string `json:"token"`
	}](t, sendOperator(t, r, "POST", "/api/traps/"+trap.ID+"/agent-credentials", `{"expected_generation":0}`, operator, 200))
	_, roots := proxyCertificate(t, directory)
	agentCert, agentKey, _, err := tlsconfig.IssueClient(clientCA, clientCAKey, trap.ID)
	if err != nil {
		t.Fatal(err)
	}

	clientPair, err := tls.X509KeyPair(agentCert, agentKey)
	if err != nil {
		t.Fatal(err)
	}

	backend := httptest.NewUnstartedServer(r.Router)
	backend.TLS, err = tlsconfig.AgentServer(filepath.Join(directory, "public.crt"), filepath.Join(directory, "public.key"), clientCA)
	if err != nil {
		t.Fatal(err)
	}

	backend.StartTLS()
	defer backend.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	proxyAddress := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile("../../../../deploy/nginx/honey-forge.conf")
	if err != nil {
		t.Fatal(err)
	}

	config := strings.NewReplacer("listen 127.0.0.1:443 ssl", "listen "+proxyAddress+" ssl", "resources.honey-forge-seg-fault.ru", "resources.example", "honey-forge-seg-fault.ru", "operator.example", "https://127.0.0.1:8443", backend.URL).Replace(string(raw))
	if err := os.WriteFile(filepath.Join(directory, "nginx.conf"), []byte("events {}\nhttp {\n"+config+"\n}"), 0600); err != nil {
		t.Fatal(err)
	}

	name := "mask2-" + string(contract.NewID())
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "docker", "run", "--detach", "--name", name, "--network", "host", "--volume", directory+":/etc/nginx/certs:ro", "nginx:alpine", "nginx", "-c", "/etc/nginx/certs/nginx.conf", "-g", "daemon off;")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("start proxy: %v %s", err, output)
	}

	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if output, err := exec.CommandContext(cleanup, "docker", "rm", "--force", name).CombinedOutput(); err != nil {
			t.Errorf("remove proxy: %v %s", err, output)
		}
	}()

	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", proxyAddress)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: dial, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()

	ready := false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get("https://resources.example/")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	if !ready {
		logs, _ := exec.CommandContext(ctx, "docker", "logs", name).CombinedOutput()
		t.Fatalf("proxy not ready: %s", logs)
	}

	for _, tt := range []struct {
		name, host, path, contains string
		status                     int
	}{
		{"cover index", "resources.example", "/", "Resource Library", 200},
		{"cover asset", "resources.example", "/assets/stream", "use strict", 200},
		{"hidden API", "resources.example", "/api/session", "Not Found", 404},
		{"hidden frontend stream", "resources.example", "/api/stream", "Not Found", 404},
		{"operator session", "operator.example", "/api/session", "unauthenticated", 401},
		{"agent absent on operator", "operator.example", "/assets/stream", "404", 404},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response, err := client.Get("https://" + tt.host + tt.path)
			if err != nil {
				t.Fatal(err)
			}

			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}

			if response.StatusCode != tt.status || !strings.Contains(string(body), tt.contains) {
				t.Fatalf("%d %s", response.StatusCode, body)
			}
		})
	}

	dialer := websocket.Dialer{
		NetDialContext:   dial,
		TLSClientConfig:  &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{clientPair}, MinVersion: tls.VersionTLS12},
		Subprotocols:     []string{"resource-stream.v1"},
		HandshakeTimeout: 3 * time.Second,
	}
	for _, tt := range []struct {
		name, token string
		status      int
	}{{"missing token", "", 200}, {"invalid token", "wrong", 200}, {"valid token", credentials.Token, 101}} {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			if tt.token != "" {
				header.Set("Authorization", "Bearer "+tt.token)
			}

			socket, response, err := dialer.DialContext(ctx, "wss://resources.example/assets/stream", header)
			if response == nil || response.StatusCode != tt.status {
				t.Fatalf("handshake: %v %v", response, err)
			}

			if socket != nil {
				defer socket.Close()

				return
			}

			defer response.Body.Close()

			body, readErr := io.ReadAll(response.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}

			if err == nil || response.Header.Get("Content-Type") != "application/javascript; charset=utf-8" || strings.Contains(string(body), "agent") {
				t.Fatalf("invalid anonymous handshake: %s %v", body, err)
			}
		})
	}
}

func proxyClientCA(t *testing.T, directory string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(3),
		Subject:               pkix.Name{CommonName: "client CA"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
	}
	raw, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	certFile, keyFile := filepath.Join(directory, "agents-ca.crt"), filepath.Join(directory, "agents-ca.key")
	for file, data := range map[string][]byte{certFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), keyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})} {
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	cert, privateKey, _, err := tlsconfig.IssueClient(certFile, keyFile, tlsconfig.ResourceProxyName)
	if err != nil {
		t.Fatal(err)
	}

	for name, data := range map[string][]byte{"resource-proxy.crt": cert, "resource-proxy.key": privateKey} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	return certFile, keyFile
}

func proxyCertificate(t *testing.T, directory string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	leaf := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		DNSNames:              []string{"operator.example", "resources.example", "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	for name, data := range map[string][]byte{
		"public.crt":     certPEM,
		"resources.crt":  certPEM,
		"backend-ca.crt": certPEM,
		"public.key":     keyPEM,
		"resources.key":  keyPEM,
	} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("invalid fixture certificate")
	}

	return certificate, roots
}
