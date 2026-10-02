package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/embiem/indie-game-gems/db"
	"github.com/embiem/indie-game-gems/mail"
	"github.com/embiem/indie-game-gems/newsletter"
	"github.com/embiem/indie-game-gems/view"
	"github.com/embiem/indie-game-gems/view/email"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// Public newsletter HTTP surface: signup (double opt-in), confirmation,
// one-click unsubscribe and the sent-issue archive.
//
// All flow logic lives in the newsletter package (newsletter.Signup /
// ConfirmSignup / UnsubscribeByToken over small store interfaces); these
// handlers only do HTTP: form parsing, rate limiting, status codes, views.

// nlConfig lazily loads the env config and builds the mailer on first use
// (package-init would run before .env is loaded in main).
type nlConfig struct {
	cfg newsletter.Config
}

var (
	nlOnce sync.Once
	nlSvc  *nlConfig
	nlErr  error
	nlMail *mail.Mailer
)

// newsletterDeps returns the lazily-initialised config + mailer.
func newsletterDeps() (*nlConfig, error) {
	nlOnce.Do(func() {
		cfg, err := newsletter.LoadConfigEnv()
		if err != nil {
			nlErr = err
			return
		}
		mailer, err := mail.New(cfg.MailConfig())
		if err != nil {
			nlErr = err
			return
		}
		nlSvc = &nlConfig{cfg: cfg}
		nlMail = mailer
	})
	if nlErr != nil {
		return nil, fmt.Errorf("newsletter config: %w", nlErr)
	}
	return nlSvc, nil
}

// ---------------------------------------------------------------------------
// GET /newsletter — signup page + sent-issue archive

func GetNewsletterPage(w http.ResponseWriter, r *http.Request) error {
	issues, err := db.Queries.ListSentIssues(r.Context())
	if err != nil {
		return err
	}
	return view.NewsletterPage(view.NewsletterPageData{
		Meta: view.PageMeta{
			Title:       "Newsletter",
			Description: "One great indie game a week in your inbox — the Indie Game Gems newsletter. One-click unsubscribe.",
			Canonical:   "/newsletter",
		},
		Issues: issues,
	}).Render(r.Context(), w)
}

// ---------------------------------------------------------------------------
// POST /newsletter/subscribe

// confirmLimiter caps confirmation mails per address, complementing the
// per-IP httprate limit on the route. In-memory by design: worst case
// across a restart is one extra mail.
var confirmLimiter = newEmailLimiter(15 * time.Minute)

type emailLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
	ttl  time.Duration
	now  func() time.Time
}

func newEmailLimiter(ttl time.Duration) *emailLimiter {
	return &emailLimiter{last: map[string]time.Time{}, ttl: ttl, now: time.Now}
}

// allow reports whether a confirmation for email may be sent now.
func (l *emailLimiter) allow(email string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if t, ok := l.last[email]; ok && now.Sub(t) < l.ttl {
		return false
	}
	l.last[email] = now
	// Opportunistic pruning keeps the map bounded on long-lived processes.
	if len(l.last) > 10_000 {
		for k, t := range l.last {
			if now.Sub(t) > l.ttl {
				delete(l.last, k)
			}
		}
	}
	return true
}

// sendNewsletterConfirm mails the double-opt-in message; a variable so
// tests can stub delivery.
var sendNewsletterConfirm = func(r *http.Request, c *newsletter.Confirmation) error {
	if _, err := newsletterDeps(); err != nil {
		return err
	}
	html, err := email.RenderConfirmHTML(c.Doc)
	if err != nil {
		return err
	}
	return nlMail.Confirmation(r.Context(), c.To,
		"Confirm your Indie Game Gems subscription", html, email.ConfirmText(c.Doc))
}

