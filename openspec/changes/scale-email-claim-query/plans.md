# Scale the Email Claim Query Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every branch of the email claim query index-servable, so a dispatch pass costs what the max-lag window holds rather than what `ntfy_notifications` holds.

**Architecture:** `dueEmails` becomes three disjoint `UNION ALL` branches — notifications with no delivery record, in-doubt `SENDING` records, due `CLAIMED`/`RETRY` records — each driving from the table its own index covers, each taking the claim's limit, with the union ordered and limited again. One composite index, `ntfy_notifications_email_idx (state, created_at, id)`, is declared in the **email** DDL documents and required only by `VerifyEmailSchema`, so a host that never emails neither carries it nor fails startup over it. The rows claimed, their order, the lease semantics and `DispatchResult` are unchanged.

**Tech Stack:** Go 1.26, `github.com/kartaladev/sqlkit` (frozen copy under `pkg/sqlkit`), PostgreSQL 16 / MySQL 8.0 / SQLite 3.35+ through `database/sql`, pgx and GORM executors, testcontainers via `sqlkittest`, testify.

**Spec:** `openspec/changes/scale-email-claim-query/` — `proposal.md` (why) and `design.md` (D1 the branch shape, D2 the index and its placement, D3 the acceptance threshold, D4/D5 what is deliberately out of scope). **There is no spec delta:** `.openspec.yaml` sets `skip_specs: true` because every observable behaviour the specs describe — which notifications a pass claims, in what order, under what lease, and what it reports — is identical before and after. The reasoning is recorded in `proposal.md` — Capabilities; do not invent a requirement to satisfy `openspec validate`.

## Global Constraints

- Go 1.26; every module declares `go 1.26.0` and the Makefile pins `GOTOOLCHAIN=go1.26.8`. Export it, or run through `make`, or the tools on `PATH` break.
- `pkg/sqlkit` is an **unedited copy** of `github.com/kartaladev/sqlkit`. Never edit it; `make sqlkit-copy-check` fails if it changes. Anything sqlkit "should" do must instead be done in `sqlstore`.
- All three dialects must pass: PostgreSQL, MySQL 8.0+, SQLite 3.35+. `make store-matrix` runs the seven driver-and-dialect combinations and needs Docker.
- Databases in tests come from testcontainers through `sqlkittest.RunTestPostgres` / `RunTestMySQL` / `RunTestSQLite` (`use-testcontainers` skill). Never a hand-rolled fake, never a shared dev database.
- Table-driven tests follow the project's `table-test` skill: the `assert` closure form, never `want`/`wantErr` fields, and `t.Context()` over `context.Background()`.
- `.claude/rules/prove-errors-with-tests.md`: a performance claim is proved by measurement **before** the fix. Task 1 is that proof and carries an explicit STOP: if no dialect shows the scan, this change does not proceed.
- `.claude/rules/library-design.md`: every behaviour decision states its default and how a consumer overrides it; limits are stated, never silently relaxed. Here that is the MySQL non-idempotent statement, which must be written down in the document header, `docs/schema.md` and the migration note.
- `.claude/rules/golang-tdd.md`: red → green → refactor. A characterization test that must pass before and after is not a red test, and this plan says so where it uses one.
- `make all` (lint, split-check, test) must pass before the change is done.
- Commit messages: imperative, sentence case, no `feat:`/`fix:` prefixes, no attribution lines.

---

## File Structure

**Created**

- `sqlstore/email_claim_measure_test.go` — internal test (`package sqlstore`). The measurement: seeding, `EXPLAIN` capture, pass timings, threshold checks. Internal because it reads `dueEmailsStatement`, which is unexported. Opt-in through `NTFY_MEASURE_ROWS`, so `make test` stays quick.
- `sqlstore/email_claim_sql_test.go` — internal test (`package sqlstore`). Asserts the SQL the claim builder writes, per dialect, with no database.
- `openspec/changes/scale-email-claim-query/measurements.md` — the evidence the archived change carries: plans before and after, medians, machine, seed.

**Modified**

- `sqlstore/email.go` — `writeLapsed` split into `writeDueRetry` + `writeInDoubt`; `dueEmailsStatement` extracted then rewritten; `emailSchemaExpectation` gains a `ntfy_notifications` entry carrying only the new index; `MigrateEmail` guards the MySQL index statement; `emailNotificationsIndex` constant added.
- `sqlstore/ddl/email/postgres.sql`, `ddl/email/sqlite.sql` — one `CREATE INDEX IF NOT EXISTS` on the notifications table, with a header note saying why it lives here.
- `sqlstore/ddl/email/mysql.sql` — the same index as a standalone `CREATE INDEX`, with the stated non-idempotence.
- `sqlstore/testdata/schema/email_{postgres,mysql,sqlite}{,_app_}.sql` — six golden files regenerated.
- `sqlstore/email_schema_test.go` — the "creates no notification table" assertion narrowed to `CREATE TABLE` statements, so an index on that table is allowed.
- `sqlstore/email_verify_test.go` — `dropEmailIndex` takes the table it drops from; a case for the new index on the notifications table.
- `docs/schema.md` — the index table, the one stated exception to the `CREATE ... IF NOT EXISTS` promise, the email schema's rollback, and the per-dialect migration note.

**Not touched:** `pkg/sqlkit` (frozen), `sqlstore/store.go`'s `schemaExpectation` (a non-email host must keep verifying clean), `ntfytest` (the conformance suite must pass unedited — editing it would mean behaviour moved).

---

### Task 1: Measurement harness and the baseline

Proves the premise before anything changes. Carries the STOP gate.

**Files:**
- Create: `sqlstore/email_claim_measure_test.go`
- Create: `openspec/changes/scale-email-claim-query/measurements.md`

**Interfaces:**
- Consumes: `Store.dueEmails`, `Store.do`, `Store.encode`, `Store.executor`, `Store.dialect`, `notificationColumns`, `decoder` — all package-internal, already present.
- Produces, for later tasks:
  - `type seedShape struct { InWindow, Recorded, Outside int; Now time.Time; Grace, MaxLag time.Duration }`
  - `func seed(t *testing.T, store *Store, shape seedShape)`
  - `func explain(t *testing.T, store *Store, statement sqlkit.Statement) string`
  - `func medianOf(samples []time.Duration) time.Duration`
  - `func measureRows(t *testing.T) int`
  - `func measureStore(t *testing.T, dialect sqlkit.Dialect) *Store`

- [ ] **Step 1: Create the measurement file with its opt-in gate and store builder**

```go
package sqlstore

// The claim measurement. It is opt-in: seeding a million rows takes minutes,
// and `make test` must stay quick. Run it with, for example:
//
//	NTFY_MEASURE_ROWS=1000000 go test -run TestMeasureClaim -timeout 60m ./...

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/sqlkit"
	"github.com/kartaladev/sqlkit/sqlkittest"
	stdsqlexec "github.com/kartaladev/sqlkit/stdsql"
)

// measureRowsEnv opts a run into the measurement and says how many
// notifications to seed inside the claim window.
const measureRowsEnv = "NTFY_MEASURE_ROWS"

// measureRows reads the seed size, skipping the test when the measurement was
// not asked for.
func measureRows(t *testing.T) int {
	t.Helper()

	raw := os.Getenv(measureRowsEnv)
	if raw == "" {
		t.Skipf("set %s to run the claim measurement, for example %s=1000000", measureRowsEnv, measureRowsEnv)
	}

	rows, err := strconv.Atoi(raw)
	require.NoErrorf(t, err, "%s must be a number", measureRowsEnv)
	require.Positivef(t, rows, "%s must be positive", measureRowsEnv)

	return rows
}

// measureStore starts a database for a dialect and builds a store on freshly
// migrated tables, email schema included.
func measureStore(t *testing.T, dialect sqlkit.Dialect) *Store {
	t.Helper()

	var driver, dsn string

	switch dialect.Name() {
	case sqlkit.PostgreSQL.Name():
		driver, dsn = "postgres", sqlkittest.RunTestPostgres(t)
	case sqlkit.MySQL.Name():
		driver, dsn = "mysql", sqlkittest.RunTestMySQL(t)
	default:
		driver, dsn = "sqlite", sqlkittest.RunTestSQLite(t)
	}

	db, err := sql.Open(driver, dsn)
	require.NoErrorf(t, err, "open a %s pool", driver)

	t.Cleanup(func() { _ = db.Close() })

	db.SetMaxOpenConns(8)

	executor, err := stdsqlexec.New(db, dialect)
	require.NoError(t, err)

	store, err := New(executor)
	require.NoError(t, err)

	require.NoError(t, store.Migrate(t.Context()))
	require.NoError(t, store.MigrateEmail(t.Context()))

	return store
}
```

- [ ] **Step 2: Add the seeding helpers to the same file**

