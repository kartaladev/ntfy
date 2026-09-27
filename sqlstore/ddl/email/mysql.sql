-- MySQL schema for ntfy's optional email delivery records, 8.0 or later.
--
-- A host that emails notifications applies this document in addition to the
-- notification store's own; a host that does not never needs it.
--
-- One row per notification that a dispatcher has considered. There is no
-- foreign key to the notifications table: retention deletes notifications in
-- batches on every dialect, and the dispatcher removes the records it leaves
-- behind.
--
-- Identifier columns are VARBINARY and timestamps are DATETIME(6), as in the
-- notification store's schema; see that document for why. MySQL has no CREATE
-- INDEX IF NOT EXISTS, so indexes are declared inside the table.
--
-- One index here belongs to the notifications table rather than the email
-- table: ntfy_notifications_email_idx lets a dispatcher claim the notifications
-- no delivery record covers without scanning the whole table. It lives in this
-- document because only a host that emails needs it; a host that does not never
-- pays for it on every publish.
--
-- That index is this document's one statement that is NOT idempotent. The table
-- it belongs to already exists, so it cannot be declared inside a CREATE TABLE,
-- and MySQL has no CREATE INDEX IF NOT EXISTS: applying this document to a
-- database that already has the index fails with error 1061. A host upgrading
-- runs the ALTER TABLE in docs/schema.md once instead of re-applying this.

CREATE TABLE IF NOT EXISTS `{{PREFIX}}ntfy_email_deliveries` (
    `notification_id`  VARBINARY(64)  NOT NULL,
    `recipient`        VARBINARY(255) NOT NULL,
    `status`           VARBINARY(16)  NOT NULL,
    `batch_id`         VARBINARY(64)  NULL,
    `owner`            VARBINARY(255) NULL,
    `lease_until`      DATETIME(6) NULL,
    `attempts`         INT NOT NULL,
    `next_attempt_at`  DATETIME(6) NULL,
    `reason`           LONGTEXT NULL,
    `sent_at`          DATETIME(6) NULL,
    `updated_at`       DATETIME(6) NOT NULL,
    PRIMARY KEY (`notification_id`),
    KEY `{{PREFIX}}ntfy_email_deliveries_lease_idx` (`status`, `lease_until`),
    KEY `{{PREFIX}}ntfy_email_deliveries_retry_idx` (`status`, `next_attempt_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Claiming the notifications no delivery record covers yet, oldest first.
CREATE INDEX `{{PREFIX}}ntfy_notifications_email_idx`
    ON `{{PREFIX}}ntfy_notifications` (`state`, `created_at`, `id`);
