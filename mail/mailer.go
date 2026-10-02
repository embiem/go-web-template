/*
Package mail sends the newsletter's emails over SMTP using go-mail
(https://github.com/wneessen/go-mail): multipart text+HTML, RFC 8058
one-click unsubscribe headers on bulk mail, Message-ID on everything, and a
send rate limit so a relay is never hammered.

The transport is plain SMTP (env-configured), so pointing it at a
transactional provider's relay later is a config change only.
*/
package mail

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	gomail "github.com/wneessen/go-mail"
)

// Config is the SMTP + identity subset the mailer needs. The newsletter
// package maps its Config onto this; the split keeps this package free of
// env access.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	// TLS is "none", "starttls" (default) or "tls" (implicit TLS, 465-style).
	TLS string
	// From is a single RFC 5322 address, e.g. "Gems <gems@example.com>".
	From string
	// ReplyTo is optional.
	ReplyTo string
	// ListID is the RFC 2919 list identifier set on bulk mail ("" = none).
	ListID string
	// RatePerSec caps sends per second (default 10, min 1).
	RatePerSec int
}

// Mailer sends emails. Safe for concurrent use.
type Mailer struct {
	client *gomail.Client
	cfg    Config

	mu       sync.Mutex
	nextSend time.Time
	interval time.Duration
}

// New builds a Mailer. It validates config but does not dial — the first
// Send connects (Mailpit may not be up when the process starts).
func New(cfg Config) (*Mailer, error) {
	if cfg.From == "" {
		return nil, errors.New("mail: From is required")
	}
	if cfg.Host == "" {
		return nil, errors.New("mail: Host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 1025
	}
	if cfg.TLS == "" {
		cfg.TLS = "starttls"
	}
	if cfg.RatePerSec < 1 {
		cfg.RatePerSec = 10
	}
	opts := []gomail.Option{
		gomail.WithPort(cfg.Port),
		gomail.WithTimeout(30 * time.Second),
	}
	switch cfg.TLS {
	case "none":
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	case "tls": // implicit TLS (port 465)
		opts = append(opts, gomail.WithSSLPort(false))
	default: // starttls
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSOpportunistic))
	}
	if cfg.Username != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(cfg.Username),
			gomail.WithPassword(cfg.Password),
		)
	}
	client, err := gomail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("mail: %w", err)
	}
	return &Mailer{
		client:   client,
		cfg:      cfg,
		interval: time.Second / time.Duration(cfg.RatePerSec),
	}, nil
}

// NewsletterMessage is one bulk issue mail for one recipient.
type NewsletterMessage struct {
	To      string
	Subject string
	HTML    string
	Text    string
	// UnsubURL is the per-recipient unsubscribe link. It goes into the
	// List-Unsubscribe header (RFC 8058 one-click) and the body footer.
	UnsubURL string
}

// Newsletter sends one bulk mail. Requires UnsubURL: bulk mail without a
// one-click opt-out violates Gmail/Yahoo sender rules.
func (m *Mailer) Newsletter(ctx context.Context, n NewsletterMessage) error {
	if n.UnsubURL == "" {
		return errors.New("mail: newsletter without UnsubURL")
	}
	msg := m.base(n.To, n.Subject)
	// RFC 8058 one-click unsubscribe. No mailto fallback: the only address
	// we could put there is a mailbox nobody monitors (or worse, the
	// recipient's own — mailto:<subscriber> just mails them back). The
	// HTTPS one-click endpoint is the supported opt-out.
	msg.SetGenHeader(gomail.HeaderListUnsubscribe, "<"+n.UnsubURL+">")
	msg.SetGenHeader(gomail.HeaderListUnsubscribePost, "List-Unsubscribe=One-Click")
	if m.cfg.ListID != "" {
		msg.SetGenHeader(gomail.Header("List-Id"), m.cfg.ListID)
	}
	msg.SetBulk() // Precedence: bulk, X-Auto-Response-Suppress: All
	setBody(msg, n.Text, n.HTML)
	return m.send(ctx, msg)
}

// Confirmation sends the double-opt-in confirmation mail (transactional: no
// bulk headers).
func (m *Mailer) Confirmation(ctx context.Context, to, subject, html, text string) error {
	msg := m.base(to, subject)
	setBody(msg, text, html)
	return m.send(ctx, msg)
}

func (m *Mailer) base(to, subject string) *gomail.Msg {
	msg := gomail.NewMsg()
	_ = msg.From(m.cfg.From)
	_ = msg.To(to)
	msg.Subject(subject)
	if m.cfg.ReplyTo != "" {
		_ = msg.ReplyTo(m.cfg.ReplyTo)
	}
	msg.SetMessageID()
	return msg
}

func setBody(msg *gomail.Msg, text, html string) {
	// multipart/alternative: text part first, HTML last (clients pick the
	// richest part they understand, so order matters).
	msg.SetBodyString(gomail.TypeTextPlain, text)
	if html != "" {
		msg.AddAlternativeString(gomail.TypeTextHTML, html)
	}
}

func (m *Mailer) send(ctx context.Context, msg *gomail.Msg) error {
	if err := m.throttle(ctx); err != nil {
		return err
	}
	return m.client.DialAndSendWithContext(ctx, msg)
}

// throttle spaces sends at least 1/RatePerSec apart.
func (m *Mailer) throttle(ctx context.Context) error {
	m.mu.Lock()
	now := time.Now()
	wait := m.nextSend.Sub(now)
	if wait > 0 {
		m.nextSend = m.nextSend.Add(m.interval)
	} else {
		m.nextSend = now.Add(m.interval)
	}
	m.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Header exposes the header constants for tests without importing go-mail.
func ListUnsubscribeHeaders(msg *gomail.Msg) (list []string, post []string) {
	return msg.GetGenHeader(gomail.HeaderListUnsubscribe),
		msg.GetGenHeader(gomail.HeaderListUnsubscribePost)
}

// PortString renders host:port for logs.
func (c Config) PortString() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}
