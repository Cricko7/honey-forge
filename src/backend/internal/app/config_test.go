package app

import "testing"

func TestConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct{ name, key, value string }{
		{"missing database", "DATABASE_URL", ""},
		{"missing cursor key", "CURSOR_KEY", ""},
		{"malformed cursor key", "CURSOR_KEY", "invalid"},
		{"short cursor key", "CURSOR_KEY", "AA=="},
		{"missing origin", "ALLOWED_ORIGIN", ""},
		{"insecure origin", "ALLOWED_ORIGIN", "http://example.com"},
		{"origin path", "ALLOWED_ORIGIN", "https://example.com/"},
		{"origin credentials", "ALLOWED_ORIGIN", "https://user:pass@example.com"},
		{"origin query", "ALLOWED_ORIGIN", "https://example.com?x=1"},
		{"invalid address", "HTTP_ADDR", "localhost"},
		{"invalid port", "HTTP_ADDR", "localhost:70000"},
		{"invalid log level", "LOG_LEVEL", "loud"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CURSOR_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
			t.Setenv("DATABASE_URL", "postgres://localhost/auth")
			t.Setenv("ALLOWED_ORIGIN", "https://example.com")
			t.Setenv("HTTP_ADDR", "")
			t.Setenv("LOG_LEVEL", "")
			t.Setenv(tt.key, tt.value)

			if _, err := LoadConfig(); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	t.Setenv("CURSOR_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("DATABASE_URL", "postgres://localhost/auth")
	t.Setenv("ALLOWED_ORIGIN", "https://localhost:8443")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("LOG_LEVEL", "")

	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != ":8080" || config.AllowedOrigin != "https://localhost:8443" {
		t.Fatalf("unexpected config: %+v", config)
	}
}
