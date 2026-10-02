package newsletter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/mail"
	viewemail "github.com/embiem/indie-game-gems/view/email"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// maxSendAttempts caps automatic retries across send runs before a delivery
// is parked as failed (claiming skips rows at or above this).
const maxSendAttempts = 3

// claimTimeout is how long a 'sending' claim is trusted. A send run that
// crashed (or was killed) between claiming a row and finishing it leaves
// the row stuck in 'sending'; the next run reclaims claims older than this.
//
// Trade-off (documented, deliberate): claiming gives at-most-once per
// claim. A crash AFTER the SMTP server accepted the message but BEFORE the
// DB row was marked sent means the reclaimed row is sent again — that one
// recipient may get a duplicate. The alternative (never reclaim) strands
// deliveries forever. Truly-once delivery is impossible with non-idempotent
// SMTP; duplicates are rarer and less harmful than never-delivered issues.
const claimTimeout = 10 * time.Minute

// claimBatch is how many deliveries one claim statement grabs.
const claimBatch = 50

// ErrIssueSent is returned when operating on an already-sent issue: sent
// issues are immutable snapshots, re-running send delivers nothing new.
var ErrIssueSent = errors.New("issue was already sent")

// SendStore is the persistence surface of the send pipeline (data.Queries
// satisfies it). An interface so orchestration is testable DB-free.
type SendStore interface {
	EnsureDeliveries(ctx context.Context, issueID pgtype.UUID) (int64, error)
	ClaimPendingDeliveries(ctx context.Context, arg data.ClaimPendingDeliveriesParams) ([]data.ClaimPendingDeliveriesRow, error)
	MarkDeliverySent(ctx context.Context, id pgtype.UUID) (int64, error)
	RecordDeliveryFailure(ctx context.Context, arg data.RecordDeliveryFailureParams) (int64, error)
	MarkDeliveryFailed(ctx context.Context, arg data.MarkDeliveryFailedParams) (int64, error)
	CountDeliveriesByStatus(ctx context.Context, issueID pgtype.UUID) ([]data.CountDeliveriesByStatusRow, error)
	MarkIssueSent(ctx context.Context, issueID pgtype.UUID) (int64, error)
	IssueHasDeliveries(ctx context.Context, issueID pgtype.UUID) (bool, error)
	UpdateIssueSnapshot(ctx context.Context, arg data.UpdateIssueSnapshotParams) (data.Issue, error)
	InsertIssue(ctx context.Context, arg data.InsertIssueParams) (data.Issue, error)
	GetIssueBySlug(ctx context.Context, slug string) (data.Issue, error)
}

// BulkMailer is the transport surface (mail.Mailer satisfies it).
type BulkMailer interface {
	Newsletter(ctx context.Context, n mail.NewsletterMessage) error
}

// Sender snapshots issues, creates deliveries and sends them resumably.
type Sender struct {
	Q      SendStore
	Cfg    Config
	Mailer BulkMailer
	// Renderer resolves directives to documents (snapshots + test sends).
	Renderer *Renderer
}

// DeliveryStats summarises one send run.
type DeliveryStats struct {
	Created int // new delivery rows created by EnsureDeliveries
	Sent    int // mails delivered in this run
	Failed  int // mails that errored in this run (released for retry)
	Parked  int // deliveries parked as failed after maxSendAttempts
	Skipped int // recipients who unsubscribed before their turn
	Total   int // delivery rows for the issue after EnsureDeliveries
}

