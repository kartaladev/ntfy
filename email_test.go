package ntfy_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

func TestMailSentinels(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		err    error
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name: "a wrapped rejection matches ErrMailRejected and not ErrMailInDoubt",
			err:  fmt.Errorf("provider said 550: %w", ntfy.ErrMailRejected),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ntfy.ErrMailRejected)
				assert.NotErrorIs(t, err, ntfy.ErrMailInDoubt)
			},
		},
		{
			name: "a wrapped doubt matches ErrMailInDoubt and not ErrMailRejected",
			err:  fmt.Errorf("timed out after accept: %w", ntfy.ErrMailInDoubt),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ntfy.ErrMailInDoubt)
				assert.NotErrorIs(t, err, ntfy.ErrMailRejected)
			},
		},
		{
			name: "any other error matches neither",
			err:  errors.New("connection refused"),
			assert: func(t *testing.T, err error) {
				assert.NotErrorIs(t, err, ntfy.ErrMailRejected)
				assert.NotErrorIs(t, err, ntfy.ErrMailInDoubt)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, tc.err)
		})
	}
}

func TestEmailStatusAndSkipReasons(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		status ntfy.EmailStatus
		assert func(t *testing.T, status ntfy.EmailStatus)
	}

	valid := func(wire string) func(t *testing.T, status ntfy.EmailStatus) {
		return func(t *testing.T, status ntfy.EmailStatus) {
			assert.True(t, status.Valid())
			assert.Equal(t, wire, string(status))
		}
	}

	cases := []testCase{
		{name: "claimed", status: ntfy.EmailStatusClaimed, assert: valid("CLAIMED")},
		{name: "sending", status: ntfy.EmailStatusSending, assert: valid("SENDING")},
		{name: "sent", status: ntfy.EmailStatusSent, assert: valid("SENT")},
		{name: "retry", status: ntfy.EmailStatusRetry, assert: valid("RETRY")},
		{name: "skipped", status: ntfy.EmailStatusSkipped, assert: valid("SKIPPED")},
		{name: "failed", status: ntfy.EmailStatusFailed, assert: valid("FAILED")},
		{name: "abandoned", status: ntfy.EmailStatusAbandoned, assert: valid("ABANDONED")},
		{
			name:   "an unknown status is not valid",
			status: "PENDING",
			assert: func(t *testing.T, status ntfy.EmailStatus) { assert.False(t, status.Valid()) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, tc.status)
		})
	}

	assert.Equal(t,
		[]string{"no_address", "filtered", "inactive", "deleted"},
		[]string{ntfy.EmailSkipNoAddress, ntfy.EmailSkipFiltered, ntfy.EmailSkipInactive, ntfy.EmailSkipDeleted},
	)
}

func TestEmailKinds(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		filter ntfy.EmailFilter
		kind   string
		assert func(t *testing.T, ok bool, err error)
	}

	cases := []testCase{
		{
			name:   "a listed kind is emailed",
			filter: ntfy.EmailKinds("offer", "assigned"),
			kind:   "assigned",
			assert: func(t *testing.T, ok bool, err error) {
				require.NoError(t, err)
				assert.True(t, ok)
			},
		},
		{
			name:   "another kind is not",
			filter: ntfy.EmailKinds("offer", "assigned"),
			kind:   "taken",
			assert: func(t *testing.T, ok bool, err error) {
				require.NoError(t, err)
				assert.False(t, ok)
			},
		},
		{
			name:   "no kinds emails nothing",
			filter: ntfy.EmailKinds(),
			kind:   "offer",
			assert: func(t *testing.T, ok bool, err error) {
				require.NoError(t, err)
				assert.False(t, ok)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ok, err := tc.filter.ShouldEmail(t.Context(), ntfy.Notification{Kind: tc.kind})
			tc.assert(t, ok, err)
		})
	}
}

// storeWithoutEmail is a store that records no email deliveries.
type storeWithoutEmail struct{ ntfy.Store }

// noopPorts are host ports that do nothing.
var (
	noopMailer   = ntfy.MailerFunc(func(context.Context, ntfy.EmailMessage) error { return nil })
	noopBook     = ntfy.AddressBookFunc(func(context.Context, string) (string, bool, error) { return "", false, nil })
	noopTemplate = ntfy.EmailTemplateFunc(func(context.Context, ntfy.EmailBatch) (ntfy.EmailContent, error) {
		return ntfy.EmailContent{}, nil
	})
)