```go
// seedShape is the database a measurement runs against.
type seedShape struct {
	// InWindow is how many ACTIVE notifications fall inside the claim window.
	InWindow int
	// Recorded is how many of those already carry a terminal delivery record,
	// so that they are considered and discarded rather than claimed.
	Recorded int
	// Outside is how many ACTIVE notifications are created before the window,
	// which no pass may ever claim.
	Outside int
	// Now is the instant the window is measured from.
	Now time.Time
	// Grace and MaxLag bound the window, as the dispatcher's defaults do.
	Grace  time.Duration
	MaxLag time.Duration
}

// rowsPerInsert keeps a seeding statement inside the dialect's bind limit.
// SQLite's default ceiling is the lowest of the three.
func rowsPerInsert(dialect sqlkit.Dialect) int {
	if dialect.Name() == sqlkit.SQLite.Name() {
		return 60
	}

	return 400
}

// seed fills the store's tables to a shape, in one transaction. It writes rows
// directly rather than through Insert: a million notifications through the
// publish path would take hours, and a measurement needs rows, not publishing
// semantics.
func seed(t *testing.T, store *Store, shape seedShape) {
	t.Helper()

	ids := ntfy.NewUUIDv7Generator()
	window := shape.Now.Add(-shape.MaxLag)
	span := shape.MaxLag - shape.Grace
	size := rowsPerInsert(store.dialect)

	start := time.Now()

	require.NoError(t, store.do(t.Context(), func(ctx context.Context) error {
		batch := make([]ntfy.Notification, 0, size)
		recorded := make([]string, 0, size)

		flush := func() error {
			if len(batch) == 0 {
				return nil
			}

			if err := seedNotifications(ctx, store, batch); err != nil {
				return err
			}

			if err := seedDeliveries(ctx, store, recorded, shape.Now); err != nil {
				return err
			}

			batch, recorded = batch[:0], recorded[:0]

			return nil
		}

		for i := range shape.InWindow + shape.Outside {
			id, err := ids.NewID()
			if err != nil {
				return err
			}

			created := shape.Now.Add(-shape.MaxLag - time.Hour)
			if i < shape.InWindow {
				created = window.Add(time.Duration(int64(span) * int64(i) / int64(max(1, shape.InWindow))))
			}

			batch = append(batch, ntfy.Notification{
				ID: id, Recipient: fmt.Sprintf("user-%d", i%1000), SourceID: "seed-" + id,
				Subject: fmt.Sprintf("subject-%d", i%1000), Kind: "seed", State: ntfy.StateActive,
				CreatedAt: sqlkit.NormalizeTime(created),
			})

			if i < shape.Recorded {
				recorded = append(recorded, id)
			}

			if len(batch) == size {
				if err := flush(); err != nil {
					return err
				}
			}
		}

		return flush()
	}))

	t.Logf("seeded %d in-window and %d outside rows in %s", shape.InWindow, shape.Outside, time.Since(start).Round(time.Second))
}

// seedNotifications writes one batch of notifications.
func seedNotifications(ctx context.Context, s *Store, batch []ntfy.Notification) error {
	w := sqlkit.NewWriter(s.dialect)
	w.Write("INSERT INTO ", s.notificationsTable(), " (", s.columnList(notificationColumns...), ") VALUES ")

	for i, n := range batch {
		values, err := s.encode(n)
		if err != nil {
			return err
		}

		if i > 0 {
			w.Write(", ")
		}

		w.Write("(", w.BindAll(values...), ")")
	}

	_, err := s.executor.Exec(ctx, w.Done())

	return err
}

// seedDeliveries writes terminal SENT records, which no branch of the claim
// may return.
func seedDeliveries(ctx context.Context, s *Store, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}

	instant := sqlkit.EncodeTime(s.dialect, &at)
	w := sqlkit.NewWriter(s.dialect)
	w.Write("INSERT INTO ", s.emailTable(), " (",
		s.columnList("notification_id", "recipient", "status", "attempts", "sent_at", "updated_at"), ") VALUES ")

	for i, id := range ids {
		if i > 0 {
			w.Write(", ")
		}

		w.Write("(", w.BindAll(id, "seed", string(ntfy.EmailStatusSent), 1, instant, instant), ")")
	}

	_, err := s.executor.Exec(ctx, w.Done())

	return err
}
```

- [ ] **Step 3: Add the plan-capture and timing helpers to the same file**

```go
// explain returns the dialect's plan for a statement, as text.
func explain(t *testing.T, store *Store, statement sqlkit.Statement) string {
	t.Helper()

	var (
		prefix string
		width  int
		detail int
	)

	switch store.dialect.Name() {
	case sqlkit.PostgreSQL.Name():
		prefix, width, detail = "EXPLAIN (ANALYZE, BUFFERS) ", 1, 0
	case sqlkit.MySQL.Name():
		prefix, width, detail = "EXPLAIN FORMAT=JSON ", 1, 0
	default:
		// EXPLAIN QUERY PLAN returns id, parent, notused, detail.
		prefix, width, detail = "EXPLAIN QUERY PLAN ", 4, 3
	}

	var lines []string

	require.NoError(t, store.executor.Query(t.Context(),
		sqlkit.Statement{SQL: prefix + statement.SQL, Args: statement.Args},
		func(rows sqlkit.Rows) error {
			for rows.Next() {
				values := make([]any, width)
				dest := make([]any, width)

				for i := range values {
					dest[i] = &values[i]
				}

				if err := rows.Scan(dest...); err != nil {
					return err
				}

				var dec decoder

				line := dec.text(values[detail])
				if dec.err != nil {
					return dec.err
				}

				lines = append(lines, line)
			}

			return rows.Err()
		}))

	return strings.Join(lines, "\n")
}

// medianOf returns the median of samples, which must not be empty.
func medianOf(samples []time.Duration) time.Duration {
	slices.Sort(samples)

	return samples[len(samples)/2]
}

// timeClaim runs one claim and returns how long it took, deleting whatever it
// claimed so that samples stay independent.
func timeClaim(t *testing.T, store *Store, claim ntfy.EmailClaim) (time.Duration, int) {
	t.Helper()

	start := time.Now()
	claimed, err := store.ClaimEmails(t.Context(), claim)
	took := time.Since(start)

	require.NoError(t, err)

	if len(claimed) > 0 {
		w := sqlkit.NewWriter(store.dialect)
		w.Write("DELETE FROM ", store.emailTable(), " WHERE ", store.quote("owner"), " = ", w.Bind(claim.Owner))

		_, err := store.executor.Exec(t.Context(), w.Done())
		require.NoError(t, err)
	}

	return took, len(claimed)
}
```

- [ ] **Step 4: Add the baseline test**

```go
// measureClaim is the claim every measurement runs, with the dispatcher's
// default grace, lag and limit.
func measureClaim(now time.Time) ntfy.EmailClaim {
	return ntfy.EmailClaim{
		Now: now, Owner: "measure", Lease: ntfy.DefaultEmailLease,
		CreatedUntil: now.Add(-ntfy.DefaultEmailGraceDelay),
		CreatedFrom:  now.Add(-ntfy.DefaultEmailMaxLag),
		Limit:        ntfy.DefaultEmailClaimLimit,
	}
}

func TestMeasureClaim(t *testing.T) {
	rows := measureRows(t)

	type testCase struct {
		name    string
		dialect sqlkit.Dialect
	}

	cases := []testCase{
		{name: "PostgreSQL", dialect: sqlkit.PostgreSQL},
		{name: "MySQL", dialect: sqlkit.MySQL},
		{name: "SQLite", dialect: sqlkit.SQLite},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := measureStore(t, tc.dialect)
			now := sqlkit.NormalizeTime(time.Now().UTC())

			seed(t, store, seedShape{
				InWindow: rows / 20, Recorded: rows / 20, Outside: rows - rows/20,
				Now: now, Grace: ntfy.DefaultEmailGraceDelay, MaxLag: ntfy.DefaultEmailMaxLag,
			})

			claim := measureClaim(now)

			plan := explain(t, store, store.dueEmailsStatement(claim, now))
			t.Logf("plan:\n%s", plan)

			var empty []time.Duration

			for range 5 {
				took, claimed := timeClaim(t, store, claim)
				require.Zero(t, claimed, "every in-window notification is already SENT, so a pass claims nothing")

				empty = append(empty, took)
			}

			t.Logf("empty pass median: %s", medianOf(empty))
		})
	}
}
```

- [ ] **Step 5: Run the baseline on all three dialects and record the plans**

Run: `GOTOOLCHAIN=go1.26.8 NTFY_MEASURE_ROWS=1000000 go test -run 'TestMeasureClaim' -timeout 60m -v ./...` from `sqlstore/`
Expected: three plans logged. PostgreSQL must contain `Seq Scan on ntfy_notifications`; MySQL's JSON must contain `"access_type": "ALL"` for the notifications table; SQLite must contain `SCAN` against `ntfy_notifications` with no `USING INDEX`. Each plan must also show a join and a sort (`Sort` / `filesort` / `USE TEMP B-TREE FOR ORDER BY`).

- [ ] **Step 6: STOP GATE — decide whether the change proceeds**

If any dialect's plan does **not** show a full scan of `ntfy_notifications`, stop here and report it. The premise of this change is that no index can serve the current disjunction across an outer join; a planner that handles it well refutes that, and the remaining tasks do not apply (`.claude/rules/prove-errors-with-tests.md`). Do not lower the bar and continue.

- [ ] **Step 7: Record the window-versus-table baseline**

Add to the same test, after the empty-pass timings:

```go
			seed(t, store, seedShape{
				InWindow: 0, Outside: rows,
				Now: now, Grace: ntfy.DefaultEmailGraceDelay, MaxLag: ntfy.DefaultEmailMaxLag,
			})

			var grown []time.Duration

			for range 5 {
				took, claimed := timeClaim(t, store, claim)
				require.Zero(t, claimed)

				grown = append(grown, took)
			}

			t.Logf("empty pass median after %d more rows outside the window: %s", rows, medianOf(grown))
```

