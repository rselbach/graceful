# Graceful - Graceful HTTP Server for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/rselbach/graceful.svg)](https://pkg.go.dev/github.com/rselbach/graceful)
[![Go Report Card](https://goreportcard.com/badge/github.com/rselbach/graceful)](https://goreportcard.com/report/github.com/rselbach/graceful)

Graceful is a lightweight wrapper around Go's standard `http.Server` that provides graceful shutdown handling, configurable timeouts, and structured logging.

## Features

- Graceful shutdown on OS signals (SIGINT, SIGTERM)
- Graceful shutdown on context cancellation
- Configurable timeouts via functional options pattern
- Structured logging with slog compatibility
- Simple API that builds on the standard library

## Installation

```bash
go get github.com/rselbach/graceful
```

## Quick Start

```go
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/rselbach/graceful"
)

func main() {
	// Create a standard HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	})

	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Create and configure the server
	server := graceful.New(mux,
		graceful.WithAddr(":8080"),
		graceful.WithReadTimeout(5*time.Second),
		graceful.WithWriteTimeout(10*time.Second),
		graceful.WithIdleTimeout(120*time.Second),
		graceful.WithShutdownTimeout(30*time.Second),
		graceful.WithLogger(logger),
	)

	// Start the server
	if err := server.ListenAndServe(); err != nil {
		logger.Error("Server error", "err", err)
		os.Exit(1)
	}
}
```

## Complete Example

Here's a more complete example that demonstrates how to use the graceful package in a real-world HTTP server:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/rselbach/graceful"
)

// Create a custom structured logger that implements the graceful.Logger interface
type appLogger struct {
	*slog.Logger
}

func newAppLogger() *appLogger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return &appLogger{Logger: slog.New(handler)}
}

// Define some HTTP handlers
func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, World!")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// Middleware for logging requests
func loggingMiddleware(logger graceful.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			
			// Call the next handler
			next.ServeHTTP(w, r)
			
			// Log the request
			logger.Info("HTTP request",
				"method", r.Method,
				"path", r.URL.Path,
				"duration", time.Since(start),
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		})
	}
}

func main() {
	// Create the logger
	logger := newAppLogger()
	logger.Info("Starting application")

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create the HTTP mux and register handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/", helloHandler)
	mux.HandleFunc("/health", healthHandler)

	// Apply middleware
	var handler http.Handler = mux
	handler = loggingMiddleware(logger)(handler)

	// Create and configure the server
	server := graceful.New(handler,
		graceful.WithAddr(":8080"),
		graceful.WithReadTimeout(5*time.Second),
		graceful.WithWriteTimeout(10*time.Second),
		graceful.WithIdleTimeout(120*time.Second),
		graceful.WithReadHeaderTimeout(5*time.Second),
		graceful.WithShutdownTimeout(30*time.Second),
		graceful.WithLogger(logger),
		graceful.WithBaseContext(ctx),
	)

	// Log the server configuration
	logger.Info("Server configured",
		"addr", ":8080",
		"read_timeout", "5s",
		"write_timeout", "10s",
		"idle_timeout", "120s",
	)

	// Start the server in a goroutine so we can demonstrate
	// programmatic shutdown alongside signal-based shutdown
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error", "err", err)
			os.Exit(1)
		}
	}()

	logger.Info("Server started, press Ctrl+C to stop")

	// Simulate work and then trigger a programmatic shutdown after 1 minute
	time.Sleep(1 * time.Minute)
	logger.Info("Initiating programmatic shutdown")
	
	// Create a context with a timeout for the shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	
	// Trigger the shutdown
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Shutdown error", "err", err)
	}

	logger.Info("Shutdown complete")
}
```

## Available Options

The `graceful.New` function accepts the following options:

| Option | Description | Default |
|--------|-------------|---------|
| `WithAddr(addr string)` | Sets the server's listening address | `:8080` |
| `WithReadTimeout(timeout time.Duration)` | Sets the server's read timeout | `5s` |
| `WithWriteTimeout(timeout time.Duration)` | Sets the server's write timeout | `10s` |
| `WithIdleTimeout(timeout time.Duration)` | Sets the server's idle timeout | `120s` |
| `WithReadHeaderTimeout(timeout time.Duration)` | Sets the server's read header timeout | `5s` |
| `WithShutdownTimeout(timeout time.Duration)` | Sets the timeout for graceful shutdown | `30s` |
| `WithLogger(logger Logger)` | Sets a custom logger | Default slog logger |
| `WithBaseContext(ctx context.Context)` | Sets a base context for the server | `context.Background()` |

## Custom Loggers

The package defines a minimal `Logger` interface that is compatible with Go's `slog.Logger`. You can provide your own implementation of this interface using the `WithLogger` option.

```go
// Logger defines a minimal logging interface
type Logger interface {
	// Info logs at LevelInfo.
	Info(msg string, args ...any)

	// Warn logs at LevelWarn.
	Warn(msg string, args ...any)

	// Error logs at LevelError.
	Error(msg string, args ...any)
}
```

## License

MIT