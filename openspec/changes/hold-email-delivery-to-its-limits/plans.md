# Hold Email Delivery to Its Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the email dispatcher keep the bounds it promises:
- the attempt limit ends every send in doubt under `AtLeastOnce`;
- a message never sent is never abandoned as in doubt;
- one dispatcher never runs two passes at once;
- a pass stops when its context ends.

**Architecture:** Every change is in the core module's `email_dispatcher.go`, with no store change.
- Two `AtLeastOnce` paths go through the existing `hold`, which already fails spent candidates and keeps the rest in doubt under their key.
- A new message whose `SENDING` write came back short is released to `CLAIMED` instead of left `SENDING`.
- A one-slot channel gates `Dispatch`, per dispatcher.
- `ctx.Err()` is checked before claiming, between recipients and batches, and before every `SENDING` write.

**Tech Stack:**
- Go 1.26 (`GOTOOLCHAIN=go1.26.8`), with the core module on the standard library only. `testing/synctest` is used in one test.
- `stretchr/testify`.
- `testcontainers-go` through `sqlkittest.RunTestPostgres`, for one PostgreSQL test in `ntfy/sqlstore`.
- `golangci-lint` v2 and `openspec`.

**Spec:** `openspec/changes/hold-email-delivery-to-its-limits/`. Read these three before starting:
- `proposal.md`: the five audit findings, with the failing output re-proved on `main` `20967b3`;
- `design.md`: D1 in-doubt sends go through `hold`, D2 a failed lookup on a send in doubt counts an attempt, D3 release a cut-short claim, D4 one pass at a time, D5 stop at the context's end, D6 where the tests go;
- `specs/notification-email/spec.md`: three modified requirements and six new scenarios.

## Global Constraints

- **Export `GOTOOLCHAIN=go1.26.8` before any Go command.** A newer Go may be first on `PATH`. The `make` targets already pin it.
- **The core module (`github.com/kartaladev/ntfy`) imports only the standard library** in production code. Tests may use `testify`, `goleak` and `go.uber.org/mock`. Enforced by `.golangci.yml` depguard and `make split-check`. This change adds no production import. `testing/synctest` is standard library and test-only.
- **Each satellite module may import only `ntfy`, `sqlkit` and its own client library** (`make split-check`). The new `sqlstore` test imports only what `sqlstore`'s tests already import.
- **Tests follow the `table-test` skill:**
  - an `assert` closure on every case, never `want`/`wantErr`;
  - `t.Context()`, never `context.Background()`;
  - `require` only for preconditions.

  New dispatcher scenarios are rows of the existing `TestEmailDispatcherDispatch` table (`run`/`assert`). The two context rows build their own context, and say why in a comment (`design.md` D6). `TestEmailDispatcherSerialisesItsPasses` and `TestOneDispatcherNeverEmailsANotificationTwice` stand alone, each with a comment saying why.