Run: the same command.
Expected: the second median is materially larger than the first, even though nothing new became claimable. That is threshold 2 failing today, and it is the defect in one number.

- [ ] **Step 8: Write the baseline into the change directory**

Create `openspec/changes/scale-email-claim-query/measurements.md` with: the machine (CPU, RAM, OS, Docker version), the seed shape, the three plans verbatim, the empty-pass medians before and after the extra rows, and the date. Leave an "After" section empty for Task 9.

- [ ] **Step 9: Commit**

```bash
git add sqlstore/email_claim_measure_test.go openspec/changes/scale-email-claim-query/measurements.md
git commit -m "Measure the email claim query before changing it"
```

---

### Task 2: Split the lease predicate into two index-friendly halves

Pure refactor. The SQL `takeOverDeliveries` writes must not change by one byte.

**Files:**
- Modify: `sqlstore/email.go:160-169`
- Create: `sqlstore/email_claim_sql_test.go`

**Interfaces:**
- Produces:
  - `func (s *Store) writeDueRetry(w *sqlkit.Writer, column func(string) string, now any)`
  - `func (s *Store) writeInDoubt(w *sqlkit.Writer, column func(string) string, now any)`
  - `func (s *Store) writeLapsed(w *sqlkit.Writer, column func(string) string, now any)` — signature unchanged, now composed of the two above.

- [ ] **Step 1: Write the characterization test for the SQL `takeOverDeliveries` writes**

This is a characterization test, not a red test: it must pass before and after, and that is the point — it pins the text so the refactor cannot move it.

Create `sqlstore/email_claim_sql_test.go`:

```go
package sqlstore

// The SQL the claim path writes, asserted per dialect with no database.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/sqlkit"
)

// dialectExecutor runs nothing. It exists so a test can build a store for a
// dialect and read the SQL it writes.
type dialectExecutor struct{ dialect sqlkit.Dialect }

func (e dialectExecutor) Dialect() sqlkit.Dialect { return e.dialect }

func (dialectExecutor) Exec(context.Context, sqlkit.Statement) (int64, error) { return 0, nil }

func (dialectExecutor) Query(context.Context, sqlkit.Statement, func(sqlkit.Rows) error) error {
	return nil
}

func (dialectExecutor) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

func (dialectExecutor) InTransaction(context.Context) bool { return false }

func (dialectExecutor) ExecStatement(context.Context, string, ...any) error { return nil }

func (dialectExecutor) QueryStatement(context.Context, string, ...any) (sqlkit.Rows, error) {
	return nil, nil
}

// storeFor builds a store that writes SQL for a dialect and runs nothing.
func storeFor(t *testing.T, dialect sqlkit.Dialect) *Store {
	t.Helper()

	store, err := New(dialectExecutor{dialect: dialect})
	require.NoError(t, err)

	return store
}

// lapsedSQL is the text writeLapsed writes for a dialect, for a plain column
// qualifier.
func lapsedSQL(t *testing.T, dialect sqlkit.Dialect) string {
	t.Helper()

	store := storeFor(t, dialect)
	w := sqlkit.NewWriter(dialect)
	store.writeLapsed(w, store.quote, "2026-09-16T00:00:00Z")

	return w.Done().SQL
}

func TestWriteLapsedIsUnchanged(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		dialect sqlkit.Dialect
		assert  func(t *testing.T, sql string)
	}

	cases := []testCase{
		{
			name:    "PostgreSQL",
			dialect: sqlkit.PostgreSQL,
			assert: func(t *testing.T, sql string) {
				assert.Equal(t,
					`(("status" IN ($1, $2) AND ("lease_until" IS NULL OR "lease_until" <= $3)`+
						` AND ("next_attempt_at" IS NULL OR "next_attempt_at" <= $4))`+
						` OR ("status" = $5 AND ("lease_until" IS NULL OR "lease_until" <= $6)))`,
					sql)
			},
		},
		{
			name:    "MySQL",
			dialect: sqlkit.MySQL,
			assert: func(t *testing.T, sql string) {
				assert.Contains(t, sql, "`status` IN (?, ?)")
				assert.Contains(t, sql, "OR (`status` = ? AND (`lease_until` IS NULL OR `lease_until` <= ?))")
			},
		},
		{
			name:    "SQLite",
			dialect: sqlkit.SQLite,
			assert: func(t *testing.T, sql string) {
				assert.Contains(t, sql, `"status" IN (?, ?)`)
				assert.Contains(t, sql, `OR ("status" = ? AND ("lease_until" IS NULL OR "lease_until" <= ?))`)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, lapsedSQL(t, tc.dialect))
		})
	}
}
```

- [ ] **Step 2: Run it against the code as it stands**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestWriteLapsedIsUnchanged' -count=1 ./...` from `sqlstore/`
Expected: PASS. It pins today's text. If it fails, the expected string is wrong — fix the string, not the code.

- [ ] **Step 3: Split the predicate**

Replace `writeLapsed` (`sqlstore/email.go:160-169`) with:

```go
// writeDueRetry writes the condition under which a CLAIMED or RETRY record may
// be claimed: unleased or lapsed, with its next attempt due. column qualifies a
// column name.
func (s *Store) writeDueRetry(w *sqlkit.Writer, column func(string) string, now any) {
	w.Write("(", column("status"), " IN (", w.BindAll(statusList(ntfy.EmailStatusClaimed, ntfy.EmailStatusRetry)...), ")",
		" AND (", column("lease_until"), " IS NULL OR ", column("lease_until"), " <= ", w.Bind(now), ")",
		" AND (", column("next_attempt_at"), " IS NULL OR ", column("next_attempt_at"), " <= ", w.Bind(now), "))")
}

// writeInDoubt writes the condition under which a SENDING record is in doubt:
// its lease lapsed, whatever the notification's state now is. column qualifies
// a column name.
func (s *Store) writeInDoubt(w *sqlkit.Writer, column func(string) string, now any) {
	w.Write("(", column("status"), " = ", w.Bind(string(ntfy.EmailStatusSending)),
		" AND (", column("lease_until"), " IS NULL OR ", column("lease_until"), " <= ", w.Bind(now), "))")
}

// writeLapsed writes the condition under which an existing delivery record may
// be claimed: a due CLAIMED or RETRY record, or a lapsed SENDING record. The
// claim uses the two halves separately, one per branch; the take-over uses both.
func (s *Store) writeLapsed(w *sqlkit.Writer, column func(string) string, now any) {
	w.Write("(")
	s.writeDueRetry(w, column, now)
	w.Write(" OR ")
	s.writeInDoubt(w, column, now)
	w.Write(")")
}
```

- [ ] **Step 4: Run the characterization test again**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestWriteLapsedIsUnchanged' -count=1 ./...` from `sqlstore/`
Expected: PASS, with the same text as in Step 2. A byte-identical `writeLapsed` is what proves `takeOverDeliveries` is untouched.

- [ ] **Step 5: Run the email conformance suites on SQLite**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add sqlstore/email.go sqlstore/email_claim_sql_test.go
git commit -m "Split the email lease predicate into its two halves"
```

---

### Task 3: Extract the claim statement builder

Separating statement building from execution lets a test read the SQL, and lets the measurement `EXPLAIN` it. Still behaviour-preserving.

**Files:**
- Modify: `sqlstore/email.go:171-207`
- Modify: `sqlstore/email_claim_sql_test.go`

**Interfaces:**
- Produces: `func (s *Store) dueEmailsStatement(claim ntfy.EmailClaim, now time.Time) sqlkit.Statement`
- Consumes: `func (s *Store) dueEmails(ctx context.Context, claim ntfy.EmailClaim, now time.Time) ([]dueEmail, error)` — signature unchanged.

- [ ] **Step 1: Extract the builder**

Replace `dueEmails` (`sqlstore/email.go:171-207`) with:

```go
// dueEmailsStatement builds the statement dueEmails runs.
func (s *Store) dueEmailsStatement(claim ntfy.EmailClaim, now time.Time) sqlkit.Statement {
	n := func(name string) string { return "n." + s.quote(name) }
	d := func(name string) string { return "d." + s.quote(name) }

	instant := sqlkit.EncodeTime(s.dialect, &now)
	createdUntil := sqlkit.NormalizeTime(claim.CreatedUntil)
	createdFrom := sqlkit.NormalizeTime(claim.CreatedFrom)

	qualifies := func(w *sqlkit.Writer) {
		w.Write(n("state"), " = ", w.Bind(string(ntfy.StateActive)),
			" AND ", n("created_at"), " <= ", w.Bind(sqlkit.EncodeTime(s.dialect, &createdUntil)),
			" AND ", n("created_at"), " >= ", w.Bind(sqlkit.EncodeTime(s.dialect, &createdFrom)))
	}

	w := sqlkit.NewWriter(s.dialect)
	w.Write("SELECT ", n("id"), ", COALESCE(", d("notification_id"), ", '')",
		" FROM ", s.notificationsTable(), " n LEFT JOIN ", s.emailTable(), " d ON ", d("notification_id"), " = ", n("id"),
		" WHERE (", d("notification_id"), " IS NULL AND ")
	qualifies(w)
	w.Write(") OR (", d("status"), " = ", w.Bind(string(ntfy.EmailStatusSending)), " AND ")
	s.writeLapsed(w, d, instant)
	w.Write(") OR (", d("status"), " <> ", w.Bind(string(ntfy.EmailStatusSending)), " AND ")
	s.writeLapsed(w, d, instant)
	w.Write(" AND ")
	qualifies(w)
	w.Write(") ORDER BY ", n("created_at"), ", ", n("id"), " LIMIT ", strconv.Itoa(claim.Limit))

	return w.Done()
}

