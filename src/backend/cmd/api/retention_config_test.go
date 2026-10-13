package main

import (
	"testing"
	"time"
)

func TestHistoryRetention(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  time.Duration
		valid bool
	}{
		{"", 24 * time.Hour, true}, {"48h", 48 * time.Hour, true}, {"0", 0, false}, {"-1h", 0, false}, {"oops", 0, false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("HISTORY_RETENTION", tt.value)
			got, err := historyRetention()
			if (err == nil) != tt.valid || got != tt.want {
				t.Fatalf("got %v %v", got, err)
			}
		})
	}
}
