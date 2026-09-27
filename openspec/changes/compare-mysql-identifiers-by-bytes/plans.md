# Byte-Exact MySQL Identifiers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make MySQL compare notification identifiers byte for byte, as PostgreSQL, SQLite and the memory store already do, and hold every store, a host's own included, to that through the conformance suite and startup schema verification.

**Architecture:**
- The MySQL DDL changes every identifier column from `VARCHAR(n) COLLATE utf8mb4_0900_as_cs` to `VARBINARY(n)`: binary strings, compared and sorted by bytes on every MySQL the library supports, with no newer server needed. Lengths become bytes, matching the core's `MaxIdentifierBytes` (255) and `MaxKindBytes` (100).
- A new `identity` conformance group in `ntfytest` fails any store that folds case, pads, strips ignorable code points or normalises.
- `pkg/sqlkit` is a vendored copy that must not be edited, so sqlstore hands `sqlkit.VerifySchema` a small `verifyDialect` wrapper. The wrapper expects the collation `binary` on MySQL only. A binary column reports no collation, which sqlkit accepts. Any character-set column reports one, which is then named. **Superseded by Task 6:** the wrapper still let `BINARY(n)` through and kept sqlkit's misleading wording, so `Store.verify` now checks MySQL identifier columns' type itself, and the wrapper is deleted.

> **Revised 2026-09-28, during Task 1:** the first version of this plan used `utf8mb4_0900_bin`, which needs MySQL 8.0.17. The maintainer ruled that a library cannot make its hosts upgrade their database, so this revision uses `VARBINARY` (`design.md` D1). Steps 1–4 were done before the revision and are unchanged; everything after them is revised.
- Existing MySQL hosts get a documented `ALTER TABLE` upgrade. A test reads that SQL from the doc and applies it.

**Tech Stack:**
- Go 1.26, with the core module on the standard library only.
- `stretchr/testify`, and `testcontainers-go` through `sqlkittest.RunTestMySQL` (MySQL 8.4.6).
- `golangci-lint` v2 and `openspec`.

**Spec:** `openspec/changes/compare-mysql-identifiers-by-bytes/`. Read `proposal.md` (why, and the failing audit output), `design.md` (D1 binary strings, D2 the verification wrapper, D3 upgrade, D4 conformance variants, D5 email columns) and `specs/notification-inbox/spec.md` (the eight scenarios this plan must satisfy).

## Global Constraints

- **Go 1.26: export `GOTOOLCHAIN=go1.26.8` before any Go command.** A newer Go may be first on `PATH`. The `make` targets already pin it.
- **The core module imports only the standard library** in production code. Tests may use `testify`, `goleak` and `go.uber.org/mock`. Enforced by `.golangci.yml` depguard and `make split-check`. This change adds no production import to the core module.
- **Each satellite module may import only `ntfy`, `sqlkit` and its own client library.** `make split-check` is authoritative.
- **`pkg/sqlkit` is never edited.** `make sqlkit-copy-check` fails on any difference from its source commit. The expected-collation fix belongs in sqlkit's own repository (Task 5).
- **Tests follow the `table-test` skill:**
  - an `assert` closure on every case, never `want`/`wantErr` fields;
  - `t.Context()`, not `context.Background()`;
  - `require` only for preconditions.
  - The identity cases and the MySQL tests do not vary context, so they carry no `ctx` field. The behaviour under test does not depend on cancellation (skill rule 3 applies only to context-sensitive behaviour). Each such table says so in a one-line comment.
- **Test doubles come from the `use-mockgen` skill.** This change needs none. The folding stores in Task 2 are deliberately broken *implementations* under test, wrapping a real `MemoryStore`. They are not doubles standing in for a collaborator.
- **External services come from the `use-testcontainers` skill.** Use `sqlkittest.RunTestMySQL(t)`, and never write a new container helper. `sqlkittest.RunTestSQLite` needs no Docker.
- **`.claude/rules/prove-errors-with-tests.md`:** each red step is run and its failing output recorded before any production edit. A red caused by compilation, a missing fixture or a container error does not count.
- **`.claude/rules/golang-tdd.md`:** red → green → refactor, and the test and the code that satisfies it land in the same commit.
- **`.claude/rules/library-design.md`:**
  - Byte identity is a guarantee with no override (`design.md` D1, D2 and D4 say why). Do not add an option for it.
  - The fix must not raise the minimum MySQL version, which stays 8.0.
- **`.claude/rules/performance-benchmark.md`:** makes no performance claim, so no benchmark.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn.
- **Done means `make all` and `make store-matrix` pass**, since a store changes.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`, and end with the session's attribution lines.

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `sqlstore/identity_test.go` | `ntfy/sqlstore` | **Create.** `TestMySQLComparesIdentifiersByBytes`: the landed audit proof, plus the NUL variants the shared suite cannot carry. |
| `ntfytest/identity.go` | `ntfy/ntfytest` | **Create.** `runIdentity` and its variant table: recipient, source, subject and kind compared byte for byte. |
| `ntfytest/suite.go:29-37` | `ntfy/ntfytest` | **Modify.** Register `identity` in `Run`. |
| `ntfytest/identity_test.go` | `ntfy/ntfytest` | **Create.** `TestIdentityRejectsFoldingStores`: proves the group rejects three folding stores, in child processes. It is the module's first test file. |
| `sqlstore/verify_test.go` | `ntfy/sqlstore` | **Modify.** Add `TestVerifySchemaRequiresByteExactIdentifiersOnMySQL`. |
| `sqlstore/verify_internal_test.go` | `ntfy/sqlstore` | **Create**, package `sqlstore`. `TestVerifyDialect` and the tripwire `TestSQLKitStillExpectsTheOldMySQLCollation`. |
| `sqlstore/store.go:184-191` | `ntfy/sqlstore` | **Modify.** Add `mysqlIdentifierCollation` and `verifyDialect`, and use it in `VerifySchema`. |
| `sqlstore/email.go:120-127` | `ntfy/sqlstore` | **Modify.** `VerifyEmailSchema` uses `verifyDialect`. |
| `sqlstore/ddl/mysql.sql` | `ntfy/sqlstore` | **Modify.** `VARBINARY(n)` for every identifier column, and the header comment. |
| `sqlstore/ddl/email/mysql.sql` | `ntfy/sqlstore` | **Modify.** The same change for the five email identifier columns, and the header comment. |
| `sqlstore/testdata/schema/{mysql,mysql_app_,email_mysql,email_mysql_app_}.sql` | `ntfy/sqlstore` | **Regenerate** with `-update`. |
| `sqlstore/email_verify_test.go` | `ntfy/sqlstore` | **Modify.** Add the `documentedBlock` helper and `TestTheDocumentedMySQLUpgradeComparesIdentifiersByBytes`. |
| `docs/schema.md` | — | **Modify.** The dialect table, the identifier bullet, and a new pre-check, upgrade and rollback section. The minimum MySQL version is unchanged. |
| `store.go:19-35` | `ntfy` | **Modify.** The `Store` godoc states byte identity. |
| `ntfytest/doc.go` | `ntfy/ntfytest` | **Modify.** The package doc names the identity property. |

**Shared-file note:** `ntfytest/suite.go`'s `Run` block is the usual registration point. Any change in flight that also adds a group conflicts textually on adjacent lines; resolve by keeping both lines.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: byte-exact MySQL identifiers, verified | 1.1, 1.2, 2.1, 2.2, 3.1, 3.2, 3.3, 3.4 |
| Task 2: the suite rejects folding stores | 1.3 |
| Task 3: the documented upgrade | 4.1, 4.2 |
| Task 4: documentation of the guarantee | 5.1, 5.2 |
| Task 5: verify and hand off | 6.1, 6.2 |
| Task 6: answer the code review | 7.1–7.8 |

Task 1 gathers eight `tasks.md` items into one commit, because none of them can land green alone:
- The new DDL fails the existing `TestVerifySchemaOnMySQL` until `verifyDialect` exists.
- `verifyDialect` fails `TestVerifySchemaOnMySQL` until the DDL changes.
- The red tests only turn green with both.

---

### Task 1: Byte-exact MySQL identifiers, verified at startup

**Files:**
- Create: `sqlstore/identity_test.go`, `ntfytest/identity.go`, `sqlstore/verify_internal_test.go`
- Modify: `ntfytest/suite.go:29-37`, `sqlstore/verify_test.go` (append), `sqlstore/store.go:184-191`, `sqlstore/email.go:120-127`, `sqlstore/ddl/mysql.sql`, `sqlstore/ddl/email/mysql.sql`
- Regenerate: `sqlstore/testdata/schema/mysql.sql`, `mysql_app_.sql`, `email_mysql.sql`, `email_mysql_app_.sql`

**Interfaces:**
- Consumes (existing):
  - In `ntfytest`:
    - `type Factory func(t *testing.T) ntfy.Store`;
    - `newEnv(t *testing.T, factory Factory) *env`, and on `*env`: `note(recipient, source, subject, kind string, version int64, created time.Time) ntfy.Notification`, `insert(coalesce bool, notifications ...ntfy.Notification) ntfy.InsertResult`, `close(req ntfy.CloseRequest, when time.Time) ntfy.CloseResult`, `get(recipient, id string) ntfy.Notification`, `list(q ntfy.ListQuery) ntfy.Page`, `count(recipient string) int64`, plus the field `store ntfy.Store`;
    - `parallel(t *testing.T, name string, fn func(t *testing.T))`, `at(n int) time.Time`.
  - In `sqlstore_test`: `openSQL(t, driver, dsn string) *sql.DB`, `stdsqlExecutor(t, db *sql.DB, dialect sqlkit.Dialect) sqlkit.Executor`, `harness.NewStore(t, executor) *sqlstore.Store`, `exec(t, executor, statement string)`, `issues(t, err error) string`.
  - `sqlkit.VerifySchema(ctx, querier, dialect sqlkit.Dialect, prefix string, expectation sqlkit.SchemaExpectation) error`.
- Produces:
  - `func runIdentity(t *testing.T, factory Factory)`, package-private and called from `Run`, with its type `identityVariant{name, of, other string}` and `func identityVariants(identifier string) []identityVariant`;
  - in package `sqlstore`: `const mysqlIdentifierCollation = "binary"` and `type verifyDialect struct{ sqlkit.Dialect }`, whose `IdentifierCollation() string` is overridden.

- [x] **Step 1: Land the MySQL proof (tasks 1.1)**

Create `sqlstore/identity_test.go`:

```go
package sqlstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/sqlstore"
	"github.com/kartaladev/ntfy/sqlstore/internal/harness"
	"github.com/kartaladev/sqlkit"
	"github.com/kartaladev/sqlkit/sqlkittest"
)