// dueEmails selects, oldest first, up to the claim's limit of notifications due
// for email.
func (s *Store) dueEmails(ctx context.Context, claim ntfy.EmailClaim, now time.Time) ([]dueEmail, error) {
	var due []dueEmail

	err := s.queryTexts(ctx, s.dueEmailsStatement(claim, now), 2, func(values []string) {
		due = append(due, dueEmail{id: values[0], recorded: values[1] != ""})
	})

	return due, err
}
```

- [ ] **Step 2: Build and run the SQLite conformance suite**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS — the extraction changes no SQL.

- [ ] **Step 3: Commit**

```bash
git add sqlstore/email.go
git commit -m "Extract the email claim statement from its execution"
```

---

### Task 4: Rewrite the claim as three `UNION ALL` branches

**Files:**
- Modify: `sqlstore/email.go` — `dueEmailsStatement`, `dueEmails`
- Modify: `sqlstore/email_claim_sql_test.go`

**Interfaces:**
- Consumes: `writeDueRetry`, `writeInDoubt` (Task 2); `dueEmailsStatement` (Task 3).
- Produces: the same `dueEmailsStatement` signature, now writing the compound statement; `dueEmail.recorded` now read from a `'0'`/`'1'` literal rather than a `COALESCE`.

- [ ] **Step 1: Write the equivalence fixture test**

A characterization test again: it must pass before and after, and it is the proof that the rewrite preserves behaviour. Add to `sqlstore/email_claim_sql_test.go`'s neighbours — put it in the external suite so it runs on every dialect through the matrix. Create the case inside `sqlstore/email_claim_equivalence_test.go`:

```go
package sqlstore_test

// The rows the claim returns, over a fixture covering every branch and its
// boundaries. It must hold before and after the query is restructured.

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

// claimFixture publishes one notification per branch and boundary and returns
// the identifiers a claim must return, oldest first.
//
//	in-window ACTIVE, no delivery record          claimed   (branch 1)
//	in-window ACTIVE, SENDING, lease lapsed       claimed   (branch 2)
//	CLOSED,           SENDING, lease lapsed       claimed   (branch 2: state is not consulted)
//	in-window ACTIVE, SENDING, lease live         skipped
//	in-window ACTIVE, RETRY,   due, lease lapsed  claimed   (branch 3)
//	in-window ACTIVE, CLAIMED, lease lapsed       claimed   (branch 3)
//	in-window ACTIVE, RETRY,   not yet due        skipped
//	CLOSED,           RETRY,   due                skipped   (branch 3 needs ACTIVE)
//	in-window ACTIVE, SENT                        skipped   (terminal)
//	in-window ACTIVE, FAILED                      skipped   (terminal)
//	older than the lag, ACTIVE, no record         skipped
//	newer than the grace, ACTIVE, no record       skipped
func TestClaimReturnsTheSameRows(t *testing.T) { /* Step 2 fills this in */ }
```

- [ ] **Step 2: Fill in the equivalence test**

```go
func TestClaimReturnsTheSameRows(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "sqlite", sqlkittest.RunTestSQLite(t))
	executor := stdsqlExecutor(t, db, sqlkit.SQLite)
	store := harness.NewEmailStore(t, executor)

	now := sqlkit.NormalizeTime(time.Now().UTC())
	grace, lag, lease := ntfy.DefaultEmailGraceDelay, ntfy.DefaultEmailMaxLag, ntfy.DefaultEmailLease

	// inWindow is an instant a claim considers: older than the grace delay,
	// newer than the lag bound.
	inWindow := now.Add(-time.Hour)

	type row struct {
		name     string
		created  time.Time
		state    ntfy.State
		status   ntfy.EmailStatus
		lease    *time.Time
		next     *time.Time
		claimed  bool
	}

	lapsed, live := now.Add(-time.Minute), now.Add(time.Hour)
	due, notDue := now.Add(-time.Minute), now.Add(time.Hour)

	rows := []row{
		{name: "unrecorded", created: inWindow, state: ntfy.StateActive, claimed: true},
		{name: "in doubt", created: inWindow, state: ntfy.StateActive, status: ntfy.EmailStatusSending, lease: &lapsed, claimed: true},
		{name: "in doubt, closed", created: inWindow, state: ntfy.StateClosed, status: ntfy.EmailStatusSending, lease: &lapsed, claimed: true},
		{name: "in doubt, lease live", created: inWindow, state: ntfy.StateActive, status: ntfy.EmailStatusSending, lease: &live},
		{name: "retry due", created: inWindow, state: ntfy.StateActive, status: ntfy.EmailStatusRetry, lease: &lapsed, next: &due, claimed: true},
		{name: "claimed lapsed", created: inWindow, state: ntfy.StateActive, status: ntfy.EmailStatusClaimed, lease: &lapsed, claimed: true},
		{name: "retry not due", created: inWindow, state: ntfy.StateActive, status: ntfy.EmailStatusRetry, lease: &lapsed, next: &notDue},
		{name: "retry due, closed", created: inWindow, state: ntfy.StateClosed, status: ntfy.EmailStatusRetry, lease: &lapsed, next: &due},
		{name: "sent", created: inWindow, state: ntfy.StateActive, status: ntfy.EmailStatusSent},
		{name: "failed", created: inWindow, state: ntfy.StateActive, status: ntfy.EmailStatusFailed},
		{name: "older than the lag", created: now.Add(-lag - time.Hour), state: ntfy.StateActive},
		{name: "newer than the grace", created: now.Add(-grace / 2), state: ntfy.StateActive},
	}

	var want []string

	for _, r := range rows {
		id := seedRow(t, store, executor, r.name, r.created, r.state, r.status, r.lease, r.next)
		if r.claimed {
			want = append(want, id)
		}
	}

	claimed, err := store.ClaimEmails(t.Context(), ntfy.EmailClaim{
		Now: now, Owner: "equivalence", Lease: lease,
		CreatedUntil: now.Add(-grace), CreatedFrom: now.Add(-lag),
		Limit:        ntfy.DefaultEmailClaimLimit,
	})
	require.NoError(t, err)

	var got []string

	for _, c := range claimed {
		got = append(got, c.Notification.ID)
	}

	assert.ElementsMatch(t, want, got)
}
```

`seedRow` writes one notification and, when `status` is set, one delivery record, returning the identifier. Add it to the same file:

```go
// seedRow writes one notification and, for a non-empty status, the delivery
// record that puts it in a branch. It returns the notification's identifier.
func seedRow(
	t *testing.T, store *sqlstore.Store, executor sqlkit.Executor,
	name string, created time.Time, state ntfy.State, status ntfy.EmailStatus, lease, next *time.Time,
) string {
	t.Helper()

	ids := ntfy.NewUUIDv7Generator()

	id, err := ids.NewID()
	require.NoError(t, err)

	_, err = store.Insert(t.Context(), name, []ntfy.Insertion{{Notification: ntfy.Notification{
		ID: id, Recipient: "alice", SourceID: "src-" + id, Subject: name,
		Kind: "offer", State: ntfy.StateActive, CreatedAt: sqlkit.NormalizeTime(created),
	}}})
	require.NoError(t, err)

	if state != ntfy.StateActive {
		_, err = store.Close(t.Context(), ntfy.CloseRequest{Subject: name, Version: 1, Reason: "fixture"},
			sqlkit.NormalizeTime(created.Add(time.Minute)), ids)
		require.NoError(t, err)
	}

	if status == "" {
		return id
	}

	dialect := executor.Dialect()
	table := dialect.Quote(strings.TrimSuffix(store.Tables()[0], sqlstore.NotificationsTable) + sqlstore.EmailDeliveriesTable)

	w := sqlkit.NewWriter(dialect)
	w.Write("INSERT INTO ", table, " (",
		dialect.Quote("notification_id"), ", ", dialect.Quote("recipient"), ", ", dialect.Quote("status"), ", ",
		dialect.Quote("owner"), ", ", dialect.Quote("lease_until"), ", ", dialect.Quote("attempts"), ", ",
		dialect.Quote("next_attempt_at"), ", ", dialect.Quote("updated_at"), ") VALUES (",
		w.BindAll(id, "alice", string(status), "fixture",
			sqlkit.EncodeTime(dialect, lease), 1, sqlkit.EncodeTime(dialect, next),
			sqlkit.EncodeTime(dialect, &created)), ")")

	_, err = executor.Exec(t.Context(), w.Done())
	require.NoError(t, err)

	return id
}
```

Add `"strings"` to that file's imports.

- [ ] **Step 3: Run the equivalence test against the current query**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestClaimReturnsTheSameRows' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS. It describes today's behaviour; if it fails, the fixture's expectations are wrong and must be fixed before the rewrite, or the rewrite will be measured against a false baseline.

- [ ] **Step 4: Rewrite the builder**

Replace `dueEmailsStatement` with:

```go
// dueEmailsStatement builds the statement dueEmails runs: three disjoint
// branches, each driving from the table its own index covers.
//
//	branch 1  notifications no delivery record covers
//	branch 2  a send left in doubt, whatever the notification's state now is
//	branch 3  a claimed or retrying record whose lease lapsed and whose
//	          notification still qualifies
//
// Each branch takes the claim's limit and the union takes it again, so the
// result is the same oldest-first page the single query returned. Every branch
// is wrapped in a derived table because SQLite allows no LIMIT on a compound
// select's branches.
func (s *Store) dueEmailsStatement(claim ntfy.EmailClaim, now time.Time) sqlkit.Statement {
	n := func(name string) string { return "n." + s.quote(name) }
	d := func(name string) string { return "d." + s.quote(name) }

	instant := sqlkit.EncodeTime(s.dialect, &now)
	createdUntil := sqlkit.NormalizeTime(claim.CreatedUntil)
	createdFrom := sqlkit.NormalizeTime(claim.CreatedFrom)
	limit := " LIMIT " + strconv.Itoa(claim.Limit)

	w := sqlkit.NewWriter(s.dialect)

	// qualifies is the notification side of a claim. Branch 2 deliberately
	// omits it: an in-doubt send is settled whatever the notification's state.
	qualifies := func() {
		w.Write(n("state"), " = ", w.Bind(string(ntfy.StateActive)),
			" AND ", n("created_at"), " <= ", w.Bind(sqlkit.EncodeTime(s.dialect, &createdUntil)),
			" AND ", n("created_at"), " >= ", w.Bind(sqlkit.EncodeTime(s.dialect, &createdFrom)))
	}

	// open starts a branch, projecting the three union columns with recorded as
	// a literal.
	open := func(alias, recorded string) {
		w.Write("SELECT ", alias, ".", s.quote("id"), " AS ", s.quote("id"),
			", ", alias, ".", s.quote("created_at"), " AS ", s.quote("created_at"),
			", ", alias, ".", s.quote("recorded"), " AS ", s.quote("recorded"),
			" FROM (SELECT ", n("id"), " AS ", s.quote("id"),
			", ", n("created_at"), " AS ", s.quote("created_at"),
			", ", recorded, " AS ", s.quote("recorded"), " FROM ")
	}

	// closeBranch ends a branch: oldest first, at most the claim's limit.
	closeBranch := func(alias string) {
		w.Write(" ORDER BY ", n("created_at"), ", ", n("id"), limit, ") ", alias)
	}

	// joined opens the delivery-driven branches, inner-joined so that a record
	// whose notification is gone is not returned.
	joined := func() {
		w.Write(s.emailTable(), " d JOIN ", s.notificationsTable(), " n ON ", n("id"), " = ", d("notification_id"), " WHERE ")
	}

	w.Write("SELECT due.", s.quote("id"), ", due.", s.quote("recorded"), " FROM (")

	open("b1", "'0'")
	w.Write(s.notificationsTable(), " n WHERE ")
	qualifies()
	w.Write(" AND NOT EXISTS (SELECT 1 FROM ", s.emailTable(), " d WHERE ", d("notification_id"), " = ", n("id"), ")")
	closeBranch("b1")

	w.Write(" UNION ALL ")

	open("b2", "'1'")
	joined()
	s.writeInDoubt(w, d, instant)
	closeBranch("b2")

	w.Write(" UNION ALL ")

	open("b3", "'1'")
	joined()
	s.writeDueRetry(w, d, instant)
	w.Write(" AND ")
	qualifies()
	closeBranch("b3")

	w.Write(") due ORDER BY due.", s.quote("created_at"), ", due.", s.quote("id"), limit)

	return w.Done()
}
```

- [ ] **Step 5: Update the row reader**

In `dueEmails`, change the `recorded` inference from the old `COALESCE` emptiness test to the branch literal:

```go
	err := s.queryTexts(ctx, s.dueEmailsStatement(claim, now), 2, func(values []string) {
		due = append(due, dueEmail{id: values[0], recorded: values[1] == "1"})
	})
