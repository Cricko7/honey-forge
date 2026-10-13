package main

import (
	"fmt"
	"os"
	"strconv"
)

func apiKeyRateLimit() (int, error) {
	value := os.Getenv("API_KEY_RATE_LIMIT_PER_MINUTE")
	if value == "" {
		return 60, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 {
		return 0, fmt.Errorf("API_KEY_RATE_LIMIT_PER_MINUTE must be a positive integer")
	}

	return limit, nil
}
