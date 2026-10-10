package app

import (
	"encoding/json"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
)

func TestCommandCatalogAdapter(t *testing.T) {
	cursors, err := contract.NewCursorCodec([]byte("integration-cursor-key-32-bytes!"))
	if err != nil {
		t.Fatal(err)
	}

	service, err := catalog.NewService(catalog.BuiltinDefinitions(), cursors)
	if err != nil {
		t.Fatal(err)
	}

	check := CommandActionCheck(service)
	if err := check(t.Context(), "tcp-banner", 1, "stop", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := check(t.Context(), "tcp-banner", 1, "stop", json.RawMessage(`{"shell":"x"}`)); err == nil {
		t.Fatal("catalog accepted unexpected parameter")
	}
	if err := check(t.Context(), "tcp-banner", 1, "restore", json.RawMessage(`{}`)); err == nil {
		t.Fatal("catalog accepted unsupported action")
	}

	result := CommandResultCheck(service)
	if err := result(t.Context(), "tcp-banner", 1, "stop", json.RawMessage(`{"runtime_state":"stopped","applied_profile_revision":null}`), false); err != nil {
		t.Fatal(err)
	}
	if err := result(t.Context(), "tcp-banner", 1, "stop", json.RawMessage(`{"runtime_state":"stopped","applied_profile_revision":null}`), true); err == nil {
		t.Fatal("catalog accepted missing applied revision")
	}
}