```

- [ ] **Step 6: Run the equivalence test**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestClaimReturnsTheSameRows' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS, with the same expectations as Step 3, unedited.

- [ ] **Step 7: Assert the generated SQL, per dialect**

Add to `sqlstore/email_claim_sql_test.go`:

```go
// claimSQL is the text dueEmailsStatement writes for a dialect.
func claimSQL(t *testing.T, dialect sqlkit.Dialect) string {
	t.Helper()

	store := storeFor(t, dialect)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	return store.dueEmailsStatement(ntfy.EmailClaim{
		Now: now, Owner: "test", Lease: 5 * time.Minute,
		CreatedUntil: now.Add(-5 * time.Minute), CreatedFrom: now.Add(-24 * time.Hour), Limit: 500,
	}, now).SQL
}

func TestClaimStatementShape(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		dialect sqlkit.Dialect
		assert  func(t *testing.T, sql string)
	}

	shape := func(t *testing.T, sql string) {
		t.Helper()

		assert.Equal(t, 2, strings.Count(sql, " UNION ALL "), "three branches")
		assert.Equal(t, 4, strings.Count(sql, " LIMIT 500"), "one limit per branch and one for the union")
		assert.Contains(t, sql, "NOT EXISTS", "branch 1 is an anti-join")
		assert.NotContains(t, sql, "LEFT JOIN", "no branch drives through an outer join")
		assert.True(t, strings.HasSuffix(sql, " LIMIT 500"), "the union is limited last")
	}

	cases := []testCase{
		{
			name:    "PostgreSQL",
			dialect: sqlkit.PostgreSQL,
			assert: func(t *testing.T, sql string) {
				shape(t, sql)
				assert.Equal(t,
					`SELECT due."id", due."recorded" FROM (`+
						`SELECT b1."id" AS "id", b1."created_at" AS "created_at", b1."recorded" AS "recorded"`+
						` FROM (SELECT n."id" AS "id", n."created_at" AS "created_at", '0' AS "recorded"`+
						` FROM "ntfy_notifications" n WHERE n."state" = $1 AND n."created_at" <= $2 AND n."created_at" >= $3`+
						` AND NOT EXISTS (SELECT 1 FROM "ntfy_email_deliveries" d WHERE d."notification_id" = n."id")`+
						` ORDER BY n."created_at", n."id" LIMIT 500) b1`+
						` UNION ALL `+
						`SELECT b2."id" AS "id", b2."created_at" AS "created_at", b2."recorded" AS "recorded"`+
						` FROM (SELECT n."id" AS "id", n."created_at" AS "created_at", '1' AS "recorded"`+
						` FROM "ntfy_email_deliveries" d JOIN "ntfy_notifications" n ON n."id" = d."notification_id"`+
						` WHERE (d."status" = $4 AND (d."lease_until" IS NULL OR d."lease_until" <= $5))`+
						` ORDER BY n."created_at", n."id" LIMIT 500) b2`+
						` UNION ALL `+
						`SELECT b3."id" AS "id", b3."created_at" AS "created_at", b3."recorded" AS "recorded"`+
						` FROM (SELECT n."id" AS "id", n."created_at" AS "created_at", '1' AS "recorded"`+
						` FROM "ntfy_email_deliveries" d JOIN "ntfy_notifications" n ON n."id" = d."notification_id"`+
						` WHERE (d."status" IN ($6, $7) AND (d."lease_until" IS NULL OR d."lease_until" <= $8)`+
						` AND (d."next_attempt_at" IS NULL OR d."next_attempt_at" <= $9))`+
						` AND n."state" = $10 AND n."created_at" <= $11 AND n."created_at" >= $12`+
						` ORDER BY n."created_at", n."id" LIMIT 500) b3`+
						`) due ORDER BY due."created_at", due."id" LIMIT 500`,
					sql)
			},
		},
		{name: "MySQL", dialect: sqlkit.MySQL, assert: shape},
		{name: "SQLite", dialect: sqlkit.SQLite, assert: shape},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, claimSQL(t, tc.dialect))
		})
	}
}
```

Add `"strings"` and `"time"` to that file's imports.

- [ ] **Step 8: Run the SQL shape test**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestClaimStatementShape' -count=1 ./...` from `sqlstore/`
Expected: PASS. If the PostgreSQL text differs, compare character by character and fix the expected string to match what the builder writes — then re-read the builder to confirm the difference was cosmetic, not semantic.

- [ ] **Step 9: Commit**

```bash
git add sqlstore/email.go sqlstore/email_claim_sql_test.go sqlstore/email_claim_equivalence_test.go
git commit -m "Claim due emails through three indexable branches"
```

---

### Task 5: Prove the preserved branch semantics on every dialect

**Files:**
- Modify: `sqlstore/email_claim_equivalence_test.go`

**Interfaces:**
- Consumes: `seedRow`, `openSQL`, `stdsqlExecutor` (existing, `harness_test.go`).

- [ ] **Step 1: Run the equivalence fixture on all three dialects**

Replace the single SQLite body of `TestClaimReturnsTheSameRows` with a table over the three dialects, each starting its own container:

