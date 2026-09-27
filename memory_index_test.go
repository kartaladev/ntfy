package ntfy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertRecipientIndex checks the invariant put and remove maintain: every
// stored notification appears once under its own recipient, the index holds
// nothing else, and no empty set is left behind.
func assertRecipientIndex(t *testing.T, s *MemoryStore) {
	t.Helper()

	indexed := 0

	for recipient, ids := range s.recipients {
		assert.NotEmptyf(t, ids, "recipient %q has an empty index entry", recipient)

		for id := range ids {
			n, stored := s.notifications[id]
			if assert.Truef(t, stored, "index holds %s for %q, which is not stored", id, recipient) {
				assert.Equalf(t, recipient, n.Recipient, "index holds %s under %q, stored under %q", id, recipient, n.Recipient)
			}

			indexed++
		}
	}

	assert.Equalf(t, len(s.notifications), indexed,
		"index holds %d identifiers, the store holds %d notifications", indexed, len(s.notifications))
}

func TestMemoryStoreRecipientIndexMirrorsTheStore(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	seed := func(t *testing.T, s *MemoryStore) {
		t.Helper()

		for i, recipient := range []string{"alice", "bob", "alice"} {
			id := "id-" + string(rune('a'+i))

			_, err := s.Insert(t.Context(), "task-1", []Insertion{{Notification: Notification{
				ID: id, Recipient: recipient, SourceID: "evt-" + id, Subject: "task-1",
				Kind: "offer", State: StateActive, CreatedAt: at.Add(time.Duration(i) * time.Second),
			}}})
			require.NoError(t, err)
		}
	}

	type testCase struct {
		name    string
		operate func(t *testing.T, s *MemoryStore)
		assert  func(t *testing.T, s *MemoryStore)
	}

	cases := []testCase{
		{
			name:    "after inserting",
			operate: func(*testing.T, *MemoryStore) {},
			assert: func(t *testing.T, s *MemoryStore) {
				assert.Len(t, s.recipients, 2)
				assert.Len(t, s.recipients["alice"], 2)
				assert.Len(t, s.recipients["bob"], 1)
			},
		},
		{
			name: "after closing, which changes state but not membership",
			operate: func(t *testing.T, s *MemoryStore) {
				_, err := s.Close(t.Context(), CloseRequest{Subject: "task-1", Version: 1, Reason: "taken"},
					at.Add(time.Minute), NewUUIDv7Generator())
				require.NoError(t, err)
			},
			assert: func(t *testing.T, s *MemoryStore) {
				assert.Len(t, s.recipients["alice"], 2)
				assert.Len(t, s.recipients["bob"], 1)
			},
		},
		{
			name: "after marking read, which changes state but not membership",
			operate: func(t *testing.T, s *MemoryStore) {
				_, err := s.MarkAllRead(t.Context(), "alice", at.Add(time.Hour), at.Add(time.Minute))
				require.NoError(t, err)
			},
			assert: func(t *testing.T, s *MemoryStore) {
				assert.Len(t, s.recipients["alice"], 2)
			},
		},
		{
			name: "after pruning removes every one of a recipient's notifications",
			operate: func(t *testing.T, s *MemoryStore) {
				_, err := s.MarkAllRead(t.Context(), "bob", at.Add(time.Hour), at.Add(time.Minute))
				require.NoError(t, err)

				_, err = s.Prune(t.Context(), PruneRequest{
					Now: at.Add(48 * time.Hour), MaxAge: time.Hour, Strategy: EvictOldestActive,
				})
				require.NoError(t, err)
			},
			assert: func(t *testing.T, s *MemoryStore) {
				assert.NotContains(t, s.recipients, "bob", "an emptied recipient keeps no index entry")
			},
		},
		{
			name: "after the count bound evicts part of a recipient's notifications",
			operate: func(t *testing.T, s *MemoryStore) {
				_, err := s.Prune(t.Context(), PruneRequest{
					Now: at.Add(time.Hour), MaxPerRecipient: 1, Strategy: EvictOldestActive,
				})
				require.NoError(t, err)
			},
			assert: func(t *testing.T, s *MemoryStore) {
				assert.Len(t, s.recipients["alice"], 1)
				assert.Contains(t, s.recipients["alice"], "id-c", "the newest survives the count bound")
				assert.Len(t, s.recipients["bob"], 1)
			},
		},
		{
			// The IDGenerator is the consumer's to replace, and the memory
			// store has no primary key to reject a repeat. The later
			// notification replaces the earlier, as it always has, and no index
			// may keep pointing the earlier recipient at it.
			name: "after an identifier is reused for another recipient",
			operate: func(t *testing.T, s *MemoryStore) {
				_, err := s.Insert(t.Context(), "task-2", []Insertion{{Notification: Notification{
					ID: "id-a", Recipient: "carol", SourceID: "evt-reused", Subject: "task-2",
					Kind: "offer", State: StateActive, CreatedAt: at.Add(time.Hour),
				}}})
				require.NoError(t, err)
			},
			assert: func(t *testing.T, s *MemoryStore) {
				assert.NotContains(t, s.recipients["alice"], "id-a", "alice keeps no entry for carol's notification")
				assert.NotContains(t, s.subjects["task-1"], "id-a", "task-1 keeps no entry for carol's notification")
				assert.NotContains(t, s.sources, sourceKey{source: "evt-id-a", recipient: "alice"})

				count, err := s.CountActive(t.Context(), "alice")
				require.NoError(t, err)
				assert.Equal(t, int64(1), count, "alice counts only her own notification")

				marked, err := s.MarkAllRead(t.Context(), "alice", at.Add(2*time.Hour), at.Add(2*time.Hour))
				require.NoError(t, err)
				assert.Equal(t, int64(1), marked.Marked, "alice marks only her own notification")

				carol, err := s.Get(t.Context(), "carol", "id-a")
				require.NoError(t, err)
				assert.Equal(t, StateActive, carol.State, "carol's notification is untouched by alice")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := NewMemoryStore()
			seed(t, s)
			tc.operate(t, s)

			assertRecipientIndex(t, s)
			tc.assert(t, s)
		})
	}
}
