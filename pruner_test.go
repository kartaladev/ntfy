package ntfy_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kartaladev/ntfy"
)

func TestNewPruner(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		nilSvc  bool
		opts    []ntfy.PruneOption
		request *ntfy.PruneRequest
		assert  func(t *testing.T, pruner *ntfy.Pruner, err error)
	}

	refused := func(t *testing.T, pruner *ntfy.Pruner, err error) {
		t.Helper()

		require.ErrorIs(t, err, ntfy.ErrConfiguration)
		assert.Nil(t, pruner)
	}

	built := func(t *testing.T, pruner *ntfy.Pruner, err error) {
		t.Helper()

		require.NoError(t, err)
		require.NotNil(t, pruner)

		_, err = pruner.Prune(t.Context())
		assert.NoError(t, err)
	}

	cases := []testCase{
		{name: "a nil service is a configuration error", nilSvc: true, assert: refused},
		{
			name: "with no options the documented defaults apply",
			request: &ntfy.PruneRequest{
				Now: serviceAt, MaxPerRecipient: 500, MaxAge: 90 * 24 * time.Hour, Strategy: ntfy.EvictOldestActive,
				WatermarkRetention: 7 * 24 * time.Hour, Batch: 1000,
			},
			assert: built,
		},
		{
			name: "options replace every default",
			opts: []ntfy.PruneOption{
				ntfy.WithMaxPerRecipient(100), ntfy.WithoutMaxAge(), ntfy.WithRetentionStrategy(ntfy.RetainActive),
				ntfy.WithWatermarkRetention(24 * time.Hour), ntfy.WithPruneBatch(50),
			},
			request: &ntfy.PruneRequest{
				Now: serviceAt, MaxPerRecipient: 100, Strategy: ntfy.RetainActive,
				WatermarkRetention: 24 * time.Hour, Batch: 50,
			},
			assert: built,
		},
		{
			name: "the count bound can be removed while the age bound stays",
			opts: []ntfy.PruneOption{ntfy.WithoutMaxPerRecipient(), ntfy.WithMaxAge(30 * 24 * time.Hour)},
			request: &ntfy.PruneRequest{
				Now: serviceAt, MaxAge: 30 * 24 * time.Hour, Strategy: ntfy.EvictOldestActive,
				WatermarkRetention: 7 * 24 * time.Hour, Batch: 1000,
			},
			assert: built,
		},
		{name: "a count bound of zero is refused", opts: []ntfy.PruneOption{ntfy.WithMaxPerRecipient(0)}, assert: refused},
		{name: "a negative count bound is refused", opts: []ntfy.PruneOption{ntfy.WithMaxPerRecipient(-1)}, assert: refused},
		{name: "an age bound of zero is refused", opts: []ntfy.PruneOption{ntfy.WithMaxAge(0)}, assert: refused},
		{name: "an unknown strategy is refused", opts: []ntfy.PruneOption{ntfy.WithRetentionStrategy("SOMETIMES")}, assert: refused},
		{
			name:   "a count bound both set and removed is refused",
			opts:   []ntfy.PruneOption{ntfy.WithMaxPerRecipient(5), ntfy.WithoutMaxPerRecipient()},
			assert: refused,
		},
		{
			name:   "an age bound both set and removed is refused",
			opts:   []ntfy.PruneOption{ntfy.WithMaxAge(time.Hour), ntfy.WithoutMaxAge()},
			assert: refused,
		},
		{
			name:   "a pruner with both bounds removed is refused",
			opts:   []ntfy.PruneOption{ntfy.WithoutMaxPerRecipient(), ntfy.WithoutMaxAge()},
			assert: refused,
		},
		{name: "a watermark retention of zero is refused", opts: []ntfy.PruneOption{ntfy.WithWatermarkRetention(0)}, assert: refused},
		{name: "a batch of zero is refused", opts: []ntfy.PruneOption{ntfy.WithPruneBatch(0)}, assert: refused},
		{name: "a nil option is ignored", opts: []ntfy.PruneOption{nil}, request: &ntfy.PruneRequest{
			Now: serviceAt, MaxPerRecipient: 500, MaxAge: 90 * 24 * time.Hour, Strategy: ntfy.EvictOldestActive,
			WatermarkRetention: 7 * 24 * time.Hour, Batch: 1000,
		}, assert: built},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, store, _, _ := mocked(t)
			if tc.request != nil {
				store.EXPECT().Prune(gomock.Any(), *tc.request).Return(ntfy.PruneResult{}, nil)
			}

			if tc.nilSvc {
				svc = nil
			}

			pruner, err := ntfy.NewPruner(svc, tc.opts...)
			tc.assert(t, pruner, err)
		})
	}
}

