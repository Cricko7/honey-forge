package events

import (
	"encoding/json"
	"testing"
	"time"
)

type storedEventRow struct{ envelope string }

func (r storedEventRow) Scan(dest ...any) error {
	*dest[0].(*[]byte) = []byte(r.envelope)
	*dest[1].(*string) = "trap"
	*dest[2].(*time.Time) = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	*dest[3].(*json.RawMessage) = json.RawMessage(`{"password":"captured"}`)
	return nil
}

func TestReadEventEnrichment(t *testing.T) {
	for _, tc := range []struct {
		name, enrichment string
		want             bool
	}{
		{"stored", `,"source_enrichment":{"country_code":"US","asn":15169}`, true},
		{"legacy", "", false},
		{"unknown", `,"source_enrichment":{"country_code":null,"asn":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event, err := readEvent(storedEventRow{`{"event_id":"event","source":{"ip":"8.8.8.8","port":12345}` + tc.enrichment + `}`})
			if err != nil {
				t.Fatal(err)
			}
			if event.TrapID != "trap" || event.ReceivedAt.IsZero() || string(event.Data) != `{"password":"captured"}` {
				t.Fatalf("lost event details: %+v", event)
			}
			if event.SourceEnrichment == nil || (event.SourceEnrichment.ASN != nil) != tc.want {
				t.Fatalf("enrichment: %+v", event.SourceEnrichment)
			}
			summary := event.Summary()
			if summary.SourceEnrichment == nil || (summary.SourceEnrichment.ASN != nil) != tc.want {
				t.Fatalf("summary enrichment: %+v", summary)
			}
			raw, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["data"]; ok {
				t.Fatal("summary exposes captured data")
			}
		})
	}
}
