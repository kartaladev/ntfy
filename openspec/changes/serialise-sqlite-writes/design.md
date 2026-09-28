## Context

See `proposal.md` — Why, for the failing output and the per-path probe table.

Where transactions come from today:

| Where | What |
| --- | --- |
| `sqlstore/sql.go` `Store.do` | `s.executor.Do(s.own(ctx), fn)`. It is the only way the store opens a transaction. Callers: `Insert`, `Close`, `MarkRead`, `MarkAllRead` (notifications), and `ClaimEmails`, `RecordEmails` (email). Every one of them writes. |
| `pkg/sqlkit/stdsql/executor.go` `Executor.Do` | `db.BeginTx(ctx, nil)`. database/sql has no way to ask for `BEGIN IMMEDIATE`, so the transaction mode is whatever the driver's connection string says. modernc's is `_txlock`, default deferred. |
| `pkg/sqlkit/gorm` | Begins through GORM, which also goes through database/sql. Same mode rule. |
| `pkg/sqlkit/sqlkittest/testutils.go:201-205` | `RunTestSQLite` adds `_pragma=journal_mode(WAL)`, `_pragma=busy_timeout(10000)` and `_txlock=immediate`. Every SQLite matrix entry uses it. |
| `sqlstore/insert.go`, `close.go` | Their first statement is `ensureWatermark`, an `INSERT … ON CONFLICT DO UPDATE` on the `*` watermark row: a write. So they already take the write lock first, and the probe saw them never fail. |
| `sqlstore/read.go` `MarkRead`, `sqlstore/email.go` `ClaimEmails` | Their first statement is a `SELECT`. These are the two that fail. |

How SQLite behaves here, as the proof and probes showed (modernc.org/sqlite v1.58.0, WAL, Apple M4 Pro, macOS 26.6.2, Go 1.26.8, 2026-09-28):

- A deferred transaction whose first statement reads takes a read snapshot. When it later writes, it must upgrade to the write lock. If another connection holds that lock, or has committed since the snapshot, SQLite returns `SQLITE_BUSY` or `SQLITE_BUSY_SNAPSHOT` (517) **at once**. Waiting could never succeed, because the snapshot is stale, so the busy handler is not consulted.
- A deferred transaction whose first statement writes asks for the write lock before it has any snapshot. If the lock is held, the busy handler waits, up to `busy_timeout`. `UPDATE … WHERE 0`, `DELETE … WHERE 0` and `UPDATE … WHERE id IS NULL` all behaved this way, although none of them matches a row: 0 of 16 failed in each case.
- The busy timeout matters to both. With a lock-first transaction but no `busy_timeout` pragma, 11–13 of 16 still failed. modernc's default busy timeout is 0.

## Goals / Non-Goals

**Goals**

- Every store write on SQLite waits for a concurrent one instead of failing, whatever transaction mode the host's connection uses.
- The matrix proves this on both SQLite executors, so the `_txlock=immediate` fixture can no longer hide a regression.
- A SQLite host is told exactly what it must configure.

**Non-Goals**

- Setting the busy timeout for the host. The store cannot set a pragma on every pooled connection reliably, and the timeout is the host's latency policy. It is documented, not enforced (D4).
- Retrying failed transactions (D2, alternative C).
- Changing `pkg/sqlkit` or `sqlkittest`'s DSN.
- PostgreSQL and MySQL. Their row locks already serialise these writes, and they do not have this failure mode.

## Decisions

### D1. The store's transaction helper takes SQLite's write lock with its first statement

`Store.do` wraps the caller's function. On SQLite it first runs one write statement that matches no row:

```sql
UPDATE "<prefix>ntfy_notifications" SET "id" = "id" WHERE 0
```

- **Why a write that matches no row.** SQLite takes the write lock for the statement's kind, not for the rows it touches, and a write statement asks for it before taking any read snapshot. So the transaction queues on the busy timeout like any writer, and every later read happens under the lock. The upgrade that fails is never attempted.
- **Why in `Store.do`, for every write transaction, not just the two that read first.**
  - It is one place. A future method that reads before it writes is covered without anyone remembering this defect.
  - `Insert`, `Close`, `MarkAllRead` and `RecordEmails` already write first, so for them the statement is redundant and harmless: they would have taken the same lock one statement later.
  - The cost is one statement that touches no row, per write transaction, on SQLite only.
