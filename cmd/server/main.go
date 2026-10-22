// Command server runs the Reading Trainer web application.
//
// Configuration is read from the environment:
//
//	ADDR               listen address (default ":8080")
//	DB_PATH            SQLite database file (default "data/app.db")
//	COOKIE_SECURE      set to true when the site is served over HTTPS
//	SESSION_TTL        session lifetime (default "168h")
//	SESSION_COOKIE_NAME  cookie name (default "rt_session")
//	BASE_URL           public base URL of the site
//	ADMIN_USERNAME     bootstraps the main administrator on an empty database
//	ADMIN_PASSWORD     password of the bootstrapped administrator
//	ADMIN_DISPLAY_NAME display name of the bootstrapped administrator
//	LOG_LEVEL          debug | info | warn | error (default "info")
//	DEV_MODE           relaxes the content security policy for development
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"readingtrainer/internal/auth"
	"readingtrainer/internal/config"
	"readingtrainer/internal/store"
	"readingtrainer/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	logger.Info("database ready", "path", cfg.DBPath)

	if err := bootstrapAdmin(ctx, st, cfg, logger); err != nil {
		return err
	}

	srv, err := web.New(cfg, st, logger)
	if err != nil {
		return err
	}

	go pruneSessions(ctx, st, logger)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-stop:
		logger.Info("shutting down", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

// bootstrapAdmin creates the main administrator when the environment asks
// for it and no such account exists yet. It never touches an existing
// database.
func bootstrapAdmin(ctx context.Context, st *store.Store, cfg config.Config, logger *slog.Logger) error {
	if !cfg.HasBootstrapAdmin() {
		return nil
	}

	hasRoot, err := st.HasRootAdmin(ctx)
	if err != nil {
		return err
	}
	if hasRoot {
		logger.Info("main administrator already exists, skipping the environment bootstrap")
		return nil
	}

	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}

	displayName := cfg.AdminDisplayName
	if displayName == "" {
		displayName = cfg.AdminUsername
	}

	user, err := st.CreateUser(ctx, cfg.AdminUsername, displayName, hash, store.RoleAdmin, true)
	if err != nil {
		return err
	}

	logger.Info("main administrator created", "username", user.Username)
	return nil
}

// pruneSessions removes expired sessions from the database.
func pruneSessions(ctx context.Context, st *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := st.DeleteExpiredSessions(ctx)
			if err != nil {
				logger.Error("prune sessions", "error", err)
				continue
			}
			if removed > 0 {
				logger.Debug("pruned sessions", "count", removed)
			}
		}
	}
}

// newLogger builds the structured logger of the application.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
