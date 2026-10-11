package tlsconfig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// IssueClient creates a new private key for each credential generation. The CA
// key stays with the orchestrator and is never mounted into a decoy container.
func IssueClient(caFile, keyFile, identity string) ([]byte, []byte, time.Time, error) {
	if identity == "" {
		return nil, nil, time.Time{}, fmt.Errorf("client identity required")
	}
	pair, err := tls.LoadX509KeyPair(caFile, keyFile)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("load issuing CA: %w", err)
	}
	ca, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("parse issuing CA: %w", err)
	}
	now := time.Now()
	if !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(ca.NotBefore) || !ca.NotAfter.After(now.Add(24*time.Hour)) {
		return nil, nil, time.Time{}, fmt.Errorf("issuing CA must be valid for more than 24 hours")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("generate client key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("generate certificate serial: %w", err)
	}
	expires := now.Add(30 * 24 * time.Hour)
	if expires.After(ca.NotAfter) {
		expires = ca.NotAfter
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: identity}, NotBefore: now.Add(-5 * time.Minute), NotAfter: expires, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
	raw, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, pair.PrivateKey)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("sign client certificate: %w", err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("encode client key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), expires, nil
}