```go
	type testCase struct {
		name     string
		dialect  sqlkit.Dialect
		executor func(t *testing.T) sqlkit.Executor
	}

	cases := []testCase{
		{
			name: "PostgreSQL", dialect: sqlkit.PostgreSQL,
			executor: func(t *testing.T) sqlkit.Executor {
				return stdsqlExecutor(t, openSQL(t, "postgres", sqlkittest.RunTestPostgres(t)), sqlkit.PostgreSQL)
			},
		},
		{
			name: "MySQL", dialect: sqlkit.MySQL,
			executor: func(t *testing.T) sqlkit.Executor {
				return stdsqlExecutor(t, openSQL(t, "mysql", sqlkittest.RunTestMySQL(t)), sqlkit.MySQL)
			},
		},
		{
			name: "SQLite", dialect: sqlkit.SQLite,
			executor: func(t *testing.T) sqlkit.Executor {
				return stdsqlExecutor(t, openSQL(t, "sqlite", sqlkittest.RunTestSQLite(t)), sqlkit.SQLite)
			},
		},
	}
```

with the body of the existing test moved into the per-case closure.

- [ ] **Step 2: Run it**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestClaimReturnsTheSameRows' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS on all three. A dialect-specific failure here is a real portability defect in the compound statement — most likely the derived-table aliases or the per-branch `LIMIT` — and must be fixed in the builder, not in the fixture.

- [ ] **Step 3: Run the email conformance suites unedited**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLPostgres|TestStoreOnStdSQLMySQL|TestStoreOnStdSQLSQLite|TestStoreOnPgxPostgres' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS, including the `email` and `email-dispatch` subtests. **If any conformance test needs editing to pass, stop:** that means the rewrite changed what is claimed, which this change forbids (`design.md` — Risks).

- [ ] **Step 4: Commit**

```bash
git add sqlstore/email_claim_equivalence_test.go
git commit -m "Check the claim's branch semantics on every dialect"
```

---

### Task 6: Declare the index in the three email documents

**Files:**
- Modify: `sqlstore/ddl/email/postgres.sql`, `ddl/email/sqlite.sql`, `ddl/email/mysql.sql`
- Modify: `sqlstore/email.go` (the index constant)
- Modify: `sqlstore/email_schema_test.go`
- Modify: `sqlstore/testdata/schema/email_postgres.sql`, `email_postgres_app_.sql`, `email_mysql.sql`, `email_mysql_app_.sql`, `email_sqlite.sql`, `email_sqlite_app_.sql`

**Interfaces:**
- Produces: `const emailNotificationsIndex = "ntfy_notifications_email_idx"` in `sqlstore/email.go`, used by Tasks 7 and 8.

- [ ] **Step 1: Add the constant**

In `sqlstore/email.go`, beneath `EmailDeliveriesTable`:

```go
// emailNotificationsIndex is the index on the notifications table that serves
// the claim's unrecorded branch. It is declared in the email documents, and
// required only by [Store.VerifyEmailSchema]: a host that never emails neither
// queries it nor pays to maintain it.
const emailNotificationsIndex = "ntfy_notifications_email_idx"
```

- [ ] **Step 2: Add the index to the PostgreSQL email document**

Append to `sqlstore/ddl/email/postgres.sql`:

```sql
-- Claiming notifications no delivery record covers. This index is on the
-- notification store's table, not this document's own: only a host that emails
-- runs the claim, so only a host that emails pays to maintain it.
CREATE INDEX IF NOT EXISTS "{{PREFIX}}ntfy_notifications_email_idx"
    ON "{{PREFIX}}ntfy_notifications" ("state", "created_at", "id");
```

- [ ] **Step 3: Add the index to the SQLite email document**

Append the same statement to `sqlstore/ddl/email/sqlite.sql`.

- [ ] **Step 4: Add the index to the MySQL email document, with its stated exception**

Append to `sqlstore/ddl/email/mysql.sql`:

```sql
-- Claiming notifications no delivery record covers. This index is on the
-- notification store's table, not this document's own: only a host that emails
-- runs the claim, so only a host that emails pays to maintain it.
--
-- This is the one statement in any ntfy document that is NOT idempotent. MySQL
-- has no CREATE INDEX IF NOT EXISTS, and the index belongs to a table this
-- document does not create, so it cannot be declared inside a CREATE TABLE as
-- every other MySQL index here is. Applying this document twice fails with
-- error 1061; an existing MySQL host adds the index with
--
--     ALTER TABLE `{{PREFIX}}ntfy_notifications`
--         ADD KEY `{{PREFIX}}ntfy_notifications_email_idx` (`state`, `created_at`, `id`);
--
-- Store.MigrateEmail skips this statement when the index is already present, so
-- the repository's own development and test flows stay re-runnable.
CREATE INDEX `{{PREFIX}}ntfy_notifications_email_idx`
    ON `{{PREFIX}}ntfy_notifications` (`state`, `created_at`, `id`);
```

- [ ] **Step 5: Narrow the golden test's "creates no notification table" assertion**

In `sqlstore/email_schema_test.go`, the `golden` helper currently asserts the whole document mentions no notification table, which the new index breaks. Replace those two loops with one that checks table *creation* only:

```go
		for _, statement := range sqlkit.SplitStatements(schema) {
			assert.Truef(t, strings.HasPrefix(statement, "CREATE "), "%q", statement)

			if !strings.HasPrefix(statement, "CREATE TABLE") {
				continue
			}

			for _, table := range store.Tables() {
				assert.NotContains(t, statement, `"`+table+`"`, "the email schema creates no notification table")
				assert.NotContains(t, statement, "`"+table+"`", "the email schema creates no notification table")
			}
		}
```

- [ ] **Step 6: Run the golden test and watch it fail on the golden files**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestEmailSchema' -count=1 ./...` from `sqlstore/`
Expected: FAIL — six cases reporting that the rendered schema differs from its golden file by the new `CREATE INDEX`. That is the intended failure: the assertion change passed, the goldens are stale.

- [ ] **Step 7: Regenerate the golden files**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestEmailSchema' -count=1 -update ./...` from `sqlstore/`
Then: `git diff --stat sqlstore/testdata/schema/`
Expected: six files changed, each gaining the index statement, with `app_` prefixes applied in the three prefixed goldens.

- [ ] **Step 8: Verify the documents apply twice where they claim to**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifyEmailSchemaOnPostgres|TestVerifyEmailSchemaOnSQLite' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS, including the existing case "a freshly migrated email schema, migrated twice, verifies" — proof that `IF NOT EXISTS` holds on those two dialects. MySQL is Task 7.

- [ ] **Step 9: Commit**

```bash
git add sqlstore/ddl/email sqlstore/email.go sqlstore/email_schema_test.go sqlstore/testdata/schema
git commit -m "Declare the claim index in the email schema documents"
```

---

### Task 7: Keep `MigrateEmail` re-runnable on MySQL

**Files:**
- Modify: `sqlstore/email.go:69-73`
- Modify: `sqlstore/email_verify_test.go`

**Interfaces:**
- Produces: `func (s *Store) mysqlHasIndex(ctx context.Context, table, index string) (bool, error)`
- Consumes: `Store.queryInt` (`sqlstore/sql.go:337`), `sqlkit.RenderSchema`, `sqlkit.ApplySchema`.

- [ ] **Step 1: Write the failing test**

Add to `sqlstore/email_verify_test.go`:

```go
func TestMigrateEmailIsRepeatableOnMySQL(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "mysql", sqlkittest.RunTestMySQL(t))
	executor := stdsqlExecutor(t, db, sqlkit.MySQL)
	store := harness.NewEmailStore(t, executor)

	require.NoError(t, store.MigrateEmail(t.Context()), "a second migration adds nothing and fails nothing")
	assert.NoError(t, store.VerifyEmailSchema(t.Context()))
}
```

- [ ] **Step 2: Run it and watch it fail with MySQL error 1061**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMigrateEmailIsRepeatableOnMySQL' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: FAIL with `Error 1061 (42000): Duplicate key name '..._ntfy_notifications_email_idx'`, named by `ApplySchema`'s wrapper. Confirm the message says 1061 — a different failure means something else is wrong.

- [ ] **Step 3: Add the index probe**

In `sqlstore/email.go`:

```go
// mysqlHasIndex reports whether an index is present on a table in the
// connection's current schema. MySQL has no CREATE INDEX IF NOT EXISTS, and
// sqlkit's ApplySchema tolerates no error, so MigrateEmail asks first.
func (s *Store) mysqlHasIndex(ctx context.Context, table, index string) (bool, error) {
	w := sqlkit.NewWriter(s.dialect)
	w.Write("SELECT COUNT(*) FROM information_schema.STATISTICS",
		" WHERE TABLE_SCHEMA = DATABASE()",
		" AND TABLE_NAME = ", w.Bind(table),
		" AND INDEX_NAME = ", w.Bind(index))

	count, err := s.queryInt(ctx, w.Done())

	return count > 0, err
}
```

- [ ] **Step 4: Guard the statement in `MigrateEmail`**

