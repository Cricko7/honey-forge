package events

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"honey-forge/internal/contract"
)

// Export walks one ingestion boundary, so new arrivals never extend the download.
func (s *HistoryService) Export(ctx context.Context, q Query, format string, out io.Writer) error {
	if _, err := readAccess(ctx); err != nil {
		return err
	}

	if format != "csv" && format != "stix" {
		return contract.NewError("invalid_query")
	}

	q.Limit = 100
	csvWriter := csv.NewWriter(out)
	encoder := json.NewEncoder(out)
	if format == "csv" {
		if err := csvWriter.Write([]string{"event_id", "event_type", "trap_id", "type_id", "type_version", "profile_revision", "occurred_at", "received_at", "session_id", "session_sequence", "source_ip", "source_port", "destination_protocol", "destination_port", "source_enrichment", "data"}); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(out, `{"type":"bundle","id":"bundle--%s","objects":[`, contract.NewID()); err != nil {
		return err
	}

	first := true
	for {
		items, more, boundary, _, err := s.List(ctx, q)
		if err != nil {
			return fmt.Errorf("list export events: %w", err)
		}

		q.Boundary = boundary
		for _, item := range items {
			// shortcut: detail reads reuse the organization checks; batch these if large exports become slow.
			event, err := s.Read(ctx, item.EventID)
			if err != nil {
				return fmt.Errorf("read export event: %w", err)
			}

			if format == "csv" {
				enrichment, err := json.Marshal(event.SourceEnrichment)
				if err != nil {
					return err
				}

				row := []string{
					event.EventID, event.EventType, event.TrapID, event.TypeID,
					strconv.FormatInt(event.TypeVersion, 10), strconv.FormatInt(event.ProfileRevision, 10),
					event.OccurredAt, event.ReceivedAt.Format(time.RFC3339Nano),
					event.SessionID, strconv.FormatInt(event.SessionSequence, 10),
					event.Source.IP, strconv.Itoa(event.Source.Port),
					event.Destination.Protocol, strconv.Itoa(event.Destination.Port),
					string(enrichment), string(event.Data),
				}
				for i, v := range row {
					trimmed := strings.TrimLeft(v, " \t\r\n")
					formula := trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0]))
					control := v != "" && strings.ContainsRune("\t\r\n", rune(v[0]))
					if formula || control {
						row[i] = "'" + v
					}
				}

				if err := csvWriter.Write(row); err != nil {
					return err
				}
			} else {
				objects, err := stixObjects(event)
				if err != nil {
					return err
				}

				for _, object := range objects {
					if !first {
						if _, err := io.WriteString(out, ","); err != nil {
							return err
						}
					}

					first = false
					if err := encoder.Encode(object); err != nil {
						return err
					}
				}
			}
		}

		if !more {
			break
		}
		if len(items) == 0 {
			return fmt.Errorf("empty export page with continuation")
		}

		last := items[len(items)-1]
		at, err := time.Parse(time.RFC3339Nano, last.OccurredAt)
		if err != nil {
			return fmt.Errorf("parse export position: %w", err)
		}

		q.AfterTime, q.AfterID = &at, last.EventID
	}

	if format == "csv" {
		csvWriter.Flush()
		return csvWriter.Error()
	}

	_, err := io.WriteString(out, "]}")
	return err
}

func stixObjects(event Event) ([]any, error) {
	ip, err := netip.ParseAddr(event.Source.IP)
	if err != nil {
		return nil, fmt.Errorf("parse export source: %w", err)
	}

	kind := "ipv6-addr"
	if ip.Unmap().Is4() {
		kind = "ipv4-addr"
	}

	id := kind + "--" + string(contract.NewID())
	at, err := time.Parse(time.RFC3339Nano, event.OccurredAt)
	if err != nil {
		return nil, fmt.Errorf("parse observation time: %w", err)
	}

	stamp := at.UTC().Format("2006-01-02T15:04:05.000000000Z")
	created := event.ReceivedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")

	return []any{
		map[string]any{"type": kind, "spec_version": "2.1", "id": id, "value": ip.Unmap().String()},
		map[string]any{
			"type": "observed-data", "spec_version": "2.1",
			"id":      "observed-data--" + string(contract.NewID()),
			"created": created, "modified": created,
			"first_observed": stamp, "last_observed": stamp,
			"number_observed": 1, "object_refs": []string{id},
			"x_honeyforge_event": event,
		},
	}, nil
}
