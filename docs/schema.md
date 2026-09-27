# The notification store schema

`ntfy/sqlstore` stores notifications in two tables, on PostgreSQL, MySQL or
SQLite, through any of the three sqlkit executors: `database/sql`
(`sqlkit/stdsql`), pgx (`sqlkit/pgx`) or GORM (`sqlkit/gorm`). One store
implementation serves all of them, proven identical by one conformance suite on
all seven driver-and-dialect combinations.

## Where the DDL lives

The schema is published per dialect, for a host to apply through its own
migration pipeline:

| Dialect | Document |
| --- | --- |
| PostgreSQL | `ntfy/sqlstore/ddl/postgres.sql` |
| MySQL 8.0+ | `ntfy/sqlstore/ddl/mysql.sql` |
| SQLite 3.35+ | `ntfy/sqlstore/ddl/sqlite.sql` |

The documents are the source of truth, and each explains its dialect's choices in
comments. `Store.Schema()` returns the store's document with its table prefix
applied, ready to hand to a migration tool. Every statement is `CREATE ... IF NOT
EXISTS`, so applying it to an existing schema changes nothing.

A host that emails notifications also applies the email schema,
`ntfy/sqlstore/ddl/email/<dialect>.sql`, returned by `Store.EmailSchema()`.
It indexes the notifications table, so it is applied after this document,
never before. It
holds the same promise, with **one stated exception**. On MySQL, the email
document's `CREATE INDEX` for `ntfy_notifications_email_idx` is not
idempotent. The index belongs to a table that already exists, so it cannot be
declared inside a `CREATE TABLE`, and MySQL has no `CREATE INDEX IF NOT
EXISTS`. Re-applying the MySQL email document to a database that already has
the index fails with error 1061. See [Adding the email claim index to an
existing host](#adding-the-email-claim-index-to-an-existing-host).

Nothing in normal operation changes the schema. `Store.Migrate(ctx)` applies it
directly, and exists for tests and development only.

## Tables

### `ntfy_notifications`

One row per notification.

| Column | Holds | Notes |
| --- | --- | --- |
| `id` | the notification's identifier | primary key; UUIDv7 by default |
| `recipient` | who it is for | identifier |
| `source_id` | what produced it | identifier; unique with `recipient` |
| `subject` | what it is about | identifier |
| `subject_version` | the subject's version it describes | integer |
| `kind` | the publisher's classification | identifier |
| `state` | `ACTIVE`, `READ` or `CLOSED` | identifier |
| `closed_reason` | why it was closed | nullable |
| `title` | a line a client can show | nullable text |
| `links` | hrefs by relation name, as a JSON object | nullable text |
| `data` | the publisher's JSON payload, byte for byte | nullable text |
| `created_at` | when it was published | timestamp |
| `read_at` | when it was first read | nullable timestamp |
| `closed_at` | when it was closed | nullable timestamp |
| `inactive_at` | when it first left `ACTIVE`; the age bound measures from it | nullable timestamp |

### `ntfy_watermarks`

A subject's close records: for each subject and kind, the highest version its
notifications were closed at, and under kind `*` the highest version every kind
was closed at. A publish below the record for its kind is suppressed, which is
what stops a retried older source reopening a closed subject.

Publishing and closing a subject both lock its `*` row first. On PostgreSQL and
MySQL that row lock, held to commit, serialises every write to one subject
without `SELECT ... FOR UPDATE`; SQLite has one writer.

| Column | Holds |
| --- | --- |
| `subject` | the subject; primary key with `kind` |
| `kind` | the kind, or `*` for every kind |
| `version` | the highest version closed; `-1` for a row only a publish created |
| `updated_at` | when the record last changed; watermark retention measures from it |

## Indexes

Each index serves a statement the store runs. A missing one does not make that
statement fail; it makes it read the whole table.

| Index | Columns | Serves |
| --- | --- | --- |
| `ntfy_notifications_source_key` (unique) | `source_id`, `recipient` | idempotent publishing |
| `ntfy_notifications_recipient_idx` | `recipient`, `created_at`, `id` | a recipient's listing and its keyset paging |
| `ntfy_notifications_state_idx` | `recipient`, `state` | counting active notifications; choosing what the count bound deletes |
| `ntfy_notifications_subject_idx` | `subject`, `kind`, `state` | closing by subject and kind; coalescing |
| `ntfy_notifications_inactive_idx` | `state`, `inactive_at` | the age bound |
| `ntfy_watermarks_updated_idx` | `updated_at` | expiring close records |
| `ntfy_notifications_email_idx` | `state`, `created_at`, `id` | the email claim reaching notifications no delivery record covers; **email schema only** |

`ntfy_notifications_email_idx` is on the notifications table, but the email
schema declares it and only `Store.VerifyEmailSchema` requires it.
`Store.VerifySchema` does not, so a host that never emails neither carries it
nor fails startup over it.

## Dialect choices

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| Identifier columns | `text COLLATE "C"` | `VARBINARY` | `TEXT COLLATE BINARY` |
| Timestamps | `timestamptz(6)` | `DATETIME(6)` | `TEXT`, RFC 3339, six fractional digits |
| `links`, `data` | `text` | `LONGTEXT` | `TEXT` |
| Indexes declared | `CREATE INDEX IF NOT EXISTS` | inside `CREATE TABLE` | `CREATE INDEX IF NOT EXISTS` |

- **Identifiers compare byte for byte and sort in byte order** on every
  dialect: case, trailing spaces, code points such as U+200B, and the Unicode
  normalisation form all count. On MySQL they are binary strings rather than
  text under a collation. The server default collation folds case,
  `utf8mb4_0900_as_cs` ignores U+200B and equates `é` with `e` and a combining
  accent, and `utf8mb4_bin` ignores trailing spaces; any of them would deliver
  one recipient's notifications to another. `utf8mb4_0900_bin` would not, but
  it needs MySQL 8.0.17, and binary strings need nothing newer than the store
  already does. Their lengths are bytes, as the library's limits are. A
  locale-aware collation can also sort identifiers so that keyset paging skips
  or repeats rows.
- **Payloads are text, never a native JSON type.** PostgreSQL's `jsonb` and
  MySQL's `JSON` reorder keys and rewrite number literals, and the store returns a
  payload exactly as it was published.

## Table prefix

`sqlstore.WithTablePrefix(prefix)` prefixes every table and index name, so the
store can share a database whose names would otherwise collide. The default is no
prefix.

- A prefix may contain only letters, digits and underscores; anything else is a
  configuration error from `sqlstore.New`.
- PostgreSQL truncates identifiers at 63 bytes and MySQL refuses names over 64.
  The longest index name is `ntfy_notifications_recipient_idx`, 34 bytes, so
  keep a prefix to 29 bytes or fewer.

## Verifying the schema at startup

`Store.VerifySchema(ctx)` compares the live database with what the store's
statements require: both tables, every column, the collation of every identifier
column, and every index above. It reports every discrepancy at once, as a
`*sqlkit.SchemaError` matching `sqlkit.ErrSchemaMismatch`, rather than failing on
first use. Call it when the host starts.

```go
executor, err := stdsqlexec.New(db, sqlkit.PostgreSQL)
if err != nil {
    return err
}

store, err := sqlstore.New(executor, sqlstore.WithTablePrefix("app_"))
if err != nil {
    return err
}

if err := store.VerifySchema(ctx); err != nil {
    return err // lists every missing table, column, collation and index
}
```

## Transactions

Every store method runs in a transaction of its own. A notification write never
joins a transaction the caller holds for other data, even one opened through the
same executor: a notification published while the caller's transaction is open
survives that transaction's rollback. Publishers deliver at least once and
publishing is idempotent, so a shared transaction would buy nothing.

On SQLite, a caller holding its own write transaction blocks the store's until
the busy timeout, because SQLite has one writer. Publish after committing.

## Adding the email claim index to an existing host

A host whose email schema predates `ntfy_notifications_email_idx` fails
`Store.VerifyEmailSchema` at startup until the index exists. Add it once,
before deploying the upgrade, with the table prefix in place of `app_`:

- **PostgreSQL and SQLite:** re-apply the email document, which is idempotent,
  or run its one new statement:

  ```sql
  CREATE INDEX IF NOT EXISTS "app_ntfy_notifications_email_idx"
      ON "app_ntfy_notifications" ("state", "created_at", "id");
  ```

- **MySQL:** run this, and do **not** re-apply the email document:

  ```sql
  ALTER TABLE `app_ntfy_notifications` ADD KEY `app_ntfy_notifications_email_idx` (`state`, `created_at`, `id`);
  ```

A host that does not email does nothing.

## Comparing identifiers byte for byte on an existing MySQL host

A MySQL schema whose identifier columns are `VARCHAR ... COLLATE
utf8mb4_0900_as_cs` fails `Store.VerifySchema` at startup, naming each
identifier column. It also fails `Store.VerifyEmailSchema` when the host
emails. Until it is upgraded, it treats `alice` and `alice` followed by U+200B,
or `josé` in its two Unicode spellings, as one recipient.

The upgrade makes those columns `VARBINARY`. It needs no newer MySQL. Run it
once, before deploying, through the migration pipeline, with the table prefix
in place of `app_`.

MySQL rebuilds each table to change a column's type, and writes to the table
wait until it finishes. Run it in a maintenance window, or with an online
schema-change tool.

### Checking before upgrading

The binary columns hold at most as many bytes as the library accepts: 255 for
an identifier, 100 for a kind. An identifier the library wrote is never longer.
This query lists any row written around the library that is. MySQL's default
strict mode refuses the upgrade for such a row and changes nothing; with strict
mode off it would truncate the identifier, so run this first and fix what it
finds.

```sql
SELECT `id` FROM `app_ntfy_notifications`
    WHERE LENGTH(`id`) > 64 OR LENGTH(`recipient`) > 255 OR LENGTH(`source_id`) > 255
       OR LENGTH(`subject`) > 255 OR LENGTH(`kind`) > 100 OR LENGTH(`state`) > 16;

SELECT `subject` FROM `app_ntfy_watermarks`
    WHERE LENGTH(`subject`) > 255 OR LENGTH(`kind`) > 100;
```

### Upgrading

```sql
ALTER TABLE `app_ntfy_notifications`
    MODIFY `id`        VARBINARY(64)  NOT NULL,
    MODIFY `recipient` VARBINARY(255) NOT NULL,
    MODIFY `source_id` VARBINARY(255) NOT NULL,
    MODIFY `subject`   VARBINARY(255) NOT NULL,
    MODIFY `kind`      VARBINARY(100) NOT NULL,
    MODIFY `state`     VARBINARY(16)  NOT NULL;

ALTER TABLE `app_ntfy_watermarks`
    MODIFY `subject` VARBINARY(255) NOT NULL,
    MODIFY `kind`    VARBINARY(100) NOT NULL;

-- Only a host that emails has this table.
ALTER TABLE `app_ntfy_email_deliveries`
    MODIFY `notification_id` VARBINARY(64)  NOT NULL,
    MODIFY `recipient`       VARBINARY(255) NOT NULL,
    MODIFY `status`          VARBINARY(16)  NOT NULL,
    MODIFY `batch_id`        VARBINARY(64)  NULL,
    MODIFY `owner`           VARBINARY(255) NULL;
```

No row changes: every identifier keeps its bytes, identifiers that were
distinct stay distinct, and no unique key can collide. What the old collation
already merged is not undone. A publish that it suppressed as a duplicate of a
byte-different source stays unpublished.

A listing page read across the upgrade can repeat or skip one notification
when the host's `IDGenerator` produces identifiers of mixed case, because the
old collation and byte order sort case differently. The default UUIDv7
identifiers sort the same under both.

### Rolling the upgrade back

Rolling back reintroduces the defect. It can also fail. Two rows written after
the upgrade may differ only in bytes the old collation ignores, such as
`event-1` and `event-1` followed by U+200B for one recipient, and then collide
on a unique key. An identifier that is not valid UTF-8 cannot be converted
back. Either way MySQL refuses the statement and changes nothing.

```sql
ALTER TABLE `app_ntfy_notifications`
    MODIFY `id`        VARCHAR(64)  COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `recipient` VARCHAR(255) COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `source_id` VARCHAR(255) COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `subject`   VARCHAR(255) COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `kind`      VARCHAR(100) COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `state`     VARCHAR(16)  COLLATE utf8mb4_0900_as_cs NOT NULL;

ALTER TABLE `app_ntfy_watermarks`
    MODIFY `subject` VARCHAR(255) COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `kind`    VARCHAR(100) COLLATE utf8mb4_0900_as_cs NOT NULL;

-- Only a host that emails has this table.
ALTER TABLE `app_ntfy_email_deliveries`
    MODIFY `notification_id` VARCHAR(64)  COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `recipient`       VARCHAR(255) COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `status`          VARCHAR(16)  COLLATE utf8mb4_0900_as_cs NOT NULL,
    MODIFY `batch_id`        VARCHAR(64)  COLLATE utf8mb4_0900_as_cs NULL,
    MODIFY `owner`           VARCHAR(255) COLLATE utf8mb4_0900_as_cs NULL;
```

## Rolling back

The schema is additive. To remove it, stop running the pruner, hub and handlers,
then drop `ntfy_notifications` and `ntfy_watermarks`. Nothing else depends on
them.

To remove only the email schema, stop the email dispatcher, drop
`ntfy_email_deliveries`, and then drop `ntfy_notifications_email_idx`. That
index lives on the notifications table, which outlives the email table:

- PostgreSQL and SQLite: `DROP INDEX "app_ntfy_notifications_email_idx";`
- MySQL: ``DROP INDEX `app_ntfy_notifications_email_idx` ON `app_ntfy_notifications`;``

Rolling the code back while keeping the index is also safe: the older claim
query ignores it.