func TestPrunerPrune(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		expect func(store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster)
		assert func(t *testing.T, result ntfy.PruneResult, err error)
	}

	cases := []testCase{
		{
			name: "evicted active notifications signal their recipients",
			expect: func(store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				gomock.InOrder(
					store.EXPECT().Prune(gomock.Any(), gomock.Any()).Return(ntfy.PruneResult{
						DeletedForAge: 7, DeletedForCount: 3, EvictedActive: 2, Recipients: []string{"bob", "alice"},
					}, nil),
					broadcaster.EXPECT().Broadcast(gomock.Any(), []ntfy.Signal{
						{Recipient: "alice", Change: ntfy.ChangePruned, At: serviceAt},
						{Recipient: "bob", Change: ntfy.ChangePruned, At: serviceAt},
					}).Return(nil),
				)
			},
			assert: func(t *testing.T, result ntfy.PruneResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, int64(7), result.DeletedForAge)
				assert.Equal(t, int64(3), result.DeletedForCount)
				assert.Equal(t, int64(2), result.EvictedActive)
			},
		},
		{
			name: "a pass that evicted nothing active signals nobody",
			expect: func(store *ntfy.MockStore, _ *ntfy.MockBroadcaster) {
				store.EXPECT().Prune(gomock.Any(), gomock.Any()).Return(ntfy.PruneResult{DeletedForAge: 4}, nil)
			},
			assert: func(t *testing.T, result ntfy.PruneResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, int64(4), result.DeletedForAge)
			},
		},
		{
			name: "a store failure is returned",
			expect: func(store *ntfy.MockStore, _ *ntfy.MockBroadcaster) {
				store.EXPECT().Prune(gomock.Any(), gomock.Any()).Return(ntfy.PruneResult{}, errors.New("disk full"))
			},
			assert: func(t *testing.T, _ ntfy.PruneResult, err error) {
				assert.ErrorContains(t, err, "disk full")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, store, broadcaster, _ := mocked(t)
			tc.expect(store, broadcaster)

			pruner, err := ntfy.NewPruner(svc)
			require.NoError(t, err)

			result, err := pruner.Prune(t.Context())
			tc.assert(t, result, err)
		})
	}
}

func TestPrunerRun(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		run    func(t *testing.T, store *ntfy.MockStore, svc *ntfy.Service) error
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name: "a non-positive interval is a configuration error",
			run: func(t *testing.T, _ *ntfy.MockStore, svc *ntfy.Service) error {
				pruner, err := ntfy.NewPruner(svc)
				require.NoError(t, err)

				return pruner.Run(t.Context(), 0)
			},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, ntfy.ErrConfiguration)
			},
		},
		{
			name: "it prunes every interval, reports failed passes and stops when cancelled",
			run: func(t *testing.T, store *ntfy.MockStore, svc *ntfy.Service) error {
				var passes atomic.Int64

				failures := make(chan error, 16)

				store.EXPECT().Prune(gomock.Any(), gomock.Any()).DoAndReturn(
					func(context.Context, ntfy.PruneRequest) (ntfy.PruneResult, error) {
						passes.Add(1)

						return ntfy.PruneResult{}, errors.New("database is restarting")
					},
				).MinTimes(2)

				pruner, err := ntfy.NewPruner(svc, ntfy.WithPruneErrorHandler(func(_ context.Context, err error) {
					select {
					case failures <- err:
					default:
					}
				}))
				require.NoError(t, err)

				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)

				go func() { done <- pruner.Run(ctx, 5*time.Millisecond) }()

				for range 2 {
					select {
					case err := <-failures:
						assert.ErrorContains(t, err, "database is restarting")
					case <-time.After(5 * time.Second):
						t.Fatal("no pass reported its failure")
					}
				}

				cancel()

				select {
				case err := <-done:
					assert.GreaterOrEqual(t, passes.Load(), int64(2))

					return err
				case <-time.After(5 * time.Second):
					t.Fatal("Run did not return after cancellation")

					return nil
				}
			},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, context.Canceled)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, store, _, _ := mocked(t)

			tc.assert(t, tc.run(t, store, svc))
		})
	}
}
