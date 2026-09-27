# Evidence

Proof for `.claude/rules/prove-errors-with-tests.md` and
`.claude/rules/performance-benchmark.md`. Absolute numbers are one machine's; the
ratio between the two fixture sizes is the property under test.

- **Machine:** Apple M4 Pro (14 cores), 24 GB RAM, macOS 26.6.2, go1.26.8
  darwin/arm64.
- **Fixture:** `seedMemoryStore` in `memory_bench_test.go`. There are 20,000
  notifications over 200 recipients, or 200,000 over 2,000. Every
  notification is ACTIVE, there is one subject per recipient, and creation
  times are 1 ms apart from 2026-01-01T00:00:00Z. The recipient that is read
  (`user-0`) holds 100 notifications at both sizes.
- **Threshold, stated before measuring (`design.md` D6):** the per-operation
  cost at 200k must be within 2× of the cost at 20k, for each of `CountActive`,
  `List` (50 rows) and `MarkAllRead`.
- **Date:** 2026-09-27.

## Before: reads scale with store size

Command: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStore' -benchmem -benchtime 20x -count 3 .`

```
goos: darwin
goarch: arm64
pkg: github.com/kartaladev/ntfy
cpu: Apple M4 Pro
BenchmarkMemoryStoreCountActive/20k-14        	      20	    354677 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/20k-14        	      20	    247192 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/20k-14        	      20	    212846 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/200k-14       	      20	   5475254 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/200k-14       	      20	   5442575 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/200k-14       	      20	   5578035 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActiveParallel-14    	      20	   5343479 ns/op	       187.1 ops/s	    1264 B/op	       6 allocs/op
BenchmarkMemoryStoreCountActiveParallel-14    	      20	   5247292 ns/op	       190.6 ops/s	    1346 B/op	       7 allocs/op
BenchmarkMemoryStoreCountActiveParallel-14    	      20	   5890188 ns/op	       169.8 ops/s	     568 B/op	       5 allocs/op
BenchmarkMemoryStoreList/20k-14               	      20	    382596 ns/op	   97589 B/op	      24 allocs/op
BenchmarkMemoryStoreList/20k-14               	      20	    399042 ns/op	   97457 B/op	      22 allocs/op
BenchmarkMemoryStoreList/20k-14               	      20	    378794 ns/op	   97457 B/op	      22 allocs/op
BenchmarkMemoryStoreList/200k-14              	      20	   9769844 ns/op	   97457 B/op	      22 allocs/op
BenchmarkMemoryStoreList/200k-14              	      20	   9903415 ns/op	   97457 B/op	      22 allocs/op
BenchmarkMemoryStoreList/200k-14              	      20	   9549494 ns/op	   97494 B/op	      22 allocs/op
BenchmarkMemoryStoreMarkAllRead/20k-14        	      20	    220929 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/20k-14        	      20	    219779 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/20k-14        	      20	    215096 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/200k-14       	      20	   5349679 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/200k-14       	      20	   6650048 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/200k-14       	      20	   5777985 ns/op	       0 B/op	       0 allocs/op
PASS
```

Cost at 200k/2000 divided by cost at 20k/200. Each figure is the median of
three runs.

| Operation | 20k/200 | 200k/2000 | Ratio |
| --- | --- | --- | --- |
| CountActive | 247 µs | 5.48 ms | 22× |
| List | 383 µs | 9.77 ms | 26× |
| MarkAllRead | 220 µs | 5.78 ms | 26× |

The store grew 10×, and the cost grew more than 10×. The larger map also loses
cache locality. The audit's claim reproduces: its figures were 4.79 ms, 7.60 ms
and 5.26 ms.

Concurrent `CountActive` at 200k/2000 ran 32 goroutines on GOMAXPROCS 14. It
reached **170–191 ops/s for the whole process**, about 5.3 ms per operation.
That is hundreds of operations per second, not thousands, so the single lock
held across the scan is the bottleneck. The audit measured 222.