// TestMySQLComparesIdentifiersByBytes holds MySQL to byte-exact identifiers,
// including the one variant the shared suite cannot carry: a NUL byte, which
// PostgreSQL refuses to store. The suite's identity group covers the rest on
// every store. The cases do not vary context, so the table has no ctx field.
func TestMySQLComparesIdentifiersByBytes(t *testing.T) {
	t.Parallel()

	executor := stdsqlExecutor(t, openSQL(t, "mysql", sqlkittest.RunTestMySQL(t)), sqlkit.MySQL)
	created := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	type testCase struct {
		name   string
		assert func(t *testing.T, store *sqlstore.Store)
	}

	note := func(id, recipient, source string) ntfy.Notification {
		return ntfy.Notification{
			ID: id, Recipient: recipient, SourceID: source, Subject: "task-1", SubjectVersion: 1,
			Kind: "offer", State: ntfy.StateActive, Title: "secret", CreatedAt: created,
		}
	}

	insert := func(t *testing.T, store *sqlstore.Store, n ntfy.Notification) ntfy.InsertResult {
		t.Helper()

		result, err := store.Insert(t.Context(), n.Subject, []ntfy.Insertion{{Notification: n}})
		require.NoError(t, err)

		return result
	}

	recipientSeesNothing := func(owner, intruder string) func(t *testing.T, store *sqlstore.Store) {
		return func(t *testing.T, store *sqlstore.Store) {
			require.Len(t, insert(t, store, note("n-1", owner, "event-1")).Created, 1)

			got, err := store.Get(t.Context(), intruder, "n-1")
			assert.ErrorIsf(t, err, ntfy.ErrNotFound, "Get as %+q returned %+q's notification", intruder, got.Recipient)

			page, err := store.List(t.Context(), ntfy.ListQuery{Recipient: intruder})
			require.NoError(t, err)
			assert.Emptyf(t, page.Notifications, "List as %+q returned another recipient's notifications", intruder)

			count, err := store.CountActive(t.Context(), intruder)
			require.NoError(t, err)
			assert.Zerof(t, count, "CountActive as %+q counted another recipient's notification", intruder)
		}
	}

	sourceIsNew := func(source, other string) func(t *testing.T, store *sqlstore.Store) {
		return func(t *testing.T, store *sqlstore.Store) {
			require.Len(t, insert(t, store, note("n-1", "alice", source)).Created, 1)

			result := insert(t, store, note("n-2", "alice", other))
			assert.Lenf(t, result.Created, 1, "%+q is a source of its own, not a redelivery of %+q", other, source)
			assert.Zero(t, result.Duplicates)
		}
	}

	cases := []testCase{
		{name: "a zero-width space makes another recipient", assert: recipientSeesNothing("alice", "alice\u200b")},
		{name: "a NUL byte makes another recipient", assert: recipientSeesNothing("alice", "alice\x00")},
		{name: "NFD makes another recipient than NFC", assert: recipientSeesNothing("josé", "jose\u0301")},
		{name: "a zero-width space makes another source", assert: sourceIsNew("event-1", "event-1\u200b")},
		{name: "a NUL byte makes another source", assert: sourceIsNew("event-1", "event-1\x00")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, harness.NewStore(t, executor))
		})
	}
}
```

- [x] **Step 2: Watch it fail for the stated reason**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestMySQLComparesIdentifiersByBytes' -count=1 .`

Expected: FAIL on all five cases.
- The three recipient cases fail with `Get as "alice\u200b" returned "alice"'s notification` (and the NUL and NFD equivalents), then `List as ... returned another recipient's notifications` and `CountActive as ... counted`.
- The two source cases fail with `"event-1\u200b" is a source of its own, not a redelivery of "event-1"`: `Created` has length 0 and `Duplicates` is 1.

If a source case fails with a duplicate-key *error* from `require.NoError` instead, that is the same defect surfacing through the unique key. Record the exact output either way. A container or compile error is not a red; fix it and re-run.

- [x] **Step 3: Write the identity conformance group (tasks 1.2)**

Create `ntfytest/identity.go`:

```go
package ntfytest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// identityVariant is a pair of identifiers that differ only in their bytes, in
// a way some collation ignores.
type identityVariant struct {
	name  string
	of    string // the identifier published
	other string // the identifier that must not match it
}

// identityVariants returns the ways an identifier can differ from another only
// in its bytes. Every string is valid UTF-8 without NUL, so every supported
// database can store it; NUL is covered by sqlstore's MySQL test instead.
func identityVariants(identifier string) []identityVariant {
	return []identityVariant{
		{name: "differing in case", of: identifier, other: strings.ToUpper(identifier[:1]) + identifier[1:]},
		{name: "with a trailing space", of: identifier, other: identifier + " "},
		{name: "with a zero-width space", of: identifier, other: identifier + "\u200b"},
		{name: "in another normalisation form", of: identifier + "-josé", other: identifier + "-jose\u0301"},
	}
}

// runIdentity asserts that a store compares recipient, source, subject and kind
// byte for byte. The cases do not vary context, so the table has no ctx field.
func runIdentity(t *testing.T, factory Factory) {
	type testCase struct {
		name    string
		variant identityVariant
		assert  func(t *testing.T, e *env, v identityVariant)
	}

	var cases []testCase

	for _, group := range []struct {
		identifier string
		assert     func(t *testing.T, e *env, v identityVariant)
	}{
		{identifier: "alice", assert: recipientIsItsBytes},
		{identifier: "event-1", assert: sourceIsItsBytes},
		{identifier: "task-1", assert: subjectIsItsBytes},
		{identifier: "offer", assert: kindIsItsBytes},
	} {
		for _, v := range identityVariants(group.identifier) {
			cases = append(cases, testCase{name: group.identifier + " " + v.name, variant: v, assert: group.assert})
		}
	}

	for _, tc := range cases {
		parallel(t, tc.name, func(t *testing.T) {
			tc.assert(t, newEnv(t, factory), tc.variant)
		})
	}
}

// recipientIsItsBytes: another recipient reads, lists, counts and marks
// nothing of the published recipient's.
func recipientIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	published := e.note(v.of, "event-1", "task-1", "offer", 1, at(0))
	e.insert(false, published)

	_, err := e.store.Get(t.Context(), v.other, published.ID)
	require.ErrorIsf(t, err, ntfy.ErrNotFound, "reading as %+q finds %+q's notification", v.other, v.of)

	assert.Emptyf(t, e.list(ntfy.ListQuery{Recipient: v.other}).Notifications, "%+q lists %+q's notification", v.other, v.of)
	assert.Zerof(t, e.count(v.other), "%+q counts %+q's notification", v.other, v.of)

	_, err = e.store.MarkRead(t.Context(), v.other, []string{published.ID}, at(1))
	require.ErrorIsf(t, err, ntfy.ErrNotFound, "%+q marks %+q's notification read", v.other, v.of)

	stored := e.get(v.of, published.ID)
	assert.Equal(t, ntfy.StateActive, stored.State, "the published recipient's notification is untouched")
	assert.Equal(t, v.of, stored.Recipient, "the recipient is returned exactly as published")
}

// sourceIsItsBytes: a source differing only in its bytes is a new source, not
// a redelivery.
func sourceIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	require.Len(t, e.insert(false, e.note("alice", v.of, "task-1", "offer", 1, at(0))).Created, 1)

	result := e.insert(false, e.note("alice", v.other, "task-1", "offer", 1, at(1)))
	assert.Lenf(t, result.Created, 1, "%+q is taken for a redelivery of %+q", v.other, v.of)
	assert.Zero(t, result.Duplicates)
}

// subjectIsItsBytes: closing a subject leaves the one differing only in its
// bytes open, and does not suppress a later publish on it.
func subjectIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	closing := e.note("alice", "event-1", v.of, "offer", 1, at(0))
	open := e.note("alice", "event-2", v.other, "offer", 1, at(1))
	e.insert(false, closing)
	e.insert(false, open)

	e.close(ntfy.CloseRequest{Subject: v.of, Version: 5, Reason: "done"}, at(2))

	assert.Equal(t, ntfy.StateClosed, e.get("alice", closing.ID).State)
	assert.Equalf(t, ntfy.StateActive, e.get("alice", open.ID).State, "closing %+q closed %+q", v.of, v.other)

	late := e.insert(false, e.note("alice", "event-3", v.other, "offer", 2, at(3)))
	assert.Lenf(t, late.Created, 1, "closing %+q suppressed a publish on %+q", v.of, v.other)
	assert.Zero(t, late.Suppressed)
}

// kindIsItsBytes: a kind filter matches its kind exactly.
func kindIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	wanted := e.note("alice", "event-1", "task-1", v.of, 1, at(0))
	e.insert(false, wanted, e.note("alice", "event-2", "task-1", v.other, 1, at(1)))

	listed := e.list(ntfy.ListQuery{Recipient: "alice", Kinds: []string{v.of}}).Notifications
	assert.Equalf(t, []string{wanted.ID}, idsOf(listed), "a filter on %+q returned %+q too", v.of, v.other)
}
```

In `ntfytest/suite.go`, register the group after `isolation`:

```go
	t.Run("isolation", func(t *testing.T) { runIsolation(t, factory) })
	t.Run("identity", func(t *testing.T) { runIdentity(t, factory) })
```

- [x] **Step 4: Run the group on memory (green) and MySQL (red)**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/identity' -count=1 .`
Expected: PASS, 16 subtests. Memory compares Go strings, which are bytes.

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLMySQL/identity' -count=1 .`
Expected:
- FAIL on every `with a zero-width space` and `in another normalisation form` row, for all four identifiers: 8 failing rows.
- PASS on the `differing in case` and `with a trailing space` rows, because `utf8mb4_0900_as_cs` is case-sensitive and NO PAD.

Record the pass/fail split for the commit message. If a case or trailing-space row fails on MySQL, stop and run `superpowers:systematic-debugging`. That would contradict `design.md` D1's table.

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQL(Postgres|SQLite)/identity|TestStoreOnPgxPostgres/identity' -count=1 .`
Expected: PASS on PostgreSQL and SQLite.

- [x] **Step 5: Write the verification red (tasks 2.1, 2.2)**

Append to `sqlstore/verify_test.go`. Its imports already include `sqlkit` and `sqlkittest`.

```go
// TestVerifySchemaRequiresByteExactIdentifiersOnMySQL is separate from
// runVerifySchema because only MySQL can declare an identifier column whose
// comparison depends on a collation; the other dialects have no such column to
// break. The cases do not vary context, so the table has no ctx field.
func TestVerifySchemaRequiresByteExactIdentifiersOnMySQL(t *testing.T) {
	t.Parallel()

	executor := stdsqlExecutor(t, openSQL(t, "mysql", sqlkittest.RunTestMySQL(t)), sqlkit.MySQL)

	type testCase struct {
		name      string
		collation string
		assert    func(t *testing.T, store *sqlstore.Store, err error)
	}

	reportsTheRecipient := func(collation string) func(t *testing.T, store *sqlstore.Store, err error) {
		return func(t *testing.T, store *sqlstore.Store, err error) {
			assert.Contains(t, issues(t, err),
				store.Tables()[0]+`.recipient: collation is "`+collation+`" but must be "binary"`)
		}
	}

	cases := []testCase{
		{
			name:      "a collation that ignores code points and normalisation is reported",
			collation: "utf8mb4_0900_as_cs",
			assert:    reportsTheRecipient("utf8mb4_0900_as_cs"),
		},
		{
			name:      "even a byte-exact collation is reported, since identifiers are binary strings",
			collation: "utf8mb4_0900_bin",
			assert:    reportsTheRecipient("utf8mb4_0900_bin"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := harness.NewStore(t, executor)
			exec(t, executor, "ALTER TABLE `"+store.Tables()[0]+"` MODIFY `recipient` VARCHAR(255) COLLATE "+
				tc.collation+" NOT NULL")

			tc.assert(t, store, store.VerifySchema(t.Context()))
		})
	}
}
```

Create `sqlstore/verify_internal_test.go`, holding only the tripwire for now:

