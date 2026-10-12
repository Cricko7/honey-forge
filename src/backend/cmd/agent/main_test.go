package main

import (
	"os"
	"path/filepath"
	"testing"

	"honey-forge/internal/platform/secrets"
)

func TestSecretFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("private-token\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NM_TOKEN_FILE", path)
	value, err := secrets.Environment("NM_TOKEN")
	if err != nil || value != "private-token" {
		t.Fatalf("secret()=%q, %v", value, err)
	}
}
