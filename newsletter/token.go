package newsletter

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token design
//
// Unsubscribe links are stateless: token = <subscriber uuid>.<hmac>. The
// secret (NEWSLETTER_SECRET) never touches the database, so a DB leak cannot
// forge unsubscribes, and verifying a link costs one constant-time HMAC — no
// plaintext-secret lookup, no extra table. Consequences, all intended:
//
//   - links in already-delivered mail keep working for the lifetime of the
//     secret (good: unsubscribing from an old mail must work),
//   - a leaked token can only unsubscribe that one address (it grants no
//     read access and cannot confirm or change anything else),
//   - rotating NEWSLETTER_SECRET invalidates all outstanding links at once.
//
// Confirmation links are the opposite: a high-entropy random token whose
// SHA-256 hash (plus expiry) is the only thing stored, so a DB dump cannot
// confirm anyone and the plaintext token exists solely in the mail.

const (
	// unsubScope domain-separates the HMAC from other uses of the secret.
	unsubScope = "newsletter-unsubscribe-v1"
	// ConfirmTokenTTL is how long a double-opt-in confirmation link works.
	ConfirmTokenTTL = 72 * time.Hour
)

// ErrBadToken is returned for malformed, forged or expired tokens.
var ErrBadToken = errors.New("invalid or expired token")

// NewUnsubToken returns the per-recipient unsubscribe token for a
// canonical (8-4-4-4-12) subscriber UUID.
func NewUnsubToken(secret, id string) string {
	mac := hmac.New(sha256.New, []byte(unsubScope+":"+secret))
	mac.Write([]byte(id))
	return id + "." + hex.EncodeToString(mac.Sum(nil)[:16])
}

// ParseUnsubToken verifies an unsubscribe token and returns the subscriber
// UUID it belongs to.
func ParseUnsubToken(secret, token string) (string, error) {
	id, sig, ok := strings.Cut(token, ".") // the UUID half contains no dots
	if !ok || parseUUID(id) == nil || len(sig) != 32 || !isHex(sig) {
		return "", ErrBadToken
	}
	want := NewUnsubToken(secret, id)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want[len(id)+1:])) != 1 {
		return "", ErrBadToken
	}
	return id, nil
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// NewConfirmToken creates a double-opt-in token. Callers store the returned
// hash + expiry and mail the plaintext token away.
func NewConfirmToken(now time.Time) (token, hash string, expires time.Time) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	token = hex.EncodeToString(b[:])
	return token, HashConfirmToken(token), now.Add(ConfirmTokenTTL)
}

// HashConfirmToken is the at-rest form of a confirmation token.
func HashConfirmToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// parseUUID validates the canonical 8-4-4-4-12 form (what pgtype.UUID
// String() emits) and returns the 16 raw bytes.
func parseUUID(s string) []byte {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return nil
	}
	hexPart := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	b := make([]byte, 16)
	if _, err := hex.Decode(b, []byte(hexPart)); err != nil {
		return nil
	}
	return b
}

// MustUUIDBytes converts a canonical UUID string to pgx-compatible bytes,
// for callers that already validated the string.
func MustUUIDBytes(id string) [16]byte {
	var out [16]byte
	b := parseUUID(id)
	if b == nil {
		panic(fmt.Sprintf("not a canonical uuid: %q", id))
	}
	copy(out[:], b)
	return out
}
