-- Newsletter: subscribers (double opt-in), issues and deliveries.
--
-- Subscriber state transitions live in newsletter/state.go; this file is
-- the persistence only. Index notes: email lookups use the unique
-- lower(email) index, confirm-by-hash the partial confirmation_token_hash
-- index, pending-delivery scans idx_deliveries_issue_pending, lookups by id
-- the PKs.

-- name: CreateSubscriber :one
INSERT INTO subscribers (email, source, confirmation_token_hash, confirmation_expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSubscriberByEmail :one
SELECT * FROM subscribers
WHERE lower(email) = $1;

-- name: GetSubscriberByID :one
SELECT * FROM subscribers
WHERE id = $1;

-- name: GetSubscriberByConfirmationHash :one
SELECT * FROM subscribers
WHERE confirmation_token_hash = $1;

-- name: SetSubscriberConfirmationToken :exec
-- (Re)sent confirmation mail for a pending signup: fresh token + expiry.
UPDATE subscribers
SET confirmation_token_hash = $2,
    confirmation_expires_at = $3,
    updated_at = now()
WHERE id = $1 AND status = 'pending';

-- name: ResubscribeSubscriber :one
-- Explicit new signup over an unsubscribed address: back to pending with a
-- fresh token. created_at keeps the original signup; the new consent is
-- stamped on confirm.
UPDATE subscribers
SET status = 'pending',
    confirmation_token_hash = $2,
    confirmation_expires_at = $3,
    unsubscribed_at = NULL,
    updated_at = now()
WHERE id = $1 AND status = 'unsubscribed'
RETURNING *;

-- name: ConfirmSubscriber :one
-- Double-opt-in completion: single-use token (hash cleared), consent stamped.
UPDATE subscribers
SET status = 'confirmed',
    confirmed_at = now(),
    confirmation_token_hash = NULL,
    confirmation_expires_at = NULL,
    updated_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: MarkSubscriberUnsubscribed :one
-- Idempotent opt-out (RFC 8058 retries must succeed): re-running keeps the
-- FIRST withdrawal timestamp as consent proof.
UPDATE subscribers
SET status = 'unsubscribed',
    unsubscribed_at = COALESCE(unsubscribed_at, now()),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListSubscribers :many
SELECT * FROM subscribers
ORDER BY created_at, email;

-- name: CountSubscribersByStatus :many
SELECT status, count(*)::bigint AS n
FROM subscribers
GROUP BY status
ORDER BY status;

-- name: CountConfirmedSubscribers :one
SELECT count(*)::bigint FROM subscribers WHERE status = 'confirmed';

-- name: DeleteExpiredConfirmationToken :exec
-- Housekeeping: forget the hash once the link can no longer be honoured.
UPDATE subscribers
SET confirmation_token_hash = NULL,
    confirmation_expires_at = NULL
WHERE confirmation_expires_at < now();

-- Issues ------------------------------------------------------------

-- name: InsertIssue :one
INSERT INTO issues (slug, subject, preheader, source_md, html, text)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateIssueSnapshot :one
-- Re-send authoring loop: refresh the draft's snapshot. Two guards:
-- sent issues are immutable, and a draft whose deliveries already exist is
-- frozen — content must not change once any recipient row was created, or
-- resumed sends would deliver different content than the first batch.
UPDATE issues
SET subject = $2, preheader = $3, source_md = $4, html = $5, text = $6,
    updated_at = now()
WHERE slug = $1 AND status = 'draft'
  AND NOT EXISTS (SELECT 1 FROM deliveries WHERE issue_id = issues.id)
RETURNING *;

-- name: IssueHasDeliveries :one
-- Snapshot freeze rule: once any delivery row exists for an issue its
-- rendered content is frozen (send-time consistency for resumed runs).
SELECT EXISTS (SELECT 1 FROM deliveries WHERE issue_id = $1) AS has_deliveries;

-- name: GetIssueBySlug :one
SELECT * FROM issues
WHERE slug = $1;

-- name: GetIssueByID :one
SELECT * FROM issues
WHERE id = $1;

-- name: ListIssues :many
SELECT * FROM issues
ORDER BY created_at DESC;

-- name: ListSentIssues :many
-- Web archive index: only sent issues are public; drafts never leak.
SELECT slug, subject, sent_at
FROM issues
WHERE status = 'sent'
ORDER BY sent_at DESC;

-- name: MarkIssueSent :execrows
UPDATE issues
SET status = 'sent', sent_at = now(), updated_at = now()
WHERE id = $1 AND status = 'draft';

-- Deliveries ----------------------------------------------------------

-- name: EnsureDeliveries :execrows
-- One row per current confirmed subscriber; re-runs are no-ops for pairs
-- that already exist (UNIQUE issue_id+subscriber_id).
INSERT INTO deliveries (issue_id, subscriber_id)
SELECT $1, s.id
FROM subscribers s
WHERE s.status = 'confirmed'
ON CONFLICT (issue_id, subscriber_id) DO NOTHING;

-- name: ClaimPendingDeliveries :many
-- Atomically claim a batch of work for one send run. One statement does
-- three things under a single snapshot:
--   1. reclaim crashed claims: 'sending' rows whose claim is older than $3
--      (the claim holder died between SMTP accept and the DB update —
--      reclaiming trades at-most-once for that edge; documented in send.go);
--   2. skip subscribers who are no longer confirmed (unsubscribed mid-send):
--      their pending row moves to terminal 'skipped' so completion works;
--   3. claim pending rows FOR UPDATE / SKIP LOCKED, flipping them to
--      'sending' with a claim stamp — a concurrent run cannot claim them.
-- Returns the claimed batch with recipient addresses.
WITH reclaimed AS (
    UPDATE deliveries
    SET status = 'pending', claimed_at = NULL
    WHERE deliveries.issue_id = $1 AND deliveries.status = 'sending'
      AND deliveries.claimed_at < $3
), skipped AS (
    UPDATE deliveries d
    SET status = 'skipped', claimed_at = NULL
    FROM subscribers s
    WHERE d.subscriber_id = s.id
      AND d.issue_id = $1
      AND d.status = 'pending'
      AND s.status <> 'confirmed'
), claimed AS (
    UPDATE deliveries d
    SET status = 'sending', claimed_at = now()
    WHERE d.id IN (
        SELECT c.id
        FROM deliveries c
        JOIN subscribers s ON s.id = c.subscriber_id
        WHERE c.issue_id = $1
          AND c.status = 'pending'
          AND c.attempts < $2
          AND s.status = 'confirmed'
        ORDER BY c.created_at
        FOR UPDATE OF c SKIP LOCKED
        LIMIT $4
    )
    RETURNING d.id, d.attempts, d.subscriber_id
)
SELECT c.id, c.attempts, c.subscriber_id, s.email
FROM claimed c
JOIN subscribers s ON s.id = c.subscriber_id
ORDER BY s.email;

-- name: MarkDeliverySent :execrows
-- Guarded by status='sending': only the run holding the claim may complete
-- it, so a slow run cannot mark a row another run already reclaimed.
UPDATE deliveries
SET status = 'sent', sent_at = now(), attempts = attempts + 1,
    last_error = '', claimed_at = NULL
WHERE id = $1 AND status = 'sending';

-- name: RecordDeliveryFailure :execrows
-- Release a failed attempt back to pending for retry (attempt counted,
-- claim released).
UPDATE deliveries
SET status = 'pending', attempts = attempts + 1, last_error = $2,
    claimed_at = NULL
WHERE id = $1 AND status = 'sending';

-- name: MarkDeliveryFailed :execrows
-- Park as failed after max attempts (claim released).
UPDATE deliveries
SET status = 'failed', attempts = attempts + 1, last_error = $2,
    claimed_at = NULL
WHERE id = $1 AND status = 'sending';

-- name: CountDeliveriesByStatus :many
SELECT status, count(*)::bigint AS n
FROM deliveries
WHERE issue_id = $1
GROUP BY status;

-- name: GetPickOnOrBeforeWithDate :one
-- Game of the Day for the newsletter: like picks.GetLatestPickOnOrBefore
-- but also returns the pick_date, which the Gotd heading shows.
SELECT p.pick_date, p.note_md, sqlc.embed(game_cards)
FROM game_cards
JOIN daily_picks p ON p.game_id = game_cards.id
WHERE p.pick_date <= $1
ORDER BY p.pick_date DESC
LIMIT 1;

-- name: GetSentIssueBySlug :one
-- Web archive: only sent issues are publicly readable; drafts 404.
SELECT slug, subject, html, text, sent_at
FROM issues
WHERE slug = $1 AND status = 'sent';
