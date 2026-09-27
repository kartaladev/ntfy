# Memory Store Recipient Index Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `MemoryStore` reads cost what a recipient holds rather than what the store holds, proved by a measurement that fails first and stays as the regression guard.

**Architecture:** Add one unexported index, `recipients map[string]map[string]struct{}`, maintained in `put` and `remove` — the two functions that already maintain the `sources` and `subjects` indexes — and read it in `CountActive`, `List`, `MarkAllRead` and `pruneCount`. Nothing else changes: no exported API, no option, no ordering, no paging semantics. The shared conformance suite passing untouched is the proof that behaviour is identical.

**Tech Stack:** Go 1.26 (standard library only in the core module), `testify` for assertions, `goleak` via `TestMain`, `ntfytest` for conformance, `make` for lint/test orchestration.

**Spec:** `openspec/changes/speed-up-memory-store-reads/proposal.md` and `openspec/changes/speed-up-memory-store-reads/design.md`. There is **no spec delta**: `.openspec.yaml` sets `skip_specs: true` because every requirement in `notification-inbox` describes what a store returns, never how fast, and putting a performance number into the contract would bind every future store implementation to meet it (proposal — Capabilities).

## Global Constraints

- **Go 1.26; export `GOTOOLCHAIN=go1.26.8` before any Go command** — Go 1.27 may be first on `PATH`, and tools built for 1.26 crash under it. `make` targets already pin it.
- **The core module imports only the standard library** in production code; tests may use `testify`, `goleak` and `go.uber.org/mock` only. Enforced by `.golangci.yml` depguard and `make split-check`.
- **Table-driven tests follow the project `table-test` skill**: `assert` closure form (never `want`/`wantErr` struct fields), a `ctx` modifier only where context matters, `t.Context()` over `context.Background()`.
- **The conformance suites must pass unchanged**: `ntfytest.Run`, `ntfytest.RunEmail`, `ntfytest.RunEmailDispatch`. Editing a suite case means the change altered behaviour and is wrong. (Note: `tasks.md` calls the store suite `ntfytest.RunStore`; the real exported name is `ntfytest.Run` — see `memory_test.go:13`.)
- **`go test -race ./...` must be clean**, and `goleak.VerifyTestMain` in `main_test.go` fails the package on any leaked goroutine.
- **`make all` must pass** (lint, split-check, test across every module).
- **`.claude/rules/prove-errors-with-tests.md`**: the performance claim is proved by a measurement that FAILS a stated threshold before any fix, and the failing output is recorded.
- **`.claude/rules/library-design.md`** rule 4: limits are stated, never silently relaxed — the doc comment gains what remains true after the fix.
- **No behaviour change**: no exported API, no option, no ordering or paging change, no `Notification` field.
- **Commit style**: imperative sentence case, no `feat:`/`fix:` prefixes, no attribution lines (`git log --format='%s'` for precedent).

---

## File Structure

| File | Responsibility |
| --- | --- |
| `memory.go` (modify) | The index field and its initialisation; `put`/`remove` maintain it; `CountActive`, `List`, `MarkAllRead`, `pruneCount` read through it; the doc comment's stated limits. |
| `memory_bench_test.go` (create, `package ntfy_test`) | The measurement harness: the seeding helper, the opt-in switch, the reporting benchmarks, the timing helper, the scaling gate test and the prune allocation gate. |
| `.github/workflows/ci.yml` (modify) | A unit-job step that runs the gate tests without `-race`, with `NTFY_MEASURE_MEMORY=1`. |
| `memory_index_test.go` (create, `package ntfy`) | The index invariant: every stored notification is indexed under its own recipient, the index holds nothing else, and a reused identifier leaves no stale entry. Internal because the invariant is about unexported state. |
| `openspec/changes/speed-up-memory-store-reads/evidence.md` (create) | The recorded before/after measurements — the proof `.claude/rules/prove-errors-with-tests.md` requires, kept with the change rather than in a terminal scrollback. |

**Why `memory_bench_test.go` carries a `//go:build !race` constraint.** Under the race detector every wall-clock measurement is meaningless (roughly an order of magnitude slower, unevenly), and `make test-race` runs the whole suite. `-race` sets the `race` build tag, so `//go:build !race` excludes the file there. The gate tests are also opt-in through `NTFY_MEASURE_MEMORY`, because they seed 220,000 notifications each and time microsecond operations. CI's unit job runs `-race`, so it runs them in a step of its own, without `-race`, with the variable set; `make all` does not run them. The Makefile's note that integration tests deliberately carry no build tag is about provisioning containers, not about timing; state that rationale in the file's comment.

**Benchmark convention this plan establishes.** The repository has no benchmarks today (`rg 'func Benchmark'` finds none across all six modules), so this change sets the shape:

1. Measurement lives in `<area>_bench_test.go`, guarded `//go:build !race`.
2. A `seed<Area>(tb testing.TB, ...)` helper builds the fixture and takes `testing.TB` so tests and benchmarks share it.
3. `BenchmarkXxx` functions **report** numbers for humans and `benchstat`; they assert nothing.
4. An ordinary `TestXxx` **gates** the property, asserting a ratio between two fixture sizes, or an invariant such as zero allocations, rather than a wall-clock number, so it holds on any machine.
5. Gate tests that seed large fixtures are opt-in through an `NTFY_MEASURE_*` variable, and CI runs them in a step of their own.

