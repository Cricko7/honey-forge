package stream

import (
	"testing"
	"time"

	"honey-forge/internal/contract"
)

func TestFrontendCursor(t *testing.T) {
	codec, err := contract.NewCursorCodec([]byte("integration-cursor-key-32-bytes!"))
	if err != nil {
		t.Fatal(err)
	}
	org := contract.NewID()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	token, err := EncodeCursor(codec, org, 42, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		org   contract.ID
		token string
		now   time.Time
		code  string
	}{
		{"same organization", org, token, now, ""},
		{"other organization", contract.NewID(), token, now, "invalid_cursor"},
		{"tampered", org, "broken", now, "invalid_cursor"},
		{"expired", org, token, now.Add(24*time.Hour + time.Nanosecond), "cursor_expired"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			seq, err := DecodeCursor(codec, tt.org, tt.token, tt.now)
			if tt.code == "" {
				if err != nil || seq != 42 {
					t.Fatalf("sequence %d, error %v", seq, err)
				}
				return
			}
			e, ok := err.(*contract.Error)
			if !ok || e.Code != tt.code {
				t.Fatalf("error %v, want %s", err, tt.code)
			}
		})
	}
}
