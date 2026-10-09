package configschema

import (
	"encoding/json"
	"strings"
	"testing"
)

const testSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["port"],"properties":{"port":{"type":"integer","minimum":1,"maximum":65535},"password":{"$ref":"#/$defs/secret"},"bait":{"type":"string"}},"$defs":{"secret":{"type":"string","writeOnly":true}}}`

func TestSchema(t *testing.T) {
	s, err := Compile(json.RawMessage(testSchema))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(json.RawMessage(`{"port":80}`), "/config"); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{`{"port":0}`, `{"port":1,"extra":1}`, `[]`} {
		if err := s.Validate(json.RawMessage(b), "/config"); err == nil {
			t.Fatalf("accepted %s", b)
		}
	}
	for _, b := range []string{`{"$ref":"https://evil.example/schema"}`, `{"$schema":"http://json-schema.org/draft-07/schema#"}`, `{"properties":{"x":{"$dynamicRef":"file:///secret"}}}`} {
		if _, err := Compile(json.RawMessage(b)); err == nil {
			t.Fatalf("accepted schema %s", b)
		}
	}
}

func TestSecrets(t *testing.T) {
	s, err := Compile(json.RawMessage(testSchema))
	if err != nil {
		t.Fatal(err)
	}
	full, err := s.MergeConfig(json.RawMessage(`{"port":1,"password":"server-secret","bait":"visible"}`), json.RawMessage(`{"port":2,"bait":"bait-password"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(full), "server-secret") {
		t.Fatal("secret lost")
	}
	public, fields, err := s.PublicConfig(full)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "server-secret") || !strings.Contains(string(public), "bait-password") || len(fields) != 1 || fields[0] != "/password" {
		t.Fatalf("%s %v", public, fields)
	}
	cleared, err := s.MergeConfig(full, json.RawMessage(`{"port":3}`), []string{"/password"})
	if err != nil || strings.Contains(string(cleared), "server-secret") {
		t.Fatalf("%s %v", cleared, err)
	}
	if _, err := s.MergeConfig(full, json.RawMessage(`{"port":3}`), []string{"/bait"}); err == nil {
		t.Fatal("nonsecret clear accepted")
	}
}

func TestSafeSchemaErrors(t *testing.T) {
	s, err := Compile(json.RawMessage(testSchema))
	if err != nil {
		t.Fatal(err)
	}
	err = s.Validate(json.RawMessage(`{"port":"captured-password"}`), "/config")
	if err == nil || strings.Contains(err.Error(), "captured-password") {
		t.Fatal("unsafe validation error")
	}
}