```go
package sqlstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kartaladev/sqlkit"
)

// TestSQLKitStillExpectsTheOldMySQLCollation is a tripwire, not a behaviour
// test. verifyDialect exists only because the vendored sqlkit expects
// utf8mb4_0900_as_cs of MySQL identifier columns; once a refreshed copy expects
// something else, this fails, and verifyDialect is revisited and deleted.
func TestSQLKitStillExpectsTheOldMySQLCollation(t *testing.T) {
	t.Parallel()

	require.Equal(t, "utf8mb4_0900_as_cs", sqlkit.MySQL.IdentifierCollation(),
		"the vendored sqlkit changed what it expects of MySQL identifier columns: revisit verifyDialect and delete it with this test")
}
```

- [x] **Step 6: Watch verification fail for the stated reason**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifySchemaRequiresByteExactIdentifiersOnMySQL|TestSQLKitStillExpectsTheOldMySQLCollation' -count=1 .`

Expected:
- The `utf8mb4_0900_as_cs` row fails inside `issues`, because verification accepts that collation and returns nil. The error is `An error is expected but got nil`, from its `require.ErrorIs` on `ErrSchemaMismatch`.
- The `utf8mb4_0900_bin` row fails with `... does not contain ".recipient: collation is \"utf8mb4_0900_bin\" but must be \"binary\""`, because the reported issue says `must be "utf8mb4_0900_as_cs"`.
- The tripwire passes.

- [x] **Step 7: Add a delegating `verifyDialect` and its red test (tasks 3.4)**

Add to `sqlstore/store.go`, just above `VerifySchema`:

```go
// mysqlIdentifierCollation is what schema verification expects of a MySQL
// identifier column: binary, MySQL's name for the collation of binary strings.
// Identifier columns are VARBINARY, which compares and sorts by bytes and pads
// nothing on every supported server, and which reports no collation at all, so
// that any column reporting a character-set collation is named.
const mysqlIdentifierCollation = "binary"

// verifyDialect is the dialect schema verification checks against. It differs
// from the store's dialect only in what it expects of MySQL identifier columns:
// the vendored sqlkit still expects utf8mb4_0900_as_cs, which ignores code
// points such as U+200B and equates NFC with NFD. It goes once sqlkit expects
// binary identifier columns itself; the tripwire
// TestSQLKitStillExpectsTheOldMySQLCollation says when.
type verifyDialect struct{ sqlkit.Dialect }

// IdentifierCollation implements [sqlkit.Dialect].
func (d verifyDialect) IdentifierCollation() string {
	return d.Dialect.IdentifierCollation()
}
```

Append to `sqlstore/verify_internal_test.go`, adding `"github.com/stretchr/testify/assert"` to its imports:

```go
func TestVerifyDialect(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		dialect sqlkit.Dialect
		assert  func(t *testing.T, got sqlkit.Dialect)
	}

	expects := func(name, collation string) func(t *testing.T, got sqlkit.Dialect) {
		return func(t *testing.T, got sqlkit.Dialect) {
			assert.Equal(t, name, got.Name(), "the dialect's name is delegated unchanged")
			assert.Equal(t, collation, got.IdentifierCollation())
		}
	}

	cases := []testCase{
		{name: "MySQL expects binary identifier columns", dialect: sqlkit.MySQL, assert: expects("mysql", "binary")},
		{name: "PostgreSQL is unchanged", dialect: sqlkit.PostgreSQL, assert: expects("postgres", "C")},
		{name: "SQLite is unchanged", dialect: sqlkit.SQLite, assert: expects("sqlite", "BINARY")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, verifyDialect{tc.dialect})
		})
	}
}
```

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifyDialect' -count=1 .`
Expected: FAIL only on `MySQL expects binary identifier columns`, with `expected: "binary" actual: "utf8mb4_0900_as_cs"`.

- [x] **Step 8: Make `verifyDialect` expect binary columns on MySQL, and use it**

Replace the method body in `sqlstore/store.go`:

```go
// IdentifierCollation implements [sqlkit.Dialect].
func (d verifyDialect) IdentifierCollation() string {
	if d.Name() == sqlkit.MySQL.Name() {
		return mysqlIdentifierCollation
	}

	return d.Dialect.IdentifierCollation()
}
```

Use it in `sqlstore/store.go`'s `VerifySchema`:

```go
func (s *Store) VerifySchema(ctx context.Context) error {
	return sqlkit.VerifySchema(s.own(ctx), s.querier, verifyDialect{s.dialect}, s.prefix, schemaExpectation)
}
```

And in `sqlstore/email.go`'s `VerifyEmailSchema`:

```go
func (s *Store) VerifyEmailSchema(ctx context.Context) error {
	return sqlkit.VerifySchema(s.own(ctx), s.querier, verifyDialect{s.dialect}, s.prefix, emailSchemaExpectation)
}
```

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifyDialect|TestVerifySchemaRequiresByteExactIdentifiersOnMySQL' -count=1 .`
Expected: PASS.

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifySchemaOnMySQL|TestVerifyEmailSchemaOnMySQL' -count=1 .`
Expected: FAIL, because the DDL still declares `VARCHAR ... COLLATE utf8mb4_0900_as_cs`. For example, the row "a freshly migrated schema, migrated twice, verifies" reports `...recipient: collation is "utf8mb4_0900_as_cs" but must be "binary"`. That is why Steps 9–10 belong in this commit.

- [x] **Step 9: Make the notification identifier columns binary (tasks 3.1)**

In `sqlstore/ddl/mysql.sql`, replace the first line and the header paragraph that begins `-- Identifier columns are pinned to utf8mb4_0900_as_cs` (through `-- for 'alice' to 'Alice' on this one dialect out of three.`) with:

```sql
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
```

Then replace the paragraph `-- Identifier columns are VARCHAR rather than TEXT because MySQL cannot index a` … `-- 3072-byte key limit at four bytes a character.` with:

```sql
-- Identifier columns are VARBINARY rather than BLOB because MySQL cannot index
-- a BLOB column without a prefix length. Every index here stays well within
-- InnoDB's 3072-byte key limit.
```

Replace the eight identifier column lines. Keep each column's `NOT NULL` and alignment:

```sql
    `id`               VARBINARY(64)  NOT NULL,
    `recipient`        VARBINARY(255) NOT NULL,
    `source_id`        VARBINARY(255) NOT NULL,
    `subject`          VARBINARY(255) NOT NULL,
```

```sql
    `kind`             VARBINARY(100) NOT NULL,
    `state`            VARBINARY(16)  NOT NULL,
```

and in `ntfy_watermarks`:

```sql
    `subject`     VARBINARY(255) NOT NULL,
    `kind`        VARBINARY(100) NOT NULL,
```

Check: `grep -c 'VARBINARY(' sqlstore/ddl/mysql.sql` prints `8`, and `grep -n 'COLLATE' sqlstore/ddl/mysql.sql` prints nothing.

- [x] **Step 10: Make the email identifier columns binary (tasks 3.2)**

In `sqlstore/ddl/email/mysql.sql`, replace the header paragraph that begins `-- Identifier columns are pinned to utf8mb4_0900_as_cs` with:

```sql
-- Identifier columns are VARBINARY and timestamps are DATETIME(6), as in the
-- notification store's schema; see that document for why. MySQL has no CREATE
-- INDEX IF NOT EXISTS, so indexes are declared inside the table.
```

Replace the five identifier column lines:

```sql
    `notification_id`  VARBINARY(64)  NOT NULL,
    `recipient`        VARBINARY(255) NOT NULL,
    `status`           VARBINARY(16)  NOT NULL,
    `batch_id`         VARBINARY(64)  NULL,
    `owner`            VARBINARY(255) NULL,
```

Check: `grep -c 'VARBINARY(' sqlstore/ddl/email/mysql.sql` prints `5`, and `grep -c 'COLLATE' sqlstore/ddl/email/mysql.sql` prints `0`.

- [x] **Step 11: Regenerate the golden schemas (tasks 3.3)**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestSchema$|TestEmailSchema' -update -count=1 .`
Then: `git diff --stat sqlstore/testdata/schema`
Expected: exactly `mysql.sql`, `mysql_app_.sql`, `email_mysql.sql` and `email_mysql_app_.sql` changed. `git diff sqlstore/testdata/schema` shows only identifier column lines and comments.

Run without `-update`: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestSchema|TestEmailSchema' -count=1 .`
Expected: PASS.

- [x] **Step 12: Run everything from Task 1 green**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestMySQLComparesIdentifiersByBytes|TestStoreOnStdSQLMySQL|TestVerifySchema|TestVerifyEmailSchema|TestVerifyDialect|TestSQLKitStillExpects|TestTheDocumentedMySQLUpgradeAddsTheEmailClaimIndex' -count=1 .`
Expected: PASS. That covers:
- all five cases of the Step 1 proof;
- all 16 identity rows and the whole store and email conformance suite on MySQL, including every read that decodes a `VARBINARY` identifier;
- `TestVerifySchemaOnMySQL` and `TestVerifyEmailSchemaOnMySQL`, which verify a fresh schema again;
- the existing claim-index upgrade test.

If a case fails with `sqlkit: cannot read ... as text`, a driver returned a type other than `[]byte` or `string` for a `VARBINARY` column. Stop and run `superpowers:systematic-debugging`. `design.md` D1 assumed this could not happen.

Run: `GOTOOLCHAIN=go1.26.8 go test -count=1 ./...` in the root, and `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -count=1 ./...`
Expected: PASS. This includes `internal/gormtest`, whose GORM MySQL run migrates the new DDL and decodes through GORM.

- [x] **Step 13: Refactor pass**

Consider `/simplify` on the touched files (`.claude/rules/golang-tdd.md`), then re-run Step 12. Run `cd sqlstore && golangci-lint run ./...` and `cd ntfytest && golangci-lint run ./...`, expecting `0 issues.`

- [x] **Step 14: Commit**

```bash
git add sqlstore/identity_test.go sqlstore/verify_test.go sqlstore/verify_internal_test.go \
        sqlstore/store.go sqlstore/email.go sqlstore/ddl/mysql.sql sqlstore/ddl/email/mysql.sql \
        sqlstore/testdata/schema ntfytest/identity.go ntfytest/suite.go
git commit -m "Compare MySQL identifiers byte for byte

MySQL's utf8mb4_0900_as_cs ignored U+200B and NUL and equated NFC with NFD,
so alice\u200b read alice's notifications. Identifier columns are now
VARBINARY, byte-exact on every supported MySQL, VerifySchema requires binary
identifier columns, and the conformance suite holds every store to
byte-exact recipient, source, subject and kind.

Before: <paste the Step 4 MySQL pass/fail split>

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

The `<paste …>` marker is the one thing filled in at execution time: it is the observed output of Step 4.

---

### Task 2: The suite rejects stores that fold identifiers

**Files:**
- Create: `ntfytest/identity_test.go`
- Test: the same file

**Interfaces:**
- Consumes: `ntfytest.Run(t *testing.T, factory ntfytest.Factory)`, `ntfy.NewMemoryStore() *ntfy.MemoryStore`, and the `ntfy.Store` method set: `Insert`, `Close`, `Get`, `List`, `CountActive`, `MarkRead`, `MarkAllRead`, `Prune`. Also `ntfy.CloseRequest{Subject, Kinds, Except, SuccessorSkip, Successor *ntfy.Successor}`, with `ntfy.Successor{SourceID, Kind}` and `ntfy.ListQuery{Recipient, Kinds, Subject}`.
- Produces: nothing other tasks use.

- [x] **Step 1: Write the proof**

Create `ntfytest/identity_test.go`:

```go
package ntfytest_test

// The conformance suite fails its own test when a store breaks it, so the only
// way to assert that it rejects a store is to run it in a child process and
// watch that process fail.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

