//go:build integration

package app

import (
	"reflect"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
)

func TestRealCatalogRejectsInvalidProfilesWithoutSideEffects(t *testing.T) {
	runtime := openIntegrationRuntime(t)
	admin := registerOperator(t, runtime, "admin@example.com", auth.OrganizationInput{Mode: "create", Name: "Demo"})
	base := catalogProfileRequest(t, runtime, admin)

	for _, tc := range []struct {
		name   string
		change func(*profiles.CreateRequest)
		code   string
	}{
		{"unknown_type", func(r *profiles.CreateRequest) { r.TypeID = "unknown" }, "unknown_trap_type"},
		{"unknown_version", func(r *profiles.CreateRequest) { r.TypeVersion = 2 }, "unknown_trap_type"},
		{"port_zero", func(r *profiles.CreateRequest) { r.Config["listeners"].([]any)[0].(profiles.Object)["port"] = 0 }, "config_invalid"},
		{"fractional_port", func(r *profiles.CreateRequest) { r.Config["listeners"].([]any)[0].(profiles.Object)["port"] = 2222.5 }, "config_invalid"},
		{"empty_listeners", func(r *profiles.CreateRequest) { r.Config["listeners"] = []any{} }, "config_invalid"},
		{"unknown_config_field", func(r *profiles.CreateRequest) { r.Config["unexpected"] = true }, "config_invalid"},
		{"duplicate_port", func(r *profiles.CreateRequest) {
			r.Config["listeners"] = append(r.Config["listeners"].([]any), profiles.Object{"name": "http", "port": 2222, "banner": "demo", "close_after_banner": true})
		}, "config_invalid"},
		{"duplicate_name", func(r *profiles.CreateRequest) {
			r.Config["listeners"] = append(r.Config["listeners"].([]any), profiles.Object{"name": "ssh", "port": 8080, "banner": "demo", "close_after_banner": true})
		}, "config_invalid"},
		{"inconsistent_logging", func(r *profiles.CreateRequest) { r.Config["logging"].(profiles.Object)["max_payload_bytes"] = 1 }, "config_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			input.RequestID = profiles.NewID()
			input.Config = validIntegrationConfig()
			tc.change(&input)
			requireOperatorError(t, sendOperator(t, runtime, "POST", "/api/profiles", integrationJSON(t, input), admin, 422), tc.code)

			// A rejected create must not reserve request_id or insert a profile.
			valid := base
			valid.RequestID = input.RequestID
			created := sendOperator(t, runtime, "POST", "/api/profiles", integrationJSON(t, valid), admin, 201)
			profile := decodeIntegration[profiles.Profile](t, created)
			path, etag := created.Header().Get("Location"), created.Header().Get("ETag")
			invalid := validIntegrationConfig()
			invalid["listeners"].([]any)[0].(profiles.Object)["port"] = 0
			requireOperatorError(t, sendOperator(t, runtime, "PATCH", path, integrationJSON(t, profiles.Object{"config": invalid}), admin, 422, etag), "config_invalid")
			read := sendOperator(t, runtime, "GET", path, "", admin, 200)
			if read.Header().Get("ETag") != etag || !reflect.DeepEqual(decodeIntegration[profiles.Profile](t, read), profile) {
				t.Fatal("rejected configuration update changed persisted profile")
			}
			sendOperator(t, runtime, "DELETE", path, "", admin, 204, etag)
			empty := decodeIntegration[profiles.Page](t, sendOperator(t, runtime, "GET", "/api/profiles", "", admin, 200))
			if len(empty.Items) != 0 {
				t.Fatal("rejected create left a profile in the database")
			}
		})
	}
}
