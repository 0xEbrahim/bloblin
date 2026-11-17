package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bloblin/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		ListenAddr:        "127.0.0.1:0",
		DataDir:           t.TempDir(),
		LogLevel:          slog.LevelInfo,
		ReadHeaderTimeout: time.Second,
		IdleTimeout:       time.Second,
		ShutdownTimeout:   2 * time.Second,
	}
}

func testApp(t *testing.T, cfg config.Config) *App {
	t.Helper()
	a, err := New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.server.Close() })
	return a
}

func awaitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for completion")
		return nil
	}
}

func TestShutdownWaitsForActiveRequest(t *testing.T) {
	a := testApp(t, testConfig(t))
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(finish)
	a.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("finished"))
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdownStarted := make(chan struct{})
	a.server.RegisterOnShutdown(func() { close(shutdownStarted) })
	serverDone := make(chan error, 1)
	go func() { serverDone <- a.serve(ctx, listener) }()

	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			requestDone <- err
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err == nil && (response.StatusCode != http.StatusOK || string(body) != "finished") {
			err = fmt.Errorf("unexpected response: %d %q", response.StatusCode, body)
		}
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	if !a.ready.Load() {
		t.Fatal("server was not ready while accepting requests")
	}
	cancel()
	select {
	case <-shutdownStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not start")
	}
	select {
	case err := <-serverDone:
		t.Fatalf("server stopped before active request completed: %v", err)
	default:
	}
	if a.ready.Load() {
		t.Fatal("server remained ready during shutdown")
	}
	finish()
	if err := awaitResult(t, requestDone); err != nil {
		t.Fatal(err)
	}
	if err := awaitResult(t, serverDone); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDeadlineClosesActiveConnection(t *testing.T) {
	cfg := testConfig(t)
	cfg.ShutdownTimeout = 20 * time.Millisecond
	a := testApp(t, cfg)
	started := make(chan struct{})
	canceled := make(chan struct{})
	a.server.Handler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- a.serve(ctx, listener) }()
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if response != nil {
			response.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	if err := awaitResult(t, serverDone); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected shutdown deadline error, got %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("active request context was not canceled after forced close")
	}
	if err := awaitResult(t, requestDone); err == nil {
		t.Fatal("client unexpectedly received a complete response")
	}
	if a.ready.Load() {
		t.Fatal("server remained ready after shutdown deadline")
	}
}

func TestReadinessLifecycle(t *testing.T) {
	a := testApp(t, testConfig(t))
	checkReady := func(want int) {
		t.Helper()
		response := httptest.NewRecorder()
		a.server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if response.Code != want {
			t.Fatalf("readiness status = %d, want %d", response.Code, want)
		}
	}
	checkReady(http.StatusServiceUnavailable)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- a.serve(ctx, listener) }()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("running readiness status = %d", response.StatusCode)
	}
	cancel()
	if err := awaitResult(t, serverDone); err != nil {
		t.Fatal(err)
	}
	checkReady(http.StatusServiceUnavailable)
}

func TestStartupFailures(t *testing.T) {
	t.Run("data directory is a file", func(t *testing.T) {
		cfg := testConfig(t)
		cfg.DataDir = filepath.Join(cfg.DataDir, "file")
		if err := os.WriteFile(cfg.DataDir, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		a := testApp(t, cfg)
		if err := a.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "BLOBLIN_DATA_DIR") {
			t.Fatalf("expected actionable directory error, got %v", err)
		}
	})
	t.Run("address already in use", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		cfg := testConfig(t)
		cfg.ListenAddr = listener.Addr().String()
		a := testApp(t, cfg)
		if err := a.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "BLOBLIN_LISTEN_ADDR") {
			t.Fatalf("expected actionable listen error, got %v", err)
		}
		if a.ready.Load() {
			t.Fatal("failed startup marked ready")
		}
	})
	t.Run("constructor validation", func(t *testing.T) {
		if _, err := New(config.Config{}, slog.Default()); err == nil {
			t.Fatal("invalid configuration accepted")
		}
		if _, err := New(testConfig(t), nil); err == nil {
			t.Fatal("nil logger accepted")
		}
	})
}
