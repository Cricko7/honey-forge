package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"time"
)

// The loopback certificate and its private key exist only for this agent
// process. The child receives the public certificate through the bootstrap pipe.
func newLocalTLS() (*tls.Config, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate local TLS key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate local TLS serial: %w", err)
	}

	now := time.Now()
	certificate := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(10, 0, 0),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	raw, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create local TLS certificate: %w", err)
	}

	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{raw}, PrivateKey: key}}}, raw, nil
}

func localClientTLS(address string, certificate []byte) (*tls.Config, error) {
	endpoint, err := url.Parse(address)
	if err != nil || endpoint.Scheme != "wss" || endpoint.Path != "/trap-stream" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return nil, fmt.Errorf("local trap endpoint must use WSS on loopback")
	}

	ip := net.ParseIP(endpoint.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("local trap endpoint must use a loopback IP")
	}

	leaf, err := x509.ParseCertificate(certificate)
	if err != nil {
		return nil, fmt.Errorf("parse local TLS certificate: %w", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(leaf)

	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, nil
}
