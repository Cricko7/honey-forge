package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
)

func TestOpenAPIContainsEveryRouteAndResponseReference(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var contract struct {
		OpenAPI string `yaml:"openapi"`
		Paths   map[string]map[string]struct {
			Responses map[string]struct {
				Ref string `yaml:"$ref"`
			} `yaml:"responses"`
		} `yaml:"paths"`
		Components struct {
			Responses map[string]any `yaml:"responses"`
		} `yaml:"components"`
	}

	if err := yaml.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("invalid OpenAPI YAML: %v", err)
	}

	if contract.OpenAPI != "3.1.0" {
		t.Fatalf("unexpected OpenAPI version %q", contract.OpenAPI)
	}

	router := testRouter(t, io.Discard).(*gin.Engine)
	routes := make(map[string]bool)

	for _, route := range router.Routes() {
		segments := strings.Split(route.Path, "/")
		for i, part := range segments {
			if strings.HasPrefix(part, ":") {
				segments[i] = "{" + part[1:] + "}"
			}
		}
		contractPath := strings.Join(segments, "/")
		routes[route.Method+" "+contractPath] = true
		if _, ok := contract.Paths[contractPath][strings.ToLower(route.Method)]; !ok {
			t.Fatalf("route %s %s is missing in OpenAPI", route.Method, route.Path)
		}
	}

	for path, methods := range contract.Paths {
		for method, operation := range methods {
			if !routes[strings.ToUpper(method)+" "+path] {
				t.Fatalf("documented operation has no route: %s %s", method, path)
			}

			for status, response := range operation.Responses {
				name := strings.TrimPrefix(response.Ref, "#/components/responses/")
				if response.Ref == "" || contract.Components.Responses[name] == nil {
					t.Fatalf("invalid response reference for %s %s, status %s", method, path, status)
				}
			}
		}
	}
}