// identityChildEnv names the fold a child process applies, and marks a process
// as a child.
const identityChildEnv = "NTFYTEST_IDENTITY_CHILD"

// folds are the ways a store can merge identifiers that differ in their bytes.
var folds = map[string]func(string) string{
	"lower-casing":                strings.ToLower,
	"trimming trailing spaces":    func(s string) string { return strings.TrimRight(s, " ") },
	"stripping zero-width spaces": func(s string) string { return strings.ReplaceAll(s, "\u200b", "") },
}

// foldingStore is a memory store that folds every identifier it is given, the
// way a database column with the wrong collation compares them.
type foldingStore struct {
	ntfy.Store
	fold func(string) string
}

func (s foldingStore) all(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = s.fold(value)
	}

	return out
}

func (s foldingStore) Insert(ctx context.Context, subject string, in []ntfy.Insertion) (ntfy.InsertResult, error) {
	out := make([]ntfy.Insertion, len(in))
	for i, insertion := range in {
		n := &insertion.Notification
		n.Recipient, n.SourceID, n.Subject, n.Kind = s.fold(n.Recipient), s.fold(n.SourceID), s.fold(n.Subject), s.fold(n.Kind)
		out[i] = insertion
	}

	return s.Store.Insert(ctx, s.fold(subject), out)
}

func (s foldingStore) Close(ctx context.Context, req ntfy.CloseRequest, at time.Time, ids ntfy.IDGenerator) (ntfy.CloseResult, error) {
	req.Subject, req.Except = s.fold(req.Subject), s.fold(req.Except)
	req.Kinds, req.SuccessorSkip = s.all(req.Kinds), s.all(req.SuccessorSkip)

	return s.Store.Close(ctx, req, at, ids)
}

func (s foldingStore) Get(ctx context.Context, recipient, id string) (ntfy.Notification, error) {
	return s.Store.Get(ctx, s.fold(recipient), id)
}

func (s foldingStore) List(ctx context.Context, q ntfy.ListQuery) (ntfy.Page, error) {
	q.Recipient, q.Subject, q.Kinds = s.fold(q.Recipient), s.fold(q.Subject), s.all(q.Kinds)

	return s.Store.List(ctx, q)
}

func (s foldingStore) CountActive(ctx context.Context, recipient string) (int64, error) {
	return s.Store.CountActive(ctx, s.fold(recipient))
}

func (s foldingStore) MarkRead(ctx context.Context, recipient string, ids []string, at time.Time) (ntfy.MarkResult, error) {
	return s.Store.MarkRead(ctx, s.fold(recipient), ids, at)
}

func (s foldingStore) MarkAllRead(ctx context.Context, recipient string, through, at time.Time) (ntfy.MarkResult, error) {
	return s.Store.MarkAllRead(ctx, s.fold(recipient), through, at)
}

// TestIdentityChild runs the identity group against the store its environment
// names. It does nothing unless TestIdentityRejectsFoldingStores started it.
func TestIdentityChild(t *testing.T) {
	mode, ok := os.LookupEnv(identityChildEnv)
	if !ok {
		t.Skip("runs only as a child of TestIdentityRejectsFoldingStores")
	}

	ntfytest.Run(t, func(*testing.T) ntfy.Store {
		if fold, folding := folds[mode]; folding {
			return foldingStore{Store: ntfy.NewMemoryStore(), fold: fold}
		}

		return ntfy.NewMemoryStore()
	})
}

// runIdentityChild runs the identity group in a child process folding by mode,
// and reports whether it passed and what it printed.
func runIdentityChild(t *testing.T, mode string) (passed bool, output string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run", "^TestIdentityChild$/^identity$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), identityChildEnv+"="+mode)
	out, err := cmd.CombinedOutput()

	return err == nil, string(out)
}

// TestIdentityRejectsFoldingStores proves the identity group fails a store that
// merges identifiers, and passes one that does not. The cases do not vary
// context, so the table has no ctx field.
func TestIdentityRejectsFoldingStores(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		mode   string
		assert func(t *testing.T, passed bool, output string)
	}

	rejected := func(t *testing.T, passed bool, output string) {
		assert.Falsef(t, passed, "ntfytest.Run accepted a store that folds identifiers:\n%s", output)
	}

	cases := []testCase{
		{
			name: "a sound store passes",
			mode: "sound",
			assert: func(t *testing.T, passed bool, output string) {
				assert.Truef(t, passed, "ntfytest.Run failed the memory store:\n%s", output)
				assert.Contains(t, output, "--- PASS: TestIdentityChild/identity/alice_differing_in_case",
					"the child ran the identity group, so passing is not vacuous")
			},
		},
	}

	for mode := range folds {
		cases = append(cases, testCase{name: "a store " + mode + " fails", mode: mode, assert: rejected})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			passed, output := runIdentityChild(t, tc.mode)
			tc.assert(t, passed, output)
		})
	}
}
```

- [x] **Step 2: Run it**

Run: `cd ntfytest && GOTOOLCHAIN=go1.26.8 go test -run 'TestIdentityRejectsFoldingStores' -count=1 .`
Expected: PASS on all four rows. Task 1 already added the group, so this test proves the group rather than driving it.

- [x] **Step 3: Invert and watch it go red**

Comment out the `t.Run("identity", ...)` line in `ntfytest/suite.go`. Re-run the command from Step 2.
Expected:
- FAIL on the three folding rows, with `ntfytest.Run accepted a store that folds identifiers`. With the group removed, the child runs no subtests and exits 0.
- `a sound store passes` also fails, on its non-vacuity check (`the child ran the identity group`). A child that ran no identity rows proves nothing.

Restore the line and re-run Step 2 to confirm PASS. Do not commit the inversion.

- [x] **Step 4: Lint and commit**

Run: `cd ntfytest && golangci-lint run ./...`, expecting `0 issues.`

```bash
git add ntfytest/identity_test.go
git commit -m "Prove the conformance suite rejects stores that fold identifiers

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 3: The documented upgrade for an existing MySQL host

**Files:**
- Modify: `docs/schema.md` (a new section after "## Adding the email claim index to an existing host", before "## Rolling back")
- Modify: `sqlstore/email_verify_test.go` (append the helper and test; add `"time"` and `"github.com/kartaladev/ntfy"` to its imports)

**Interfaces:**
- Consumes: `harness.NewEmailStore(t, executor) *sqlstore.Store`, `prefixOf(store) string`, `exec(t, executor, statement)`, `issues(t, err) string`, `(*sqlstore.Store).Insert/Get/VerifySchema/VerifyEmailSchema`, and `openSQL`, which returns the `*sql.DB` used for the pre-check query.
- Produces: `func documentedBlock(t *testing.T, heading, prefix string) []string`. It returns the statements of the first fenced `sql` block under a heading line of `docs/schema.md`, with comment lines dropped and `app_` replaced by the prefix.

- [x] **Step 1: Write the failing upgrade test**

Append to `sqlstore/email_verify_test.go`:

```go
// documentedBlock reads the statements of the first fenced sql block under a
// heading line of docs/schema.md, comment lines dropped and the documented app_
// prefix replaced by the store's, so that the upgrade a host runs is the one
// the test ran.
func documentedBlock(t *testing.T, heading, prefix string) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "docs", "schema.md"))
	require.NoError(t, err)

	document := string(raw)

	start := strings.Index(document, "\n"+heading+"\n")
	require.GreaterOrEqualf(t, start, 0, "docs/schema.md has the heading %q", heading)

	body := document[start:]

	open := strings.Index(body, "```sql\n")
	require.GreaterOrEqualf(t, open, 0, "%q is followed by an sql block", heading)

	body = body[open+len("```sql\n"):]

	end := strings.Index(body, "\n```")
	require.GreaterOrEqualf(t, end, 0, "the sql block under %q is closed", heading)

	var kept []string

	for line := range strings.SplitSeq(body[:end], "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			kept = append(kept, line)
		}
	}

	var statements []string

	for statement := range strings.SplitSeq(strings.Join(kept, "\n"), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			statements = append(statements, strings.ReplaceAll(statement, "app_", prefix))
		}
	}

	require.NotEmptyf(t, statements, "the sql block under %q holds statements", heading)

	return statements
}

// TestTheDocumentedMySQLUpgradeComparesIdentifiersByBytes puts a populated
// schema back on the old columns with the documented rollback, requires it to
// fail verification, runs the documented pre-check and upgrade, and requires the
// schema to verify and to compare recipients byte for byte.
func TestTheDocumentedMySQLUpgradeComparesIdentifiersByBytes(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "mysql", sqlkittest.RunTestMySQL(t))
	executor := stdsqlExecutor(t, db, sqlkit.MySQL)
	store := harness.NewEmailStore(t, executor)
	prefix := prefixOf(store)

	alices := ntfy.Notification{
		ID: "n-1", Recipient: "alice", SourceID: "event-1", Subject: "task-1", SubjectVersion: 1,
		Kind: "offer", State: ntfy.StateActive, CreatedAt: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
	}
	_, err := store.Insert(t.Context(), "task-1", []ntfy.Insertion{{Notification: alices}})
	require.NoError(t, err)

	for _, statement := range documentedBlock(t, "### Rolling the upgrade back", prefix) {
		exec(t, executor, statement)
	}

	assert.Contains(t, issues(t, store.VerifySchema(t.Context())),
		prefix+`ntfy_notifications.recipient: collation is "utf8mb4_0900_as_cs" but must be "binary"`,
		"the old columns fail verification")
	assert.Contains(t, issues(t, store.VerifyEmailSchema(t.Context())),
		prefix+`ntfy_email_deliveries.owner: collation is "utf8mb4_0900_as_cs" but must be "binary"`)

	for _, check := range documentedBlock(t, "### Checking before upgrading", prefix) {
		rows, err := db.QueryContext(t.Context(), check)
		require.NoErrorf(t, err, "run the documented pre-check %q", check)

		assert.False(t, rows.Next(), "the pre-check finds no identifier the upgrade would refuse")
		require.NoError(t, rows.Close())
	}

	for _, statement := range documentedBlock(t, "### Upgrading", prefix) {
		exec(t, executor, statement)
	}

	assert.NoError(t, store.VerifySchema(t.Context()))
	assert.NoError(t, store.VerifyEmailSchema(t.Context()))

	_, err = store.Get(t.Context(), "alice\u200b", "n-1")
	assert.ErrorIs(t, err, ntfy.ErrNotFound, "after the upgrade another recipient reads nothing of alice's")

	got, err := store.Get(t.Context(), "alice", "n-1")
	require.NoError(t, err, "the upgrade keeps alice's notification")
	assert.Equal(t, "alice", got.Recipient)
}
```

- [x] **Step 2: Watch it fail**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentedMySQLUpgradeComparesIdentifiersByBytes' -count=1 .`
Expected: FAIL with `docs/schema.md has the heading "### Rolling the upgrade back"`. The defect being fixed is that no documented upgrade exists, so the missing section is the right red.

- [x] **Step 3: Write the section (tasks 4.1)**

Insert into `docs/schema.md`, directly before `## Rolling back`, so that it follows "Adding the email claim index to an existing host":

````markdown
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
````

- [x] **Step 4: Watch it pass**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentedMySQLUpgrade' -count=1 .`
Expected: PASS for both upgrade tests.

