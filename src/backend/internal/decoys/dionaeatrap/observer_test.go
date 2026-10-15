package dionaeatrap

import (
	"os"
	"os/exec"
	"testing"
)

func TestDionaeaObservationAdapter(t *testing.T) {
	python := os.Getenv("HF_PYTHON")
	if python == "" {
		python = "python3"
	}
	cmd := exec.CommandContext(t.Context(), python, "-I", "-B", "scripts/observer_test.py")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Dionaea adapter: %v\n%s", err, output)
	}
}
