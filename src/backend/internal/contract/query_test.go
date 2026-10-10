package contract

import (
	"net/url"
	"testing"
)

func TestQuery(t *testing.T) {
	for _, tt := range []struct {
		query string
		code  string
		limit int
	}{
		{"", "", 50}, {"limit=100", "", 100}, {"limit=0", "invalid_query", 0}, {"limit=1&limit=2", "invalid_query", 0}, {"sort=name", "invalid_query", 0}, {"from=2026-01-01T00:00:00Z", "invalid_query", 0},
	} {
		t.Run(tt.query, func(t *testing.T) {
			v, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			page, e := ParseListQuery(v)
			if tt.code == "" {
				if e != nil || page.Limit != tt.limit {
					t.Fatalf("%+v %v", page, e)
				}
			} else if e == nil || e.Code != tt.code {
				t.Fatalf("error %v", e)
			}
		})
	}
}

func TestCursor(t *testing.T) {
	codec, err := NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	scope := CursorScope{OrganizationID: "11111111-1111-4111-8111-111111111111", Collection: "profiles", Filters: "type=tcp-banner"}
	token, err := codec.Encode(scope, CursorPosition{Boundary: "first-page-snapshot", After: "last-id"})
	if err != nil {
		t.Fatal(err)
	}
	position, err := codec.Decode(scope, token)
	if err != nil || position.Boundary != "first-page-snapshot" {
		t.Fatalf("%+v %v", position, err)
	}
	foreign := scope
	foreign.OrganizationID = "22222222-2222-4222-8222-222222222222"
	for _, tt := range []struct {
		scope CursorScope
		token string
	}{{foreign, token}, {scope, token + "x"}, {CursorScope{OrganizationID: scope.OrganizationID, Collection: "traps"}, token}} {
		if _, err := codec.Decode(tt.scope, tt.token); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
}

func TestEnvelope(t *testing.T) {
	for _, tt := range []struct {
		body string
		ok   bool
	}{
		{`{"message_id":"11111111-1111-4111-8111-111111111111","type":"hello","payload":{}}`, true},
		{`{"message_id":"11111111-1111-4111-8111-111111111111","type":"hello","payload":null}`, false},
		{`{"message_id":"bad","type":"hello","payload":{}}`, false},
		{`{"message_id":"11111111-1111-4111-8111-111111111111","type":"hello","payload":[],"extra":1}`, false},
	} {
		_, err := DecodeEnvelope([]byte(tt.body))
		if (err == nil) != tt.ok {
			t.Fatalf("%s: %v", tt.body, err)
		}
	}
}
