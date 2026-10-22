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
