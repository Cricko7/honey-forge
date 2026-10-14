package tlsconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// Check deployment trust boundaries too: secure clients cannot compensate for
// accidentally restoring plaintext listeners or disabling upstream verification.
func TestComposeTelemetryTLS(t *testing.T) {
	for _, name := range []string{"compose.infra.yaml", "docker-compose.real.yml"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../../../../" + name)
			if err != nil {
				t.Fatal(err)
			}

			var document struct {
				Services map[string]struct {
					Environment map[string]string `yaml:"environment"`
					Entrypoint  []string          `yaml:"entrypoint"`
					Healthcheck struct {
						Test []string `yaml:"test"`
					} `yaml:"healthcheck"`
				} `yaml:"services"`
			}
			if err := yaml.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}

			kafka := document.Services["kafka"]
			if strings.Contains(kafka.Environment["KAFKA_LISTENER_SECURITY_PROTOCOL_MAP"], "PLAINTEXT") || kafka.Environment["KAFKA_SSL_KEYSTORE_LOCATION"] == "" {
				t.Fatal("Kafka listeners must use TLS")
			}

			if !strings.Contains(strings.Join(kafka.Healthcheck.Test, " "), "--command-config") {
				t.Fatal("Kafka healthcheck must use TLS trust")
			}

			if !strings.Contains(strings.Join(document.Services["redis"].Entrypoint, " "), "redis.sh") || !strings.Contains(strings.Join(document.Services["redis"].Healthcheck.Test, " "), "--tls") {
				t.Fatal("Redis listener and healthcheck must use TLS")
			}

			if !strings.Contains(strings.Join(document.Services["postgres"].Entrypoint, " "), "postgres.sh") {
				t.Fatal("PostgreSQL must start with TLS certificates")
			}

			for _, service := range []string{"api", "orchestrator", "agent"} {
				env := document.Services[service].Environment
				if dsn := env["DATABASE_URL"]; dsn != "" && !strings.Contains(dsn, "sslmode=verify-full") {
					t.Fatal("database client must verify TLS hostname")
				}

				if address := env["AGENT_REDIS_URL"]; address != "" && !strings.HasPrefix(address, "rediss://") {
					t.Fatal("agent buffer must use rediss")
				}
			}
		})
	}

	raw, err := os.ReadFile("../../../../../frontend/nginx.conf")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(raw), "proxy_ssl_verify on;") || !strings.Contains(string(raw), "proxy_ssl_trusted_certificate") || !strings.Contains(string(raw), "proxy_ssl_name api;") {
		t.Fatal("nginx must authenticate its API upstream")
	}
}
