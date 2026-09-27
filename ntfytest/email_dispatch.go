package ntfytest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// RunEmailDispatch runs email dispatchers end to end over stores the factory
// builds: concurrent dispatchers, a dispatcher that stops mid-send, an
// at-least-once resend after some or all of its notifications were read, the
// reason a failed delivery records, and a redeploy.
func RunEmailDispatch(t *testing.T, factory EmailFactory) {
	t.Helper()

	parallel(t, "two dispatchers running at once send every notification exactly once", func(t *testing.T) {
		d := newDispatchEnv(t, factory)

		var published []string

		for i := range 24 {
			published = append(published, d.publish(fmt.Sprintf("user-%d", i%6)).ID)
		}

		d.clock.advance(10 * time.Minute)

		opts := []ntfy.EmailOption{ntfy.WithEmailClaimLimit(5), ntfy.WithEmailBatchLimit(2)}
		one, two := d.dispatcher("one", opts...), d.dispatcher("two", opts...)

		for round := 0; ; round++ {
			require.Lessf(t, round, 100, "dispatch did not finish")

			var (
				wg           sync.WaitGroup
				first, other ntfy.DispatchResult
				errA, errB   error
			)

			wg.Go(func() { first, errA = one.Dispatch(t.Context()) })
			wg.Go(func() { other, errB = two.Dispatch(t.Context()) })
			wg.Wait()

			require.NoError(t, errA)
			require.NoError(t, errB)

			if first.Claimed == 0 && other.Claimed == 0 {
				break
			}
		}

		d.clock.advance(time.Hour)

		late, err := one.Dispatch(t.Context())
		require.NoError(t, err)
		assert.Zero(t, late.Claimed, "no delivery is left claimed or sending once leases lapse")

		sent := d.sentIDs()
		slices.Sort(sent)
		slices.Sort(published)
		assert.Equal(t, published, sent, "every notification is sent exactly once")
		assert.Empty(t, d.reported())
	})

	for _, guarantee := range []ntfy.DeliveryGuarantee{ntfy.AtMostOnce, ntfy.AtLeastOnce} {
		parallel(t, "a dispatcher that stops after sending, under "+string(guarantee), func(t *testing.T) {
			d := newDispatchEnv(t, factory)
			d.publish("alice")
			d.clock.advance(10 * time.Minute)

			ctx, cancel := context.WithCancel(t.Context())

			d.onSend = func(ntfy.EmailMessage) { cancel() }

			stopping := d.dispatcher("stopping", ntfy.WithDeliveryGuarantee(guarantee))
			_, err := stopping.Dispatch(ctx)
			require.NoError(t, err)
			require.Len(t, d.sent(), 1)

			d.onSend = nil
			d.clock.advance(ntfy.DefaultEmailLease + time.Minute)

			next := d.dispatcher("next", ntfy.WithDeliveryGuarantee(guarantee))
			result, err := next.Dispatch(t.Context())
			require.NoError(t, err)

			if guarantee == ntfy.AtMostOnce {
				assert.Equal(t, 1, result.Abandoned)
				assert.Len(t, d.sent(), 1, "never sent again")

				return
			}

			assert.Equal(t, 1, result.Sent)

			messages := d.sent()
			require.Len(t, messages, 2, "sent once more")
			assert.Equal(t, messages[0].IdempotencyKey, messages[1].IdempotencyKey)
			assert.Equal(t, messages[0].NotificationIDs, messages[1].NotificationIDs)

			d.clock.advance(time.Hour)

			settled, err := next.Dispatch(t.Context())
			require.NoError(t, err)
			assert.Zero(t, settled.Claimed)
		})
	}

	parallel(t, "an at-least-once resend covers only what is still active", func(t *testing.T) {
		d := newDispatchEnv(t, factory)
		read := d.publish("alice")
		kept := d.publish("alice")
		d.clock.advance(10 * time.Minute)

		ctx, cancel := context.WithCancel(t.Context())

		d.setOnSend(func(ntfy.EmailMessage) { cancel() })

		stopping := d.dispatcher("stopping", ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce))
		_, err := stopping.Dispatch(ctx)
		require.NoError(t, err)
		require.Len(t, d.sent(), 1)
		require.Len(t, d.sent()[0].NotificationIDs, 2)

		d.setOnSend(nil)

		_, err = d.svc.MarkRead(t.Context(), "alice", read.ID)
		require.NoError(t, err)

		d.clock.advance(ntfy.DefaultEmailLease + time.Minute)

		next := d.dispatcher("next", ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce))
		result, err := next.Dispatch(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, result.Sent)
		assert.Equal(t, 1, result.SkippedInactive)

		messages := d.sent()
		require.Len(t, messages, 2)
		assert.Equal(t, messages[0].IdempotencyKey, messages[1].IdempotencyKey)
		assert.Equal(t, []string{kept.ID}, messages[1].NotificationIDs)
		assert.Equal(t, ntfy.EmailSkipInactive, d.records.reasonOf(read.ID))
	})

	parallel(t, "an at-least-once resend sends nothing once every notification is read", func(t *testing.T) {
		d := newDispatchEnv(t, factory)
		one := d.publish("alice")
		two := d.publish("alice")
		d.clock.advance(10 * time.Minute)

		ctx, cancel := context.WithCancel(t.Context())

		d.setOnSend(func(ntfy.EmailMessage) { cancel() })

		stopping := d.dispatcher("stopping", ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce))
		_, err := stopping.Dispatch(ctx)
		require.NoError(t, err)
		require.Len(t, d.sent(), 1)

		d.setOnSend(nil)

		_, err = d.svc.MarkRead(t.Context(), "alice", one.ID, two.ID)
		require.NoError(t, err)

		d.clock.advance(ntfy.DefaultEmailLease + time.Minute)

		next := d.dispatcher("next", ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce))
		result, err := next.Dispatch(t.Context())
		require.NoError(t, err)
		assert.Zero(t, result.Sent)
		assert.Equal(t, 2, result.SkippedInactive)
		assert.Len(t, d.sent(), 1, "nothing is sent a second time")
	})

	parallel(t, "an at-least-once resend that fails stays in doubt under its key", func(t *testing.T) {
		d := newDispatchEnv(t, factory)
		d.publish("alice")
		d.clock.advance(10 * time.Minute)

		ctx, cancel := context.WithCancel(t.Context())

		d.setOnSend(func(ntfy.EmailMessage) { cancel() })

		stopping := d.dispatcher("stopping", ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce))
		_, err := stopping.Dispatch(ctx)
		require.NoError(t, err)

		d.setOnSend(nil)
		d.clock.advance(ntfy.DefaultEmailLease + time.Minute)
		newer := d.publish("alice")

		d.setSendErr(func(ntfy.EmailMessage) error { return errors.New("connection refused") })

		next := d.dispatcher("next", ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce))
		failed, err := next.Dispatch(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, failed.Retried)

		d.setSendErr(nil)
		d.clock.advance(time.Hour)

		_, err = next.Dispatch(t.Context())
		require.NoError(t, err)

		messages := d.sent()
		require.Len(t, messages, 4, "the original, the failed resend, the resend, and the newer one alone")
		assert.Equal(t, messages[0].IdempotencyKey, messages[2].IdempotencyKey, "the resend keeps the key")
		assert.Equal(t, messages[0].NotificationIDs, messages[2].NotificationIDs, "and absorbs nothing")
		assert.Equal(t, []string{newer.ID}, messages[3].NotificationIDs)
	})

	parallel(t, "a failed delivery records the library's own reason", func(t *testing.T) {
		d := newDispatchEnv(t, factory)
		n := d.publish("alice")
		d.clock.advance(10 * time.Minute)

		d.setSendErr(func(ntfy.EmailMessage) error {
			return errors.New("550 5.1.1 <alice@example.com>: Recipient address rejected")
		})

		result, err := d.dispatcher("failing", ntfy.WithEmailMaxAttempts(1)).Dispatch(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, result.Failed)
		assert.Zero(t, result.Unrecorded)

		assert.Equal(t, ntfy.EmailReasonSendFailed, d.records.reasonOf(n.ID))
		require.Len(t, d.reported(), 1)
		assert.Contains(t, d.reported()[0].Error(), "alice@example.com", "the host still receives the whole error")
	})

	parallel(t, "a store accepts host detail up to the documented bound", func(t *testing.T) {
		d := newDispatchEnv(t, factory)
		n := d.publish("alice")
		d.clock.advance(10 * time.Minute)

		d.setSendErr(func(ntfy.EmailMessage) error { return errors.New("connection refused") })

		// A multi-byte detail longer than the bound checks the store accepts a
		// reason of MaxEmailReasonBytes bytes, whatever its column counts. The
		// EmailStore port does not read a reason back, so this asserts what the
		// store accepted, not what it kept.
		detail := strings.Repeat("é", ntfy.MaxEmailReasonBytes)

		result, err := d.dispatcher("detailed",
			ntfy.WithEmailMaxAttempts(1),
			ntfy.WithEmailFailureDetail(func(context.Context, ntfy.EmailFailure) string { return detail }),
		).Dispatch(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, result.Failed)
		assert.Zero(t, result.Unrecorded, "the bounded reason fits the delivery record")

		assert.Len(t, d.records.reasonOf(n.ID), ntfy.MaxEmailReasonBytes)
	})

	// Construction never touches a store, so this proves the refusal is the same
	// on every store, rather than testing each store's column width.
	parallel(t, "an owner too long to store is refused before any traffic", func(t *testing.T) {
		d := newDispatchEnv(t, factory)

		_, err := ntfy.NewEmailDispatcher(d.svc, noopSuiteMailer, noopSuiteBook, noopSuiteTemplate,
			ntfy.WithEmailOwner(strings.Repeat("o", ntfy.MaxIdentifierBytes+1)))
		require.ErrorIs(t, err, ntfy.ErrConfiguration)
	})

	parallel(t, "a redeployed dispatcher does not resend", func(t *testing.T) {
		d := newDispatchEnv(t, factory)
		d.publish("alice")
		d.clock.advance(10 * time.Minute)

		before, err := d.dispatcher("before").Dispatch(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, before.Sent)

		d.clock.advance(ntfy.DefaultEmailLease + time.Hour)

		after, err := d.dispatcher("after").Dispatch(t.Context())
		require.NoError(t, err)
		assert.Zero(t, after.Claimed)
		assert.Len(t, d.sent(), 1)
	})
}

