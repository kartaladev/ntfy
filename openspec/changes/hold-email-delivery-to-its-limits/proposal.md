## Why

The email dispatcher promises bounds it does not keep. Under `AtLeastOnce`, the spec and `docs/email.md` say that "the attempt limit still ends" a send left in doubt, yet two paths never compare the attempts with the limit, so such a send is repeated or re-claimed forever. A pass that loses part of its lease before sending leaves a message that was never sent looking like one in doubt, and under the default `AtMostOnce` that email is lost. On PostgreSQL, one dispatcher whose `Dispatch` calls overlap emails most notifications twice. A pass also keeps calling the `Mailer` after its context is cancelled.

**Proved by:** the 2026-09-28 adversarial audit, re-run on current `main` (`20967b3`, after PR #11) on 2026-09-28, with Go 1.26.8 and Docker for PostgreSQL. The tests below are the audit proofs, adapted into the house `table-test` form. They were run temporarily, then removed; each is the red step of a task. Every one fails for the stated reason:

1. **(Medium) A send that keeps coming back `ErrMailInDoubt` is resent forever under `AtLeastOnce`.** `send`'s `ErrMailInDoubt` branch leaves the delivery `SENDING` and never checks the attempt limit. The audit's `TestAuditEmailInDoubtResendIgnoresAttemptLimit` sent 8 messages in 8 passes with `WithEmailMaxAttempts(2)`, and never recorded `FAILED`. Adapted as `TestEmailDispatcherDispatch/at_least_once:_a_send_that_stays_in_doubt_fails_at_the_attempt_limit`:

   ```
   expected: ntfy.DispatchResult{Claimed:1, ... Retried:0, Failed:1, ...}
   actual  : ntfy.DispatchResult{Claimed:1, ... Retried:1, Failed:0, ...}
   Messages: the send that reaches the limit fails
   expected: "FAILED"   actual: "SENDING"
   expected: "send_in_doubt"   actual: ""
   expected: ntfy.DispatchResult{Claimed:0, ...}   actual: ntfy.DispatchResult{Claimed:1, ... Retried:1, ...}
   ```

2. **(Low–Medium) A failed address lookup while settling a send in doubt counts no attempt and writes no record under `AtLeastOnce`**, so the delivery is claimed again on every pass. The audit's `TestAuditEmailInDoubtLookupFailureIgnoresAttemptLimit` reported `"8" is not less than or equal to "2"`: 8 claims in 8 passes with a limit of 2. Adapted as `…/at_least_once:_a_lookup_that_keeps_failing_on_a_send_in_doubt_fails_at_the_attempt_limit`:

   ```
   actual  : ntfy.DispatchResult{Claimed:1, ... Retried:1, Failed:0, ...}
   Messages: the lookup that reaches the limit fails it
   expected: "FAILED"   actual: "SENDING"
   expected: "lookup_failed"   actual: ""
   ```

3. **(Medium) A pass that loses part of its lease loses an email that was never sent.** The pass writes `SENDING` on the notifications it still holds, sees the change count come back short, and returns without sending. That record carries the lapsed lease, so the next pass takes the message for one in doubt. Under the default `AtMostOnce` it records `ABANDONED`. The audit's `TestAuditEmailPartialLeaseLossAbandonsUnsentNotification` reported `statuses: [SENDING ABANDONED]`. Adapted as `…/a_pass_that_loses_part_of_its_lease_before_sending_releases_the_rest_unsent,_not_in_doubt`:

   ```
   expected: ntfy.DispatchResult{Claimed:1, Sent:1, Messages:1, ... Abandoned:0, ...}
   actual  : ntfy.DispatchResult{Claimed:1, Sent:0, Messages:0, ... Abandoned:1, ...}
   Messages: the newer notification is emailed, not abandoned as in doubt
   "[{alice@example.com ...}]" should have 2 item(s), but has 1
   ```

4. **(Low–Medium) On PostgreSQL, one dispatcher whose passes overlap emails notifications twice.** This needs two `Dispatch` calls on one dispatcher at once and a clock that gives both the same reading. Both claims then share an owner and a lease end. `claimedEmails` reads back by owner and `lease_until` (`sqlstore/email.go`), so both receive the same notifications, and `RecordEmails` checks the owner only. MySQL and SQLite do not reproduce it.
   - At the store: the audit's `TestAuditEmailSameOwnerConcurrentClaimsOverlap/PostgreSQL` reported `two claims returned the same notification 116 times` in 30 rounds of 4.
   - End to end: `TestOneDispatcherNeverEmailsANotificationTwice`, the audit's `TestAuditEmailOneDispatcherConcurrentPassesDoubleSend` renamed, reported `Should be empty, but was [...]`, listing **48 of 60** notifications emailed twice.
   - With the memory store: `TestEmailDispatcherSerialisesItsPasses` shows the dispatcher does nothing to stop the overlap. The memory store keeps it from doing harm there.

     ```
     expected: 1   actual: 2
     Messages: a second pass on one dispatcher waits for the first to finish
     ```

5. **(Low, hardening) A pass keeps calling the `Mailer` after its context is cancelled.** The loops over senders in doubt and recipients never check `ctx.Err()`. The spec only requires `Run` to stop. The audit's `TestAuditEmailPassKeepsSendingAfterCancel` reported `Should be zero, but was 2`. Adapted as `…/a_pass_stops_when_its_context_ends,_and_a_later_pass_emails_what_it_had_not_reached` and `…/a_pass_whose_context_has_already_ended_claims_nothing`:

   ```
   Should be zero, but was 2
   Messages: the Mailer is not called once the pass's context has ended
   Expected error with "context canceled" in chain but got nil.
   actual  : ntfy.DispatchResult{Claimed:3, Sent:1, Messages:1, ... Retried:2, ...}
   "[... 5 messages ...]" should have 3 item(s), but has 5
   ```

   The later pass sent bob and carol a second message: the cancelled pass had marked them `RETRY` instead of leaving them untouched.

Every finding reproduced on `main`, so none is carried as **Unverified** or "Does not reproduce".

## What Changes

- **Under `AtLeastOnce`, the attempt limit ends a send in doubt.**
  - A send the `Mailer` answers with `ErrMailInDoubt` is still repeated under the same idempotency key. Once the send that reaches the limit comes back in doubt, the delivery is recorded `FAILED` (`send_in_doubt`) and never claimed again.
  - A failed address lookup while settling a send in doubt counts an attempt. The send stays in doubt under its key, and the lookup that reaches the limit records `FAILED` (`lookup_failed`).
- **A pass that loses part of its lease before sending releases the rest unsent.** A new message whose `SENDING` record could not be written on every notification is not sent. What the pass still holds is released to the next pass as unsent (`CLAIMED`), so it is never mistaken for a send in doubt. The error handler hears of it. A resend under `AtLeastOnce` is left in doubt as today, because it is one already.
- **One dispatcher never runs two passes at once.** `Dispatch` waits for a pass the same dispatcher is running. If its context ends while it waits, it returns the context's error. A host that wants passes in parallel builds several dispatchers, each with its own owner, as it already can. The `EmailStore` contract says that one owner's claims never overlap, so a store need not keep them apart.
- **A pass stops when its context ends.**
  - `Dispatch` claims nothing if its context has already ended.
  - Mid-pass, it settles and sends nothing more: no `Mailer` call and no `SENDING` record after the context ends.
  - It returns what it did so far with the context's error.
  - What it had not reached stays claimed until the lease lapses. A later pass then takes it over with no attempt spent.
  - **Compatibility:** `Dispatch` returned an error only when claiming failed. It now also returns the context's error when the pass is cut short. `Run` already treats that as the end of the loop. No tag has been cut, so this is recorded rather than versioned.
- **Docs and godoc:** `docs/email.md` (the outcome table, one pass and the stated limits), and the godoc of `Dispatch`, `EmailStore.ClaimEmails` and `WithEmailOwner`.

Out of scope:
- A `ConfigurationError` from the ID generator already spends no attempt (PR #11).
- The memory-versus-SQL email store divergences (an empty owner, `Limit` 0) belong to `align-memory-store-with-sql`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-email`:
  - The dispatch loop requirement gains a pass that stops at its context's end.
  - The at-most-once/at-least-once requirement bounds a send in doubt, and a failed lookup on one, by the attempt limit.
  - The concurrent-dispatchers requirement covers one dispatcher's overlapping passes, and a pass that loses part of its lease.

## Impact

- **Code:**
  - `email_dispatcher.go`: `Dispatch`, `send`, `resolveDoubt`, a new `release`, and a pass gate on `EmailDispatcher`.
  - `email.go`: the `EmailStore` godoc.
  - No store changes: memory, `sqlstore` and `ntfytest` are untouched.
- **Tests:**
  - Five new cases in `TestEmailDispatcherDispatch` (`email_dispatch_test.go`), plus a `ClaimEmails` counter on its `recordingEmailStore`.
  - A new `TestEmailDispatcherSerialisesItsPasses` (`testing/synctest`).
  - A new `sqlstore/email_dispatch_test.go` with `TestOneDispatcherNeverEmailsANotificationTwice` on PostgreSQL.
  - Needles in `TestTheEmailDocumentMatchesTheImplementation`.
- **Docs:** `docs/email.md`.
- **Hosts:**
  - A host calling `Dispatch` from its own scheduler sees `context.Canceled` or `context.DeadlineExceeded` when the pass is cut short, with the partial result.
  - A host that called `Dispatch` concurrently on one dispatcher now has those calls run one after another.
  - An `AtLeastOnce` host whose sender keeps answering in doubt sees those deliveries end `FAILED` after the attempt limit instead of being repeated forever.
- **Dependencies:** none. The core stays on the standard library; `testing/synctest` is standard library, test-only.
