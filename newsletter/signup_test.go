package newsletter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/embiem/indie-game-gems/data"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- fakes ---------------------------------------------------------------

type fakeSignupStore struct {
	subs  map[string]*data.Subscriber // by email
	pgErr error                       // optional injected error
}

func (f *fakeSignupStore) GetSubscriberByEmail(_ context.Context, email string) (data.Subscriber, error) {
	if f.pgErr != nil {
		return data.Subscriber{}, f.pgErr
	}
	if sub, ok := f.subs[email]; ok {
		return *sub, nil
	}
	return data.Subscriber{}, pgx.ErrNoRows
}

func (f *fakeSignupStore) CreateSubscriber(_ context.Context, p data.CreateSubscriberParams) (data.Subscriber, error) {
	sub := &data.Subscriber{Email: p.Email, Status: "pending", Source: p.Source,
		ConfirmationTokenHash: p.ConfirmationTokenHash, ConfirmationExpiresAt: p.ConfirmationExpiresAt}
	f.subs[p.Email] = sub
	return *sub, nil
}

func (f *fakeSignupStore) SetSubscriberConfirmationToken(_ context.Context, p data.SetSubscriberConfirmationTokenParams) error {
	for _, sub := range f.subs {
		if sub.ID == p.ID {
			sub.ConfirmationTokenHash = p.ConfirmationTokenHash
			sub.ConfirmationExpiresAt = p.ConfirmationExpiresAt
			return nil
		}
	}
	return pgx.ErrNoRows
}

func (f *fakeSignupStore) ResubscribeSubscriber(_ context.Context, p data.ResubscribeSubscriberParams) (data.Subscriber, error) {
	for _, sub := range f.subs {
		if sub.ID == p.ID {
			sub.Status = "pending"
			sub.UnsubscribedAt = pgtype.Timestamptz{}
			sub.ConfirmationTokenHash = p.ConfirmationTokenHash
			sub.ConfirmationExpiresAt = p.ConfirmationExpiresAt
			return *sub, nil
		}
	}
	return data.Subscriber{}, pgx.ErrNoRows
}

