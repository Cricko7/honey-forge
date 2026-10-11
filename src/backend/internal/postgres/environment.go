package postgres

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// EnvironmentURL preserves explicit deployment DSNs and supports the local
// Compose bootstrap without putting the generated password in container env.
func EnvironmentURL() (string, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn, nil
	}
	file := os.Getenv("POSTGRES_PASSWORD_FILE")
	if file == "" {
		return "", fmt.Errorf("DATABASE_URL or POSTGRES_PASSWORD_FILE is required")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read database password: %w", err)
	}
	password := strings.TrimSuffix(string(raw), "\n")
	if password == "" {
		return "", fmt.Errorf("database password file is empty")
	}
	dsn := url.URL{Scheme: "postgres", Host: "postgres:5432", Path: "/honey_forge", User: url.UserPassword("honey_forge", password)}
	query := url.Values{"sslmode": {"verify-full"}, "sslrootcert": {"/certs/ca.crt"}}
	dsn.RawQuery = query.Encode()
	return dsn.String(), nil
}
