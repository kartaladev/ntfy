package ntfy_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kartaladev/ntfy"
)

// serviceAt is the instant a mocked service's clock reads.
var serviceAt = time.Date(2026, 9, 14, 8, 30, 0, 0, time.UTC)

// sequence returns an ID generator minting id-1, id-2, ...
func sequence() ntfy.IDGenerator {
	var n atomic.Int64

	return ntfy.IDGeneratorFunc(func() (string, error) {
		return "id-" + strconv.FormatInt(n.Add(1), 10), nil
	})
}

// signalErrors records what a service hands its signal error handler.
type signalErrors struct {
	mu   sync.Mutex
	errs []error
}

// handle is a signal error handler.
func (s *signalErrors) handle(_ context.Context, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.errs = append(s.errs, err)
}

// all returns what was recorded.
func (s *signalErrors) all() []error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]error(nil), s.errs...)
}

// mocked builds a service over mock ports.
func mocked(t *testing.T) (*ntfy.Service, *ntfy.MockStore, *ntfy.MockBroadcaster, *signalErrors) {
	t.Helper()

	ctrl := gomock.NewController(t)
	store := ntfy.NewMockStore(ctrl)
	broadcaster := ntfy.NewMockBroadcaster(ctrl)
	handler := &signalErrors{}

	svc, err := ntfy.New(store,
		ntfy.WithBroadcaster(broadcaster),
		ntfy.WithClock(ntfy.ClockFunc(func() time.Time { return serviceAt })),
		ntfy.WithIDGenerator(sequence()),
		ntfy.WithSignalErrorHandler(handler.handle),
	)
	require.NoError(t, err)

	return svc, store, broadcaster, handler
}

// insertReturns makes a mocked Insert create every insertion it is given.
func insertReturns(created func(insertions []ntfy.Insertion) ntfy.InsertResult) func(context.Context, string, []ntfy.Insertion) (ntfy.InsertResult, error) {
	return func(_ context.Context, _ string, insertions []ntfy.Insertion) (ntfy.InsertResult, error) {
		return created(insertions), nil
	}
}

// createAll is an insert result creating every insertion.
func createAll(insertions []ntfy.Insertion) ntfy.InsertResult {
	var result ntfy.InsertResult
	for _, insertion := range insertions {
		result.Created = append(result.Created, insertion.Notification)
	}

	return result
}

