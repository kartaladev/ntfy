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

Command: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreReadsDoNotScaleWithStoreSize' -count=1 -v .`

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

Command: `rg -n 's\.notifications\[' memory.go memory_email.go`. It was run after
the index was added to `put` and `remove`, so the line numbers are post-change.

```
memory_email.go:157:		if _, exists := s.notifications[id]; !exists {
memory.go:122:	s.notifications[n.ID] = n
memory.go:181:		if n := s.notifications[id]; n.Recipient == recipient && n.Kind == kind && n.State != StateClosed {
memory.go:229:		n := s.notifications[id]
memory.go:248:		s.notifications[id] = n
memory.go:274:	n, ok := s.notifications[id]
memory.go:383:		if n, ok := s.notifications[id]; !ok || n.Recipient != recipient {
memory.go:393:		n := s.notifications[id]
memory.go:408:		s.notifications[id] = n
memory.go:431:		s.notifications[id] = n
```

That pattern does not match `delete(...)` or a reassignment of the whole map.
So the check also ran
`rg -n 'delete\(s\.notifications|s\.notifications =|\.Recipient =' --glob '!*_test.go' .`.
It found one delete, at `memory.go:145` in `remove`. It found no reassignment of
the map, and no code in the library that assigns `Recipient`.

The writes are:

- `put` (:122);
- `remove` (:145);
- in-place replacements in `Close` (:248), `MarkRead` (:408) and `MarkAllRead`
  (:431).

The three replacements change only state, timestamps and `ClosedReason`, never
`Recipient`. `memory_email.go` only reads the map. D2 holds, and the index needs
maintaining in `put` and `remove` only.

## Prune count bound

The plan's benchmark prunes with a bound of 50 against 100 per recipient. There,
every recipient is materialised and evicted in both versions, so it cannot show
design.md D4's claim. D4 claims that the pass copies the whole store before any
per-recipient work, even when nobody exceeds the bound. The measurement therefore
uses the steady state: `DefaultMaxPerRecipient` (500) against 100 per recipient.
Nothing is removed, so one seeded store serves every call.

**Threshold, stated before measuring:** a within-bound pass at 200k/2000 makes
at most 2× the allocations it makes at 20k/200. This is
`TestMemoryStorePruneCountAllocationsDoNotScaleWithStoreSize`, which uses
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