- **Test doubles follow the `use-mockgen` skill.** This change needs none: the harness's `MailerFunc`, `AddressBookFunc`, `EmailTemplateFunc` and decorated memory store are the email tests' existing doubles.
- **External services follow the `use-testcontainers` skill.** Use `sqlkittest.RunTestPostgres(t)` with the existing `openSQL`, `stdsqlExecutor` and `harness.NewEmailStore`. Never write a container helper.
- **`.claude/rules/prove-errors-with-tests.md`:** every red step below is run, and its failure is compared with the expected output before any production edit. A red from compilation, a missing fixture or a container error does not count.
- **`.claude/rules/golang-tdd.md`:** red → green → refactor. The test and the code that satisfies it land in one commit. Consider `/simplify` after each green.
- **`.claude/rules/library-design.md`:** each behaviour's default and override are in `design.md` D1–D5. The overrides are `WithEmailMaxAttempts`, `WithDeliveryGuarantee`, `WithEmailLease`, several dispatchers with `WithEmailOwner`, and the host's own context. Add no new option.
- **`.claude/rules/performance-benchmark.md`:** this change makes no performance claim, so no benchmark is needed.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn.
- **`.claude/rules/gopls-navigation.md`:** navigate with `gopls` (`$(go env GOPATH)/bin/gopls` if it is not on `PATH`).
- **Done means `make all` and `make store-matrix` pass.** The dispatcher runs over every store in the matrix.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`. End each with:

  ```
  Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A
  ```

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `email_dispatcher.go` | `ntfy` | **Modify.** `send`'s in-doubt branch (Task 1); `resolveDoubt`'s lookup branch (Task 2); `errSendNotStarted`, `emailPass.release` and the short-write path in `send` (Task 3); the context checks in `Dispatch` and `send`, and `Dispatch`'s godoc (Task 4); the `passes` field, its construction, the gate in `Dispatch`, and the `WithEmailOwner` godoc (Task 5). |
| `email.go` | `ntfy` | **Modify.** The `EmailStore.ClaimEmails` godoc: one owner's claims never overlap (Task 5). |
| `email_dispatch_test.go` | `ntfy` | **Modify.** Five rows in `TestEmailDispatcherDispatch` (Tasks 1–4). A `claims` counter and a counting `ClaimEmails` on `recordingEmailStore`, and a new `TestEmailDispatcherSerialisesItsPasses` (Task 5). |
| `sqlstore/email_dispatch_test.go` | `ntfy/sqlstore` | **Create.** `TestOneDispatcherNeverEmailsANotificationTwice` on PostgreSQL (Task 5). |
| `example_email_test.go` | `ntfy` | **Modify.** Needles in `TestTheEmailDocumentMatchesTheImplementation` (Task 6). |
| `docs/email.md` | — | **Modify.** The outcome table, "One pass", "Delivery guarantees" and "Stated limits" (Task 6). |

**Shared-file note:** `email_dispatch_test.go`'s `TestEmailDispatcherDispatch` table is a likely conflict point with any email change in flight, such as `align-memory-store-with-sql`. Every row here is inserted directly **before** the row `"a failed claim is returned"`. Resolve a conflict by keeping both sets of rows.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: a send that stays in doubt fails at the limit | 1.1, 1.2 |
| Task 2: a failed lookup on a send in doubt counts an attempt | 2.1, 2.2 |
| Task 3: release a new message whose claim was cut short | 3.1, 3.2 |
| Task 4: stop a pass when its context ends | 4.1, 4.2 |
| Task 5: one pass at a time on one dispatcher | 5.1, 5.2, 5.3 |
| Task 6: document | 6.1, 6.2 |
| Task 7: verify | 7.1 |

Tasks 1–5 must run in this order:
- Task 3's test assumes Task 1's code and none of Task 4's.
- Task 5's gate relies on Task 4's check after acquiring it. Without that check, a `select` with a free slot and an ended context could pick either branch.

---

### Task 1: A send that stays in doubt fails at the attempt limit

**Files:**
- Modify: `email_dispatch_test.go` (insert a row before `"a failed claim is returned"` in `TestEmailDispatcherDispatch`)
- Modify: `email_dispatcher.go`, the `case errors.Is(err, ErrMailInDoubt):` branch of `(*emailPass).send`, around line 838

**Interfaces:**
- Consumes (existing):
  - `func (p *emailPass) hold(candidates []EmailCandidate, batch, reason string, cause error, countAttempt bool)`;
  - `const EmailReasonSendInDoubt = "send_in_doubt"`;
  - the test harness: `newDispatchHarness(t) *dispatchHarness`, `h.build(opts ...ntfy.EmailOption) *ntfy.EmailDispatcher`, `h.publish(recipient, kind string) ntfy.Notification`, `h.pastGrace()`, `h.dispatch() (ntfy.DispatchResult, error)`, `h.sent() []ntfy.EmailMessage`, `h.clock.advance(time.Duration)`, `h.email.lastRecordOf(t, id) ntfy.EmailRecord`, and the field `h.send func(ctx context.Context, message ntfy.EmailMessage) error`.
- Produces: no new names.

- [ ] **Step 1: Write the failing row**

Insert into the `cases` slice of `TestEmailDispatcherDispatch`, directly before the row whose `name` is `"a failed claim is returned"`:

```go
		{
			name: "at least once: a send that stays in doubt fails at the attempt limit",
			run: func(t *testing.T, h *dispatchHarness) (ntfy.DispatchResult, error) {
				h.build(ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce), ntfy.WithEmailMaxAttempts(2))
				h.publish("alice", "offer")
				h.pastGrace()

				h.send = func(context.Context, ntfy.EmailMessage) error { return ntfy.ErrMailInDoubt }

				first, err := h.dispatch()
				require.NoError(t, err)
				require.Equal(t, ntfy.DispatchResult{Claimed: 1, Retried: 1}, first, "the first send stays in doubt")

				h.clock.advance(ntfy.DefaultEmailLease + time.Minute)

				return h.dispatch()
			},
			assert: func(t *testing.T, h *dispatchHarness, result ntfy.DispatchResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, ntfy.DispatchResult{Claimed: 1, Failed: 1}, result, "the send that reaches the limit fails")

				messages := h.sent()
				require.Len(t, messages, 2)
				assert.Equal(t, messages[0].IdempotencyKey, messages[1].IdempotencyKey)

				record := h.email.lastRecordOf(t, messages[0].NotificationIDs[0])
				assert.Equal(t, ntfy.EmailStatusFailed, record.Status)
				assert.Equal(t, ntfy.EmailReasonSendInDoubt, record.Reason)

				h.clock.advance(ntfy.DefaultEmailLease + time.Minute)

				later, err := h.dispatch()
				require.NoError(t, err)
				assert.Equal(t, ntfy.DispatchResult{}, later, "and is never claimed again")
				assert.Len(t, h.sent(), 2)
			},
		},
```

- [ ] **Step 2: Run it and watch it fail for the stated reason**

Run: `go test -run 'TestEmailDispatcherDispatch/at_least_once:_a_send_that_stays_in_doubt_fails_at_the_attempt_limit' -count=1 .`

Expected: FAIL. This was observed on 2026-09-28:

```
Error:    Not equal:
          expected: ntfy.DispatchResult{Claimed:1, ..., Retried:0, Failed:1, ...}
          actual  : ntfy.DispatchResult{Claimed:1, ..., Retried:1, Failed:0, ...}
Messages: the send that reaches the limit fails
Error:    Not equal:  expected: "FAILED"  actual  : "SENDING"
Error:    Not equal:  expected: "send_in_doubt"  actual  : ""
Error:    Not equal:  expected: ntfy.DispatchResult{Claimed:0, ...}  actual  : ntfy.DispatchResult{Claimed:1, ..., Retried:1, ...}
Messages: and is never claimed again
```

If it fails on compilation or on the `require.Equal` in `run`, stop: the harness has drifted from this plan.

- [ ] **Step 3: Route the in-doubt branch through `hold`**

In `email_dispatcher.go`, replace:

```go
	case errors.Is(err, ErrMailInDoubt):
		// Left SENDING under this batch, so a later pass resends the same message
		// once the lease lapses.
		p.result.Retried += len(live)
		p.report(recipient, batch, err)
```

with:

```go
	case errors.Is(err, ErrMailInDoubt):
		// Left SENDING under this batch, so a later pass resends the same message
		// once the lease lapses, until the attempt that reaches the limit fails
		// it. The SENDING record already counted this attempt.
		for i := range live {
			live[i].Attempts++
		}

		p.hold(live, batch, EmailReasonSendInDoubt, err, false)
```

- [ ] **Step 4: Run it and the dispatcher suite, and watch them pass**

Run: `go test -run 'TestEmailDispatcher' -count=1 .`
Expected: PASS. The row "at least once: ErrMailInDoubt resends the same message once its lease lapses" still passes, because the default limit of 5 keeps its candidate.

- [ ] **Step 5: Commit**

```bash
git add email_dispatcher.go email_dispatch_test.go
git commit -m "End a send that stays in doubt at the attempt limit

