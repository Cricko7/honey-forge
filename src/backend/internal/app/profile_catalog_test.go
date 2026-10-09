package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
	"github.com/Cricko7/honey-forge/src/backend/modules/profiles"
)

const tcpConfig = `{"listeners":[{"name":"ssh","port":2222,"banner":"SSH-2.0-demo\r\n","close_after_banner":true}],"logging":{"capture_payload":false,"max_payload_bytes":0},"management":{"heartbeat_interval_seconds":10,"telemetry_flush_interval_ms":500}}`

func TestProfileCatalogTCPConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(profiles.Object)
		valid  bool
	}{
		{"valid", func(profiles.Object) {}, true},
		{"unknown", func(c profiles.Object) { c["origin"] = "http://attacker" }, false},
		{"missing", func(c profiles.Object) { delete(c, "management") }, false},
		{"empty listeners", func(c profiles.Object) { c["listeners"] = []any{} }, false},
		{"missing bool", func(c profiles.Object) { delete(c["listeners"].([]any)[0].(map[string]any), "close_after_banner") }, false},
		{"duplicate names and ports", func(c profiles.Object) { l := c["listeners"].([]any); c["listeners"] = append(l, l[0]) }, false},
		{"banner bytes", func(c profiles.Object) {
			c["listeners"].([]any)[0].(map[string]any)["banner"] = strings.Repeat("я", 4096)
		}, false},
		{"payload inconsistent", func(c profiles.Object) { c["logging"].(map[string]any)["max_payload_bytes"] = 1 }, false},
		{"heartbeat out of range", func(c profiles.Object) { c["management"].(map[string]any)["heartbeat_interval_seconds"] = 11 }, false},
		{"null logging", func(c profiles.Object) { c["logging"] = nil }, false},
		{"exact fractional port", func(c profiles.Object) {
			c["listeners"].([]any)[0].(map[string]any)["port"] = json.Number("2222.000000000000000000001")
		}, false},
		{"scientific integer", func(c profiles.Object) { c["listeners"].([]any)[0].(map[string]any)["port"] = json.Number("2.222e3") }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c profiles.Object
			if e := json.Unmarshal([]byte(tcpConfig), &c); e != nil {
				t.Fatal(e)
			}
			tc.change(c)
			typ, e := lookupProfileType(t.Context(), "tcp-banner", 1)
			if e != nil {
				t.Fatal(e)
			}
			e = typ.CheckConfig(t.Context(), c)
			if tc.valid != (e == nil) {
				t.Fatalf("config error: %v", e)
			}
			if e != nil && !errors.Is(e, profiles.ErrConfigInvalid) {
				t.Fatalf("wrong error: %v", e)
			}
		})
	}
	if _, e := lookupProfileType(t.Context(), "tcp-banner", 2); !errors.Is(e, profiles.ErrUnknownType) {
		t.Fatalf("unknown version: %v", e)
	}
}

func TestTCPConfigFieldPaths(t *testing.T) {
	c, e := httpx.DecodeObject([]byte(tcpConfig))
	if e != nil {
		t.Fatal(e)
	}
	c["listeners"].([]any)[0].(map[string]any)["port"] = 0
	e = checkTCPConfig(t.Context(), c)
	var fieldErr *profiles.ValidationError
	if !errors.As(e, &fieldErr) || len(fieldErr.Fields) == 0 || fieldErr.Fields[0].Path != "/config/listeners/0/port" {
		t.Fatalf("paths: %#v %v", fieldErr, e)
	}
}
