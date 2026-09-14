package ntfy_test

import (
	"context"
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