- **Why the notifications table.** Every store has it, the email store included. SQLite's write lock covers the whole database, so which table the statement names does not change what it locks.
- **Why `WHERE 0`.** SQLite reads an integer as a boolean. PostgreSQL refuses it ("argument of WHERE must be type boolean"). So if the dialect check ever let the statement through on PostgreSQL, every write would fail and the matrix would catch it at once, instead of the store paying a silent extra statement.
- **How the dialect branch is shaped.** `Store.writeLock()` returns the statement on SQLite and the zero `sqlkit.Statement` on every other dialect. `Store.do` always passes it to `Exec`, and `sqlkit.Executor.Exec` documents the zero Statement as a no-op returning 0 that sends nothing. So `do` has no branch of its own, and the dialect branch is a pure function that an internal table test covers on all three dialects without a database or a test double.
- **Not in `Prune` or `PurgeEmailRecords`.** They run each statement on its own, in no transaction, and a single statement cannot deadlock on an upgrade. They were measured unaffected, and they call `s.own`, not `s.do`.

**Default:** on SQLite, every store write transaction begins with the lock statement. **Override:** none. This is a correctness requirement, not a policy. Turning it off could only bring back a failure mode the store promises not to have (library-design rule 4: the line is stated, not left to configuration). A host that already sets `_txlock=immediate` loses nothing: its transaction already holds the lock, and the statement matches no row. What a host does control is how long a writer waits (its busy timeout) and the journal mode. Both stay in the connection string it owns, and D4 documents both.

*Alternatives considered:*

- **A. Verify the connection's mode at `New` or `VerifySchema`, and refuse a deferred one.** This would honour "wiring mistakes fail at construction". But the mode cannot be observed through SQL. No pragma reports it, and the executor does not expose the DSN. The only probe is behavioural: open two connections and time a lock conflict. That is slow, depends on timing, and needs a pool with at least two connections. It would also turn a correct host (deferred, but serialised by some other means) into a startup failure. Rejected: the store can simply make the mode not matter.
- **B. Begin `IMMEDIATE` transactions through sqlkit.** `database/sql`'s `BeginTx` cannot send `BEGIN IMMEDIATE`. `TxOptions` offers only isolation level and read-only, and modernc maps neither to IMMEDIATE. It would need a driver-specific connection hook in each of sqlkit's executor modules, recorded as a patch against the copy, for something sqlstore can do with one statement. Rejected: sqlstore-side suffices, as the task prefers.
- **C. Retry the transaction on `SQLITE_BUSY` / 517.**
  - It works only for a whole-transaction retry, because the snapshot is stale.
  - sqlkit does not classify driver errors, so sqlstore would have to match driver error text or codes per driver. That means two drivers, and GORM wraps its errors differently.
  - It needs a retry policy (attempts, backoff), which becomes one more option.
  - It still fails under enough contention, where queueing on the lock would not.
  - Rejected, as more code and a weaker guarantee.
- **D. Document `_txlock=immediate` and nothing else.** This leaves the zero-configuration default broken (library-design rule 1), and a host that misses one line of docs finds out in production. Rejected. The docs still say the mode does not matter (D4).
- **E. Serialise writes with a mutex in the Store.** It covers one process only. The inbox spec promises that several Stores, in several processes, may share the tables. Rejected.
- **F. Reorder `MarkRead` to write first.** `MarkRead` could update first and count after, rolling back on `ErrNotFound`. That fixes `MarkRead` only. `ClaimEmails` must select before it knows what to insert, so it would still need D1. Rejected in favour of one mechanism.

### D2. The matrix covers SQLite's default transaction mode

`sqlkittest.RunTestSQLite` stays as it is, because changing it would be a sqlkit patch and would drop the immediate coverage. Instead:

- `harness.DeferredSQLiteDSN(t *testing.T) string` returns a fresh database file's DSN with `foreign_keys(1)`, `journal_mode(WAL)` and `busy_timeout(10000)`, and no `_txlock`. It lives in `sqlstore/internal/harness`, which imports no driver, so both test binaries can use it: database/sql with modernc, and GORM with glebarez.
- `TestStoreOnStdSQLSQLiteDeferred` (in `sqlstore/harness_test.go`) and `TestStoreOnGormSQLiteDeferred` (in `sqlstore/internal/gormtest/store_test.go`) run `ntfytest.Run`, `RunEmail` and `RunEmailDispatch` over it.
- `Makefile` `STORE_MATRIX` gains both, and the target's comment and `docs/schema.md` stop saying "seven". The matrix now has seven driver-and-dialect combinations plus two SQLite transaction-mode variants.

Both failed before the fix. See `proposal.md`: `RunEmail` and `RunEmailDispatch` fail with `SQLITE_BUSY`, because their parallel cases share one database file and so contend for its lock.

### D3. A focused SQLite concurrency test

`sqlstore/sqlite_writes_test.go` holds `TestSQLiteWritesWaitForEachOther`, a table in the `table-test` form over `DeferredSQLiteDSN`. Its cases:

- 16 concurrent `MarkRead`s of one recipient's notifications;
- 8 `MarkRead`s racing 8 `Insert`s on another subject;
- 16 concurrent `ClaimEmails` by different owners over 64 due notifications, each with `Limit` 4.

Each case asserts that no call failed, and asserts the outcome: every notification READ, or no notification claimed by two owners. The behaviour does not depend on the context, so the table carries no `ctx` field. The file says so.

Beside it, `sqlstore/lock_internal_test.go` (package `sqlstore`) holds `TestWriteLock`. It covers the dialect branch: the exact statement on SQLite, with and without a table prefix, and the zero Statement on PostgreSQL and MySQL.

The audit's control case (immediate mode) is not landed. The existing matrix entries already cover immediate mode.

### D4. What `docs/schema.md` tells a SQLite host

A new subsection, "SQLite connections", under "Transactions":

- The store takes SQLite's write lock at the start of each of its write transactions. The connection's transaction mode (`_txlock`) therefore does not matter, and deferred, the default, is fine.
- A **busy timeout is required** (`_pragma=busy_timeout(…)` with modernc; some drivers set a default, modernc does not). Without one, a write that finds another in progress fails at once with `database is locked`.
- **WAL is recommended**, so that reads, which take no write lock, are not blocked by a write in progress.
- An example DSN.

`sqlstore/doc.go` gets one sentence pointing at the same guarantee.

The busy timeout is documented, not verified at startup. `PRAGMA busy_timeout` reports only the connection that runs it, so a check could pass on one pooled connection while another has none. The value is also the host's latency policy. It is recorded here as a deliberate limit on rule 6 (wiring mistakes fail at construction).

## Risks / Trade-offs

- **[Risk] A future SQLite version stops taking the write lock for a write statement that matches no row.** → The lock is taken per statement kind, by the statement's `OP_Transaction`, not per row. The guard tests in D2 and D3 would go red on that version, because they run the real engine at 16-way concurrency.
- **[Trade-off] Readers of a write transaction now wait for other writers** from the first statement, not from the first real write. On SQLite that was already true of every transaction that succeeded: two writers cannot overlap. Only the failure mode changes.
- **[Trade-off] One extra statement per write transaction on SQLite.** It touches no row. No performance claim is made either way, so `.claude/rules/performance-benchmark.md` does not apply.
- **[Risk] A host's own trigger on the notifications table fires on the no-op update.** → A row trigger fires per row changed, and none is. A statement-level trigger does not exist in SQLite.
- **[Risk] The dialect check is wrong and the statement runs on PostgreSQL or MySQL.** → `TestWriteLock` (internal, table-driven over the three dialects) requires the zero Statement on PostgreSQL and MySQL. As a second line, PostgreSQL refuses `WHERE 0`, so the PostgreSQL matrix entries would go red as well.

## Migration Plan

Nothing to migrate. No schema or API change. A SQLite host keeps its connection string. One that sets `_txlock=immediate` may drop it, but need not. Rollback is reverting the commit.
