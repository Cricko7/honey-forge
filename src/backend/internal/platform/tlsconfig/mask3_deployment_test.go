package tlsconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestMASK3DeploymentBoundaries(t *testing.T) {
	for _, name := range []string{"compose.infra.yaml", "docker-compose.real.yml"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../../../../" + name)
			if err != nil {
				t.Fatal(err)
			}

			var compose struct {
				Services map[string]struct {
					Environment map[string]string `yaml:"environment"`
					Ports       []string          `yaml:"ports"`
					Healthcheck struct {
						Test []string `yaml:"test"`
					} `yaml:"healthcheck"`
				} `yaml:"services"`
			}
			if err := yaml.Unmarshal(raw, &compose); err != nil {
				t.Fatal(err)
			}

			redis := compose.Services["redis"]
			if name == "compose.infra.yaml" && !strings.Contains(redis.Environment["REDIS_PASSWORD"], "REDIS_PASSWORD:?") || name == "docker-compose.real.yml" && redis.Environment["REDIS_PASSWORD_FILE"] != "/certs/redis-password" {
				t.Fatal("Redis starts without an authenticated password source")
			}

			if !strings.Contains(strings.Join(redis.Healthcheck.Test, " "), "REDISCLI_AUTH") {
				t.Fatal("Redis healthcheck is unauthenticated")
			}

			if name == "docker-compose.real.yml" {
				web := compose.Services["web"]
				if len(web.Ports) != 1 || web.Ports[0] != "${MGMT_BIND_IP:-127.0.0.1}:8443:443" {
					t.Fatal("management port has an unsafe default binding")
				}

				orch := compose.Services["orchestrator"].Environment
				if orch["MGMT_NETWORK"] == "" || orch["MGMT_NETWORK"] == orch["DECOY_NETWORK"] {
					t.Fatal("management and decoy networks are not separate")
				}

				for _, service := range []string{"agent", "orchestrator"} {
					variable := "AGENT_REDIS_URL_FILE"
					if service == "agent" {
						variable = "NM_STORE_URL_FILE"
					}

					if compose.Services[service].Environment[variable] != "/certs/redis-url" {
						t.Fatalf("%s does not authenticate to Redis", service)
					}
				}

				if len(compose.Services["agent"].Ports) != 0 {
					t.Fatal("manual management agent exposes decoy ports")
				}
			}
		})
	}

	rawBootstrap, err := os.ReadFile("../../../../../deploy/tls/bootstrap.sh")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"openssl rand -hex 32 > redis-password", "publish redis redis.crt redis.key ca.crt redis-password", "publish orchestrator", "redis-url"} {
		if !strings.Contains(string(rawBootstrap), want) {
			t.Fatalf("bootstrap is missing %s", want)
		}
	}

	raw, err := os.ReadFile("../../../../../deploy/tls/redis.sh")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"REDIS_PASSWORD", "requirepass", "--protected-mode yes"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("Redis script is missing %s", want)
		}
	}

	raw, err = os.ReadFile("../../../../../deploy/decoys/entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"set -eu", "iptables -P OUTPUT DROP", "--ctstate ESTABLISHED --ctdir REPLY", "su-exec 65532:65532"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("worker entrypoint is missing %s", want)
		}
	}

	raw, err = os.ReadFile("../../../../../deploy/nginx/honey-forge.conf")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(raw), "listen 443 ssl") || !strings.Contains(string(raw), "listen 127.0.0.1:443 ssl") {
		t.Fatal("production management proxy binds to all interfaces")
	}
}
