package tlsconfig

import (
	"os"
	"strings"
	"testing"
)

func TestDeploymentUsesPatchedGo(t *testing.T) {
	for _, name := range []string{"Dockerfile.api", "Dockerfile.agent", "deploy/decoys/Dockerfile", "deploy/decoys/Dockerfile.orchestrator"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../../../../" + name)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(string(raw), "FROM golang:1.27.2-alpine AS build") {
				t.Fatal("build must include the Go HTTP/2 and TLS security fixes")
			}
		})
	}
}