```go
// MigrateEmail applies the email delivery schema. Like [Store.Migrate] it exists
// for tests and development.
//
// On MySQL the index this document adds to the notifications table cannot be
// written as IF NOT EXISTS, so an existing one is checked for and its statement
// skipped, keeping repeated calls a no-op as they are on the other dialects.
func (s *Store) MigrateEmail(ctx context.Context) error {
	statements := sqlkit.RenderSchema(s.emailDocument(), s.prefix)

	if s.dialect.Name() == sqlkit.MySQL.Name() {
		index := s.prefix + emailNotificationsIndex

		present, err := s.mysqlHasIndex(s.own(ctx), s.prefix+NotificationsTable, index)
		if err != nil {
			return err
		}

		if present {
			statements = slices.DeleteFunc(statements, func(statement string) bool {
				return strings.Contains(statement, index)
			})
		}
	}

	return sqlkit.ApplySchema(ctx, s.execer, s.dialect, statements)
}
```

`slices` and `strings` are already imported in this file.

- [ ] **Step 5: Run the test again**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMigrateEmailIsRepeatableOnMySQL' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS.

- [ ] **Step 6: Confirm the frozen copy is untouched**

Run: `make sqlkit-copy-check`
Expected: PASS — the guard lives in `sqlstore`, never in `pkg/sqlkit`.

- [ ] **Step 7: Commit**

```bash
git add sqlstore/email.go sqlstore/email_verify_test.go
git commit -m "Keep MigrateEmail repeatable on MySQL"
```

---

### Task 8: Require the index in `VerifyEmailSchema` only

**Files:**
- Modify: `sqlstore/email.go:34-45`
- Modify: `sqlstore/email_verify_test.go`

**Interfaces:**
- Consumes: `emailNotificationsIndex` (Task 6); `sqlkit.SchemaExpectation`, whose `indexIssues` reports nothing for a table absent from the live schema and ignores an empty `Columns` list (`pkg/sqlkit/verify.go:224-241`).

- [ ] **Step 1: Write the failing tests**

In `sqlstore/email_verify_test.go`, extend `dropEmailIndex` to take the table it drops from, and add two cases to `runVerifyEmailSchema`:

```go
// dropEmailIndex drops a prefixed index from a table in the executor's dialect.
func dropEmailIndex(t *testing.T, executor sqlkit.Executor, store *sqlstore.Store, table, index string) {
	t.Helper()

	dialect := executor.Dialect()
	name := dialect.Quote(prefixOf(store) + index)

	if dialect.Name() == sqlkit.MySQL.Name() {
		exec(t, executor, "DROP INDEX "+name+" ON "+dialect.Quote(table))

		return
	}

	exec(t, executor, "DROP INDEX "+name)
}
```

Update the two existing call sites to pass `store.EmailTables()[0]`, then add:

```go
		{
			name:  "a missing claim index on the notifications table is reported",
			store: withEmail,
			breaks: func(t *testing.T, store *sqlstore.Store) {
				dropEmailIndex(t, executor, store, store.Tables()[0], "ntfy_notifications_email_idx")
			},
			verify: verifyEmail,
			assert: func(t *testing.T, store *sqlstore.Store, err error) {
				assert.Contains(t, issues(t, err), "index "+prefixOf(store)+"ntfy_notifications_email_idx is missing")
			},
		},
		{
			name:   "the notification schema verifies without the claim index",
			store:  func(t *testing.T) *sqlstore.Store { return harness.NewStore(t, executor) },
			breaks: unbroken,
			verify: func(t *testing.T, store *sqlstore.Store) error { return store.VerifySchema(t.Context()) },
			assert: func(t *testing.T, _ *sqlstore.Store, err error) { assert.NoError(t, err) },
		},
```

- [ ] **Step 2: Run them and watch the first fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifyEmailSchemaOnPostgres' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: FAIL on "a missing claim index on the notifications table is reported" — `VerifyEmailSchema` returns nil today, because the expectation does not mention the index, so `issues` fails on `require.ErrorIs(t, err, sqlkit.ErrSchemaMismatch)`. The second new case passes already and must keep passing.

- [ ] **Step 3: Require the index in the email expectation only**

In `sqlstore/email.go`:

```go
// emailSchemaExpectation is what [Store.VerifyEmailSchema] requires, before the
// table prefix.
//
// The notifications entry carries no columns: the base expectation already
// checks them, and this one adds only the index the claim needs. Requiring it
// here rather than in schemaExpectation is what keeps the email schema optional
// — a host that never emails verifies clean without it.
var emailSchemaExpectation = sqlkit.SchemaExpectation{
	EmailDeliveriesTable: {
		Columns: []string{
			"notification_id", "recipient", "status", "batch_id", "owner", "lease_until", "attempts",
			"next_attempt_at", "reason", "sent_at", "updated_at",
		},
		IdentifierColumns: []string{"notification_id", "recipient", "status", "batch_id", "owner"},
		Indexes:           []string{"ntfy_email_deliveries_lease_idx", "ntfy_email_deliveries_retry_idx"},
	},
	NotificationsTable: {
		Indexes: []string{emailNotificationsIndex},
	},
}
```

- [ ] **Step 4: Run the verification suites on all three dialects**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestVerifyEmailSchema' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS, including "the notification schema verifies without the claim index" and the existing "a missing email table is reported" (the notifications entry must not make that case report extra issues).

- [ ] **Step 5: Commit**

```bash
git add sqlstore/email.go sqlstore/email_verify_test.go
git commit -m "Require the claim index when verifying the email schema"
```

---

### Task 9: Re-measure against the threshold

**Files:**
- Modify: `sqlstore/email_claim_measure_test.go`
- Modify: `openspec/changes/scale-email-claim-query/measurements.md`

- [ ] **Step 1: Add the threshold assertions to the measurement**

Append to the body of each `TestMeasureClaim` case, after the plan is captured:

```go
			switch tc.dialect.Name() {
			case sqlkit.PostgreSQL.Name():
				assert.NotContains(t, plan, "Seq Scan on ntfy_notifications", "threshold 1")
				assert.Less(t, medianOf(empty), 100*time.Millisecond, "threshold 3")
			case sqlkit.MySQL.Name():
				assert.NotContains(t, plan, `"access_type": "ALL"`, "threshold 1")
			default:
				assert.NotContains(t, plan, "SCAN ntfy_notifications\n", "threshold 1")
			}
```

and after the window-versus-table measurement:

```go
			assert.Less(t, medianOf(grown), 2*medianOf(empty),
				"threshold 2: cost scales with the window, not the table")
```

Add `"github.com/stretchr/testify/assert"` to the file's imports.

- [ ] **Step 2: Add the working-pass measurement**

Append a second seeded case to the same test, where the in-window rows are claimable:

```go
			seed(t, store, seedShape{
				InWindow: ntfy.DefaultEmailClaimLimit * 4, Recorded: 0, Outside: 0,
				Now: now, Grace: ntfy.DefaultEmailGraceDelay, MaxLag: ntfy.DefaultEmailMaxLag,
			})

			var working []time.Duration

			for range 5 {
				took, claimed := timeClaim(t, store, claim)
				require.Equal(t, ntfy.DefaultEmailClaimLimit, claimed)

				working = append(working, took)
			}

			t.Logf("500-candidate pass median: %s", medianOf(working))

			if tc.dialect.Name() == sqlkit.PostgreSQL.Name() {
				assert.Less(t, medianOf(working), 500*time.Millisecond, "threshold 4")
			}
```

- [ ] **Step 3: Run the measurement on all three dialects**

Run: `GOTOOLCHAIN=go1.26.8 NTFY_MEASURE_ROWS=1000000 go test -run 'TestMeasureClaim' -timeout 60m -v ./...` from `sqlstore/`
Expected: PASS, with plans showing index scans: PostgreSQL an `Index Scan`/`Index Only Scan` using `ntfy_notifications_email_idx` for branch 1 and `ntfy_email_deliveries_lease_idx` / `_retry_idx` for branches 2 and 3; MySQL `"key": "..._ntfy_notifications_email_idx"` with `access_type` other than `ALL`; SQLite `SEARCH ... USING INDEX ..._ntfy_notifications_email_idx`.

- [ ] **Step 4: Measure the index's cost on the write side**

Add a second test to the same file:

```go
func TestMeasurePublishWithClaimIndex(t *testing.T) {
	rows := measureRows(t)
	store := measureStore(t, sqlkit.PostgreSQL)
	now := sqlkit.NormalizeTime(time.Now().UTC())

	with := time.Now()
	seed(t, store, seedShape{InWindow: rows, Now: now, Grace: ntfy.DefaultEmailGraceDelay, MaxLag: ntfy.DefaultEmailMaxLag})
	withIndex := time.Since(with)

	exec := func(sql string) {
		_, err := store.executor.Exec(t.Context(), sqlkit.Statement{SQL: sql})
		require.NoError(t, err)
	}

	exec("TRUNCATE " + store.notificationsTable() + ", " + store.emailTable())
	exec("DROP INDEX " + store.quote(store.prefix+emailNotificationsIndex))

	without := time.Now()
	seed(t, store, seedShape{InWindow: rows, Now: now, Grace: ntfy.DefaultEmailGraceDelay, MaxLag: ntfy.DefaultEmailMaxLag})
	withoutIndex := time.Since(without)

	t.Logf("seeding %d rows: %s with the index, %s without", rows, withIndex, withoutIndex)
}
```

Run: `GOTOOLCHAIN=go1.26.8 NTFY_MEASURE_ROWS=1000000 go test -run 'TestMeasurePublishWithClaimIndex' -timeout 60m -v ./...` from `sqlstore/`
Expected: two durations logged, the difference recorded as the trade-off `design.md` — Risks calls for.

