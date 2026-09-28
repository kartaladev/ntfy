# Align the Memory Store with SQL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the memory store (and MySQL, for reused identifiers) behave exactly like the other SQL stores on the store-contract edges the 2026-09-28 audit found, and hold every store, a host's own included, to that in `ntfytest`.

**Architecture:**
- Two new conformance groups carry every case as a red test first: `ntfytest/edges.go` (`runEdges`, registered in `Run`) and `ntfytest/email_edges.go` (`runEmailEdges`, registered in `RunEmail`).
- The fixes are small and local:
  - `MemoryStore.insert` refuses a taken identifier and undoes its own writes;
  - `MemoryStore.Close` restores a snapshot when its successors are refused;
  - `sqlstore`'s `writeNotifications` read-back compares source and recipient, which catches MySQL's absorbed primary-key collision;
  - `MemoryStore.Insert` begins the close record on every non-empty call;
  - `MemoryStore.Prune` truncates both cutoffs to microseconds;
  - the memory `EmailStore` treats a limit ≤ 0 as "nothing", and changes only held deliveries.

**Tech Stack:**
- Go 1.26, with the core module on the standard library only.
- `stretchr/testify`, and `testcontainers-go` through `sqlkittest.RunTestMySQL` / `RunTestPostgres` (MySQL 8.4.6). `sqlkittest.RunTestSQLite` needs no Docker.
- `golangci-lint` v2 and `openspec`.

**Spec:** `openspec/changes/align-memory-store-with-sql/`. Read:
- `proposal.md`: why, and the failing audit output;
- `design.md`: D1 reused identifiers, D2 close records, D3 cutoffs, D4 email edges, D5 where the cases live;
- `specs/notification-inbox/spec.md`, `specs/notification-retention/spec.md` and `specs/notification-email/spec.md`: the scenarios this plan must satisfy.

## Global Constraints

- **Go 1.26: export `GOTOOLCHAIN=go1.26.8` before any Go command.** A newer Go may be first on `PATH`. The `make` targets already pin it.
- **The core module imports only the standard library** in production code. Tests may use `testify`, `goleak` and `go.uber.org/mock`. Enforced by `.golangci.yml` depguard and `make split-check`. This change adds only `fmt` to `memory.go`.
- **Each satellite module may import only `ntfy`, `sqlkit` and its own client library.** `make split-check` is authoritative. `ntfytest` already imports `ntfy` and `testify`; nothing new.
- **`pkg/sqlkit` is not touched.** `make sqlkit-copy-check` must stay green.
- **Tests follow the `table-test` skill:**
  - an `assert` closure on every case, never `want`/`wantErr` fields;
  - `t.Context()`, not `context.Background()`;
  - `require` only for preconditions.
  - Neither new group varies context, so neither table has a `ctx` field. Each file says so in its doc comment (skill rule 3 applies only to context-sensitive behaviour).
