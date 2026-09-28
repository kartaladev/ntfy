# Serialise SQLite Writes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On SQLite, make every store write transaction wait for a concurrent one instead of failing with `database is locked`, whatever transaction mode the host's connection uses, and prove it across the conformance matrix.

**Architecture:**
- `Store.do` is the only place the store opens a transaction. On SQLite it will first execute `UPDATE "<prefix>ntfy_notifications" SET "id" = "id" WHERE 0`. That is a write statement matching no row, so SQLite takes its write lock before the transaction reads anything, and the lock upgrade that deadlocks deferred transactions never happens.
- The statement comes from a pure `Store.writeLock()`, which returns the zero `sqlkit.Statement` on PostgreSQL and MySQL. The executor treats the zero Statement as a no-op, so `do` needs no branch.
- Two new matrix entries run the full shared suites on SQLite with the driver's default (deferred) transaction mode.

**Tech Stack:**
- Go 1.26, with the core module on the standard library only.
- `stretchr/testify`.
- modernc.org/sqlite v1.58.0 through database/sql, and glebarez/sqlite through GORM.
- `golangci-lint` v2 and `openspec`.

**Spec:** `openspec/changes/serialise-sqlite-writes/`. Read:
- `proposal.md`: why, the failing audit output, and the per-path probe table;
- `design.md`: D1 the lock-first statement and the rejected alternatives, D2 the matrix, D3 the focused test, D4 the docs;
- `specs/notification-inbox/spec.md` and `specs/notification-email/spec.md`: the scenarios this plan must satisfy.

## Global Constraints

- **Go 1.26: export `GOTOOLCHAIN=go1.26.8` before any Go command**, or pass it inline as below. A newer Go may be first on `PATH`. The `make` targets already pin it.
- **The core module imports only the standard library** in production code. This change touches no core module file.
- **Each satellite module may import only `ntfy`, `sqlkit` and its own client library** among `github.com/kartaladev` modules. `make split-check` is authoritative. This change adds no import to a production file.
- **`pkg/sqlkit` changes only through a recorded patch** (`pkg/sqlkit/README.md`, `PATCHES.md`). This change does not touch it. `sqlkittest.RunTestSQLite` keeps `_txlock=immediate`, and `make sqlkit-copy-check` must still pass.
- **Tests follow the `table-test` skill:**
  - an `assert` closure per case, never `want`/`wantErr` fields;
  - `t.Context()`, never `context.Background()`, in test bodies;
  - `require` only for preconditions.
  - Neither new table varies context, so neither has a `ctx` field, and each says so in a one-line comment.
