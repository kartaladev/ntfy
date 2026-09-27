package sqlstore

// The claim measurement. It is opt-in: seeding a million rows per dialect takes
// minutes, needs Docker for PostgreSQL and MySQL, and the race detector distorts
// its timings, so `make test` never runs it. Run it with, for example:
//
//	NTFY_MEASURE_ROWS=1000000 go test -run TestMeasureClaim -timeout 90m -v .
//
// It asserts the thresholds of
// openspec/changes/archive/2026-09-27-scale-email-claim-query/design.md — D3
// that hold on any machine: no full scan of the notifications table, and an
// empty pass that costs the same after rows are added outside the claim window.
// The absolute timings are logged for the change's measurements.md.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/sqlkit"
	"github.com/kartaladev/sqlkit/sqlkittest"
	stdsqlexec "github.com/kartaladev/sqlkit/stdsql"
)

// measureRowsEnv opts a run into the measurement and says how many
// notifications to seed before the table grows.
const measureRowsEnv = "NTFY_MEASURE_ROWS"

// The thresholds, stated before anything was measured. An empty pass after the
// table doubles outside the window may cost at most growthRatio times what it
// cost before, plus growthSlack for timer noise on a pass that is already fast.
const (
	growthRatio = 1.5
	growthSlack = 5 * time.Millisecond
	samples     = 7
)

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
	require.GreaterOrEqualf(t, rows, 1000, "%s must be at least 1000", measureRowsEnv)

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

// seedShape is the database a measurement runs against.
type seedShape struct {
	// InWindow is how many ACTIVE notifications fall inside the claim window.
	InWindow int
	// Recorded is how many rows already carry a terminal delivery record, so
	// that a pass considers and discards them rather than claiming them. It
	// counts the in-window rows first, then the outside ones.
	Recorded int
	// Outside is how many ACTIVE notifications are older than the window, which
	// no pass may ever claim.
	Outside int
	// Now is the instant the window is measured from.
	Now time.Time
	// Grace and MaxLag bound the window, as the dispatcher's defaults do.
	Grace  time.Duration
	MaxLag time.Duration
}

// rowsPerInsert keeps a seeding statement inside the dialect's bind limit.
// SQLite's is the lowest of the three.
func rowsPerInsert(dialect sqlkit.Dialect) int {
	if dialect.Name() == sqlkit.SQLite.Name() {
		return 60
	}

	return 400
}

// seed fills the store's tables to a shape, in one transaction, and returns the
// in-window notifications it left without a delivery record. It writes rows
// directly rather than through Insert: a million notifications through the
// publish path would take hours, and a measurement needs rows, not publishing
// semantics.
func seed(t *testing.T, store *Store, shape seedShape) []string {
	t.Helper()

	ids := ntfy.NewUUIDv7Generator()
	window := shape.Now.Add(-shape.MaxLag)
	span := shape.MaxLag - shape.Grace
	size := rowsPerInsert(store.dialect)
	start := time.Now()

	var unrecorded []string

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

			created := shape.Now.Add(-shape.MaxLag - time.Hour - time.Duration(i)*time.Millisecond)
			if i < shape.InWindow {
				// Spread across the window, clear of both of its edges.
				created = window.Add(time.Minute + time.Duration(int64(span-2*time.Minute)*int64(i)/int64(shape.InWindow)))
			}

			batch = append(batch, ntfy.Notification{
				ID: id, Recipient: fmt.Sprintf("user-%d", i%1000), SourceID: "seed-" + id,
				Subject: fmt.Sprintf("subject-%d", i%1000), Kind: "seed", State: ntfy.StateActive,
				CreatedAt: sqlkit.NormalizeTime(created),
			})

			switch {
			case i < shape.Recorded:
				recorded = append(recorded, id)
			case i < shape.InWindow:
				unrecorded = append(unrecorded, id)
			}

			if len(batch) == size {
				if err := flush(); err != nil {
					return err
				}
			}
		}

		return flush()
	}))

	t.Logf("seeded %d in-window (%d recorded) and %d outside rows in %s",
		shape.InWindow, shape.Recorded, shape.Outside, time.Since(start).Round(time.Second))

	return unrecorded
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

