package ntfy_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// ExampleNewEmailDispatcher wires email as docs/email.md describes: the host's
// address book, template and mailer, narrowed to the kinds that ask someone to
// act.
func ExampleNewEmailDispatcher() {
	start := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	now := start

	svc, _ := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithClock(ntfy.ClockFunc(func() time.Time { return now })))

	addressBook := ntfy.AddressBookFunc(func(_ context.Context, recipient string) (string, bool, error) {
		return recipient + "@example.com", true, nil
	})
	template := ntfy.EmailTemplateFunc(func(_ context.Context, batch ntfy.EmailBatch) (ntfy.EmailContent, error) {
		return ntfy.EmailContent{Subject: fmt.Sprintf("%d tasks need you", len(batch.Notifications))}, nil
	})
	mailer := ntfy.MailerFunc(func(_ context.Context, message ntfy.EmailMessage) error {
		fmt.Println(message.To, "-", message.Subject)

		return nil
	})

	dispatcher, err := ntfy.NewEmailDispatcher(svc, mailer, addressBook, template,
		ntfy.WithEmailKinds("offer", "assigned"))
	if err != nil {
		fmt.Println(err)

		return
	}

	ctx := context.Background()

	_, _ = svc.Publish(ctx,
		ntfy.Draft{Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer"},
		ntfy.Draft{Recipient: "alice", SourceID: "event-2", Subject: "task-2", Kind: "assigned"},
		ntfy.Draft{Recipient: "alice", SourceID: "event-3", Subject: "task-3", Kind: "taken"},
	)

	now = start.Add(ntfy.DefaultEmailGraceDelay + time.Minute)

	result, _ := dispatcher.Dispatch(ctx)
	fmt.Println("sent", result.Sent, "skipped", result.SkippedFiltered)

	// Output:
	// alice@example.com - 2 tasks need you
	// sent 2 skipped 1
}

// TestTheEmailDocumentMatchesTheImplementation keeps docs/email.md from drifting
// away from the code: every default it states must be the value the code
// applies, every option it names must exist, and every stated limit must be
// there.
func TestTheEmailDocumentMatchesTheImplementation(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("docs/email.md")
	require.NoError(t, err, "docs/email.md is where email is documented")

	document := "\n" + string(raw)

	type testCase struct {
		name    string
		section string
		needles []string
	}

	cases := []testCase{
		{
			name:    "wiring and ports",
			section: "Wiring",
			needles: []string{
				funcName(ntfy.NewEmailDispatcher), "AddressBook", "EmailTemplate", "Mailer", "EmailFilter",
				"ErrMailRejected", "ErrMailInDoubt", "EmailSchema", "VerifyEmailSchema", "ddl/email",
			},
		},
		{
			name:    "defaults and options",
			section: "Defaults and overrides",
			needles: []string{
				"5 minutes", "24 hours", strconv.Itoa(ntfy.DefaultEmailBatchLimit), strconv.Itoa(ntfy.DefaultEmailClaimLimit),
				strconv.Itoa(ntfy.DefaultEmailMaxAttempts), "1 minute", "1 hour", "20%",
				funcName(ntfy.WithEmailGraceDelay), funcName(ntfy.WithoutEmailGraceDelay), funcName(ntfy.WithEmailMaxLag),
				funcName(ntfy.WithEmailBatchLimit), funcName(ntfy.WithEmailClaimLimit), funcName(ntfy.WithEmailLease),
				funcName(ntfy.WithEmailMaxAttempts), funcName(ntfy.WithEmailBackoff), funcName(ntfy.WithEmailKinds),
				funcName(ntfy.WithEmailFilter), funcName(ntfy.WithDeliveryGuarantee), funcName(ntfy.WithEmailOwner),
				funcName(ntfy.WithEmailErrorHandler), "AtMostOnce",
			},
		},
		{
			name:    "both delivery guarantees",
			section: "Delivery guarantees",
			needles: []string{"AtMostOnce", "AtLeastOnce", "IdempotencyKey", "ABANDONED", "never merged"},
		},
		{
			name:    "the stated limits",
			section: "Stated limits",
			needles: []string{"loses that message's email", "twice", "between the recheck and the send", "skip is final"},
		},
	}

	assert.Equal(t, 5*time.Minute, ntfy.DefaultEmailGraceDelay)
	assert.Equal(t, 24*time.Hour, ntfy.DefaultEmailMaxLag)
	assert.Equal(t, 5*time.Minute, ntfy.DefaultEmailLease)
	assert.Equal(t, time.Minute, ntfy.DefaultEmailBackoff)
	assert.Equal(t, time.Hour, ntfy.DefaultEmailBackoffCeiling)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			section := docSection(t, document, tc.section)

			for _, needle := range tc.needles {
				assert.Containsf(t, section, needle, "the %q section mentions %s", tc.section, needle)
			}
		})
	}
}
