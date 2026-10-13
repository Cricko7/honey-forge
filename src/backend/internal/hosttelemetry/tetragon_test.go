package hosttelemetry

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/process_exec.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseTetragon(t *testing.T) {
	raw := fixture(t)
	now := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name             string
		raw              []byte
		ignored, invalid bool
	}{
		{name: "documented JSON", raw: raw},
		{name: "exit ignored", raw: []byte(`{"process_exit":{},"time":"2026-10-13T12:00:01Z"}`), ignored: true},
		{name: "missing process", raw: []byte(`{"process_exec":{}}`), invalid: true},
		{name: "missing uid", raw: bytes.Replace(raw, []byte(`"uid": 1000,`), nil, 1), invalid: true},
		{name: "bad time", raw: bytes.ReplaceAll(raw, []byte("2026-10-13T12:00:01Z"), []byte("not-time")), invalid: true},
		{name: "future", raw: bytes.ReplaceAll(raw, []byte("2026-10-13T12:00:01Z"), []byte("2026-10-14T12:00:01Z")), invalid: true},
		{name: "oversized", raw: []byte(strings.Repeat("x", MaxRecordBytes+1)), invalid: true},
		{name: "invalid JSON", raw: []byte(`{"process_exec":`), invalid: true},
		{name: "null exec", raw: []byte(`{"process_exec":null}`), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := ParseTetragon(tc.raw, now)
			if (err != nil) != tc.invalid || !tc.invalid && (e == nil) != tc.ignored {
				t.Fatalf("event=%+v err=%v", e, err)
			}
			if e == nil {
				return
			}
			if e.Binary != "/usr/bin/id" || e.PID != 2345 || e.UID != 1000 || e.PPID == nil || *e.PPID != 1 {
				t.Fatalf("wrong process: %+v", e)
			}
			encoded, err := json.Marshal(e)
			if err != nil || bytes.Contains(encoded, []byte("super-secret")) {
				t.Fatalf("sensor arguments retained: %s %v", encoded, err)
			}
		})
	}
}

func TestContainerMatches(t *testing.T) {
	full := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, observed string
		match          bool
	}{
		{"full", full, true},
		{"documented docker prefix", full[:31], true},
		{"docker URI", "docker://" + full, true},
		{"short unsafe", "aaaa", false},
		{"other", strings.Repeat("b", 64), false},
		{"arbitrary suffix", "some/path/" + full, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ContainerMatches(tc.observed, full); got != tc.match {
				t.Fatalf("match=%v want=%v", got, tc.match)
			}
		})
	}
}
