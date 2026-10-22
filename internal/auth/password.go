// Package auth contains password hashing and random token helpers.
//
// Password hashing uses PBKDF2-HMAC-SHA256 from the Go standard library
// (crypto/pbkdf2), so the project needs no third party crypto dependency.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// algorithm is the identifier stored in the encoded hash.
	algorithm = "pbkdf2-sha256"
	// defaultIterations is the PBKDF2 work factor.
	defaultIterations = 210000
	saltLength        = 16
	keyLength         = 32
)

// ErrInvalidHash is returned when an encoded password hash cannot be parsed.
var ErrInvalidHash = errors.New("auth: invalid password hash format")

// HashPassword derives a salted PBKDF2 hash from a plain text password.
//
// The returned string has the format:
//
//	pbkdf2-sha256$<iterations>$<salt-b64>$<key-b64>
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, defaultIterations, keyLength)
	if err != nil {
		return "", fmt.Errorf("auth: derive key: %w", err)
	}
	return fmt.Sprintf("%s$%d$%s$%s",
		algorithm,
		defaultIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded hash.
// The comparison is constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != algorithm {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) == 0 {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// RandomToken returns a URL safe random token of n bytes of entropy.
func RandomToken(n int) (string, error) {
	if n <= 0 {
		n = 32
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