- [ ] **Step 5: Write the results into the change directory**

Fill in `measurements.md`'s "After" section: the three plans, the empty-pass medians before and after the extra out-of-window rows, the 500-candidate median, the write-side figures, and one line per threshold saying met or missed. **Report a missed threshold as a finding; never lower the threshold.**

- [ ] **Step 6: Commit**

```bash
git add sqlstore/email_claim_measure_test.go openspec/changes/scale-email-claim-query/measurements.md
git commit -m "Measure the restructured claim against its thresholds"
```

---

### Task 10: Document the schema change and the migration

**Files:**
- Modify: `docs/schema.md`

- [ ] **Step 1: Add the index to the index table**

In `docs/schema.md` — Indexes, add a row and a sentence beneath the table:

```markdown
| `ntfy_notifications_email_idx` | `state`, `created_at`, `id` | claiming notifications due for email (email schema) |
```

```markdown
The last index belongs to the **email** schema, not the notification schema: it
is declared in `ddl/email/*.sql` and required by `VerifyEmailSchema`, so a host
that never emails neither carries it nor fails startup over it.
```

- [ ] **Step 2: State the one exception to the idempotence promise**

In `docs/schema.md` — Where the DDL lives, immediately after "Every statement is `CREATE ... IF NOT EXISTS`, so applying it to an existing schema changes nothing.":

```markdown
There is one exception, in the email schema only. MySQL has no `CREATE INDEX IF
NOT EXISTS`, and `ntfy_notifications_email_idx` sits on a table the email
document does not create, so it cannot be declared inside a `CREATE TABLE` as
every other MySQL index is. Applying `ddl/email/mysql.sql` twice fails with
error 1061. On MySQL, an existing host adds that index with the `ALTER TABLE`
below rather than by re-applying the document.
```

- [ ] **Step 3: Add the per-dialect migration note**

Add a section before "Rolling back":

```markdown
## Adding the email claim index to an existing host

A host that already emails needs `ntfy_notifications_email_idx` before
upgrading; `VerifyEmailSchema` fails at startup until it is there, which is the
intended signal. A host that does not email needs nothing.

| Dialect | How |
| --- | --- |
| PostgreSQL | re-apply `ddl/email/postgres.sql`, or run its `CREATE INDEX IF NOT EXISTS` statement |
| SQLite | re-apply `ddl/email/sqlite.sql`, or run its `CREATE INDEX IF NOT EXISTS` statement |
| MySQL | run the `ALTER TABLE` below — **re-applying the document is not the upgrade path** |

```sql
ALTER TABLE `ntfy_notifications`
    ADD KEY `ntfy_notifications_email_idx` (`state`, `created_at`, `id`);
```

With a table prefix configured, both names carry it.
```

- [ ] **Step 4: Extend the rollback section to the email schema**

Replace `docs/schema.md` — Rolling back with:

```markdown
## Rolling back

The schema is additive. To remove the notification store, stop running the
pruner, hub and handlers, then drop `ntfy_notifications` and `ntfy_watermarks`.
Nothing else depends on them.

To remove **email delivery** while keeping notifications, stop the dispatcher,
drop `ntfy_email_deliveries`, and drop `ntfy_notifications_email_idx` — the
index lives on the notifications table, so dropping the delivery table leaves it
behind:

```sql
DROP INDEX ntfy_notifications_email_idx;                        -- PostgreSQL, SQLite
ALTER TABLE `ntfy_notifications` DROP KEY `ntfy_notifications_email_idx`;  -- MySQL
```

Dropping `ntfy_notifications` removes the index with it, so a full rollback
needs no separate step.
```

- [ ] **Step 5: Verify the MySQL `ALTER TABLE` runs against the old schema**

Add to `sqlstore/email_verify_test.go`:

```go
func TestMySQLMigrationNoteAddsTheClaimIndex(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "mysql", sqlkittest.RunTestMySQL(t))
	executor := stdsqlExecutor(t, db, sqlkit.MySQL)
	store := harness.NewStore(t, executor)
	prefix := prefixOf(store)

	// The email table as it stands on a host that has not upgraded: the
	// deliveries table exists, the claim index does not.
	exec(t, executor, strings.ReplaceAll(store.EmailSchema(),
		"CREATE INDEX `"+prefix+"ntfy_notifications_email_idx` ON `"+prefix+"ntfy_notifications` (`state`, `created_at`, `id`)", ""))

	require.ErrorIs(t, store.VerifyEmailSchema(t.Context()), sqlkit.ErrSchemaMismatch)

	exec(t, executor, "ALTER TABLE `"+prefix+"ntfy_notifications` ADD KEY `"+prefix+
		"ntfy_notifications_email_idx` (`state`, `created_at`, `id`)")

	assert.NoError(t, store.VerifyEmailSchema(t.Context()))
}
```

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMySQLMigrationNoteAddsTheClaimIndex' -count=1 -timeout 30m ./...` from `sqlstore/`
Expected: PASS — the statement the documentation gives a host is the statement that satisfies verification. If the `ReplaceAll` matches nothing, print `store.EmailSchema()` and align the string with what the document renders.

- [ ] **Step 6: Re-read the documentation as a host would**

Read `docs/schema.md` end to end. A host upgrading on MySQL must be able to find the `ALTER TABLE` without opening the DDL, and a host rolling email back must find the index drop. Fix anything that only makes sense to someone who has read this plan.

- [ ] **Step 7: Commit**

```bash
git add docs/schema.md sqlstore/email_verify_test.go
git commit -m "Document the email claim index and its migration"
```

---

### Task 11: Full verification

**Files:** none modified; this task proves the whole change.

- [ ] **Step 1: Run the full build, lint and tests**

Run: `make all`
Expected: `0 issues.` from lint on every module, split-check clean, tests passing.

- [ ] **Step 2: Confirm the frozen copy is unchanged**

Run: `make sqlkit-copy-check`
Expected: PASS.

- [ ] **Step 3: Run the seven-combination store matrix**

Run: `make store-matrix`
Expected: all seven combinations pass, GORM entry points included. The GORM executor reports `?` as its bind marker on every database, so this is where a compound statement that only works with numbered placeholders would surface.

- [ ] **Step 4: Confirm the measurement stays opt-in**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMeasure' -count=1 -v ./...` from `sqlstore/`
Expected: every measurement case SKIPs, naming `NTFY_MEASURE_ROWS`. `make test` must not seed a million rows.

- [ ] **Step 5: Validate the change**

Run: `openspec validate "scale-email-claim-query" --strict`
Expected: `Change 'scale-email-claim-query' is valid`.

- [ ] **Step 6: Commit anything outstanding**

```bash
git status --porcelain
git commit -am "Finish scaling the email claim query"
```

---

## Self-Review

**1. Spec coverage.** This change has no spec delta (`skip_specs: true`), so coverage is measured against `design.md`'s decisions and `tasks.md`:

| Source | Covered by |
| --- | --- |
| D1 three `UNION ALL` branches, per-branch limit, `recorded` literal | Tasks 3, 4 |
| D2 index name, columns, email documents, email expectation only | Tasks 6, 8 |
| D2 MySQL non-idempotence, `MigrateEmail` guard, stated in three places | Tasks 6, 7, 10 |
| D3 thresholds 1–4 | Tasks 1, 9 |
| D4 residual accepted, D5 ordering stays global | No task, by design; the `UNION ALL` shape leaves both open |
| `tasks.md` 1.1–1.4 baseline and STOP | Task 1 |
| `tasks.md` 2.1–2.4 index, MySQL, expectation | Tasks 6, 7, 8 |
| `tasks.md` 3.1–3.4 rewrite, reader, branch semantics, conformance | Tasks 2, 3, 4, 5 |
| `tasks.md` 4.1–4.5 re-measure, write-side cost, evidence | Task 9 |
| `tasks.md` 5.1–5.4 docs, `make all`, `make store-matrix` | Tasks 10, 11 |

Two gaps found and fixed while reviewing: `tasks.md` never mentions that adding a notification-table index to the email document **breaks the existing golden test's "creates no notification table" assertion** and the six golden files (now Task 6, steps 5–7), and never mentions that `dropEmailIndex` assumes the deliveries table (now Task 8, step 1).

**2. Placeholder scan.** No "TBD", no "add error handling", no "similar to Task N". Every code step carries the code, every command carries its expected output, and the PostgreSQL claim SQL is written out in full in Task 4 rather than described.

**3. Type consistency.** Checked across tasks: `dueEmailsStatement(claim ntfy.EmailClaim, now time.Time) sqlkit.Statement` is produced in Task 3 and consumed unchanged in Tasks 4, 9; `writeDueRetry`/`writeInDoubt`/`writeLapsed` share one signature `(w *sqlkit.Writer, column func(string) string, now any)`; `emailNotificationsIndex` is defined in Task 6 and used in Tasks 7, 8, 9; `seed`/`seedShape`/`explain`/`medianOf`/`timeClaim`/`measureStore`/`measureRows` are defined in Task 1 and used in Task 9; `dropEmailIndex` gains its `table` parameter in Task 8 with both existing call sites updated in the same step.

One inconsistency found and fixed: Task 1's `timeClaim` deletes by `owner`, so the measurement claim must carry a fixed `Owner` — `measureClaim` sets `"measure"`, and the deletion binds `claim.Owner` rather than a literal.
