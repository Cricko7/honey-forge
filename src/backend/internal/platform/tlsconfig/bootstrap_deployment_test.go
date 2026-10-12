package tlsconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestLocalStackBootstrapsWithoutRequiredEnvironment(t *testing.T) {
	raw, err := os.ReadFile("../../../../../docker-compose.real.yml")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(raw), ":?") {
		t.Fatal("local startup requires manual environment configuration")
	}

	var document struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
			Volumes     []string          `yaml:"volumes"`
			DependsOn   map[string]struct {
				Condition string `yaml:"condition"`
			} `yaml:"depends_on"`
		}
	}
	// web/agent use the short dependency syntax; inspect relevant services only.
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}

	services := root["services"].(map[string]any)
	delete(services, "web")
	delete(services, "agent")
	filtered, err := yaml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := yaml.Unmarshal(filtered, &document); err != nil {
		t.Fatal(err)
	}

	for _, service := range []string{"postgres", "redis", "kafka"} {
		if document.Services[service].DependsOn["tls-init"].Condition != "service_healthy" {
			t.Fatalf("%s must wait for generated certificates", service)
		}
	}

	for _, service := range []string{"api", "orchestrator", "postgres"} {
		if document.Services[service].Environment["POSTGRES_PASSWORD_FILE"] != "/certs/postgres-password" {
			t.Fatalf("%s must read its saved password file", service)
		}
	}

	if document.Services["api"].Environment["CURSOR_KEY_FILE"] != "/certs/cursor-key" {
		t.Fatal("API must load the saved cursor key")
	}

	for name, service := range document.Services {
		for _, mount := range service.Volumes {
			if strings.HasPrefix(mount, "tls_private:") && name != "tls-init" {
				t.Fatalf("private signing volume exposed to %s", name)
			}
		}
	}
}
