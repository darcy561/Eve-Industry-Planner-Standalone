package logs

import (
	"log/slog"
	"os"
	"strings"
)

// Logger returns a structured JSON logger configured via LOG_LEVEL env.
func Logger() *slog.Logger {
	level := parseLogLevel(os.Getenv("LOG_LEVEL"))
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(handler)
}

func parseLogLevel(v string) slog.Leveler {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		lvl := slog.LevelDebug
		return &lvl
	case "warn", "warning":
		lvl := slog.LevelWarn
		return &lvl
	case "error":
		lvl := slog.LevelError
		return &lvl
	default:
		lvl := slog.LevelInfo
		return &lvl
	}
}

// Convenience helpers for functions to log without wiring a logger.

// Debug logs a debug message with optional key/value pairs.
func Debug(msg string, kv ...any) {
	Logger().Debug(msg, kv...)
}

// Info logs an info message with optional key/value pairs.
func Info(msg string, kv ...any) {
	Logger().Info(msg, kv...)
}

// Warn logs a warning message with optional key/value pairs.
func Warn(msg string, kv ...any) {
	Logger().Warn(msg, kv...)
}

// Error logs an error message with optional key/value pairs.
func Error(msg string, kv ...any) {
	Logger().Error(msg, kv...)
}

// Component returns a logger pre-tagged with a component field.
func Component(name string) *slog.Logger {
	return Logger().With(slog.String("component", name))
}
