package graceful

import (
	"log/slog"
)

// Logger defines a minimal logging interface with only the methods used by the graceful package.
// It's compatible with slog.Logger but only includes the necessary methods.
type Logger interface {
	// Info logs at LevelInfo.
	Info(msg string, args ...any)

	// Warn logs at LevelWarn.
	Warn(msg string, args ...any)

	// Error logs at LevelError.
	Error(msg string, args ...any)
}

// newDefaultLogger returns the default no-op logger.
func newDefaultLogger() Logger { return noOpLogger{} }

type noOpLogger struct{}

func (noOpLogger) Info(_ string, _ ...any)  {}
func (noOpLogger) Warn(_ string, _ ...any)  {}
func (noOpLogger) Error(_ string, _ ...any) {}

func errAttr(err error) slog.Attr { return slog.Any("err", err) }
