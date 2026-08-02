package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ip2country/internal/config"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestLoad_AppliesDefaultsAndParsesRequiredValues(t *testing.T) {
	getenv := fakeEnv(map[string]string{
		"DATASTORE_CSV_PATH":    "testdata/foo.csv",
		"RATE_LIMIT_GLOBAL_RPS": "100",
		"RATE_LIMIT_PER_IP_RPS": "10",
	})

	cfg, err := config.Load(getenv)

	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.ServerPort)
	assert.Equal(t, "csv", cfg.DatastoreType)
	assert.Equal(t, "testdata/foo.csv", cfg.DatastoreCSVPath)
	assert.Equal(t, 100, cfg.RateLimitGlobalRPS)
	assert.Equal(t, 10, cfg.RateLimitPerIPRPS)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestLoad_RejectsInvalidInput(t *testing.T) {
	base := map[string]string{
		"DATASTORE_CSV_PATH":    "testdata/foo.csv",
		"RATE_LIMIT_GLOBAL_RPS": "100",
		"RATE_LIMIT_PER_IP_RPS": "10",
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing global rps", func(v map[string]string) { delete(v, "RATE_LIMIT_GLOBAL_RPS") }},
		{"non-integer global rps", func(v map[string]string) { v["RATE_LIMIT_GLOBAL_RPS"] = "fast" }},
		{"zero global rps", func(v map[string]string) { v["RATE_LIMIT_GLOBAL_RPS"] = "0" }},
		{"negative per-ip rps", func(v map[string]string) { v["RATE_LIMIT_PER_IP_RPS"] = "-1" }},
		{"missing csv path for csv datastore", func(v map[string]string) { delete(v, "DATASTORE_CSV_PATH") }},
		{"invalid log level", func(v map[string]string) { v["LOG_LEVEL"] = "verbose" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := make(map[string]string, len(base))
			for k, v := range base {
				values[k] = v
			}
			tt.mutate(values)

			_, err := config.Load(fakeEnv(values))

			assert.Error(t, err)
		})
	}
}
