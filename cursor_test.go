package ntfy_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

func TestCursor(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 14, 8, 30, 0, 123456000, time.UTC)
	position := ntfy.CursorPosition{CreatedAt: at, ID: "0199-id"}

	base := ntfy.ListQuery{
		Recipient: "alice",
		States:    []ntfy.State{ntfy.StateActive, ntfy.StateRead},
		Kinds:     []string{"offer", "taken"},
		Subject:   "task-1",
	}

	type testCase struct {
		name   string
		decode func(cursor string) ntfy.ListQuery
		assert func(t *testing.T, got ntfy.CursorPosition, ok bool, err error)
	}

	cases := []testCase{
		{
			name: "a cursor round-trips under the query that produced it",
			decode: func(cursor string) ntfy.ListQuery {
				q := base
				q.Cursor = cursor

				return q
			},
			assert: func(t *testing.T, got ntfy.CursorPosition, ok bool, err error) {
				require.NoError(t, err)
				assert.True(t, ok)
				assert.True(t, at.Equal(got.CreatedAt), "created at %s, got %s", at, got.CreatedAt)
				assert.Equal(t, "0199-id", got.ID)
			},
		},
		{
			name: "filters listed in another order are the same query",
			decode: func(cursor string) ntfy.ListQuery {
				q := base
				q.States = []ntfy.State{ntfy.StateRead, ntfy.StateActive}
				q.Kinds = []string{"taken", "offer"}
				q.Cursor = cursor

				return q
			},
			assert: func(t *testing.T, got ntfy.CursorPosition, ok bool, err error) {
				require.NoError(t, err)
				assert.True(t, ok)
				assert.Equal(t, "0199-id", got.ID)
			},
		},
		{
			name: "no cursor means the first page",
			decode: func(string) ntfy.ListQuery {
				return base
			},
			assert: func(t *testing.T, _ ntfy.CursorPosition, ok bool, err error) {
				require.NoError(t, err)
				assert.False(t, ok)
			},
		},
		{
			name: "a cursor under another recipient is refused",
			decode: func(cursor string) ntfy.ListQuery {
				q := base
				q.Recipient = "bob"
				q.Cursor = cursor

				return q
			},
			assert: func(t *testing.T, _ ntfy.CursorPosition, _ bool, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
		{
			name: "a cursor under other states is refused",
			decode: func(cursor string) ntfy.ListQuery {
				q := base
				q.States = []ntfy.State{ntfy.StateActive}
				q.Cursor = cursor

				return q
			},
			assert: func(t *testing.T, _ ntfy.CursorPosition, _ bool, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
		{
			name: "a cursor under another subject is refused",
			decode: func(cursor string) ntfy.ListQuery {
				q := base
				q.Subject = "task-2"
				q.Cursor = cursor

				return q
			},
			assert: func(t *testing.T, _ ntfy.CursorPosition, _ bool, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
		{
			name: "a cursor that is not one of ours is refused",
			decode: func(string) ntfy.ListQuery {
				q := base
				q.Cursor = "not-a-cursor"

				return q
			},
			assert: func(t *testing.T, _ ntfy.CursorPosition, _ bool, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cursor := ntfy.EncodeCursor(base, position)
			got, ok, err := ntfy.DecodeCursor(tc.decode(cursor))
			tc.assert(t, got, ok, err)
		})
	}
}

func TestListQueryValidate(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		query  ntfy.ListQuery
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name:  "a recipient alone is a valid query",
			query: ntfy.ListQuery{Recipient: "alice"},
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:  "a recipient is required",
			query: ntfy.ListQuery{},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
		{
			name:  "the page size cap is allowed",
			query: ntfy.ListQuery{Recipient: "alice", Limit: ntfy.MaxListLimit},
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:  "a page size above the cap is refused",
			query: ntfy.ListQuery{Recipient: "alice", Limit: ntfy.MaxListLimit + 1},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
		{
			name:  "a negative page size is refused",
			query: ntfy.ListQuery{Recipient: "alice", Limit: -1},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
		{
			name:  "an unknown state is refused",
			query: ntfy.ListQuery{Recipient: "alice", States: []ntfy.State{"UNREAD"}},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
		{
			name:  "a malformed cursor is refused",
			query: ntfy.ListQuery{Recipient: "alice", Cursor: "%%%"},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, tc.query.Validate())
		})
	}
}

func TestListQueryEffectiveLimit(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ntfy.DefaultListLimit, ntfy.ListQuery{}.EffectiveLimit())
	assert.Equal(t, 7, ntfy.ListQuery{Limit: 7}.EffectiveLimit())
}
