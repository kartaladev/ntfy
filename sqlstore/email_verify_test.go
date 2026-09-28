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

// mysqlClaimIndexLine is the line of docs/schema.md that introduces the MySQL
// statement adding the email claim index to an existing host.
const mysqlClaimIndexLine = "- **MySQL:** run this, and do **not** re-apply the email document:"

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

	for _, statement := range documentedBlock(t, mysqlClaimIndexLine, prefixOf(store)) {
		exec(t, executor, statement)
	}

	assert.NoError(t, store.VerifyEmailSchema(t.Context()))
	assert.NoError(t, store.MigrateEmail(t.Context()), "and the development path stays re-runnable")
}

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
		prefix+`ntfy_notifications.recipient: type is "varchar" collated "utf8mb4_0900_as_cs" but must be "varbinary"`,
		"the old columns fail verification")
	assert.Contains(t, issues(t, store.VerifyEmailSchema(t.Context())),
		prefix+`ntfy_email_deliveries.owner: type is "varchar" collated "utf8mb4_0900_as_cs" but must be "varbinary"`)

	for _, check := range documentedBlock(t, "### Checking before upgrading", prefix) {
		rows, err := db.QueryContext(t.Context(), check)
		require.NoErrorf(t, err, "run the documented pre-check %q", check)

		assert.False(t, rows.Next(), "the pre-check finds no identifier the upgrade would refuse")
		require.NoError(t, rows.Err(), "the pre-check ran to the end, so finding nothing means nothing")
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
