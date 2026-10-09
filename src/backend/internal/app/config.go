package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL   string
	AllowedOrigin string
	HTTPAddr      string
	LogLevel      slog.Level
}

func LoadConfig() (Config, error) {
	config := Config{
		DatabaseURL:   strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AllowedOrigin: strings.TrimSpace(os.Getenv("ALLOWED_ORIGIN")),
		HTTPAddr:      strings.TrimSpace(os.Getenv("HTTP_ADDR")),
		LogLevel:      slog.LevelInfo,
	}

	if config.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if err := validateOrigin(config.AllowedOrigin); err != nil {
		return Config{}, err
	}

	if config.HTTPAddr == "" {
		config.HTTPAddr = ":8080"
	}

	_, port, err := net.SplitHostPort(config.HTTPAddr)
	if err != nil {
		return Config{}, errors.New("HTTP_ADDR must use host:port format")
	}

	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return Config{}, errors.New("HTTP_ADDR port must be between 1 and 65535")
	}

	if raw := os.Getenv("LOG_LEVEL"); raw != "" {
		if err := config.LogLevel.UnmarshalText([]byte(raw)); err != nil {
			return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error: %w", err)
		}
	}

	return config, nil
}

func validateOrigin(origin string) error {
	value, err := url.Parse(origin)
	if err != nil || value.Scheme != "https" || value.Hostname() == "" || value.User != nil ||
		value.Path != "" || value.RawQuery != "" || value.ForceQuery || value.Fragment != "" ||
		value.Opaque != "" || origin != "https://"+value.Host {
		return errors.New("ALLOWED_ORIGIN must be an exact HTTPS origin without path, query or fragment")
	}

	return nil
}