Under AtLeastOnce, a send the Mailer answered with ErrMailInDoubt stayed
SENDING and was resent on every lease lapse, whatever the attempt limit.
It now goes through hold, which fails it at the limit as send_in_doubt.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 2: A failed lookup on a send in doubt counts an attempt

**Files:**
- Modify: `email_dispatch_test.go` (insert a row before `"a failed claim is returned"`)
- Modify: `email_dispatcher.go`, the `case err != nil:` branch of `(*emailPass).resolveDoubt`, around line 705

**Interfaces:**
- Consumes (existing):
  - `hold` as in Task 1;
  - `const EmailReasonLookupFailed = "lookup_failed"`;
  - `errTransient`, declared at the top of `TestEmailDispatcherDispatch`;
  - the harness field `h.lookup func(recipient string) error`.
- Produces: no new names.

- [ ] **Step 1: Write the failing row**

Insert before `"a failed claim is returned"`:

```go
		{
			name: "at least once: a lookup that keeps failing on a send in doubt fails at the attempt limit",
			run: func(t *testing.T, h *dispatchHarness) (ntfy.DispatchResult, error) {
				h.build(ntfy.WithDeliveryGuarantee(ntfy.AtLeastOnce), ntfy.WithEmailMaxAttempts(3))
				h.publish("alice", "offer")
				h.pastGrace()

				h.send = func(context.Context, ntfy.EmailMessage) error { return ntfy.ErrMailInDoubt }

				first, err := h.dispatch()
				require.NoError(t, err)
				require.Equal(t, ntfy.DispatchResult{Claimed: 1, Retried: 1}, first, "the first send stays in doubt")

				h.lookup = func(string) error { return errTransient }

				h.clock.advance(ntfy.DefaultEmailLease + time.Minute)

				second, err := h.dispatch()
				require.NoError(t, err)
				assert.Equal(t, ntfy.DispatchResult{Claimed: 1, Retried: 1}, second, "a failed lookup keeps it in doubt")

				h.clock.advance(ntfy.DefaultEmailLease + time.Minute)

				return h.dispatch()
			},
			assert: func(t *testing.T, h *dispatchHarness, result ntfy.DispatchResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, ntfy.DispatchResult{Claimed: 1, Failed: 1}, result, "the lookup that reaches the limit fails it")

				messages := h.sent()
				require.Len(t, messages, 1)

				record := h.email.lastRecordOf(t, messages[0].NotificationIDs[0])
				assert.Equal(t, ntfy.EmailStatusFailed, record.Status)
				assert.Equal(t, ntfy.EmailReasonLookupFailed, record.Reason)
			},
		},
```

- [ ] **Step 2: Run it and watch it fail for the stated reason**

Run: `go test -run 'TestEmailDispatcherDispatch/at_least_once:_a_lookup_that_keeps_failing' -count=1 .`

Expected: FAIL. This was observed on 2026-09-28:

```
Error:    Not equal:
          expected: ntfy.DispatchResult{Claimed:1, ..., Retried:0, Failed:1, ...}
          actual  : ntfy.DispatchResult{Claimed:1, ..., Retried:1, Failed:0, ...}
Messages: the lookup that reaches the limit fails it
Error:    Not equal:  expected: "FAILED"  actual  : "SENDING"
Error:    Not equal:  expected: "lookup_failed"  actual  : ""
```

- [ ] **Step 3: Hold the send in doubt with the attempt counted**

In `resolveDoubt`, replace:

```go
	case err != nil:
		// The record stays SENDING, so the batch keeps its key; the lease lapses
		// and a later pass tries again.
		p.result.Retried += len(candidates)
		p.report(recipient, batch, err)
```

with:

```go
	case err != nil:
		// Held SENDING under its key with this attempt counted, so the lease
		// lapses and a later pass tries again, until the attempt that reaches
		// the limit fails it.
		p.hold(candidates, batch, EmailReasonLookupFailed, err, true)
```

- [ ] **Step 4: Run the dispatcher suite and watch it pass**

Run: `go test -run 'TestEmailDispatcher' -count=1 .`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add email_dispatcher.go email_dispatch_test.go
git commit -m "Count a failed lookup on a send in doubt as an attempt

Under AtLeastOnce, an address lookup that failed while settling a send in
doubt wrote nothing, so the delivery was claimed again forever. It is now
held SENDING under its key with the attempt counted, and fails at the
limit as lookup_failed.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 3: Release a new message whose claim was cut short

**Files:**
- Modify: `email_dispatch_test.go` (insert a row before `"a failed claim is returned"`)
- Modify: `email_dispatcher.go`: a new package variable beside `errSendStopped` (around line 42); a new method after `(*emailPass).record`; the `SENDING` write in `(*emailPass).send`

**Interfaces:**
- Consumes (existing):
  - `func (p *emailPass) record(candidates []EmailCandidate, record EmailRecord) bool`;
  - `func (p *emailPass) report(recipient, batch string, err error)`;
  - `func candidateIDs(candidates []EmailCandidate) []string`;
  - `EmailStore.RecordEmails(ctx context.Context, record EmailRecord) (int64, error)`;
  - the test helper `ids(notifications ...ntfy.Notification) []string`.
- Produces:
  - `var errSendNotStarted error`, unexported;
  - `func (p *emailPass) release(recipient, batch string, candidates []EmailCandidate)`.

- [ ] **Step 1: Write the failing row**

Insert before `"a failed claim is returned"`:

