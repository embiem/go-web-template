package newsletter

import (
	"strings"
	"testing"
)

func env(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(env(map[string]string{
		"BASE_URL":            "http://localhost:3100/",
		"NEWSLETTER_SECRET":   "s3cret",
		"MAIL_FROM":           "Gems <gems@example.com>",
		"MAIL_POSTAL_ADDRESS": "1 Gem Way",
	}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if c.BaseURL != "http://localhost:3100" { // trailing slash trimmed
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
	if c.SMTPHost != "localhost" || c.SMTPPort != 1025 || c.SMTPTLS != "starttls" || c.RatePerSec != 10 {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if got := c.UnsubURL("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"); !strings.HasPrefix(got, "http://localhost:3100/newsletter/unsubscribe/0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001.") {
		t.Fatalf("UnsubURL = %q", got)
	}
	if _, err := ParseUnsubToken(c.Secret, "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"+c.UnsubURL("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001")[len(c.BaseURL+"/newsletter/unsubscribe/0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"):]); err != nil {
		t.Fatalf("UnsubURL token does not verify: %v", err)
	}
	if got := c.GameURL("hades"); got != "http://localhost:3100/games/hades" {
		t.Fatalf("GameURL = %q", got)
	}
	if got := c.ListID(); got != "Indie Game Gems <newsletter.example.com>" {
		t.Fatalf("ListID = %q", got)
	}
}

func TestLoadConfigRequired(t *testing.T) {
	_, err := LoadConfig(env(map[string]string{}))
	if err == nil {
		t.Fatal("missing env accepted")
	}
	for _, want := range []string{"BASE_URL", "NEWSLETTER_SECRET", "MAIL_FROM", "MAIL_POSTAL_ADDRESS"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestLoadConfigValidation(t *testing.T) {
	base := map[string]string{
		"BASE_URL": "http://x", "NEWSLETTER_SECRET": "s", "MAIL_FROM": "a@b.co",
		"MAIL_POSTAL_ADDRESS": "p",
	}
	bad := map[string]string{"SMTP_PORT": "http", "SMTP_TLS": "sometimes", "SMTP_RATE_PER_SEC": "0"}
	for k, v := range bad {
		pairs := map[string]string{"BASE_URL": "http://x", "NEWSLETTER_SECRET": "s", "MAIL_FROM": "a@b.co", "MAIL_POSTAL_ADDRESS": "p", k: v}
		if _, err := LoadConfig(env(pairs)); err == nil {
			t.Fatalf("%s=%q accepted", k, v)
		}
	}
	_ = base
}

func TestValidateForSending(t *testing.T) {
	base := func(secret string) Config {
		return Config{BaseURL: "https://gems.test", Secret: secret,
			From: "gems@gems.test", PostalAddress: "1 Gem Way",
			SMTPHost: "localhost", SMTPPort: 1025, SMTPTLS: "none"}
	}
	t.Run("dev is always allowed", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		c := base("dev-secret-change-me")
		if err := c.ValidateForSending(); err != nil {
			t.Fatalf("dev send refused: %v", err)
		}
	})
	t.Run("production strong secret ok", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		c := base("0f3a9c1e77b24d6a8e5c4f9012ab34cd56ef78901234567890abcdef12345678")
		if err := c.ValidateForSending(); err != nil {
			t.Fatalf("strong secret refused: %v", err)
		}
	})
	for name, secret := range map[string]string{
		"missing":         "",
		"too short":       "short-secret",
		"example default": "dev-secret-change-me",
		"second default":  "dev-only-secret-6f8d3a2c9b17e450aa32",
	} {
		t.Run("production rejects "+name, func(t *testing.T) {
			t.Setenv("APP_ENV", "production")
			err := base(secret).ValidateForSending()
			if err == nil {
				t.Fatalf("%s secret accepted for production send", name)
			}
		})
	}
}