// seedDeliveries writes terminal SENT records, which no branch of the claim may
// return.
func seedDeliveries(ctx context.Context, s *Store, ids []string, at time.Time) error {
	for _, chunk := range chunks(ids, rowsPerInsert(s.dialect)) {
		instant := sqlkit.EncodeTime(s.dialect, &at)
		w := sqlkit.NewWriter(s.dialect)
		w.Write("INSERT INTO ", s.emailTable(), " (",
			s.columnList("notification_id", "recipient", "status", "attempts", "sent_at", "updated_at"), ") VALUES ")

		for i, id := range chunk {
			if i > 0 {
				w.Write(", ")
			}

			w.Write("(", w.BindAll(id, "seed", string(ntfy.EmailStatusSent), 1, instant, instant), ")")
		}

		if _, err := s.executor.Exec(ctx, w.Done()); err != nil {
			return err
		}
	}

	return nil
}

// countBy counts a table's rows grouped by one column.
func countBy(t *testing.T, store *Store, table, column string) map[string]int {
	t.Helper()

	statement := sqlkit.Statement{SQL: "SELECT " + store.quote(column) + ", COUNT(*) FROM " + table +
		" GROUP BY " + store.quote(column)}

	counts := map[string]int{}

	require.NoError(t, store.executor.Query(t.Context(), statement, func(rows sqlkit.Rows) error {
		for rows.Next() {
			var key, count any
			if err := rows.Scan(&key, &count); err != nil {
				return err
			}

			var dec decoder

			name, n := dec.text(key), dec.integer(count)
			if dec.err != nil {
				return dec.err
			}

			counts[name] = int(n)
		}

		return rows.Err()
	}))

	return counts
}

// requireShape checks a seeded database holds what its shape promised: every
// notification ACTIVE, and exactly the recorded ones carrying a SENT record.
func requireShape(t *testing.T, store *Store, shape seedShape) {
	t.Helper()

	require.Equal(t, map[string]int{string(ntfy.StateActive): shape.InWindow + shape.Outside},
		countBy(t, store, store.notificationsTable(), "state"))
	require.Equal(t, map[string]int{string(ntfy.EmailStatusSent): shape.Recorded},
		countBy(t, store, store.emailTable(), "status"))
}

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

// notificationsAliases are the names the claim gives the notifications table.
var notificationsAliases = []string{"n", "ntfy_notifications"}

var (
	postgresSeqScan = regexp.MustCompile(`Seq Scan on (\S+)(?: (\S+))?`)
	sqliteScan      = regexp.MustCompile(`^\s*SCAN (\S+)(.*)$`)
)

// fullScans lists the full scans of the notifications table a plan shows.
func fullScans(t *testing.T, dialect sqlkit.Dialect, plan string) []string {
	t.Helper()

	notifications := func(name string) bool {
		return slices.Contains(notificationsAliases, strings.Trim(name, "\"`"))
	}

	var found []string

	switch dialect.Name() {
	case sqlkit.PostgreSQL.Name():
		for _, match := range postgresSeqScan.FindAllStringSubmatch(plan, -1) {
			if notifications(match[1]) || notifications(match[2]) {
				found = append(found, match[0])
			}
		}
	case sqlkit.MySQL.Name():
		var tree any
		require.NoError(t, json.Unmarshal([]byte(plan), &tree), "MySQL plans are JSON")

		var walk func(node any)

		walk = func(node any) {
			switch value := node.(type) {
			case map[string]any:
				name, _ := value["table_name"].(string)
				if access, _ := value["access_type"].(string); access == "ALL" && notifications(name) {
					found = append(found, "table "+name+": access_type ALL")
				}

				for _, child := range value {
					walk(child)
				}
			case []any:
				for _, child := range value {
					walk(child)
				}
			}
		}

		walk(tree)
	default:
		for line := range strings.SplitSeq(plan, "\n") {
			match := sqliteScan.FindStringSubmatch(line)
			if len(match) == 3 && notifications(match[1]) && !strings.Contains(match[2], "USING") {
				found = append(found, strings.TrimSpace(line))
			}
		}
	}

	return found
}

// medianOf returns the median of samples, which must not be empty.
func medianOf(samples []time.Duration) time.Duration {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)

	return sorted[len(sorted)/2]
}

