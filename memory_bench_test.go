//go:build !race

// Measurement for the memory store. The race detector distorts wall-clock
// timing beyond usefulness, so this file is excluded under -race; make all's
// ordinary `go test ./...` is what enforces the gate.

package ntfy_test

import (
	"math"
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
