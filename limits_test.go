package ntfy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

func TestDefaultLimits(t *testing.T) {
	t.Parallel()

	limits := ntfy.DefaultLimits()

	require.NotNil(t, limits.MaxTitleBytes)
	assert.Equal(t, 1024, *limits.MaxTitleBytes)
	require.NotNil(t, limits.MaxDataBytes)
	assert.Equal(t, 65536, *limits.MaxDataBytes)
	require.NotNil(t, limits.MaxLinks)
	assert.Equal(t, 16, *limits.MaxLinks)
	require.NotNil(t, limits.MaxLinkRelationBytes)
	assert.Equal(t, ntfy.MaxKindBytes, *limits.MaxLinkRelationBytes)
	require.NotNil(t, limits.MaxLinkHrefBytes)
	assert.Equal(t, 2048, *limits.MaxLinkHrefBytes)
	require.NotNil(t, limits.MaxDraftsPerPublish)
	assert.Equal(t, 1000, *limits.MaxDraftsPerPublish)
	require.NotNil(t, limits.MaxFilterValues)
	assert.Equal(t, 100, *limits.MaxFilterValues)
	assert.Equal(t, []string{"http", "https"}, limits.LinkSchemes)
}

func TestDefaultLinkSchemesIsACopy(t *testing.T) {
	t.Parallel()

	schemes := ntfy.DefaultLinkSchemes()
	schemes[0] = "javascript"

	assert.Equal(t, []string{"http", "https"}, ntfy.DefaultLinkSchemes(), "a caller cannot change the defaults")
}

// TestWithLimitsSnapshotsHostValues proves that a host mutating the pointers
// or the scheme slice it gave WithLimits, after New returns, cannot change an
// already-built service's policy.
func TestWithLimitsSnapshotsHostValues(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		limits func() ntfy.Limits
		mutate func(l ntfy.Limits)
		draft  ntfy.Draft
	}

	cases := []testCase{
		{
			name:   "mutating the host's limit pointer after New does not raise the service's link cap",
			limits: func() ntfy.Limits { return ntfy.Limits{MaxLinks: ntfy.Limit(2)} },
			mutate: func(l ntfy.Limits) { *l.MaxLinks = 1000 },
			draft: ntfy.Draft{
				Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer",
				Links: map[string]string{
					"a": "http://x.test/1", "b": "http://x.test/2", "c": "http://x.test/3",
				},
			},
		},
		{
			name:   "mutating the host's scheme slice after New does not widen the service's permitted schemes",
			limits: func() ntfy.Limits { return ntfy.Limits{LinkSchemes: []string{"https"}} },
			mutate: func(l ntfy.Limits) { l.LinkSchemes[0] = "http" },
			draft: ntfy.Draft{
				Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer",
				Links: map[string]string{"a": "http://x.test/1"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			limits := tc.limits()

			svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithLimits(limits))
			require.NoError(t, err)

			tc.mutate(limits)

			_, err = svc.Publish(t.Context(), tc.draft)
			assert.ErrorIs(t, err, ntfy.ErrValidation,
				"the service keeps enforcing the limits it was given, not the host's later mutation")
		})
	}
}

func TestNewRefusesMeaninglessLimits(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		limits ntfy.Limits
		assert func(t *testing.T, svc *ntfy.Service, err error)
	}

	refused := func(t *testing.T, svc *ntfy.Service, err error) {
		t.Helper()

		require.ErrorIs(t, err, ntfy.ErrConfiguration)
		assert.Nil(t, svc)
	}

	cases := []testCase{
		{
			name:   "a zero title limit is refused",
			limits: ntfy.Limits{MaxTitleBytes: ntfy.Limit(0)},
			assert: refused,
		},
		{
			name:   "a negative data limit is refused",
			limits: ntfy.Limits{MaxDataBytes: ntfy.Limit(-1)},
			assert: refused,
		},
		{
			name:   "a zero filter value limit is refused",
			limits: ntfy.Limits{MaxFilterValues: ntfy.Limit(0)},
			assert: refused,
		},
		{
			name:   "a zero draft limit is refused",
			limits: ntfy.Limits{MaxDraftsPerPublish: ntfy.Limit(0)},
			assert: refused,
		},
		{
			name:   "naming schemes and accepting any scheme at once is refused",
			limits: ntfy.Limits{LinkSchemes: []string{"mailto"}, AnyLinkScheme: true},
			assert: refused,
		},
		{
			name:   "naming no scheme at all is refused",
			limits: ntfy.Limits{LinkSchemes: []string{}},
			assert: refused,
		},
		{
			name:   "an empty scheme is refused",
			limits: ntfy.Limits{LinkSchemes: []string{"mailto", ""}},
			assert: refused,
		},
		{
			name:   "unset limits keep the defaults",
			limits: ntfy.Limits{},
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.NoError(t, err)
				assert.NotNil(t, svc)
			},
		},
		{
			name:   "a raised limit is accepted",
			limits: ntfy.Limits{MaxDataBytes: ntfy.Limit(1 << 20)},
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.NoError(t, err)
				assert.NotNil(t, svc)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithLimits(tc.limits))
			tc.assert(t, svc, err)
		})
	}
}
