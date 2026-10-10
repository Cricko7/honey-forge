// Package configschema compiles immutable catalogue schemas and validates dynamic JSON.
package configschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"honey-forge/internal/contract"
)

type Schema struct{ compiled *jsonschema.Schema }
type offlineLoader struct{}

func (offlineLoader) Load(string) (any, error) {
	return nil, fmt.Errorf("external schema loading is forbidden")
}

func Compile(raw json.RawMessage) (*Schema, error) {
	if err := contract.CheckJSON(raw); err != nil {
		return nil, contract.NewError("validation_failed")
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, contract.NewError("validation_failed")
	}
	if err := checkLocal(doc); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(offlineLoader{})
	c.AssertFormat()
	const location = "https://honey-forge.invalid/schema"
	if err := c.AddResource(location, doc); err != nil {
		return nil, fmt.Errorf("add schema: %w", err)
	}
	compiled, err := c.Compile(location)
	if err != nil {
		return nil, contract.NewError("validation_failed")
	}
	return &Schema{compiled}, nil
}

// Inspect only schema keywords, not arbitrary const/default/example data.
func checkLocal(v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"$ref", "$dynamicRef", "$recursiveRef"} {
		if ref, ok := m[key].(string); ok && !strings.HasPrefix(ref, "#") {
			return contract.NewError("validation_failed")
		}
	}
	if draft, ok := m["$schema"].(string); ok && strings.TrimSuffix(draft, "#") != "https://json-schema.org/draft/2020-12/schema" {
		return contract.NewError("validation_failed")
	}
	for _, key := range []string{"$defs", "properties", "patternProperties", "dependentSchemas"} {
		if children, ok := m[key].(map[string]any); ok {
			for _, child := range children {
				if err := checkLocal(child); err != nil {
					return err
				}
			}
		}
	}
	for _, key := range []string{"additionalProperties", "unevaluatedProperties", "propertyNames", "items", "unevaluatedItems", "contains", "not", "if", "then", "else", "contentSchema"} {
		if err := checkLocal(m[key]); err != nil {
			return err
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		if children, ok := m[key].([]any); ok {
			for _, child := range children {
				if err := checkLocal(child); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Schema) Validate(raw json.RawMessage, path string) error {
	if err := contract.CheckJSON(raw); err != nil {
		return contract.NewError("invalid_json")
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return contract.NewError("invalid_json")
	}
	if _, ok := v.(map[string]any); !ok {
		return contract.NewError("validation_failed")
	}
	if err := s.compiled.Validate(v); err != nil {
		result := contract.NewError("validation_failed")
		var validation *jsonschema.ValidationError
		if errors.As(err, &validation) {
			collectFields(validation, path, &result.Fields)
		}
		return result
	}
	return nil
}

func collectFields(e *jsonschema.ValidationError, prefix string, fields *[]contract.FieldError) {
	if len(*fields) >= contract.MaxFieldErrors {
		return
	}
	if len(e.Causes) > 0 {
		for _, child := range e.Causes {
			collectFields(child, prefix, fields)
		}
		return
	}
	path := prefix
	for _, part := range e.InstanceLocation {
		path += "/" + strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
	}
	*fields = append(*fields, contract.FieldError{Path: path, Code: "invalid_value", Message: "Value does not satisfy the schema constraints"})
}
