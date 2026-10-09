package app

import (
	"os"
	"testing"
)

func TestRuntime(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	runtime, err := Open(t.Context(), Config{DatabaseURL: dsn, CursorKey: make([]byte, 32), BrowserOrigins: []string{"https://operator.example"}, MigrationsPath: "../../migrations"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if runtime.Router == nil || runtime.Mutations == nil || runtime.Schemas == nil || runtime.Cursors == nil || runtime.Browser == nil || runtime.Catalog == nil {
		t.Fatal("missing shared dependency")
	}
}
