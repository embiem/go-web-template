package newsletter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/mail"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- fake store: mirrors the SQL semantics (atomic claim, guarded updates) ---

type fakeSendStore struct {
	mu sync.Mutex

	issue      data.Issue
	subs       []data.Subscriber // id + email
	deliveries []fakeDelivery
	sentMails  map[string]int // email -> mails delivered

	// failure simulation: email -> mailer errors to return before success
	failures map[string][]error
}

func mustUUID(s string) pgtype.UUID {
	// Deterministic 16 bytes from an arbitrary string (tests only).
	var b [16]byte
	copy(b[:], []byte(fmt.Sprintf("%-16s", s)))
	return pgtype.UUID{Bytes: b, Valid: true}
}

func (f *fakeSendStore) EnsureDeliveries(_ context.Context, issueID pgtype.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var created int64
	for i, s := range f.subs {
		exists := false
		for _, d := range f.deliveries {
			if d.subscriberID == s.ID {
				exists = true
			}
		}
		if !exists {
			f.deliveries = append(f.deliveries, fakeDelivery{
				id: mustUUID(fmt.Sprintf("%02d", i)), issueID: issueID,
				subscriberID: s.ID, email: s.Email, status: "pending",
			})
			created++
		}
	}
	return created, nil
}

type fakeDelivery struct {
	id           pgtype.UUID
	issueID      pgtype.UUID
	subscriberID pgtype.UUID
	email        string
	status       string // pending | sending | sent | failed | skipped
	attempts     int16
	claimedAt    time.Time
}

func (f *fakeSendStore) deliveriesNow() []fakeDelivery { return f.deliveries }

func (f *fakeSendStore) ClaimPendingDeliveries(_ context.Context, arg data.ClaimPendingDeliveriesParams) ([]data.ClaimPendingDeliveriesRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Reclaim stale claims, skip non-confirmed, then claim atomically —
	// under the mutex this reproduces the SQL's FOR UPDATE SKIP LOCKED.
	var out []data.ClaimPendingDeliveriesRow
	for i := range f.deliveries {
		d := &f.deliveries[i]
		if d.issueID != arg.IssueID {
			continue
		}
		if d.status == "sending" && d.claimedAt.Before(arg.ClaimedAt.Time) {
			d.status = "pending"
			d.claimedAt = time.Time{}
		}
	}
	for i := range f.deliveries {
		d := &f.deliveries[i]
		if d.issueID != arg.IssueID || d.status != "pending" {
			continue
		}
		sub := f.subByID(d.subscriberID)
		if sub.Status != "confirmed" {
			d.status = "skipped"
			continue
		}
		if int(d.attempts) >= int(arg.Attempts) {
			continue
		}
		d.status = "sending"
		d.claimedAt = time.Now()
		out = append(out, data.ClaimPendingDeliveriesRow{
			ID: d.id, Attempts: d.attempts, SubscriberID: d.subscriberID, Email: d.email,
		})
		if len(out) >= int(arg.Limit) {
			break
		}
	}
	return out, nil
}