```go
		{
			name: "a pass that loses part of its lease before sending releases the rest unsent, not in doubt",
			run: func(t *testing.T, h *dispatchHarness) (ntfy.DispatchResult, error) {
				older := h.publish("alice", "offer")
				h.publish("alice", "offer")
				h.pastGrace()

				other := h.build(ntfy.WithEmailOwner("dispatcher-b"), ntfy.WithEmailClaimLimit(1))
				first := h.build(ntfy.WithEmailLease(time.Minute))

				// While the first pass looks alice's address up, its lease lapses and
				// the other dispatcher takes, and sends, the older notification.
				var interleaved atomic.Bool

				h.lookup = func(string) error {
					if interleaved.CompareAndSwap(false, true) {
						h.clock.advance(2 * time.Minute)

						_, err := other.Dispatch(t.Context())
						require.NoError(t, err)
					}

					return nil
				}

				_, err := first.Dispatch(t.Context())
				require.NoError(t, err)
				require.Len(t, h.sent(), 1, "only the other dispatcher sent")
				require.Equal(t, ids(older), h.sent()[0].NotificationIDs)

				h.clock.advance(ntfy.DefaultEmailLease + time.Minute)

				return other.Dispatch(t.Context())
			},
			assert: func(t *testing.T, h *dispatchHarness, result ntfy.DispatchResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, ntfy.DispatchResult{Claimed: 1, Sent: 1, Messages: 1}, result,
					"the newer notification is emailed, not abandoned as in doubt")

				messages := h.sent()
				require.Len(t, messages, 2)
				assert.Equal(t, ntfy.EmailStatusSent, h.email.lastRecordOf(t, messages[1].NotificationIDs[0]).Status)
			},
		},
```

- [ ] **Step 2: Run it and watch it fail for the stated reason**

Run: `go test -run 'TestEmailDispatcherDispatch/a_pass_that_loses_part_of_its_lease' -count=1 .`

Expected: FAIL. This was observed on 2026-09-28:

```
Error:    Not equal:
          expected: ntfy.DispatchResult{Claimed:1, Sent:1, Messages:1, ..., Abandoned:0, ...}
          actual  : ntfy.DispatchResult{Claimed:1, Sent:0, Messages:0, ..., Abandoned:1, ...}
Messages: the newer notification is emailed, not abandoned as in doubt
Error:    "[{alice@example.com 1 notifications body  map[] ... alice [...]}]" should have 2 item(s), but has 1
```

If a `require` in `run` fails instead, the interleaving did not happen, and the test proves nothing. Check that `other` was built with claim limit 1 and `first` with a one-minute lease.

- [ ] **Step 3: Add the error and the release**

Beside `errSendStopped` in `email_dispatcher.go`:

```go
// errSendNotStarted is what the error handler hears when a new message could
// not be recorded as sending on every notification it covers, because the pass
// lost part of its lease: the message is not sent, and what the pass still held
// is released to a later pass.
var errSendNotStarted = errors.New("the message could not be recorded as sending on every notification it covers, " +
	"so it was not sent; what the pass still held is released to a later pass")
```

After `(*emailPass).record`:

```go
// release returns a new message's candidates that the pass still holds to later
// passes as unsent, after its SENDING record could not be written on all of
// them, so that a message never sent is never taken for one in doubt. A
// candidate another dispatcher took over is not changed. The attempt the
// SENDING record counted stays counted. It writes directly rather than through
// record, which already counted the lost candidates as unrecorded.
func (p *emailPass) release(recipient, batch string, candidates []EmailCandidate) {
	_, err := p.d.store.RecordEmails(p.ctx, EmailRecord{
		Owner: p.d.owner, IDs: candidateIDs(candidates), Status: EmailStatusClaimed, At: p.now,
	})
	if err != nil {
		p.report(recipient, batch, fmt.Errorf("release: %w", err))
	}

	p.report(recipient, batch, errSendNotStarted)
}
```

- [ ] **Step 4: Release on a short `SENDING` write of a new message**

In `send`, replace:

```go
	// Committed before the send, so that a pass that stops mid-send leaves the
	// message in doubt rather than unsent.
	if !p.record(live, EmailRecord{Status: EmailStatusSending, BatchID: batch, Attempt: true}) {
		return
	}
```

with:

```go
	// Committed before the send, so that a pass that stops mid-send leaves the
	// message in doubt rather than unsent. A new message that could not be
	// recorded on every notification is not sent, and what the pass still holds
	// is released as unsent; a resend was in doubt already and stays so.
	if !p.record(live, EmailRecord{Status: EmailStatusSending, BatchID: batch, Attempt: true}) {
		if !resend {
			p.release(recipient, batch, live)
		}

		return
	}
```

- [ ] **Step 5: Run the dispatcher and memory email suites, and watch them pass**

Run: `go test -run 'TestEmailDispatcher|TestMemoryStoreEmailConformance' -count=1 .`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add email_dispatcher.go email_dispatch_test.go
git commit -m "Release a new message whose claim was cut short instead of abandoning it

A pass that lost part of its lease wrote SENDING on what it still held and
returned without sending. The next pass took that for a send in doubt, and
under AtMostOnce abandoned an email that was never sent. What the pass
still holds is now released to CLAIMED, and the error handler hears of it.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 4: Stop a pass when its context ends

**Files:**
- Modify: `email_dispatch_test.go` (insert two rows before `"a failed claim is returned"`)
- Modify: `email_dispatcher.go`: `(*EmailDispatcher).Dispatch` and its godoc (around lines 386–450), and `(*emailPass).send` before the `SENDING` write

**Interfaces:**
- Consumes (existing): `emailPass.ctx context.Context`, `emailPass.result DispatchResult`, and the harness fields `h.addresses map[string]string` and `h.dispatcher *ntfy.EmailDispatcher`.
- Produces: `Dispatch`'s signature is unchanged, `func (d *EmailDispatcher) Dispatch(ctx context.Context) (DispatchResult, error)`. It now also returns `ctx.Err()`, unwrapped, with the partial result.

- [ ] **Step 1: Write the two failing rows**

Insert before `"a failed claim is returned"`:

