package validate

import "testing"

func TestUsername(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"too short", "ab", true},
		{"valid", "alice", false},
		{"with dots and dashes", "alice.smith-01_x", false},
		{"with space", "alice smith", true},
		{"with unicode", "အလီစီ", true},
		{"too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Username(tc.input)
			if (got != "") != tc.wantErr {
				t.Fatalf("Username(%q) = %q, wantErr = %v", tc.input, got, tc.wantErr)
			}
		})
	}
}

func TestPassword(t *testing.T) {
	if msg := Password("short"); msg == "" {
		t.Error("Password(\"short\") should fail")
	}
	if msg := Password("longenoughpassword"); msg != "" {
		t.Errorf("Password of a valid length should pass, got %q", msg)
	}
	long := make([]byte, MaxPasswordLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if msg := Password(string(long)); msg == "" {
		t.Error("an oversized password should fail")
	}
}

func TestPasswordConfirmation(t *testing.T) {
	if msg := PasswordConfirmation("secret123", "secret123"); msg != "" {
		t.Errorf("matching passwords should pass, got %q", msg)
	}
	if msg := PasswordConfirmation("secret123", "other"); msg == "" {
		t.Error("different passwords should fail")
	}
	if msg := PasswordConfirmation("secret123", ""); msg == "" {
		t.Error("an empty confirmation should fail")
	}
}

func TestOptionKey(t *testing.T) {
	for _, key := range []string{"a", "b", "c", "d", "A"} {
		if msg := OptionKey(key); msg != "" {
			t.Errorf("OptionKey(%q) should pass, got %q", key, msg)
		}
	}
	for _, key := range []string{"", "e", "1", "ab"} {
		if msg := OptionKey(key); msg == "" {
			t.Errorf("OptionKey(%q) should fail", key)
		}
	}
}

func TestRole(t *testing.T) {
	if msg := Role(RoleAdmin); msg != "" {
		t.Errorf("Role(admin) should pass, got %q", msg)
	}
	if msg := Role(RoleUser); msg != "" {
		t.Errorf("Role(user) should pass, got %q", msg)
	}
	if msg := Role("superuser"); msg == "" {
		t.Error("an unknown role should fail")
	}
}

func TestErrors(t *testing.T) {
	errs := New()
	if errs.Any() {
		t.Error("a fresh error set should be empty")
	}
	errs.Add("username", "bad")
	if !errs.Any() {
		t.Error("the error set should not be empty")
	}
	if got := errs.Field("username"); got != "bad" {
		t.Errorf("Field(username) = %q, want %q", got, "bad")
	}
	if got := errs.Field("missing"); got != "" {
		t.Errorf("Field(missing) = %q, want an empty string", got)
	}
	if len(errs.Messages()) != 1 {
		t.Errorf("Messages() returned %d entries, want 1", len(errs.Messages()))
	}
}