The existing `TestTheDocumentedMySQLUpgradeAddsTheEmailClaimIndex` finds its statement with `documentedStatement(t, "ALTER TABLE", …)`, which returns the **first** line of `docs/schema.md` starting with `ALTER TABLE`. The new section comes after the claim-index section, so that first line is still the claim-index statement. If that test fails with a MySQL syntax error on ``ALTER TABLE `…ntfy_notifications` `` (a multi-line statement cut at one line), the new section was placed too early. Move it after the claim-index section.

- [x] **Step 5: Commit**

```bash
git add docs/schema.md sqlstore/email_verify_test.go
git commit -m "Document and prove the MySQL upgrade to byte-exact identifiers

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 4: Document the guarantee

**Files:**
- Modify: `docs/schema.md` (the dialect table's identifier row, and the "Identifiers compare…" bullet)
- Modify: `store.go:19-35` (the `Store` godoc)
- Modify: `ntfytest/doc.go`

**Interfaces:** none. This task changes only documentation.

- [x] **Step 1: Update `docs/schema.md` (tasks 5.1)**

Leave `| MySQL 8.0+ | …` as it is: the minimum does not change.

Replace the dialect table's identifier row with:

```markdown
| Identifier columns | `text COLLATE "C"` | `VARBINARY` | `TEXT COLLATE BINARY` |
```

Replace the bullet beginning `- **Identifiers compare case-sensitively and sort in byte order**`, including its continuation lines, with:

```markdown
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
```

Check: `grep -rn '0900_as_cs' docs sqlstore/ddl` prints only three kinds of line: the "Rolling the upgrade back" block, the upgrade section's opening sentence, and the explanatory mentions in the bullet and in `sqlstore/ddl/mysql.sql`'s header. None presents it as the current column definition.

- [x] **Step 2: Update the godoc (tasks 5.2)**

In `store.go`, insert this paragraph into the `Store` godoc directly after the paragraph ending "…already stamped.":

```go
// A store compares identifiers byte for byte. Two recipients, sources,
// subjects or kinds are the same only when their bytes are: a store does not
// fold case, ignore trailing spaces or ignorable code points, or normalise, and
// it returns every identifier exactly as it was given. ntfytest asserts this on
// every store.
//
```

In `ntfytest/doc.go`, append a paragraph after the isolation paragraph, keeping `package ntfytest` last:

```go
//
// It also asserts identity: a store compares recipients, sources, subjects and
// kinds byte for byte, so identifiers differing only in case, a trailing space,
// an ignorable code point or their Unicode normalisation are different.
```

- [x] **Step 3: Verify the docs and commit**

Run `GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy.Store | head -30`. The byte-identity paragraph should show.
Run `cd ntfytest && GOTOOLCHAIN=go1.26.8 go doc .`. The identity paragraph should show.
Run `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .`, then `golangci-lint run ./...` in the root and in `ntfytest`. Expect PASS and `0 issues.`

```bash
git add docs/schema.md store.go ntfytest/doc.go
git commit -m "Document that every store compares identifiers byte for byte

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 5: Verify and hand off

**Files:**
- Modify: this `plans.md` (the execution record below)
- Modify: `tasks.md` (tick every box)

**Interfaces:** none.

- [x] **Step 1: Run the gates (tasks 6.1)**

Run: `make all`
Expected: every module lints with `0 issues.`, split-check passes, and every module's tests pass.

Run: `make store-matrix`
Expected: the store conformance suite, including `identity`, passes on all seven driver-and-dialect combinations.

Run: `make sqlkit-copy-check`
Expected: no diff. `pkg/sqlkit` is untouched.

If `redis` `TestListen/cancelling_stops_listening_and_unsubscribes` fails, it is the known timing race from the 2026-09-28 audit and is unrelated to this change. Re-run the module once. If it fails again, record that here and do not fix it in this change.

- [x] **Step 2: Record the sqlkit follow-up (tasks 6.2)**

Fill the execution record below with the actual outputs, and leave this issue text there for the maintainer to file in sqlkit's repository:

```markdown
**sqlkit: MySQL identifier columns should compare byte for byte**

`mysql.IdentifierCollation()` returns `utf8mb4_0900_as_cs`. That collation
ignores code points such as U+200B and U+0000 and equates NFC with NFD, so
identifier columns declared with it merge strings the PostgreSQL ("C") and
SQLite (BINARY) dialects keep apart. ntfy proved a cross-recipient read with it
(`TestMySQLComparesIdentifiersByBytes`).

ntfy's fix keeps MySQL 8.0 as the floor by declaring identifier columns
VARBINARY (binary strings: byte comparison, no padding, no collation), rather
than utf8mb4_0900_bin, which needs 8.0.17.

Asked for:
1. MySQL identifier columns are checked by type: `DATA_TYPE` must be
   `varbinary`. Checking collation cannot do it. A binary string reports no
   collation, and today any column reporting none passes, including `BINARY(n)`,
   which pads with NUL so a stored identifier stops matching, and `BLOB`.
2. The issue for a wrong identifier column names its declared type and
   collation, and says identifiers "will not compare byte for byte" rather than
   "will compare case-insensitively", which is wrong for every case-sensitive
   collation.
3. The `sqlkittest` MySQL fixtures declare identifier columns VARBINARY.

ntfy carries this today as `Store.verify` in `sqlstore/verify.go`. It asks
sqlkit nothing about MySQL identifier collation, and reads the columns' types
itself. Once a sqlkit release with the above is copied into ntfy's
`pkg/sqlkit`, ntfy hands the check back and deletes its own; the tripwire
`TestSQLKitStillExpectsTheOldMySQLCollation` fails first.
```

- [x] **Step 3: Tick `tasks.md` and commit**

Tick every box in `tasks.md` that the steps above completed. Add anything that turned out differently from this plan to the `> **Revised …**` block at the top.

