package main

import (
	"fmt"
	"os"
	"time"
)

func historyRetention() (time.Duration, error) {
	raw := os.Getenv("HISTORY_RETENTION")
	if raw == "" {
		return 24 * time.Hour, nil
	}
	retention, err := time.ParseDuration(raw)
	if err != nil || retention <= 0 {
		return 0, fmt.Errorf("HISTORY_RETENTION must be a positive duration, e.g. 24h")
	}
	return retention, nil
}
