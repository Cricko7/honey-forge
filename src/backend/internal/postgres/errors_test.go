package postgres

import (
	"strings"
	"testing"
)

func TestConnectionErrorsDoNotExposeSecrets(t *testing.T) {
	_, err := Open(t.Context(), "postgres://operator:server-secret@localhost:invalid/database")
	if err == nil || strings.Contains(err.Error(), "server-secret") {
		t.Fatalf("unsafe database error %v", err)
	}
}
