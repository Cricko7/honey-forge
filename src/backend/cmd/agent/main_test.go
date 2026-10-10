package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("private-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := secret("AGENT_TOKEN", path)
	if err != nil || value != "private-token" {
		t.Fatalf("secret()=%q, %v", value, err)
	}
}
