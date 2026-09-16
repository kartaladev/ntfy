## Context

See `proposal.md` — Why. `design.md` applies here because the schema's condition names performance, and because the index has a correctness obligation: an index that falls out of step with the map is a worse defect than the slowness it cures.

The constraints that shape the approach:

- **`notifications` membership changes in exactly two functions.** `put` (memory.go:115-126) is the only add; `remove` (:130-140) is the only delete. Both already maintain the `sources` and `subjects` indexes.
- **`Recipient` is immutable once stored.** The three other writers of `s.notifications` — `Close` (:227), `MarkRead` (:387), `MarkAllRead` (:410) — replace a value in place and never change its recipient, so none of them can invalidate a recipient index.
- **The precedent is in the same file.** `subjects map[string]map[string]struct{}` (:28-31) is an id-set index maintained in `put`/`remove` and read by `hasOpen` (:158), `Close` (:207) and `pruneWatermarks` (:503). This change is its twin.
- **The SQL store already does this.** Its `(recipient, created_at, id)` index serves listing and paging, and `(recipient, state)` serves counting. The memory store is the outlier, not the SQL store.
- **The conformance suite is the correctness gate.** `ntfytest.RunStore` and `RunEmail` must pass unchanged; they are what proves the index stayed in step.
- **Not every scan is a request path.** `Prune`'s age sweep (:427) and `ClaimEmails` (memory_email.go:34) are host-driven periodic passes that are global by nature — the age bound and the claim ordering are not recipient-scoped.

## Goals / Non-Goals

**Goals:**

- A read costs what a recipient holds, not what the store holds.
- The cliff is proved by a benchmark before it is fixed, and that benchmark stays as the regression guard.
- The acceptance gate is a property (cost does not grow with store size), not a wall-clock number that a different machine invalidates.
- The store's remaining limits are written down.

**Non-Goals:**

- Any behaviour change: no API, no option, no ordering or paging change.
- Making `MemoryStore` a production store. It stays single-process and volatile; this removes an unstated cliff, it does not change what the store is for.
- Optimising the periodic passes (D4).
- Touching the SQL store or the shared suite's cases.

## Decisions

### D1. An id-set index keyed by recipient, not derived counters

Add `recipients map[string]map[string]struct{}`, mirroring `subjects` exactly: recipient to the set of that recipient's notification identifiers. `CountActive`, `List` and `MarkAllRead` iterate that set and look each identifier up in `notifications`.

*Alternative considered:* also keep `activeCounts map[string]int64` so `CountActive` is O(1). Rejected for now. A counter is derived from **state**, so it must be updated at five sites including the three state mutations (`Close`, `MarkRead`, `MarkAllRead`), and a single missed transition returns a wrong count — a silent correctness bug, where a missed index update at least surfaces as a missing notification. The id-set is maintained at the two sites that already maintain two other indexes, and it reduces the work by the factor that matters. If a benchmark later shows counting still dominates, a counter can be added on that evidence.

**Default:** the index is unconditional and unexported; there is nothing to configure. **Override:** none needed — a host that outgrows the memory store's real limits (single process, volatile) moves to `ntfy/sqlstore`, which is the documented override for the store as a whole.

### D2. Maintained in `put` and `remove` only

`put` adds the identifier to its recipient's set, creating the set if absent. `remove` deletes it and drops the empty set, exactly as it does for `subjects` (:134-139). Nothing else touches it, because nothing else changes membership or recipient.

This is the load-bearing claim of the change, so the implementer verifies it rather than trusting it: every write to `s.notifications` must be either `put`, `remove`, or an in-place replacement that preserves `Recipient`. The check is mechanical (`rg 's\.notifications\[' memory.go memory_email.go`) and belongs in the task list.

### D3. `List` keeps its sort; no per-recipient ordered structure

`List` (:262-301) filters, then sorts with `slices.SortFunc(matched, newestFirst)` (:285), then pages. With the index it filters over the recipient's identifiers instead of the whole map, and keeps the sort.

*Alternative considered:* hold each recipient's identifiers in a created-at-ordered slice so listing is a scan of the page's worth. Rejected: it makes `put` an ordered insert and `remove` a search, complicates nothing else in the file, and buys an asymptotic improvement over a set that a prune bound already keeps small (`DefaultMaxPerRecipient = 500`). Sorting a few hundred entries is not what made this 7.60 ms — scanning 200,000 was.

