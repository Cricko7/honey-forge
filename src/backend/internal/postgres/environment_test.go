package postgres

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentURLLoadsSavedPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	file := filepath.Join(t.TempDir(), "password")
	t.Setenv("POSTGRES_PASSWORD_FILE", file)
	if _, err := EnvironmentURL(); err == nil {
		t.Fatal("missing secret accepted")
	}
	if err := os.WriteFile(file, []byte("special:@ /password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dsn, err := EnvironmentURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	if password != "special:@ /password" || parsed.Query().Get("sslmode") != "verify-full" {
		t.Fatal("saved password or TLS configuration changed")
	}
	t.Setenv("DATABASE_URL", "explicit")
	dsn, err = EnvironmentURL()
	if err != nil || dsn != "explicit" {
		t.Fatal("explicit DATABASE_URL must take precedence")
	}
}