```bash
git add openspec/changes/compare-mysql-identifiers-by-bytes/tasks.md openspec/changes/compare-mysql-identifiers-by-bytes/plans.md
git commit -m "Record the byte-exact identifier change as built

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

Then continue with `.claude/rules/development-workflow.md` step 5: `/code-review`, then `/opsx:archive compare-mysql-identifiers-by-bytes`.

### Task 6: Answer the code review (tasks 7.1–7.8)

> Added 2026-09-28, after `/code-review high` returned nine findings. Each was checked against the code before being accepted (`superpowers:receiving-code-review`). The maintainer chose to fix #2 (column types) and #6 (identifier length) here rather than defer them. #8 is resolved by #2's design. #3 is resolved for MySQL by #2, with the rest going to the sqlkit issue.

**Files:**
- Create: `sqlstore/verify.go`
- Modify: `id.go`, `notification.go`, `service.go`, `service_test.go`, `sqlstore/store.go`, `sqlstore/email.go`, `sqlstore/verify_test.go`, `sqlstore/verify_internal_test.go`, `sqlstore/email_verify_test.go`, `ntfytest/identity.go`, `ntfytest/identity_test.go`, `docs/schema.md`
- Delete: `verifyDialect` and `TestVerifyDialect` (from Task 1), and `documentedStatement`

**Interfaces:**
- Produces:
  - `ntfy.MaxIDBytes = 64`;
  - unexported `boundedIDs struct{ IDGenerator }`, which the service wraps around every generator;
  - `func (s *Store) verify(ctx context.Context, expectation sqlkit.SchemaExpectation) error`, which both verification methods call;
  - `const mysqlIdentifierType = "varbinary"`;
  - `func documentedBlock(t *testing.T, line, prefix string) []string`, keyed by any line of `docs/schema.md`;
  - `const mysqlClaimIndexLine`.
- Consumes: `sqlkit.NewWriter`, `(*sqlkit.Writer).Write/BindAll/Done`, `sqlkit.Querier.QueryStatement`, `sqlkit.DecodeString`, `sqlkit.SchemaError`, `sqlkit.SchemaIssue`.

- [x] **Step 1: Prove an over-long or empty generated identifier is accepted (7.1, red)**

Append to `service_test.go`, adding `"strings"` to its imports:

```go
// TestServiceRefusesIdentifiersNoStoreCanHold holds a host's IDGenerator to
// what every store can hold: an identifier of 1 to MaxIDBytes bytes. The cases
// do not vary context, so the table has no ctx field.
func TestServiceRefusesIdentifiersNoStoreCanHold(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		id     string
		seeded bool // alice already has a notification on task-1, minted soundly
		act    func(t *testing.T, svc *ntfy.Service) error
		assert func(t *testing.T, store ntfy.Store, err error)
	}

	publish := func(t *testing.T, svc *ntfy.Service) error {
		_, err := svc.Publish(t.Context(), ntfy.Draft{Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer"})

		return err
	}

	// closeWithSuccessor closes task-1 with a successor, whose identifier the
	// service under test mints.
	closeWithSuccessor := func(t *testing.T, svc *ntfy.Service) error {
		_, err := svc.Close(t.Context(), ntfy.CloseRequest{
			Subject: "task-1", Version: 5, Reason: "taken",
			Successor: &ntfy.Successor{SourceID: "event-5", Kind: "taken", SubjectVersion: 5},
		})

		return err
	}

	refused := func(t *testing.T, store ntfy.Store, err error) {
		require.ErrorIs(t, err, ntfy.ErrConfiguration)
		assert.ErrorContains(t, err, "ID generator")

		page, err := store.List(t.Context(), ntfy.ListQuery{Recipient: "alice"})
		require.NoError(t, err)
		assert.Empty(t, page.Notifications, "nothing is written under an identifier no store can hold")
	}

	cases := []testCase{
		{name: "an identifier one byte too long is refused", id: strings.Repeat("x", ntfy.MaxIDBytes+1), act: publish, assert: refused},
		{name: "an empty identifier is refused", id: "", act: publish, assert: refused},
		{
			name: "an identifier of exactly the limit is stored",
			id:   strings.Repeat("x", ntfy.MaxIDBytes),
			act:  publish,
			assert: func(t *testing.T, store ntfy.Store, err error) {
				require.NoError(t, err)

				got, err := store.Get(t.Context(), "alice", strings.Repeat("x", ntfy.MaxIDBytes))
				require.NoError(t, err)
				assert.Equal(t, "alice", got.Recipient)
			},
		},
		{
			name:   "a successor's identifier is held to the limit too",
			id:     strings.Repeat("x", ntfy.MaxIDBytes+1),
			seeded: true,
			act:    closeWithSuccessor,
			assert: func(t *testing.T, _ ntfy.Store, err error) {
				require.ErrorIs(t, err, ntfy.ErrConfiguration)
				assert.ErrorContains(t, err, "ID generator")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := ntfy.NewMemoryStore()

			if tc.seeded {
				seed, err := ntfy.New(store)
				require.NoError(t, err)

				_, err = seed.Publish(t.Context(),
					ntfy.Draft{Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer", SubjectVersion: 1})
				require.NoError(t, err)
			}

			svc, err := ntfy.New(store, ntfy.WithIDGenerator(ntfy.IDGeneratorFunc(func() (string, error) { return tc.id, nil })))
			require.NoError(t, err)

			tc.assert(t, store, tc.act(t, svc))
		})
	}
}
```

Add to the identifier limits in `notification.go`, so the test compiles and fails on behaviour:

```go
	// MaxIDBytes is the longest notification identifier an [IDGenerator] may
	// mint.
	MaxIDBytes = 64
```

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestServiceRefusesIdentifiersNoStoreCanHold' -count=1 .`
Observed: FAIL on the too-long, empty and successor rows, with `Expected error with "ntfy: invalid configuration" in chain but got nil`. The exact-limit row passes.

- [x] **Step 2: Bound every generated identifier (7.1, green)**

Append to `id.go`:

```go
// boundedIDs holds a generator to what every store can hold. An identifier
// that is empty or longer than [MaxIDBytes] would be truncated by one database
// and refused by another; it fails the write that asked for it instead, the
// same way on every store.
type boundedIDs struct{ IDGenerator }

// NewID implements [IDGenerator].
func (g boundedIDs) NewID() (string, error) {
	id, err := g.IDGenerator.NewID()
	if err != nil {
		return "", err
	}

	if id == "" || len(id) > MaxIDBytes {
		return "", &ConfigurationError{Detail: fmt.Sprintf(
			"the ID generator minted an identifier of %d bytes; it must be 1 to %d", len(id), MaxIDBytes)}
	}

	return id, nil
}
```

```go
// NewID implements [IDGenerator].
func (g boundedIDs) NewID() (string, error) {
	id, err := g.IDGenerator.NewID()
	if err != nil {
		return "", err
	}

	if id == "" || len(id) > MaxIDBytes {
		return "", &ConfigurationError{Detail: fmt.Sprintf(
			"the ID generator minted an identifier of %d bytes; it must be 1 to %d", len(id), MaxIDBytes)}
	}

	return id, nil
}
```

In `New` (`service.go`), after the limits are validated:

```go
	// Checked per identifier rather than probed here: minting one to look at
	// would spend a value of a host generator backed by a sequence.
	svc.ids = boundedIDs{svc.ids}
```

`WithIDGenerator`'s godoc and `IDGenerator.NewID`'s doc now state the 1–`MaxIDBytes` bound and the `ConfigurationError`.

Run `go test -race -count=1 ./...` in the root. Observed: `ok`.

- [x] **Step 3: Prove verification accepts `BINARY(n)` and misreports (7.2, red)**

Replace `TestVerifySchemaRequiresByteExactIdentifiersOnMySQL` in `sqlstore/verify_test.go` with:

```go
// TestVerifySchemaRequiresByteExactIdentifiersOnMySQL is separate from
// runVerifySchema because only MySQL can declare an identifier column that does
// not compare byte for byte; the other dialects have no such column to break.
// The cases do not vary context, so the table has no ctx field.
func TestVerifySchemaRequiresByteExactIdentifiersOnMySQL(t *testing.T) {
	t.Parallel()

	executor := stdsqlExecutor(t, openSQL(t, "mysql", sqlkittest.RunTestMySQL(t)), sqlkit.MySQL)

	type testCase struct {
		name   string
		column string // the recipient column's new definition
		assert func(t *testing.T, store *sqlstore.Store, err error)
	}

	reportsTheRecipient := func(declared string) func(t *testing.T, store *sqlstore.Store, err error) {
		return func(t *testing.T, store *sqlstore.Store, err error) {
			listed := issues(t, err)
			assert.Contains(t, listed, store.Tables()[0]+`.recipient: type is `+declared+` but must be VARBINARY`)
			assert.NotContains(t, listed, "case-insensitively", "the issue says what is actually wrong")
		}
	}

	cases := []testCase{
		{
			name:   "a collation that ignores code points and normalisation is reported",
			column: "VARCHAR(255) COLLATE utf8mb4_0900_as_cs",
			assert: reportsTheRecipient(`"varchar(255)" collated "utf8mb4_0900_as_cs"`),
		},
		{
			name:   "even a byte-exact collation is reported, since identifiers are binary strings",
			column: "VARCHAR(255) COLLATE utf8mb4_0900_bin",
			assert: reportsTheRecipient(`"varchar(255)" collated "utf8mb4_0900_bin"`),
		},
		{
			name:   "a fixed-width binary string, which pads with NUL, is reported",
			column: "BINARY(255)",
			assert: reportsTheRecipient(`"binary(255)"`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := harness.NewStore(t, executor)
			exec(t, executor, "ALTER TABLE `"+store.Tables()[0]+"` MODIFY `recipient` "+tc.column+" NOT NULL")

			tc.assert(t, store, store.VerifySchema(t.Context()))
		})
	}
}
```

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifySchemaRequiresByteExactIdentifiersOnMySQL' -count=1 .`
Observed:
- the `BINARY(255)` row fails with `An error is expected but got nil`;
- both `VARCHAR` rows fail with `... collation is "utf8mb4_0900_as_cs" but must be "binary", or identifiers will compare case-insensitively" does not contain "... type is \"varchar(255)\" ..."` and `should not contain "case-insensitively"`.

- [x] **Step 4: Check MySQL identifier columns' type in sqlstore (7.2, green)**

Create `sqlstore/verify.go`:

```go
package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/kartaladev/sqlkit"
)

// mysqlIdentifierType is the only type a MySQL identifier column may have.
// VARBINARY compares and sorts by bytes and pads nothing, on every supported
// server. A character-set column compares by collation, and even a byte-exact
// collation such as utf8mb4_0900_bin needs MySQL 8.0.17; BINARY pads with NUL,
// so a stored identifier stops matching the one it was stored as.
const mysqlIdentifierType = "varbinary"

// verify compares the live schema with an expectation, reporting every
// discrepancy in one [*sqlkit.SchemaError].
//
// On MySQL, sqlkit cannot judge identifier columns: it checks their collation,
// which a binary string does not have and which cannot tell VARBINARY from
// BINARY or BLOB, and it still expects utf8mb4_0900_as_cs. So sqlkit checks
// everything else, and the store checks the identifier columns' type itself.
// That split goes once sqlkit checks MySQL identifier columns as binary strings;
// the tripwire TestSQLKitStillExpectsTheOldMySQLCollation says when to look.
func (s *Store) verify(ctx context.Context, expectation sqlkit.SchemaExpectation) error {
	ctx = s.own(ctx)

	if s.dialect.Name() != sqlkit.MySQL.Name() {
		return sqlkit.VerifySchema(ctx, s.querier, s.dialect, s.prefix, expectation)
	}

	err := sqlkit.VerifySchema(ctx, s.querier, s.dialect, s.prefix, withoutIdentifierColumns(expectation))

	var schema *sqlkit.SchemaError
	if err != nil && !errors.As(err, &schema) {
		return err
	}

	typed, typeErr := s.identifierTypeIssues(ctx, expectation)
	if typeErr != nil {
		return typeErr
	}

	if len(typed) == 0 {
		return err
	}

	if schema == nil {
		schema = &sqlkit.SchemaError{Dialect: s.dialect.Name()}
	}

	schema.Issues = append(typed, schema.Issues...)

	return schema
}

// withoutIdentifierColumns returns an expectation that asks sqlkit nothing about
// identifier columns' collation, and everything else unchanged.
func withoutIdentifierColumns(expectation sqlkit.SchemaExpectation) sqlkit.SchemaExpectation {
	out := make(sqlkit.SchemaExpectation, len(expectation))
	for table, expected := range expectation {
		expected.IdentifierColumns = nil
		out[table] = expected
	}

	return out
}

// identifierTypeIssues reports each present MySQL identifier column whose type
// is not VARBINARY. A missing table or column is sqlkit's to report.
func (s *Store) identifierTypeIssues(
	ctx context.Context, expectation sqlkit.SchemaExpectation,
) ([]sqlkit.SchemaIssue, error) {
	names := slices.Sorted(maps.Keys(expectation))

	identifiers := make(map[string][]string, len(names))
	tables := make([]any, 0, len(names))

	for _, name := range names {
		identifiers[s.prefix+name] = expectation[name].IdentifierColumns
		tables = append(tables, s.prefix+name)
	}

	w := sqlkit.NewWriter(s.dialect)
	w.Write("SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, COALESCE(COLLATION_NAME, '') ")
	w.Write("FROM information_schema.COLUMNS ")
	w.Write("WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME IN (", w.BindAll(tables...), ") ")
	w.Write("ORDER BY TABLE_NAME, ORDINAL_POSITION")
	statement := w.Done()

	rows, err := s.querier.QueryStatement(ctx, statement.SQL, statement.Args...)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: read the live identifier column types: %w", err)
	}

	var (
		issues []sqlkit.SchemaIssue
		raw    = make([]any, 5)
		dest   = make([]any, 5)
		values = make([]string, 5)
	)

	for i := range raw {
		dest[i] = &raw[i]
	}

	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("sqlstore: scan an identifier column type: %w", err)
		}

		for i, value := range raw {
			if values[i], err = sqlkit.DecodeString(value); err != nil {
				return nil, err
			}
		}

		table, column, dataType, columnType, collation := values[0], values[1], values[2], values[3], values[4]
		if dataType == mysqlIdentifierType || !slices.Contains(identifiers[table], column) {
			continue
		}

		declared := fmt.Sprintf("%q", columnType)
		if collation != "" {
			declared += fmt.Sprintf(" collated %q", collation)
		}

		issues = append(issues, sqlkit.SchemaIssue{
			Table: table, Column: column,
			Detail: "type is " + declared + " but must be VARBINARY, or identifiers will not compare byte for byte",
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlstore: read the identifier column types: %w", err)
	}

	return issues, nil
}
```

Delete `mysqlIdentifierCollation`, `verifyDialect` and its method from `sqlstore/store.go`. Make both verification methods call `verify`:

```go
func (s *Store) VerifySchema(ctx context.Context) error {
	return s.verify(ctx, schemaExpectation)
}
```

```go
func (s *Store) VerifyEmailSchema(ctx context.Context) error {
	return s.verify(ctx, emailSchemaExpectation)
}
```

Replace `sqlstore/verify_internal_test.go` with only the tripwire, reworded:

```go

// TestSQLKitStillExpectsTheOldMySQLCollation is a tripwire, not a behaviour
// test. The store checks MySQL identifier columns' type itself, in verify,
// only because the vendored sqlkit expects utf8mb4_0900_as_cs of them. Once a
// refreshed copy expects something else, this fails: see whether sqlkit now
// checks MySQL identifier columns as binary strings, and if so hand that back
// to it and delete this test.
func TestSQLKitStillExpectsTheOldMySQLCollation(t *testing.T) {
	t.Parallel()

	require.Equal(t, "utf8mb4_0900_as_cs", sqlkit.MySQL.IdentifierCollation(),
		"the vendored sqlkit changed what it expects of MySQL identifier columns: revisit Store.verify and this test")
}
```

In `TestTheDocumentedMySQLUpgradeComparesIdentifiersByBytes`, expect the new wording: `ntfy_notifications.recipient: type is "varchar(255)" collated "utf8mb4_0900_as_cs" but must be VARBINARY`, and the same for `ntfy_email_deliveries.owner`.

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifySchemaRequiresByteExactIdentifiersOnMySQL|TestSQLKitStill|TestVerifySchemaOn|TestVerifyEmailSchemaOn|TestTheDocumentedMySQL' -count=1 .`
Observed: `ok`.

- [x] **Step 5: Prove the pre-check misses the email table (7.3, red)**

Append to `sqlstore/email_verify_test.go`:

```go
// TestTheDocumentedMySQLPreCheckFindsOverLongIdentifiers puts a schema back on
// the old, character-counted columns and plants, in each table, an identifier
// that fits there but not in its VARBINARY column. The documented pre-check
// must list every one, so that no ALTER of the upgrade is refused, or with
// strict mode off truncates, part way through. The cases do not vary context,
// so the table has no ctx field.
func TestTheDocumentedMySQLPreCheckFindsOverLongIdentifiers(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "mysql", sqlkittest.RunTestMySQL(t))
	executor := stdsqlExecutor(t, db, sqlkit.MySQL)

	// 200 characters fit VARCHAR(255); their 400 bytes do not fit VARBINARY(255).
	long := strings.Repeat("\u00e9", 200)

	type testCase struct {
		name   string
		plant  func(t *testing.T, prefix string) string // returns the key the pre-check lists
		assert func(t *testing.T, key string, found []string)
	}

	listed := func(t *testing.T, key string, found []string) {
		assert.Contains(t, found, key, "the pre-check lists the row the upgrade would refuse")
	}

	cases := []testCase{
		{
			name: "a notification's recipient",
			plant: func(t *testing.T, prefix string) string {
				_, err := db.ExecContext(t.Context(), "INSERT INTO `"+prefix+"ntfy_notifications` "+
					"(`id`, `recipient`, `source_id`, `subject`, `subject_version`, `kind`, `state`, `created_at`) "+
					"VALUES ('n-long', ?, 'event-1', 'task-1', 1, 'offer', 'ACTIVE', NOW(6))", long)
				require.NoError(t, err)

				return "n-long"
			},
			assert: listed,
		},
		{
			name: "a close record's subject",
			plant: func(t *testing.T, prefix string) string {
				_, err := db.ExecContext(t.Context(), "INSERT INTO `"+prefix+"ntfy_watermarks` "+
					"(`subject`, `kind`, `version`, `updated_at`) VALUES (?, '*', 1, NOW(6))", long)
				require.NoError(t, err)

				return long
			},
			assert: listed,
		},
		{
			name: "an email delivery's owner",
			plant: func(t *testing.T, prefix string) string {
				_, err := db.ExecContext(t.Context(), "INSERT INTO `"+prefix+"ntfy_email_deliveries` "+
					"(`notification_id`, `recipient`, `status`, `owner`, `attempts`, `updated_at`) "+
					"VALUES ('n-long', 'alice', 'SENDING', ?, 0, NOW(6))", long)
				require.NoError(t, err)

				return "n-long"
			},
			assert: listed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := harness.NewEmailStore(t, executor)
			prefix := prefixOf(store)

			for _, statement := range documentedBlock(t, "### Rolling the upgrade back", prefix) {
				exec(t, executor, statement)
			}

			key := tc.plant(t, prefix)

			var found []string

			for _, check := range documentedBlock(t, "### Checking before upgrading", prefix) {
				rows, err := db.QueryContext(t.Context(), check)
				require.NoErrorf(t, err, "run the documented pre-check %q", check)

				for rows.Next() {
					var value string
					require.NoError(t, rows.Scan(&value))

					found = append(found, value)
				}

				require.NoError(t, rows.Err())
				require.NoError(t, rows.Close())
			}

			tc.assert(t, key, found)
		})
	}
}
```

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentedMySQLPreCheckFindsOverLongIdentifiers' -count=1 .`
Observed: FAIL only on `an email delivery's owner`, with `[]string(nil) does not contain "n-long"`.

- [x] **Step 6: Complete the pre-check and state that statements commit one by one (7.3, green)**

In `docs/schema.md`, under "Comparing identifiers byte for byte on an existing MySQL host":
- add after the rebuild paragraph:

```markdown
Each `ALTER TABLE` commits on its own, because MySQL cannot roll a schema change
back. If one is refused, the tables altered before it stay converted and the
rest do not: `Store.VerifySchema` or `Store.VerifyEmailSchema` then names what
is left. Fix the cause and run the statements again. Re-running one that already
succeeded is harmless: it
redeclares the columns as they already are.
```

- rewrite the pre-check's opening paragraph to name all three limits, 64, 255 and 100;
- add to its `sql` block:

```sql
-- Only a host that emails has this table.
SELECT `notification_id` FROM `app_ntfy_email_deliveries`
    WHERE LENGTH(`notification_id`) > 64 OR LENGTH(`recipient`) > 255 OR LENGTH(`status`) > 16
       OR LENGTH(`batch_id`) > 64 OR LENGTH(`owner`) > 255;
```

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentedMySQL' -count=1 -v .`
Observed: all three documented-SQL tests pass.

- [x] **Step 7: One helper for documented SQL (7.6, refactor under green)**

Delete `documentedStatement`. Key the claim-index test on its MySQL line:

```go
// mysqlClaimIndexLine is the line of docs/schema.md that introduces the MySQL
// statement adding the email claim index to an existing host.
const mysqlClaimIndexLine = "- **MySQL:** run this, and do **not** re-apply the email document:"
```

```go
	for _, statement := range documentedBlock(t, mysqlClaimIndexLine, prefixOf(store)) {
		exec(t, executor, statement)
	}
```

`documentedBlock` now takes any line (the parameter is renamed `line`), and finds the closing fence even when it is indented under a list item:

```go
// documentedBlock reads the statements of the first fenced sql block after a
// line of docs/schema.md, such as a heading, with comment lines dropped and the
// documented app_ prefix replaced by the store's, so that the SQL a host runs is
// the SQL the test ran.
func documentedBlock(t *testing.T, line, prefix string) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "docs", "schema.md"))
	require.NoError(t, err)

	document := string(raw)

	start := strings.Index(document, "\n"+line+"\n")
	require.GreaterOrEqualf(t, start, 0, "docs/schema.md has the line %q", line)

	body := document[start:]

	open := strings.Index(body, "```sql\n")
	require.GreaterOrEqualf(t, open, 0, "%q is followed by an sql block", line)

	body = body[open+len("```sql\n"):]

	// The closing fence may be indented, as it is under a list item.
	end := strings.Index(body, "```")
	require.GreaterOrEqualf(t, end, 0, "the sql block under %q is closed", line)

	var kept []string

	for sql := range strings.SplitSeq(body[:end], "\n") {
		if !strings.HasPrefix(strings.TrimSpace(sql), "--") {
			kept = append(kept, sql)
		}
	}

	var statements []string

	for statement := range strings.SplitSeq(strings.Join(kept, "\n"), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			statements = append(statements, strings.ReplaceAll(statement, "app_", prefix))
		}
	}

	require.NotEmptyf(t, statements, "the sql block under %q holds statements", line)

	return statements
}
```

Run the Step 6 command. Observed: pass. Lint: `0 issues.`

- [x] **Step 8: Correct the normalisation wording (7.7)**

In `docs/schema.md`, the "Identifiers compare byte for byte" bullet now says that `utf8mb4_0900_as_cs` "equates a precomposed `é` (U+00E9) with `e` followed by a combining acute accent (U+0301)". It no longer says it equates `é` with `e`.

- [x] **Step 9: Prove the suite misses stores folding only their filters (7.4, 7.5, red)**

Replace `ntfytest/identity_test.go` with:

```go
package ntfytest_test

// The conformance suite fails its own test when a store breaks it, so the only
// way to assert that it rejects a store is to run it in a child process and
// watch that process fail.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

// identityChildEnv names the fold a child process applies and where, as
// "<fold>/<scope>", and marks a process as a child.
const identityChildEnv = "NTFYTEST_IDENTITY_CHILD"

// folds are the ways a store can merge identifiers that differ in their bytes.
var folds = map[string]func(string) string{
	"lower-casing":                strings.ToLower,
	"trimming trailing spaces":    func(s string) string { return strings.TrimRight(s, " ") },
	"stripping zero-width spaces": func(s string) string { return strings.ReplaceAll(s, "\u200b", "") },
	// The one canonical composition the suite's variants use, which is what a
	// collation equating NFC with NFD does to them.
	"normalising to NFC": func(s string) string { return strings.ReplaceAll(s, "e\u0301", "\u00e9") },
}

// The scopes a fold is applied in.
const (
	// everywhere folds every identifier the store is given.
	everywhere = "everywhere"
	// filtersOnly folds only what a store is asked to match against, and stores
	// what it is given as it is: the subject and kind filters of a listing, a
	// close's subject, kinds, exception and successor skips, and mark-all-read's
	// recipient.
	filtersOnly = "filters only"
)

// foldingStore is a memory store that folds identifiers, the way a database
// column with the wrong collation compares them.
type foldingStore struct {
	ntfy.Store
	fold        func(string) string
	filtersOnly bool
}

func (s foldingStore) all(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = s.fold(value)
	}

	return out
}

