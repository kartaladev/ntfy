## Why

On SQLite, two of the store's write transactions read before they write: `MarkRead` counts the notifications it was given before updating them (`sqlstore/read.go`, `MarkRead`), and `ClaimEmails` selects the due notifications before inserting delivery records (`sqlstore/email.go`, `ClaimEmails`). SQLite begins a transaction DEFERRED unless the connection asks otherwise. A deferred transaction that has read holds a read snapshot, and when two of them then try to write, SQLite cannot upgrade the loser's lock. It fails at once with `database is locked` (`SQLITE_BUSY`, or `SQLITE_BUSY_SNAPSHOT` (517) once another writer has committed), without waiting out the busy timeout.

So a SQLite host whose connection string does not set `_txlock=immediate` gets calls that fail under ordinary concurrency: a user marking two notifications read in two tabs, or two email dispatchers claiming at once. Nothing tells the host to set it. Only the test fixture `sqlkittest.RunTestSQLite` sets it. Neither `sqlstore` nor `docs/schema.md` mentions it. The conformance matrix passes only because of that fixture, which is why the defect has not been seen.

**Proved by:** `TestAuditSQLiteConcurrentMarkRead`, from the 2026-09-28 audit, copied into `sqlstore/` and re-run on current `main` (20967b3) on 2026-09-28. It runs 16 concurrent `MarkRead` calls over a SQLite file opened with WAL and a 10 s busy timeout, varying only `_txlock`. The immediate control passes. The deferred case failed three runs out of three:

```
--- FAIL: TestAuditSQLiteConcurrentMarkRead (0.00s)
    --- FAIL: TestAuditSQLiteConcurrentMarkRead/the_driver's_default_deferred_transactions (0.01s)
        Messages: 12 of 16 concurrent MarkRead calls failed; first: sqlkit: database is locked (5) (SQLITE_BUSY)
                  statement: UPDATE "t2_ntfy_notifications" SET "state" = ?, "read_at" = ?, "inactive_at" = ? WHERE "recipient" = ? AND "state" = ? AND "id" IN (?)
        (errors include: sqlkit: database is locked (517))
    --- PASS: TestAuditSQLiteConcurrentMarkRead/control:_immediate_transactions,_as_sqlkittest's_DSN_sets (0.06s)
```

(Runs 1 and 3 reported 13 of 16.) The same day, a throwaway probe over the same deferred connection checked the other write paths, 16 concurrent calls each:

| Path | Shape | Failed |
| --- | --- | --- |
| `MarkRead` alone | read, then write | 11–13 of 16 |
| `MarkRead` racing `Insert` on another subject | read, then write, against a writer | 6–8 of 16 |
| `ClaimEmails` | read, then write | 14 of 16, three runs |
| `Insert`, same subject and distinct subjects | writes first (locks the `*` watermark) | 0 |
| `Close` | writes first (locks the `*` watermark) | 0 |
| `MarkAllRead` | one write | 0 |
| `Prune` | no transaction; one statement at a time | 0 |

`RecordEmails` (updates only) and `PurgeEmailRecords` (no transaction) were not probed. Read from the code, neither reads before writing inside a transaction, so neither is claimed as affected.

The shared suites fail too once the connection is deferred. Over a WAL, 10 s busy-timeout DSN without `_txlock`, `ntfytest.Run` passes, but `ntfytest.RunEmail` and `ntfytest.RunEmailDispatch` fail on both SQLite executors, database/sql and GORM. The failing cases include `email/concurrency/two_owners_claiming_concurrently_never_both_receive_a_notification` and `email-dispatch/two_dispatchers_running_at_once_send_every_notification_exactly_once`, and every error is `sqlkit: database is locked (5) (SQLITE_BUSY)`. Task 1 lands these tests as the red step.

The same probe showed the fix: if a deferred transaction's first statement is a write, even one that matches no row (`UPDATE … WHERE 0`), it takes SQLite's write lock before it reads, and concurrent callers queue on the busy timeout. The MarkRead shape went from 11–13 failures in 16 to 0.

## What Changes

- **On SQLite, every store write transaction takes the write lock with its first statement.** It does so whatever mode the host's connection begins transactions in. The store's one transaction helper runs a no-op write before the method's own statements. Two transactions that read before they write can then no longer deadlock on the lock upgrade: the second waits for the first, up to the host's busy timeout. PostgreSQL and MySQL are unchanged.
- **The conformance matrix runs SQLite with the driver's default transaction mode as well.** New entry points run the whole shared notification, email and dispatch suites over a WAL, busy-timeout DSN without `_txlock`, on database/sql and on GORM. `make store-matrix` runs them. The existing `_txlock=immediate` entry points stay.
- **A focused SQLite test** holds `MarkRead`, `MarkRead` racing `Insert`, and `ClaimEmails` to zero failures under 16-way concurrency on that DSN.
- **`docs/schema.md` states what a SQLite host must set, and what it need not.**
  - The store serialises its own writes, so `_txlock` does not matter.
  - A busy timeout is still required. Without one, a writer that finds the lock held fails at once. The probe proved this: with the lock-first fix but no busy timeout, 11–13 of 16 still failed.
  - WAL is recommended, so that reads are not blocked by a write.
- **No new option, no API change.** Not breaking.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-inbox`: "Every store behaves identically" now also covers SQLite whatever transaction mode the host's connection uses. Concurrent writes wait for each other rather than fail.
- `notification-email`: "Concurrent dispatchers never send the same notification twice" now also requires concurrent claims on SQLite to succeed without the host choosing a transaction mode.

## Impact

- **Code:**
  - `sqlstore/sql.go`: `Store.do` and a new `Store.writeLock`.
  - `sqlstore/internal/harness/harness.go`: a new `DeferredSQLiteDSN` helper.
  - New `sqlstore/sqlite_writes_test.go`, and `sqlstore/lock_internal_test.go` for the dialect branch.
  - New entry points in `sqlstore/harness_test.go` and `sqlstore/internal/gormtest/store_test.go`.
  - `Makefile`: `STORE_MATRIX` gains the two deferred entry points.
- **Docs:** `docs/schema.md` gets a "SQLite connections" subsection under "Transactions", and its "seven combinations" wording changes. `sqlstore/doc.go` gets one sentence.
- **Hosts:**
  - A SQLite host needs no change to its connection string for correctness. A host without a busy timeout is told to add one.
  - Each store write transaction on SQLite now runs one extra statement that touches no row.
  - PostgreSQL and MySQL hosts see nothing.
- **`pkg/sqlkit`:** untouched. `sqlkittest.RunTestSQLite` keeps `_txlock=immediate`, so the existing matrix entries are unchanged.
- **Dependencies:** none.
