BEGIN;

-- Newsletter backend: subscribers (double opt-in), issues (rendered
-- snapshots) and deliveries (per-recipient send ledger, resumable).

CREATE TABLE subscribers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Normalised (lowercased, trimmed) address. Case-insensitive uniqueness
    -- is enforced independently below so even a non-normalised write cannot
    -- create a duplicate.
    email TEXT NOT NULL,
    -- pending | confirmed | unsubscribed
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'confirmed', 'unsubscribed')),
    -- Double opt-in: the mailed confirmation link carries a random token;
    -- only its SHA-256 hash is stored so a DB leak cannot confirm anyone.
    -- Cleared once used (single-use) or expired.
    confirmation_token_hash TEXT,
    confirmation_expires_at TIMESTAMPTZ,
    -- Consent proof: when they asked, when they confirmed, where from
    -- (e.g. "web", "footer"), and when they withdrew.
    source TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ,
    unsubscribed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_subscribers_email_ci ON subscribers (lower(email));
CREATE INDEX idx_subscribers_confirmation_hash
    ON subscribers (confirmation_token_hash)
    WHERE confirmation_token_hash IS NOT NULL;
-- Serves the send path's confirmed-subscriber scans (EnsureDeliveries and
-- the ListPendingDeliveries join); most reads filter on status.
CREATE INDEX idx_subscribers_status ON subscribers (status);

CREATE TRIGGER set_subscribers_updated_at
BEFORE UPDATE ON subscribers
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();

-- One newsletter issue. source_md is the authored markdown file (with
-- directives); html/text are the rendered snapshots taken at send time.
-- html/text keep a literal {{unsubscribe_url}} placeholder that is swapped
-- per recipient during delivery, so the snapshot stays shareable.
CREATE TABLE issues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL UNIQUE,
    subject TEXT NOT NULL,
    preheader TEXT NOT NULL DEFAULT '',
    source_md TEXT NOT NULL,
    html TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL DEFAULT '',
    -- draft | sent
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'sent')),
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER set_issues_updated_at
BEFORE UPDATE ON issues
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();

-- Per-recipient ledger. UNIQUE (issue_id, subscriber_id) means one row per
-- recipient ever; delivery happens under an atomic claim (status='sending'
-- + claimed_at, claimed with FOR UPDATE SKIP LOCKED), so concurrent send
-- runs never mail the same recipient twice. Guarantee: at-most-once per
-- claim — a crash between SMTP acceptance and the DB update can duplicate
-- that one mail on reclaim (at-least-once would need idempotent SMTP).
CREATE TABLE deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    subscriber_id UUID NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    -- pending | sending | sent | failed | skipped
    -- (failed = gave up after max attempts; skipped = subscriber was no
    -- longer confirmed when their turn came — terminal, not an error)
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'skipped')),
    -- Stamp set when a send run claims the row; a claim older than the
    -- send loop's timeout is treated as crashed and reclaimed.
    claimed_at TIMESTAMPTZ,
    attempts SMALLINT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, subscriber_id)
);

-- The send loop's scan: pending deliveries of one issue.
CREATE INDEX idx_deliveries_issue_pending
    ON deliveries (issue_id, created_at)
    WHERE status = 'pending';

-- FK cascade support: deleting a subscriber (e.g. a GDPR purge) must not
-- seq-scan deliveries. The issue_id side is covered by the UNIQUE
-- (issue_id, subscriber_id) leftmost prefix.
CREATE INDEX idx_deliveries_subscriber ON deliveries (subscriber_id);

COMMIT;