Cursor paging is unaffected: `DecodeCursor`/`EncodeCursor` (:263, :292) work off the sorted, filtered result and never off map order.

### D4. The periodic passes stay full scans, deliberately

`Prune`'s age sweep (:424-433) selects by `InactiveAt` across all recipients, and `ClaimEmails` (memory_email.go:34) orders candidates globally by creation time. Neither is recipient-scoped, so a recipient index does not serve them, and both are host-driven passes on an interval rather than per-request work.

`pruneCount` (:448-473) is the exception worth changing: it currently allocates `byRecipient` by copying **every** `Notification` value in the store (:449-452) before doing per-recipient work. With the index it iterates `recipients` directly and materialises one recipient's notifications at a time, which removes a whole-store allocation held under the lock. The eviction logic itself is untouched.

### D5. Keep `sync.Mutex`; do not pre-emptively switch to `RWMutex`

The measured 222 req/s came from holding the lock across 200,000 iterations, not from readers excluding each other. Shortening the critical section by three orders of magnitude addresses the cause. An `RWMutex` would then permit concurrent readers, but it changes the serialisation the doc comment describes (memory.go:19-21), and a read path that mutates under `RLock` becomes a data race rather than a slow path.

So: measure first. If the benchmark misses the gate in D6 with the index in place, `RWMutex` is the next step, on that evidence, and `go test -race` plus the conformance suite are what qualify it. Recorded as an open question rather than done speculatively.

### D6. The gate is a scaling property, with indicative absolutes

**Primary gate — cost must not grow with store size.** Benchmark `CountActive`, `List` (50 rows) and `MarkAllRead` for one recipient holding 100 notifications, at two store sizes: 20k notifications / 200 recipients, and 200k / 2000. Per-operation cost at 200k must be **within 2× of the cost at 20k** for each operation. Today the ratio tracks the 10× size increase; the index makes it flat. This is the property being fixed, and it holds on any machine.

**Secondary, indicative only** (the audit's hardware, 200k/2000/100): `CountActive` under 50 µs/op, `List` under 200 µs/op, `MarkAllRead` under 100 µs/op, and 32 concurrent `CountActive` goroutines above 20,000 ops/sec against the measured 222. These are recorded in the benchmark's comment as what was seen, not asserted as thresholds, because a CI runner will not reproduce them.

A performance change without a stated gate cannot be verified, and a gate a shared runner fails at random is worse than none — hence the split.

## Risks / Trade-offs

- **The index falls out of step with `notifications`** → The whole conformance suite runs against the memory store; a stale or missing entry makes a notification vanish from listing or counting, which the suite's paging and counting cases catch. D2's mechanical check is the second guard.
- **A future writer adds a third mutation site and forgets the index** → The doc comment on the field states that membership changes only in `put`/`remove`, as `subjects` does today. This is a convention, not a compiler guarantee; the suite is the backstop.
- **The 2× ratio is too loose or too tight** → It is a first calibration. If the ratio proves noisy on a shared runner, widen it and say so in the benchmark comment rather than deleting the gate; the shape of the curve is the signal, and a loose gate still catches a return to linear growth.
- **Benchmarks make `go test ./...` slow** → Benchmarks do not run under `go test` unless `-bench` is given, and building 200k notifications belongs in the benchmark's setup, not in a test.
- **This reads as an endorsement of the memory store for production** → D-none: the doc comment's stated limits are part of the change precisely so that faster does not read as suitable.

## Migration Plan

1. No data migration, no schema change, no configuration change, no API change.
2. Hosts on `MemoryStore`: nothing to do. Reads get faster; behaviour is identical.
3. Hosts on `ntfy/sqlstore`: entirely unaffected.
4. Rollback is reverting the change; nothing persists that a rollback would strand.

## Open Questions

- Whether `RWMutex` is needed after the index lands (D5). Deferrable: it is a follow-up qualified by the same benchmark and the same suite, and it changes neither this task breakdown nor any contract.
- Whether `ClaimEmails`'s global scan (memory_email.go:34) becomes a problem for a host running email dispatch against the memory store. Deferrable: it is a periodic pass, and the claim's global ordering is inherent to it — if it matters, it is its own change with its own benchmark.
