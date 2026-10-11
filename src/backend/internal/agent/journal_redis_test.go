package agent

import (
	"crypto/tls"
	"testing"
)

func TestRedisOptionsRequireAuthenticatedTLS(t *testing.T) {
	for _, tt := range []struct {
		name, address string
		config        *tls.Config
		valid         bool
	}{
		{"TLS", "rediss://redis.example:6379/0", nil, true},
		{"plaintext remote", "redis://redis.example:6379/0", nil, false},
		{"plaintext loopback", "redis://127.0.0.1:6379/0", nil, false},
		{"skip verification query", "rediss://redis.example:6379/0?skip_verify=true", nil, false},
		{"skip verification config", "rediss://redis.example:6379/0", &tls.Config{InsecureSkipVerify: true}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			options, err := redisOptions(tt.address, tt.config)
			if (err == nil) != tt.valid {
				t.Fatalf("redisOptions()=%+v %v", options, err)
			}
			if tt.valid && (options.TLSConfig == nil || options.TLSConfig.InsecureSkipVerify || options.TLSConfig.MinVersion < tls.VersionTLS12 || options.TLSConfig.ServerName != "redis.example") {
				t.Fatal("Redis must verify the configured endpoint over TLS 1.2+")
			}
		})
	}
}
