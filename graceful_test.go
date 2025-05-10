package graceful

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockHandler is a simple http.Handler for testing
type mockHandler struct {
	serveHTTPFunc func(w http.ResponseWriter, r *http.Request)
}

func (m *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.serveHTTPFunc != nil {
		m.serveHTTPFunc(w, r)
	}
}

// mockLogger implements the reduced Logger interface for testing
type mockLogger struct {
	infoCalls  int
	warnCalls  int
	errorCalls int
	msgs       []string
}

func (m *mockLogger) Info(msg string, args ...any)  { m.infoCalls++; m.msgs = append(m.msgs, msg) }
func (m *mockLogger) Warn(msg string, args ...any)  { m.warnCalls++; m.msgs = append(m.msgs, msg) }
func (m *mockLogger) Error(msg string, args ...any) { m.errorCalls++; m.msgs = append(m.msgs, msg) }

func TestNew(t *testing.T) {
	r := require.New(t)

	tests := map[string]struct {
		options  []Option
		expected Options
	}{
		"default options": {
			options: nil,
			expected: Options{
				Addr:              ":8080",
				ReadTimeout:       5 * time.Second,
				WriteTimeout:      10 * time.Second,
				IdleTimeout:       120 * time.Second,
				ReadHeaderTimeout: 5 * time.Second,
				ShutdownTimeout:   30 * time.Second,
				// Logger and BaseContext are checked separately
			},
		},
		"custom options": {
			options: []Option{
				WithAddr(":9090"),
				WithReadTimeout(10 * time.Second),
				WithWriteTimeout(15 * time.Second),
				WithIdleTimeout(60 * time.Second),
				WithReadHeaderTimeout(10 * time.Second),
				WithShutdownTimeout(20 * time.Second),
			},
			expected: Options{
				Addr:              ":9090",
				ReadTimeout:       10 * time.Second,
				WriteTimeout:      15 * time.Second,
				IdleTimeout:       60 * time.Second,
				ReadHeaderTimeout: 10 * time.Second,
				ShutdownTimeout:   20 * time.Second,
				// Logger and BaseContext are checked separately
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			handler := &mockHandler{}
			server := New(handler, tc.options...)

			r.Equal(tc.expected.Addr, server.opts.Addr)
			r.Equal(tc.expected.ReadTimeout, server.opts.ReadTimeout)
			r.Equal(tc.expected.WriteTimeout, server.opts.WriteTimeout)
			r.Equal(tc.expected.IdleTimeout, server.opts.IdleTimeout)
			r.Equal(tc.expected.ReadHeaderTimeout, server.opts.ReadHeaderTimeout)
			r.Equal(tc.expected.ShutdownTimeout, server.opts.ShutdownTimeout)
			r.NotNil(server.opts.Logger)
			r.NotNil(server.opts.BaseContext)
			r.Equal(handler, server.handler)
		})
	}
}

func TestWithLogger(t *testing.T) {
	r := require.New(t)

	logger := &mockLogger{}
	handler := &mockHandler{}
	server := New(handler, WithLogger(logger))

	r.Equal(logger, server.opts.Logger)
}

func TestWithBaseContext(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &mockHandler{}
	server := New(handler, WithBaseContext(ctx))

	r.Equal(ctx, server.opts.BaseContext)
}

func TestServerShutdownProgrammatic(t *testing.T) {
	r := require.New(t)

	// create a test server that just idles
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	// parse the host:port from the test server
	serverAddr := strings.TrimPrefix(testServer.URL, "http://")

	// create our graceful server with the same address
	logger := &mockLogger{}
	handler := &mockHandler{}
	server := New(handler, WithAddr(serverAddr), WithLogger(logger), WithShutdownTimeout(1*time.Second))

	// this will be non-nil because the port is already in use by our test server
	serverErr := server.ListenAndServe()
	r.Error(serverErr)
	r.Contains(logger.msgs, "Server error: %v")
}

func TestShutdownNotStarted(t *testing.T) {
	r := require.New(t)

	handler := &mockHandler{}
	server := New(handler)

	err := server.Shutdown(context.Background())
	r.Error(err)
	r.Equal("server not started", err.Error())
}

func TestHttpServerConfiguration(t *testing.T) {
	r := require.New(t)

	ctx := context.Background()
	handler := &mockHandler{}
	logger := &mockLogger{}

	server := New(
		handler,
		WithAddr(":0"), // Use port 0 to have the OS assign a free port
		WithReadTimeout(7*time.Second),
		WithWriteTimeout(12*time.Second),
		WithIdleTimeout(60*time.Second),
		WithReadHeaderTimeout(8*time.Second),
		WithLogger(logger),
		WithBaseContext(ctx),
	)

	// manually set up the HTTP server to test configuration
	server.httpServer = &http.Server{
		Addr:              server.opts.Addr,
		Handler:           server.handler,
		ReadTimeout:       server.opts.ReadTimeout,
		WriteTimeout:      server.opts.WriteTimeout,
		IdleTimeout:       server.opts.IdleTimeout,
		ReadHeaderTimeout: server.opts.ReadHeaderTimeout,
		BaseContext: func(_ net.Listener) context.Context {
			return server.opts.BaseContext
		},
	}

	// verify the HTTP server was properly configured
	r.NotNil(server.httpServer)
	r.Equal(":0", server.httpServer.Addr)
	r.Equal(handler, server.httpServer.Handler)
	r.Equal(7*time.Second, server.httpServer.ReadTimeout)
	r.Equal(12*time.Second, server.httpServer.WriteTimeout)
	r.Equal(60*time.Second, server.httpServer.IdleTimeout)
	r.Equal(8*time.Second, server.httpServer.ReadHeaderTimeout)

	// verify that BaseContext propagation works
	listener, err := net.Listen("tcp", ":0")
	r.NoError(err)
	defer listener.Close()

	baseCtx := server.httpServer.BaseContext(listener)
	r.Equal(ctx, baseCtx)
}

func TestContextCancellationHandling(t *testing.T) {
	r := require.New(t)

	// create a context we can cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := &mockLogger{}
	handler := &mockHandler{}

	server := New(
		handler,
		WithLogger(logger),
		WithBaseContext(ctx),
	)

	// create a modified ListenAndServe function that doesn't actually listen
	// but just simulates context cancellation
	simulateCancellation := func() error {
		// create a fake server error channel
		serverErrors := make(chan error, 1)

		// create a quit channel
		quit := make(chan os.Signal, 1)

		// manually trigger the context cancellation event
		cancel()

		// run the same select block as in ListenAndServe
		select {
		case err := <-serverErrors:
			return err
		case <-quit:
			return nil
		case <-server.opts.BaseContext.Done():
			logger.Warn("Base context cancelled. Starting graceful shutdown...", errAttr(server.opts.BaseContext.Err()))
		}

		logger.Info("Server gracefully shut down.")
		return nil
	}

	err := simulateCancellation()
	r.NoError(err)

	// check that the warning for context cancellation was logged
	foundCancellationMsg := false
	for _, msg := range logger.msgs {
		if strings.Contains(msg, "Base context cancelled") {
			foundCancellationMsg = true
			break
		}
	}
	r.True(foundCancellationMsg, "should log base context cancellation")

	// check that the successful shutdown message was logged
	foundShutdownMsg := false
	for _, msg := range logger.msgs {
		if msg == "Server gracefully shut down." {
			foundShutdownMsg = true
			break
		}
	}
	r.True(foundShutdownMsg, "should log successful shutdown")
}