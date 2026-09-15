## 1. Prove the cliff (red)

- [ ] 1.1 Add `memory_bench_test.go` in the root module with a helper that builds a `MemoryStore` holding N notifications spread evenly over R recipients, at the two sizes `design.md` D6 names (20k/200 and 200k/2000, 100 each). Verify with `go test -run '^$' -bench 'BenchmarkMemoryStore' -benchtime 10x ./...` that it builds both sizes and reports allocations (`-benchmem`).
- [ ] 1.2 Benchmark `CountActive`, `List` (50 rows) and `MarkAllRead` for a single recipient at both sizes. Verify the recorded output shows per-operation cost growing roughly with store size — the audit measured 4.79 ms, 7.60 ms and 5.26 ms at 200k — and record the run in the change's notes as the proof required by `.claude/rules/prove-errors-with-tests.md`.
- [ ] 1.3 Add a concurrent `CountActive` benchmark (`b.RunParallel`, comparable to 32 goroutines) and verify it reports whole-process throughput in the hundreds of operations per second, not the thousands, confirming the single lock is the bottleneck.
- [ ] 1.4 Add the scaling assertion from `design.md` D6 as a Go test — not a benchmark — that times each operation at both sizes and fails when the 200k cost exceeds 2× the 20k cost. Verify with `go test -run 'TestMemoryStoreReadsDoNotScaleWithStoreSize' -count=1 ./...` that it **fails** today, and that it fails on the ratio assertion rather than on setup.

## 2. Index notifications by recipient (green)

- [ ] 2.1 Add the unexported `recipients map[string]map[string]struct{}` field to `MemoryStore` (memory.go:22-34) and initialise it in `NewMemoryStore` (:60-68), with a doc comment stating — as `subjects` does — that membership changes only in `put` and `remove`. Verify with `go build ./...`.
- [ ] 2.2 Maintain the index in `put` (:115-126) and `remove` (:130-140), dropping a recipient's empty set exactly as `remove` does for `subjects` (:134-139). Verify with `go test -count=1 ./...` that the existing root-module tests still pass.
- [ ] 2.3 Verify D2's load-bearing claim mechanically before relying on it: run `rg -n 's\.notifications\[' memory.go memory_email.go` and confirm every write is `put`, `remove`, or an in-place replacement that preserves `Recipient` (`Close` :227, `MarkRead` :387, `MarkAllRead` :410). Record the result in the change's notes; if a fourth mutation site exists, stop and revise `design.md` D2 before continuing.
- [ ] 2.4 Read `CountActive` (:341-354) through the index and verify the counting cases of `ntfytest.Run` still pass.
- [ ] 2.5 Read `List` (:262-301) through the index, keeping `matches`, `newestFirst`, the sort and the cursor handling untouched, and verify the listing and paging cases of `ntfytest.Run` still pass — including "Newest first with exact paging".
- [ ] 2.6 Read `MarkAllRead` (:394-415) through the index and verify the mark-all cases still pass, including "Mark all read does not swallow what arrived later".
- [ ] 2.7 Rework `pruneCount` (:448-473) to iterate `recipients` instead of copying every `Notification` into `byRecipient` (:449-452), materialising one recipient at a time and leaving `evict` and the strategy logic unchanged. Verify with `go test -run 'TestPrune' -count=1 ./...` and `-benchmem` on the prune benchmark that the whole-store allocation is gone.

## 3. Meet the gate

- [ ] 3.1 Re-run the scaling test from 1.4 and verify it now passes: per-operation cost at 200k within 2× of 20k for `CountActive`, `List` and `MarkAllRead`.
- [ ] 3.2 Re-run the benchmarks from 1.2 and 1.3 and record the new figures beside the old ones in the benchmark's comment, marked as indicative of one machine rather than as thresholds, per `design.md` D6.
- [ ] 3.3 If the gate is missed with the index in place, stop and revisit `design.md` D5 (`RWMutex`) with the benchmark output as the evidence, rather than tuning the threshold to pass.

## 4. Prove nothing else changed

- [ ] 4.1 Run the full conformance suite against the memory store — `ntfytest.Run` and `ntfytest.RunEmail` — and verify every case passes unchanged, with no edits to any suite case. Any suite edit means the index changed behaviour and the change is wrong.
- [ ] 4.2 Run `go test -race -count=1 ./...` in the root module and verify no data races and no goroutine leaks (`goleak` is wired in `main_test.go`).
- [ ] 4.3 Run `make all` and verify lint, split-check and tests pass across every module.

## 5. State what remains

- [ ] 5.1 Update the `MemoryStore` doc comment (memory.go:15-21) to state the limits that survive this change: single process, everything lost on restart, and pruning and email claims proportional to the whole store rather than to one recipient. Verify with `go doc ./... | grep -A12 'type MemoryStore'` that the rendered documentation reads correctly and no longer implies unqualified parity with the SQL store.
- [ ] 5.2 Verify the claim in that comment is now true of what the code does: read it against `Prune` (:418-444) and `ClaimEmails` (memory_email.go:25-66), and confirm it neither overstates the fix nor repeats the removed cliff.
- [ ] 5.3 Run `openspec validate "speed-up-memory-store-reads" --strict` and verify the change is valid.