The change `scale-email-claim-query` also adds measurement. This plan assumes the convention above; if that change lands first with a different one, follow theirs and note the deviation in `evidence.md`.

---

## Revision: corrected to the code as built

On 2026-09-27, after implementation and review, every code block below was
replaced with the code as committed, and the prose around each was corrected
to match. `evidence.md` records the measurements, and what the review changed.
In summary:

- The scaling gate does not call `t.Parallel()`. Each cost is the least of 5
  interleaved rounds, because a single mean flaked under full CPU load.
- The gates are opt-in through `NTFY_MEASURE_MEMORY` and run in their own
  non-race CI step. CI's unit job runs `-race`, which excludes this file.
- `put` removes any notification already stored under the same identifier, so
  a reused identifier cannot leave another recipient's index pointing at it.
- `put` and `remove` share the `indexAdd` and `indexRemove` helpers.
- The prune measurement uses the within-bound steady state and gates on zero
  allocations. `pruneCount` collects and sorts only the recipients over the
  bound.
- Task 7 did not run. The gate passed with the index, so `design.md` D5 stands.

---

## Task 1: Benchmark harness and recorded evidence of the cliff

**Files:**
- Create: `memory_bench_test.go`
- Create: `openspec/changes/speed-up-memory-store-reads/evidence.md`

**Interfaces:**
- Consumes: the exported store API only — `ntfy.NewMemoryStore() *ntfy.MemoryStore`, `(*ntfy.MemoryStore).Insert(ctx, subject string, insertions []ntfy.Insertion) (ntfy.InsertResult, error)`, `CountActive`, `List`, `MarkAllRead`, `Prune`, and `ntfy.NewUUIDv7Generator() *ntfy.UUIDv7Generator`.
- Produces: `func seedMemoryStore(tb testing.TB, notifications, recipients int) (*ntfy.MemoryStore, string)` — fills a store with `notifications` ACTIVE notifications spread evenly over `recipients`, and returns the store and the recipient the measurements read (`"user-0"`); `var seedStart time.Time` — the creation time of the first seeded notification.

- [ ] **Step 1: Create the file with its build constraint, seeding helper and fixture constant**

```go
//go:build !race

// Measurement for the memory store: benchmarks that report, and two gate tests
// that fail when a recipient's reads, or the count bound's pass, grow with the
// store's total size.
//
// Two gates keep it out of the default test run's way:
//
//   - The build constraint excludes the file under -race, which distorts
//     wall-clock timing beyond usefulness.
//   - The gate tests seed 220,000 notifications each and time microsecond
//     operations, so they run only when NTFY_MEASURE_MEMORY is set. CI's unit
//     job runs them in a step of their own, without -race:
//
//	NTFY_MEASURE_MEMORY=1 go test -count=1 -run 'TestMemoryStore(ReadsDoNotScale|PruneCountWithinBound)' .

package ntfy_test

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// The fixture sizes design.md D6 names: the recipient read holds 100
// notifications at both, well inside DefaultMaxPerRecipient = 500, so only the
// store's total size differs.
//
// Indicative figures, one machine only (Apple M4 Pro, go1.26.8, 2026-09-27),
// never thresholds; the gates below assert a ratio and an invariant. At 200k,
// before the recipient index and after it:
//
//	CountActive               5.48 ms  ->  5.8 µs
//	List (50 rows)            9.77 ms  ->   25 µs
//	MarkAllRead               5.78 ms  ->  2.2 µs
//	CountActive, 32 callers   ~180 ops/s  ->  ~185,000 ops/s
//	Prune, within the bound   130 MB, 34-54 ms  ->  0 B, 15 µs
//
// openspec/changes/speed-up-memory-store-reads/evidence.md has the runs.
const (
	smallNotifications, smallRecipients = 20_000, 200
	largeNotifications, largeRecipients = 200_000, 2_000
)

// benchmarkSizes pairs each fixture size with the name its sub-benchmark
// reports under, so every benchmark measures the same two points.
var benchmarkSizes = []struct {
	name                      string
	notifications, recipients int
}{
	{"20k", smallNotifications, smallRecipients},
	{"200k", largeNotifications, largeRecipients},
}

// measureEnv opts in to the gate tests. See the file comment.
const measureEnv = "NTFY_MEASURE_MEMORY"

// requireMeasurement skips a gate test unless measureEnv is set.
func requireMeasurement(t *testing.T) {
	t.Helper()

	if os.Getenv(measureEnv) == "" {
		t.Skipf("set %s=1 to run the memory store's measurement gates", measureEnv)
	}
}

// seedStart is when the first seeded notification was created. Later ones are
// one millisecond apart, so every creation time is distinct and ordering is
// exact.
var seedStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// seedMemoryStore fills a store with ACTIVE notifications spread evenly over
// recipients, one subject per recipient, and returns it with the recipient the
// measurements read. Insertions are batched per recipient so that seeding is
// the fixture's cost, not the measurement's.
func seedMemoryStore(tb testing.TB, notifications, recipients int) (store *ntfy.MemoryStore, read string) {
	tb.Helper()

	require.Zerof(tb, notifications%recipients,
		"fixture sizes must divide evenly: %d notifications over %d recipients", notifications, recipients)

	store = ntfy.NewMemoryStore()
	ids := ntfy.NewUUIDv7Generator()
	perRecipient := notifications / recipients

	for r := range recipients {
		recipient := "user-" + strconv.Itoa(r)
		subject := "task-" + recipient
		insertions := make([]ntfy.Insertion, 0, perRecipient)

		for i := range perRecipient {
			id, err := ids.NewID()
			require.NoError(tb, err)

			n := r*perRecipient + i

			insertions = append(insertions, ntfy.Insertion{Notification: ntfy.Notification{
				ID:        id,
				Recipient: recipient,
				SourceID:  "evt-" + strconv.Itoa(n),
				Subject:   subject,
				Kind:      "offer",
				State:     ntfy.StateActive,
				CreatedAt: seedStart.Add(time.Duration(n) * time.Millisecond),
			}})
		}

		result, err := store.Insert(tb.Context(), subject, insertions)
		require.NoError(tb, err)
		require.Len(tb, result.Created, perRecipient)
	}

	return store, "user-0"
}
```

