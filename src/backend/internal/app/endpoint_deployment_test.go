package app

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestEndpointDeploymentContract(t *testing.T) {
	for _, name := range []string{"docker-compose.real.yml", ".github/workflows/ci-cd.yml", "api/openapi.yaml"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../../../" + name)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := yaml.Unmarshal(raw, &document); err != nil {
				t.Fatalf("invalid deployment/contract YAML: %v", err)
			}
			if name == "docker-compose.real.yml" {
				var compose struct {
					Services map[string]struct {
						Environment map[string]string `yaml:"environment"`
					} `yaml:"services"`
				}
				if err := yaml.Unmarshal(raw, &compose); err != nil {
					t.Fatal(err)
				}
				api := compose.Services["api"].Environment
				if _, err := configureAgentEndpoint(api["AGENT_WS_URL"], api["API_PUBLIC_ORIGIN"], []string{api["BROWSER_ORIGINS"]}); err != nil {
					t.Fatal(err)
				}
				for _, service := range []string{"agent", "orchestrator"} {
					if !strings.HasPrefix(compose.Services[service].Environment["AGENT_WS_URL"], "wss://resources.localhost/") {
						t.Fatalf("%s bypasses the resource proxy", service)
					}
				}
				if compose.Services["orchestrator"].Environment["AGENT_PROXY_SERVICE"] != "web" {
					t.Fatal("resource proxy must be reachable from the management network")
				}
			}
		})
	}
}