func TestNewEmailDispatcher(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		store       ntfy.Store
		nilSvc      bool
		nilMailer   bool
		nilBook     bool
		nilTemplate bool
		opts        []ntfy.EmailOption
		assert      func(t *testing.T, d *ntfy.EmailDispatcher, err error)
	}

	refused := func(t *testing.T, d *ntfy.EmailDispatcher, err error) {
		t.Helper()

		require.ErrorIs(t, err, ntfy.ErrConfiguration)
		assert.Nil(t, d)
	}

	cases := []testCase{
		{
			name: "with no options it builds, with a generated owner",
			assert: func(t *testing.T, d *ntfy.EmailDispatcher, err error) {
				require.NoError(t, err)
				require.NotNil(t, d)
				assert.True(t, strings.HasPrefix(d.Owner(), "email-"), "owner %q", d.Owner())
			},
		},
		{
			name: "every option is accepted together",
			opts: []ntfy.EmailOption{
				ntfy.WithEmailGraceDelay(time.Minute), ntfy.WithEmailMaxLag(time.Hour),
				ntfy.WithEmailBatchLimit(5), ntfy.WithEmailClaimLimit(50), ntfy.WithEmailLease(time.Minute),
				ntfy.WithEmailMaxAttempts(3), ntfy.WithEmailBackoff(time.Second, time.Minute),
				ntfy.WithEmailKinds("offer"), ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce),
				ntfy.WithEmailOwner("dispatcher-1"),
				ntfy.WithEmailErrorHandler(func(context.Context, error) {}),
			},
			assert: func(t *testing.T, d *ntfy.EmailDispatcher, err error) {
				require.NoError(t, err)
				assert.Equal(t, "dispatcher-1", d.Owner())
			},
		},
		{
			name: "no grace delay and a lag is accepted",
			opts: []ntfy.EmailOption{ntfy.WithoutEmailGraceDelay(), ntfy.WithEmailMaxLag(time.Second)},
			assert: func(t *testing.T, d *ntfy.EmailDispatcher, err error) {
				require.NoError(t, err)
				assert.NotNil(t, d)
			},
		},
		{name: "a nil service", nilSvc: true, assert: refused},
		{name: "a nil mailer", nilMailer: true, assert: refused},
		{name: "a nil address book", nilBook: true, assert: refused},
		{name: "a nil template", nilTemplate: true, assert: refused},
		{name: "a store that records no email deliveries", store: storeWithoutEmail{ntfy.NewMemoryStore()}, assert: refused},
		{name: "a zero grace delay", opts: []ntfy.EmailOption{ntfy.WithEmailGraceDelay(0)}, assert: refused},
		{name: "a negative grace delay", opts: []ntfy.EmailOption{ntfy.WithEmailGraceDelay(-time.Minute)}, assert: refused},
		{
			name:   "a grace delay both set and removed",
			opts:   []ntfy.EmailOption{ntfy.WithEmailGraceDelay(time.Minute), ntfy.WithoutEmailGraceDelay()},
			assert: refused,
		},
		{name: "a zero max lag", opts: []ntfy.EmailOption{ntfy.WithEmailMaxLag(0)}, assert: refused},
		{
			name:   "a max lag no longer than the grace delay",
			opts:   []ntfy.EmailOption{ntfy.WithEmailGraceDelay(time.Hour), ntfy.WithEmailMaxLag(time.Hour)},
			assert: refused,
		},
		{name: "a zero batch limit", opts: []ntfy.EmailOption{ntfy.WithEmailBatchLimit(0)}, assert: refused},
		{name: "a negative claim limit", opts: []ntfy.EmailOption{ntfy.WithEmailClaimLimit(-1)}, assert: refused},
		{name: "a zero lease", opts: []ntfy.EmailOption{ntfy.WithEmailLease(0)}, assert: refused},
		{name: "zero attempts", opts: []ntfy.EmailOption{ntfy.WithEmailMaxAttempts(0)}, assert: refused},
		{name: "a zero backoff base", opts: []ntfy.EmailOption{ntfy.WithEmailBackoff(0, time.Minute)}, assert: refused},
		{
			name:   "a backoff ceiling below its base",
			opts:   []ntfy.EmailOption{ntfy.WithEmailBackoff(time.Minute, time.Second)},
			assert: refused,
		},
		{name: "a nil filter", opts: []ntfy.EmailOption{ntfy.WithEmailFilter(nil)}, assert: refused},
		{name: "no kinds", opts: []ntfy.EmailOption{ntfy.WithEmailKinds()}, assert: refused},
		{
			name:   "a filter and kinds together",
			opts:   []ntfy.EmailOption{ntfy.WithEmailFilter(ntfy.EmailKinds("offer")), ntfy.WithEmailKinds("offer")},
			assert: refused,
		},
		{name: "an unknown guarantee", opts: []ntfy.EmailOption{ntfy.WithDeliveryGuarantee("EXACTLY_ONCE")}, assert: refused},
		{name: "an empty owner", opts: []ntfy.EmailOption{ntfy.WithEmailOwner("")}, assert: refused},
		{name: "a nil error handler", opts: []ntfy.EmailOption{ntfy.WithEmailErrorHandler(nil)}, assert: refused},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := tc.store
			if store == nil {
				store = ntfy.NewMemoryStore()
			}

			var svc *ntfy.Service

			if !tc.nilSvc {
				var err error

				svc, err = ntfy.New(store)
				require.NoError(t, err)
			}

			mailer, book, template := ntfy.Mailer(noopMailer), ntfy.AddressBook(noopBook), ntfy.EmailTemplate(noopTemplate)

			if tc.nilMailer {
				mailer = nil
			}

			if tc.nilBook {
				book = nil
			}

			if tc.nilTemplate {
				template = nil
			}

			d, err := ntfy.NewEmailDispatcher(svc, mailer, book, template, tc.opts...)
			tc.assert(t, d, err)
		})
	}
}
