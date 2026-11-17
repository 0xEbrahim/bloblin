package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"

	"bloblin/internal/api/native"
	"bloblin/internal/config"
)

type App struct {
	cfg    config.Config
	logger *slog.Logger
	ready  atomic.Bool
	server *http.Server
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	a := &App{cfg: cfg, logger: logger}
	a.server = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           native.NewHandler(logger, a.ready.Load),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	return a, nil
}

// Run starts the application and drains active requests when ctx is canceled.
// An App owns its server and may only be run once.
func (a *App) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(a.cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("prepare data directory %q (BLOBLIN_DATA_DIR): %w", a.cfg.DataDir, err)
	}
	listener, err := net.Listen("tcp", a.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %q (BLOBLIN_LISTEN_ADDR): %w", a.cfg.ListenAddr, err)
	}
	return a.serve(ctx, listener)
}

func (a *App) serve(ctx context.Context, listener net.Listener) error {
	serveErr := make(chan error, 1)
	// All startup work is complete before the first request can be dispatched.
	a.ready.Store(true)
	defer a.ready.Store(false)
	go func() { serveErr <- a.server.Serve(listener) }()
	a.logger.Info("server listening", "address", listener.Addr().String(), "data_dir", a.cfg.DataDir)

	select {
	case err := <-serveErr:
		a.ready.Store(false)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.Join(fmt.Errorf("serve HTTP: %w", err), a.server.Close())
	case <-ctx.Done():
		a.ready.Store(false)
		a.logger.Info("server shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		// Shutdown leaves active connections open on timeout; Close releases them.
		closeErr := a.server.Close()
		return errors.Join(fmt.Errorf("graceful shutdown: %w", err), closeErr)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	a.logger.Info("server stopped")
	return nil
}
