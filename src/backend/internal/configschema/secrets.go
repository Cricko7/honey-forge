package configschema

import (
	"bytes"
	"encoding/json"
	"honey-forge/internal/contract"
	"sort"
	"strconv"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func (s *Schema) PublicConfig(full json.RawMessage) (json.RawMessage, []string, error) {
	value, err := object(full)
	if err != nil {
		return nil, nil, err
	}
	paths := s.secretPaths(value)
	fields := []string{}
	for _, path := range paths {
		if _, exists := at(value, path); exists {
			fields = append(fields, path)
			remove(value, path)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, contract.NewError("internal_error")
	}
	return raw, fields, nil
}

// MergeConfig treats incoming config as a replacement, retaining only omitted
// installed writeOnly values, then validates the full effective configuration.
func (s *Schema) MergeConfig(previous, incoming json.RawMessage, clear []string) (json.RawMessage, error) {
	old, err := object(previous)
	if err != nil {
		return nil, err
	}
	next, err := object(incoming)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, path := range s.secretPaths(old) {
		allowed[path] = true
	}
	for _, path := range s.secretPaths(next) {
		allowed[path] = true
	}
	clears := map[string]bool{}
	for _, path := range clear {
		if !allowed[path] {
			return nil, contract.NewError("validation_failed")
		}
		if _, provided := at(next, path); provided {
			e := contract.NewError("validation_failed")
			e.Fields = []contract.FieldError{{Path: "/clear_secret_fields", Code: "conflicting_value", Message: "A secret cannot be assigned and cleared in the same request"}}
			return nil, e
		}
		clears[path] = true
	}
	for path := range allowed {
		if clearedPath(path, clears) {
			continue
		}
		if _, exists := at(next, path); exists {
			continue
		}
		if v, exists := at(old, path); exists {
			if !put(next, path, v) {
				return nil, contract.NewError("validation_failed")
			}
		}
	}
	for path := range clears {
		remove(next, path)
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return nil, contract.NewError("internal_error")
	}
	if err := contract.CheckConfig(raw); err != nil {
		return nil, err
	}
	if err := s.Validate(raw, "/config"); err != nil {
		return nil, err
	}
	return raw, nil
}

func object(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := contract.CheckJSON(raw); err != nil {
		return nil, contract.NewError("invalid_json")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value map[string]any
	if err := d.Decode(&value); err != nil || value == nil {
		return nil, contract.NewError("validation_failed")
	}
	return value, nil
}
func (s *Schema) secretPaths(value map[string]any) []string {
	set := map[string]bool{}
	walkSecrets(s.compiled, value, "", set, map[*jsonschema.Schema]bool{})
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
func walkSecrets(s *jsonschema.Schema, value any, path string, set map[string]bool, seen map[*jsonschema.Schema]bool) {
	if s == nil || seen[s] {
		return
	}
	seen[s] = true
	defer delete(seen, s)
	if s.WriteOnly {
		set[path] = true
		return
	}
	walkSecrets(s.Ref, value, path, set, seen)
	walkSecrets(s.RecursiveRef, value, path, set, seen)
	if s.DynamicRef != nil {
		walkSecrets(s.DynamicRef.Ref, value, path, set, seen)
	}
	for _, group := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf} {
		for _, child := range group {
			walkSecrets(child, value, path, set, seen)
		}
	}
	for _, child := range []*jsonschema.Schema{s.Then, s.Else} {
		walkSecrets(child, value, path, set, seen)
	}
	for _, child := range s.DependentSchemas {
		walkSecrets(child, value, path, set, seen)
	}
	if m, ok := value.(map[string]any); ok {
		for key, child := range s.Properties {
			descendSecret(child, m[key], path+"/"+escape(key), set, seen)
		}
		for key, v := range m {
			matched := s.Properties[key] != nil
			for pattern, child := range s.PatternProperties {
				if pattern.MatchString(key) {
					matched = true
					descendSecret(child, v, path+"/"+escape(key), set, seen)
				}
			}
			if !matched {
				if child, ok := s.AdditionalProperties.(*jsonschema.Schema); ok {
					descendSecret(child, v, path+"/"+escape(key), set, seen)
				}
			}
		}
	} else if value == nil {
		for key, child := range s.Properties {
			walkSecrets(child, nil, path+"/"+escape(key), set, seen)
		}
	}
	if array, ok := value.([]any); ok {
		for i, v := range array {
			child := s.Items2020
			if i < len(s.PrefixItems) {
				child = s.PrefixItems[i]
			}
			walkSecrets(child, v, path+"/"+strconv.Itoa(i), set, map[*jsonschema.Schema]bool{})
		}
	}
}