```go
		{
			name: "a pass stops when its context ends, and a later pass emails what it had not reached",
			run: func(t *testing.T, h *dispatchHarness) (ntfy.DispatchResult, error) {
				h.addresses["carol"] = "carol@example.com"

				// Cancelled from inside the first send, which a ctx modifier applied
				// before the call cannot express.
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				var afterCancel atomic.Int32

				h.send = func(sendCtx context.Context, _ ntfy.EmailMessage) error {
					if sendCtx.Err() != nil {
						afterCancel.Add(1)

						return sendCtx.Err()
					}

					cancel()

					return nil
				}
				h.build()
				h.publish("alice", "offer")
				h.publish("bob", "offer")
				h.publish("carol", "offer")
				h.pastGrace()

				stopped, err := h.dispatcher.Dispatch(ctx)
				assert.Zero(t, afterCancel.Load(), "the Mailer is not called once the pass's context has ended")
				assert.ErrorIs(t, err, context.Canceled, "the pass returns its context's error")
				assert.Equal(t, ntfy.DispatchResult{Claimed: 3, Sent: 1, Messages: 1}, stopped, "with what it did before")

				h.clock.advance(ntfy.DefaultEmailLease + time.Minute)

				return h.dispatch()
			},
			assert: func(t *testing.T, h *dispatchHarness, result ntfy.DispatchResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, ntfy.DispatchResult{Claimed: 2, Sent: 2, Messages: 2}, result)
				assert.Len(t, h.sent(), 3)
			},
		},
		{
			// The context is cancelled inside run so that the harness is built
			// first; the table has no ctx modifier.
			name: "a pass whose context has already ended claims nothing",
			run: func(t *testing.T, h *dispatchHarness) (ntfy.DispatchResult, error) {
				h.build()
				h.publish("alice", "offer")
				h.pastGrace()

				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				return h.dispatcher.Dispatch(ctx)
			},
			assert: func(t *testing.T, h *dispatchHarness, result ntfy.DispatchResult, err error) {
				require.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, ntfy.DispatchResult{}, result)
				assert.Empty(t, h.sent())
			},
		},
```

- [ ] **Step 2: Run them and watch both fail for the stated reason**

Run: `go test -run 'TestEmailDispatcherDispatch/a_pass_(stops_when|whose_context)' -count=1 .`

Expected: FAIL. This was observed on 2026-09-28:

```
--- FAIL: .../a_pass_stops_when_its_context_ends,_and_a_later_pass_emails_what_it_had_not_reached
    Error:    Should be zero, but was 2
    Messages: the Mailer is not called once the pass's context has ended
    Error:    Expected error with "context canceled" in chain but got nil.
    Error:    Not equal: ... actual  : ntfy.DispatchResult{Claimed:3, Sent:1, Messages:1, ..., Retried:2, ...}
    Error:    "[... bob ... carol ... bob ... carol ...]" should have 3 item(s), but has 5
--- FAIL: .../a_pass_whose_context_has_already_ended_claims_nothing
    Error:    Expected error with "context canceled" in chain but got nil.
```

- [ ] **Step 3: Check the context in `Dispatch`**

Replace the body of `Dispatch` from its first line down to the purge. The result:

```go
func (d *EmailDispatcher) Dispatch(ctx context.Context) (DispatchResult, error) {
	if err := ctx.Err(); err != nil {
		return DispatchResult{}, err
	}

	now := normalizeTime(d.service.clock.Now())

	candidates, err := d.store.ClaimEmails(ctx, EmailClaim{
		Now: now, Owner: d.owner, Lease: d.lease,
		CreatedUntil: now.Add(-d.grace), CreatedFrom: now.Add(-d.maxLag), Limit: d.claim,
	})
	if err != nil {
		return DispatchResult{}, err
	}

	pass := &emailPass{d: d, ctx: ctx, now: now}
	pass.result.Claimed = len(candidates)

	var (
		doubts      = map[string][]EmailCandidate{}
		batches     []string
		byRecipient = map[string][]EmailCandidate{}
		recipients  []string
	)

	// Candidates arrive oldest first; grouping keeps that order within a group.
	for _, c := range candidates {
		if c.Status == EmailStatusSending {
			if _, seen := doubts[c.BatchID]; !seen {
				batches = append(batches, c.BatchID)
			}

			doubts[c.BatchID] = append(doubts[c.BatchID], c)

			continue
		}

		recipient := c.Notification.Recipient
		if _, seen := byRecipient[recipient]; !seen {
			recipients = append(recipients, recipient)
		}

		byRecipient[recipient] = append(byRecipient[recipient], c)
	}

	// A pass whose context ends stops where it is: what it has not reached stays
	// claimed, with no attempt counted, until the lease lapses.
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return pass.result, err
		}

		pass.resolveDoubt(doubts[batch])
	}

	for _, recipient := range recipients {
		if err := ctx.Err(); err != nil {
			return pass.result, err
		}

		pass.deliver(recipient, byRecipient[recipient])
	}

	if err := ctx.Err(); err != nil {
		return pass.result, err
	}

	purged, err := d.store.PurgeEmailRecords(ctx, d.claim)
	pass.result.Purged = purged

	if err != nil {
		d.onError(ctx, fmt.Errorf("ntfy: purge email records: %w", err))
	}

	return pass.result, nil
}
```

- [ ] **Step 4: Start no send once the context has ended**

In `send`, directly before the comment `// Committed before the send, ...` that Task 3 left, insert:

```go
	// A pass whose context ended starts no send: the candidates stay claimed
	// under the lease, with no attempt counted, for a later pass.
	if p.ctx.Err() != nil {
		return
	}

```

- [ ] **Step 5: Update `Dispatch`'s godoc**

Replace:

```go
// It returns an error only when claiming fails. Every later failure is counted
// in the result and reported to the email error handler, and never stops the
// pass.
```

with:

```go
// It returns an error when claiming fails, and when ctx ends. A pass whose
// context has already ended claims nothing; one whose context ends mid-pass
// starts no further send and returns what it did so far with ctx's error,
// leaving what it had not reached claimed for a later pass to take over once
// the lease lapses. Every other failure is counted in the result and reported
// to the email error handler, and never stops the pass.
```

- [ ] **Step 6: Run the dispatcher suite and watch it pass**

