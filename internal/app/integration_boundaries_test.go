package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"honey-forge/internal/catalog"
	"honey-forge/internal/contract"
	"honey-forge/src/backend/modules/profiles"
	"net/http/httptest"
	"testing"
)

func TestRouterIgnoresUntrustedProxyIdentity(t *testing.T) {
	router := NewRouter()
	router.GET("/probe", func(c *gin.Context) { c.String(200, c.ClientIP()) })
	req := httptest.NewRequest("GET", "/probe", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Body.String() != "192.0.2.1" {
		t.Fatalf("trusted spoofed identity: %s", response.Body.String())
	}
}

func TestProfileCatalogSecretBoundary(t *testing.T) {
	definition := catalog.BuiltinDefinitions()[0]
	definition.Entry.TypeID = "secret-demo"
	definition.Entry.ConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string"},"password":{"type":"string","writeOnly":true}}}`)
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.NewService([]catalog.Definition{definition}, codec)
	if err != nil {
		t.Fatal(err)
	}
	typ, err := ProfileTypeLookup(service)(t.Context(), "secret-demo", 1)
	if err != nil {
		t.Fatal(err)
	}
	stored := profiles.Object{"name": "old", "password": "private"}
	fields := profiles.InstalledSecrets(typ, stored)
	if len(fields) != 1 || fields[0] != "/password" {
		t.Fatal(fields)
	}
	p := profiles.Profile{Config: stored, SecretFieldsSet: fields}
	redacted := profiles.Redacted(p)
	if _, ok := redacted.Config["password"]; ok {
		t.Fatal("secret leaked")
	}
	for _, tc := range []struct {
		name       string
		patch      profiles.PatchRequest
		wantSecret bool
		want       error
	}{
		{"preserve", profiles.PatchRequest{Config: profiles.Object{"name": "new"}}, true, nil},
		{"clear only", profiles.PatchRequest{ClearSecretFields: []string{"/password"}}, false, nil},
		{"clear replacement", profiles.PatchRequest{Config: profiles.Object{"name": "new"}, ClearSecretFields: []string{"/password"}}, false, nil},
		{"conflict", profiles.PatchRequest{Config: profiles.Object{"name": "new", "password": "replacement"}, ClearSecretFields: []string{"/password"}}, false, profiles.ErrValidation},
		{"invalid replacement", profiles.PatchRequest{Config: profiles.Object{"name": 22}}, false, profiles.ErrConfigInvalid},
		{"clear nonsecret", profiles.PatchRequest{ClearSecretFields: []string{"/name"}}, false, profiles.ErrValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := profiles.PatchConfig(typ, p, tc.patch)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error: %v want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if err := typ.CheckConfig(t.Context(), value); err != nil {
				t.Fatal(err)
			}
			_, hasSecret := value["password"]
			if hasSecret != tc.wantSecret {
				t.Fatal(value)
			}
			if stored["password"] != "private" {
				t.Fatal("stored input mutated")
			}
		})
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ProfileTypeLookup(service)(canceled, "secret-demo", 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ProfileTypeLookup(service)(t.Context(), "secret-demo", 2); !errors.Is(err, profiles.ErrUnknownType) {
		t.Fatal(err)
	}
}
