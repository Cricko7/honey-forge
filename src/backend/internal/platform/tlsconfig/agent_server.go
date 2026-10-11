package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
)

const ResourceProxyName = "resource-proxy"

// AgentServer allows browser traffic without a client certificate. Agent
// upgrades enforce mTLS separately, after TLS has verified supplied certificates.
func AgentServer(certFile, keyFile, caFile string) (*tls.Config, error) {
	load := func() (*tls.Config, error) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load API certificate: %w", err)
		}
		roots, err := clientRoots(caFile)
		if err != nil {
			return nil, err
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, ClientCAs: roots, ClientAuth: tls.VerifyClientCertIfGiven, SessionTicketsDisabled: true}, nil
	}
	config, err := load()
	if err != nil {
		return nil, err
	}
	config.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) { return load() }
	return config, nil
}

// Forwarded certificates are accepted only over authenticated proxy mTLS.
// A caller-controlled HTTP header alone never establishes an identity.
func AgentIdentity(request *http.Request, caFile string) (string, error) {
	if request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.VerifiedChains[0]) == 0 {
		return "", fmt.Errorf("verified client certificate required")
	}
	leaf := request.TLS.VerifiedChains[0][0]
	if leaf.Subject.CommonName != ResourceProxyName {
		return leaf.Subject.CommonName, nil
	}
	values := request.Header.Values("X-Agent-Certificate")
	if len(values) != 1 {
		return "", fmt.Errorf("forwarded client certificate required")
	}
	raw, err := url.QueryUnescape(values[0])
	if err != nil {
		return "", fmt.Errorf("decode client certificate: %w", err)
	}
	block, rest := pem.Decode([]byte(raw))
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		return "", fmt.Errorf("invalid forwarded certificate")
	}
	leaf, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse client certificate: %w", err)
	}
	roots, err := clientRoots(caFile)
	if err != nil {
		return "", err
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return "", fmt.Errorf("verify forwarded certificate: %w", err)
	}
	if leaf.Subject.CommonName == ResourceProxyName {
		return "", fmt.Errorf("proxy certificate cannot be an agent")
	}
	return leaf.Subject.CommonName, nil
}
