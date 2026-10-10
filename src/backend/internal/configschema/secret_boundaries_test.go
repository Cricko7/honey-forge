package configschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSecretAssignmentAndClearConflict(t *testing.T) {
	s, err := Compile(json.RawMessage(testSchema))
	if err != nil {
		t.Fatal(err)
	}
	full, err := s.MergeConfig(json.RawMessage(`{"port":1,"password":"old"}`), json.RawMessage(`{"port":2,"password":"new"}`), []string{"/password"})
	if err == nil || err.Error() != "validation_failed" || len(full) != 0 {
		t.Fatalf("%s %v", full, err)
	}
}

func TestRecursiveSecretRedaction(t *testing.T) {
	s, err := Compile(json.RawMessage(`{"type":"object","properties":{"secret":{"type":"string","writeOnly":true},"next":{"$ref":"#"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	public, fields, err := s.PublicConfig(json.RawMessage(`{"secret":"first-secret","next":{"secret":"second-secret"}}`))
	if err != nil || strings.Contains(string(public), "secret") || len(fields) != 2 {
		t.Fatalf("%s %v %v", public, fields, err)
	}
}

func TestEffectiveConfigLimit(t *testing.T) {
	s, err := Compile(json.RawMessage(`{"type":"object","properties":{"secret":{"type":"string","writeOnly":true},"visible":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(map[string]string{"secret": strings.Repeat("s", 80*1024)})
	next, _ := json.Marshal(map[string]string{"visible": strings.Repeat("v", 80*1024)})
	if _, err := s.MergeConfig(old, next, nil); err == nil || err.Error() != "config_too_large" {
		t.Fatalf("effective config limit: %v", err)
	}
}