Run: `go test -run 'TestEmailDispatcher' -count=1 .`
Expected: PASS. `TestEmailDispatcherRun`'s rows still pass: `Run` already returns `ctx.Err()` when `Dispatch` errs with the context ended.

- [ ] **Step 7: Commit**

```bash
git add email_dispatcher.go email_dispatch_test.go
git commit -m "Stop a dispatch pass when its context ends

A pass kept settling and sending after its context was cancelled, calling
the Mailer with the ended context and marking the rest RETRY. It now claims
nothing on an ended context, starts no send once it ends mid-pass, and
returns what it did with the context's error; what it had not reached is
taken over after the lease with no attempt spent.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 5: One pass at a time on one dispatcher

**Files:**
- Modify: `email_dispatch_test.go`: `recordingEmailStore` (around lines 41–61), and a new test function appended at the end of the file
- Create: `sqlstore/email_dispatch_test.go`
- Modify: `email_dispatcher.go`: the `EmailDispatcher` struct (around line 55); the `d := &EmailDispatcher{...}` literal in `NewEmailDispatcher` (around line 237); the start of `Dispatch`; the `WithEmailOwner` godoc (around line 166)
- Modify: `email.go`: the `ClaimEmails` godoc in `EmailStore` (around line 134)

**Interfaces:**
- Consumes (existing):
  - in `sqlstore_test`: `openSQL(t *testing.T, driver, dsn string) *sql.DB`, `stdsqlExecutor(t *testing.T, db *sql.DB, dialect sqlkit.Dialect) sqlkit.Executor`, `harness.NewEmailStore(t, executor) *sqlstore.Store`;
  - `sqlkittest.RunTestPostgres(t) string`;
  - `ntfy.ClockFunc`, `ntfy.MailerFunc`, `ntfy.AddressBookFunc`, `ntfy.EmailTemplateFunc`.
- Produces:
  - the field `passes chan struct{}` on `EmailDispatcher`;
  - the test-only field `claims atomic.Int32` on `recordingEmailStore`, and `func (s *recordingEmailStore) ClaimEmails(ctx context.Context, claim ntfy.EmailClaim) ([]ntfy.EmailCandidate, error)`.

- [ ] **Step 1: Count claims in the harness**

In `email_dispatch_test.go`, add a field to `recordingEmailStore`:

```go
	records []ntfy.EmailRecord
	dropped atomic.Bool
	claims  atomic.Int32
}

// ClaimEmails counts the claims made through the store.
func (s *recordingEmailStore) ClaimEmails(ctx context.Context, claim ntfy.EmailClaim) ([]ntfy.EmailCandidate, error) {
	s.claims.Add(1)

	return s.EmailStore.ClaimEmails(ctx, claim)
}
```

The first two field lines above already exist; add the third, and the method after the struct.

- [ ] **Step 2: Write the failing memory-store test**

Add `"testing/synctest"` to the file's imports, then append:

```go
// TestEmailDispatcherSerialisesItsPasses stands apart from the Dispatch table
// because it runs inside a synctest bubble, so that "the second pass is
// waiting" is a durable state rather than a sleep.
func TestEmailDispatcherSerialisesItsPasses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newDispatchHarness(t)

		entered := make(chan struct{})
		release := make(chan struct{})

		h.send = func(context.Context, ntfy.EmailMessage) error {
			close(entered)
			<-release

			return nil
		}
		h.build()
		h.publish("alice", "offer")
		h.pastGrace()

		var (
			wg      sync.WaitGroup
			results [2]ntfy.DispatchResult
			errs    [2]error
		)

		wg.Go(func() { results[0], errs[0] = h.dispatch() })
		<-entered

		wg.Go(func() { results[1], errs[1] = h.dispatch() })
		synctest.Wait()

		assert.Equal(t, int32(1), h.email.claims.Load(), "a second pass on one dispatcher waits for the first to finish")

		close(release)
		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		assert.Equal(t, int32(2), h.email.claims.Load(), "and then claims")
		assert.Equal(t, ntfy.DispatchResult{Claimed: 1, Sent: 1, Messages: 1}, results[0])
		assert.Equal(t, ntfy.DispatchResult{}, results[1])
		assert.Len(t, h.sent(), 1)
	})
}
```

- [ ] **Step 3: Run it and watch it fail for the stated reason**

Run: `go test -run 'TestEmailDispatcherSerialisesItsPasses' -count=1 .`

Expected: FAIL. This was observed on 2026-09-28:

```
Error:    Not equal:
          expected: 1
          actual  : 2
Messages: a second pass on one dispatcher waits for the first to finish
```

A failure reading `deadlock: main bubble goroutine has exited but blocked goroutines remain` means something started a goroutine outside the test. Investigate with `superpowers:systematic-debugging` rather than adding a sleep.

- [ ] **Step 4: Write the failing PostgreSQL test**

Create `sqlstore/email_dispatch_test.go`:

```go
package sqlstore_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/sqlstore/internal/harness"
	"github.com/kartaladev/sqlkit"
	"github.com/kartaladev/sqlkit/sqlkittest"
)

