package sqlstore_test

import (
	"os"
	"path/filepath"
	"strings"
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

// emailIndex is an index VerifyEmailSchema requires and the table it is on,
// both before the table prefix.
type emailIndex struct{ table, name string }

// The indexes VerifyEmailSchema requires. One is on the notifications table:
// the email schema adds it, and only VerifyEmailSchema requires it.
var requiredEmailIndexes = []emailIndex{
	{table: sqlstore.EmailDeliveriesTable, name: "ntfy_email_deliveries_lease_idx"},
	{table: sqlstore.EmailDeliveriesTable, name: "ntfy_email_deliveries_retry_idx"},
	{table: sqlstore.NotificationsTable, name: "ntfy_notifications_email_idx"},
}

// dropEmailIndex drops a prefixed email index in the executor's dialect.
func dropEmailIndex(t *testing.T, executor sqlkit.Executor, store *sqlstore.Store, index emailIndex) {
	t.Helper()

	dialect := executor.Dialect()
	name := dialect.Quote(prefixOf(store) + index.name)

	if dialect.Name() == sqlkit.MySQL.Name() {
		exec(t, executor, "DROP INDEX "+name+" ON "+dialect.Quote(prefixOf(store)+index.table))

		return
	}

	exec(t, executor, "DROP INDEX "+name)
}

// runVerifyEmailSchema checks VerifyEmailSchema over one executor, breaking a
// freshly migrated email schema one object at a time.
func runVerifyEmailSchema(t *testing.T, executor sqlkit.Executor) {
	t.Helper()

	type testCase struct {
		name   string
		store  func(t *testing.T) *sqlstore.Store
		breaks func(t *testing.T, store *sqlstore.Store)
		verify func(t *testing.T, store *sqlstore.Store) error
		assert func(t *testing.T, store *sqlstore.Store, err error)
	}

	withEmail := func(t *testing.T) *sqlstore.Store { return harness.NewEmailStore(t, executor) }
	verifyEmail := func(t *testing.T, store *sqlstore.Store) error { return store.VerifyEmailSchema(t.Context()) }
	unbroken := func(*testing.T, *sqlstore.Store) {}

	cases := []testCase{
		{
			name:  "a freshly migrated email schema, migrated twice, verifies",
			store: withEmail,
			breaks: func(t *testing.T, store *sqlstore.Store) {
				require.NoError(t, store.MigrateEmail(t.Context()), "migrating an existing schema changes nothing")
			},
			verify: verifyEmail,
			assert: func(t *testing.T, _ *sqlstore.Store, err error) { assert.NoError(t, err) },
		},
		{
			name:   "the notification schema verifies without the email table",
			store:  func(t *testing.T) *sqlstore.Store { return harness.NewStore(t, executor) },
			breaks: unbroken,
			verify: func(t *testing.T, store *sqlstore.Store) error { return store.VerifySchema(t.Context()) },
			assert: func(t *testing.T, _ *sqlstore.Store, err error) { assert.NoError(t, err) },
		},
		{
			name:   "a missing email table is reported",
			store:  func(t *testing.T) *sqlstore.Store { return harness.NewStore(t, executor) },
			breaks: unbroken,
			verify: verifyEmail,
			assert: func(t *testing.T, store *sqlstore.Store, err error) {
				assert.Contains(t, issues(t, err), store.EmailTables()[0]+": table is missing")
			},
		},
		{
			name:  "a missing column is reported, alongside a missing index",
			store: withEmail,
			breaks: func(t *testing.T, store *sqlstore.Store) {
				dialect := executor.Dialect()
				exec(t, executor, "ALTER TABLE "+dialect.Quote(store.EmailTables()[0])+" DROP COLUMN "+dialect.Quote("reason"))
				dropEmailIndex(t, executor, store, requiredEmailIndexes[0])
			},
			verify: verifyEmail,
			assert: func(t *testing.T, store *sqlstore.Store, err error) {
				listed := issues(t, err)
				assert.Contains(t, listed, store.EmailTables()[0]+".reason: column is missing")
				assert.Contains(t, listed, "index "+prefixOf(store)+requiredEmailIndexes[0].name+" is missing")
			},
		},
	}

	for _, index := range requiredEmailIndexes {
		cases = append(cases, testCase{
			name:  "a missing " + index.name + " index is reported",
			store: withEmail,
			breaks: func(t *testing.T, store *sqlstore.Store) {
				dropEmailIndex(t, executor, store, index)
			},
			verify: verifyEmail,
			assert: func(t *testing.T, store *sqlstore.Store, err error) {
				assert.Contains(t, issues(t, err), "index "+prefixOf(store)+index.name+" is missing")
			},
		})
	}

	cases = append(cases, testCase{
		name:  "the notification schema verifies without the email index on its table",
		store: withEmail,
		breaks: func(t *testing.T, store *sqlstore.Store) {
			dropEmailIndex(t, executor, store, requiredEmailIndexes[2])
		},
		verify: func(t *testing.T, store *sqlstore.Store) error { return store.VerifySchema(t.Context()) },
		assert: func(t *testing.T, _ *sqlstore.Store, err error) { assert.NoError(t, err) },
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := tc.store(t)
			tc.breaks(t, store)

			tc.assert(t, store, tc.verify(t, store))
		})
	}
}

func TestVerifyEmailSchemaOnPostgres(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "postgres", sqlkittest.RunTestPostgres(t))

	runVerifyEmailSchema(t, stdsqlExecutor(t, db, sqlkit.PostgreSQL))
}

func TestVerifyEmailSchemaOnMySQL(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "mysql", sqlkittest.RunTestMySQL(t))

	runVerifyEmailSchema(t, stdsqlExecutor(t, db, sqlkit.MySQL))
}

func TestVerifyEmailSchemaOnSQLite(t *testing.T) {
	t.Parallel()

	db := openSQL(t, "sqlite", sqlkittest.RunTestSQLite(t))

	runVerifyEmailSchema(t, stdsqlExecutor(t, db, sqlkit.SQLite))
}

// documentedStatement reads the SQL statement docs/schema.md gives on the line
// starting with a keyword, with the documented app_ prefix replaced by the
// store's.
func documentedStatement(t *testing.T, keyword, prefix string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "docs", "schema.md"))
	require.NoError(t, err)

	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, keyword) {
			return strings.TrimSuffix(strings.ReplaceAll(line, "app_", prefix), ";")
		}
	}

	t.Fatalf("docs/schema.md gives no statement starting with %q", keyword)

	return ""
}

// TestTheDocumentedMySQLUpgradeAddsTheEmailClaimIndex applies the email schema
// as it stood before the claim index, then the upgrade docs/schema.md gives for
// MySQL, and requires the result to verify.
func TestTheDocumentedMySQLUpgradeAddsTheEmailClaimIndex(t *testing.T) {
	t.Parallel()

	executor := stdsqlExecutor(t, openSQL(t, "mysql", sqlkittest.RunTestMySQL(t)), sqlkit.MySQL)
	store := harness.NewEmailStore(t, executor)
	index := requiredEmailIndexes[2]

	dropEmailIndex(t, executor, store, index)
	require.Error(t, store.VerifyEmailSchema(t.Context()), "an email schema from before the index does not verify")

	exec(t, executor, documentedStatement(t, "ALTER TABLE", prefixOf(store)))

	assert.NoError(t, store.VerifyEmailSchema(t.Context()))
	assert.NoError(t, store.MigrateEmail(t.Context()), "and the development path stays re-runnable")
}

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
