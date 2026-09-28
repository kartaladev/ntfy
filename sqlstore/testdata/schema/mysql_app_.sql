-- MySQL schema for the ntfy notification store, 8.0 or later.
--
-- Identifier columns are VARBINARY: binary strings, which compare and sort by
-- their bytes and pad nothing, on every supported server. Every character-set
-- collation merges identifiers the other stores keep apart: the server default,
-- utf8mb4_0900_ai_ci, folds case; utf8mb4_0900_as_cs ignores code points such
-- as U+200B and equates NFC with NFD; utf8mb4_bin ignores trailing spaces; and
-- utf8mb4_0900_bin, the one that would not, needs 8.0.17. Any of the others
-- would deliver one recipient's notifications to another on this one dialect
-- out of three. Their lengths are bytes, as the library's own limits are:
-- MaxIdentifierBytes is 255 and MaxKindBytes 100.
--
-- Identifier columns are VARBINARY rather than BLOB because MySQL cannot index
-- a BLOB column without a prefix length. Every index here stays well within
-- InnoDB's 3072-byte key limit.
--
-- Timestamps are DATETIME(6), not TIMESTAMP: TIMESTAMP converts through the
-- session time zone and runs out of range in 2038.
--
-- links and data are LONGTEXT, not JSON. MySQL's JSON type sorts object keys
-- and rewrites number literals, and the store promises to return a publisher's
-- payload exactly as it was published.
--
-- MySQL has no CREATE INDEX IF NOT EXISTS, so indexes are declared inside each
-- table.
--
-- Table and index names carry the host's configured prefix, applied when this
-- document is rendered; with no prefix configured they are exactly as written.

CREATE TABLE IF NOT EXISTS `app_ntfy_notifications` (
    `id`               VARBINARY(64)  NOT NULL,
    `recipient`        VARBINARY(255) NOT NULL,
    `source_id`        VARBINARY(255) NOT NULL,
    `subject`          VARBINARY(255) NOT NULL,
    `subject_version`  BIGINT NOT NULL,
    `kind`             VARBINARY(100) NOT NULL,
    `state`            VARBINARY(16)  NOT NULL,
    `closed_reason`    VARCHAR(255) NULL,
    `title`            LONGTEXT NULL,
    `links`            LONGTEXT NULL,
    `data`             LONGTEXT NULL,
    `created_at`       DATETIME(6) NOT NULL,
    `read_at`          DATETIME(6) NULL,
    `closed_at`        DATETIME(6) NULL,
    `inactive_at`      DATETIME(6) NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `app_ntfy_notifications_source_key` (`source_id`, `recipient`),
    KEY `app_ntfy_notifications_recipient_idx` (`recipient`, `created_at`, `id`),
    KEY `app_ntfy_notifications_state_idx` (`recipient`, `state`),
    KEY `app_ntfy_notifications_subject_idx` (`subject`, `kind`, `state`),
    KEY `app_ntfy_notifications_inactive_idx` (`state`, `inactive_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- A subject's close records: the highest version closed per kind, and per
-- every kind under kind '*'. Publishing and closing a subject both lock its '*'
-- row first, which is what serialises them.
CREATE TABLE IF NOT EXISTS `app_ntfy_watermarks` (
    `subject`     VARBINARY(255) NOT NULL,
    `kind`        VARBINARY(100) NOT NULL,
    `version`     BIGINT NOT NULL,
    `updated_at`  DATETIME(6) NOT NULL,
    PRIMARY KEY (`subject`, `kind`),
    KEY `app_ntfy_watermarks_updated_idx` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
