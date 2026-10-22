package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, algorithm+"$") {
		t.Errorf("hash %q does not carry the algorithm prefix", hash)
	}
	if !VerifyPassword("correct horse battery staple", hash) {
		t.Error("VerifyPassword rejected the right password")
	}
	if VerifyPassword("wrong password", hash) {
		t.Error("VerifyPassword accepted the wrong password")
	}
}

func TestHashIsSalted(t *testing.T) {
	first, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Error("two hashes of the same password must differ (no salt)")
	}
}

func TestVerifyPasswordRejectsBrokenHashes(t *testing.T) {
	broken := []string{
		"",
		"plaintext",
		"pbkdf2-sha256$notanumber$c2FsdA==$aGFzaA==",
		"pbkdf2-sha256$1000$!!!notbase64!!!$aGFzaA==",
		"bcrypt$1000$c2FsdA==$aGFzaA==",
		"pbkdf2-sha256$1000$c2FsdA==",
	}
	for _, hash := range broken {
		if VerifyPassword("anything", hash) {
			t.Errorf("VerifyPassword accepted the broken hash %q", hash)
		}
	}
}

func TestRandomToken(t *testing.T) {
	first, err := RandomToken(32)
	if err != nil {
		t.Fatalf("RandomToken: %v", err)
	}
	second, err := RandomToken(32)
	if err != nil {
		t.Fatalf("RandomToken: %v", err)
	}
	if first == second {
		t.Error("two random tokens must differ")
	}
	if len(first) < 40 {
		t.Errorf("token %q is too short", first)
	}
}