- **Test doubles come from the `use-mockgen` skill.** This change needs none. `TestWriteLock` reuses the existing `dialectExecutor` in `sqlstore/email_claim_sql_test.go`, a no-op executor that exists to build a store for a dialect. It stands in for no collaborator's behaviour.
- **External services come from the `use-testcontainers` skill.** SQLite runs in-process and needs no container. The deferred DSN helper lives in `sqlstore/internal/harness`, beside the helpers both test binaries share. No container helper is written.
- **`.claude/rules/prove-errors-with-tests.md`:** each red step is run and its failing output compared with the expected output below before any production edit. A red caused by compilation or a missing fixture does not count.
- **`.claude/rules/golang-tdd.md`:** red → green → refactor. The test and the code that satisfies it land in the same commit.
- **`.claude/rules/library-design.md`:** the lock statement has no override (`design.md` D1 says why). Do not add an option for it. The host's control is its connection string: busy timeout and journal mode.
- **`.claude/rules/performance-benchmark.md`:** this change makes no performance claim, so it needs no benchmark.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn.
- **Done means `make all` and `make store-matrix` pass**, since a store changes, and `make sqlkit-copy-check` passes.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`, and end with the session's attribution lines.

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `sqlstore/internal/harness/harness.go` | `ntfy/sqlstore` | **Modify.** Add `DeferredSQLiteDSN`: a fresh SQLite file DSN with WAL and a busy timeout, and no `_txlock`. |
| `sqlstore/sqlite_writes_test.go` | `ntfy/sqlstore` | **Create**, package `sqlstore_test`. `TestSQLiteWritesWaitForEachOther`: MarkRead, MarkRead racing Insert, and ClaimEmails at 16-way concurrency. |
| `sqlstore/harness_test.go` | `ntfy/sqlstore` | **Modify.** Add `TestStoreOnStdSQLSQLiteDeferred`. |
| `sqlstore/internal/gormtest/store_test.go` | `ntfy/sqlstore` | **Modify.** Add `TestStoreOnGormSQLiteDeferred`. |
| `sqlstore/lock_internal_test.go` | `ntfy/sqlstore` | **Create**, package `sqlstore`. `TestWriteLock`: the dialect branch. |
| `sqlstore/sql.go:107-110` | `ntfy/sqlstore` | **Modify.** `Store.do` executes `writeLock()` first; add `Store.writeLock`. |
| `sqlstore/doc.go` | `ntfy/sqlstore` | **Modify.** One sentence on SQLite writes. |
| `Makefile:160-175` | — | **Modify.** `STORE_MATRIX` gains two entries; the target's comment is reworded. |
| `docs/schema.md` | — | **Modify.** The intro's matrix sentence, and a new "SQLite connections" subsection under "Transactions". |

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: prove the defect, focused and in the matrix | 1.1, 1.2 |
| Task 2: the dialect branch, red then green, and the fix | 1.3, 2.1 |
| Task 3: wire the matrix and document | 3.1, 3.2 |
| Task 4: verify and record | 4.1 |

Task 1 writes only tests, and they are red. Its files stay uncommitted and are committed with Task 2's fix, so no commit on the branch is red and each test lands with the code that satisfies it (`golang-tdd.md`).

---

### Task 1: Prove the defect (red)

**Files:**
- Modify: `sqlstore/internal/harness/harness.go` (add a function after `Reach`)
- Create: `sqlstore/sqlite_writes_test.go`
- Modify: `sqlstore/harness_test.go` (append after `TestStoreOnStdSQLSQLite`)
- Modify: `sqlstore/internal/gormtest/store_test.go` (append after `TestStoreOnGormSQLite`)

**Interfaces:**
- Consumes (existing):
  - `openSQL(t *testing.T, driver, dsn string) *sql.DB` and `stdsqlExecutor(t *testing.T, db *sql.DB, dialect sqlkit.Dialect) sqlkit.Executor`, both in `sqlstore/harness_test.go`;
  - `harness.NewEmailStore(t *testing.T, executor sqlkit.Executor) *sqlstore.Store`, `harness.Factory(executor) ntfytest.Factory` and `harness.EmailFactory(executor) ntfytest.EmailFactory`;
  - in `gormtest`: `openGORM(t *testing.T, dialector gorm.Dialector) *gorm.DB` and `run(t *testing.T, db *gorm.DB, dialect sqlkit.Dialect)`;
  - `(*sqlstore.Store).Insert(ctx, subject string, insertions []ntfy.Insertion) (ntfy.InsertResult, error)`;
  - `(*sqlstore.Store).MarkRead(ctx, recipient string, ids []string, at time.Time) (ntfy.MarkResult, error)`;
  - `(*sqlstore.Store).ClaimEmails(ctx, claim ntfy.EmailClaim) ([]ntfy.EmailCandidate, error)`;
  - `(*sqlstore.Store).Get(ctx, recipient, id string) (ntfy.Notification, error)`.
- Produces:
  - `func DeferredSQLiteDSN(t *testing.T) string` in package `harness`;
  - `TestSQLiteWritesWaitForEachOther`, `TestStoreOnStdSQLSQLiteDeferred` and `TestStoreOnGormSQLiteDeferred`;
  - file-level helpers in `sqlstore/sqlite_writes_test.go`: `concurrently(n int, call func(i int) error) []error` and `firstError(errs []error) error`.

- [ ] **Step 1: Add the deferred DSN helper**

Add `"path/filepath"` to the imports of `sqlstore/internal/harness/harness.go`, and add after `Reach`:

```go
// DeferredSQLiteDSN returns a DSN for a fresh SQLite database file in the
// test's own temporary directory, opened the way a careful host opens one:
// foreign keys on, write-ahead logging, and a ten-second busy timeout. Unlike
// sqlkittest.RunTestSQLite it leaves the transaction mode at the driver's
// default, deferred, because a host is not asked to set one. It is a plain
// string, so both test binaries can hand it to their own driver.
func DeferredSQLiteDSN(t *testing.T) string {
	t.Helper()

	return "file:" + filepath.Join(t.TempDir(), "deferred.db") +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(10000)"
}
```

- [ ] **Step 2: Write the focused failing test**

Create `sqlstore/sqlite_writes_test.go`:

```go
package sqlstore_test

