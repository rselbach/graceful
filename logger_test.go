package graceful

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// The mockLogger is defined in graceful_test.go

func TestLoggerInterface(t *testing.T) {
	r := require.New(t)

	// test that slog.Logger implements our Logger interface
	var _ Logger = slog.Default()
	r.NotNil(slog.Default())

	// test newDefaultLogger returns a valid logger that implements our interface
	logger := newDefaultLogger()
	r.NotNil(logger)
}

func TestErrorAttr(t *testing.T) {
	r := require.New(t)

	testErr := fmt.Errorf("test error")
	attr := errAttr(testErr)

	r.Equal("err", attr.Key)
	r.Equal(testErr, attr.Value.Any())
}

func TestLoggerMethodsCalled(t *testing.T) {
	r := require.New(t)

	// create mock logger
	logger := &mockLogger{}

	// call methods
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	// verify calls were recorded
	r.Equal(1, logger.infoCalls)
	r.Equal(1, logger.warnCalls)
	r.Equal(1, logger.errorCalls)

	// verify messages were stored
	r.Equal(3, len(logger.msgs))
	r.Equal("info message", logger.msgs[0])
	r.Equal("warn message", logger.msgs[1])
	r.Equal("error message", logger.msgs[2])
}
