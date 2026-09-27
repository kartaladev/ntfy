-- PostgreSQL schema for ntfy's optional email delivery records.
--
-- A host that emails notifications applies this document in addition to the
-- notification store's own; a host that does not never needs it.
--
-- One row per notification that a dispatcher has considered. There is no
-- foreign key to the notifications table: retention deletes notifications in
-- batches on every dialect, and the dispatcher removes the records it leaves
-- behind.
--
-- Identifier columns are pinned to the C collation, as in the notification
-- store's schema.
--
-- One index here belongs to the notifications table rather than the email
-- table: ntfy_notifications_email_idx lets a dispatcher claim the notifications
-- no delivery record covers without scanning the whole table. It lives in this
-- document because only a host that emails needs it; a host that does not never
-- pays for it on every publish.

CREATE TABLE IF NOT EXISTS "{{PREFIX}}ntfy_email_deliveries" (
    "notification_id"  text COLLATE "C" NOT NULL,
    "recipient"        text COLLATE "C" NOT NULL,
    "status"           text COLLATE "C" NOT NULL,
    "batch_id"         text COLLATE "C",
    "owner"            text COLLATE "C",
    "lease_until"      timestamptz(6),
    "attempts"         integer NOT NULL,
    "next_attempt_at"  timestamptz(6),
    "reason"           text,
    "sent_at"          timestamptz(6),
    "updated_at"       timestamptz(6) NOT NULL,
    CONSTRAINT "{{PREFIX}}ntfy_email_deliveries_pkey" PRIMARY KEY ("notification_id")
);

-- Finding leases that lapsed.
CREATE INDEX IF NOT EXISTS "{{PREFIX}}ntfy_email_deliveries_lease_idx"
    ON "{{PREFIX}}ntfy_email_deliveries" ("status", "lease_until");

-- Finding retries that are due.
CREATE INDEX IF NOT EXISTS "{{PREFIX}}ntfy_email_deliveries_retry_idx"
    ON "{{PREFIX}}ntfy_email_deliveries" ("status", "next_attempt_at");

-- Claiming the notifications no delivery record covers yet, oldest first.
CREATE INDEX IF NOT EXISTS "{{PREFIX}}ntfy_notifications_email_idx"
    ON "{{PREFIX}}ntfy_notifications" ("state", "created_at", "id");
