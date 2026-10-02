package newsletter

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/embiem/indie-game-gems/mail"
)

// Config carries every env-driven setting the newsletter needs. Load it once
// (CLI PersistentPreRunE / main.go) and pass it down; nothing in this package
// reads the environment itself outside LoadConfig.
type Config struct {
	// BaseURL is the public origin of the site (e.g. https://indiegamegems.example).
	// All links and image URLs in mails are absolute against it.
	BaseURL string
	// Secret keys the HMAC in unsubscribe links. Rotating it invalidates all
	// outstanding unsubscribe links (they must re-signup to stop mail — do
	// not rotate casually).
	Secret string
	// From is the RFC 5322 envelope/display sender, e.g.
	// "Indie Game Gems <gems@indiegamegems.example>". Exactly one address.
	From string
	// ReplyTo is optional; empty = replies go to From.
	ReplyTo string
	// PostalAddress is the physical address required in bulk mail footers
	// (CAN-SPAM / GDPR imprint).
	PostalAddress string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	// SMTPTLS is one of "none", "starttls" (default), "tls" (implicit TLS,
	// port 465 style).
	SMTPTLS string
	// RatePerSec caps outbound mails per second to stay polite with the
	// upstream relay.
	RatePerSec int
}

// LoadConfig reads the environment. It fails when required values are
// missing rather than defaulting to something that would silently mis-send.
func LoadConfig(getenv func(string) string) (Config, error) {
	var c Config
	var errs []error
	req := func(key string) string {
		v := strings.TrimSpace(getenv(key))
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", key))
		}
		return v
	}
	c.BaseURL = strings.TrimRight(req("BASE_URL"), "/")
	c.Secret = req("NEWSLETTER_SECRET")
	c.From = req("MAIL_FROM")
	c.PostalAddress = req("MAIL_POSTAL_ADDRESS")
	c.ReplyTo = getenv("MAIL_REPLY_TO")

	c.SMTPHost = getenv("SMTP_HOST")
	if c.SMTPHost == "" {
		c.SMTPHost = "localhost"
	}
	c.SMTPPort = 1025
	if v := getenv("SMTP_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			errs = append(errs, fmt.Errorf("SMTP_PORT %q is not a valid port", v))
		} else {
			c.SMTPPort = p
		}
	}
	c.SMTPUser = getenv("SMTP_USERNAME")
	c.SMTPPass = getenv("SMTP_PASSWORD")
	c.SMTPTLS = getenv("SMTP_TLS")
	switch c.SMTPTLS {
	case "":
		c.SMTPTLS = "starttls"
	case "none", "starttls", "tls":
	default:
		errs = append(errs, fmt.Errorf("SMTP_TLS must be one of none|starttls|tls, got %q", c.SMTPTLS))
	}
	c.RatePerSec = 10
	if v := getenv("SMTP_RATE_PER_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("SMTP_RATE_PER_SEC %q is not a positive integer", v))
		} else {
			c.RatePerSec = n
		}
	}
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("newsletter config: %w", joinErrors(errs))
	}
	return c, nil
}

// LoadConfigEnv is LoadConfig over the process environment.
func LoadConfigEnv() (Config, error) { return LoadConfig(os.Getenv) }

// devSecretDefaults are the placeholder values shipped in .env.example (and
// old checkouts) for local Mailpit setups. Refusing them in production
// keeps an unrotated example value from signing unsubscribe links people
// can forge (NEWSLETTER_SECRET is the HMAC key; unsubscribe is one-click).
var devSecretDefaults = []string{
	"dev-secret-change-me",
	"dev-only-secret-6f8d3a2c9b17e450aa32",
}

// ValidateForSending refuses to mail real people with an unsafe setup.
// Local development (APP_ENV != production, i.e. Mailpit) only needs the
// config to load at all; a production run requires a real secret because
// unsubscribe links are bearer tokens HMAC-signed with it.
//
// Call it from every command that sends mail (send, test).
func (c Config) ValidateForSending() error {
	if os.Getenv("APP_ENV") != "production" {
		return nil
	}
	if c.Secret == "" {
		return errors.New("NEWSLETTER_SECRET is required to send mail in production")
	}
	if len(c.Secret) < 32 {
		return errors.New("NEWSLETTER_SECRET is too short: use >=32 random bytes (e.g. `openssl rand -hex 32`)")
	}
	for _, dev := range devSecretDefaults {
		if subtle.ConstantTimeCompare([]byte(c.Secret), []byte(dev)) == 1 {
			return errors.New("NEWSLETTER_SECRET still has the example development value — rotate it before sending in production")
		}
	}
	return nil
}

// MailFromDomain returns the domain of the From address (after the @), used
// for the List-Id header. Best-effort: empty From domain yields "".
func (c Config) MailFromDomain() string {
	_, domain, ok := strings.Cut(c.From, "@")
	if !ok {
		return ""
	}
	return strings.TrimSuffix(domain, ">")
}

// ListID returns a RFC 2919 List-Id header value for the newsletter.
func (c Config) ListID() string {
	if d := c.MailFromDomain(); d != "" {
		return "Indie Game Gems <newsletter." + d + ">"
	}
	return ""
}

// GameURL is the public page of a game.
func (c Config) GameURL(slug string) string { return c.BaseURL + "/games/" + slug }

// UnsubURL is the per-recipient unsubscribe link embedded in every
// newsletter mail and its List-Unsubscribe header.
func (c Config) UnsubURL(id string) string {
	return c.BaseURL + "/newsletter/unsubscribe/" + NewUnsubToken(c.Secret, id)
}

// ConfirmURL is the double-opt-in link mailed to new subscribers.
func (c Config) ConfirmURL(token string) string {
	return c.BaseURL + "/newsletter/confirm?token=" + token
}

// MailConfig maps this config onto the SMTP mailer's settings.
func (c Config) MailConfig() mail.Config {
	return mail.Config{
		Host: c.SMTPHost, Port: c.SMTPPort,
		Username: c.SMTPUser, Password: c.SMTPPass, TLS: c.SMTPTLS,
		From: c.From, ReplyTo: c.ReplyTo,
		ListID: c.ListID(), RatePerSec: c.RatePerSec,
	}
}