// timeClaim runs one claim and returns how long it took, deleting whatever it
// claimed so that samples stay independent.
func timeClaim(t *testing.T, store *Store, claim ntfy.EmailClaim) (took time.Duration, count int) {
	t.Helper()

	start := time.Now()
	claimed, err := store.ClaimEmails(t.Context(), claim)
	took = time.Since(start)

	require.NoError(t, err)

	if len(claimed) > 0 {
		w := sqlkit.NewWriter(store.dialect)
		w.Write("DELETE FROM ", store.emailTable(), " WHERE ", store.quote("owner"), " = ", w.Bind(claim.Owner))

		_, err := store.executor.Exec(t.Context(), w.Done())
		require.NoError(t, err)
	}

	return took, len(claimed)
}

// timeClaims runs a claim samples times and returns the median, requiring each
// run to claim want candidates.
func timeClaims(t *testing.T, store *Store, claim ntfy.EmailClaim, want int) time.Duration {
	t.Helper()

	var took []time.Duration

	for range samples {
		d, claimed := timeClaim(t, store, claim)
		require.Equal(t, want, claimed)

		took = append(took, d)
	}

	return medianOf(took)
}

// benchClaim runs a claim that claims nothing as a Go benchmark and logs the
// standard result line, so that runs can be compared with benchstat. It lives
// inside the test rather than in a Benchmark function because the databases
// come from sqlkittest's helpers, which take a *testing.T.
func benchClaim(t *testing.T, store *Store, claim ntfy.EmailClaim, label string) {
	t.Helper()

	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := store.ClaimEmails(t.Context(), claim); err != nil {
				b.Fatal(err)
			}
		}
	})

	t.Logf("BENCH %s\t%s\t%s", label, result.String(), result.MemString())
}

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
			shape := seedShape{
				InWindow: rows / 20, Recorded: rows / 20 * 9 / 10, Outside: rows - rows/20,
				Now: now, Grace: ntfy.DefaultEmailGraceDelay, MaxLag: ntfy.DefaultEmailMaxLag,
			}

			unrecorded := seed(t, store, shape)
			requireShape(t, store, shape)
			require.Len(t, unrecorded, shape.InWindow-shape.Recorded)

			claim := measureClaim(now)

			plan := explain(t, store, store.dueEmailsStatement(claim, now))
			t.Logf("plan with work available:\n%s", plan)

			claimable := min(claim.Limit, len(unrecorded))
			working := timeClaims(t, store, claim, claimable)
			t.Logf("MEASURE %s working pass (%d claimed) median: %s", tc.name, claimable, working)

			require.NoError(t, seedDeliveries(t.Context(), store, unrecorded, now))

			empty := timeClaims(t, store, claim, 0)
			t.Logf("MEASURE %s empty pass median: %s", tc.name, empty)
			benchClaim(t, store, claim, tc.name+" empty pass")

			idle := explain(t, store, store.dueEmailsStatement(claim, now))
			t.Logf("plan with nothing claimable:\n%s", idle)

			// The rows added outside the window carry SENT records, as a mature
			// host's do until retention removes them: history grows both tables.
			seed(t, store, seedShape{
				Outside: rows, Recorded: rows, Now: now.Add(-time.Hour), Grace: shape.Grace, MaxLag: shape.MaxLag,
			})

			grown := timeClaims(t, store, claim, 0)
			benchClaim(t, store, claim, tc.name+" empty pass, table grown")
			t.Logf("MEASURE %s empty pass median after %d more recorded rows outside the window: %s", tc.name, rows, grown)

			// Threshold 1: no branch scans the notifications table in full.
			assert.Empty(t, fullScans(t, tc.dialect, plan), "a full scan of the notifications table with work available")
			assert.Empty(t, fullScans(t, tc.dialect, idle), "a full scan of the notifications table on an idle pass")

			// Threshold 2: the empty pass costs what the window holds, not what the
			// table holds.
			limit := time.Duration(float64(empty)*growthRatio) + growthSlack
			assert.LessOrEqualf(t, grown, limit,
				"an empty pass went from %s to %s when %d rows were added outside the window", empty, grown, rows)
		})
	}
}
