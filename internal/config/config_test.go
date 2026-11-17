package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8080" || cfg.DataDir != "./data" || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.ReadHeaderTimeout != 5*time.Second || cfg.ReadTimeout != 0 || cfg.WriteTimeout != 0 || cfg.IdleTimeout != time.Minute || cfg.ShutdownTimeout != 30*time.Second {
		t.Fatalf("unexpected timeout defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	values := map[string]string{
		"BLOBLIN_LISTEN_ADDR":              "127.0.0.1:9000",
		"BLOBLIN_DATA_DIR":                 "storage",
		"BLOBLIN_LOG_LEVEL":                "debug",
		"BLOBLIN_HTTP_READ_HEADER_TIMEOUT": "2s",
		"BLOBLIN_HTTP_READ_TIMEOUT":        "3s",
		"BLOBLIN_HTTP_WRITE_TIMEOUT":       "4s",
		"BLOBLIN_HTTP_IDLE_TIMEOUT":        "5s",
		"BLOBLIN_SHUTDOWN_TIMEOUT":         "6s",
	}
	cfg, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" || cfg.DataDir != "storage" || cfg.LogLevel != slog.LevelDebug || cfg.ReadHeaderTimeout != 2*time.Second || cfg.ReadTimeout != 3*time.Second || cfg.WriteTimeout != 4*time.Second || cfg.IdleTimeout != 5*time.Second || cfg.ShutdownTimeout != 6*time.Second {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	tests := []struct {
		name, value string
	}{
		{"BLOBLIN_LISTEN_ADDR", "localhost"},
		{"BLOBLIN_LISTEN_ADDR", ":70000"},
		{"BLOBLIN_LISTEN_ADDR", ":-1"},
		{"BLOBLIN_LISTEN_ADDR", "localhost:http"},
		{"BLOBLIN_LISTEN_ADDR", ""},
		{"BLOBLIN_DATA_DIR", " "},
		{"BLOBLIN_DATA_DIR", ""},
		{"BLOBLIN_LOG_LEVEL", "trace"},
		{"BLOBLIN_LOG_LEVEL", "INFO+1"},
		{"BLOBLIN_LOG_LEVEL", ""},
		{"BLOBLIN_HTTP_READ_HEADER_TIMEOUT", "0s"},
		{"BLOBLIN_HTTP_READ_HEADER_TIMEOUT", "-1s"},
		{"BLOBLIN_HTTP_READ_TIMEOUT", "-1s"},
		{"BLOBLIN_HTTP_WRITE_TIMEOUT", "-1s"},
		{"BLOBLIN_HTTP_WRITE_TIMEOUT", "later"},
		{"BLOBLIN_HTTP_IDLE_TIMEOUT", "0s"},
		{"BLOBLIN_HTTP_IDLE_TIMEOUT", "-1s"},
		{"BLOBLIN_SHUTDOWN_TIMEOUT", "0s"},
		{"BLOBLIN_SHUTDOWN_TIMEOUT", "-1s"},
		{"BLOBLIN_SHUTDOWN_TIMEOUT", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			_, err := load(func(key string) (string, bool) {
				if key == tc.name {
					return tc.value, true
				}
				return "", false
			})
			if err == nil || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("expected actionable %s error, got %v", tc.name, err)
			}
		})
	}
}

func TestLoadAllowsEphemeralPortAndDisabledTransferTimeouts(t *testing.T) {
	values := map[string]string{
		"BLOBLIN_LISTEN_ADDR":        "[::1]:0",
		"BLOBLIN_HTTP_READ_TIMEOUT":  "0s",
		"BLOBLIN_HTTP_WRITE_TIMEOUT": "0s",
	}
	cfg, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "[::1]:0" || cfg.ReadTimeout != 0 || cfg.WriteTimeout != 0 {
		t.Fatalf("unexpected configuration: %+v", cfg)
	}
}
