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
		{name: "NFD makes another recipient than NFC", assert: recipientSeesNothing("jos\u00e9", "jose\u0301")},
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
