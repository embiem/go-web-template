package newsletter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/embiem/indie-game-gems/data"
	viewemail "github.com/embiem/indie-game-gems/view/email"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The three public flows (signup, confirm, unsubscribe) as orchestration
// over small store interfaces, so the HTTP handlers stay thin and the
// branch logic is unit-testable without a database. data.Queries satisfies
// every interface below.

// SignupStore is the persistence surface of the signup flow.
type SignupStore interface {
	GetSubscriberByEmail(ctx context.Context, email string) (data.Subscriber, error)
	CreateSubscriber(ctx context.Context, params data.CreateSubscriberParams) (data.Subscriber, error)
	SetSubscriberConfirmationToken(ctx context.Context, params data.SetSubscriberConfirmationTokenParams) error
	ResubscribeSubscriber(ctx context.Context, params data.ResubscribeSubscriberParams) (data.Subscriber, error)
}

// SignupOutcome tells the caller what happened. The public HTTP response is
// identical for every outcome (no address enumeration); the outcome exists
// for logging only.
type SignupOutcome string

const (
	SignupCreated SignupOutcome = "created" // new pending subscriber + confirmation sent
	SignupResent  SignupOutcome = "resent"  // pending re-request: fresh token, mail re-sent
	SignupResub   SignupOutcome = "resub"   // was unsubscribed: back to pending, mail sent
	SignupNoop    SignupOutcome = "noop"    // already confirmed: nothing sent
)

// Confirmation is what the caller must mail away.
type Confirmation struct {
	To    string
	Token string // plaintext, exists only here and in the mail
	Doc   viewemail.ConfirmDoc
}

// ErrInvalidEmail marks signups that failed address validation (as opposed
// to store/transport errors).
var ErrInvalidEmail = errors.New("invalid email address")

// Signup runs the double-opt-in entry point. It never reveals whether the
// address exists: every real-address outcome returns a Confirmation (except
// SignupNoop, where the caller renders the standard response without
// sending anything).
func Signup(ctx context.Context, st SignupStore, cfg Config, now time.Time, rawEmail, source string) (SignupOutcome, *Confirmation, error) {
	email, err := NormalizeAndValidate(rawEmail)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrInvalidEmail, err)
	}
	token, hash, expires := NewConfirmToken(now)

	sub, getErr := st.GetSubscriberByEmail(ctx, email)
	if errors.Is(getErr, pgx.ErrNoRows) {
		_, err := st.CreateSubscriber(ctx, data.CreateSubscriberParams{
			Email:                 email,
			Source:                source,
			ConfirmationTokenHash: pgText(hash),
			ConfirmationExpiresAt: pgTimestamptz(expires),
		})
		switch {
		case err == nil:
			return SignupCreated, confirmation(cfg, email, token), nil
		case isUniqueViolation(err):
			// Concurrent first signup for the same address won the race;
			// the row now exists — fall through to the existing-subscriber
			// branches instead of 500ing on the unique index.
			sub, getErr = st.GetSubscriberByEmail(ctx, email)
			switch {
			case getErr == nil:
			case errors.Is(getErr, pgx.ErrNoRows):
				// Winner's row vanished again (deleted?): retry-able, so
				// surface the original violation rather than a confusing
				// "no rows".
				return "", nil, err
			default:
				return "", nil, getErr
			}
		default:
			return "", nil, err
		}
	} else if getErr != nil {
		return "", nil, getErr
	}
	return signupExisting(ctx, st, cfg, sub, hash, expires, email, token)
}

// signupExisting applies the right action for an address that already has a
// subscriber row.
func signupExisting(ctx context.Context, st SignupStore, cfg Config, sub data.Subscriber, hash string, expires time.Time, email, token string) (SignupOutcome, *Confirmation, error) {
	switch Status(sub.Status) {
	case StatusConfirmed:
		return SignupNoop, nil, nil
	case StatusUnsubscribed:
		// Only a fresh signup (this one) may clear an opt-out — this is
		// that explicit re-consent.
		if _, err := st.ResubscribeSubscriber(ctx, data.ResubscribeSubscriberParams{
			ID:                    sub.ID,
			ConfirmationTokenHash: pgText(hash),
			ConfirmationExpiresAt: pgTimestamptz(expires),
		}); err != nil {
			return "", nil, err
		}
		return SignupResub, confirmation(cfg, email, token), nil
	default: // pending: refresh the token so old links die
		if err := st.SetSubscriberConfirmationToken(ctx, data.SetSubscriberConfirmationTokenParams{
			ID:                    sub.ID,
			ConfirmationTokenHash: pgText(hash),
			ConfirmationExpiresAt: pgTimestamptz(expires),
		}); err != nil {
			return "", nil, err
		}
		return SignupResent, confirmation(cfg, email, token), nil
	}
}

