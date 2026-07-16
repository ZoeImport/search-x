// Package logging builds the shared stdout and rolling-file structured logger.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Config struct {
	Level      string
	File       string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

func New(config Config) (*slog.Logger, io.Closer, error) {
	level, err := parseLevel(config.Level)
	if err != nil {
		return nil, nil, err
	}
	if config.File == "" {
		return nil, nil, fmt.Errorf("log file must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(config.File), 0o750); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	if config.MaxSizeMB <= 0 {
		config.MaxSizeMB = 100
	}
	if config.MaxBackups <= 0 {
		config.MaxBackups = 5
	}
	if config.MaxAgeDays <= 0 {
		config.MaxAgeDays = 7
	}
	rolling := &lumberjack.Logger{Filename: config.File, MaxSize: config.MaxSizeMB, MaxBackups: config.MaxBackups, MaxAge: config.MaxAgeDays, Compress: config.Compress}
	writer := io.MultiWriter(os.Stdout, rolling)
	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}))
	return logger, rolling, nil
}

func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", raw)
	}
}
