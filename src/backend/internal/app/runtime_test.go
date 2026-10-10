//go:build integration

package app

import "testing"

func TestRuntime(t *testing.T) {
	runtime := openIntegrationRuntime(t)

	if runtime.Router == nil || runtime.Mutations == nil || runtime.Schemas == nil || runtime.Cursors == nil || runtime.Browser == nil || runtime.Catalog == nil {
		t.Fatal("missing shared dependency")
	}
}