func newTestCfg(t *testing.T) Config {
	t.Helper()
	c, err := LoadConfig(env(map[string]string{
		"BASE_URL": "https://gems.test", "NEWSLETTER_SECRET": "sec",
		"MAIL_FROM": "Gems <gems@gems.test>", "MAIL_POSTAL_ADDRESS": "1 Gem Way",
	}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func subByID(id string) pgtype.UUID {
	return pgtype.UUID{Bytes: MustUUIDBytes(id), Valid: true}
}

// --- Signup branches -----------------------------------------------------

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestSignupCreatesPendingAndReturnsConfirmation(t *testing.T) {
	st := &fakeSignupStore{subs: map[string]*data.Subscriber{}}
	out, c, err := Signup(context.Background(), st, newTestCfg(t), now, " Reader@Example.COM ", "web")
	if err != nil || out != SignupCreated {
		t.Fatalf("outcome=%v err=%v", out, err)
	}
	if c == nil || c.To != "reader@example.com" {
		t.Fatalf("confirmation = %+v", c)
	}
	if !strings.HasPrefix(c.Doc.ConfirmURL, "https://gems.test/newsletter/confirm?token=") {
		t.Fatalf("confirm URL = %q", c.Doc.ConfirmURL)
	}
	sub := st.subs["reader@example.com"]
	if sub.Status != "pending" || sub.ConfirmationTokenHash.String == "" || !sub.ConfirmationExpiresAt.Valid {
		t.Fatalf("subscriber = %+v", sub)
	}
	if HashConfirmToken(c.Token) != sub.ConfirmationTokenHash.String {
		t.Fatal("stored hash is not the token's hash")
	}
	if !sub.ConfirmationExpiresAt.Time.Equal(now.Add(ConfirmTokenTTL)) {
		t.Fatalf("expiry = %v", sub.ConfirmationExpiresAt.Time)
	}
}

func TestSignupPendingResendsWithFreshToken(t *testing.T) {
	st := &fakeSignupStore{subs: map[string]*data.Subscriber{
		"p@example.com": {ID: subByID("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"), Email: "p@example.com", Status: "pending"},
	}}
	old := st.subs["p@example.com"].ConfirmationTokenHash
	out, c, err := Signup(context.Background(), st, newTestCfg(t), now, "p@example.com", "web")
	if err != nil || out != SignupResent || c == nil {
		t.Fatalf("outcome=%v c=%v err=%v", out, c, err)
	}
	if st.subs["p@example.com"].ConfirmationTokenHash == old {
		t.Fatal("token not refreshed")
	}
}

func TestSignupConfirmedIsNoopWithoutMail(t *testing.T) {
	st := &fakeSignupStore{subs: map[string]*data.Subscriber{
		"c@example.com": {ID: subByID("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"), Email: "c@example.com", Status: "confirmed"},
	}}
	out, c, err := Signup(context.Background(), st, newTestCfg(t), now, "c@example.com", "web")
	if err != nil || out != SignupNoop || c != nil {
		t.Fatalf("outcome=%v c=%v err=%v", out, c, err)
	}
}

func TestSignupUnsubscribedRequiresReconsent(t *testing.T) {
	st := &fakeSignupStore{subs: map[string]*data.Subscriber{
		"u@example.com": {ID: subByID("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"), Email: "u@example.com", Status: "unsubscribed"},
	}}
	out, c, err := Signup(context.Background(), st, newTestCfg(t), now, "u@example.com", "web")
	if err != nil || out != SignupResub || c == nil {
		t.Fatalf("outcome=%v c=%v err=%v", out, c, err)
	}
	sub := st.subs["u@example.com"]
	if sub.Status != "pending" {
		t.Fatalf("status = %q, want pending (fresh opt-in)", sub.Status)
	}
	if sub.UnsubscribedAt.Valid {
		t.Fatal("unsubscribed_at not cleared")
	}
}

func TestSignupValidation(t *testing.T) {
	st := &fakeSignupStore{subs: map[string]*data.Subscriber{}}
	for _, bad := range []string{"", "nope", "a@b"} {
		_, _, err := Signup(context.Background(), st, newTestCfg(t), now, bad, "web")
		if !errors.Is(err, ErrInvalidEmail) {
			t.Fatalf("Signup(%q) err = %v, want ErrInvalidEmail", bad, err)
		}
	}
}

// --- ConfirmSignup branches ---------------------------------------------

type fakeConfirmStore struct {
	sub  data.Subscriber
	gone bool
}

func (f *fakeConfirmStore) GetSubscriberByConfirmationHash(_ context.Context, _ pgtype.Text) (data.Subscriber, error) {
	if f.gone {
		return data.Subscriber{}, pgx.ErrNoRows
	}
	return f.sub, nil
}

func (f *fakeConfirmStore) ConfirmSubscriber(_ context.Context, id pgtype.UUID) (data.Subscriber, error) {
	f.sub.Status = "confirmed"
	f.sub.ConfirmedAt = pgtype.Timestamptz{Time: now, Valid: true}
	f.sub.ConfirmationTokenHash = pgtype.Text{}
	return f.sub, nil
}

func TestConfirmSignupOK(t *testing.T) {
	fs := &fakeConfirmStore{sub: data.Subscriber{Email: "p@example.com", Status: "pending",
		ConfirmationExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}}}
	token := "tok123"
	fs.sub.ConfirmationTokenHash = pgtype.Text{String: HashConfirmToken(token), Valid: true}
	out, email, err := ConfirmSignup(context.Background(), fs, now, token)
	if err != nil || out != ConfirmOK || email != "p@example.com" {
		t.Fatalf("out=%v email=%q err=%v", out, email, err)
	}
	if fs.sub.Status != "confirmed" || !fs.sub.ConfirmedAt.Valid {
		t.Fatalf("subscriber = %+v", fs.sub)
	}
}

func TestConfirmSignupBranches(t *testing.T) {
	cases := []struct {
		name  string
		token string
		sub   data.Subscriber
		gone  bool
		now   time.Time
		want  ConfirmOutcome
	}{
		{"unknown hash", "tok", data.Subscriber{}, true, now, ConfirmInvalid},
		{"empty token", "", data.Subscriber{}, false, now, ConfirmInvalid},
		{"expired", "tok", data.Subscriber{Status: "pending", ConfirmationExpiresAt: pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true}}, false, now, ConfirmExpired},
		{"already", "tok", data.Subscriber{Status: "confirmed"}, false, now, ConfirmAlready},
		{"withdrawn", "tok", data.Subscriber{Status: "unsubscribed"}, false, now, ConfirmWithdrawn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := ConfirmSignup(context.Background(), &fakeConfirmStore{sub: tc.sub, gone: tc.gone}, tc.now, tc.token)
			if err != nil || out != tc.want {
				t.Fatalf("out=%v err=%v want=%v", out, err, tc.want)
			}
		})
	}
}

// --- UnsubscribeByToken --------------------------------------------------

// fakeUnsubStore satisfies newsletter.UnsubscribeStore in memory.
type fakeUnsubStore struct {
	subs map[string]data.Subscriber // keyed by canonical uuid string
}

func (f *fakeUnsubStore) GetSubscriberByID(_ context.Context, id pgtype.UUID) (data.Subscriber, error) {
	sub, ok := f.subs[id.String()]
	if !ok {
		return data.Subscriber{}, pgx.ErrNoRows
	}
	return sub, nil
}

