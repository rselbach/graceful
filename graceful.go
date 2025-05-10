// Package graceful provides a wrapper for the standard http.Server with graceful
// shutdown capabilities. It handles OS signals and context cancellation
// to ensure that in-flight requests can complete before the server exits.
package graceful

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Options holds the configuration for the server.
type Options struct {
	Addr              string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	Logger            Logger
	// BaseContext can be used to signal a shutdown from an external source,
	// in addition to OS signals.
	BaseContext context.Context
}

// Option is a function type used to configure Server Options.
type Option func(*Options)

// WithAddr sets the server's listening address.
func WithAddr(addr string) Option {
	return func(o *Options) {
		o.Addr = addr
	}
}

// WithReadTimeout sets the server's read timeout.
func WithReadTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.ReadTimeout = timeout
	}
}

// WithWriteTimeout sets the server's write timeout.
func WithWriteTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.WriteTimeout = timeout
	}
}

// WithIdleTimeout sets the server's idle timeout.
func WithIdleTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.IdleTimeout = timeout
	}
}

// WithReadHeaderTimeout sets the server's read header timeout.
func WithReadHeaderTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.ReadHeaderTimeout = timeout
	}
}

// WithShutdownTimeout sets the timeout for graceful shutdown.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.ShutdownTimeout = timeout
	}
}

// WithBaseContext sets a base context for the server.
// If this context is cancelled, the server will begin its shutdown sequence.
func WithBaseContext(ctx context.Context) Option {
	return func(o *Options) {
		o.BaseContext = ctx
	}
}

// WithLogger sets an optional logger.
func WithLogger(log Logger) Option {
	return func(o *Options) {
		o.Logger = log
	}
}

// Server wraps the http.Server and manages its lifecycle.
type Server struct {
	httpServer *http.Server
	opts       Options
	handler    http.Handler
}

// New creates a new Server instance.
// The handler is the http.Handler (e.g., a mux) that will serve requests.
// opts are functional options to customize the server.
func New(handler http.Handler, opts ...Option) *Server {
	options := Options{
		Addr:              ":8080",
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		ShutdownTimeout:   30 * time.Second,
		Logger:            newDefaultLogger(),
		BaseContext:       context.Background(), // Default to a non-cancelable context
	}

	for _, opt := range opts {
		opt(&options)
	}

	return &Server{
		handler: handler,
		opts:    options,
	}
}

// ListenAndServe starts the HTTP server and blocks until the server is shutdown
// or an error occurs. It handles OS signals for graceful termination.
func (s *Server) ListenAndServe() error {
	s.httpServer = &http.Server{
		Addr:              s.opts.Addr,
		Handler:           s.handler,
		ReadTimeout:       s.opts.ReadTimeout,
		WriteTimeout:      s.opts.WriteTimeout,
		IdleTimeout:       s.opts.IdleTimeout,
		ReadHeaderTimeout: s.opts.ReadHeaderTimeout,
		BaseContext: func(_ net.Listener) context.Context { // Propagate base context for per-request contexts
			return s.opts.BaseContext
		},
	}

	// Channel to listen for errors from the HTTP server goroutine
	serverErrors := make(chan error, 1)

	// Start the server in a new goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- fmt.Errorf("error listening and serving: %w", err)
		} else if errors.Is(err, http.ErrServerClosed) {
			s.opts.Logger.Info("Server closed.")
		}
	}()

	// channel to listen for OS signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// block until we receive a signal, a server error, or the base context is done
	select {
	case err := <-serverErrors:
		s.opts.Logger.Error("Server error: %v", err)
		// Attempt a quick shutdown if an unexpected server error occurs
		// Create a short context for this emergency shutdown.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := s.httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
			s.opts.Logger.Error("Emergency shutdown failed", errAttr(shutdownErr))
		}
		return err // Return the original error

	case sig := <-quit:
		s.opts.Logger.Info(fmt.Sprintf("Received OS signal %s. Starting graceful shutdown...", sig))

	case <-s.opts.BaseContext.Done():
		s.opts.Logger.Warn("Base context cancelled. Starting graceful shutdown...", errAttr(s.opts.BaseContext.Err()))
	}

	// create a context with a timeout for the graceful shutdown.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.opts.ShutdownTimeout)
	defer shutdownCancel()

	// attempt to gracefully shut down the server.
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.opts.Logger.Error("Graceful shutdown failed. Forcing close.", errAttr(err))
		// attempt to close the server immediately.
		if closeErr := s.httpServer.Close(); closeErr != nil {
			s.opts.Logger.Error("Failed to close server", errAttr(closeErr))
		}
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	s.opts.Logger.Info("Server gracefully shut down.")
	return nil
}

// Shutdown can be called to programmatically initiate a graceful shutdown.
// This is useful if you want to trigger shutdown from another part of your application,
// not just OS signals or BaseContext cancellation.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return errors.New("server not started")
	}
	s.opts.Logger.Info("Programmatic shutdown initiated.")
	return s.httpServer.Shutdown(ctx)
}
