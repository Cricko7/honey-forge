package app

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestAPIKeyOpenAPIContract(t *testing.T) {
	raw, err := os.ReadFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	type operation struct {
		Security  []map[string][]string `yaml:"security"`
		Responses map[string]any        `yaml:"responses"`
	}
	type pathItem struct {
		Get    *operation `yaml:"get"`
		Post   *operation `yaml:"post"`
		Patch  *operation `yaml:"patch"`
		Delete *operation `yaml:"delete"`
	}
	var document struct {
		Paths map[string]pathItem `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/traps", "/api/traps/{id}/commands", "/api/events", "/api/stream", "/api/organization"} {
		operations, ok := document.Paths[path]
		if !ok {
			t.Fatalf("missing path: %s", path)
		}

		for method, operation := range map[string]*operation{"GET": operations.Get, "POST": operations.Post, "PATCH": operations.Patch, "DELETE": operations.Delete} {
			if operation == nil {
				continue
			}

			bearer := false
			for _, scheme := range operation.Security {
				if _, ok := scheme["apiKeyBearer"]; ok {
					bearer = true
				}
			}

			if !bearer || operation.Responses["429"] == nil {
				t.Fatalf("missing Bearer/quota contract: %s %s", method, path)
			}
		}
	}

	for _, path := range []string{"/api/api-keys", "/api/api-keys/{id}", "/api/session", "/api/organization/join-code"} {
		item := document.Paths[path]
		for method, operation := range map[string]*operation{"GET": item.Get, "POST": item.Post, "PATCH": item.Patch, "DELETE": item.Delete} {
			if operation == nil {
				continue
			}

			for _, scheme := range operation.Security {
				if _, ok := scheme["apiKeyBearer"]; ok {
					t.Fatalf("credential management grants key access: %s %s", method, path)
				}
			}
		}
	}

	if !strings.Contains(string(raw), "API_KEY_RATE_LIMIT_PER_MINUTE") {
		t.Fatal("missing rate limit configuration")
	}
}