- [ ] **Step 2: Add the three read benchmarks at both fixture sizes**

```go
func BenchmarkMemoryStoreCountActive(b *testing.B) {
	for _, size := range benchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			store, recipient := seedMemoryStore(b, size.notifications, size.recipients)
			ctx := b.Context()

			for b.Loop() {
				if _, err := store.CountActive(ctx, recipient); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMemoryStoreList(b *testing.B) {
	for _, size := range benchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			store, recipient := seedMemoryStore(b, size.notifications, size.recipients)
			ctx := b.Context()
			query := ntfy.ListQuery{Recipient: recipient, Limit: 50}

			for b.Loop() {
				if _, err := store.List(ctx, query); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMemoryStoreMarkAllRead(b *testing.B) {
	for _, size := range benchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			store, recipient := seedMemoryStore(b, size.notifications, size.recipients)
			ctx := b.Context()

			// through precedes every seeded notification, so the pass marks
			// nothing and every iteration measures the same scan.
			through := seedStart.Add(-time.Hour)
			at := seedStart.Add(time.Hour)

			for b.Loop() {
				if _, err := store.MarkAllRead(ctx, recipient, through, at); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
```

- [ ] **Step 3: Add the concurrent benchmark that exposes the single lock**

```go
// BenchmarkMemoryStoreCountActiveParallel measures whole-process throughput:
// the scan holds the one mutex, so adding goroutines cannot add throughput
// until the scan is gone. It runs at least 32 goroutines, the concurrency the
// audit measured, whatever GOMAXPROCS is, and reports ops/s for the whole
// process.
func BenchmarkMemoryStoreCountActiveParallel(b *testing.B) {
	const goroutines = 32

	store, recipient := seedMemoryStore(b, largeNotifications, largeRecipients)
	ctx := b.Context()

	b.SetParallelism((goroutines + runtime.GOMAXPROCS(0) - 1) / runtime.GOMAXPROCS(0))
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := store.CountActive(ctx, recipient); err != nil {
				b.Error(err)

				return
			}
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/s")
}
```

- [ ] **Step 4: Run the benchmarks and capture the output**

Run: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStore' -benchmem -benchtime 20x ./...`

Expected: all four benchmarks report, and per-operation cost tracks store size roughly tenfold — the audit measured `CountActive` 4.79 ms, `List` 7.60 ms and `MarkAllRead` 5.26 ms at 200k/2000, and the parallel benchmark achieved about 222 operations per second in total (roughly 4.5 ms/op) across 32 goroutines. Exact figures will differ by machine; the **shape** (200k roughly 10× the cost of 20k) is what matters. Keep the raw output for the next step.

- [ ] **Step 5: STOP GATE — decide whether the change proceeds**

This change rests on an unproven claim: an audit's measurement on another machine. If this run does **not** reproduce it, stop here. Do not implement the index.

The claim is refuted if both of these hold:
- For each of `CountActive`, `List` and `MarkAllRead`, the cost at 200k/2000 is within `design.md` D6's 2× gate of the cost at 20k/200. That means reads do not scale with store size.
- The parallel benchmark reports thousands of operations per second, not hundreds.

In that case, still write `evidence.md` (next step) with the numbers, under a heading "Does not reproduce". Commit only that file and the benchmark file, and report the change as parked. `.claude/rules/prove-errors-with-tests.md` forbids justifying the change on an unreproduced claim, and a threshold lowered until it fails proves nothing.

If the shape does reproduce, continue.

- [ ] **Step 6: Record the evidence**

Create `openspec/changes/speed-up-memory-store-reads/evidence.md`:

```markdown
# Evidence

Proof for `.claude/rules/prove-errors-with-tests.md`. Absolute numbers are one
machine's; the ratio between the two fixture sizes is the property under test.

## Before: reads scale with store size

Command: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStore' -benchmem -benchtime 20x ./...`

Machine: <goos/goarch/cpu lines from the benchmark output>

<paste the benchmark output here>

Cost at 200k/2000 divided by cost at 20k/200:

| Operation | 20k/200 | 200k/2000 | Ratio |
| --- | --- | --- | --- |
| CountActive | <ns/op> | <ns/op> | <ratio> |
| List | <ns/op> | <ns/op> | <ratio> |
| MarkAllRead | <ns/op> | <ns/op> | <ratio> |

Concurrent CountActive at 200k/2000: <ns/op>, i.e. <ops/sec> for the whole
process across all goroutines.

## After

