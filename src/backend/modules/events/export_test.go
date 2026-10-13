package events

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
)

type exportFake struct {
	queries []Query
	err     error
}

func (f *exportFake) List(_ context.Context, q Query) ([]EventSummary, bool, int64, int64, error) {
	f.queries = append(f.queries, q)

	id := "11111111-1111-4111-8111-111111111111"
	if len(f.queries) > 1 {
		id = "22222222-2222-4222-8222-222222222222"
	}

	return []EventSummary{{EventID: id, OccurredAt: "2026-10-12T07:30:00Z"}}, len(f.queries) == 1, 42, 0, f.err
}

func (f *exportFake) Read(_ context.Context, id string) (Event, error) {
	return Event{
		AgentEvent: AgentEvent{
			EventID: id, EventType: "=formula",
			OccurredAt: "2026-10-12T07:30:00Z",
			Source:     Source{IP: "2001:db8::1"},
			Data:       json.RawMessage(`{"input":"a,b\nкоманда"}`),
		},
		ReceivedAt: time.Date(2026, 10, 12, 7, 31, 0, 0, time.UTC),
	}, f.err
}

func TestExport(t *testing.T) {
	for _, format := range []string{"csv", "stix"} {
		t.Run(format, func(t *testing.T) {
			f := &exportFake{}
			var out bytes.Buffer
			ctx := contract.WithPrincipal(t.Context(), contract.Principal{Role: contract.Viewer, OrganizationID: "org"})
			err := NewHistoryService(f).Export(ctx, Query{TrapID: "trap", TypeID: "tcp-banner", Boundary: -1}, format, &out)
			if err != nil {
				t.Fatal(err)
			}

			if len(f.queries) != 2 || f.queries[1].Boundary != 42 || f.queries[1].AfterID == "" || f.queries[1].TypeID != "tcp-banner" {
				t.Fatalf("queries: %+v", f.queries)
			}

			if format == "csv" {
				rows, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
				if err != nil || len(rows) != 3 || rows[1][1] != "'=formula" || !strings.Contains(rows[1][15], "команда") {
					t.Fatalf("csv: %v %v", rows, err)
				}
			} else {
				var bundle struct {
					Type    string
					Objects []map[string]any
				}
				if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
					t.Fatal(err)
				}

				if bundle.Type != "bundle" || len(bundle.Objects) != 4 || bundle.Objects[0]["type"] != "ipv6-addr" || bundle.Objects[1]["x_honeyforge_event"] == nil {
					t.Fatalf("bundle: %+v", bundle)
				}
			}
		})
	}
}

func TestExportRejectsAccessAndStorageFailure(t *testing.T) {
	f := &exportFake{}
	var out bytes.Buffer
	if err := NewHistoryService(f).Export(t.Context(), Query{}, "csv", &out); err == nil || len(f.queries) != 0 {
		t.Fatal("unauthorized export")
	}

	ctx := contract.WithPrincipal(t.Context(), contract.Principal{Role: contract.Admin, OrganizationID: "org"})
	f.err = errors.New("storage failure")

	if err := NewHistoryService(f).Export(ctx, Query{}, "csv", &out); !errors.Is(err, f.err) {
		t.Fatalf("error: %v", err)
	}
}
