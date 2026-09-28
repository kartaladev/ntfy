package ntfy_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// customBroadcaster is a broadcaster the host supplies, told apart from the
// default by its type.
type customBroadcaster struct{ ntfy.InProcessBroadcaster }

func TestNew(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 9, 14, 8, 30, 0, 0, time.UTC)
	custom := &customBroadcaster{}

	draft := ntfy.Draft{Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer"}

	type testCase struct {
		name   string
		store  ntfy.Store
		opts   []ntfy.Option
		assert func(t *testing.T, svc *ntfy.Service, err error)
	}

	cases := []testCase{
		{
			name:  "a nil store is a configuration error",
			store: nil,
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.ErrorIs(t, err, ntfy.ErrConfiguration)
				assert.Nil(t, svc)
			},
		},
		{
			name:  "with no options it broadcasts in process, reads the system clock and mints UUIDv7 identifiers",
			store: ntfy.NewMemoryStore(),
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.NoError(t, err)
				assert.IsType(t, &ntfy.InProcessBroadcaster{}, svc.Broadcaster())

				result, err := svc.Publish(t.Context(), draft)
				require.NoError(t, err)
				require.Len(t, result.Created, 1)

				created := result.Created[0]
				assert.WithinDuration(t, time.Now(), created.CreatedAt, time.Minute)
				require.Len(t, created.ID, 36)
				assert.Equal(t, byte('7'), created.ID[14])
			},
		},
		{
			name:  "options replace the clock, the identifiers and the broadcaster",
			store: ntfy.NewMemoryStore(),
			opts: []ntfy.Option{
				ntfy.WithClock(ntfy.ClockFunc(func() time.Time { return fixed })),
				ntfy.WithIDGenerator(ntfy.IDGeneratorFunc(func() (string, error) { return "host-id", nil })),
				ntfy.WithBroadcaster(custom),
			},
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.NoError(t, err)
				assert.Same(t, custom, svc.Broadcaster())

				result, err := svc.Publish(t.Context(), draft)
				require.NoError(t, err)
				require.Len(t, result.Created, 1)
				assert.Equal(t, "host-id", result.Created[0].ID)
				assert.True(t, fixed.Equal(result.Created[0].CreatedAt))
			},
		},
		{
			name:  "nil options leave the defaults in place",
			store: ntfy.NewMemoryStore(),
			opts: []ntfy.Option{
				nil, ntfy.WithClock(nil), ntfy.WithIDGenerator(nil), ntfy.WithBroadcaster(nil),
				ntfy.WithSignalErrorHandler(nil),
			},
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.NoError(t, err)
				assert.IsType(t, &ntfy.InProcessBroadcaster{}, svc.Broadcaster())

				_, err = svc.Publish(context.WithoutCancel(t.Context()), draft)
				assert.NoError(t, err)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, err := ntfy.New(tc.store, tc.opts...)
			tc.assert(t, svc, err)
		})
	}
}

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
			assert: func(t *testing.T, store ntfy.Store, err error) {
				require.ErrorIs(t, err, ntfy.ErrConfiguration)
				assert.ErrorContains(t, err, "ID generator")

				page, err := store.List(t.Context(), ntfy.ListQuery{Recipient: "alice"})
				require.NoError(t, err)
				require.Len(t, page.Notifications, 1, "no successor was written")
				assert.Equal(t, ntfy.StateActive, page.Notifications[0].State, "the refused close closed nothing")
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
