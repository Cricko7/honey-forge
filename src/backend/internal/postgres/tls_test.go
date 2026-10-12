package postgres

import (
	"crypto/tls"
	"testing"
)

func TestParseConfigRequiresVerifyFullWithoutPlaintextFallback(t *testing.T) {
	for _, tt := range []struct {
		name, dsn string
		valid     bool
	}{
		{"verify full", "postgres://operator:private@db.example/database?sslmode=verify-full", true},
		{"default fallback", "postgres://operator:private@db.example/database", false},
		{"disabled", "postgres://operator:private@db.example/database?sslmode=disable", false},
		{"require without verification", "postgres://operator:private@db.example/database?sslmode=require", false},
		{"verify CA without hostname", "postgres://operator:private@db.example/database?sslmode=verify-ca", false},
		{"keyword DSN", "host=db.example user=operator password=private dbname=database sslmode=verify-full", true},
		{"unix socket without TLS", "host=/tmp user=operator dbname=database sslmode=verify-full", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseConfig(tt.dsn)
			if (err == nil) != tt.valid {
				t.Fatalf("parseConfig() error=%v, want valid=%v", err, tt.valid)
			}

			if tt.valid && (config.ConnConfig.TLSConfig.InsecureSkipVerify || config.ConnConfig.TLSConfig.MinVersion < tls.VersionTLS12 || len(config.ConnConfig.Fallbacks) != 0) {
				t.Fatal("database TLS validation or fallback policy is unsafe")
			}
		})
	}
}