func PostNewsletterSubscribe(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return badRequest(w, "Could not read the signup form.")
	}
	rawEmail := r.PostFormValue("email")

	// Honeypot: the "organization" field is visually hidden; humans never
	// fill it, bots do. Drop them with the standard success response.
	if r.PostFormValue("organization") != "" {
		slog.Info("newsletter signup honeypot hit")
		return renderSubscribeResult(w, r, view.NewsletterFormState{Kind: "success"})
	}

	svc, err := newsletterDeps()
	if err != nil {
		return err
	}
	outcome, confirmation, err := newsletter.Signup(r.Context(), db.Queries, svc.cfg, time.Now(), rawEmail, "web")
	if err != nil {
		if errors.Is(err, newsletter.ErrInvalidEmail) {
			// Validation feedback is about the input, not about whether
			// the address exists.
			return renderSubscribeResult(w, r, view.NewsletterFormState{
				Email:   rawEmail,
				Kind:    "error",
				Message: "That doesn't look like an email address — check it and try again.",
			})
		}
		return err
	}

	if confirmation != nil {
		if outcome == newsletter.SignupResent && !confirmLimiter.allow(confirmation.To) {
			slog.Info("confirmation re-send throttled", "to", confirmation.To)
		} else if err := sendNewsletterConfirm(r, confirmation); err != nil {
			return fmt.Errorf("send confirmation: %w", err)
		}
	}
	slog.Info("newsletter signup", "outcome", outcome)
	return renderSubscribeResult(w, r, view.NewsletterFormState{Kind: "success"})
}

// renderSubscribeResult shows the signup slot: the htmx partial on its own,
// or the full page re-rendered with the result for no-JS POSTs. The HX-Target
// header names the swap slot that submitted ("newsletter-signup[-home|-footer]"),
// so each embedded instance gets its own variant back.
func renderSubscribeResult(w http.ResponseWriter, r *http.Request, form view.NewsletterFormState) error {
	if r.Header.Get("HX-Request") == "true" {
		switch strings.TrimPrefix(r.Header.Get("HX-Target"), "newsletter-signup-") {
		case "footer":
			return view.NewsletterSignupCompact(form, "footer").Render(r.Context(), w)
		case "home":
			return view.NewsletterSignup(form, "home").Render(r.Context(), w)
		default:
			return view.NewsletterSignup(form, "").Render(r.Context(), w)
		}
	}
	issues, err := db.Queries.ListSentIssues(r.Context())
	if err != nil {
		return err
	}
	return view.NewsletterPage(view.NewsletterPageData{
		Meta: view.PageMeta{
			Title:       "Newsletter",
			Description: "One great indie game a week in your inbox.",
			Canonical:   "/newsletter",
		},
		Issues: issues,
		Form:   form,
	}).Render(r.Context(), w)
}

// ---------------------------------------------------------------------------
// GET + POST /newsletter/confirm

// GetNewsletterConfirm renders the confirmation page WITHOUT changing
// anything: link-scanning mail gateways prefetch GETs, so the state change
// only happens on the button's POST (same pattern as unsubscribe).
func GetNewsletterConfirm(w http.ResponseWriter, r *http.Request) error {
	// Status page driven by a one-time token: never cacheable.
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0, s-maxage=0")
	token := r.URL.Query().Get("token")
	outcome := confirmOutcomeReady
	if token == "" {
		outcome = string(newsletter.ConfirmInvalid)
	}
	return view.NewsletterConfirmPage(outcome, token, "").Render(r.Context(), w)
}

// confirmOutcomeReady marks the pre-confirm GET state (token present, not
// yet consumed).
const confirmOutcomeReady = "ready"

// PostNewsletterConfirm consumes the token: single use, consent stamped.
// Concurrent double-clicks land on the "already confirmed" page instead of
// a 500 (the guarded UPDATE yields no rows for the loser).
func PostNewsletterConfirm(w http.ResponseWriter, r *http.Request) error {
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0, s-maxage=0")
	if err := r.ParseForm(); err != nil {
		return badRequest(w, "Could not read the confirmation form.")
	}
	outcome, emailAddr, err := newsletter.ConfirmSignup(r.Context(), db.Queries, time.Now(), r.PostFormValue("token"))
	if err != nil {
		return err
	}
	return view.NewsletterConfirmPage(string(outcome), "", emailAddr).Render(r.Context(), w)
}

// ---------------------------------------------------------------------------
// GET /newsletter/issues/{slug} — "read in browser" web archive

// emailArchiveCSP replaces the site-wide policy for snapshot responses: the
// email HTML is built entirely from inline style attributes (Gmail/Outlook
// strip <style> blocks), so the site's style-src 'self' would strip every
// style. The snapshot is static rendered output — no scripts, no forms — so
// everything else is locked down harder than the site default.
const emailArchiveCSP = "default-src 'none'; " +
	"script-src 'none'; " +
	"style-src 'unsafe-inline'; " +
	"img-src 'self' https: data:; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// badRequest writes a plain 400 (malformed form bodies and similar client