// SQLite has one writer. These cases run the store's write paths that read
// before they write, concurrently, over a connection that leaves the
// transaction mode at the driver's default (deferred), as a host that sets
// only a busy timeout does. Every call must wait its turn, not fail. The cases
// do not vary context, so the table has no ctx field.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/sqlstore"
	"github.com/kartaladev/ntfy/sqlstore/internal/harness"
	"github.com/kartaladev/sqlkit"
)

func TestSQLiteWritesWaitForEachOther(t *testing.T) {
	t.Parallel()

	const callers = 16

	created := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	recipient := func(i int) string { return fmt.Sprintf("r-%02d", i) }
	id := func(i int) string { return fmt.Sprintf("n-%02d", i) }

	// seed publishes n ACTIVE notifications on task-1, one per recipient.
	seed := func(t *testing.T, store *sqlstore.Store, n int) {
		t.Helper()

		insertions := make([]ntfy.Insertion, 0, n)
		for i := range n {
			insertions = append(insertions, ntfy.Insertion{Notification: ntfy.Notification{
				ID: id(i), Recipient: recipient(i), SourceID: fmt.Sprintf("event-%d", i),
				Subject: "task-1", SubjectVersion: 1, Kind: "offer", State: ntfy.StateActive, CreatedAt: created,
			}})
		}

		result, err := store.Insert(t.Context(), "task-1", insertions)
		require.NoError(t, err)
		require.Len(t, result.Created, n)
	}

	type testCase struct {
		name   string
		assert func(t *testing.T, store *sqlstore.Store)
	}

	cases := []testCase{
		{
			name: "concurrent MarkRead calls all succeed",
			assert: func(t *testing.T, store *sqlstore.Store) {
				seed(t, store, callers)

				failed := concurrently(callers, func(i int) error {
					_, err := store.MarkRead(t.Context(), recipient(i), []string{id(i)}, created.Add(time.Minute))

					return err
				})

				assert.Emptyf(t, failed, "%d of %d concurrent MarkRead calls failed; first: %v",
					len(failed), callers, firstError(failed))

				for i := range callers {
					got, err := store.Get(t.Context(), recipient(i), id(i))
					require.NoError(t, err)
					assert.Equalf(t, ntfy.StateRead, got.State, "%s is not READ", id(i))
				}
			},
		},
		{
			name: "MarkRead racing Insert on another subject all succeed",
			assert: func(t *testing.T, store *sqlstore.Store) {
				seed(t, store, callers)

				failed := concurrently(callers, func(i int) error {
					if i%2 == 0 {
						_, err := store.Insert(t.Context(), "task-2", []ntfy.Insertion{{Notification: ntfy.Notification{
							ID: fmt.Sprintf("m-%02d", i), Recipient: "bob", SourceID: fmt.Sprintf("other-%d", i),
							Subject: "task-2", SubjectVersion: 1, Kind: "offer", State: ntfy.StateActive, CreatedAt: created,
						}}})

						return err
					}

					_, err := store.MarkRead(t.Context(), recipient(i), []string{id(i)}, created.Add(time.Minute))

					return err
				})

				assert.Emptyf(t, failed, "%d of %d concurrent MarkRead and Insert calls failed; first: %v",
					len(failed), callers, firstError(failed))
			},
		},
		{
			name: "concurrent ClaimEmails calls all succeed and never share a notification",
			assert: func(t *testing.T, store *sqlstore.Store) {
				const due, limit = 64, 4 // callers × limit = due: serialised claims take every one

				seed(t, store, due)

				now := created.Add(time.Minute)

				var (
					mu     sync.Mutex
					owners = make(map[string]string, due)
				)

				failed := concurrently(callers, func(i int) error {
					owner := fmt.Sprintf("owner-%02d", i)

					claimed, err := store.ClaimEmails(t.Context(), ntfy.EmailClaim{
						Now: now, Owner: owner, Lease: time.Minute,
						CreatedUntil: now, CreatedFrom: now.Add(-time.Hour), Limit: limit,
					})
					if err != nil {
						return err
					}

					mu.Lock()
					defer mu.Unlock()

					for _, candidate := range claimed {
						if other, taken := owners[candidate.Notification.ID]; taken {
							return fmt.Errorf("%s was claimed by %s and by %s", candidate.Notification.ID, other, owner)
						}

						owners[candidate.Notification.ID] = owner
					}

					return nil
				})

				assert.Emptyf(t, failed, "%d of %d concurrent ClaimEmails calls failed; first: %v",
					len(failed), callers, firstError(failed))
				assert.Len(t, owners, due, "serialised claims take every due notification exactly once")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			executor := stdsqlExecutor(t, openSQL(t, "sqlite", harness.DeferredSQLiteDSN(t)), sqlkit.SQLite)

			tc.assert(t, harness.NewEmailStore(t, executor))
		})
	}
}

