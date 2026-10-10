package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"honey-forge/internal/contract"
)

func testService(t *testing.T, defs ...Definition) *Service {
	t.Helper()
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if defs == nil {
		defs = BuiltinDefinitions()
	}
	s, err := NewService(defs, codec)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func operatorContext(role contract.Role) context.Context {
	return contract.WithPrincipal(context.Background(), contract.Principal{Role: role, OrganizationID: "11111111-1111-4111-8111-111111111111"})
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var e *contract.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func TestNewService(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*Definition)
	}{
		{"missing base action", func(d *Definition) { d.Entry.Actions = d.Entry.Actions[:2] }},
		{"duplicate event", func(d *Definition) { d.Entry.EventSchemas = append(d.Entry.EventSchemas, d.Entry.EventSchemas[0]) }},
		{"duplicate action", func(d *Definition) { d.Entry.Actions = append(d.Entry.Actions, d.Entry.Actions[0]) }},
		{"schema without draft", func(d *Definition) { d.Entry.ConfigSchema = json.RawMessage(`{"type":"object"}`) }},
		{"remote reference", func(d *Definition) {
			d.Entry.ConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"https://example.com/schema"}`)
		}},
		{"auth missing event", func(d *Definition) { d.SupportsAuthentication = true }},
		{"service action missing event", func(d *Definition) { d.SupportsServiceActions = true }},
		{"unsafe UI widget", func(d *Definition) { d.Entry.UI.Widgets["/listeners"] = "javascript" }},
		{"invalid UI pointer", func(d *Definition) { d.Entry.UI.FieldOrder = []string{"listeners"} }},
		{"empty title", func(d *Definition) { d.Entry.Title = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defs := BuiltinDefinitions()
			tt.change(&defs[0])
			codec, _ := contract.NewCursorCodec(make([]byte, 32))
			if _, err := NewService(defs, codec); err == nil {
				t.Fatal("accepted invalid descriptor")
			}
		})
	}
	defs := BuiltinDefinitions()
	defs = append(defs, defs[0])
	codec, _ := contract.NewCursorCodec(make([]byte, 32))
	if _, err := NewService(defs, codec); err == nil {
		t.Fatal("duplicate type version accepted")
	}
}

func TestLookupType(t *testing.T) {
	s := testService(t)
	e, err := s.LookupType(t.Context(), "tcp-banner", 1)
	wantCode(t, err, "")
	if e.InteractionLevel != "low" || !e.AvailableForNewProfiles || len(e.EventSchemas) != 3 || len(e.Actions) != 3 {
		t.Fatalf("incomplete entry: %+v", e)
	}
	e.UI.Widgets["/listeners"] = "password"
	e.ConfigSchema[0] = '!'
	e.Actions[0].Title = "changed"
	again, err := s.LookupType(t.Context(), "tcp-banner", 1)
	wantCode(t, err, "")
	if !json.Valid(again.ConfigSchema) || again.Actions[0].Title == "changed" || again.UI.Widgets["/listeners"] == "password" {
		t.Fatal("caller mutated catalogue")
	}
	wantCode(t, func() error { _, err := s.LookupType(t.Context(), "tcp-banner", 2); return err }(), "resource_not_found")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.LookupType(ctx, "tcp-banner", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestList(t *testing.T) {
	defs := BuiltinDefinitions()[:1]
	second := BuiltinDefinitions()[0]
	second.Entry.TypeVersion = 2
	second.Entry.AvailableForNewProfiles = false
	defs = append([]Definition{second}, defs...)
	s := testService(t, defs...)
	ctx := operatorContext(contract.Viewer)
	first, tag, err := s.List(ctx, Query{Limit: 1})
	wantCode(t, err, "")
	if len(first.Items) != 1 || first.Items[0].TypeVersion != 1 || first.NextCursor == nil {
		t.Fatalf("first page: %+v", first)
	}
	secondPage, tag2, err := s.List(ctx, Query{Limit: 1, Cursor: *first.NextCursor})
	wantCode(t, err, "")
	if tag != tag2 || len(secondPage.Items) != 1 || secondPage.Items[0].TypeVersion != 2 || secondPage.NextCursor != nil {
		t.Fatalf("second page: %+v", secondPage)
	}
	_, _, err = s.List(operatorContext(contract.Agent), Query{Limit: 1})
	wantCode(t, err, "forbidden")
	_, _, err = s.List(context.Background(), Query{Limit: 1})
	wantCode(t, err, "unauthenticated")
	_, _, err = s.List(ctx, Query{Limit: 1, Cursor: *first.NextCursor, TypeID: "tcp-banner"})
	wantCode(t, err, "invalid_cursor")
	other := contract.WithPrincipal(ctx, contract.Principal{Role: contract.Viewer, OrganizationID: contract.NewID()})
	_, _, err = s.List(other, Query{Limit: 1, Cursor: *first.NextCursor})
	wantCode(t, err, "invalid_cursor")
	unavailable := false
	page, _, err := s.List(ctx, Query{Limit: 100, Available: &unavailable})
	wantCode(t, err, "")
	if len(page.Items) != 1 || page.Items[0].TypeVersion != 2 {
		t.Fatal("availability filter")
	}
	page, _, err = s.List(ctx, Query{Limit: 50, TypeID: "missing"})
	wantCode(t, err, "")
	b, err := json.Marshal(page)
	wantCode(t, err, "")
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatal(string(b))
	}
}

func TestConcurrentReads(t *testing.T) {
	s := testService(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 8 {
				if _, _, err := s.List(operatorContext(contract.Admin), Query{Limit: 50}); err != nil {
					t.Error(err)
				}
				if err := s.CheckConfig(t.Context(), "tcp-banner", 1, json.RawMessage(validConfig)); err != nil {
					t.Error(err)
				}
				entry, err := s.LookupType(t.Context(), "tcp-banner", 1)
				if err != nil {
					t.Error(err)
					return
				}
				entry.UI.Widgets["/listeners"] = "independent caller"
			}
		})
	}
	wg.Wait()
}

func TestSnapshotETag(t *testing.T) {
	defs := BuiltinDefinitions()
	second := BuiltinDefinitions()[0]
	second.Entry.TypeVersion = 2
	a := testService(t, append(defs, second)...)
	b := testService(t, append([]Definition{second}, BuiltinDefinitions()...)...)
	ctx := operatorContext(contract.Admin)
	page, tag, err := a.List(ctx, Query{Limit: 1})
	wantCode(t, err, "")
	_, tag2, err := b.List(ctx, Query{Limit: 1, Cursor: *page.NextCursor})
	wantCode(t, err, "")
	if tag != tag2 {
		t.Fatal("ETag depends on installation order")
	}
	third := BuiltinDefinitions()[0]
	third.Entry.TypeVersion = 3
	c := testService(t, append(append(BuiltinDefinitions(), second), third)...)
	_, _, err = c.List(ctx, Query{Limit: 1, Cursor: *page.NextCursor})
	wantCode(t, err, "invalid_cursor")
}