// errors must not surface as 500s through Make).
func badRequest(w http.ResponseWriter, msg string) error {
	http.Error(w, msg, http.StatusBadRequest)
	return nil
}

// archiveUnsubHref is what the archived snapshot's {{unsubscribe_url}}
// placeholder points at: the neutral newsletter page (the per-recipient
// tokenised link only exists in the delivered mail).
func archiveUnsubHref(baseURL string) string {
	return strings.TrimSuffix(baseURL, "/") + "/newsletter"
}

// GetNewsletterIssuePage serves the stored HTML snapshot of a sent issue,
// standalone (Content-Type text/html): the archive shows exactly what
// subscribers received — no re-render, no site chrome. The snapshot is a
// complete document, so the email's own table layout keeps its WYSIWYG.
func GetNewsletterIssuePage(w http.ResponseWriter, r *http.Request) error {
	issue, err := db.Queries.GetSentIssueBySlug(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound(w, r)
		}
		return err
	}
	// Immutable snapshot of a sent issue.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	// Override SecurityHeaders' site CSP: the email depends on inline styles.
	w.Header().Set("Content-Security-Policy", emailArchiveCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Snapshots store the {{unsubscribe_url}} placeholder; the archive has
	// no per-recipient token, so point it at the neutral newsletter page.
	base := ""
	if svc, err := newsletterDeps(); err == nil {
		base = svc.cfg.BaseURL
	}
	_, err = w.Write([]byte(strings.ReplaceAll(issue.Html,
		newsletter.PlaceholderUnsubURL, archiveUnsubHref(base))))
	return err
}

// ---------------------------------------------------------------------------
// GET + POST /newsletter/unsubscribe/{token}

func GetNewsletterUnsubscribe(w http.ResponseWriter, r *http.Request) error {
	if _, err := newsletterDeps(); err != nil {
		return err
	}
	// Read-only: a GET (or an email scanner's prefetch) must never opt
	// anyone out. The actual withdrawal happens on the POST below.
	email, err := newsletter.SubscriberEmailByToken(r.Context(), db.Queries, newsletterSecret(), chi.URLParam(r, "token"))
	if err != nil {
		return unsubscribeLookupErr(w, r, err)
	}
	return view.NewsletterUnsubscribePage(chi.URLParam(r, "token"), email).Render(r.Context(), w)
}

// PostNewsletterUnsubscribe serves BOTH the browser form (same-origin POST
// from the GET page) and mailbox providers' RFC 8058 one-click POST. The
// provider request carries no cookies and no Origin/Sec-Fetch-Site headers;
// the global util.SameOriginOnly middleware accepts header-less requests by
// design (sameOrigin: absent Sec-Fetch-Site falls back to Origin, absent
// Origin = non-browser client), so no route exemption is needed — verified
// by TestOneClickUnsubscribeWithoutOrigin through the full middleware
// stack. Auth is the unguessable HMAC token in the URL itself; the response
// is a plain 200 with no redirect (RFC 8058 §3: redirects break one-click).
func PostNewsletterUnsubscribe(w http.ResponseWriter, r *http.Request) error {
	if _, err := newsletterDeps(); err != nil {
		return err
	}
	return postUnsubscribe(w, r, db.Queries, newsletterSecret())
}

// postUnsubscribe is split out for tests: the one-click POST must return
// 200 without a redirect, through the real middleware stack, with a fake
// store and no DB.
func postUnsubscribe(w http.ResponseWriter, r *http.Request, store newsletter.UnsubscribeStore, secret string) error {
	email, err := newsletter.UnsubscribeByToken(r.Context(), store, secret, chi.URLParam(r, "token"))
	if err != nil {
		return unsubscribeLookupErr(w, r, err)
	}
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0, s-maxage=0")
	return view.NewsletterUnsubscribedPage(email).Render(r.Context(), w)
}

func unsubscribeLookupErr(w http.ResponseWriter, r *http.Request, err error) error {
	switch {
	case errors.Is(err, newsletter.ErrBadToken), errors.Is(err, newsletter.ErrUnknownSubscriber),
		errors.Is(err, pgx.ErrNoRows):
		return notFound(w, r)
	}
	return err
}

// newsletterSecret returns the HMAC key, or "" after Make has logged the
// config error (Unsubscribe* handlers re-check via newsletter() first).
func newsletterSecret() string {
	svc, err := newsletterDeps()
	if err != nil {
		return ""
	}
	return svc.cfg.Secret
}
