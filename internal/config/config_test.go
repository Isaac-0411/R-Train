package config

import (
	"testing"
	"time"
)

func TestSessionTimeoutDefaultsToThirtyMinutes(t *testing.T) {
	t.Setenv("SESSION_TTL", "")

	cfg := Load()
	if cfg.SessionTTL != 30*time.Minute {
		t.Errorf("SessionTTL = %v, want 30m", cfg.SessionTTL)
	}
}

func TestSessionTimeoutIsReadFromTheEnvironment(t *testing.T) {
	t.Setenv("SESSION_TTL", "2h")

	cfg := Load()
	if cfg.SessionTTL != 2*time.Hour {
		t.Errorf("SessionTTL = %v, want 2h", cfg.SessionTTL)
	}
}

func TestSessionTimeoutHasAMinimum(t *testing.T) {
	t.Setenv("SESSION_TTL", "5s")

	cfg := Load()
	if cfg.SessionTTL != time.Minute {
		t.Errorf("SessionTTL = %v, want 1m", cfg.SessionTTL)
	}
}

func TestPortIsUsedWhenAddrIsEmpty(t *testing.T) {
	t.Setenv("ADDR", "")
	t.Setenv("PORT", "3000")

	cfg := Load()
	if cfg.Addr != ":3000" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":3000")
	}
}

func TestAddrWinsOverPort(t *testing.T) {
	t.Setenv("ADDR", "127.0.0.1:8080")
	t.Setenv("PORT", "3000")

	cfg := Load()
	if cfg.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "127.0.0.1:8080")
	}
}

func TestDatabaseURLWinsOverDBPath(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@host/db?sslmode=require")
	t.Setenv("DB_PATH", "data/app.db")

	cfg := Load()
	if cfg.DSN() != "postgres://user:pass@host/db?sslmode=require" {
		t.Errorf("DSN() = %q, want the DATABASE_URL", cfg.DSN())
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestSQLitePathIsTheDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_PATH", "data/app.db")

	cfg := Load()
	if cfg.DSN() != "data/app.db" {
		t.Errorf("DSN() = %q, want %q", cfg.DSN(), "data/app.db")
	}
}

func TestDatabaseURLMustBePostgres(t *testing.T) {
	t.Setenv("DATABASE_URL", "mysql://user:pass@host/db")

	cfg := Load()
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() = nil, want an error for a non-PostgreSQL DATABASE_URL")
	}
}