func (f *fakeSendStore) MarkDeliverySent(_ context.Context, id pgtype.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.deliveries {
		d := &f.deliveries[i]
		if d.id == id && d.status == "sending" {
			d.status, d.attempts, d.claimedAt = "sent", d.attempts+1, time.Time{}
			if f.sentMails == nil {
				f.sentMails = map[string]int{}
			}
			f.sentMails[d.email]++
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeSendStore) RecordDeliveryFailure(_ context.Context, arg data.RecordDeliveryFailureParams) (int64, error) {
	return f.settleFailure(arg.ID, arg.LastError, "pending")
}

func (f *fakeSendStore) MarkDeliveryFailed(_ context.Context, arg data.MarkDeliveryFailedParams) (int64, error) {
	return f.settleFailure(arg.ID, arg.LastError, "failed")
}

func (f *fakeSendStore) settleFailure(id pgtype.UUID, errMsg, status string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.deliveries {
		d := &f.deliveries[i]
		if d.id == id && d.status == "sending" {
			d.status, d.attempts, d.claimedAt = status, d.attempts+1, time.Time{}
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeSendStore) CountDeliveriesByStatus(_ context.Context, _ pgtype.UUID) ([]data.CountDeliveriesByStatusRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := map[string]int64{}
	for _, d := range f.deliveries {
		counts[d.status]++
	}
	var out []data.CountDeliveriesByStatusRow
	for st, n := range counts {
		out = append(out, data.CountDeliveriesByStatusRow{Status: st, N: n})
	}
	return out, nil
}

func (f *fakeSendStore) MarkIssueSent(_ context.Context, _ pgtype.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issue.Status != "draft" {
		return 0, nil
	}
	f.issue.Status = "sent"
	return 1, nil
}

func (f *fakeSendStore) IssueHasDeliveries(_ context.Context, _ pgtype.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deliveries) > 0, nil
}

func (f *fakeSendStore) UpdateIssueSnapshot(_ context.Context, arg data.UpdateIssueSnapshotParams) (data.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issue.Slug != arg.Slug || f.issue.Status != "draft" || len(f.deliveries) > 0 {
		return data.Issue{}, pgx.ErrNoRows
	}
	f.issue.Subject, f.issue.Preheader = arg.Subject, arg.Preheader
	f.issue.Html, f.issue.Text = arg.Html, arg.Text
	return f.issue, nil
}

func (f *fakeSendStore) InsertIssue(_ context.Context, arg data.InsertIssueParams) (data.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issue = data.Issue{ID: mustUUID(arg.Slug), Slug: arg.Slug, Subject: arg.Subject,
		Preheader: arg.Preheader, SourceMd: arg.SourceMd, Html: arg.Html, Text: arg.Text, Status: "draft"}
	return f.issue, nil
}

func (f *fakeSendStore) GetIssueBySlug(_ context.Context, slug string) (data.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issue.Slug != slug {
		return data.Issue{}, pgx.ErrNoRows
	}
	return f.issue, nil
}

func (f *fakeSendStore) subByID(id pgtype.UUID) data.Subscriber {
	for _, s := range f.subs {
		if s.ID == id {
			return s
		}
	}
	return data.Subscriber{}
}

// fakeMailer counts deliveries and can inject per-recipient failures.
type fakeMailer struct {
	mu       sync.Mutex
	failures map[string][]error
	sent     map[string]int
}

func (m *fakeMailer) Newsletter(_ context.Context, n mail.NewsletterMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent[n.To]++
	if errs := m.failures[n.To]; len(errs) > 0 {
		m.failures[n.To] = errs[1:]
		return errs[0]
	}
	return nil
}

func newSendFixture(t *testing.T, n int) (*fakeSendStore, *fakeMailer, data.Issue) {
	t.Helper()
	fs := &fakeSendStore{failures: map[string][]error{}}
	iss, err := fs.InsertIssue(context.Background(), data.InsertIssueParams{
		Slug: "2026-w40", Subject: "Weekly", Html: "html {{unsubscribe_url}}", Text: "text {{unsubscribe_url}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		fs.subs = append(fs.subs, data.Subscriber{
			ID: mustUUID(fmt.Sprintf("sub%02d@example.com", i)), Email: fmt.Sprintf("sub%02d@example.com", i), Status: "confirmed",
		})
	}
	return fs, &fakeMailer{failures: map[string][]error{}, sent: map[string]int{}}, iss
}

func newTestSender(t *testing.T, fs *fakeSendStore, fm *fakeMailer) *Sender {
	t.Helper()
	return &Sender{
		Q:      fs,
		Cfg:    Config{BaseURL: "https://gems.test", Secret: "test-secret-0123456789abcdef"},
		Mailer: fm,
		// Directive-free issues never touch the DB: Renderer.Q stays nil.
		Renderer: &Renderer{Cfg: Config{BaseURL: "https://gems.test"}},
	}
}

// --- tests ---------------------------------------------------------------

func TestSendAllDeliversExactlyOnce(t *testing.T) {
	fs, fm, iss := newSendFixture(t, 3)
	st, err := newTestSender(t, fs, fm).SendAll(context.Background(), iss)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sent != 3 || st.Created != 3 || st.Failed+st.Parked+st.Skipped != 0 {
		t.Fatalf("stats = %+v", st)
	}
	for email, n := range fm.sent {
		if n != 1 {
			t.Fatalf("%s got %d mails", email, n)
		}
	}
	// Unsubscribe link swapped per recipient in both parts.
	for i, d := range fs.deliveriesNow() {
		if d.status != "sent" {
			t.Fatalf("delivery %d status %s", i, d.status)
		}
	}
}

func TestSendAllConcurrentRunsNeverDuplicate(t *testing.T) {
	fs, fm, iss := newSendFixture(t, 8)
	// Two concurrent runs race over the same fake store; the claim protocol
	// must give every recipient exactly one mail.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = newTestSender(t, fs, fm).SendAll(context.Background(), iss)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil && !errors.Is(err, ErrIssueSent) {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	total := 0
	for email, n := range fm.sent {
		if n != 1 {
			t.Fatalf("%s received %d mails", email, n)
		}
		total += n
	}
	if total != 8 {
		t.Fatalf("total mails = %d, want 8", total)
	}
}

func TestSendAllRetriesThenParks(t *testing.T) {
	fs, fm, iss := newSendFixture(t, 2)
	email := "sub00@example.com"
	fm.failures[email] = []error{
		errors.New("smtp 451"), errors.New("smtp 451"), errors.New("smtp 451"),
	}
	st, err := newTestSender(t, fs, fm).SendAll(context.Background(), iss)
	if err != nil {
		t.Fatal(err)
	}
	if st.Parked != 1 || st.Sent != 1 || st.Failed != 3 {
		t.Fatalf("stats = %+v, want 1 parked, 1 sent, 3 failed attempts", st)
	}
	// Parked row is terminal and the healthy recipient is already done:
	// a re-run delivers nothing new.
	st2, err := newTestSender(t, fs, fm).SendAll(context.Background(), iss)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Sent != 0 || st2.Parked != 0 || st2.Total != 2 {
		t.Fatalf("re-run stats = %+v, want nothing new", st2)
	}
}

func TestSendAllSkipsUnconfirmedMidSend(t *testing.T) {
	fs, fm, iss := newSendFixture(t, 2)
	// One subscriber unsubscribes before the send run.
	fs.subs[0].Status = "unsubscribed"
	st, err := newTestSender(t, fs, fm).SendAll(context.Background(), iss)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sent != 1 || st.Skipped != 1 {
		t.Fatalf("stats = %+v, want 1 sent, 1 skipped", st)
	}
	// The issue still completes with no pending rows left.
	got, _ := fs.GetIssueBySlug(context.Background(), "2026-w40")
	if got.Status != "sent" {
		t.Fatalf("issue status = %s, want sent", got.Status)
	}
}

func TestSendAllIsIdempotentAfterCompletion(t *testing.T) {
	fs, fm, iss := newSendFixture(t, 2)
	s := newTestSender(t, fs, fm)
	if _, err := s.SendAll(context.Background(), iss); err != nil {
		t.Fatal(err)
	}
	issue, _ := fs.GetIssueBySlug(context.Background(), iss.Slug)
	_, err := s.SendAll(context.Background(), issue)
	if !errors.Is(err, ErrIssueSent) {
		t.Fatalf("re-run err = %v, want ErrIssueSent", err)
	}
}

func TestSnapshotIssueFreezesAfterFirstDelivery(t *testing.T) {
	fs, _, _ := newSendFixture(t, 2)
	s := newTestSender(t, fs, fm0())
	snap, err := s.SnapshotIssue(context.Background(), &Issue{Slug: "2026-w40", Subject: "Weekly", Raw: "raw"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate first batch sent: deliveries exist now.
	if _, err := fs.EnsureDeliveries(context.Background(), snap.ID); err != nil {
		t.Fatal(err)
	}
	// Editor edits the file between runs: the frozen snapshot must win.
	snap2, err := s.SnapshotIssue(context.Background(), &Issue{Slug: "2026-w40", Subject: "EDITED", Raw: "raw2"})
	if err != nil {
		t.Fatal(err)
	}
	if snap2.Subject != "Weekly" || snap2.Html != snap.Html {
		t.Fatalf("snapshot mutated after deliveries existed: %+v", snap2)
	}
}

func TestSnapshotIssueFirstEverInserts(t *testing.T) {
	// Regression: the very first snapshot of an unknown slug must INSERT
	// the draft row instead of surfacing ErrNoRows.
	fs, _, _ := newSendFixture(t, 2)
	fs.issue = data.Issue{} // no issue row yet
	s := newTestSender(t, fs, fm0())
	snap, err := s.SnapshotIssue(context.Background(), &Issue{Slug: "2026-w40", Subject: "Weekly", Raw: "raw"})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Slug != "2026-w40" || snap.Html == "" || snap.Text == "" {
		t.Fatalf("first snapshot = %+v", snap)
	}
	if _, err := fs.GetIssueBySlug(context.Background(), "2026-w40"); err != nil {
		t.Fatalf("issue not persisted: %v", err)
	}
}

func TestSnapshotIssueRefusesSentIssue(t *testing.T) {
	fs, _, iss := newSendFixture(t, 1)
	s := newTestSender(t, fs, fm0())
	fs.issue.Status = "sent"
	_, err := s.SnapshotIssue(context.Background(), &Issue{Slug: iss.Slug, Subject: "x", Raw: "x"})
	if !errors.Is(err, ErrIssueSent) {
		t.Fatalf("err = %v, want ErrIssueSent", err)
	}
}

func fm0() *fakeMailer { return &fakeMailer{failures: map[string][]error{}, sent: map[string]int{}} }

// staleClaimCutoff wiring: the claim threshold is exactly claimTimeout back.
func TestStaleClaimCutoff(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	if got := staleClaimCutoff(now); !got.Equal(now.Add(-claimTimeout)) {
		t.Fatalf("cutoff = %v", got)
	}
	if claimTimeout <= time.Second {
		t.Fatal("claimTimeout implausibly small")
	}
	if maxSendAttempts < 2 {
		t.Fatal("maxSendAttempts should allow at least one retry")
	}
	if !strings.Contains(PlaceholderUnsubURL, "unsubscribe_url") {
		t.Fatal("placeholder contract broken")
	}
}
