package ntfy

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upperBound is the most a delay may be: the ceiling plus the jitter, or the
// largest duration when the ceiling is close enough to it that adding the
// jitter would overflow.
func upperBound(ceiling time.Duration) time.Duration {
	if ceiling > math.MaxInt64-ceiling/5 {
		return math.MaxInt64
	}

	return ceiling + ceiling/5
}

func TestEmailDispatcherDelay(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		base     time.Duration
		ceiling  time.Duration
		attempts int
		assert   func(t *testing.T, delay time.Duration)
	}

	future := func(ceiling time.Duration) func(t *testing.T, delay time.Duration) {
		return func(t *testing.T, delay time.Duration) {
			assert.Positive(t, delay, "a retry is always scheduled into the future")
			assert.LessOrEqual(t, delay, upperBound(ceiling), "and never past the ceiling plus its jitter")
		}
	}

	curve := func(want time.Duration) func(t *testing.T, delay time.Duration) {
		return func(t *testing.T, delay time.Duration) {
			assert.GreaterOrEqual(t, delay, time.Duration(float64(want)*0.8))
			assert.LessOrEqual(t, delay, time.Duration(float64(want)*1.2))
		}
	}

	// The default curve is one minute doubling to a one-hour cap. An uncapped
	// ceiling is how a host says "no cap"; validate accepts it.
	cases := []testCase{
		{name: "default curve, 1 attempt", base: time.Minute, ceiling: time.Hour, attempts: 1, assert: curve(time.Minute)},
		{name: "default curve, 2 attempts", base: time.Minute, ceiling: time.Hour, attempts: 2, assert: curve(2 * time.Minute)},
		{name: "default curve, 3 attempts", base: time.Minute, ceiling: time.Hour, attempts: 3, assert: curve(4 * time.Minute)},
		{name: "default curve, 4 attempts", base: time.Minute, ceiling: time.Hour, attempts: 4, assert: curve(8 * time.Minute)},
		{name: "default curve, 5 attempts", base: time.Minute, ceiling: time.Hour, attempts: 5, assert: curve(16 * time.Minute)},
		{name: "default curve, 6 attempts", base: time.Minute, ceiling: time.Hour, attempts: 6, assert: curve(32 * time.Minute)},
		{name: "default curve, 7 attempts", base: time.Minute, ceiling: time.Hour, attempts: 7, assert: curve(time.Hour)},
		{name: "default curve, 8 attempts", base: time.Minute, ceiling: time.Hour, attempts: 8, assert: curve(time.Hour)},
		{name: "default curve, 9 attempts", base: time.Minute, ceiling: time.Hour, attempts: 9, assert: curve(time.Hour)},
		{name: "default curve, 10 attempts", base: time.Minute, ceiling: time.Hour, attempts: 10, assert: curve(time.Hour)},
		{name: "uncapped, 22 attempts", base: time.Hour, ceiling: math.MaxInt64, attempts: 22, assert: future(math.MaxInt64)},
		{name: "uncapped, 23 attempts", base: time.Hour, ceiling: math.MaxInt64, attempts: 23, assert: future(math.MaxInt64)},
		{name: "uncapped, 25 attempts", base: time.Hour, ceiling: math.MaxInt64, attempts: 25, assert: future(math.MaxInt64)},
		{name: "uncapped, 55 attempts", base: time.Hour, ceiling: math.MaxInt64, attempts: 55, assert: future(math.MaxInt64)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := &EmailDispatcher{backoff: tc.base, ceiling: tc.ceiling}
			tc.assert(t, d.delay(tc.attempts))
		})
	}
}

func TestEmailConfigBackoffCeiling(t *testing.T) {
	t.Parallel()

	hour, twoHours, minute := time.Hour, 2*time.Hour, time.Minute

	type testCase struct {
		name   string
		config emailConfig
		assert func(t *testing.T, err error)
	}

	refused := func(t *testing.T, err error) {
		require.ErrorIs(t, err, ErrConfiguration)
		assert.Contains(t, err.Error(), "backoff ceiling")
	}

	// No option sets a base without a ceiling today: WithEmailBackoff sets both.
	// The check compares the values in effect, so adding one cannot panic, nor
	// slip a base past the default ceiling.
	cases := []testCase{
		{
			name:   "a base with no ceiling, within the default ceiling",
			config: emailConfig{backoff: &hour},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:   "a base with no ceiling, above the default ceiling",
			config: emailConfig{backoff: &twoHours},
			assert: refused,
		},
		{
			name:   "a ceiling below its base",
			config: emailConfig{backoff: &hour, ceiling: &minute},
			assert: refused,
		},
		{
			name:   "a ceiling with no base, below the default base",
			config: emailConfig{ceiling: &[]time.Duration{time.Second}[0]},
			assert: refused,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := &EmailDispatcher{
				grace: DefaultEmailGraceDelay, maxLag: DefaultEmailMaxLag,
				backoff: DefaultEmailBackoff, ceiling: DefaultEmailBackoffCeiling,
			}
			tc.assert(t, tc.config.apply(d))
		})
	}
}