- **Test doubles come from the `use-mockgen` skill.** This change needs none. `ntfy.IDGeneratorFunc` is the port's own adapter, not a double.
- **External services come from the `use-testcontainers` skill.** Use the existing `sqlstore` entry points (`TestStoreOnStdSQL*`, `TestStoreOnPgx*`, `gormtest`). Never write a new container helper.
- **`.claude/rules/prove-errors-with-tests.md`:** each red step is run, and its failing output is compared with the output quoted here, before any production edit. A red caused by compilation, a missing fixture or a container error does not count.
- **`.claude/rules/golang-tdd.md`:** red → green → refactor. The test and the code that satisfies it land in the same commit. Consider `/simplify` on touched code after each green.
- **`.claude/rules/library-design.md`:** no option is added and no default changes for a service user (`design.md` D1–D4 each state default and override). Do not add an option.
- **`.claude/rules/performance-benchmark.md`:** makes no performance claim, so no benchmark. The MySQL read-back reading two more columns is labelled **unverified** in `design.md`, and justifies nothing.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn.
- **`.claude/rules/gopls-navigation.md`:** use `gopls` (for example `gopls references`) to find every caller of `MemoryStore.insert` before changing its signature. There are two: `Insert` and `Close`.
- **Done means `make all` and `make store-matrix` pass**, since a store changes.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`, and end with:

  ```
  Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A
  ```

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `ntfytest/edges.go` | `ntfy/ntfytest` | **Create.** `runEdges`: the store edges (reused identifiers, close record on an insert that creates nothing, microsecond cutoffs). |
| `ntfytest/email_edges.go` | `ntfy/ntfytest` | **Create.** `runEmailEdges`: the `EmailStore` edges (limit ≤ 0, empty owner). |
| `ntfytest/suite.go:27-38` | `ntfy/ntfytest` | **Modify.** Register `edges` in `Run`. |
| `ntfytest/email.go:36-46` | `ntfy/ntfytest` | **Modify.** Register `edges` in `RunEmail`. |
| `ntfytest/doc.go` | `ntfy/ntfytest` | **Modify.** Name the edges the suite asserts. |
| `memory.go` | `ntfy` | **Modify.** `Insert`, `insert`, `put`, `Close`, a new `snapshot`, and `Prune`. |
| `memory_email.go` | `ntfy` | **Modify.** `ClaimEmails`, `RecordEmails`, `PurgeEmailRecords`. |
| `memory_index_test.go:122-151` | `ntfy` | **Modify.** The reused-identifier row now asserts refusal. |
| `sqlstore/insert.go` (`writeNotifications`) | `ntfy/sqlstore` | **Modify.** The read-back compares source and recipient. |
| `store.go`, `email.go`, `id.go` | `ntfy` | **Modify.** Godoc for the edges. |

**Shared-file note:** `Run` in `ntfytest/suite.go` and `RunEmail` in `ntfytest/email.go` are the usual registration points. A change in flight that also adds a group conflicts textually on the adjacent line; resolve by keeping both lines.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: refuse a reused identifier | 1.1, 1.2, 1.3, 1.4 |
| Task 2: close record on every non-empty insert | 2.1 |
| Task 3: microsecond cutoffs | 3.1 |
| Task 4: the `EmailStore` edges | 4.1, 4.2 |
| Task 5: document the edges | 5.1 |
| Task 6: verify | 6.1 |

Task 1 gathers four `tasks.md` items into one commit. Its red cases fail on memory and on MySQL, and `make store-matrix` is green only once both stores are fixed.

---

### Task 1: Refuse a reused notification identifier (tasks 1.1–1.4)

**Files:**
- Create: `ntfytest/edges.go`
- Modify: `ntfytest/suite.go:27-38`, `memory.go`, `memory_index_test.go:122-151`, `sqlstore/insert.go`

**Interfaces:**
- Consumes (existing, in `ntfytest`):
  - `type Factory func(t *testing.T) ntfy.Store`;
  - `newEnv(t *testing.T, factory Factory) *env`, with the fields `t`, `store ntfy.Store` and `ids *ntfy.UUIDv7Generator`;
  - on `*env`:
    - `note(recipient, source, subject, kind string, version int64, created time.Time) ntfy.Notification`;
    - `insert(coalesce bool, notifications ...ntfy.Notification) ntfy.InsertResult`;
    - `tryInsert(coalesce bool, notifications ...ntfy.Notification) (ntfy.InsertResult, error)`;
    - `get(recipient, id string) ntfy.Notification`;
    - `list(q ntfy.ListQuery) ntfy.Page`;
  - `parallel(t *testing.T, name string, fn func(t *testing.T))`, `at(n int) time.Time`.
- Consumes (existing, in `ntfy`):
  - `func (s *MemoryStore) remove(n Notification)`;
  - `func (s *MemoryStore) ensureWatermark(key watermarkKey, at time.Time)`;
  - `type watermarkKey struct{ subject, kind string }`, `type watermark struct{ version int64; updatedAt time.Time }`;
  - `func (r CloseRequest) SuccessorInsertions(recipients []string, at time.Time, ids IDGenerator) ([]Insertion, error)`.
- Consumes (existing, in `sqlstore`): `type pairKey struct{ first, second string }`, `func pair(first, second string) pairKey`, `func (s *Store) queryTexts(ctx context.Context, statement sqlkit.Statement, width int, visit func(values []string)) error`, `func (s *Store) columnList(names ...string) string`.
- Produces:
  - `func runEdges(t *testing.T, factory Factory)`, package-private, registered in `Run`;
  - `func (s *MemoryStore) insert(subject string, insertions []Insertion) (InsertResult, error)`, replacing `insert(...) InsertResult`;
  - `func (s *MemoryStore) snapshot(keys []watermarkKey, ids []string) func()`;
  - the error texts `ntfy: notification identifier %q is already stored` (memory) and `sqlstore: notification identifier %q is already stored` (MySQL's read-back). Neither is a sentinel (`design.md` D1).

- [ ] **Step 1: Write the failing conformance cases (task 1.1)**

Create `ntfytest/edges.go`:

```go
package ntfytest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// runEdges holds every store to one behaviour on inputs the store contract
// admits but the service rarely sends: an identifier the host's generator
// repeats, a publish that creates nothing, and instants finer than the
// microsecond every store keeps. The cases do not vary context, so the table
// has no ctx field.
func runEdges(t *testing.T, factory Factory) {
	type testCase struct {
		name   string
		assert func(t *testing.T, e *env)
	}

	cases := []testCase{
		{
			name: "a publish reusing a stored identifier is refused and keeps what is stored",
			assert: func(t *testing.T, e *env) {
				stored := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
				e.insert(false, stored)

				reused := e.note("bob", "event-2", "task-2", "offer", 1, at(1))
				reused.ID = stored.ID

				_, err := e.tryInsert(false, reused)
				require.Error(t, err, "a publish reusing alice's identifier is refused")

				got := e.get("alice", stored.ID)
				assert.Equal(t, "task-1", got.Subject, "alice's notification is unchanged")

				_, err = e.store.Get(t.Context(), "bob", stored.ID)
				require.ErrorIs(t, err, ntfy.ErrNotFound, "bob has no notification under alice's identifier")
			},
		},
		{
			name: "a publish repeating an identifier within itself writes nothing",
			assert: func(t *testing.T, e *env) {
				carol := e.note("carol", "event-1", "task-1", "offer", 1, at(0))
				alice := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
				bob := e.note("bob", "event-1", "task-1", "offer", 1, at(0))
				bob.ID = alice.ID

				_, err := e.tryInsert(false, carol, alice, bob)
				require.Error(t, err, "a publish repeating an identifier is refused")

				for _, recipient := range []string{"carol", "alice", "bob"} {
					assert.Emptyf(t, e.list(ntfy.ListQuery{Recipient: recipient}).Notifications,
						"nothing of the refused publish is stored for %s", recipient)
				}
			},
		},
		{
			name: "a close whose successor reuses a stored identifier changes nothing",
			assert: func(t *testing.T, e *env) {
				e.insert(false, e.note("alice", "event-1", "task-1", "offer", 1, at(0)))

				carol := e.note("carol", "event-9", "task-9", "offer", 1, at(0))
				e.insert(false, carol)

				repeating := ntfy.IDGeneratorFunc(func() (string, error) { return carol.ID, nil })

				_, err := e.store.Close(t.Context(), ntfy.CloseRequest{
					Subject: "task-1", Version: 5, Reason: "taken",
					Successor: &ntfy.Successor{SourceID: "event-5", Kind: "taken", SubjectVersion: 5},
				}, at(4), repeating)
				require.Error(t, err, "a close whose successor reuses carol's identifier is refused")

				page := e.list(ntfy.ListQuery{Recipient: "alice"})
				require.Len(t, page.Notifications, 1, "alice has only the offer: no successor was written")
				assert.Equal(t, ntfy.StateActive, page.Notifications[0].State, "alice's offer is still open")

				assert.Equal(t, "task-9", e.get("carol", carol.ID).Subject, "carol's notification is unchanged")

				late := e.insert(false, e.note("dave", "event-4", "task-1", "offer", 4, at(5)))
				assert.Len(t, late.Created, 1, "the close record was not raised, so a publish below it is not suppressed")
			},
		},
		{
			name: "an insertion that writes nothing is not checked for its identifier",
			assert: func(t *testing.T, e *env) {
				alice := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
				bob := e.note("bob", "event-2", "task-1", "offer", 1, at(0))
				e.insert(false, alice, bob)

				again := e.note("alice", "event-1", "task-1", "offer", 1, at(1))
				again.ID = bob.ID

				result := e.insert(false, again)
				assert.Equal(t, 1, result.Duplicates, "a redelivered source is a duplicate, whatever its identifier")
				assert.Empty(t, result.Created)
				assert.Equal(t, "event-2", e.get("bob", bob.ID).SourceID, "bob's notification is unchanged")
			},
		},
	}

	for _, tc := range cases {
		parallel(t, tc.name, func(t *testing.T) {
			tc.assert(t, newEnv(t, factory))
		})
	}
}
```

In `ntfytest/suite.go`, add the last line of `Run`:

```go
	t.Run("retention", func(t *testing.T) { runRetention(t, factory) })
	t.Run("edges", func(t *testing.T) { runEdges(t, factory) })
}
```

- [ ] **Step 2: Run the cases on memory and watch three fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/edges' -count=1 .`

