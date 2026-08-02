package config

import (
	"fmt"
	"strconv"
)

type Config struct {
	ServerPort         string
	DatastoreType      string
	DatastoreCSVPath   string
	RateLimitGlobalRPS int
	RateLimitPerIPRPS  int
	LogLevel           string
}

var validLogLevels = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}

func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		ServerPort:    envOrDefault(getenv, "SERVER_PORT", "8080"),
		DatastoreType: envOrDefault(getenv, "DATASTORE_TYPE", "csv"),
	}

	if cfg.DatastoreType == "csv" {
		cfg.DatastoreCSVPath = getenv("DATASTORE_CSV_PATH")
		if cfg.DatastoreCSVPath == "" {
			return Config{}, fmt.Errorf("DATASTORE_CSV_PATH is required when DATASTORE_TYPE=csv")
		}
	}

	globalRPS, err := parsePositiveInt(getenv, "RATE_LIMIT_GLOBAL_RPS")
	if err != nil {
		return Config{}, err
	}
	cfg.RateLimitGlobalRPS = globalRPS

	perIPRPS, err := parsePositiveInt(getenv, "RATE_LIMIT_PER_IP_RPS")
	if err != nil {
		return Config{}, err
	}
	cfg.RateLimitPerIPRPS = perIPRPS

	cfg.LogLevel = envOrDefault(getenv, "LOG_LEVEL", "info")
	if !validLogLevels[cfg.LogLevel] {
		return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: must be one of debug, info, warn, error", cfg.LogLevel)
	}

	return cfg, nil
}

func envOrDefault(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

func parsePositiveInt(getenv func(string) string, key string) (int, error) {
	raw := getenv(key)
	if raw == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %d", key, n)
	}
	return n, nil
}