<filled in by Task 6>
```

- [ ] **Step 7: Verify the file builds under both tag states**

Run: `GOTOOLCHAIN=go1.26.8 go vet ./... && GOTOOLCHAIN=go1.26.8 go vet -race ./...`
Expected: both clean. The second proves the `!race` constraint excludes the file rather than breaking the race build.

- [ ] **Step 8: Commit**

```bash
git add memory_bench_test.go openspec/changes/speed-up-memory-store-reads/evidence.md
git commit -m "Add memory store read benchmarks and record the scan cliff"
```

---

## Task 2: The recipient index, maintained in put and remove

**Files:**
- Create: `memory_index_test.go`
- Modify: `memory.go:22-34` (struct), `memory.go:60-68` (`NewMemoryStore`), `memory.go:115-126` (`put`), `memory.go:130-140` (`remove`)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: the unexported field `recipients map[string]map[string]struct{}` on `MemoryStore`, mapping a recipient to the set of that recipient's notification identifiers. `put(n Notification)` and `remove(n Notification)` keep their signatures; they are the only writers. Read paths land in Task 3.

- [ ] **Step 1: Write the invariant test**

Create `memory_index_test.go`. It is internal (`package ntfy`) because the invariant is about unexported state, and it runs the six operation shapes that touch membership. The last one reuses an identifier for another recipient. It stays red after Step 6 until `put` removes the notification it replaces, which is the code in Step 5.

```go
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
```

- [ ] **Step 2: Run the test to verify it fails to compile for the right reason**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreRecipientIndexMirrorsTheStore' -count=1 .`
Expected: FAIL — `s.recipients undefined (type *MemoryStore has no field or method recipients)`. This is the field not existing yet, not the invariant. The next step makes it compile and fail on the invariant instead, which is the real red.

- [ ] **Step 3: Add the field and initialise it, without maintaining it yet**

In `memory.go`, add to the `MemoryStore` struct after `subjects` (`memory.go:31`):

```go
	// recipients indexes notification identifiers by recipient, so that
	// listing, counting, marking all read and the count bound touch one
	// recipient's notifications rather than every notification. Membership
	// changes only in put and remove.
	recipients map[string]map[string]struct{}
```

and to `NewMemoryStore` (`memory.go:60-68`), after the `subjects` entry:

```go
		recipients:    make(map[string]map[string]struct{}),
```

- [ ] **Step 4: Run the test to verify it now fails on the invariant**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreRecipientIndexMirrorsTheStore' -count=1 .`
Expected: FAIL with `index holds 0 identifiers, the store holds 3 notifications` on every case. That is the intended red: the field exists and nothing maintains it.

- [ ] **Step 5: Maintain the index in `put`**

Replace `put` (`memory.go:114-126`) with:

```go
// put stores a notification and indexes it. A notification already stored
// under the same identifier is removed first, with its index entries, so that
// no index keeps pointing its recipient or subject at the replacement. The
// caller holds the lock.
func (s *MemoryStore) put(n Notification) {
	if old, ok := s.notifications[n.ID]; ok {
		s.remove(old)
	}

	s.notifications[n.ID] = n
	s.sources[sourceKey{source: n.SourceID, recipient: n.Recipient}] = n.ID
	indexAdd(s.subjects, n.Subject, n.ID)
	indexAdd(s.recipients, n.Recipient, n.ID)
}
```

- [ ] **Step 6: Maintain the index in `remove`**

Replace `remove` (`memory.go:128-140`) with the following, and add the two set-index helpers after it. `put` (Step 5) uses them too:

```go
// remove deletes a notification and its index entries. The caller holds the
// lock.
func (s *MemoryStore) remove(n Notification) {
	delete(s.notifications, n.ID)
	delete(s.sources, sourceKey{source: n.SourceID, recipient: n.Recipient})
	indexRemove(s.subjects, n.Subject, n.ID)
	indexRemove(s.recipients, n.Recipient, n.ID)
}

// indexAdd adds id to key's set in index, creating the set if key has none.
func indexAdd(index map[string]map[string]struct{}, key, id string) {
	ids := index[key]
	if ids == nil {
		ids = make(map[string]struct{})
		index[key] = ids
	}

	ids[id] = struct{}{}
}

// indexRemove removes id from key's set in index, dropping the set once it is
// empty so that an index never holds a key with nothing under it.
func indexRemove(index map[string]map[string]struct{}, key, id string) {
	ids := index[key]
	delete(ids, id)

	if len(ids) == 0 {
		delete(index, key)
	}
}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreRecipientIndexMirrorsTheStore' -count=1 -v .`
Expected: PASS, all six subtests.

- [ ] **Step 8: Verify design.md D2's load-bearing claim mechanically**

Run: `rg -n 's\.notifications\[' memory.go memory_email.go`

Expected: writes at exactly these sites — `put` (`s.notifications[n.ID] = n`), `remove` (`delete`), `Close` (`s.notifications[id] = n`), `MarkRead` (`s.notifications[id] = n`), `MarkAllRead` (`s.notifications[id] = n`); every other hit is a read. The three in-place replacements preserve `Recipient`, so none of them can invalidate the index.

**STOP** if a write exists that is not one of those five, or if any of the three replacements can change `Recipient`: the index would need maintaining there too, which contradicts `design.md` D2. Revise the design before continuing rather than adding a fourth maintenance site silently. Record the command's output in `evidence.md` under a `## D2 verification` heading either way.

- [ ] **Step 9: Run the conformance suite to verify nothing else changed**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStore' -count=1 ./...`
Expected: PASS — `TestMemoryStoreConformance` and `TestMemoryStoreEmailConformance` unchanged, with no edits to any suite case.

- [ ] **Step 10: Commit**