Expected: FAIL. The guard passes; the other three fail for the stated reason:

```
--- FAIL: TestMemoryStoreConformance/edges/a_close_whose_successor_reuses_a_stored_identifier_changes_nothing
        Error:      	An error is expected but got nil.
        Messages:   	a close whose successor reuses carol's identifier is refused
--- FAIL: TestMemoryStoreConformance/edges/a_publish_repeating_an_identifier_within_itself_writes_nothing
        Error:      	An error is expected but got nil.
        Messages:   	a publish repeating an identifier is refused
--- FAIL: TestMemoryStoreConformance/edges/a_publish_reusing_a_stored_identifier_is_refused_and_keeps_what_is_stored
        Error:      	An error is expected but got nil.
        Messages:   	a publish reusing alice's identifier is refused
```

- [ ] **Step 3: Run the cases on SQL and watch MySQL fail**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQL(MySQL|Postgres|SQLite)/edges' -count=1 .`

Expected: FAIL on MySQL only, with the same three `An error is expected but got nil.` lines under `TestStoreOnStdSQLMySQL/edges/...`. PostgreSQL and SQLite pass: their drivers refuse the primary-key collision.

- [ ] **Step 4: Rewrite the index row that asserted replacement (task 1.2, red)**

In `memory_index_test.go`, replace the whole last row of `cases` in `TestMemoryStoreRecipientIndexMirrorsTheStore` (the one named "after an identifier is reused for another recipient") with:

```go
		{
			// The IDGenerator is the consumer's to replace. An insert reusing
			// a stored identifier is refused, as the SQL store's primary key
			// refuses it, and neither the refused notification nor the one it
			// collided with moves in any index.
			name: "after an insert reusing an identifier is refused",
			operate: func(t *testing.T, s *MemoryStore) {
				_, err := s.Insert(t.Context(), "task-2", []Insertion{
					{Notification: Notification{
						ID: "id-fresh", Recipient: "dave", SourceID: "evt-fresh", Subject: "task-2",
						Kind: "offer", State: StateActive, CreatedAt: at.Add(time.Hour),
					}},
					{Notification: Notification{
						ID: "id-a", Recipient: "carol", SourceID: "evt-reused", Subject: "task-2",
						Kind: "offer", State: StateActive, CreatedAt: at.Add(time.Hour),
					}},
				})
				require.Error(t, err)
			},
			assert: func(t *testing.T, s *MemoryStore) {
				assert.Contains(t, s.recipients["alice"], "id-a", "alice keeps her notification")
				assert.Contains(t, s.subjects["task-1"], "id-a", "task-1 keeps alice's notification")
				assert.NotContains(t, s.recipients, "carol", "carol has no index entry")
				assert.NotContains(t, s.recipients, "dave", "the insert's earlier notification was taken back")
				assert.NotContains(t, s.subjects, "task-2", "task-2 has no index entry")
				assert.NotContains(t, s.sources, sourceKey{source: "evt-reused", recipient: "carol"})
				assert.NotContains(t, s.sources, sourceKey{source: "evt-fresh", recipient: "dave"})

				alice, err := s.Get(t.Context(), "alice", "id-a")
				require.NoError(t, err)
				assert.Equal(t, "evt-id-a", alice.SourceID, "alice's notification is unchanged")
			},
		},
```

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreRecipientIndexMirrorsTheStore' -count=1 .`

Expected: FAIL on `after_an_insert_reusing_an_identifier_is_refused` with `An error is expected but got nil.`

- [ ] **Step 5: Refuse a taken identifier in `insert` (task 1.2, green)**

In `memory.go`, add `"fmt"` to the imports, after `"context"`. Replace `Insert`, `insert` and `put` (from `// Insert implements [Store].` through the end of `put`) with:

```go
// Insert implements [Store]. It is all or nothing: a notification whose
// identifier is already stored fails the insert, and nothing is written.
func (s *MemoryStore) Insert(_ context.Context, subject string, insertions []Insertion) (InsertResult, error) {
	if len(insertions) == 0 {
		return InsertResult{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.insert(subject, insertions)
}

// insert applies suppression, coalescing and idempotency, in that order, to
// notifications on one subject. A notification it would write under an
// identifier already stored, this call's own included, fails the insert: what
// the call wrote is removed, and the store is as it was. The caller holds the
// lock.
func (s *MemoryStore) insert(subject string, insertions []Insertion) (InsertResult, error) {
	var result InsertResult

	for _, insertion := range insertions {
		n := insertion.Notification
		n.Subject = subject

		if n.SubjectVersion < s.floor(subject, n.Kind) {
			result.Suppressed++

			continue
		}

		if insertion.Coalesce && s.hasOpen(n.Recipient, subject, n.Kind) {
			result.Coalesced++

			continue
		}

		if _, duplicate := s.sources[sourceKey{source: n.SourceID, recipient: n.Recipient}]; duplicate {
			result.Duplicates++

			continue
		}

		if _, taken := s.notifications[n.ID]; taken {
			for _, created := range result.Created {
				s.remove(created)
			}

			return InsertResult{}, fmt.Errorf("ntfy: notification identifier %q is already stored", n.ID)
		}

		s.put(n.Clone())
		result.Created = append(result.Created, n.Clone())

		s.ensureWatermark(watermarkKey{subject: subject, kind: allKinds}, n.CreatedAt)
	}

	return result, nil
}

// put stores a notification under an identifier no notification has, and
// indexes it. The caller holds the lock, and has checked the identifier.
func (s *MemoryStore) put(n Notification) {
	s.notifications[n.ID] = n
	s.sources[sourceKey{source: n.SourceID, recipient: n.Recipient}] = n.ID
	indexAdd(s.subjects, n.Subject, n.ID)
	indexAdd(s.recipients, n.Recipient, n.ID)
}
```

