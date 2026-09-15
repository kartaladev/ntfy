## Why

`MemoryStore` is the zero-configuration default (`ntfy.New(ntfy.NewMemoryStore())`, service.go:76), and its every read scans the whole store under one mutex: `CountActive` (memory.go:347), `List` (:273), `MarkAllRead` (:402). An adversarial audit measured it at 200k notifications across 2000 recipients — 100 each, well inside the shipped `DefaultMaxPerRecipient = 500` — and found `CountActive` at 4.79 ms per call, `List` at 7.60 ms, and 32 concurrent callers achieving **222 requests per second for the whole process**, regardless of core count, because the scan holds the lock.

A badge count is the most frequent call an inbox makes. So ordinary authenticated traffic takes the default store down, and the doc comment promises the opposite: that it "keeps every guarantee of the Store contract, and passes the same conformance suite as the SQL store" (memory.go:15-21), with no mention of a cliff. That silence is what `.claude/rules/library-design.md` rule 4 forbids.

The same file already holds the answer. `subjects` (memory.go:28-31) indexes identifiers by subject "so that closing, coalescing and expiring watermarks touch one subject's notifications rather than every notification". Recipients never got the same treatment.

## What Changes

- **A recipient index**, the twin of the existing `subjects` index, so that a read touches one recipient's notifications rather than every notification. `CountActive`, `List` and `MarkAllRead` become proportional to the recipient's own data, as they already are on the SQL store, whose `(recipient, created_at, id)` index serves exactly this.
- **The first benchmarks in the repository.** There are none today (`rg 'func Benchmark'` finds nothing across all six modules). Per `.claude/rules/prove-errors-with-tests.md` the cliff is proved by a benchmark that fails a stated threshold before anything is optimised, and that benchmark becomes the regression guard.
- **A scaling assertion as the acceptance gate**, not a wall-clock number: per-operation cost must stay flat as the store grows while a recipient's own holdings stay fixed. That is the property being fixed, and unlike a millisecond figure it survives a change of machine.
- **The doc comment states what remains.** After the change the store is still single-process, still lost on restart, and its pruning and email-claim passes still visit everything. Those are limits a host chooses against, so they are written down rather than left to be discovered.
- **No behaviour changes.** No API, no option, no ordering, no paging semantics. The same conformance suite passes untouched, which is the whole test of whether this was done right.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This change sets `skip_specs: true`.

Every requirement in `notification-inbox` describes what a store returns, not how fast: listing "newest first ... in pages that repeat and skip nothing", counting ACTIVE notifications, marking read, and "Every store behaves identically", which is asserted by the shared suite. An index changes none of them, and the suite passing unchanged is the evidence. The performance characteristic that does change is not a behavioural contract anywhere in the specs, and inventing a requirement to carry it would put a number in the contract that every future store implementation would then have to meet. The limits that remain belong in godoc, where the store's existing limits already live.

## Impact

- **Code:** `memory.go` only — a recipient index maintained in `put` (:115-126) and `remove` (:130-140), which are already the chokepoints for the `sources` and `subjects` indexes; `CountActive`, `List`, `MarkAllRead` and `pruneCount` read through it. `Recipient` is immutable once stored, so the state mutations in `Close` (:227), `MarkRead` (:387) and `MarkAllRead` (:410) cannot invalidate it — see `design.md` D2.
- **APIs:** none. `MemoryStore` gains no exported surface; the struct's new field is unexported.
- **Tests:** new benchmarks proving the cliff and gating the fix; the existing `ntfytest.RunStore` and `RunEmail` suites must pass unchanged, which is what proves the index is maintained correctly.
- **Docs:** the `MemoryStore` doc comment's stated limits.
- **Not in scope:** the SQL store, whose indexes already cover these paths; the `Prune` age sweep (memory.go:427) and `ClaimEmails` (memory_email.go:34), which are host-driven periodic passes over the whole store by nature — see `design.md` D4; and the other findings from the same audit, each of which is its own change.
