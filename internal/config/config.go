// Package config loads runtime configuration from the environment.
//
// Everything has a sane default so the app boots with no configuration at
// all, but every value can be overridden with an environment variable which
// makes container deployments straightforward.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the runtime configuration of the application.
type Config struct {
	// Addr is the listen address, e.g. ":8080".
	Addr string
	// DBPath is the path of the SQLite database file.
	DBPath string
	// CookieSecure marks session cookies as Secure (required behind HTTPS).
	CookieSecure bool
	// SessionTTL is the idle timeout of a session: the user is signed out
	// after this long without a request. Every request starts the clock
	// again.
	SessionTTL time.Duration
	// SessionCookieName is the name of the session cookie.
	SessionCookieName string
	// BaseURL is the externally visible base URL, used for absolute links.
	BaseURL string
	// AdminUsername, AdminPassword and AdminDisplayName bootstrap the root
	// administrator on an empty database. They are only used when no root
	// admin exists yet, so restarts never overwrite an existing account.
	AdminUsername    string
	AdminPassword    string
	AdminDisplayName string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// DevMode relaxes some caching headers for local development.
	DevMode bool
}

// Load reads the configuration from the environment.
func Load() Config {
	cfg := Config{
		Addr:              env("ADDR", ":8080"),
		DBPath:            env("DB_PATH", "data/app.db"),
		CookieSecure:      envBool("COOKIE_SECURE", false),
		SessionTTL:        envDuration("SESSION_TTL", 30*time.Minute),
		SessionCookieName: env("SESSION_COOKIE_NAME", "rt_session"),
		BaseURL:           strings.TrimRight(env("BASE_URL", ""), "/"),
		AdminUsername:     env("ADMIN_USERNAME", ""),
		AdminPassword:     env("ADMIN_PASSWORD", ""),
		AdminDisplayName:  env("ADMIN_DISPLAY_NAME", "Administrator"),
		LogLevel:          strings.ToLower(env("LOG_LEVEL", "info")),
		DevMode:           envBool("DEV_MODE", false),
	}

	if cfg.SessionTTL < time.Minute {
		cfg.SessionTTL = time.Minute
	}
	return cfg
}

// HasBootstrapAdmin reports whether an environment bootstrapped admin is configured.
func (c Config) HasBootstrapAdmin() bool {
	return c.AdminUsername != "" && c.AdminPassword != ""
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return parsed
}

// Validate checks the configuration for obvious mistakes.
func (c Config) Validate() error {
	if c.DBPath == "" {
		return fmt.Errorf("config: DB_PATH must not be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: unknown LOG_LEVEL %q", c.LogLevel)
	}
	return nil
}