```bash
git add memory.go memory_index_test.go openspec/changes/speed-up-memory-store-reads/evidence.md
git commit -m "Index memory store notifications by recipient"
```

---

## Task 3: Read through the index

**Files:**
- Modify: `memory_bench_test.go` (add the timing helper and the gate test)
- Modify: `memory.go:262-301` (`List`), `memory.go:340-354` (`CountActive`), `memory.go:393-415` (`MarkAllRead`)

**Interfaces:**
- Consumes: `seedMemoryStore` and `seedStart` from Task 1; the `recipients` index from Task 2.
- Produces: `func timePerOp(tb testing.TB, iterations int, op func(tb testing.TB)) time.Duration` — the mean cost of one operation after a warm-up call; and `TestMemoryStoreReadsDoNotScaleWithStoreSize`, the gate every later task must keep green.

- [ ] **Step 1: Add the timing helper to `memory_bench_test.go`**

```go
// timePerOp returns the mean wall-clock cost of one op, after a warm-up call.
// It is deliberately not a benchmark: the gate asserts how cost scales with
// store size, which holds on any machine, so it belongs in an ordinary test
// that CI runs rather than behind -bench. A fixed iteration count keeps the
// test's own runtime predictable.
func timePerOp(tb testing.TB, iterations int, op func(tb testing.TB)) time.Duration {
	tb.Helper()

	op(tb)

	start := time.Now()

	for range iterations {
		op(tb)
	}

	return time.Since(start) / time.Duration(iterations)
}
```

- [ ] **Step 2: Add the scaling gate test, and `github.com/stretchr/testify/assert` to the file's imports** (Task 1 left it out, because nothing used it until now)

```go
// TestMemoryStoreReadsDoNotScaleWithStoreSize is the acceptance gate from
// design.md D6. The recipient holds 100 notifications at both fixture sizes,
// so a read whose cost tracks the store's total size is the defect. The gate
// is a ratio, not a wall-clock number, because a shared CI runner will not
// reproduce anyone's absolute figures.
//
// It deliberately does not call t.Parallel: a sequential test runs while the
// package's parallel tests are paused, so it does not measure their load.
// Each cost is the least of several rounds, interleaved between the two
// sizes, because interference from the rest of the machine only ever adds
// time.
func TestMemoryStoreReadsDoNotScaleWithStoreSize(t *testing.T) {
	requireMeasurement(t)

	const (
		rounds     = 5
		iterations = 200
		ratioLimit = 2.0
	)

	small, smallRecipient := seedMemoryStore(t, smallNotifications, smallRecipients)
	large, largeRecipient := seedMemoryStore(t, largeNotifications, largeRecipients)

	type testCase struct {
		name   string
		op     func(store *ntfy.MemoryStore, recipient string) func(tb testing.TB)
		assert func(t *testing.T, ratio float64, smallCost, largeCost time.Duration)
	}

	cases := []testCase{
		{
			name: "CountActive",
			op: func(store *ntfy.MemoryStore, recipient string) func(tb testing.TB) {
				return func(tb testing.TB) {
					if _, err := store.CountActive(tb.Context(), recipient); err != nil {
						tb.Fatal(err)
					}
				}
			},
			assert: func(t *testing.T, ratio float64, smallCost, largeCost time.Duration) {
				assert.LessOrEqualf(t, ratio, ratioLimit,
					"CountActive cost grew with store size: %s at 20k, %s at 200k (ratio %.1f)",
					smallCost, largeCost, ratio)
			},
		},
		{
			name: "List",
			op: func(store *ntfy.MemoryStore, recipient string) func(tb testing.TB) {
				query := ntfy.ListQuery{Recipient: recipient, Limit: 50}

				return func(tb testing.TB) {
					if _, err := store.List(tb.Context(), query); err != nil {
						tb.Fatal(err)
					}
				}
			},
			assert: func(t *testing.T, ratio float64, smallCost, largeCost time.Duration) {
				assert.LessOrEqualf(t, ratio, ratioLimit,
					"List cost grew with store size: %s at 20k, %s at 200k (ratio %.1f)",
					smallCost, largeCost, ratio)
			},
		},
		{
			name: "MarkAllRead",
			op: func(store *ntfy.MemoryStore, recipient string) func(tb testing.TB) {
				// through precedes every seeded notification, so nothing is
				// marked and each call measures the same scan.
				through := seedStart.Add(-time.Hour)
				at := seedStart.Add(time.Hour)

				return func(tb testing.TB) {
					if _, err := store.MarkAllRead(tb.Context(), recipient, through, at); err != nil {
						tb.Fatal(err)
					}
				}
			},
			assert: func(t *testing.T, ratio float64, smallCost, largeCost time.Duration) {
				assert.LessOrEqualf(t, ratio, ratioLimit,
					"MarkAllRead cost grew with store size: %s at 20k, %s at 200k (ratio %.1f)",
					smallCost, largeCost, ratio)
			},
		},
	}

	for _, tc := range cases {
		// Deliberately not t.Parallel(): all three operations take the same
		// store mutex, so running them at once would have each measuring the
		// others' contention. The ratio would survive it; the figures in the
		// failure message would not.
		t.Run(tc.name, func(t *testing.T) {
			smallOp, largeOp := tc.op(small, smallRecipient), tc.op(large, largeRecipient)
			smallCost, largeCost := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)

			for range rounds {
				smallCost = min(smallCost, timePerOp(t, iterations, smallOp))
				largeCost = min(largeCost, timePerOp(t, iterations, largeOp))
			}

			tc.assert(t, float64(largeCost)/float64(smallCost), smallCost, largeCost)
		})
	}
}
```