// stored folds an identifier the store keeps or looks up by key, which a
// filters-only store leaves alone.
func (s foldingStore) stored(value string) string {
	if s.filtersOnly {
		return value
	}

	return s.fold(value)
}

func (s foldingStore) Insert(ctx context.Context, subject string, in []ntfy.Insertion) (ntfy.InsertResult, error) {
	out := make([]ntfy.Insertion, len(in))
	for i, insertion := range in {
		n := &insertion.Notification
		n.Recipient, n.SourceID = s.stored(n.Recipient), s.stored(n.SourceID)
		n.Subject, n.Kind = s.stored(n.Subject), s.stored(n.Kind)
		out[i] = insertion
	}

	return s.Store.Insert(ctx, s.stored(subject), out)
}

func (s foldingStore) Close(ctx context.Context, req ntfy.CloseRequest, at time.Time, ids ntfy.IDGenerator) (ntfy.CloseResult, error) {
	req.Subject, req.Except = s.fold(req.Subject), s.fold(req.Except)
	req.Kinds, req.SuccessorSkip = s.all(req.Kinds), s.all(req.SuccessorSkip)

	return s.Store.Close(ctx, req, at, ids)
}

func (s foldingStore) Get(ctx context.Context, recipient, id string) (ntfy.Notification, error) {
	return s.Store.Get(ctx, s.stored(recipient), id)
}

func (s foldingStore) List(ctx context.Context, q ntfy.ListQuery) (ntfy.Page, error) {
	q.Recipient, q.Subject, q.Kinds = s.stored(q.Recipient), s.fold(q.Subject), s.all(q.Kinds)

	return s.Store.List(ctx, q)
}

func (s foldingStore) CountActive(ctx context.Context, recipient string) (int64, error) {
	return s.Store.CountActive(ctx, s.stored(recipient))
}

func (s foldingStore) MarkRead(ctx context.Context, recipient string, ids []string, at time.Time) (ntfy.MarkResult, error) {
	return s.Store.MarkRead(ctx, s.stored(recipient), ids, at)
}

func (s foldingStore) MarkAllRead(ctx context.Context, recipient string, through, at time.Time) (ntfy.MarkResult, error) {
	return s.Store.MarkAllRead(ctx, s.fold(recipient), through, at)
}

// TestIdentityChild runs the identity group against the store its environment
// names. It does nothing unless TestIdentityRejectsFoldingStores started it.
func TestIdentityChild(t *testing.T) {
	mode, ok := os.LookupEnv(identityChildEnv)
	if !ok {
		t.Skip("runs only as a child of TestIdentityRejectsFoldingStores")
	}

	name, scope, _ := strings.Cut(mode, "/")

	ntfytest.Run(t, func(*testing.T) ntfy.Store {
		if fold, folding := folds[name]; folding {
			return foldingStore{Store: ntfy.NewMemoryStore(), fold: fold, filtersOnly: scope == filtersOnly}
		}

		return ntfy.NewMemoryStore()
	})
}

// runIdentityChild runs the identity group in a child process folding by mode,
// and reports whether it passed and what it printed.
func runIdentityChild(t *testing.T, mode string) (passed bool, output string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run", "^TestIdentityChild$/^identity$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), identityChildEnv+"="+mode)
	out, err := cmd.CombinedOutput()

	return err == nil, string(out)
}

