package mail

import (
	"context"
	"strings"
	"testing"

	gomail "github.com/wneessen/go-mail"
)

func newTestMailer(t *testing.T) *Mailer {
	t.Helper()
	m, err := New(Config{
		Host: "localhost", Port: 1025, From: "Gems <gems@example.com>", TLS: "none",
		ListID: "Indie Game Gems <newsletter.example.com>",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// TestNewsletterHeaders pins the RFC 8058 one-click unsubscribe pair and the
// other headers Gmail/Yahoo bulk-sender rules expect on bulk mail.
func TestNewsletterHeaders(t *testing.T) {
	m := newTestMailer(t)
	msg := m.base("reader@example.com", "Weekly gems")
	msg.SetGenHeader(gomail.HeaderListUnsubscribe,
		"<https://example.com/newsletter/unsubscribe/tok>")
	msg.SetGenHeader(gomail.HeaderListUnsubscribePost, "List-Unsubscribe=One-Click")
	msg.SetGenHeader(gomail.Header("List-Id"), m.cfg.ListID)

	lu, lup := ListUnsubscribeHeaders(msg)
	// go-mail stores one value per URI. Only the HTTPS one-click endpoint:
	// a mailto fallback would point at a mailbox nobody monitors.
	if len(lu) != 1 ||
		lu[0] != "<https://example.com/newsletter/unsubscribe/tok>" {
		t.Fatalf("List-Unsubscribe = %q, want only the https one-click URI", lu)
	}
	if len(lup) != 1 || lup[0] != "List-Unsubscribe=One-Click" {
		t.Fatalf("List-Unsubscribe-Post = %q, want exactly List-Unsubscribe=One-Click", lup)
	}
	if id := msg.GetGenHeader(gomail.Header("List-Id")); len(id) != 1 || id[0] == "" {
		t.Fatalf("List-Id missing: %q", id)
	}
	if mid := msg.GetMessageID(); !strings.HasPrefix(mid, "<") || !strings.HasSuffix(mid, ">") {
		t.Fatalf("Message-ID = %q, want angle-bracketed id", mid)
	}
}

// TestNewsletterRequiresUnsubURL guards against sending bulk mail without a
// one-click opt-out.
func TestNewsletterRequiresUnsubURL(t *testing.T) {
	m := newTestMailer(t)
	err := m.Newsletter(context.Background(), NewsletterMessage{
		To: "a@example.com", Subject: "s", HTML: "<p>x</p>", Text: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "UnsubURL") {
		t.Fatalf("want UnsubURL error, got %v", err)
	}
}

// TestSetBodyAlternativeOrder pins text-then-html so multipart/alternative
// clients prefer the HTML part.
func TestSetBodyAlternativeOrder(t *testing.T) {
	msg := gomail.NewMsg()
	setBody(msg, "plain words", "<p>html words</p>")
	var parts []gomail.ContentType
	for _, p := range msg.GetParts() {
		parts = append(parts, p.GetContentType())
	}
	if len(parts) != 2 || parts[0] != gomail.TypeTextPlain || parts[1] != gomail.TypeTextHTML {
		t.Fatalf("part order = %v, want [text/plain text/html]", parts)
	}
}