- [ ] **Step 3: Run the gate to verify it fails on the ratio**

Run: `NTFY_MEASURE_MEMORY=1 GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreReadsDoNotScaleWithStoreSize' -count=1 -v .`
Expected: FAIL on all three subtests, with messages of the shape `CountActive cost grew with store size: 480µs at 20k, 4.8ms at 200k (ratio 10.0)`. Confirm the failure is the ratio assertion, not a seeding error. **If the gate passes** against the code as it stands, stop. The cliff does not reproduce on this machine even though Task 1's benchmarks suggested it did. Record both runs in `evidence.md` and report the change as parked rather than continuing. This run takes a few seconds and allocates roughly 150 MB for the two fixtures — expected, and it drops to milliseconds once the index is read.

- [ ] **Step 4: Read `CountActive` through the index**

Replace `CountActive` (`memory.go:340-354`) with:

```go
// CountActive implements [Store].
func (s *MemoryStore) CountActive(_ context.Context, recipient string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int64

	for id := range s.recipients[recipient] {
		if s.notifications[id].State == StateActive {
			count++
		}
	}

	return count, nil
}
```

A recipient with nothing stored has no index entry; ranging a nil map yields nothing, so the zero answer is unchanged.

- [ ] **Step 5: Read `List` through the index**

In `List` (`memory.go:262-301`), replace only the matching loop (`memory.go:271-283`) with:

```go
	var matched []Notification

	for id := range s.recipients[q.Recipient] {
		n := s.notifications[id]

		if !matches(n, q) {
			continue
		}

		if continued && !before(n, position) {
			continue
		}

		matched = append(matched, n)
	}
```

Everything after it is untouched: `slices.SortFunc(matched, newestFirst)`, the limit, `EncodeCursor` and the `Clone` loop. `matches` keeps its `n.Recipient != q.Recipient` case — it is now redundant, and it stays because `matches` is a free function that must remain correct on its own terms.

- [ ] **Step 6: Read `MarkAllRead` through the index**

Replace the loop in `MarkAllRead` (`memory.go:402-412`) with:

```go
	for id := range s.recipients[recipient] {
		n := s.notifications[id]

		if n.State != StateActive || n.CreatedAt.After(through) {
			continue
		}

		n.State = StateRead
		n.ReadAt = timePtr(at)
		n.InactiveAt = timePtr(at)
		s.notifications[id] = n
		result.Marked++
	}
```

The recipient check is now the index lookup. Writing `s.notifications` while ranging `s.recipients[recipient]` is safe: they are different maps, and membership does not change here.

- [ ] **Step 7: Run the gate to verify it passes**

Run: `NTFY_MEASURE_MEMORY=1 GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreReadsDoNotScaleWithStoreSize' -count=1 -v .`
Expected: PASS, all three subtests, with both costs in the same order of magnitude (ratio near 1.0).

**If the gate still fails**, stop and go to Task 7 rather than widening `ratioLimit`. A threshold tuned until it passes proves nothing.

- [ ] **Step 8: Run the conformance suite**

Run: `GOTOOLCHAIN=go1.26.8 go test -count=1 ./...`
Expected: PASS, including `TestMemoryStoreConformance`'s listing, paging ("Newest first with exact paging"), counting and mark-all cases, with no suite case edited.

- [ ] **Step 9: Commit**

```bash
git add memory.go memory_bench_test.go
git commit -m "Read the memory store through its recipient index"
```

---

## Task 4: Prune the count bound without copying the whole store

**Files:**
- Modify: `memory.go:446-473` (`pruneCount`)
- Modify: `memory_bench_test.go` (add the prune benchmark and its allocation gate)

**Interfaces:**
- Consumes: the `recipients` index from Task 2; `seedMemoryStore` from Task 1.
- Produces: `func withinBound() ntfy.PruneRequest`, `BenchmarkMemoryStorePruneCount` and `TestMemoryStorePruneCountWithinBoundAllocatesNothing`. `pruneCount(req PruneRequest, result *PruneResult)` keeps its signature; `evict` and the strategy logic are untouched.

- [ ] **Step 1: Add the prune benchmark**

```go
// withinBound is a count-bound pass in which no seeded recipient exceeds the
// bound, the steady state of a host running the pruner with the default. It
// removes nothing, so one seeded store serves every call.
func withinBound() ntfy.PruneRequest {
	return ntfy.PruneRequest{
		Now: seedStart.Add(48 * time.Hour), MaxPerRecipient: ntfy.DefaultMaxPerRecipient, Strategy: ntfy.EvictOldestActive,
	}
}

// BenchmarkMemoryStorePruneCount measures the count bound's pass when every
// recipient is within it, so B/op and allocs/op are what the pass costs merely
// to find that out.
func BenchmarkMemoryStorePruneCount(b *testing.B) {
	for _, size := range benchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			store, _ := seedMemoryStore(b, size.notifications, size.recipients)
			ctx := b.Context()
			req := withinBound()

			for b.Loop() {
				if _, err := store.Prune(ctx, req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestMemoryStorePruneCountWithinBoundAllocatesNothing gates the count bound's
// pass on allocations rather than time: a pass that finds every recipient
// within the bound, the steady state, must not allocate at all, at either
// store size. Zero allocations is zero bytes, so it also catches a regression
// that adds a single allocation sized to the whole store, which a ratio of
// allocation counts would not. Allocation counts do not depend on the
// machine's load.
func TestMemoryStorePruneCountWithinBoundAllocatesNothing(t *testing.T) {
	requireMeasurement(t)

	const runs = 5

	for _, size := range benchmarkSizes {
		t.Run(size.name, func(t *testing.T) {
			store, _ := seedMemoryStore(t, size.notifications, size.recipients)
			req := withinBound()

			var (
				result ntfy.PruneResult
				err    error
			)

			allocs := testing.AllocsPerRun(runs, func() {
				result, err = store.Prune(t.Context(), req)
			})

			require.NoError(t, err)
			require.Zero(t, result.DeletedForCount+result.EvictedActive, "the fixture must stay within the bound")
			assert.Zerof(t, allocs, "a within-bound count pass allocated %.0f times at %s", allocs, size.name)
		})
	}
}
```

