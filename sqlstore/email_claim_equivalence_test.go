package sqlstore_test

// The rows the claim returns, over a fixture covering every branch of the
// claim and its boundaries, on every dialect. It pins today's behaviour, so it
// must pass unedited before and after the claim query is restructured.

import (
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

// claimRow is one notification of the fixture and the delivery record, if
// any, that puts it in a branch of the claim.
type claimRow struct {
	name string
	// orphan writes only the delivery record, as retention leaves one behind
	// when it deletes the notification.
	orphan  bool
	created time.Time
	state   ntfy.State
	status  ntfy.EmailStatus
	lease   *time.Time
	next    *time.Time
	claimed bool
}

// seedClaimRow writes one notification and, for a non-empty status, its
// delivery record, and returns the notification's identifier.
func seedClaimRow(t *testing.T, store *sqlstore.Store, executor sqlkit.Executor, ids ntfy.IDGenerator, r claimRow) string {
	t.Helper()

	id, err := ids.NewID()
	require.NoError(t, err)

	if r.orphan {
		insertDelivery(t, store, executor, id, r)

		return id
	}

	_, err = store.Insert(t.Context(), r.name, []ntfy.Insertion{{Notification: ntfy.Notification{
		ID: id, Recipient: "alice", SourceID: "src-" + id, Subject: r.name,
		Kind: "offer", State: ntfy.StateActive, CreatedAt: sqlkit.NormalizeTime(r.created),
	}}})
	require.NoError(t, err)

	if r.state != ntfy.StateActive {
		_, err = store.Close(t.Context(), ntfy.CloseRequest{Subject: r.name, Version: 1, Reason: "fixture"},
			sqlkit.NormalizeTime(r.created.Add(time.Minute)), ids)
		require.NoError(t, err)
	}

	if r.status != "" {
		insertDelivery(t, store, executor, id, r)
	}

	return id
}

// insertDelivery writes the delivery record of a fixture row.
func insertDelivery(t *testing.T, store *sqlstore.Store, executor sqlkit.Executor, id string, r claimRow) {
	t.Helper()

	dialect := executor.Dialect()
	table := dialect.Quote(strings.TrimSuffix(store.Tables()[0], sqlstore.NotificationsTable) + sqlstore.EmailDeliveriesTable)
	updated := sqlkit.NormalizeTime(r.created)

	w := sqlkit.NewWriter(dialect)
	w.Write("INSERT INTO ", table, " (",
		dialect.Quote("notification_id"), ", ", dialect.Quote("recipient"), ", ", dialect.Quote("status"), ", ",
		dialect.Quote("owner"), ", ", dialect.Quote("lease_until"), ", ", dialect.Quote("attempts"), ", ",
		dialect.Quote("next_attempt_at"), ", ", dialect.Quote("updated_at"), ") VALUES (",
		w.BindAll(id, "alice", string(r.status), "fixture",
			sqlkit.EncodeTime(dialect, r.lease), 1, sqlkit.EncodeTime(dialect, r.next),
			sqlkit.EncodeTime(dialect, &updated)), ")")

	_, err := executor.Exec(t.Context(), w.Done())
	require.NoError(t, err)
}

// claimIDs lists the identifiers of claimed candidates, in the order returned.
func claimIDs(candidates []ntfy.EmailCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.Notification.ID)
	}

	return out
}

