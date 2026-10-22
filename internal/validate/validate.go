// Package validate provides server-side input validation helpers.
//
// Every handler validates untrusted input with these helpers before it
// reaches the store. The same rules are mirrored in web/static/app.js so
// the browser can give instant feedback, but the browser is never trusted.
package validate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Errors maps a form field name to a human readable error message.
type Errors map[string]string

// Add records an error for the given field.
func (e Errors) Add(field, message string) { e[field] = message }

// Any reports whether at least one validation error was recorded.
func (e Errors) Any() bool { return len(e) > 0 }

// Field returns the error message for a field, or "" when it is valid.
func (e Errors) Field(field string) string { return e[field] }

// Messages returns every error message, sorted by field name.
func (e Errors) Messages() []string {
	fields := make([]string, 0, len(e))
	for f := range e {
		fields = append(fields, f)
	}
	// Stable ordering keeps the UI deterministic.
	for i := 0; i < len(fields); i++ {
		for j := i + 1; j < len(fields); j++ {
			if fields[j] < fields[i] {
				fields[i], fields[j] = fields[j], fields[i]
			}
		}
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, e[f])
	}
	return out
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

const (
	// MinPasswordLength is the shortest password we accept.
	MinPasswordLength = 8
	// MaxPasswordLength protects against hashing DoS with huge inputs.
	MaxPasswordLength = 128
	// MaxUsernameLength keeps usernames displayable.
	MaxUsernameLength = 32
	// MaxDisplayNameLength keeps display names displayable.
	MaxDisplayNameLength = 64
	// MaxTitleLength is the longest lesson title we accept.
	MaxTitleLength = 120
	// MaxPassageLength is the longest reading passage we accept.
	MaxPassageLength = 20000
	// MaxPromptLength is the longest question prompt we accept.
	MaxPromptLength = 1000
	// MaxOptionLength is the longest answer option we accept.
	MaxOptionLength = 500
)

// Username validates a login username. It returns an error message or "".
func Username(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "Username is required."
	}
	if !utf8.ValidString(v) {
		return "Username contains invalid characters."
	}
	if len(v) < 3 {
		return "Username must be at least 3 characters long."
	}
	if len(v) > MaxUsernameLength {
		return fmt.Sprintf("Username must be at most %d characters long.", MaxUsernameLength)
	}
	if !usernameRe.MatchString(v) {
		return "Username may only contain letters, numbers, dots, dashes and underscores."
	}
	return ""
}

// Password validates a plain text password. It returns an error message or "".
func Password(raw string) string {
	if raw == "" {
		return "Password is required."
	}
	if len(raw) < MinPasswordLength {
		return fmt.Sprintf("Password must be at least %d characters long.", MinPasswordLength)
	}
	if len(raw) > MaxPasswordLength {
		return fmt.Sprintf("Password must be at most %d characters long.", MaxPasswordLength)
	}
	return ""
}

// PasswordConfirmation checks that the two password fields match.
func PasswordConfirmation(password, confirmation string) string {
	if confirmation == "" {
		return "Please confirm your password."
	}
	if password != confirmation {
		return "Passwords do not match."
	}
	return ""
}

// DisplayName validates an optional display name.
func DisplayName(raw string) string {
	v := strings.TrimSpace(raw)
	if utf8.RuneCountInString(v) > MaxDisplayNameLength {
		return fmt.Sprintf("Display name must be at most %d characters long.", MaxDisplayNameLength)
	}
	return ""
}

// Title validates a lesson title.
func Title(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "Title is required."
	}
	if utf8.RuneCountInString(v) > MaxTitleLength {
		return fmt.Sprintf("Title must be at most %d characters long.", MaxTitleLength)
	}
	return ""
}

// Passage validates a reading passage (the Myanmar text).
func Passage(raw string) string {
	if utf8.RuneCountInString(raw) > MaxPassageLength {
		return fmt.Sprintf("The text must be at most %d characters long.", MaxPassageLength)
	}
	return ""
}

// Prompt validates a question prompt.
func Prompt(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "Question text is required."
	}
	if utf8.RuneCountInString(v) > MaxPromptLength {
		return fmt.Sprintf("Question must be at most %d characters long.", MaxPromptLength)
	}
	return ""
}

// Option validates a single answer option.
func Option(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "Every answer option is required."
	}
	if utf8.RuneCountInString(v) > MaxOptionLength {
		return fmt.Sprintf("Answer option must be at most %d characters long.", MaxOptionLength)
	}
	return ""
}

// Role validates an account role.
func Role(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case RoleAdmin, RoleUser:
		return ""
	default:
		return "Unknown account type."
	}
}

// Account role names.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// New returns an empty error set.
func New() Errors { return Errors{} }