- [ ] **Step 2: Run it and record the allocation figure**

Run: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStorePruneCount' -benchmem -benchtime 10x .`
Also run the gate: `NTFY_MEASURE_MEMORY=1 GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStorePruneCountWithinBoundAllocatesNothing' -count=1 -v .`

Expected: the benchmark reports B/op in the megabytes, because the pass copies every `Notification` into `byRecipient` before any per-recipient work. The gate FAILS at both sizes, on the zero-allocation assertion. The threshold is zero allocations for a pass that finds everyone within the bound. It is stated here, before measuring, and zero allocations means zero bytes. Note the figures; Step 5 compares against them.

- [ ] **Step 3: Rewrite `pruneCount` to iterate the index**

Replace `pruneCount` (`memory.go:446-473`) with:

```go
// pruneCount brings each recipient within the count bound, inactive
// notifications first. The caller holds the lock.
func (s *MemoryStore) pruneCount(req PruneRequest, result *PruneResult) {
	// Only recipients over the bound are collected, so a pass that finds
	// everyone within it, the steady state, allocates nothing. Collecting
	// before evicting also keeps eviction from disturbing the iteration, and
	// sorting keeps Recipients in a stable order.
	var over []string

	for recipient, ids := range s.recipients {
		if len(ids) > req.MaxPerRecipient {
			over = append(over, recipient)
		}
	}

	slices.Sort(over)

	for _, recipient := range over {
		ids := s.recipients[recipient]
		excess := len(ids) - req.MaxPerRecipient

		held := make([]Notification, 0, len(ids))
		for id := range ids {
			held = append(held, s.notifications[id])
		}

		slices.SortFunc(held, oldestFirst)

		excess -= s.evict(held, excess, false, &result.DeletedForCount)

		if req.Strategy != EvictOldestActive || excess == 0 {
			continue
		}

		s.evict(held, excess, true, &result.EvictedActive)
		result.Recipients = append(result.Recipients, recipient)
	}
}
```

Two differences from the old body, both deliberate. Only recipients over the bound are collected and sorted, so a pass that finds everyone within it allocates nothing. Only one recipient's notifications are materialised at a time. `PruneResult.Recipients` keeps its sorted order. `evict` still walks the `held` slice it was given, so removing entries from `ids` during eviction cannot disturb it.

- [ ] **Step 4: Run the prune and retention tests**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestPrune|TestMemoryStore' -count=1 ./...`
Expected: PASS — the count bound, the `RetainActive` and `EvictOldestActive` strategies, and the pruner's reported `Recipients` all unchanged.

- [ ] **Step 5: Re-run the benchmark and verify the allocation is gone**

Run: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStorePruneCount' -benchmem -benchtime 10x .`
Expected: 0 B/op and 0 allocs/op at both sizes, and the gate from Step 2 passes. Record both figures in `evidence.md` under `## Prune count bound`.

- [ ] **Step 6: Commit**

```bash
git add memory.go memory_bench_test.go openspec/changes/speed-up-memory-store-reads/evidence.md
git commit -m "Prune the memory store count bound without copying every notification"
```

---

## Task 5: State what remains in the doc comment

**Files:**
- Modify: `memory.go:15-21` (the `MemoryStore` doc comment)

**Interfaces:**
- Consumes: the finished behaviour from Tasks 2-4.
- Produces: nothing in code. This is the `library-design.md` rule 4 obligation: the limits that survive are written down.

- [ ] **Step 1: Replace the doc comment**

```go
// MemoryStore is the default [Store]: notifications held in process memory.
//
// It suits tests and a single-process host that can afford to lose its
// notifications on restart. It keeps every guarantee of the Store contract, and
// passes the same conformance suite as the SQL store. A MemoryStore is safe for
// concurrent use; every method runs under one lock, which is the whole of its
// serialisation.
//
// Listing a recipient's notifications, counting them and marking them all read
// cost what that recipient holds, because notifications are indexed by
// recipient. The periodic passes are not scoped to a recipient: pruning by age
// and claiming emails visit every notification in the store, and the count
// bound visits every recipient. The host drives them on an interval, and each
// holds the lock while it runs, so their cost grows with the whole store.
//
// Nothing here survives a restart, and nothing is shared between processes. A
// host that needs either uses ntfy/sqlstore.
```

- [ ] **Step 2: Verify the rendered documentation**

Run: `GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy.MemoryStore`
Expected: the paragraphs render in order, and nothing claims unqualified parity with the SQL store.

- [ ] **Step 3: Verify the claim is true of the code**

