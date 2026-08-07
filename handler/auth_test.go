package handler

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestValidateCredentials(t *testing.T) {
	tests := []struct {
		name        string
		username    string
		password    string
		wantName    bool
		wantPasswrd bool
	}{
		{"valid", "alice", "correct horse", false, false},
		{"empty username", "", "correct horse", true, false},
		{"empty password", "alice", "", false, true},
		{"both empty", "", "", true, true},
		{"username too long", strings.Repeat("a", MaxUsernameLen+1), "correct horse", true, false},
		{"username at limit", strings.Repeat("a", MaxUsernameLen), "correct horse", false, false},
		{"username with space", "al ice", "correct horse", true, false},
		{"username with newline", "alice\n", "correct horse", true, false},
		{"password too short", "alice", strings.Repeat("x", MinPasswordLen-1), false, true},
		{"password at min", "alice", strings.Repeat("x", MinPasswordLen), false, false},
		// bcrypt ignores bytes past 72, so a longer one must be rejected
		// instead of silently truncated.
		{"password too long", "alice", strings.Repeat("x", MaxPasswordLen+1), false, true},
		{"password at max", "alice", strings.Repeat("x", MaxPasswordLen), false, false},
		// Multi-byte runes: 8 characters is long enough, 73 bytes is not.
		{"short in bytes but 8 runes", "alice", "äöüäöüäö", false, false},
		{"25 runes over 72 bytes", "alice", strings.Repeat("😀", 19), false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nameErr, passwordErr := validateCredentials(tt.username, tt.password)
			if (nameErr != "") != tt.wantName {
				t.Errorf("nameErr = %q, want error: %v", nameErr, tt.wantName)
			}
			if (passwordErr != "") != tt.wantPasswrd {
				t.Errorf("passwordErr = %q, want error: %v", passwordErr, tt.wantPasswrd)
			}
		})
	}
}

// dummyHash must be a real bcrypt hash at DefaultCost, otherwise the unknown-user
// login path returns early and leaks account existence through response time.
func TestDummyHashIsComparable(t *testing.T) {
	cost, err := bcrypt.Cost(dummyHash)
	if err != nil {
		t.Fatalf("dummyHash is not a bcrypt hash: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Errorf("dummyHash cost = %d, want %d", cost, bcrypt.DefaultCost)
	}
	if err := bcrypt.CompareHashAndPassword(dummyHash, []byte("anything")); err == nil {
		t.Error("dummyHash matched a password, it must never match")
	}
}