func (f *fakeUnsubStore) MarkSubscriberUnsubscribed(_ context.Context, id pgtype.UUID) (data.Subscriber, error) {
	sub, ok := f.subs[id.String()]
	if !ok {
		return data.Subscriber{}, pgx.ErrNoRows
	}
	sub.Status = "unsubscribed"
	sub.UnsubscribedAt = pgtype.Timestamptz{Time: now, Valid: true}
	f.subs[id.String()] = sub
	return sub, nil
}

func TestUnsubscribeByTokenIdempotent(t *testing.T) {
	st := &fakeUnsubStore{subs: map[string]data.Subscriber{
		"0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001": {ID: subByID("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"), Email: "r@example.com", Status: "confirmed"},
	}}
	token := NewUnsubToken("sec", "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001")
	for range 2 { // RFC 8058: repeated POSTs are safe
		email, err := UnsubscribeByToken(context.Background(), st, "sec", token)
		if err != nil || email != "r@example.com" {
			t.Fatalf("email=%q err=%v", email, err)
		}
	}
	if st.subs["0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"].Status != "unsubscribed" {
		t.Fatal("not unsubscribed")
	}
}

func TestUnsubscribeByTokenUnknownSubscriber(t *testing.T) {
	st := &fakeUnsubStore{subs: map[string]data.Subscriber{}}
	// Valid HMAC but the subscriber row is gone (purge).
	token := NewUnsubToken("sec", "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001")
	_, err := UnsubscribeByToken(context.Background(), st, "sec", token)
	if !errors.Is(err, ErrUnknownSubscriber) {
		t.Fatalf("err = %v, want ErrUnknownSubscriber", err)
	}
}

// --- Concurrency races ----------------------------------------------------

// losingCreateStore simulates the exact DB race: two concurrent signups of
// the same address, both get ErrNoRows from the read, the loser's INSERT
// hits the unique index.
type losingCreateStore struct {
	fakeSignupStore
	createCalls int
}

func (f *losingCreateStore) CreateSubscriber(ctx context.Context, p data.CreateSubscriberParams) (data.Subscriber, error) {
	f.createCalls++
	// First call is the loser: the winner's row already exists, so the
	// INSERT violates the index. Simulate the winner by materialising the
	// row at the same moment the violation is returned.
	if f.createCalls == 1 {
		f.subs[p.Email] = &data.Subscriber{Email: p.Email, Status: "pending"}
		return data.Subscriber{}, &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	}
	return f.fakeSignupStore.CreateSubscriber(ctx, p)
}

func TestSignupUniqueViolationRace(t *testing.T) {
	// The row the winner created is visible on the re-read: pending ->
	// token refresh + resent confirmation, no 500.
	fs := &losingCreateStore{fakeSignupStore: fakeSignupStore{subs: map[string]*data.Subscriber{}}}
	out, conf, err := Signup(context.Background(), fs, newTestCfg(t), now, "a@example.com", "web")
	if err != nil || out != SignupResent || conf == nil {
		t.Fatalf("out=%v conf=%v err=%v — want resent on unique-violation race", out, conf, err)
	}
	if fs.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1 (loser must not insert again)", fs.createCalls)
	}
}

func TestSignupUniqueViolationRaceNoRow(t *testing.T) {
	// Perverse case: the violating row disappears before the re-read —
	// surface the original error rather than a lie.
	fs := &losingCreateStore{fakeSignupStore: fakeSignupStore{
		subs:  map[string]*data.Subscriber{},
		pgErr: pgx.ErrNoRows,
	}}
	_, _, err := Signup(context.Background(), fs, newTestCfg(t), now, "a@example.com", "web")
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("err=%v, want original unique-violation error", err)
	}
}

// racingConfirmStore simulates two concurrent confirmations: the guarded
// single-use UPDATE yields no rows for the loser.
type racingConfirmStore struct {
	fakeConfirmStore
	confirmErr error
}

func (f *racingConfirmStore) ConfirmSubscriber(ctx context.Context, id pgtype.UUID) (data.Subscriber, error) {
	if f.confirmErr != nil {
		return data.Subscriber{}, f.confirmErr
	}
	return f.fakeConfirmStore.ConfirmSubscriber(ctx, id)
}

func TestConfirmSignupConcurrentAlready(t *testing.T) {
	fs := &racingConfirmStore{fakeConfirmStore: fakeConfirmStore{sub: data.Subscriber{
		Email: "p@example.com", Status: "pending",
		ConfirmationExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
	}}}
	fs.sub.ConfirmationTokenHash = pgtype.Text{String: HashConfirmToken("tok123"), Valid: true}
	fs.confirmErr = pgx.ErrNoRows // loser of the race
	out, email, err := ConfirmSignup(context.Background(), fs, now, "tok123")
	if err != nil || out != ConfirmAlready || email != "p@example.com" {
		t.Fatalf("out=%v email=%q err=%v — want ConfirmAlready, not a 500", out, email, err)
	}
}
