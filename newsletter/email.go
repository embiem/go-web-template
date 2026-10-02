package newsletter

import (
	"fmt"
	"net/mail"
	"strings"
)

// NormalizeEmail canonicalises an address for storage and comparison:
// surrounding whitespace removed, lowercased. The local part of an email is
// technically case-sensitive; in practice every major provider delivers
// case-insensitively, and storing one canonical form is what makes
// uniqueness (and duplicate signups) enforceable.
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ValidateEmail enforces the shape we accept for newsletter signups: a plain
// address without display name or comments, a domain containing a dot, and
// RFC-ish length limits. It deliberately rejects things that parse as valid
// per RFC 5322 but are useless or hostile for a public list (localhost
// domains, quoted local parts, folding whitespace).
func ValidateEmail(s string) error {
	if s == "" {
		return fmt.Errorf("email is empty")
	}
	if len(s) > 254 {
		return fmt.Errorf("email is too long")
	}
	addr, err := mail.ParseAddress(s)
	if err != nil {
		return fmt.Errorf("not a valid email address: %w", err)
	}
	if addr.Name != "" {
		return fmt.Errorf("email must be a plain address without a display name")
	}
	if addr.Address != s {
		// Comments, folding, different capitalisation, angle brackets etc.
		return fmt.Errorf("email must be a plain lowercase address")
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok {
		return fmt.Errorf("email must contain @")
	}
	if local == "" {
		return fmt.Errorf("email local part is empty")
	}
	if len(local) > 64 {
		return fmt.Errorf("email local part is too long")
	}
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") ||
		strings.Contains(domain, "..") {
		return fmt.Errorf("email domain %q is not a public domain", domain)
	}
	return nil
}

// NormalizeAndValidate is the single entry point handlers and the CLI use
// for user-supplied addresses.
func NormalizeAndValidate(s string) (string, error) {
	e := NormalizeEmail(s)
	if err := ValidateEmail(e); err != nil {
		return "", err
	}
	return e, nil
}
