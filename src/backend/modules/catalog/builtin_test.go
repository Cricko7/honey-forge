package catalog

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestBuiltinSchemasMatchSpecification(t *testing.T) {
	document, err := os.ReadFile("testdata/tcp-banner-spec.json")
	if err != nil {
		t.Fatal(err)
	}

	var blocks []json.RawMessage
	if err := json.Unmarshal(document, &blocks); err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 5 {
		t.Fatalf("expected 5 source schema blocks, got %d", len(blocks))
	}

	entry := BuiltinDefinitions()[0].Entry
	actual := []json.RawMessage{entry.ConfigSchema}
	for _, event := range entry.EventSchemas {
		actual = append(actual, event.DataSchema)
	}

	for i, raw := range actual {
		var want, got any
		if err := json.Unmarshal(blocks[i], &want); err != nil {
			t.Fatal(err)
		}

		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("schema %d differs from specification", i)
		}
	}

	var source struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(blocks[4], &source); err != nil {
		t.Fatal(err)
	}

	for _, action := range entry.Actions {
		param := "EmptyObject"
		if action.Action == "apply_config" {
			param = "ApplyConfigParams"
		}

		for _, tt := range []struct {
			name string
			raw  json.RawMessage
		}{{param, action.ParamsSchema}, {"RuntimeResult", action.ResultSchema}} {
			var schema struct {
				Draft string                     `json:"$schema"`
				Ref   string                     `json:"$ref"`
				Defs  map[string]json.RawMessage `json:"$defs"`
			}
			if err := json.Unmarshal(tt.raw, &schema); err != nil {
				t.Fatal(err)
			}

			if schema.Draft != "https://json-schema.org/draft/2020-12/schema" || schema.Ref != "#/$defs/"+tt.name {
				t.Fatalf("action %s does not publish a self-contained schema", action.Action)
			}

			var want, got any
			if err := json.Unmarshal(source.Defs[tt.name], &want); err != nil {
				t.Fatal(err)
			}

			if err := json.Unmarshal(schema.Defs[tt.name], &got); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("action %s schema %s differs from specification", action.Action, tt.name)
			}
		}
	}
}
