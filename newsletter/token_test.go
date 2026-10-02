package newsletter

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-not-for-production"

func TestUnsubTokenRoundTrip(t *testing.T) {
	id := "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"
	tok := NewUnsubToken(testSecret, id)
	// Shape: <uuid>.<32 hex chars>.
	if !strings.HasPrefix(tok, id+".") || len(tok) != len(id)+1+32 {
		t.Fatalf("token = %q, want <uuid>.<32 hex>", tok)
	}
	got, err := ParseUnsubToken(testSecret, tok)
	if err != nil || got != id {
		t.Fatalf("ParseUnsubToken = %q, %v; want %q", got, err, id)
	}
}

func TestUnsubTokenTamper(t *testing.T) {
	id := "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"
	tok := NewUnsubToken(testSecret, id)
	sig := tok[len(id)+1:]
	tampered := []string{
		id + "." + strings.Repeat("0", len(sig)),      // wrong sig
		id + "." + sig[:len(sig)-1] + "0",             // last byte flipped
		"0199aaaa-bbbb-7ccc-8ddd-eeeeffff0002." + sig, // other id, same sig
		id,                              // no sig
		strings.ToUpper(id) + "." + sig, // wrong id casing
		"id.sig.extra",                  // junk
	}
	for _, tk := range tampered {
		if _, err := ParseUnsubToken(testSecret, tk); !errors.Is(err, ErrBadToken) {
			t.Fatalf("accepted tampered token %q (err=%v)", tk, err)
		}
	}
}

// A token minted with a different secret must not verify — this is what
// makes NEWSLETTER_SECRET rotation a kill switch.
func TestUnsubTokenWrongSecret(t *testing.T) {
	id := "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"
	tok := NewUnsubToken("other-secret", id)
	if _, err := ParseUnsubToken(testSecret, tok); !errors.Is(err, ErrBadToken) {
		t.Fatalf("cross-secret token verified: %v", err)
	}
}

func TestConfirmToken(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tok, hash, exp := NewConfirmToken(now)
	if len(tok) != 64 || hash == tok || len(hash) != 64 {
		t.Fatalf("token/hash shapes wrong: len(tok)=%d hash==tok:%v", len(tok), hash == tok)
	}
	if !exp.Equal(now.Add(ConfirmTokenTTL)) {
		t.Fatalf("expiry = %v, want %v", exp, now.Add(ConfirmTokenTTL))
	}
	if HashConfirmToken(tok) != hash {
		t.Fatal("hash is not reproducible")
	}
	// Two tokens never collide.
	_, hash2, _ := NewConfirmToken(now)
	if hash2 == hash {
		t.Fatal("confirm tokens collide")
	}
}

func TestParseUUID(t *testing.T) {
	if parseUUID("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001") == nil {
		t.Fatal("valid uuid rejected")
	}
	for _, s := range []string{
		"", "0199aaaabbbb7ccc8dddeeeeffff0001", // no dashes
		"0199aaaa-bbbb-7ccc-8ddd-eeeeffff000",  // short
		"0199aaaa_bbbb_7ccc_8ddd_eeeeffff0001", // underscores
		"zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz", // not hex
	} {
		if parseUUID(s) != nil {
			t.Fatalf("accepted %q", s)
		}
	}
	b := MustUUIDBytes("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001")
	if b[0] != 0x01 || b[15] != 0x01 {
		t.Fatalf("bytes wrong: %v", b)
	}
}
