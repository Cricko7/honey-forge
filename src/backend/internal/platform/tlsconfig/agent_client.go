package tlsconfig

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func SPKIPin(cert *x509.Certificate) string {
	digest := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "sha256/" + base64.StdEncoding.EncodeToString(digest[:])
}

func readPins(file string) (map[string]bool, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read server pins: %w", err)
	}
	pins := map[string]bool{}
	for _, pin := range strings.Fields(string(raw)) {
		digest, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(pin, "sha256/"))
		if !strings.HasPrefix(pin, "sha256/") || err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("server pin must be sha256/base64 SPKI digest")
		}
		pins[pin] = true
	}
	if len(pins) == 0 {
		return nil, fmt.Errorf("at least one server pin is required")
	}
	return pins, nil
}

// AgentClient requires both normal PKI verification and an explicit SPKI pin.
// Files are reread on each handshake; a broken rotation fails closed.
func AgentClient(caFile, certFile, keyFile, pinsFile string) (*tls.Config, error) {
	if caFile == "" || certFile == "" || keyFile == "" || pinsFile == "" {
		return nil, fmt.Errorf("agent CA, certificate, key and server pins files are required")
	}
	roots, err := clientRoots(caFile)
	if err != nil {
		return nil, err
	}
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return nil, fmt.Errorf("load agent certificate: %w", err)
	}
	if _, err := readPins(pinsFile); err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return nil, fmt.Errorf("reload agent certificate: %w", err)
			}
			return &cert, nil
		},
		VerifyConnection: func(state tls.ConnectionState) error {
			pins, err := readPins(pinsFile)
			if err != nil {
				return err
			}
			if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 || !pins[SPKIPin(state.PeerCertificates[0])] {
				return fmt.Errorf("server public key pin mismatch")
			}
			return nil
		},
	}, nil
}

func clientRoots(file string) (*x509.CertPool, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read client CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, fmt.Errorf("client CA file has no certificates")
	}
	return roots, nil
}