// TestOneDispatcherNeverEmailsANotificationTwice calls Dispatch twice at once
// on one dispatcher, under a clock that gives both passes the same reading. On
// PostgreSQL two claims by one owner at one instant can return the same
// notifications, so the dispatcher must not let its passes overlap. It is the
// only case in this file, so it is not a table.
func TestOneDispatcherNeverEmailsANotificationTwice(t *testing.T) {
	t.Parallel()

	executor := stdsqlExecutor(t, openSQL(t, "postgres", sqlkittest.RunTestPostgres(t)), sqlkit.PostgreSQL)
	store := harness.NewEmailStore(t, executor)

	start := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)

	var (
		clockMu sync.Mutex
		at      = start
	)

	clock := ntfy.ClockFunc(func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()

		return at
	})
	setClock := func(to time.Time) {
		clockMu.Lock()
		defer clockMu.Unlock()

		at = to
	}

	svc, err := ntfy.New(store, ntfy.WithClock(clock))
	require.NoError(t, err)

	var (
		sendMu sync.Mutex
		sends  = map[string]int{}
	)

	mailer := ntfy.MailerFunc(func(_ context.Context, m ntfy.EmailMessage) error {
		sendMu.Lock()
		defer sendMu.Unlock()

		for _, id := range m.NotificationIDs {
			sends[id]++
		}

		return nil
	})
	book := ntfy.AddressBookFunc(func(context.Context, string) (string, bool, error) { return "x@example.com", true, nil })
	template := ntfy.EmailTemplateFunc(func(context.Context, ntfy.EmailBatch) (ntfy.EmailContent, error) {
		return ntfy.EmailContent{Subject: "s", TextBody: "b"}, nil
	})

	dispatcher, err := ntfy.NewEmailDispatcher(svc, mailer, book, template)
	require.NoError(t, err)

	for i := range 20 {
		setClock(start.Add(time.Duration(i) * time.Hour))

		for r := range 3 {
			_, err := svc.Publish(t.Context(), ntfy.Draft{
				Recipient: fmt.Sprintf("user-%d-%d", i, r), SourceID: fmt.Sprintf("src-%d-%d", i, r),
				Subject: fmt.Sprintf("subject-%d-%d", i, r), Kind: "offer",
			})
			require.NoError(t, err)
		}

		setClock(start.Add(time.Duration(i)*time.Hour + ntfy.DefaultEmailGraceDelay + time.Minute))

		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() {
				_, err := dispatcher.Dispatch(t.Context())
				assert.NoError(t, err)
			})
		}

		wg.Wait()
	}

	var twice []string

	for id, n := range sends {
		if n > 1 {
			twice = append(twice, id)
		}
	}

	assert.Len(t, sends, 60, "every notification is emailed")
	assert.Empty(t, twice, "notifications emailed more than once by one dispatcher's overlapping passes")
}
```

- [ ] **Step 5: Run it and watch it fail for the stated reason**

Run: `cd sqlstore && go test -run 'TestOneDispatcherNeverEmailsANotificationTwice' -count=1 .`

Expected: FAIL. On 2026-09-28 the run listed 48 of the 60 notification IDs:

```
--- FAIL: TestOneDispatcherNeverEmailsANotificationTwice
    Error:    Should be empty, but was [01a0e5c4-6e05-7000-... 01a0e5c4-6e4f-7000-... ...]
    Messages: notifications emailed more than once by one dispatcher's overlapping passes
```

The count varies with scheduling; it must be above zero. A container start failure does not count as red. Re-run it once Docker is available.

- [ ] **Step 6: Add the gate**

In the `EmailDispatcher` struct, after `detail`:

```go
	// passes holds one token while a pass runs, so that one dispatcher never
	// runs two passes at once: two claims by one owner must not overlap.
	passes chan struct{}
```

In `NewEmailDispatcher`, add to the `d := &EmailDispatcher{...}` literal, after the `detail:` line:

```go
		passes:  make(chan struct{}, 1),
```

At the top of `Dispatch`, before the `if err := ctx.Err()` check that Task 4 added:

```go
	// One pass at a time: a second call waits for the first, or gives up when
	// its context ends. The check below then covers a context that ended while
	// the slot was free, which select may not have noticed.
	select {
	case d.passes <- struct{}{}:
	case <-ctx.Done():
		return DispatchResult{}, ctx.Err()
	}

	defer func() { <-d.passes }()

```

Add a paragraph to `Dispatch`'s godoc, after its first paragraph:

```go
// A dispatcher runs one pass at a time: a call made while another is running
// waits for it, and returns ctx's error without claiming if ctx ends first.
// Passes in parallel need several dispatchers, each with its own owner.
```

- [ ] **Step 7: State the one-owner contract, and the parallel route**

In `email.go`, replace:

```go
	// ClaimEmails takes a lease on notifications due for email and returns them.
	// A notification another claim holds is never returned, so two dispatchers
	// never receive the same one.
```

with:

```go
	// ClaimEmails takes a lease on notifications due for email and returns them.
	// A notification another claim holds is never returned, so two dispatchers
	// never receive the same one. One owner's claims never overlap: a
	// dispatcher runs one pass at a time, so a store need not keep two
	// concurrent claims by one owner apart.
```

In `email_dispatcher.go`, replace the `WithEmailOwner` godoc:

```go
// WithEmailOwner replaces the generated owner, "email-" followed by a UUIDv7,
// that the dispatcher records in its leases. Two dispatchers must never share
// one. It must not be empty.
```

with:

```go
// WithEmailOwner replaces the generated owner, "email-" followed by a UUIDv7,
// that the dispatcher records in its leases. Two dispatchers must never share
// one. A dispatcher runs one pass at a time; a host that wants passes in
// parallel builds several dispatchers, each with its own owner. It must not be
// empty.
```

- [ ] **Step 8: Run both tests and the race detector, and watch them pass**

Run: `go test -race -run 'TestEmailDispatcher' -count=1 .`
Expected: PASS.

Run: `cd sqlstore && go test -run 'TestOneDispatcherNeverEmailsANotificationTwice' -count=1 .`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add email_dispatcher.go email.go email_dispatch_test.go sqlstore/email_dispatch_test.go
git commit -m "Run one dispatch pass at a time on one dispatcher

Two Dispatch calls on one dispatcher at one clock reading made two claims
under one owner and lease end. On PostgreSQL both read back the same
notifications, and 48 of 60 were emailed twice. Dispatch now waits for a
pass the same dispatcher is running, and the EmailStore contract says one
owner's claims never overlap.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 6: Document

**Files:**
- Modify: `example_email_test.go`, the `cases` of `TestTheEmailDocumentMatchesTheImplementation` (around lines 76–118)
- Modify: `docs/email.md`

**Interfaces:**
- Consumes (existing): `docSection(t, document, heading string) string`, which cuts from `"\n## " + heading + "\n"` to the next `"\n## "`.
- Produces: no code names.

- [ ] **Step 1: Add the failing needles**

In `TestTheEmailDocumentMatchesTheImplementation`:
- Add a case after `"defaults and options"`:

  ```go
  		{
  			name:    "one pass",
  			section: "One pass",
  			needles: []string{"one pass at a time", "its context ends"},
  		},
  ```

- Change the `"both delivery guarantees"` needles to:

  ```go
  			needles: []string{"AtMostOnce", "AtLeastOnce", "IdempotencyKey", "ABANDONED", "never merged", "subset", "attempt limit"},
  ```

- Append `"reaches the sender", "released",` to the `"the stated limits"` needles, after `ntfy.EmailReasonSendInDoubt,`.

- [ ] **Step 2: Run it and watch it fail on the missing needles**

Run: `go test -run 'TestTheEmailDocumentMatchesTheImplementation' -count=1 .`

Expected: FAIL. The failures read `the "One pass" section mentions one pass at a time`, `... mentions its context ends`, `the "Delivery guarantees" section mentions attempt limit`, `the "Stated limits" section mentions reaches the sender` and `... mentions released`.

- [ ] **Step 3: Update `docs/email.md`**

In "One pass", replace:

```markdown
`Dispatch` returns an error only when claiming fails. Every other failure is
counted in the `DispatchResult` and reported to the error handler, and never stops
the pass.
```

with:

```markdown
A dispatcher runs **one pass at a time**: a `Dispatch` called while the same
dispatcher is running one waits for it, or returns the context's error if its
context ends first. To run passes in parallel, build several dispatchers; each
has its own owner.