// isUniqueViolation reports a PostgreSQL unique-constraint violation (23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func confirmation(cfg Config, email, token string) *Confirmation {
	return &Confirmation{
		To:    email,
		Token: token,
		Doc: viewemail.ConfirmDoc{
			ConfirmURL:    cfg.ConfirmURL(token),
			HomeURL:       cfg.BaseURL,
			PostalAddress: cfg.PostalAddress,
			Reason: "Someone (hopefully you) signed this address up for the Indie Game Gems " +
				"newsletter. If that wasn't you, ignore this mail — nothing else will happen. " +
				"The link expires in 3 days.",
		},
	}
}

// ConfirmOutcome is the confirmation-link result, one per status page.
type ConfirmOutcome string

const (
	ConfirmOK        ConfirmOutcome = "ok"
	ConfirmAlready   ConfirmOutcome = "already"   // link clicked twice
	ConfirmExpired   ConfirmOutcome = "expired"   // hash found but too old
	ConfirmInvalid   ConfirmOutcome = "invalid"   // unknown hash
	ConfirmWithdrawn ConfirmOutcome = "withdrawn" // unsubscribed before confirming
)

// ConfirmStore is the persistence surface of the confirm flow.
type ConfirmStore interface {
	GetSubscriberByConfirmationHash(ctx context.Context, hash pgtype.Text) (data.Subscriber, error)
	ConfirmSubscriber(ctx context.Context, id pgtype.UUID) (data.Subscriber, error)
}

// ConfirmSignup consumes a confirmation token: finds the hash, checks
// expiry and flips the state. Consent is stamped by the UPDATE
// (confirmed_at = now()). Returns the outcome and the subscriber's address
// ("" except for ok/already, where the page echoes it).
func ConfirmSignup(ctx context.Context, st ConfirmStore, now time.Time, token string) (ConfirmOutcome, string, error) {
	if token == "" {
		return ConfirmInvalid, "", nil
	}
	sub, err := st.GetSubscriberByConfirmationHash(ctx, pgtype.Text{String: HashConfirmToken(token), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return ConfirmInvalid, "", nil
	}
	if err != nil {
		return "", "", err
	}
	if sub.ConfirmationExpiresAt.Valid && now.After(sub.ConfirmationExpiresAt.Time) {
		return ConfirmExpired, "", nil
	}
	switch Status(sub.Status) {
	case StatusConfirmed:
		return ConfirmAlready, sub.Email, nil
	case StatusUnsubscribed:
		return ConfirmWithdrawn, "", nil
	}
	if _, err := st.ConfirmSubscriber(ctx, sub.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A concurrent confirm already consumed the token (single-use,
			// guarded UPDATE): same outcome for this click.
			return ConfirmAlready, sub.Email, nil
		}
		return "", "", err
	}
	return ConfirmOK, sub.Email, nil
}

// UnsubscribeStore is the persistence surface of the unsubscribe flow.
type UnsubscribeStore interface {
	GetSubscriberByID(ctx context.Context, id pgtype.UUID) (data.Subscriber, error)
	MarkSubscriberUnsubscribed(ctx context.Context, id pgtype.UUID) (data.Subscriber, error)
}

// LookupStore is the read-only subset used by the GET unsubscribe page.
type LookupStore interface {
	GetSubscriberByID(ctx context.Context, id pgtype.UUID) (data.Subscriber, error)
}

// ErrUnknownSubscriber marks tokens whose subscriber no longer exists.
var ErrUnknownSubscriber = errors.New("unknown subscriber")

// UnsubscribeByToken verifies an unsubscribe token and opts the subscriber
// out (idempotent, RFC 8058 safe). Returns the address for the response page.
func UnsubscribeByToken(ctx context.Context, st UnsubscribeStore, secret, token string) (email string, err error) {
	id, err := ParseUnsubToken(secret, token)
	if err != nil {
		return "", err
	}
	sub, err := st.GetSubscriberByID(ctx, pgUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUnknownSubscriber
	}
	if err != nil {
		return "", err
	}
	updated, err := st.MarkSubscriberUnsubscribed(ctx, sub.ID)
	if err != nil {
		return "", err
	}
	return updated.Email, nil
}

// SubscriberEmailByToken verifies a token and returns the address WITHOUT
// changing anything: the GET unsubscribe page must render a confirm step,
// not opt the user out on a mere preview (email scanners prefetch links).
func SubscriberEmailByToken(ctx context.Context, st LookupStore, secret, token string) (string, error) {
	id, err := ParseUnsubToken(secret, token)
	if err != nil {
		return "", err
	}
	sub, err := st.GetSubscriberByID(ctx, pgUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUnknownSubscriber
	}
	if err != nil {
		return "", err
	}
	return sub.Email, nil
}

// --- pgtype helpers ------------------------------------------------------

func pgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

func pgUUID(id string) pgtype.UUID {
	return pgtype.UUID{Bytes: MustUUIDBytes(id), Valid: true}
}

func pgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