// dispatchClock is a clock a case moves by hand.
type dispatchClock struct{ now atomic.Pointer[time.Time] }

func (c *dispatchClock) Now() time.Time { return *c.now.Load() }

func (c *dispatchClock) advance(d time.Duration) {
	next := c.Now().Add(d)
	c.now.Store(&next)
}

// The ports a case that never dispatches still has to supply.
var (
	noopSuiteMailer = ntfy.MailerFunc(func(context.Context, ntfy.EmailMessage) error { return nil })
	noopSuiteBook   = ntfy.AddressBookFunc(func(_ context.Context, recipient string) (string, bool, error) {
		return recipient + "@example.com", true, nil
	})
	noopSuiteTemplate = ntfy.EmailTemplateFunc(func(context.Context, ntfy.EmailBatch) (ntfy.EmailContent, error) {
		return ntfy.EmailContent{Subject: "s", TextBody: "b"}, nil
	})
)

// recordingEmailStore decorates a store so that its email records fail once
// their context is cancelled, as a store talking to a database does, so that a
// cancelled pass behaves identically on every store. It keeps every record the
// store accepted, so a case can assert what reached the delivery record on any
// store alike: the EmailStore port does not read a reason back.
type recordingEmailStore struct {
	ntfy.Store
	ntfy.EmailStore

	mu      sync.Mutex
	records []ntfy.EmailRecord
}