// concurrently runs call n times at once, one goroutine per index, and returns
// the errors in the order they arrived.
func concurrently(n int, call func(i int) error) []error {
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed []error
	)

	for i := range n {
		wg.Go(func() {
			if err := call(i); err != nil {
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	return failed
}

// firstError is the first of errs, or nil.
func firstError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}

	return errs[0]
}
```

- [ ] **Step 3: Run it and watch every row fail for the stated reason**

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestSQLiteWritesWaitForEachOther' -count=1 -v .`

Expected: FAIL on all three rows, each with a `database is locked` message. The counts vary from run to run. The 2026-09-28 probe of the same shapes saw:

```
--- FAIL: TestSQLiteWritesWaitForEachOther/concurrent_MarkRead_calls_all_succeed
        Messages: 13 of 16 concurrent MarkRead calls failed; first: sqlkit: database is locked (5) (SQLITE_BUSY)
--- FAIL: TestSQLiteWritesWaitForEachOther/MarkRead_racing_Insert_on_another_subject_all_succeed
        Messages: 7 of 16 concurrent MarkRead and Insert calls failed; first: sqlkit: database is locked (5) (SQLITE_BUSY)
--- FAIL: TestSQLiteWritesWaitForEachOther/concurrent_ClaimEmails_calls_all_succeed_and_never_share_a_notification
        Messages: 14 of 16 concurrent ClaimEmails calls failed; first: sqlkit: database is locked (5) (SQLITE_BUSY)
```

The first error may instead read `sqlkit: database is locked (517)` (SQLITE_BUSY_SNAPSHOT). Both are the stated reason. A compile error, or `no such table`, is not.

**STOP:** if a row passes here, the defect does not reproduce for that path.
- Run it twice more with `-count=1`.
- If it still passes, record the three runs' output in `design.md` under a new heading "Does not reproduce", naming the path.
- Delete that row, and tell the maintainer before going on. Do not change production code for a path this test cannot turn red.

- [ ] **Step 4: Add the two matrix entry points**

Append to `sqlstore/harness_test.go`, after `TestStoreOnStdSQLSQLite`:

```go
// TestStoreOnStdSQLSQLiteDeferred runs the suites on SQLite with the driver's
// default, deferred, transactions. sqlkittest's DSN asks for immediate ones;
// a host is not asked to, so the store must be correct either way.
func TestStoreOnStdSQLSQLiteDeferred(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "sqlite", harness.DeferredSQLiteDSN(t))

	executor := stdsqlExecutor(t, db, sqlkit.SQLite)

	ntfytest.Run(t, harness.Factory(executor))
	t.Run("email", func(t *testing.T) { ntfytest.RunEmail(t, harness.EmailFactory(executor)) })
	t.Run("email-dispatch", func(t *testing.T) { ntfytest.RunEmailDispatch(t, harness.EmailFactory(executor)) })
}
```

Append to `sqlstore/internal/gormtest/store_test.go`, after `TestStoreOnGormSQLite`:

```go
// TestStoreOnGormSQLiteDeferred runs the suites on SQLite with the driver's
// default, deferred, transactions, as TestStoreOnStdSQLSQLiteDeferred does for
// database/sql.
func TestStoreOnGormSQLiteDeferred(t *testing.T) {
	t.Parallel()

	run(t, openGORM(t, sqlite.Open(harness.DeferredSQLiteDSN(t))), sqlkit.SQLite)
}
```

- [ ] **Step 5: Run both and watch the email groups fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestStoreOnStdSQLSQLiteDeferred' -count=1 . 2>&1 | grep -E -- '--- FAIL|database is locked' | sort | uniq -c`

Expected: FAIL. `ntfytest.Run`'s groups pass. The `email` and `email-dispatch` groups fail, and every error is `sqlkit: database is locked (5) (SQLITE_BUSY)`. The 2026-09-28 probe run included:

```
--- FAIL: TestStoreOnStdSQLSQLiteDeferred/email/concurrency/two_owners_claiming_concurrently_never_both_receive_a_notification
--- FAIL: TestStoreOnStdSQLSQLiteDeferred/email-dispatch/two_dispatchers_running_at_once_send_every_notification_exactly_once
--- FAIL: TestStoreOnStdSQLSQLiteDeferred/email/leases (…)
--- FAIL: TestStoreOnStdSQLSQLiteDeferred/email/outcomes (…)
```

Which non-concurrency cases fail varies: the suite's parallel cases share one database file, so they contend for its lock too.

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestStoreOnGormSQLiteDeferred' -count=1 ./internal/gormtest 2>&1 | grep -E -- '--- FAIL|database is locked' | sort | uniq -c`

Expected: the same shape of failure under `TestStoreOnGormSQLiteDeferred/email…` and `…/email-dispatch/…`, again all `sqlkit: database is locked (5) (SQLITE_BUSY)`.

- [ ] **Step 6: Do not commit yet**

The branch must never hold a red commit. Task 1's files are committed with Task 2's fix, in Task 2 Step 7.

---

### Task 2: Take SQLite's write lock first (red → green)

**Files:**
- Modify: `sqlstore/sql.go:107-110` (`Store.do`), adding `Store.writeLock` directly after it
- Create: `sqlstore/lock_internal_test.go`

**Interfaces:**
- Consumes (existing):
  - `dialectExecutor{dialect sqlkit.Dialect}` and `storeFor(t *testing.T, dialect sqlkit.Dialect) *Store`, both in `sqlstore/email_claim_sql_test.go`, package `sqlstore`;
  - `New(executor sqlkit.Executor, opts ...Option) (*Store, error)` and `WithTablePrefix(prefix string) Option`;
  - `sqlkit.NewWriter(dialect sqlkit.Dialect) *sqlkit.Writer`, `(*Writer).Write(parts ...string)`, `(*Writer).Done() sqlkit.Statement`, and `sqlkit.Statement{SQL string; Args []any}` with `IsZero() bool`;
  - `sqlkit.Executor.Exec(ctx, statement sqlkit.Statement) (int64, error)`, documented to treat the zero Statement as a no-op returning 0;
  - the unexported `(*Store).notificationsTable() string` and `(*Store).quote(identifier string) string`.
- Produces:
  - `func (s *Store) writeLock() sqlkit.Statement`: on SQLite, `UPDATE <quoted prefixed notifications table> SET "id" = "id" WHERE 0` with no args; the zero Statement on every other dialect;
  - `func (s *Store) do(ctx context.Context, fn func(ctx context.Context) error) error`, signature unchanged, now executing `writeLock()` before `fn`.

- [ ] **Step 1: Add a stub `writeLock` so the test can compile**

In `sqlstore/sql.go`, directly after `do`, add:

```go
// writeLock is the statement that takes the database's write lock at the
// start of a store transaction.
func (s *Store) writeLock() sqlkit.Statement {
	return sqlkit.Statement{}
}
```

- [ ] **Step 2: Write the failing dialect test**

Create `sqlstore/lock_internal_test.go`:

```go
package sqlstore

// The statement that takes SQLite's write lock, asserted per dialect with no
// database. The cases do not vary context, so the table has no ctx field.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/sqlkit"
)

func TestWriteLock(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		store  func(t *testing.T) *Store
		assert func(t *testing.T, statement sqlkit.Statement)
	}

	prefixed := func(t *testing.T) *Store {
		t.Helper()

		store, err := New(dialectExecutor{dialect: sqlkit.SQLite}, WithTablePrefix("app_"))
		require.NoError(t, err)

		return store
	}

	none := func(t *testing.T, statement sqlkit.Statement) {
		t.Helper()

		assert.Truef(t, statement.IsZero(), "no lock statement on this dialect, got %q", statement.SQL)
	}

	cases := []testCase{
		{
			name:  "SQLite writes a no-op update first",
			store: func(t *testing.T) *Store { return storeFor(t, sqlkit.SQLite) },
			assert: func(t *testing.T, statement sqlkit.Statement) {
				assert.Equal(t, `UPDATE "ntfy_notifications" SET "id" = "id" WHERE 0`, statement.SQL)
				assert.Empty(t, statement.Args)
			},
		},
		{
			name:  "SQLite names the prefixed table",
			store: prefixed,
			assert: func(t *testing.T, statement sqlkit.Statement) {
				assert.Equal(t, `UPDATE "app_ntfy_notifications" SET "id" = "id" WHERE 0`, statement.SQL)
			},
		},
		{
			name:   "PostgreSQL needs no lock statement",
			store:  func(t *testing.T) *Store { return storeFor(t, sqlkit.PostgreSQL) },
			assert: none,
		},
		{
			name:   "MySQL needs no lock statement",
			store:  func(t *testing.T) *Store { return storeFor(t, sqlkit.MySQL) },
			assert: none,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, tc.store(t).writeLock())
		})
	}
}
```

- [ ] **Step 3: Run it and watch the SQLite rows fail on their text**

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestWriteLock' -count=1 -v .`

Expected: FAIL on the two SQLite rows. The PostgreSQL and MySQL rows pass.

```
--- FAIL: TestWriteLock/SQLite_writes_a_no-op_update_first
        Error: Not equal:
               expected: "UPDATE \"ntfy_notifications\" SET \"id\" = \"id\" WHERE 0"
               actual  : ""
--- FAIL: TestWriteLock/SQLite_names_the_prefixed_table
        Error: Not equal:
               expected: "UPDATE \"app_ntfy_notifications\" SET \"id\" = \"id\" WHERE 0"
               actual  : ""
--- PASS: TestWriteLock/PostgreSQL_needs_no_lock_statement
--- PASS: TestWriteLock/MySQL_needs_no_lock_statement
```

- [ ] **Step 4: Implement `writeLock` and call it from `do`**

Replace `do` and the stub in `sqlstore/sql.go` with:

```go
// do runs fn in a transaction of the store's own.
//
// Its first statement is writeLock's. On SQLite that takes the database's
// write lock before fn reads anything, so a transaction that reads before it
// writes waits for a concurrent writer, up to the connection's busy timeout,
// instead of failing at once with "database is locked" when it tries to
// upgrade. The host's transaction mode, deferred or immediate, then does not
// matter. On other dialects the statement is zero and sends nothing.
func (s *Store) do(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.executor.Do(s.own(ctx), func(ctx context.Context) error {
		if _, err := s.executor.Exec(ctx, s.writeLock()); err != nil {
			return err
		}

		return fn(ctx)
	})
}

// writeLock is the statement that takes the database's write lock at the
// start of a store transaction: on SQLite, an update that matches no row. A
// write statement takes SQLite's write lock whatever rows it touches, and
// before the transaction holds a read snapshot. `WHERE 0` is SQLite's false;
// PostgreSQL refuses it, which makes a wrong dialect check fail loudly. Every
// other dialect gets the zero Statement: their row locks already serialise the
// store's writes.
func (s *Store) writeLock() sqlkit.Statement {
	if s.dialect.Name() != sqlkit.SQLite.Name() {
		return sqlkit.Statement{}
	}

	w := sqlkit.NewWriter(s.dialect)
	w.Write("UPDATE ", s.notificationsTable(), " SET ", s.quote("id"), " = ", s.quote("id"), " WHERE 0")

	return w.Done()
}
```

- [ ] **Step 5: Run the red tests and watch them pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestWriteLock|TestSQLiteWritesWaitForEachOther|TestStoreOnStdSQLSQLiteDeferred' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy/sqlstore`

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestStoreOnGormSQLiteDeferred' -count=1 ./internal/gormtest`
Expected: `ok  	github.com/kartaladev/ntfy/sqlstore/internal/gormtest`

Then confirm the test notices a revert. Temporarily change `do`'s body back to `return s.executor.Do(s.own(ctx), fn)` and rerun the first command. `TestSQLiteWritesWaitForEachOther` must fail with `database is locked` again. Restore the implementation.

- [ ] **Step 6: Run the whole module, with the race detector**

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -race -count=1 ./...`
Expected: every package `ok`. The PostgreSQL and MySQL entry points need Docker. If Docker is unavailable they fail to reach a container, and Task 4's `make store-matrix` must then be run where Docker is.

- [ ] **Step 7: Commit Task 1 and Task 2 together**

```bash
git add sqlstore/internal/harness/harness.go sqlstore/sqlite_writes_test.go sqlstore/harness_test.go \
        sqlstore/internal/gormtest/store_test.go sqlstore/lock_internal_test.go sqlstore/sql.go
git commit -m "Take SQLite's write lock at the start of every store transaction

MarkRead and ClaimEmails read before they write. Under SQLite's default
deferred transactions, two of them cannot both upgrade to the write lock, and
the loser failed at once with \"database is locked\" instead of waiting out
the busy timeout. Only sqlkittest's _txlock=immediate DSN hid it.

Store.do now opens every transaction with an update matching no row on
SQLite, so each waits its turn whatever mode the host's connection uses.
The conformance suites also run over a deferred DSN on database/sql and GORM.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

Use the attribution lines current in the executing session if they differ.

---

### Task 3: Wire the matrix and document

**Files:**
- Modify: `Makefile:160-170` (`STORE_MATRIX` and the `store-matrix` comment)
- Modify: `docs/schema.md` (the intro paragraph, lines 3-7; the "Transactions" section)
- Modify: `sqlstore/doc.go`

**Interfaces:**
- Consumes: `TestStoreOnStdSQLSQLiteDeferred` and `TestStoreOnGormSQLiteDeferred` from Task 1; the guarantee from Task 2.
- Produces: `make store-matrix` runs nine entries; `docs/schema.md` has a `### SQLite connections` heading.

- [ ] **Step 1: Check the matrix count before the change**

Run: `sed -n '/^STORE_MATRIX/,/^$/p' Makefile | grep -o 'TestStoreOn[A-Za-z]*' | wc -l`
Expected: `7`, the current entry count, which is the red for this step.

- [ ] **Step 2: Add the entries and reword the comment**

Replace the `STORE_MATRIX` definition and the comment above `store-matrix:` in `Makefile` with:

```make
STORE_MATRIX := sqlstore:TestStoreOnStdSQLPostgres sqlstore:TestStoreOnStdSQLMySQL \
                sqlstore:TestStoreOnStdSQLSQLite \
                sqlstore:TestStoreOnStdSQLSQLiteDeferred \
                sqlstore:TestStoreOnPgxPostgres \
                sqlstore/internal/gormtest:TestStoreOnGormPostgres \
                sqlstore/internal/gormtest:TestStoreOnGormMySQL \
                sqlstore/internal/gormtest:TestStoreOnGormSQLite \
                sqlstore/internal/gormtest:TestStoreOnGormSQLiteDeferred

## store-matrix: run the notification store conformance suite over all seven
## driver-and-dialect combinations, and over SQLite again with the driver's
## default (deferred) transaction mode on both SQLite drivers.
```

Run: `sed -n '/^STORE_MATRIX/,/^$/p' Makefile | grep -o 'TestStoreOn[A-Za-z]*' | wc -l`
Expected: `9`

- [ ] **Step 3: Document what a SQLite host must set**

In `docs/schema.md`, change the intro's last sentence from

```
implementation serves all of them, proven identical by one conformance suite on
all seven driver-and-dialect combinations.
```

to

```
implementation serves all of them, proven identical by one conformance suite on
all seven driver-and-dialect combinations, and on SQLite in both transaction
modes.
```

At the end of the "## Transactions" section, after the paragraph beginning "On SQLite, a caller holding its own write transaction", add:

````markdown
### SQLite connections

SQLite has one writer. The store takes the database's write lock with the
first statement of each of its write transactions, so every write waits for
the one before it rather than failing. That is why the connection's
transaction mode does not matter. Deferred, the driver's default, is fine, and
`_txlock=immediate` is not needed.

A SQLite host must still set:

- **A busy timeout.** It is how long a write waits for the lock. Without one,
  a write that finds another in progress fails at once with `database is
  locked`. modernc.org/sqlite sets none by default.

and should set:

- **Write-ahead logging**, so that reads, which take no write lock, are not
  blocked by a write in progress.

With modernc.org/sqlite:

```text
file:/var/lib/app/app.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)
```
````

Run: `grep -n '_txlock\|busy timeout\|### SQLite connections' docs/schema.md`
Expected: the new heading, the `_txlock=immediate is not needed` sentence and the busy-timeout bullet, all inside "Transactions".

- [ ] **Step 4: One sentence in the package doc**

Replace the last paragraph of `sqlstore/doc.go` with:

```go
// Every method runs in a transaction of its own and never joins a transaction
// the caller holds for other data. On SQLite, each write transaction takes the
// database's write lock first, so concurrent writes wait for each other up to
// the connection's busy timeout, whatever transaction mode the connection
// uses. See docs/schema.md for the connection settings a SQLite host needs.
package sqlstore
```

Run: `GOTOOLCHAIN=go1.26.8 go doc -C sqlstore .`
Expected: the package doc prints the new sentences.

- [ ] **Step 5: Run the matrix entries that need no Docker**

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run '^TestStoreOnStdSQLSQLite(Deferred)?$' -count=1 . && GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run '^TestStoreOnGormSQLite(Deferred)?$' -count=1 ./internal/gormtest`
Expected: both `ok`.

- [ ] **Step 6: Commit**

```bash
git add Makefile docs/schema.md sqlstore/doc.go
git commit -m "Run the store matrix on SQLite's default transactions and document SQLite connections

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 4: Verify and record

**Files:**
- Modify: `openspec/changes/serialise-sqlite-writes/plans.md` (append an "Execution record")
- Modify: `openspec/changes/serialise-sqlite-writes/tasks.md` (tick boxes via `/opsx:apply`)

**Interfaces:**
- Consumes: everything above.
- Produces: the recorded gate results.

- [ ] **Step 1: Run the gates**

Run: `make all`
Expected: lint, split-check and every module's tests pass.

Run: `make store-matrix`
Expected: nine `==>` headers, each followed by `ok`. Needs Docker for the PostgreSQL and MySQL entries.

Run: `make sqlkit-copy-check && git diff --stat main -- pkg/sqlkit`
Expected: the check passes, and the diff prints nothing.

- [ ] **Step 2: Record the results**

Append to this file:

```markdown
## Execution record

- <date>: `make all` ok; `make store-matrix` ok on 9 entries; `make sqlkit-copy-check` ok; pkg/sqlkit untouched.
- Red runs (Task 1 Step 3, Task 1 Step 5, Task 2 Step 3): <paste the three FAIL summaries>.
```

Fill in the real date and the real output. Then commit:

```bash
git add openspec/changes/serialise-sqlite-writes/plans.md openspec/changes/serialise-sqlite-writes/tasks.md
git commit -m "Record serialise-sqlite-writes verification

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

## Self-review

- **Spec coverage:**
  - inbox "shared suite passes on SQLite with the driver's default transactions": Task 1 Step 4, and Task 3 Step 2 wires it into the matrix;
  - "concurrent mark-read calls": Task 1 Step 2, row 1;
  - "mark-read racing a publish": row 2;
  - "documented": Task 3 Steps 3-4;
  - email "concurrent claims on SQLite": row 3, plus the email suites in the deferred matrix entries;
  - the existing scenarios: the unchanged matrix entries.
- **Rules:** a STOP step sits in Task 1 Step 3. No commit is red, because Task 1 and Task 2 land together. No option is added. `pkg/sqlkit` is untouched.
- **Names:** `DeferredSQLiteDSN`, `writeLock`, `concurrently`, `firstError`, `TestSQLiteWritesWaitForEachOther`, `TestStoreOnStdSQLSQLiteDeferred`, `TestStoreOnGormSQLiteDeferred` and `TestWriteLock` are spelled the same in `tasks.md`, `design.md` and every task above.