// TestIdentityRejectsFoldingStores proves the identity group fails a store that
// merges identifiers, wherever it merges them, and passes one that does not.
// The cases do not vary context, so the table has no ctx field.
func TestIdentityRejectsFoldingStores(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		mode   string
		assert func(t *testing.T, passed bool, output string)
	}

	// rejected requires an identity case itself to have failed, so that a child
	// failing for any other reason, such as a panic, is not taken for one.
	rejected := func(t *testing.T, passed bool, output string) {
		assert.Falsef(t, passed, "ntfytest.Run accepted a store that folds identifiers:\n%s", output)
		assert.Containsf(t, output, "--- FAIL: TestIdentityChild/identity/",
			"an identity case, not something else, rejected the store:\n%s", output)
	}

	cases := []testCase{
		{
			name: "a sound store passes",
			mode: "sound",
			assert: func(t *testing.T, passed bool, output string) {
				assert.Truef(t, passed, "ntfytest.Run failed the memory store:\n%s", output)
				assert.Contains(t, output, "--- PASS: TestIdentityChild/identity/alice_differing_in_case",
					"the child ran the identity group, so passing is not vacuous")
			},
		},
	}

	for name := range folds {
		for _, scope := range []string{everywhere, filtersOnly} {
			cases = append(cases, testCase{
				name: "a store " + name + " " + scope + " fails", mode: name + "/" + scope, assert: rejected,
			})
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			passed, output := runIdentityChild(t, tc.mode)
			tc.assert(t, passed, output)
		})
	}
}
```

Run: `cd ntfytest && GOTOOLCHAIN=go1.26.8 go test -run 'TestIdentityRejectsFoldingStores' -count=1 -v .`
Observed: FAIL on all four `filters only` rows. The four `everywhere` rows, including the new NFC store, and the sound control pass.

- [x] **Step 10: Filter by the byte-different identifier (7.5, green)**

Replace `recipientIsItsBytes`, `subjectIsItsBytes` and `kindIsItsBytes` in `ntfytest/identity.go` with:

```go
// recipientIsItsBytes: another recipient reads, lists, counts and marks
// nothing of the published recipient's, and a close that names the other
// recipient to spare or to skip a successor spares or skips nothing of theirs.
func recipientIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	published := e.note(v.of, "event-1", "task-1", "offer", 1, at(0))
	e.insert(false, published)

	_, err := e.store.Get(t.Context(), v.other, published.ID)
	require.ErrorIsf(t, err, ntfy.ErrNotFound, "reading as %+q finds %+q's notification", v.other, v.of)

	assert.Emptyf(t, e.list(ntfy.ListQuery{Recipient: v.other}).Notifications, "%+q lists %+q's notification", v.other, v.of)
	assert.Zerof(t, e.count(v.other), "%+q counts %+q's notification", v.other, v.of)

	_, err = e.store.MarkRead(t.Context(), v.other, []string{published.ID}, at(1))
	require.ErrorIsf(t, err, ntfy.ErrNotFound, "%+q marks %+q's notification read", v.other, v.of)

	marked, err := e.store.MarkAllRead(t.Context(), v.other, at(1), at(1))
	require.NoError(t, err)
	assert.Zerof(t, marked.Marked, "marking all of %+q's read marks %+q's", v.other, v.of)

	stored := e.get(v.of, published.ID)
	assert.Equal(t, ntfy.StateActive, stored.State, "the published recipient's notification is untouched")
	assert.Equal(t, v.of, stored.Recipient, "the recipient is returned exactly as published")

	closed := e.close(ntfy.CloseRequest{
		Subject: "task-1", Version: 5, Reason: "taken", Except: v.other,
		Successor:     &ntfy.Successor{SourceID: "event-5", Kind: "taken", SubjectVersion: 5},
		SuccessorSkip: []string{v.other},
	}, at(2))

	assert.Equalf(t, ntfy.StateClosed, e.get(v.of, published.ID).State, "sparing %+q spared %+q", v.other, v.of)
	require.Lenf(t, closed.Successors, 1, "skipping %+q's successor skipped %+q's", v.other, v.of)
	assert.Equal(t, v.of, closed.Successors[0].Recipient)
}
```

```go
// subjectIsItsBytes: listing or closing one subject reaches nothing on another
// differing only in its bytes, in either direction, and closing one does not
// suppress a later publish on the other.
func subjectIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	closing := e.note("alice", "event-1", v.of, "offer", 1, at(0))
	open := e.note("alice", "event-2", v.other, "offer", 1, at(1))
	e.insert(false, closing)
	e.insert(false, open)

	listed := e.list(ntfy.ListQuery{Recipient: "alice", Subject: v.other}).Notifications
	assert.Equalf(t, []string{open.ID}, idsOf(listed), "listing %+q returned %+q's notifications too", v.other, v.of)

	e.close(ntfy.CloseRequest{Subject: v.of, Version: 5, Reason: "done"}, at(2))

	assert.Equal(t, ntfy.StateClosed, e.get("alice", closing.ID).State)
	assert.Equalf(t, ntfy.StateActive, e.get("alice", open.ID).State, "closing %+q closed %+q", v.of, v.other)

	late := e.insert(false, e.note("alice", "event-3", v.other, "offer", 2, at(3)))
	assert.Lenf(t, late.Created, 1, "closing %+q suppressed a publish on %+q", v.of, v.other)
	assert.Zero(t, late.Suppressed)

	// The other way round: closing the byte-different subject leaves one that
	// was never named alone.
	untouched := e.note("alice", "event-4", "untouched-"+v.of, "offer", 1, at(4))
	e.insert(false, untouched)
	e.close(ntfy.CloseRequest{Subject: "untouched-" + v.other, Version: 5, Reason: "done"}, at(5))

	assert.Equalf(t, ntfy.StateActive, e.get("alice", untouched.ID).State,
		"closing %+q closed %+q", "untouched-"+v.other, "untouched-"+v.of)
}
```

```go
// kindIsItsBytes: a kind filter, or a close by kind, reaches its kind exactly.
func kindIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	wanted := e.note("alice", "event-1", "task-1", v.of, 1, at(0))
	e.insert(false, wanted, e.note("alice", "event-2", "task-1", v.other, 1, at(1)))

	listed := e.list(ntfy.ListQuery{Recipient: "alice", Kinds: []string{v.of}}).Notifications
	assert.Equalf(t, []string{wanted.ID}, idsOf(listed), "a filter on %+q returned %+q too", v.of, v.other)

	unnamed := e.note("alice", "event-3", "task-2", v.of, 1, at(2))
	e.insert(false, unnamed)
	e.close(ntfy.CloseRequest{Subject: "task-2", Kinds: []string{v.other}, Version: 5, Reason: "done"}, at(3))

	assert.Equalf(t, ntfy.StateActive, e.get("alice", unnamed.ID).State, "closing kind %+q closed %+q", v.other, v.of)
}
```

Run the Step 9 command. Observed: all nine rows pass.
Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOn.*/identity' -count=1 -v .`
Observed: 64 identity rows pass across PostgreSQL (database/sql and pgx), MySQL and SQLite.
Inversion: with the "in another normalisation form" variant temporarily removed, exactly the two NFC rows fail; restored, they pass.

- [ ] **Step 11: Update the sqlkit issue text, run the gates, commit (7.8)**

The issue text under Task 5 Step 2 now asks for a type check and names `BINARY`/`BLOB`. Run `make all`, `make store-matrix` and `make sqlkit-copy-check`, and record the results in the execution record below. Commit the root, sqlstore and ntfytest changes, each with the tests that drove it.

---

## Execution record

Executed 2026-09-28 in the worktree `.claude/worktrees/compare-mysql-identifiers-by-bytes`, branch `compare-mysql-identifiers-by-bytes`, against MySQL 8.4.6 (`sqlkittest.MySQLImage`), Go 1.26.8.

**What changed from the first plan.** Midway through Task 1, the maintainer rejected `utf8mb4_0900_bin` because it needs MySQL 8.0.17: a library cannot make its hosts upgrade their database. The design moved to `VARBINARY` (`design.md` D1). Steps 1–4 were already done and needed no change. A throwaway probe confirmed the facts D2 rests on, then was deleted:
- MySQL reports a NULL collation for `VARBINARY` columns, which sqlkit accepts;
- `CAST('alice' AS BINARY) = 'alice '` is 0, so binary strings do not pad.

**Task 1 Step 2 (red):** all five cases failed for the stated reason.
- The three recipient cases failed with `Get as "alice\u200b" returned "alice"'s notification`, `... "alice\x00" ...` and `Get as "jose\u0301" returned "jos\u00e9"'s notification`, each also listing and counting the other recipient's notification.
- The two source cases failed with `"event-1\u200b" is a source of its own, not a redelivery of "event-1"`, and likewise for `\x00`. In each, `Created` was empty and `Duplicates` was 1.

This settled the proposal's claim that the collation also merges sources, which had been labelled unverified.

**Task 1 Step 4 (red on MySQL):**
- 8 of 16 identity rows failed: the U+200B and NFC/NFD rows for alice, event-1, task-1 and offer. The failures read `closing "task-1" closed "task-1\u200b"`, `closing "task-1" suppressed a publish on "task-1\u200b"` and `a filter on "offer" returned "offer\u200b" too`.
- The case and trailing-space rows passed, as `design.md` D1 predicted.
- Memory passed 16 of 16. PostgreSQL (database/sql and pgx) and SQLite passed 48 of 48.

**Task 1 Steps 6–8:**
- Before the wrapper, the `utf8mb4_0900_as_cs` row failed with `An error is expected but got nil`, and the `utf8mb4_0900_bin` row failed with `... but must be "utf8mb4_0900_as_cs" ... does not contain ... but must be "binary"`.
- `TestVerifyDialect` failed only on MySQL, with `expected: "binary" actual: "utf8mb4_0900_as_cs"`.
- With the wrapper but the old DDL, verification named all eight notification/watermark columns and all five email columns, as expected.

**Task 2:** as planned, plus one strengthening. The `sound` control could have passed with a child that ran no tests at all. So the child now runs with `-test.v`, and the control requires `--- PASS: TestIdentityChild/identity/alice_differing_in_case`. With the `identity` line removed from `Run`, all four rows failed, the control included; restored, all passed. Lint also asked for named results on `runIdentityChild` (`unnamedResult`), and for `\u200b` escapes instead of literal U+200B (ST1018).

**Task 3:** red, with `docs/schema.md has the heading "### Rolling the upgrade back"`. Once the section was written, `TestTheDocumentedMySQLUpgradeComparesIdentifiersByBytes` and the existing `TestTheDocumentedMySQLUpgradeAddsTheEmailClaimIndex` both passed.

**Gates (Task 5 Step 1):**
- `make all`: lint gave `0 issues.` in all six modules, and split-check passed. Tests passed in `ntfy`, `ntfytest`, `sqlstore`, `sqlstore/internal/gormtest` and `websocket`.
  - `redis` failed on `TestListen/cancelling_stops_listening_and_unsubscribes` (`listen_test.go:162`, "the channel has no subscriber left"). This is the timing race from the 2026-09-28 audit and is unrelated to this change. A re-run of `redis` passed.
  - The loop stops at the first failing module, so `nats` and the `pkg/sqlkit` modules were run separately. All passed.
- `make store-matrix`: exit 0 on all seven driver-and-dialect combinations.
- `make sqlkit-copy-check`: exit 0 against `32e7763297a4ac0107c0702da5a5b497c9029083`. `pkg/sqlkit` is untouched.

**sqlkit follow-up (Task 5 Step 2):** the issue text under Task 5 Step 2 is ready to file in sqlkit's repository. Filing it is left to the maintainer; it has not been filed.
