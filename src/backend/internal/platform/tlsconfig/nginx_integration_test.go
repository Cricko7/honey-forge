package tlsconfig

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This needs only Docker, with no database or running HoneyForge stack.
func TestNginxMutualTLSIntegration(t *testing.T) {
	if os.Getenv("TEST_NGINX_MTLS") != "1" {
		t.Skip("set TEST_NGINX_MTLS=1 to test the real nginx TLS hops")
	}
	dir := t.TempDir()
	ca, caKey := testCA(t)
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer fixture.Close()
	pair := fixture.TLS.Certificates[0]
	key := pair.PrivateKey.(crypto.Signer)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	serverDER, err := x509.CreateCertificate(rand.Reader, leaf, leaf, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err = x509.ParseCertificate(serverDER)
	if err != nil {
		t.Fatal(err)
	}
	pair.Certificate = [][]byte{serverDER}
	private, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture has localhost SAN; use localhost for both proxy_ssl_name and SNI.
	for name, data := range map[string][]byte{"public.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]}), "public.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	rawCA, err := os.ReadFile(ca)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents-ca.crt"), rawCA, 0600); err != nil {
		t.Fatal(err)
	}
	proxyCert, proxyKey, _, err := IssueClient(ca, caKey, ResourceProxyName)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"resource-proxy.crt": proxyCert, "resource-proxy.key": proxyKey} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	config, err := AgentServer(filepath.Join(dir, "public.crt"), filepath.Join(dir, "public.key"), ca)
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := AgentIdentity(r, ca)
		if err == nil && id == "trap-one" {
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(200)
	}))
	backend.Listener.Close()
	backend.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	backend.TLS = config
	backend.StartTLS()
	defer backend.Close()
	port := backend.Listener.Addr().(*net.TCPAddr).Port
	raw, err := os.ReadFile("../../../../../deploy/nginx/honey-forge.conf")
	if err != nil {
		t.Fatal(err)
	}
	// Use the checked-in production resource server, including client verification
	// and the overwritten forwarding header. Docker Desktop reaches the host peer.
	configText := strings.ReplaceAll(string(raw), "\r\n", "\n")
	resource := configText[strings.LastIndex(configText, "server {\n"):]
	resource = strings.NewReplacer("resources.example.org", "localhost", "resources.crt", "public.crt", "resources.key", "public.key", "backend-ca.crt", "public.crt", "https://127.0.0.1:8443", fmt.Sprintf("https://host.docker.internal:%d", port)).Replace(resource)
	if err := os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte("events {}\nhttp {\n"+resource+"\n}"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	name := fmt.Sprintf("mask7-test-%d", time.Now().UnixNano())
	image := os.Getenv("TEST_NGINX_IMAGE")
	if image == "" {
		image = "nginx:alpine"
	}
	output, err := exec.CommandContext(ctx, "docker", "run", "--detach", "--name", name, "--publish", "127.0.0.1::443", "--add-host", "host.docker.internal:host-gateway", "--volume", dir+":/etc/nginx/certs:ro", image, "nginx", "-c", "/etc/nginx/certs/nginx.conf", "-g", "daemon off;").CombinedOutput()
	if err != nil {
		t.Fatalf("start nginx: %v %s", err, output)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(cleanup, "docker", "rm", "--force", name).CombinedOutput(); err != nil {
			t.Errorf("remove test nginx: %v %s", err, output)
		}
	}()
	output, err = exec.CommandContext(ctx, "docker", "port", name, "443/tcp").CombinedOutput()
	if err != nil {
		t.Fatalf("nginx port: %v %s", err, output)
	}
	_, mappedPort, err := net.SplitHostPort(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatal(err)
	}
	address := "https://localhost:" + mappedPort + "/assets/stream"
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	for _, tt := range []struct {
		name, identity string
		status         int
	}{{"anonymous", "", 200}, {"spoofed forwarding", "", 200}, {"valid agent", "trap-one", 204}, {"different trap", "another-trap", 200}} {
		t.Run(tt.name, func(t *testing.T) {
			tlsClient := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			if tt.identity != "" {
				cert, key, _, err := IssueClient(ca, caKey, tt.identity)
				if err != nil {
					t.Fatal(err)
				}
				pair, err := tls.X509KeyPair(cert, key)
				if err != nil {
					t.Fatal(err)
				}
				tlsClient.Certificates = []tls.Certificate{pair}
			}
			transport := &http.Transport{TLSClientConfig: tlsClient, DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			var response *http.Response
			var requestError error
			for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
				request, err := http.NewRequestWithContext(ctx, "GET", address, nil)
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("X-Agent-Certificate", url.QueryEscape(string(proxyCert)))
				response, err = client.Do(request)
				requestError = err
				if err == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if response == nil {
				logs, _ := exec.CommandContext(ctx, "docker", "logs", name).CombinedOutput()
				t.Fatalf("nginx unavailable: %v %s", requestError, logs)
			}
			response.Body.Close()
			if response.StatusCode != tt.status {
				t.Fatalf("status=%d want=%d", response.StatusCode, tt.status)
			}
		})
	}
}