// SnapshotIssue renders and stores the issue. Drafts without deliveries are
// re-snapshotted on every send (the editing loop). Once any delivery row
// exists the snapshot is FROZEN: the stored html/text is returned unchanged,
// so a resumed send delivers exactly what the first batch got. Sent issues
// are immutable (ErrIssueSent).
func (s *Sender) SnapshotIssue(ctx context.Context, iss *Issue) (data.Issue, error) {
	existing, err := s.Q.GetIssueBySlug(ctx, iss.Slug)
	switch {
	case err == nil && existing.Status == "sent":
		return data.Issue{}, ErrIssueSent
	case err == nil:
		frozen, err := s.Q.IssueHasDeliveries(ctx, existing.ID)
		if err != nil {
			return data.Issue{}, err
		}
		if frozen {
			// Content froze when the first delivery was created.
			return existing, nil
		}
	case errors.Is(err, pgx.ErrNoRows):
		// First snapshot: fall through to render+insert.
	default:
		return data.Issue{}, err
	}

	doc, err := s.Renderer.Resolve(ctx, iss)
	if err != nil {
		return data.Issue{}, fmt.Errorf("resolve %s: %w", iss.Slug, err)
	}
	html, err := viewemail.RenderHTML(doc)
	if err != nil {
		return data.Issue{}, err
	}
	text := viewemail.Text(doc)

	snap := data.UpdateIssueSnapshotParams{
		Slug: iss.Slug, Subject: iss.Subject, Preheader: iss.Preheader,
		SourceMd: iss.Raw, Html: html, Text: text,
	}
	row, err := s.Q.UpdateIssueSnapshot(ctx, snap)
	switch {
	case err == nil:
		return row, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Either a concurrent run created the first delivery (frozen —
		// serve the stored snapshot), the issue was sent, or the draft
		// row doesn't exist yet (first-ever snapshot: insert it).
		current, getErr := s.Q.GetIssueBySlug(ctx, iss.Slug)
		switch {
		case getErr == nil && current.Status == "sent":
			return data.Issue{}, ErrIssueSent
		case getErr == nil:
			return current, nil // frozen by a concurrent run's deliveries
		case errors.Is(getErr, pgx.ErrNoRows):
			return s.Q.InsertIssue(ctx, data.InsertIssueParams{
				Slug: iss.Slug, Subject: iss.Subject, Preheader: iss.Preheader,
				SourceMd: iss.Raw, Html: html, Text: text,
			})
		default:
			return data.Issue{}, getErr
		}
	default:
		return data.Issue{}, err
	}
}

// SendAll performs one resumable send pass for a snapshotted issue: create
// deliveries for confirmed subscribers, then claim-and-send every pending
// one in batches.
//
// Delivery guarantee: each delivery is claimed atomically (status flips to
// 'sending' with SKIP LOCKED), so concurrent runs never claim the same row
// and each recipient gets at most one mail per claim. Claims older than
// claimTimeout are treated as crashed runs and reclaimed — the one case
// that can duplicate a mail is a process death between SMTP acceptance and
// the guarded status update (see claimTimeout). Unsubscribes during a send
// move the pending row to terminal 'skipped', so the issue still completes.
func (s *Sender) SendAll(ctx context.Context, issue data.Issue) (DeliveryStats, error) {
	if issue.Status == "sent" {
		return DeliveryStats{}, ErrIssueSent
	}
	var st DeliveryStats

	created, err := s.Q.EnsureDeliveries(ctx, issue.ID)
	if err != nil {
		return st, fmt.Errorf("create deliveries: %w", err)
	}
	st.Created = int(created)

	staleBefore := staleClaimCutoff(time.Now())
	for {
		if err := ctx.Err(); err != nil {
			return st, err // interrupted: claimed rows resume after claimTimeout
		}
		batch, err := s.Q.ClaimPendingDeliveries(ctx, data.ClaimPendingDeliveriesParams{
			IssueID:   issue.ID,
			Attempts:  maxSendAttempts,
			ClaimedAt: pgTimestamptz(staleBefore),
			Limit:     claimBatch,
		})
		if err != nil {
			return st, err
		}
		if len(batch) == 0 {
			break
		}
		for _, d := range batch {
			if err := s.sendOne(ctx, issue, d, &st); err != nil {
				return st, err
			}
		}
	}

	counts, err := s.Q.CountDeliveriesByStatus(ctx, issue.ID)
	if err != nil {
		return st, err
	}
	var pending int
	for _, c := range counts {
		st.Total += int(c.N)
		switch c.Status {
		case "pending":
			pending = int(c.N)
		case "skipped":
			st.Skipped = int(c.N)
		}
	}
	if pending == 0 {
		if n, err := s.Q.MarkIssueSent(ctx, issue.ID); err != nil {
			return st, err
		} else if n == 0 {
			// No draft row: a concurrent run already completed the issue.
			current, getErr := s.Q.GetIssueBySlug(ctx, issue.Slug)
			if getErr != nil {
				return st, getErr
			}
			if current.Status != "sent" {
				return st, fmt.Errorf("mark issue sent: no draft row for %s", issue.Slug)
			}
		}
	}
	return st, nil
}

// sendOne delivers one claimed row and settles its claim. The guarded
// updates only fire while the claim ('sending') is still ours: a concurrent
// run cannot have touched it (claims are exclusive until released).
func (s *Sender) sendOne(ctx context.Context, issue data.Issue, d data.ClaimPendingDeliveriesRow, st *DeliveryStats) error {
	unsub := s.Cfg.UnsubURL(d.SubscriberID.String())
	err := s.Mailer.Newsletter(ctx, mail.NewsletterMessage{
		To:       d.Email,
		Subject:  issue.Subject,
		HTML:     strings.ReplaceAll(issue.Html, PlaceholderUnsubURL, unsub),
		Text:     strings.ReplaceAll(issue.Text, PlaceholderUnsubURL, unsub),
		UnsubURL: unsub,
	})
	if err == nil {
		st.Sent++
		if _, err := s.Q.MarkDeliverySent(ctx, d.ID); err != nil {
			return err
		}
		return nil
	}

	st.Failed++
	attempts := int(d.Attempts) + 1
	slog.Warn("newsletter delivery failed",
		"to", d.Email, "attempt", attempts, "err", err)
	if attempts >= maxSendAttempts {
		st.Parked++
		_, err = s.Q.MarkDeliveryFailed(ctx, data.MarkDeliveryFailedParams{
			ID: d.ID, LastError: err.Error(),
		})
	} else {
		_, err = s.Q.RecordDeliveryFailure(ctx, data.RecordDeliveryFailureParams{
			ID: d.ID, LastError: err.Error(),
		})
	}
	return err
}

// staleClaimCutoff is the claimed_at threshold before which a claim counts
// as crashed. Factored for tests.
func staleClaimCutoff(now time.Time) time.Time {
	return now.Add(-claimTimeout)
}