func (s *recordingEmailStore) RecordEmails(ctx context.Context, record ntfy.EmailRecord) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	changed, err := s.EmailStore.RecordEmails(ctx, record)
	if err == nil && changed > 0 {
		s.mu.Lock()
		s.records = append(s.records, record)
		s.mu.Unlock()
	}

	return changed, err
}

// reasonOf returns the reason last recorded for a notification.
func (s *recordingEmailStore) reasonOf(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := len(s.records) - 1; i >= 0; i-- {
		if slices.Contains(s.records[i].IDs, id) {
			return s.records[i].Reason
		}
	}

	return ""
}

// dispatchEnv is one dispatch case's service and the ports it observes.
type dispatchEnv struct {
	t       *testing.T
	clock   *dispatchClock
	svc     *ntfy.Service
	records *recordingEmailStore

	mu       sync.Mutex
	messages []ntfy.EmailMessage
	errs     []error
	onSend   func(ntfy.EmailMessage)
	sendErr  func(ntfy.EmailMessage) error
	seq      int
}

func newDispatchEnv(t *testing.T, factory EmailFactory) *dispatchEnv {
	t.Helper()

	store := factory(t)
	clock := &dispatchClock{}
	start := base
	clock.now.Store(&start)

	records := &recordingEmailStore{Store: store, EmailStore: store}

	svc, err := ntfy.New(records, ntfy.WithClock(clock))
	require.NoError(t, err)

	return &dispatchEnv{t: t, clock: clock, svc: svc, records: records}
}

