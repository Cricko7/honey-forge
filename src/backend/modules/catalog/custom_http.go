package catalog

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"honey-forge/internal/contract"
)

// CheckCustomHTTPConfig enforces graph references after declarative validation.
func CheckCustomHTTPConfig(raw json.RawMessage) error {
	var config struct {
		Services []struct {
			Name  string `json:"name"`
			Port  int    `json:"port"`
			Entry string `json:"entry"`
		} `json:"services"`
		Blocks []struct {
			ID      string                     `json:"id"`
			Kind    string                     `json:"kind"`
			Config  map[string]json.RawMessage `json:"config"`
			Outputs map[string]string          `json:"outputs"`
		} `json:"blocks"`
	}
	invalid := contract.NewError("config_invalid")
	if json.Unmarshal(raw, &config) != nil {
		return invalid
	}
	blocks := make(map[string]int, len(config.Blocks))
	for i, block := range config.Blocks {
		if _, exists := blocks[block.ID]; exists {
			return invalid
		}
		blocks[block.ID] = i
		for _, field := range []string{"value", "payload", "headers", "body"} {
			item, exists := block.Config[field]
			if !exists {
				continue
			}
			if field == "value" && (block.Kind == "condition" || block.Kind == "switch") {
				continue
			}
			if field == "body" {
				var format string
				if json.Unmarshal(block.Config["format"], &format) != nil {
					return invalid
				}
				if format != "json" {
					continue
				}
			}
			var value string
			if json.Unmarshal(item, &value) != nil || !json.Valid([]byte(value)) {
				return invalid
			}
		}
	}
	names, ports, entries := map[string]bool{}, map[int]bool{}, map[string]bool{}
	for _, service := range config.Services {
		index, exists := blocks[service.Entry]
		if !exists || names[service.Name] || ports[service.Port] || entries[service.Entry] {
			return invalid
		}
		block := config.Blocks[index]
		var name string
		var port int
		if block.Kind != "input" || json.Unmarshal(block.Config["name"], &name) != nil || json.Unmarshal(block.Config["port"], &port) != nil || name != service.Name || port != service.Port {
			return invalid
		}
		names[name], ports[port], entries[service.Entry] = true, true, true
	}
	for _, block := range config.Blocks {
		if block.Kind == "input" && !entries[block.ID] {
			return invalid
		}
		for _, target := range block.Outputs {
			if _, exists := blocks[target]; !exists {
				return invalid
			}
		}
		if block.Kind == "fork" && block.Outputs["branch1"] == "" && block.Outputs["branch2"] == "" && block.Outputs["branch3"] == "" {
			return invalid
		}
		if block.Kind == "call" {
			var target string
			if json.Unmarshal(block.Config["target"], &target) != nil {
				return invalid
			}
			index, exists := blocks[target]
			if !exists || config.Blocks[index].Kind != "input" {
				return invalid
			}
		}
		if block.Kind == "send" {
			var path string
			if json.Unmarshal(block.Config["path"], &path) != nil {
				return invalid
			}
			parsed, err := url.ParseRequestURI(path)
			if err != nil || parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
				return invalid
			}
		}
	}
	return nil
}

func checkCustomHTTPEvent(raw json.RawMessage) error {
	var data struct {
		Payload json.RawMessage `json:"payload"`
	}
	var compact bytes.Buffer
	if json.Unmarshal(raw, &data) != nil || json.Compact(&compact, data.Payload) != nil || compact.Len() > 4096 {
		return contract.NewError("telemetry_invalid")
	}
	return nil
}
