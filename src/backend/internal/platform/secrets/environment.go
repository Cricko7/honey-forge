package secrets

import (
	"fmt"
	"os"
	"strings"
)

// Environment reads NAME_FILE when configured; a broken file fails closed.
func Environment(name string) (string, error) {
	file := os.Getenv(name + "_FILE")
	if file == "" {
		return os.Getenv(name), nil
	}

	raw, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", name, err)
	}

	value := strings.TrimSuffix(string(raw), "\n")
	if value == "" {
		return "", fmt.Errorf("%s file is empty", name)
	}

	return value, nil
}