The undo is exact because an insert only adds. Each notification it wrote had a free identifier and a source and recipient no other notification held, so `remove` takes back exactly its entries.

One gap is left on purpose. The `ensureWatermark` inside the loop runs after each successful `put`. So in the index row, `dave`'s `put` begins `task-2`'s close record before `carol`'s refusal, and the undo does not take it back. Task 2 moves `ensureWatermark` out of `insert` and closes the gap. That is why the index row's close-record assertion is added only in Task 2 Step 1.

In `Close`, update the call site (the rest of `Close` changes in Step 7):

```go
	if len(successors) > 0 {
		inserted, err := s.insert(req.Subject, successors)
		if err != nil {
			return CloseResult{}, err
		}

		result.Successors = inserted.Created
		result.SuccessorsSuppressed = inserted.Suppressed
	}
```

- [ ] **Step 6: Run the insert cases and the index row, and check the guard can fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/edges|TestMemoryStoreRecipientIndexMirrorsTheStore' -count=1 .`

Expected: the two insert cases, the guard and every index row pass. Only `a_close_whose_successor_reuses_a_stored_identifier_changes_nothing` still fails, now past `require.Error`. It fails at `alice's offer is still open` (`expected: "ACTIVE"`, `actual  : "CLOSED"`), and at `the close record was not raised, so a publish below it is not suppressed`, because the close was applied before the refusal.

Then prove the guard guards. Temporarily move the `taken` block above the `duplicate` block and re-run. `an_insertion_that_writes_nothing_is_not_checked_for_its_identifier` must fail with `Received unexpected error: ntfy: notification identifier "…" is already stored`. Move the block back.

- [ ] **Step 7: Make `Close` put back what it changed (task 1.3)**

In `memory.go`, replace the middle of `Close`, from `s.ensureWatermark(watermarkKey{subject: req.Subject, kind: allKinds}, at)` through the end of the function, with:

```go
	all := watermarkKey{subject: req.Subject, kind: allKinds}

	keys := []watermarkKey{all}
	if len(req.Kinds) > 0 {
		keys = keys[:0]
		for _, kind := range req.Kinds {
			keys = append(keys, watermarkKey{subject: req.Subject, kind: kind})
		}
	}

	restore := s.snapshot(append([]watermarkKey{all}, keys...), closing)

	s.ensureWatermark(all, at)

	for _, key := range keys {
		mark, ok := s.watermarks[key]
		if !ok || mark.version < req.Version {
			mark.version = req.Version
		}

		mark.updatedAt = at
		s.watermarks[key] = mark
	}

	for _, id := range closing {
		n := s.notifications[id]
		n.State = StateClosed
		n.ClosedReason = req.Reason
		n.ClosedAt = timePtr(at)

		if n.InactiveAt == nil {
			n.InactiveAt = timePtr(at)
		}

		s.notifications[id] = n
		result.Closed++
	}

	if len(successors) > 0 {
		inserted, err := s.insert(req.Subject, successors)
		if err != nil {
			restore()

			return CloseResult{}, err
		}

		result.Successors = inserted.Created
		result.SuccessorsSuppressed = inserted.Suppressed
	}

	return result, nil
}

// snapshot records the close records under keys and the notifications under
// ids as they are now, and returns a function that puts them back. A key with
// no record is put back as absent. The caller holds the lock.
func (s *MemoryStore) snapshot(keys []watermarkKey, ids []string) func() {
	marks := make(map[watermarkKey]watermark, len(keys))

	var absent []watermarkKey

	for _, key := range keys {
		if mark, ok := s.watermarks[key]; ok {
			marks[key] = mark
		} else {
			absent = append(absent, key)
		}
	}

	notifications := make(map[string]Notification, len(ids))
	for _, id := range ids {
		notifications[id] = s.notifications[id]
	}

	return func() {
		for key, mark := range marks {
			s.watermarks[key] = mark
		}

		for _, key := range absent {
			delete(s.watermarks, key)
		}

		for id, n := range notifications {
			s.notifications[id] = n
		}
	}
}
```

Replace `Close`'s doc comment with:

```go
// Close implements [Store]. It is all or nothing: which notifications close,
// and the successors' identifiers, are settled before anything changes, so a
// generator that fails leaves the store as it was. A successor refused for its
// identifier puts back what the close changed.
```

The snapshot needs no deep copy. Closing assigns fresh `timePtr` values to a copied struct and never mutates through the old pointers. It also changes no index membership, since recipient, subject and source are untouched.

