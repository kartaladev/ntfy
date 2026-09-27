package sqlstore

// The SQL the claim path writes, asserted per dialect with no database, and the
// recorded flag the claim reads back, checked on SQLite.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/sqlkit"
	"github.com/kartaladev/sqlkit/sqlkittest"
	stdsqlexec "github.com/kartaladev/sqlkit/stdsql"
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

// TestWriteLapsedIsUnchanged pins the predicate takeOverDeliveries writes, so
// that splitting it for the claim's branches cannot move it.
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
				assert.Equal(t,
					"((`status` IN (?, ?) AND (`lease_until` IS NULL OR `lease_until` <= ?)"+
						" AND (`next_attempt_at` IS NULL OR `next_attempt_at` <= ?))"+
						" OR (`status` = ? AND (`lease_until` IS NULL OR `lease_until` <= ?)))",
					sql)
			},
		},
		{
			name:    "SQLite",
			dialect: sqlkit.SQLite,
			assert: func(t *testing.T, sql string) {
				assert.Equal(t,
					`(("status" IN (?, ?) AND ("lease_until" IS NULL OR "lease_until" <= ?)`+
						` AND ("next_attempt_at" IS NULL OR "next_attempt_at" <= ?))`+
						` OR ("status" = ? AND ("lease_until" IS NULL OR "lease_until" <= ?)))`,
					sql)
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

// TestDueEmailsMarksWhatIsRecorded pins the flag ClaimEmails uses to choose
// between inserting a delivery record and taking one over.
func TestDueEmailsMarksWhatIsRecorded(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", sqlkittest.RunTestSQLite(t))
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	executor, err := stdsqlexec.New(db, sqlkit.SQLite)
	require.NoError(t, err)

	store, err := New(executor)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context()))
	require.NoError(t, store.MigrateEmail(t.Context()))

	now := sqlkit.NormalizeTime(time.Now().UTC())
	claim := ntfy.EmailClaim{
		Now: now, Owner: "first", Lease: time.Minute,
		CreatedUntil: now.Add(-time.Minute), CreatedFrom: now.Add(-time.Hour), Limit: 10,
	}

	var unseen []string

	for i := range 2 {
		id := fmt.Sprintf("n-%d", i)
		_, err := store.Insert(t.Context(), id, []ntfy.Insertion{{Notification: ntfy.Notification{
			ID: id, Recipient: "alice", SourceID: "src-" + id, Subject: id, Kind: "offer",
			State: ntfy.StateActive, CreatedAt: now.Add(-30*time.Minute + time.Duration(i)*time.Minute),
		}}})
		require.NoError(t, err)

		unseen = append(unseen, id)
	}

	// The first notification is claimed and its lease left to lapse, so it has a
	// record; the second never was.
	_, err = store.ClaimEmails(t.Context(), ntfy.EmailClaim{
		Now: now, Owner: "first", Lease: time.Minute,
		CreatedUntil: now.Add(-time.Minute), CreatedFrom: now.Add(-time.Hour), Limit: 1,
	})
	require.NoError(t, err)

	due, err := store.dueEmails(t.Context(), claim, now.Add(2*time.Minute))
	require.NoError(t, err)

	assert.Equal(t, []dueEmail{{id: unseen[0], recorded: true}, {id: unseen[1], recorded: false}}, due)
}

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

// TestClaimStatementShape pins the shape that lets an index serve each branch:
// one outer join, tested for NULL, as the unrecorded branch's anti-join; no
// disjunction; and a limit per branch as well as on the union.
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
		assert.Equal(t, 1, strings.Count(sql, " LEFT JOIN "), "only branch 1 joins outward, as its anti-join")
		assert.Equal(t, 1, strings.Count(sql, `."notification_id" IS NULL`)+strings.Count(sql, ".`notification_id` IS NULL"),
			"and tests the join for NULL")
		assert.NotContains(t, sql, "NOT EXISTS", "no anti-join MySQL would materialise")
		assert.NotContains(t, sql, " OR (", "no branch filters on a disjunction of branches")
		assert.True(t, strings.HasSuffix(sql, " LIMIT 500"), "the union is limited last")
	}

	cases := []testCase{
		{name: "PostgreSQL", dialect: sqlkit.PostgreSQL, assert: shape},
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