### The gate fails

Command: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreReadsDoNotScaleWithStoreSize' -count=1 -v .` (as recorded; since review the gate needs `NTFY_MEASURE_MEMORY=1`, see "Where the gates run")

```
=== RUN   TestMemoryStoreReadsDoNotScaleWithStoreSize/CountActive
        	Error:      	"26.83171919716468" is not less than or equal to "2"
        	Messages:   	CountActive cost grew with store size: 211.052µs at 20k, 5.662888ms at 200k (ratio 26.8)
=== RUN   TestMemoryStoreReadsDoNotScaleWithStoreSize/List
        	Error:      	"25.636941418834514" is not less than or equal to "2"
        	Messages:   	List cost grew with store size: 410.661µs at 20k, 10.528092ms at 200k (ratio 25.6)
=== RUN   TestMemoryStoreReadsDoNotScaleWithStoreSize/MarkAllRead
        	Error:      	"23.669459971109934" is not less than or equal to "2"
        	Messages:   	MarkAllRead cost grew with store size: 222.914µs at 20k, 5.276254ms at 200k (ratio 23.7)
--- FAIL: TestMemoryStoreReadsDoNotScaleWithStoreSize (4.68s)
```

All three cases fail on the ratio assertion. None fails in seeding.

### The gate's estimator was hardened, and the threshold was kept

The first version called `t.Parallel()` and took one mean of 200 calls. After
the fix it passed 30 of 30 runs on an idle machine. It then ran 10 times while
14 busy loops saturated every core, and `List` failed twice, at ratios 4.5 and
2.6. A 100-notification read is only tens of microseconds, so a single mean is
at the mercy of the scheduler.

The noise was removed, and the threshold was not touched. It is still 2×:

- The gate no longer calls `t.Parallel()`. A sequential test runs while the
  package's parallel tests are paused.
- Each cost is the least of 5 rounds of 200 calls, interleaved between the two
  sizes. Interference only ever adds time.

Afterwards the gate passed 20 of 20 runs idle and 20 of 20 under the same full
load.

The hardened gate was then re-proved red. The three read loops were temporarily
restored to the whole-map scan, with the index still maintained. The gate failed
on the ratio again:

```
        	Messages:   	CountActive cost grew with store size: 216.831µs at 20k, 7.890666ms at 200k (ratio 36.4)
        	Messages:   	List cost grew with store size: 366.441µs at 20k, 9.816165ms at 200k (ratio 26.8)
        	Messages:   	MarkAllRead cost grew with store size: 221.302µs at 20k, 7.693306ms at 200k (ratio 34.8)
--- FAIL: TestMemoryStoreReadsDoNotScaleWithStoreSize (30.57s)
```

With the index read restored, it passes in 0.41 s.

## D2 verification

This check was re-run against the code as finally committed, after the review
fixes, so the line numbers match the tree.

Command: `rg -n 's\.notifications\[' memory.go memory_email.go`

```
memory_email.go:157:		if _, exists := s.notifications[id]; !exists {
memory.go:135:	if old, ok := s.notifications[n.ID]; ok {
memory.go:139:	s.notifications[n.ID] = n
memory.go:194:		if n := s.notifications[id]; n.Recipient == recipient && n.Kind == kind && n.State != StateClosed {
memory.go:242:		n := s.notifications[id]
memory.go:261:		s.notifications[id] = n
memory.go:287:	n, ok := s.notifications[id]
memory.go:308:		n := s.notifications[id]
memory.go:384:		if s.notifications[id].State == StateActive {
memory.go:398:		if n, ok := s.notifications[id]; !ok || n.Recipient != recipient {
memory.go:408:		n := s.notifications[id]
memory.go:423:		s.notifications[id] = n
memory.go:439:		n := s.notifications[id]
memory.go:448:		s.notifications[id] = n
memory.go:507:			held = append(held, s.notifications[id])
```