// setOnSend replaces the hook the mailer runs on every send.
func (d *dispatchEnv) setOnSend(onSend func(ntfy.EmailMessage)) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.onSend = onSend
}

// setSendErr makes the mailer fail every send with what sendErr returns.
func (d *dispatchEnv) setSendErr(sendErr func(ntfy.EmailMessage) error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.sendErr = sendErr
}

// publish publishes a notification for a recipient on a subject of its own.
func (d *dispatchEnv) publish(recipient string) ntfy.Notification {
	d.t.Helper()

	d.mu.Lock()
	d.seq++
	id := d.seq
	d.mu.Unlock()

	result, err := d.svc.Publish(d.t.Context(), ntfy.Draft{
		Recipient: recipient, SourceID: fmt.Sprintf("event-%d", id), Subject: fmt.Sprintf("task-%d", id), Kind: "offer",
	})
	require.NoError(d.t, err)
	require.Len(d.t, result.Created, 1)

	return result.Created[0]
}

// dispatcher builds a dispatcher with an owner of its own over the case's
// service.
func (d *dispatchEnv) dispatcher(owner string, opts ...ntfy.EmailOption) *ntfy.EmailDispatcher {
	d.t.Helper()

	mailer := ntfy.MailerFunc(func(_ context.Context, message ntfy.EmailMessage) error {
		d.mu.Lock()
		d.messages = append(d.messages, message)
		onSend, sendErr := d.onSend, d.sendErr
		d.mu.Unlock()

		if onSend != nil {
			onSend(message)
		}

		if sendErr != nil {
			return sendErr(message)
		}

		return nil
	})

	book := ntfy.AddressBookFunc(func(_ context.Context, recipient string) (string, bool, error) {
		return recipient + "@example.com", true, nil
	})

	template := ntfy.EmailTemplateFunc(func(context.Context, ntfy.EmailBatch) (ntfy.EmailContent, error) {
		return ntfy.EmailContent{Subject: "work for you", TextBody: "see the application"}, nil
	})

	opts = append([]ntfy.EmailOption{
		ntfy.WithEmailOwner(owner),
		ntfy.WithEmailErrorHandler(func(_ context.Context, err error) {
			d.mu.Lock()
			defer d.mu.Unlock()

			d.errs = append(d.errs, err)
		}),
	}, opts...)

	dispatcher, err := ntfy.NewEmailDispatcher(d.svc, mailer, book, template, opts...)
	require.NoError(d.t, err)

	return dispatcher
}

// sent returns the messages sent so far.
func (d *dispatchEnv) sent() []ntfy.EmailMessage {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.messages)
}

// sentIDs lists every notification identifier a sent message covered.
func (d *dispatchEnv) sentIDs() []string {
	var out []string

	for _, message := range d.sent() {
		out = append(out, message.NotificationIDs...)
	}

	return out
}

// reported returns the errors reported so far.
func (d *dispatchEnv) reported() []error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.errs)
}
