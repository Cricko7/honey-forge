package enginepipe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
)

func TestDionaeaCredentialsUsePinnedIdentityAndBoundedNullableFields(t *testing.T) {
	s := testSnapshot(t)
	s.TypeID = "dionaea"
	s.Config["services"] = []any{map[string]any{"name": "database", "port": 3306, "protocol": "mysql"}}
	c, err := ParseConfig(t.Context(), s, "dionaea")
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	b := bridge{snapshot: s, config: c, sessions: map[string]*session{}}
	var observed []events.AgentEvent
	sink := func(ctx context.Context, e events.AgentEvent) error {
		if err := catalogue.CheckEvent(ctx, "dionaea", 1, e.EventType, e.Data); err != nil {
			return err
		}
		observed = append(observed, e)
		return nil
	}
	if err := b.accept(t.Context(), message{Kind: "opened", SessionID: sessionID, SourceIP: "192.0.2.1", SourcePort: 1234}, sink); err != nil {
		t.Fatal(err)
	}
	if err := b.accept(t.Context(), message{Kind: "auth", SessionID: sessionID, Username: "guest"}, sink); err != nil {
		t.Fatal(err)
	}
	e := observed[1]
	var data map[string]any
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["password"] != nil || data["username"] != "guest" || data["outcome"] != "unknown" || e.TypeID != "dionaea" || e.Destination.Port != 3306 || e.SessionSequence != 2 {
		t.Fatal(e, string(e.Data))
	}
	for _, tc := range []struct {
		name string
		m    message
	}{
		{"unknown session", message{Kind: "auth", SessionID: string(contract.NewID())}},
		{"oversized login", message{Kind: "auth", SessionID: sessionID, Username: strings.Repeat("я", 513)}},
		{"double counted bytes", message{Kind: "auth", SessionID: sessionID, ReceivedBytes: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := b.accept(t.Context(), tc.m, sink); err == nil {
				t.Fatal("accepted invalid credentials event")
			}
		})
	}
	s.TypeID = "conpot"
	b.snapshot = s
	if err := b.accept(t.Context(), message{Kind: "auth", SessionID: sessionID}, sink); err == nil {
		t.Fatal("Conpot accepted credentials observations")
	}
}