`Dispatch` returns an error when claiming fails, and when its context ends. A
pass stops when its context ends. If the context had already ended, it claims
nothing. Otherwise it starts no further send, and returns what it did so far
with the context's error. What it had not reached stays claimed; a later pass
takes it over once the lease lapses, with no attempt spent. Every other failure
is counted in the `DispatchResult` and reported to the error handler, and never
stops the pass.
```

In the outcome table, after the row beginning `` | `ErrMailInDoubt`, or a pass that stopped mid-send ``, add:

```markdown
| under `AtLeastOnce`, a send in doubt at the attempt limit: the sender answered `ErrMailInDoubt`, or the address lookup failed while settling it | `FAILED` (`send_in_doubt` or `lookup_failed`) | never |
```

In "Delivery guarantees", replace the end of the `AtLeastOnce` bullet:

```markdown
  being `ACTIVE` since. If none is left, nothing is sent. With a sender that deduplicates on
  the key, the email arrives once; with one that does not, it can arrive twice.
```

with:

```markdown
  being `ACTIVE` since. If none is left, nothing is sent. With a sender that deduplicates on
  the key, the email arrives once; with one that does not, it can arrive twice.
  Every send, and every failed address lookup while settling one, counts toward
  the attempt limit. The attempt that reaches the limit records `FAILED`, so a
  sender that keeps answering `ErrMailInDoubt` does not repeat a message forever.
```

At the end of "Stated limits", append:

```markdown
- **A context that ends between the pass's last check and a send still
  reaches the sender**, which receives the ended context. Its outcome is
  recorded as usual.
- **A message whose `SENDING` record could not be written on every
  notification is not sent.** That happens when the pass's lease lapsed and
  another dispatcher took some. What the pass still held is released to a
  later pass as unsent, never abandoned. The error handler hears of it, and
  the attempt it counted stays counted.
```

- [ ] **Step 4: Run it and watch it pass**

Run: `go test -run 'TestTheEmailDocumentMatchesTheImplementation|TestTheDocumentMatchesTheImplementation' -count=1 .`
Expected: PASS.

- [ ] **Step 5: Check the godoc reads right**

Run: `go doc github.com/kartaladev/ntfy.EmailDispatcher.Dispatch && go doc github.com/kartaladev/ntfy.WithEmailOwner && go doc github.com/kartaladev/ntfy.EmailStore`
Expected: each shows the text from Tasks 4 and 5.

- [ ] **Step 6: Commit**

```bash
git add docs/email.md example_email_test.go
git commit -m "Document the limits email delivery now holds to

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

---

### Task 7: Verify

**Files:** none changed, except `tasks.md` checkboxes and this plan's execution record.

**Interfaces:** none.

- [ ] **Step 1: Run the gates**

Run: `make all`
Expected: `lint`, `split-check` and `test` all pass.

Run: `make store-matrix`
Expected: every driver-and-dialect combination passes.

Run: `go test -race -count=1 ./...` in the root.
Expected: PASS.

- [ ] **Step 2: Record and tick**

Append an "Execution record" section to this file with the date, the commands and their outcome. Tick `tasks.md` 7.1. Any gate failure goes to `superpowers:systematic-debugging` first; it is not patched blind.

- [ ] **Step 3: Commit**

```bash
git add openspec/changes/hold-email-delivery-to-its-limits/tasks.md openspec/changes/hold-email-delivery-to-its-limits/plans.md
git commit -m "Record the gates for hold-email-delivery-to-its-limits

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A"
```

## Self-Review

- **Spec coverage:**
  - "A pass stops when its context ends" and "A pass whose context has already ended" → Task 4.
  - "A send that stays in doubt fails at the attempt limit" → Task 1.
  - "A lookup that keeps failing on a send in doubt fails at the attempt limit" → Task 2.
  - "One dispatcher's overlapping passes", on every store → Task 5: memory through synctest, PostgreSQL end to end. The other stores are covered by the gate being store-independent and by `make store-matrix`.
  - "A pass that lost part of its claim loses no email" → Task 3.
  - The stated limits → Task 6.
- **Placeholders:** none. Every code step shows the code, and every red step shows the failure observed on 2026-09-28.
- **Names:**
  - `errSendNotStarted`, `release(recipient, batch string, candidates []EmailCandidate)`, `passes chan struct{}` and `claims atomic.Int32` are used consistently across Tasks 3–5.
  - `hold`'s signature matches `main`.