That pattern does not match `delete(...)`, a reassignment of the whole map, or
an assignment to `Recipient`. A second command covers those:

```
$ rg -n 'delete\(s\.notifications|s\.notifications =|\.Recipient =' --glob '!*_test.go' --glob '!openspec/**' .
./memory.go:148:	delete(s.notifications, n.ID)
./memory.go:194:		if n := s.notifications[id]; n.Recipient == recipient && n.Kind == kind && n.State != StateClosed {
./memory.go:249:		case req.Except != "" && n.Recipient == req.Except:
./ntfytest/broadcaster.go:184:		if signal.Recipient == recipient {
```

Those hits are the one delete, in `remove` (:148), and three `==` comparisons.
Nothing reassigns the map, and nothing in the library assigns `Recipient`.

The writes are:

- `put` (:139), which first removes any notification already stored under the
  identifier (:135);
- `remove` (:148);
- in-place replacements in `Close` (:261), `MarkRead` (:423) and `MarkAllRead`
  (:448).

The three replacements change only state, timestamps and `ClosedReason`, never
`Recipient`. `memory_email.go` only reads the map. D2 holds, and the index needs
maintaining in `put` and `remove` only.

### The identifier-reuse gap, found in review

D2 as first written missed a case. When `put` stored an identifier that was
already held, it overwrote the map entry and left the earlier notification's
index entries in place. `IDGenerator` is the consumer's to replace, and the
memory store has no primary key to reject a repeat, unlike the SQL store.

After such a reuse, `recipients` pointed the earlier recipient at the
replacement, so `CountActive` and `MarkAllRead` reached another recipient's
notification. `subjects` and `sources` went stale too; that part already
happened on `main`.

The case "after an identifier is reused for another recipient" in
`TestMemoryStoreRecipientIndexMirrorsTheStore` failed before the fix:

```
            	Messages:   	index holds id-a under "alice", stored under "carol"
            	Messages:   	index holds 4 identifiers, the store holds 3 notifications
            	Messages:   	alice keeps no entry for carol's notification
            	Messages:   	task-1 keeps no entry for carol's notification
            	Messages:   	alice counts only her own notification
            	Messages:   	alice marks only her own notification
            	Messages:   	carol's notification is untouched by alice
FAIL
```

`put` now removes the stored notification with all its index entries before
storing the replacement. The replacement still wins, as it always did, and
every index follows it. The case passes, and so does the conformance suite.

The security review traced every path that assigns an identifier. Publish and
Close successors both mint identifiers through the host's `IDGenerator`. No
HTTP route accepts an identifier to store. A reuse therefore needs host code
that breaks the `IDGenerator` contract. That makes this a correctness fix, not
an exploitable vulnerability.

## Prune count bound

The plan's benchmark prunes with a bound of 50 against 100 per recipient. There,
every recipient is materialised and evicted in both versions, so it cannot show
design.md D4's claim. D4 claims that the pass copies the whole store before any
per-recipient work, even when nobody exceeds the bound. The measurement therefore
uses the steady state: `DefaultMaxPerRecipient` (500) against 100 per recipient.
Nothing is removed, so one seeded store serves every call.

**Threshold, stated before measuring:** a within-bound pass at 200k/2000 makes
at most 2× the allocations it makes at 20k/200. This is
`TestMemoryStorePruneCountAllocationsDoNotScaleWithStoreSize` (since replaced, see below), which uses
`testing.AllocsPerRun`. Allocation counts are deterministic, so this gate does
not depend on load.

Before, the gate fails:

```
        	Error:      	"16045" is not less than or equal to "3246"
        	Messages:   	a within-bound count pass allocated with store size: 1623 allocations at 20k, 16045 at 200k
FAIL
```

Command: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStorePruneCount' -benchmem -benchtime 20x -count 3 .`

Before:

```
BenchmarkMemoryStorePruneCount/20k-14         	      20	   3890777 ns/op	12991161 B/op	    1623 allocs/op
BenchmarkMemoryStorePruneCount/20k-14         	      20	   4830338 ns/op	12990889 B/op	    1623 allocs/op
BenchmarkMemoryStorePruneCount/20k-14         	      20	   6237544 ns/op	12990655 B/op	    1623 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	  54317519 ns/op	130088696 B/op	   16045 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	  46741683 ns/op	130088156 B/op	   16045 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	  34035398 ns/op	130088150 B/op	   16045 allocs/op
```

After (`pruneCount` iterates `recipients`, materialising only recipients over the
bound), the gate passes:

```
BenchmarkMemoryStorePruneCount/20k-14         	      20	     18081 ns/op	    9392 B/op	      12 allocs/op
BenchmarkMemoryStorePruneCount/20k-14         	      20	     18273 ns/op	    9392 B/op	      12 allocs/op
BenchmarkMemoryStorePruneCount/20k-14         	      20	     16215 ns/op	    9392 B/op	      12 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	    242860 ns/op	  100784 B/op	      16 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	    230804 ns/op	  100784 B/op	      16 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	    225475 ns/op	  100784 B/op	      16 allocs/op
```

At 200k a pass that removes nothing drops from 130 MB to 100 KB, and it holds the
lock for about 0.23 ms instead of 34–54 ms. The 100 KB that remains is the
sorted list of recipient keys. It is proportional to recipients, not to
notifications, and it is kept because `PruneResult.Recipients` is reported in
sorted order.

### Tightened in review: a within-bound pass allocates nothing

Review raised two gaps in that gate:

- It counted allocations, not bytes. A regression that added a single
  allocation sized to the whole store would have passed: 12→13 allocations at
  20k, and 16→17 at 200k.
- The 100 KB of sorted keys still grew with the recipient count.

**Threshold, stated before measuring:** a pass that finds every recipient within
the bound makes **zero** allocations, at both sizes. Zero allocations is zero
bytes, so this catches the single large allocation too. The test is
`TestMemoryStorePruneCountWithinBoundAllocatesNothing`. It failed against the
code above:

```
        	Messages:   	a within-bound count pass allocated 12 times at 20k
        	Messages:   	a within-bound count pass allocated 16 times at 200k
--- FAIL: TestMemoryStorePruneCountWithinBoundAllocatesNothing (0.17s)
```

`pruneCount` now collects and sorts only the recipients over the bound. In the
steady state there are none, so it allocates nothing. The test passes:

```
BenchmarkMemoryStorePruneCount/20k-14         	      20	      2042 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStorePruneCount/20k-14         	      20	      1294 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStorePruneCount/20k-14         	      20	      1140 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	     14581 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	     14412 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStorePruneCount/200k-14        	      20	     17169 ns/op	       0 B/op	       0 allocs/op
```

At 200k, the within-bound pass went from 130 MB and 34–54 ms at the start of
this change to 0 B and about 15 µs. What remains is one walk over the recipient
keys, which the doc comment states.

## Where the gates run

Review found that CI's only unit job runs the root module with `-race`. That
build excludes `memory_bench_test.go`, so the gates never ran on a pull request.
Two changes fix this:

- Both gate tests are now opt-in through `NTFY_MEASURE_MEMORY`, following
  `NTFY_MEASURE_ROWS` in sqlstore. They seed 220,000 notifications each and time
  microsecond operations, so they stay out of the default local run.
- CI's unit job has a step of its own, without `-race`, that sets the variable.
  The gates are the only tests running there:

```
NTFY_MEASURE_MEMORY=1 go test -count=1 -run 'TestMemoryStore(ReadsDoNotScale|PruneCountWithinBound)' -v .
```

That run passes locally. With the variable unset, both tests report SKIP.

## After

Absolute figures are indicative of one machine. The gate is
`TestMemoryStoreReadsDoNotScaleWithStoreSize`, which asserts the ratio only.