func TestServicePublish(t *testing.T) {
	t.Parallel()

	draft := func(recipient, subject string) ntfy.Draft {
		return ntfy.Draft{Recipient: recipient, SourceID: "event-1", Subject: subject, Kind: "offer", SubjectVersion: 2}
	}

	type testCase struct {
		name   string
		drafts []ntfy.Draft
		expect func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster)
		assert func(t *testing.T, result ntfy.PublishResult, err error, handled []error)
	}

	cases := []testCase{
		{
			name:   "an invalid draft publishes nothing",
			drafts: []ntfy.Draft{draft("alice", "task-1"), {Recipient: "bob"}},
			expect: func(*testing.T, *ntfy.MockStore, *ntfy.MockBroadcaster) {},
			assert: func(t *testing.T, _ ntfy.PublishResult, err error, _ []error) {
				assert.ErrorIs(t, err, ntfy.ErrValidation)
			},
		},
		{
			name:   "no drafts publish nothing",
			expect: func(*testing.T, *ntfy.MockStore, *ntfy.MockBroadcaster) {},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, _ []error) {
				require.NoError(t, err)
				assert.Empty(t, result.Created)
			},
		},
		{
			name: "drafts are stamped and inserted one subject at a time, in subject order",
			drafts: []ntfy.Draft{
				draft("alice", "task-b"),
				{Recipient: "bob", SourceID: "event-2", Subject: "task-a", Kind: "offer", Coalesce: true},
				draft("carol", "task-b"),
			},
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				gomock.InOrder(
					store.EXPECT().Insert(gomock.Any(), "task-a", gomock.Any()).DoAndReturn(
						func(_ context.Context, _ string, insertions []ntfy.Insertion) (ntfy.InsertResult, error) {
							require.Len(t, insertions, 1)
							assert.True(t, insertions[0].Coalesce, "the draft's coalescing is kept")

							n := insertions[0].Notification
							assert.Equal(t, "bob", n.Recipient)
							assert.Equal(t, ntfy.StateActive, n.State)
							assert.True(t, serviceAt.Equal(n.CreatedAt))
							assert.NotEmpty(t, n.ID)

							return createAll(insertions), nil
						}),
					store.EXPECT().Insert(gomock.Any(), "task-b", gomock.Any()).DoAndReturn(
						func(_ context.Context, _ string, insertions []ntfy.Insertion) (ntfy.InsertResult, error) {
							require.Len(t, insertions, 2)
							assert.Equal(t, "alice", insertions[0].Notification.Recipient)
							assert.Equal(t, "carol", insertions[1].Notification.Recipient)
							assert.NotEqual(t, insertions[0].Notification.ID, insertions[1].Notification.ID)

							return createAll(insertions), nil
						}),
					broadcaster.EXPECT().Broadcast(gomock.Any(), []ntfy.Signal{
						{Recipient: "alice", Change: ntfy.ChangeCreated, At: serviceAt},
						{Recipient: "bob", Change: ntfy.ChangeCreated, At: serviceAt},
						{Recipient: "carol", Change: ntfy.ChangeCreated, At: serviceAt},
					}).Return(nil),
				)
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, handled []error) {
				require.NoError(t, err)
				assert.Len(t, result.Created, 3)
				assert.Empty(t, handled)
			},
		},
		{
			name:   "results are aggregated across subjects",
			drafts: []ntfy.Draft{draft("alice", "task-1"), draft("bob", "task-2")},
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().Insert(gomock.Any(), "task-1", gomock.Any()).
					Return(ntfy.InsertResult{Duplicates: 1, Coalesced: 2}, nil)
				store.EXPECT().Insert(gomock.Any(), "task-2", gomock.Any()).DoAndReturn(
					insertReturns(func(insertions []ntfy.Insertion) ntfy.InsertResult {
						result := createAll(insertions)
						result.Suppressed = 3

						return result
					}))
				broadcaster.EXPECT().Broadcast(gomock.Any(), []ntfy.Signal{
					{Recipient: "bob", Change: ntfy.ChangeCreated, At: serviceAt},
				}).Return(nil)
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, _ []error) {
				require.NoError(t, err)
				assert.Len(t, result.Created, 1)
				assert.Equal(t, 1, result.Duplicates)
				assert.Equal(t, 3, result.Suppressed)
				assert.Equal(t, 2, result.Coalesced)
			},
		},
		{
			name:   "nothing created signals nobody",
			drafts: []ntfy.Draft{draft("alice", "task-1")},
			expect: func(t *testing.T, store *ntfy.MockStore, _ *ntfy.MockBroadcaster) {
				store.EXPECT().Insert(gomock.Any(), "task-1", gomock.Any()).Return(ntfy.InsertResult{Duplicates: 1}, nil)
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, _ []error) {
				require.NoError(t, err)
				assert.Equal(t, 1, result.Duplicates)
			},
		},
		{
			name:   "a recipient created twice is signalled once",
			drafts: []ntfy.Draft{draft("alice", "task-1"), {Recipient: "alice", SourceID: "event-9", Subject: "task-1", Kind: "taken"}},
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().Insert(gomock.Any(), "task-1", gomock.Any()).DoAndReturn(insertReturns(createAll))
				broadcaster.EXPECT().Broadcast(gomock.Any(), []ntfy.Signal{
					{Recipient: "alice", Change: ntfy.ChangeCreated, At: serviceAt},
				}).Return(nil)
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, _ []error) {
				require.NoError(t, err)
				assert.Len(t, result.Created, 2)
			},
		},
		{
			name:   "a broadcast failure reaches the handler and does not fail the publish",
			drafts: []ntfy.Draft{draft("alice", "task-1")},
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().Insert(gomock.Any(), "task-1", gomock.Any()).DoAndReturn(insertReturns(createAll))
				broadcaster.EXPECT().Broadcast(gomock.Any(), gomock.Any()).Return(errors.New("broker down"))
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, handled []error) {
				require.NoError(t, err)
				assert.Len(t, result.Created, 1)
				require.Len(t, handled, 1)
				assert.ErrorContains(t, handled[0], "broker down")
			},
		},
		{
			name:   "a store failure is returned after signalling what was already written",
			drafts: []ntfy.Draft{draft("alice", "task-1"), draft("bob", "task-2")},
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				gomock.InOrder(
					store.EXPECT().Insert(gomock.Any(), "task-1", gomock.Any()).DoAndReturn(insertReturns(createAll)),
					store.EXPECT().Insert(gomock.Any(), "task-2", gomock.Any()).
						Return(ntfy.InsertResult{}, errors.New("connection reset")),
					broadcaster.EXPECT().Broadcast(gomock.Any(), []ntfy.Signal{
						{Recipient: "alice", Change: ntfy.ChangeCreated, At: serviceAt},
					}).Return(nil),
				)
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, _ []error) {
				assert.ErrorContains(t, err, "connection reset")
				assert.Len(t, result.Created, 1, "what was written before the failure is reported")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, store, broadcaster, handler := mocked(t)
			tc.expect(t, store, broadcaster)

			result, err := svc.Publish(t.Context(), tc.drafts...)
			tc.assert(t, result, err, handler.all())
		})
	}
}