Read `Prune` (`memory.go:418-444`) and `ClaimEmails` (`memory_email.go:25-66`) against the paragraph. Both must still range the whole `notifications` map — they do, deliberately (`design.md` D4). If either was changed by an earlier task, the comment is now wrong and one of them must be corrected.

- [ ] **Step 4: Confirm the markdown doc test is unaffected**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .`
Expected: PASS. It checks that `docs/notifications.md` names the exported functions (`docs_test.go:72` includes `NewMemoryStore`); it does not read godoc, so a doc-comment change cannot break it and `docs/notifications.md` needs no edit.

- [ ] **Step 5: Commit**

```bash
git add memory.go
git commit -m "State the memory store's remaining limits"
```

---

## Task 6: Verify the whole change and record the after-numbers

**Files:**
- Modify: `openspec/changes/speed-up-memory-store-reads/evidence.md`

**Interfaces:**
- Consumes: everything above.
- Produces: the completed evidence record; a green `make all`.

- [ ] **Step 1: Re-run the read benchmarks**

Run: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStore' -benchmem -benchtime 20x ./...`
Expected: per-operation cost roughly equal at both fixture sizes, and the parallel benchmark's throughput far above the ~222 ops/sec recorded in Task 1.

- [ ] **Step 2: Fill in the `## After` section of `evidence.md`**

Paste the output and the recomputed ratio table beside the before-figures. Add one line, verbatim:

```markdown
Absolute figures are indicative of one machine. The gate is
`TestMemoryStoreReadsDoNotScaleWithStoreSize`, which asserts the ratio only.
```

- [ ] **Step 3: Run the full conformance suites with no suite edits**

Run: `GOTOOLCHAIN=go1.26.8 go test -count=1 ./... && git diff --stat ntfytest/`
Expected: tests PASS, and `git diff` reports **no changes** under `ntfytest/`. Any suite edit means the index changed behaviour and the change is wrong.

- [ ] **Step 4: Run the race detector**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./...`
Expected: PASS, no data races, no goroutine leaks from `goleak`. `memory_bench_test.go` is excluded here by its build constraint. The gates are run by the CI step in Step 5.

- [ ] **Step 5: Run the full make target**

Run: `make all`
Expected: lint, split-check and tests pass in every module. The gate tests report SKIP here, because they are opt-in. Run them as CI's unit job does:

`NTFY_MEASURE_MEMORY=1 GOTOOLCHAIN=go1.26.8 go test -count=1 -run 'TestMemoryStore(ReadsDoNotScale|PruneCountWithinBound)' -v .`

Expected: PASS.

- [ ] **Step 6: Validate the change**

Run: `openspec validate "speed-up-memory-store-reads" --strict`
Expected: `Change 'speed-up-memory-store-reads' is valid`.

- [ ] **Step 7: Commit**

```bash
git add openspec/changes/speed-up-memory-store-reads/evidence.md
git commit -m "Record the memory store's post-index measurements"
```

---

## Task 7 (conditional): Decide the mutex on evidence

**Run this task only if Task 3 Step 7 failed the gate.** Otherwise `design.md` D5 stands as written — `sync.Mutex` is kept, and the open question is answered by the measurement rather than by a change.

**Files:**
- Modify: `memory.go:22-34` (the mutex), and every method that takes it
- Modify: `openspec/changes/speed-up-memory-store-reads/design.md` (record the answer to D5's open question)

**Interfaces:**
- Consumes: the gate from Task 3, the benchmarks from Task 1.
- Produces: either an `sync.RWMutex` with read paths under `RLock`, or a recorded decision that it was not the cause.

- [ ] **Step 1: Establish what is actually contended**

Run: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStoreCountActiveParallel' -benchtime 20x -cpuprofile cpu.out . && GOTOOLCHAIN=go1.26.8 go tool pprof -top -nodecount 15 cpu.out`
Expected: a profile naming where time goes. If lock contention (`sync.(*Mutex).Lock`, `runtime.lock2`) dominates, an `RWMutex` is warranted; if the scan still dominates, the index is not being read somewhere and the fix is in Task 3, not here.

- [ ] **Step 2: Change the mutex only if the profile justified it**

```go
	mu            sync.RWMutex
```

and in `Get`, `List`, `CountActive` only — the three methods that do not mutate — replace `s.mu.Lock()` / `defer s.mu.Unlock()` with:

```go
	s.mu.RLock()
	defer s.mu.RUnlock()
```

`MarkAllRead` keeps the write lock: it mutates. Do not move it under `RLock`.

- [ ] **Step 3: Verify under the race detector, which is what qualifies this**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./...`
Expected: PASS. A read path that mutates under `RLock` shows up here as a data race; that is the whole reason this step is not optional.

- [ ] **Step 4: Re-run the gate and the parallel benchmark**

Run: `NTFY_MEASURE_MEMORY=1 GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreReadsDoNotScaleWithStoreSize' -count=1 . && GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStoreCountActiveParallel' -benchtime 20x .`
Expected: the gate passes, and concurrent throughput scales with goroutines rather than flattening.

- [ ] **Step 5: Record the answer in `design.md`**

Replace D5's open question with what the profile showed and what was done, including the case where the answer was "not needed". Update the doc comment from Task 5 if the serialisation it describes has changed.

- [ ] **Step 6: Commit**

```bash
git add memory.go openspec/changes/speed-up-memory-store-reads/design.md
git commit -m "Let memory store reads run concurrently"
```
