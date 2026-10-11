package tlsconfig

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func TestClientRejectsMissingAndInvalidCA(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(invalid, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, path string }{
		{"missing", filepath.Join(t.TempDir(), "missing.pem")},
		{"invalid", invalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Client(tt.path); err == nil {
				t.Fatal("invalid CA configuration accepted")
			}
		})
	}
	config, err := Client("")
	if err != nil || config.MinVersion != tls.VersionTLS12 || config.InsecureSkipVerify {
		t.Fatalf("unsafe system trust configuration: %+v %v", config, err)
	}
}

func TestSecureRejectsInsecureSettingsAndClonesConfig(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config *tls.Config
		valid  bool
	}{
		{"default", nil, true},
		{"verification disabled", &tls.Config{InsecureSkipVerify: true}, false},
		{"old TLS only", &tls.Config{MaxVersion: tls.VersionTLS11}, false},
		{"raise minimum", &tls.Config{MinVersion: tls.VersionTLS10}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config, err := Secure(tt.config)
			if (err == nil) != tt.valid {
				t.Fatalf("Secure()=%+v %v", config, err)
			}
			if tt.valid && (config.MinVersion < tls.VersionTLS12 || config.InsecureSkipVerify || config == tt.config) {
				t.Fatal("configuration must be secure and independently owned")
			}
		})
	}
}
