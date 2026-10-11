package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironment(t *testing.T) {
	for _, name := range []string{"direct", "file", "missing file", "empty file"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_SECRET", "direct-value")
			t.Setenv("TEST_SECRET_FILE", "")
			want := "direct-value"
			wantError := false
			if name != "direct" {
				file := filepath.Join(t.TempDir(), "secret")
				t.Setenv("TEST_SECRET_FILE", file)
				want = "private-value"
				wantError = name == "missing file" || name == "empty file"
				if name != "missing file" {
					value := "private-value\n"
					if name == "empty file" {
						value = "\n"
					}
					if err := os.WriteFile(file, []byte(value), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			value, err := Environment("TEST_SECRET")
			if (err != nil) != wantError || !wantError && value != want {
				t.Fatalf("value=%q err=%v", value, err)
			}
		})
	}
}