func TestClaimReturnsTheSameRows(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		executor func(t *testing.T) sqlkit.Executor
		assert   func(t *testing.T, want, first, rest []string)
	}

	same := func(t *testing.T, want, first, rest []string) {
		t.Helper()

		// Branch 1 holds more rows than the first claim's limit, so a branch that
		// is not itself ordered oldest first would show.
		require.Len(t, want, 8, "the fixture claims one row per claimable branch and boundary")
		assert.Equal(t, want[:3], first, "a limited claim takes the oldest across every branch")
		assert.Equal(t, want[3:], rest, "and the next claim takes the rest, oldest first")
	}

	cases := []testCase{
		{
			name: "PostgreSQL",
			executor: func(t *testing.T) sqlkit.Executor {
				return stdsqlExecutor(t, openSQL(t, "postgres", sqlkittest.RunTestPostgres(t)), sqlkit.PostgreSQL)
			},
			assert: same,
		},
		{
			name: "MySQL",
			executor: func(t *testing.T) sqlkit.Executor {
				return stdsqlExecutor(t, openSQL(t, "mysql", sqlkittest.RunTestMySQL(t)), sqlkit.MySQL)
			},
			assert: same,
		},
		{
			name: "SQLite",
			executor: func(t *testing.T) sqlkit.Executor {
				return stdsqlExecutor(t, openSQL(t, "sqlite", sqlkittest.RunTestSQLite(t)), sqlkit.SQLite)
			},
			assert: same,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			executor := tc.executor(t)
			store := harness.NewEmailStore(t, executor)
			ids := ntfy.NewUUIDv7Generator()

			now := sqlkit.NormalizeTime(time.Now().UTC())
			grace, lag, lease := ntfy.DefaultEmailGraceDelay, ntfy.DefaultEmailMaxLag, ntfy.DefaultEmailLease

			// at is an instant inside the claim window; each row gets its own, so
			// the claim's oldest-first order is observable.
			at := func(minutesAgo int) time.Time { return now.Add(-time.Hour - time.Duration(minutesAgo)*time.Minute) }

			lapsed, live := now.Add(-time.Minute), now.Add(time.Hour)
			due, notDue := now.Add(-time.Minute), now.Add(time.Hour)

			// Oldest first. Branch 1 is a notification with no delivery record;
			// branch 2 a lapsed SENDING record whatever the notification's state;
			// branch 3 a due, lapsed CLAIMED or RETRY record of a notification that
			// still qualifies.
			rows := []claimRow{
				{name: "older than the lag", created: now.Add(-lag - time.Hour), state: ntfy.StateActive},
				{name: "unrecorded, oldest", created: at(14), state: ntfy.StateActive, claimed: true},
				{name: "unrecorded, older", created: at(13), state: ntfy.StateActive, claimed: true},
				{name: "in doubt, closed", created: at(12), state: ntfy.StateClosed, status: ntfy.EmailStatusSending, lease: &lapsed, claimed: true},
				{name: "unrecorded", created: at(11), state: ntfy.StateActive, claimed: true},
				{name: "retry due, closed", created: at(10), state: ntfy.StateClosed, status: ntfy.EmailStatusRetry, lease: &lapsed, next: &due},
				{name: "retry due", created: at(9), state: ntfy.StateActive, status: ntfy.EmailStatusRetry, lease: &lapsed, next: &due, claimed: true},
				{name: "in doubt, lease live", created: at(8), state: ntfy.StateActive, status: ntfy.EmailStatusSending, lease: &live},
				{name: "in doubt", created: at(7), state: ntfy.StateActive, status: ntfy.EmailStatusSending, lease: &lapsed, claimed: true},
				{name: "retry not due", created: at(6), state: ntfy.StateActive, status: ntfy.EmailStatusRetry, lease: &lapsed, next: &notDue},
				{name: "claimed, lease lapsed", created: at(5), state: ntfy.StateActive, status: ntfy.EmailStatusClaimed, lease: &lapsed, claimed: true},
				{name: "claimed, lease live", created: at(4), state: ntfy.StateActive, status: ntfy.EmailStatusClaimed, lease: &live},
				{name: "sent", created: at(3), state: ntfy.StateActive, status: ntfy.EmailStatusSent},
				{name: "failed", created: at(2), state: ntfy.StateActive, status: ntfy.EmailStatusFailed},
				{name: "in doubt, notification deleted", created: at(1), orphan: true, status: ntfy.EmailStatusSending, lease: &lapsed},
				{name: "retry due, notification deleted", created: at(1), orphan: true, status: ntfy.EmailStatusRetry, lease: &lapsed, next: &due},
				{name: "unrecorded, newer", created: at(1), state: ntfy.StateActive, claimed: true},
				{name: "newer than the grace", created: now.Add(-grace / 2), state: ntfy.StateActive},
			}

			// Seeded newest first, so that identifiers, which sort in the order
			// they were minted, run against the creation order a claim follows.
			seeded := make([]string, len(rows))
			for i := len(rows) - 1; i >= 0; i-- {
				seeded[i] = seedClaimRow(t, store, executor, ids, rows[i])
			}

			var want []string

			for i, r := range rows {
				if r.claimed {
					want = append(want, seeded[i])
				}
			}

			claim := func(limit int) []string {
				claimed, err := store.ClaimEmails(t.Context(), ntfy.EmailClaim{
					Now: now, Owner: "equivalence", Lease: lease,
					CreatedUntil: now.Add(-grace), CreatedFrom: now.Add(-lag), Limit: limit,
				})
				require.NoError(t, err)

				return claimIDs(claimed)
			}

			first := claim(3)
			rest := claim(ntfy.DefaultEmailClaimLimit)

			tc.assert(t, want, first, rest)
		})
	}
}
