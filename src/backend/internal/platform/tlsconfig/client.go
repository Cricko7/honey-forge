// Package tlsconfig shares authenticated TLS settings for outbound connections.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func Client(caFile string) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile == "" {
		return config, nil
	}

	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read TLS CA: %w", err)
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}

	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("TLS CA file has no certificates")
	}

	config.RootCAs = roots

	return config, nil
}

// Secure preserves caller trust roots while preventing verification bypasses
// and old TLS versions. Callers retain ownership of their input configuration.
func Secure(config *tls.Config) (*tls.Config, error) {
	if config == nil {
		config = &tls.Config{}
	}

	if config.InsecureSkipVerify || (config.MaxVersion != 0 && config.MaxVersion < tls.VersionTLS12) {
		return nil, fmt.Errorf("TLS requires certificate verification and TLS 1.2 or newer")
	}

	config = config.Clone()
	config.MinVersion = max(config.MinVersion, tls.VersionTLS12)

	return config, nil
}