- [ ] **Step 8: Run the whole memory package**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .`

Expected: PASS, including every `TestMemoryStoreConformance/edges` case and the existing `a_close_whose_successor_cannot_be_stamped_changes_nothing`.

- [ ] **Step 9: Compare the read-back with source and recipient (task 1.4)**

In `sqlstore/insert.go`, in `writeNotifications`, replace the read-back loop (from `for _, chunk := range chunks(notifications, chunkSize) {` after the INSERT loop, through `return written, nil`) with:

```go
	// stored is the source and recipient of the row under each identifier.
	stored := make(map[string]pairKey, len(notifications))

	for _, chunk := range chunks(notifications, chunkSize) {
		ids := make([]any, 0, len(chunk))
		for _, n := range chunk {
			ids = append(ids, n.ID)
		}

		w := sqlkit.NewWriter(s.dialect)
		w.Write("SELECT ", s.columnList("id", "source_id", "recipient"), " FROM ", s.notificationsTable(),
			" WHERE ", s.quote("id"), " IN (", w.BindAll(ids...), ")")

		if err := s.queryTexts(ctx, w.Done(), 3, func(values []string) {
			stored[values[0]] = pair(values[1], values[2])
		}); err != nil {
			return nil, err
		}
	}

	for _, n := range notifications {
		row, ok := stored[n.ID]

		switch {
		case !ok:
			// Skipped: a concurrent publish took its source and recipient.
		case row == pair(n.SourceID, n.Recipient):
			written[n.ID] = true
		default:
			// Another notification holds the identifier. PostgreSQL and SQLite
			// refuse the INSERT itself; MySQL's ON DUPLICATE KEY UPDATE absorbs
			// a primary-key collision too, so it is caught here instead, and
			// the transaction rolls back.
			return nil, fmt.Errorf("sqlstore: notification identifier %q is already stored", n.ID)
		}
	}

	return written, nil
```

Update `writeNotifications`' doc comment to:

```go
// writeNotifications inserts notifications, skipping any whose source and
// recipient already have one, and reports which identifiers were written. An
// identifier another notification holds fails the write.
```

- [ ] **Step 10: Run every dialect**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQL(MySQL|Postgres|SQLite)$' -count=1 .`

Expected: `ok  	github.com/kartaladev/ntfy/sqlstore`. MySQL's three edges cases now pass.

- [ ] **Step 11: Commit**

```bash
git add ntfytest/edges.go ntfytest/suite.go memory.go memory_index_test.go sqlstore/insert.go
git commit -m "Refuse a reused notification identifier on every store

The memory store replaced whatever was stored under a reused identifier,
another recipient's notification included, and MySQL reported a
notification it never wrote. Both now fail the write and change nothing,
as PostgreSQL and SQLite already did; ntfytest's new edges group holds
every store to it.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 2: Begin the close record on every non-empty insert (task 2.1)

**Files:**
- Modify: `ntfytest/edges.go`, `memory.go` (`Insert`, `insert`), `memory_index_test.go` (the Task 1 row)

**Interfaces:**
- Consumes: `runEdges` and its `testCase{name string; assert func(t *testing.T, e *env)}` (Task 1); `(*env).prune(req ntfy.PruneRequest) ntfy.PruneResult`, `days(n int) time.Duration`; `normalizeTime(time.Time) time.Time` (`clock.go`).
- Produces: `MemoryStore.Insert` begins `(subject, "*")` at `normalizeTime(insertions[0].Notification.CreatedAt)` after a successful insert. `insert` no longer touches close records.

- [ ] **Step 1: Write the failing case**

Append this case to `cases` in `runEdges` (`ntfytest/edges.go`):

```go
		{
			name: "a publish that creates nothing still begins its subject's close record",
			assert: func(t *testing.T, e *env) {
				e.insert(false, e.note("alice", "event-1", "task-1", "offer", 1, at(0)))

				duplicate := e.insert(false, e.note("alice", "event-1", "task-2", "offer", 1, at(0)))
				require.Equal(t, 1, duplicate.Duplicates)

				result := e.prune(ntfy.PruneRequest{Now: at(0).Add(days(30)), WatermarkRetention: days(1)})
				assert.Equal(t, int64(1), result.WatermarksDeleted, "task-2's close record began with its publish, and expired")
			},
		},
```

In `memory_index_test.go`'s Task 1 row ("after an insert reusing an identifier is refused"), add after the `evt-fresh` assertion:

```go
				assert.NotContains(t, s.watermarks, watermarkKey{subject: "task-2", kind: allKinds},
					"a refused insert begins no close record")
```

- [ ] **Step 2: Run it and watch it fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/edges|TestMemoryStoreRecipientIndexMirrorsTheStore' -count=1 .`

Expected: FAIL.

```
--- FAIL: TestMemoryStoreConformance/edges/a_publish_that_creates_nothing_still_begins_its_subject's_close_record
        Error:      	Not equal:
                    	expected: 1
                    	actual  : 0
        Messages:   	task-2's close record began with its publish, and expired
--- FAIL: TestMemoryStoreRecipientIndexMirrorsTheStore/after_an_insert_reusing_an_identifier_is_refused
        Messages:   	a refused insert begins no close record
```

Then run `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/edges' -count=1 .`. Expected: PASS, because SQL already begins the record.

- [ ] **Step 3: Move `ensureWatermark` into `Insert`**

In `memory.go`, delete this line and the blank line before it from the loop in `insert`:

```go
		s.ensureWatermark(watermarkKey{subject: subject, kind: allKinds}, n.CreatedAt)
```

Replace `Insert` with:

```go
// Insert implements [Store]. It is all or nothing: a notification whose
// identifier is already stored fails the insert, and nothing is written. Any
// insert that carries a notification begins the subject's close record, as the
// SQL store's lock row does, whether or not it creates one.
func (s *MemoryStore) Insert(_ context.Context, subject string, insertions []Insertion) (InsertResult, error) {
	if len(insertions) == 0 {
		return InsertResult{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.insert(subject, insertions)
	if err != nil {
		return InsertResult{}, err
	}

	s.ensureWatermark(watermarkKey{subject: subject, kind: allKinds}, normalizeTime(insertions[0].Notification.CreatedAt))

	return result, nil
}
```

`Close`, the other caller of `insert`, already ensures the record at the close's instant before inserting successors, so it needs no change.

- [ ] **Step 4: Run the memory package**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ntfytest/edges.go memory.go memory_index_test.go
git commit -m "Begin a subject's close record on every non-empty insert

The SQL store writes the record first, as the subject's lock; the memory
store wrote it only when a notification was created, so a later pass
reported a different number of expired records.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 3: Compare retention cutoffs at microseconds (task 3.1)

**Files:**
- Modify: `ntfytest/edges.go`, `memory.go` (`Prune`)

**Interfaces:**
- Consumes: `runEdges` (Task 1); on `*env`: `markRead(recipient string, when time.Time, ids ...string) ntfy.MarkResult`, `close(req ntfy.CloseRequest, when time.Time) ntfy.CloseResult`, `all(recipient string) []ntfy.Notification`.
- Produces: `MemoryStore.Prune` compares `normalizeTime(req.Now.Add(-req.MaxAge))` and `normalizeTime(req.Now.Add(-req.WatermarkRetention))`.

- [ ] **Step 1: Write the failing cases**

In `ntfytest/edges.go`, add `"time"` to the imports. Append to `cases`:

```go
		{
			name: "a notification inactive in the age cutoff's microsecond is kept",
			assert: func(t *testing.T, e *env) {
				n := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
				e.insert(false, n)

				read := at(0).Add(time.Hour)
				e.markRead("alice", read, n.ID)

				result := e.prune(ntfy.PruneRequest{Now: read.Add(time.Hour), MaxAge: time.Hour - 500*time.Nanosecond})
				assert.Zero(t, result.DeletedForAge, "the cutoff is compared at microseconds, where it equals the read time")
				assert.Len(t, e.all("alice"), 1)
			},
		},
		{
			name: "a close record changed in the watermark cutoff's microsecond is kept",
			assert: func(t *testing.T, e *env) {
				e.close(ntfy.CloseRequest{Subject: "task-1", Kinds: []string{"offer"}, Version: 5}, at(0))

				result := e.prune(ntfy.PruneRequest{
					Now: at(0).Add(days(1) + 500*time.Nanosecond), WatermarkRetention: days(1),
				})
				assert.Zero(t, result.WatermarksDeleted, "the cutoff is compared at microseconds, where it equals the close time")

				late := e.insert(false, e.note("alice", "event-1", "task-1", "offer", 1, at(1)))
				assert.Equal(t, 1, late.Suppressed, "the close record still suppresses")
			},
		},
```

- [ ] **Step 2: Run them and watch them fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/edges' -count=1 .`

Expected: FAIL. The watermark case reports 2, because a close by kind writes both the `offer` and the `*` record:

```
--- FAIL: TestMemoryStoreConformance/edges/a_notification_inactive_in_the_age_cutoff's_microsecond_is_kept
        Error:      	Should be zero, but was 1
        Messages:   	the cutoff is compared at microseconds, where it equals the read time
        Error:      	"[]" should have 1 item(s), but has 0
--- FAIL: TestMemoryStoreConformance/edges/a_close_record_changed_in_the_watermark_cutoff's_microsecond_is_kept
        Error:      	Should be zero, but was 2
        Messages:   	the cutoff is compared at microseconds, where it equals the close time
        Error:      	Not equal:
                    	expected: 1
                    	actual  : 0
        Messages:   	the close record still suppresses
```

Then run `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/edges' -count=1 .`. Expected: PASS.

- [ ] **Step 3: Truncate both cutoffs**

In `memory.go`, in `Prune`, change:

```go
		cutoff := req.Now.Add(-req.MaxAge)
```

to

```go
		cutoff := normalizeTime(req.Now.Add(-req.MaxAge))
```

and

```go
		s.pruneWatermarks(req.Now.Add(-req.WatermarkRetention), &result)
```

to

```go
		s.pruneWatermarks(normalizeTime(req.Now.Add(-req.WatermarkRetention)), &result)
```

- [ ] **Step 4: Run the memory package**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ntfytest/edges.go memory.go
git commit -m "Compare the memory store's retention cutoffs at microseconds

Every SQL dialect truncates the age and watermark cutoffs to the precision
it stores; the memory store compared them at nanoseconds, so a row inside
the cutoff's own microsecond was deleted there and kept everywhere else.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 4: Pin the `EmailStore` edges (tasks 4.1, 4.2)

**Files:**
- Create: `ntfytest/email_edges.go`
- Modify: `ntfytest/email.go:36-46`, `memory_email.go`

**Interfaces:**
- Consumes (existing, in `ntfytest`):
  - `type EmailFactory func(t *testing.T) EmailStore`;
  - `newEmailEnv(t *testing.T, factory EmailFactory) *emailEnv`, which embeds `*env` and has the field `email EmailStore`;
  - on `*emailEnv`: `published(recipient string, age time.Duration) ntfy.Notification`, `claimAt(owner string, now time.Time, limit int) []ntfy.EmailCandidate`, `claim(owner string) []ntfy.EmailCandidate`, `record(record ntfy.EmailRecord) int64`;
  - `emailNow`, `minutes(n int) time.Duration`.
- Produces:
  - `func runEmailEdges(t *testing.T, factory EmailFactory)`, registered in `RunEmail`;
  - `MemoryStore.RecordEmails` changes only a delivery with a lease, held by the record's owner;
  - `MemoryStore.ClaimEmails` and `PurgeEmailRecords` return at once for a limit ≤ 0.

- [ ] **Step 1: Write the empty-owner cases (task 4.1)**

Create `ntfytest/email_edges.go`:

```go
package ntfytest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// runEmailEdges holds every store to one behaviour on email inputs the
// dispatcher never sends: a limit that is not positive, and the empty owner.
// The cases do not vary context, so the table has no ctx field.
func runEmailEdges(t *testing.T, factory EmailFactory) {
	type testCase struct {
		name   string
		assert func(t *testing.T, e *emailEnv)
	}

	cases := []testCase{
		{
			name: "a record under the empty owner does not change a released delivery",
			assert: func(t *testing.T, e *emailEnv) {
				n := e.published("alice", minutes(10))

				require.Len(t, e.claim("owner-a"), 1)
				require.EqualValues(t, 1, e.record(ntfy.EmailRecord{Owner: "owner-a", IDs: []string{n.ID}, Status: ntfy.EmailStatusSent}))

				assert.Zero(t, e.record(ntfy.EmailRecord{Owner: "", IDs: []string{n.ID}, Status: ntfy.EmailStatusClaimed}),
					"a released delivery is held by no owner, the empty one included")
				assert.Empty(t, e.claimAt("owner-b", emailNow.Add(time.Hour), 100), "the delivery stays sent")
			},
		},
		{
			name: "a claim under the empty owner can be recorded by it",
			assert: func(t *testing.T, e *emailEnv) {
				n := e.published("alice", minutes(10))

				require.Len(t, e.claimAt("", emailNow, 100), 1)

				assert.EqualValues(t, 1, e.record(ntfy.EmailRecord{Owner: "", IDs: []string{n.ID}, Status: ntfy.EmailStatusSent}),
					"the empty owner holds what it claimed")
				assert.Empty(t, e.claimAt("owner-b", emailNow.Add(time.Hour), 100), "the delivery is sent")
			},
		},
	}

	for _, tc := range cases {
		parallel(t, tc.name, func(t *testing.T) {
			tc.assert(t, newEmailEnv(t, factory))
		})
	}
}
```

In `ntfytest/email.go`, add the last line of `RunEmail`:

```go
	t.Run("concurrency", func(t *testing.T) { runEmailConcurrency(t, factory) })
	t.Run("edges", func(t *testing.T) { runEmailEdges(t, factory) })
}
```

- [ ] **Step 2: Run them and watch the first fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreEmailConformance/edges' -count=1 .`

Expected: FAIL on the first case only; the guard passes.

```
--- FAIL: TestMemoryStoreEmailConformance/edges/a_record_under_the_empty_owner_does_not_change_a_released_delivery
        Error:      	Should be zero, but was 1
        Messages:   	a released delivery is held by no owner, the empty one included
        Error:      	Should be empty, but was [{{… alice … offer ACTIVE …} CLAIMED  0}]
        Messages:   	the delivery stays sent
```

Then run `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/email/edges' -count=1 .`. Expected: PASS.

- [ ] **Step 3: Change only held deliveries**

In `memory_email.go`, in `RecordEmails`, change:

```go
		if !ok || delivery.owner != record.Owner {
```

to

```go
		// A released delivery has no lease and is held by no owner, the empty
		// one included, as a NULL owner matches nothing in SQL.
		if !ok || delivery.leaseUntil == nil || delivery.owner != record.Owner {
```

A claim always sets the lease, and every status but SENDING clears the lease with the owner. So a lease on memory is exactly a non-NULL owner on SQL.

- [ ] **Step 4: Run the email conformance**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreEmailConformance' -count=1 .`

Expected: PASS, the guard included. If the guard fails, the check wrongly refuses a claim under the empty owner.

- [ ] **Step 5: Commit**

```bash
git add ntfytest/email_edges.go ntfytest/email.go memory_email.go
git commit -m "Change only held email deliveries on the memory store

A record under the empty owner matched every released delivery in memory,
where SQL's NULL owner matches nothing.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

- [ ] **Step 6: Write the limit cases (task 4.2)**

In `runEmailEdges`, before `cases := []testCase{`, add:

```go
	noLimit := func(limit int) func(t *testing.T, e *emailEnv) {
		return func(t *testing.T, e *emailEnv) {
			n := e.published("alice", minutes(10))

			assert.Emptyf(t, e.claimAt("owner-a", emailNow, limit), "a claim with limit %d claims nothing", limit)

			claimed := e.claimAt("owner-b", emailNow, 100)
			require.Len(t, claimed, 1, "the claim with no positive limit took no lease")
			assert.Equal(t, n.ID, claimed[0].Notification.ID)
		}
	}
```

and append to `cases`:

```go
		{name: "a claim with a limit of zero claims nothing", assert: noLimit(0)},
		{name: "a claim with a negative limit claims nothing", assert: noLimit(-1)},
		{
			name: "a purge with a limit of zero deletes nothing",
			assert: func(t *testing.T, e *emailEnv) {
				n := e.published("alice", minutes(10))

				require.Len(t, e.claim("owner-a"), 1)
				require.EqualValues(t, 1, e.record(ntfy.EmailRecord{Owner: "owner-a", IDs: []string{n.ID}, Status: ntfy.EmailStatusSent}))

				e.markRead("alice", emailNow, n.ID)
				require.EqualValues(t, 1, e.prune(ntfy.PruneRequest{Now: emailNow.Add(days(2)), MaxAge: days(1)}).DeletedForAge)

				purged, err := e.email.PurgeEmailRecords(t.Context(), 0)
				require.NoError(t, err)
				assert.Zero(t, purged, "a purge with limit 0 deletes nothing")

				purged, err = e.email.PurgeEmailRecords(t.Context(), 100)
				require.NoError(t, err)
				assert.EqualValues(t, 1, purged, "the orphaned record is still there to purge")
			},
		},
```

- [ ] **Step 7: Run them and watch all three fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreEmailConformance/edges' -count=1 .`

Expected: FAIL.

```
--- FAIL: TestMemoryStoreEmailConformance/edges/a_claim_with_a_limit_of_zero_claims_nothing
        Error:      	Should be empty, but was [{{… alice … offer ACTIVE …} CLAIMED  0}]
        Messages:   	a claim with limit 0 claims nothing
        Error:      	"[]" should have 1 item(s), but has 0
        Messages:   	the claim with no positive limit took no lease
--- FAIL: TestMemoryStoreEmailConformance/edges/a_claim_with_a_negative_limit_claims_nothing
        Messages:   	a claim with limit -1 claims nothing
--- FAIL: TestMemoryStoreEmailConformance/edges/a_purge_with_a_limit_of_zero_deletes_nothing
        Error:      	Should be zero, but was 1
        Messages:   	a purge with limit 0 deletes nothing
        Error:      	Not equal:
                    	expected: int(1)
                    	actual  : int64(0)
        Messages:   	the orphaned record is still there to purge
```

- [ ] **Step 8: Do nothing for a limit ≤ 0**

In `memory_email.go`, make `ClaimEmails` begin:

```go
func (s *MemoryStore) ClaimEmails(_ context.Context, claim EmailClaim) ([]EmailCandidate, error) {
	if claim.Limit <= 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
```

and change its truncation from `if claim.Limit > 0 && len(due) > claim.Limit {` to:

```go
	if len(due) > claim.Limit {
```

Make `PurgeEmailRecords` begin:

```go
func (s *MemoryStore) PurgeEmailRecords(_ context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
```

and change its truncation from `if limit > 0 && len(orphans) > limit {` to:

```go
	if len(orphans) > limit {
```

- [ ] **Step 9: Run memory and SQL email conformance**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -run 'TestMemoryStoreEmailConformance' -count=1 .`

Then run `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQL(MySQL|Postgres|SQLite)/email/edges' -count=1 .`.

Expected: PASS on both.

- [ ] **Step 10: Commit**

```bash
git add ntfytest/email_edges.go memory_email.go
git commit -m "Claim and purge nothing on the memory store without a positive limit

SQL returns at once for a limit of zero or less; the memory store claimed
or purged everything, taking a lease on a whole backlog.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 5: Document the edges (task 5.1)

**Files:**
- Modify: `store.go` (`Store.Insert`, `Store.Close`, `Store.Prune`), `email.go` (`EmailStore` methods, `EmailClaim.Limit`), `id.go` (`IDGenerator.NewID`), `ntfytest/doc.go`

**Interfaces:**
- Consumes: the behaviour of Tasks 1–4.
- Produces: godoc only; no signature changes.

- [ ] **Step 1: `store.go`**

Replace the comments on three `Store` methods:

```go
	// Insert publishes notifications that all share one subject, applying, in
	// the subject's serialised write: watermark suppression, coalescing, and
	// idempotency on (SourceID, Recipient). A notification it would write under
	// an identifier already stored, or twice in one call, fails the insert with
	// an error, and nothing is written. An insert carrying any notification
	// begins the subject's close record, whether or not it creates one.
	Insert(ctx context.Context, subject string, insertions []Insertion) (InsertResult, error)
	// Close closes a subject's notifications by kind and version, raises the
	// subject's watermark, and publishes req.Successor to the recipients it
	// closed, all in one transaction. ids stamps each successor. A successor
	// that cannot be stamped, or whose identifier is already stored, fails the
	// close, which then changes nothing.
	Close(ctx context.Context, req CloseRequest, at time.Time, ids IDGenerator) (CloseResult, error)
```

```go
	// Prune applies retention bounds and expires watermarks. Both cutoffs are
	// compared at microseconds, the precision every store keeps.
	Prune(ctx context.Context, req PruneRequest) (PruneResult, error)
```

- [ ] **Step 2: `email.go`**

```go
	// ClaimEmails takes a lease on notifications due for email and returns them.
	// A notification another claim holds is never returned, so two dispatchers
	// never receive the same one. A limit of zero or less claims nothing.
	ClaimEmails(ctx context.Context, claim EmailClaim) ([]EmailCandidate, error)
	// RecordEmails writes an outcome for the claim's notifications, and returns
	// how many it changed. A notification whose lease the owner no longer holds
	// is not changed. A released notification is held by no owner, the empty
	// one included.
	RecordEmails(ctx context.Context, record EmailRecord) (int64, error)
	// PurgeEmailRecords deletes up to limit delivery records whose notification
	// no longer exists, and returns how many it deleted. A limit of zero or less
	// deletes nothing.
	PurgeEmailRecords(ctx context.Context, limit int) (int64, error)
```

```go
	// Limit caps how many notifications one claim returns. Zero or less claims
	// nothing.
	Limit int
```

- [ ] **Step 3: `id.go`**

```go
	// NewID returns a new identifier, unique among every notification, of 1 to
	// [MaxIDBytes] bytes. The service refuses any other with a
	// [ConfigurationError]. An identifier that repeats one already stored fails
	// the publish or close that received it; no stored notification is
	// replaced.
	NewID() (string, error)
```

- [ ] **Step 4: `ntfytest/doc.go`**

Add a paragraph before `package ntfytest`:

```go
//
// It also asserts the edges of the store contract that the service rarely
// reaches: a reused notification identifier fails the write and changes
// nothing, an insert that creates nothing still begins its subject's close
// record, retention cutoffs are compared at microseconds, and an email claim or
// purge without a positive limit, or a record under an owner that holds
// nothing, changes nothing.
```

- [ ] **Step 5: Check the rendered docs and lint**

Run:

```sh
GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy.Store
GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy.EmailStore
GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy.IDGenerator
GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy/ntfytest
make lint
```

Expected: the new sentences appear, and `0 issues.` for every module.

- [ ] **Step 6: Commit**

```bash
git add store.go email.go id.go ntfytest/doc.go
git commit -m "Document the store contract's edges

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 6: Verify (task 6.1)

**Files:**
- Modify: `openspec/changes/align-memory-store-with-sql/plans.md` (execution record), `tasks.md` (checkboxes)

**Interfaces:** none.

- [ ] **Step 1: Run the gates**

```sh
make all
make store-matrix
GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./...
(cd ntfytest && GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./...)
make sqlkit-copy-check
```

Expected:
- `make all`: lint `0 issues.` on every module, split-check silent, every test `ok`;
- `make store-matrix`: `ok` on all seven combinations;
- the race runs: `ok`;
- `sqlkit-copy-check`: passes, since `pkg/sqlkit` is untouched.

A Redis container test can fail once on a refused dial while containers stop. That is unrelated. Re-run `make all` once; if it fails again, use `superpowers:systematic-debugging`.

- [ ] **Step 2: Record and commit**

Append the results (date, commands, `ok` lines) to the execution record below, tick `tasks.md`, and commit:

```bash
git add openspec/changes/align-memory-store-with-sql/plans.md openspec/changes/align-memory-store-with-sql/tasks.md
git commit -m "Record verification of align-memory-store-with-sql

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

## Execution record

- **2026-09-28, while proposing:** every red step above was run against `main` at `20967b3`, with the cases added in one pass. The quoted lines are from that run. The code in Tasks 1–4 was then applied and then reverted. With it applied:
  - `go test -race -count=1 .` passed;
  - `make all` passed (after one unrelated Redis retry);
  - `make store-matrix` passed on all seven combinations.

  One difference from the plan's order: in that single pass, `ensureWatermark` had already moved (Task 2) when the index row ran. The staged order was reasoned, not run: in Task 1, `dave`'s close record survives the refusal. So the row's close-record assertion is added in Task 2 Step 1, where it is the red line quoted there.
