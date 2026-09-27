package ntfy_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
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

	// publishedLinks and publishedData belong to the isolation case below: it
	// mutates them after Publish returns, so the insertion the store was handed
	// must already be a copy.
	publishedLinks := map[string]string{"task": "/v1/tasks/task-1"}
	publishedData := json.RawMessage(`{"by":"carol"}`)
	published := new([]ntfy.Insertion)

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
		{
			name: "a draft mutated after publishing does not change what the store was handed",
			drafts: []ntfy.Draft{{
				Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer",
				Links: publishedLinks, Data: publishedData,
			}},
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().Insert(gomock.Any(), "task-1", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, insertions []ntfy.Insertion) (ntfy.InsertResult, error) {
						*published = insertions

						return createAll(insertions), nil
					})
				broadcaster.EXPECT().Broadcast(gomock.Any(), gomock.Any()).Return(nil)
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, _ []error) {
				require.NoError(t, err)
				require.Len(t, result.Created, 1)

				publishedLinks["task"] = "/hijacked"
				publishedData[2] = 'X'

				require.Len(t, *published, 1)
				assert.Equal(t, "/v1/tasks/task-1", (*published)[0].Notification.Links["task"],
					"the store was handed a copy of the draft's links")
				assert.JSONEq(t, `{"by":"carol"}`, string((*published)[0].Notification.Data))
				assert.Equal(t, "/hijacked", publishedLinks["task"], "the caller's own map did change")
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

func TestServicePublishRefusesTooManyDrafts(t *testing.T) {
	t.Parallel()

	// No Insert is expected: the refusal happens before any transaction opens.
	svc, _, _, _ := mocked(t)

	drafts := make([]ntfy.Draft, 0, ntfy.DefaultMaxDraftsPerPublish+1)
	for i := range ntfy.DefaultMaxDraftsPerPublish + 1 {
		drafts = append(drafts, ntfy.Draft{
			Recipient: "alice", SourceID: "event-" + strconv.Itoa(i), Subject: "task-1", Kind: "offer",
		})
	}

	result, err := svc.Publish(t.Context(), drafts...)

	require.ErrorIs(t, err, ntfy.ErrValidation)
	assert.Empty(t, result.Created)

	var validation *ntfy.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Len(t, validation.Issues, 1)
	assert.Equal(t, "/drafts", validation.Issues[0].Pointer)
}

func TestServicePublishHonoursConfiguredLimits(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{"padding":"` + strings.Repeat("p", ntfy.DefaultMaxDataBytes) + `"}`)

	draft := ntfy.Draft{
		Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer", Data: payload,
	}

	t.Run("the default payload limit refuses an oversized draft", func(t *testing.T) {
		t.Parallel()

		svc, err := ntfy.New(ntfy.NewMemoryStore())
		require.NoError(t, err)

		_, err = svc.Publish(t.Context(), draft)
		assert.ErrorIs(t, err, ntfy.ErrValidation)
	})

	t.Run("a raised payload limit accepts it and returns it byte for byte", func(t *testing.T) {
		t.Parallel()

		svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithLimits(ntfy.Limits{
			MaxDataBytes: ntfy.Limit(1 << 20),
		}))
		require.NoError(t, err)

		result, err := svc.Publish(t.Context(), draft)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)

		stored, err := svc.Get(t.Context(), "alice", result.Created[0].ID)
		require.NoError(t, err)
		assert.Equal(t, []byte(payload), []byte(stored.Data))
	})

	t.Run("a lowered draft cap refuses a publish the default would accept", func(t *testing.T) {
		t.Parallel()

		svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithLimits(ntfy.Limits{
			MaxDraftsPerPublish: ntfy.Limit(1),
		}))
		require.NoError(t, err)

		second := draft
		second.Data = nil
		second.SourceID = "event-2"

		first := draft
		first.Data = nil

		_, err = svc.Publish(t.Context(), first, second)
		assert.ErrorIs(t, err, ntfy.ErrValidation)
	})
}