Command: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStore(CountActive|List|MarkAllRead)' -benchmem -benchtime 20x -count 3 .`

```
goos: darwin
goarch: arm64
pkg: github.com/kartaladev/ntfy
cpu: Apple M4 Pro
BenchmarkMemoryStoreCountActive/20k-14        	      20	      5042 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/20k-14        	      20	      5183 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/20k-14        	      20	      5456 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/200k-14       	      20	      5669 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/200k-14       	      20	      5819 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActive/200k-14       	      20	      5938 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActiveParallel-14    	      20	     15917 ns/op	     62934 ops/s	    1292 B/op	       7 allocs/op
BenchmarkMemoryStoreCountActiveParallel-14    	      20	      8038 ns/op	    125065 ops/s	    1312 B/op	       6 allocs/op
BenchmarkMemoryStoreCountActiveParallel-14    	      20	      6700 ns/op	    154640 ops/s	     172 B/op	       4 allocs/op
BenchmarkMemoryStoreList/20k-14               	      20	     33800 ns/op	   97589 B/op	      24 allocs/op
BenchmarkMemoryStoreList/20k-14               	      20	     41479 ns/op	   97578 B/op	      22 allocs/op
BenchmarkMemoryStoreList/20k-14               	      20	     26121 ns/op	   97457 B/op	      22 allocs/op
BenchmarkMemoryStoreList/200k-14              	      20	     25594 ns/op	   97457 B/op	      22 allocs/op
BenchmarkMemoryStoreList/200k-14              	      20	     25919 ns/op	   97457 B/op	      22 allocs/op
BenchmarkMemoryStoreList/200k-14              	      20	     25304 ns/op	   97457 B/op	      22 allocs/op
BenchmarkMemoryStoreMarkAllRead/20k-14        	      20	      2123 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/20k-14        	      20	      2075 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/20k-14        	      20	      2073 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/200k-14       	      20	      2521 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/200k-14       	      20	      2179 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemoryStoreMarkAllRead/200k-14       	      20	      2158 ns/op	       0 B/op	       0 allocs/op
```

The parallel benchmark ran with 20,000 iterations: at 20, the setup of 32
goroutines dominates a 5 µs operation.

Command: `GOTOOLCHAIN=go1.26.8 go test -run '^$' -bench 'BenchmarkMemoryStoreCountActiveParallel' -benchmem -benchtime 20000x -count 3 .`

```
BenchmarkMemoryStoreCountActiveParallel-14    	   20000	      5236 ns/op	    190970 ops/s	       3 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActiveParallel-14    	   20000	      5385 ns/op	    185695 ops/s	       1 B/op	       0 allocs/op
BenchmarkMemoryStoreCountActiveParallel-14    	   20000	      5420 ns/op	    184492 ops/s	       0 B/op	       0 allocs/op
```

Each figure below is the median of three runs:

| Operation | 20k/200 | 200k/2000 | Ratio | Before, at 200k |
| --- | --- | --- | --- | --- |
| CountActive | 5.18 µs | 5.82 µs | 1.1× | 5.48 ms |
| List | 33.8 µs | 25.6 µs | 0.8× | 9.77 ms |
| MarkAllRead | 2.08 µs | 2.18 µs | 1.0× | 5.78 ms |

Concurrent `CountActive` at 200k/2000, with 32 goroutines, reached about
185,000 ops/s. Before the index it reached 170–191 ops/s.

design.md D6's indicative targets were `CountActive` under 50 µs, `List` under
200 µs, `MarkAllRead` under 100 µs, and more than 20,000 concurrent ops/s. All
four are met on this machine.

`TestMemoryStoreReadsDoNotScaleWithStoreSize` passes. The gate was met, so
`design.md` D5 stands: `sync.Mutex` is kept, and plans.md Task 7 (`RWMutex`) does
not run.
