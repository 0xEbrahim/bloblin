package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains the settings needed to start the single-node server.
type Config struct {
	ListenAddr        string
	DataDir           string
	LogLevel          slog.Level
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		ListenAddr:        ":8080",
		DataDir:           "./data",
		LogLevel:          slog.LevelInfo,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   30 * time.Second,
	}

	if value, ok := lookup("BLOBLIN_LISTEN_ADDR"); ok {
		cfg.ListenAddr = value
	}
	if value, ok := lookup("BLOBLIN_DATA_DIR"); ok {
		cfg.DataDir = value
	}
	if value, ok := lookup("BLOBLIN_LOG_LEVEL"); ok {
		if err := cfg.LogLevel.UnmarshalText([]byte(strings.ToUpper(value))); err != nil {
			return Config{}, fmt.Errorf("BLOBLIN_LOG_LEVEL: use DEBUG, INFO, WARN, or ERROR: %w", err)
		}
	}
	for _, setting := range []struct {
		name   string
		target *time.Duration
	}{
		{"BLOBLIN_HTTP_READ_HEADER_TIMEOUT", &cfg.ReadHeaderTimeout},
		{"BLOBLIN_HTTP_READ_TIMEOUT", &cfg.ReadTimeout},
		{"BLOBLIN_HTTP_WRITE_TIMEOUT", &cfg.WriteTimeout},
		{"BLOBLIN_HTTP_IDLE_TIMEOUT", &cfg.IdleTimeout},
		{"BLOBLIN_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
	} {
		if value, ok := lookup(setting.name); ok {
			duration, err := time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s: use a Go duration such as 5s or 1m: %w", setting.name, err)
			}
			*setting.target = duration
		}
	}
	return cfg, cfg.Validate()
}

func (cfg Config) Validate() error {
	_, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("BLOBLIN_LISTEN_ADDR: use host:port (for example, 127.0.0.1:8080): %w", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return fmt.Errorf("BLOBLIN_LISTEN_ADDR: port must be a number from 0 to 65535")
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return fmt.Errorf("BLOBLIN_DATA_DIR: set a non-empty directory path")
	}
	if cfg.LogLevel != slog.LevelDebug && cfg.LogLevel != slog.LevelInfo && cfg.LogLevel != slog.LevelWarn && cfg.LogLevel != slog.LevelError {
		return fmt.Errorf("BLOBLIN_LOG_LEVEL: use DEBUG, INFO, WARN, or ERROR")
	}
	for _, setting := range []struct {
		name      string
		value     time.Duration
		allowZero bool
	}{
		{"BLOBLIN_HTTP_READ_HEADER_TIMEOUT", cfg.ReadHeaderTimeout, false},
		{"BLOBLIN_HTTP_READ_TIMEOUT", cfg.ReadTimeout, true},
		{"BLOBLIN_HTTP_WRITE_TIMEOUT", cfg.WriteTimeout, true},
		{"BLOBLIN_HTTP_IDLE_TIMEOUT", cfg.IdleTimeout, false},
		{"BLOBLIN_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout, false},
	} {
		if setting.value < 0 || (!setting.allowZero && setting.value == 0) {
			return fmt.Errorf("%s: duration must be %s", setting.name, positiveOrNonnegative(setting.allowZero))
		}
	}
	return nil
}

func positiveOrNonnegative(allowZero bool) string {
	if allowZero {
		return "zero or positive"
	}
	return "positive"
}
