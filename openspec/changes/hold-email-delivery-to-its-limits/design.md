## Context

See `proposal.md`, Why, for the five findings and their failing output. All five live in the core module's `email_dispatcher.go`. No store needs to change.

| Where (on `main`, `20967b3`) | What it does today |
| --- | --- |
| `email_dispatcher.go:393` `Dispatch` | Claims, settles sends in doubt batch by batch, delivers recipient by recipient, then purges. It never looks at `ctx` itself, and several calls on one dispatcher run side by side. |
| `email_dispatcher.go:688` `resolveDoubt` | Under `AtLeastOnce`, if the address lookup fails, it counts `Retried` and reports the error. It writes nothing, so the record stays `SENDING` with its attempts unchanged. |
| `email_dispatcher.go:769` `send` | Records `SENDING` with `Attempt: true`, then sends. If that write changes fewer rows than it asked for (`record` returns false), it returns. The rows it did change stay `SENDING` under the pass's lease. |
| `email_dispatcher.go:838` `send`, `ErrMailInDoubt` under `AtLeastOnce` | Counts `Retried`, reports the error, and leaves the record `SENDING`. The attempt was counted by the `SENDING` write, but nothing compares it with the limit. |
| `email_dispatcher.go:865` `hold` | Already does the right thing for a resend that fails: it splits candidates into spent (`FAILED`) and kept (left `SENDING`, or re-recorded `SENDING` with an attempt counted when `countAttempt`). |
| `sqlstore/email.go:392` `claimedEmails` | Reads a claim's rows back by `owner` and `lease_until`. Two claims by one owner at one instant share both. |
| `email.go:131` `EmailStore` | "A notification another claim holds is never returned, so two dispatchers never receive the same one." It says nothing about two claims by one owner. |

Constraints:
- The core module imports only the standard library.
- `EmailStore` is a port hosts may implement, held to `ntfytest.RunEmail`. Changing its contract costs every host store, so this change keeps the fix in the dispatcher wherever it can.
- The at-least-once design (`2026-09-27-email-delivery-robustness`, D1 and D6) holds. A send in doubt keeps its key and never grows, and a failed resend stays in doubt until the lease lapses.

## Goals / Non-Goals

**Goals**
- The attempt limit ends every path a delivery can loop on under `AtLeastOnce`.
- A message that was never sent is never recorded as in doubt, so `AtMostOnce` never abandons it.
- One dispatcher never emails a notification twice, on any store, however often the host calls `Dispatch`.
- A cancelled pass calls no host sender and starts no send.

**Non-Goals**
- Keeping two claims by one owner apart inside `sqlstore`. That would need a per-claim token in the schema (D4, alternatives).
- Bounding a crash loop. Under `AtLeastOnce`, a process that stops mid-send on every attempt is resent on every lapse. The in-doubt branch that this change bounds is a different path. No test proves the crash loop is unbounded, so it is **unverified** and not planned.
- Memory-versus-SQL email store divergences, which belong to `align-memory-store-with-sql`.
- Refunding the attempt a lost-lease message counted (D3).
- Recording the outcome of a send whose context ended during it. **Unverified:** on the SQL stores, the `SENT` write after such a send may fail on the ended context. The send would then be left in doubt. It is not proven by any test here: the proofs run on the memory store, which ignores the context. This change neither causes nor fixes it. If it matters, the first task is a failing test on `sqlstore`.

## Decisions

### D1. A send the sender reports in doubt goes through `hold`, like any failed resend

In `send`, the `ErrMailInDoubt` branch under `AtLeastOnce` does what the default branch does for a resend. It adds the attempt the `SENDING` record already counted to each candidate, then calls `hold(live, batch, EmailReasonSendInDoubt, err, false)`:
- a candidate whose attempts reached the limit is recorded `FAILED` with reason `send_in_doubt`;
- the others stay `SENDING` under the key, counted `Retried`, exactly as today.

This applies to a first send and to a resend alike. The message is in doubt either way, so it keeps its key.

*Alternatives:*
- **Check the limit in `resolveDoubt`, before resending.** This would bound the loop one pass later, and would also fail a send that crashed at the limit. It is rejected here. It sends one more message than the limit allows, and the crash-loop half is unproven (Non-Goals).
- **Record `RETRY` after an in-doubt send.** Rejected by the earlier design's D6: a `RETRY` goes out as a new message under a new key, merged with newer notifications.

**Default:** at most `DefaultEmailMaxAttempts` (5) attempts, then `FAILED` (`send_in_doubt`). **Override:** `WithEmailMaxAttempts` sets the limit, and `WithDeliveryGuarantee(AtMostOnce)` never repeats a send in doubt at all. `WithEmailFailureDetail` hears of the `FAILED` outcome only, as for every recorded failure.

### D2. A failed lookup on a send in doubt counts an attempt and stays in doubt

In `resolveDoubt`, the lookup-error branch calls `hold(candidates, batch, EmailReasonLookupFailed, err, true)`. This mirrors a fresh delivery, where a failed lookup is `retry(..., countAttempt: true)`, and a resend whose notifications cannot be read, which is already `hold(..., true)`:
- below the limit, the record is re-written `SENDING` under its key with one more attempt. It keeps the lease and lapses to the next pass as before;
- at the limit, it is recorded `FAILED` (`lookup_failed`).

*Alternative:* record `RETRY` with a backoff, as a fresh delivery does. Rejected for the same reason as D1: the message would lose its key.

**Default / Override:** as D1. A host whose address book is down for longer than `attempts × lease` loses those in-doubt emails as `FAILED`. The same holds today for a fresh delivery after `attempts` backoffs.

### D3. A new message whose `SENDING` write came back short is released, not left in doubt

When `record(live, SENDING)` returns false for a **new** message (`batch` was empty on entry), `send` releases what the pass still holds and returns without sending:
- `release` writes `CLAIMED` for `live` under the pass's owner. `RecordEmails` changes only what the owner still holds, so the notifications another dispatcher took over are untouched.
- `CLAIMED` clears the owner and the lease in every store, so the next pass claims them again as unsent and sends them as a new message.
- The error handler hears `errSendNotStarted`, which wraps no host error: the message was not sent. Before, a lost lease was only counted in `Unrecorded`.
- The release writes directly, not through `record`, so `Unrecorded` is not counted twice.

A **resend** (`batch` given) keeps today's behaviour. Its records were `SENDING` under that key before this pass, so a short write leaves a message in doubt that already was. Releasing it would lose its key.

The same release runs when the `SENDING` write fails with an error. That path is not separately proven: whether rows were written is then unknown, and the release is a best effort. It changes nothing that is not the pass's own.

*Alternatives:*
- **Make the `SENDING` write all-or-nothing in the store.** That is a contract change for every `EmailStore`, host ones included. Rejected while the dispatcher can fix it alone.
- **Check the lease's time before writing `SENDING`.** This narrows the window but cannot close it, because another dispatcher can take over between the check and the write.
- **Send to what was recorded.** The store returns a count, not which rows, and part of the message now belongs to another dispatcher.

*Limit:* the attempt the short `SENDING` write counted stays counted, because `RecordEmails` has no way to take one back. It happens only when a lease lapses mid-pass, which `WithEmailLease` already warns against.

**Default:** released, reported, sent by a later pass. **Override:** none for the release itself: an unsent message must never be abandoned, and nothing is gained by configuring that. `WithEmailLease` sizes the lease so that it does not lapse mid-pass.

### D4. One dispatcher runs one pass at a time

`EmailDispatcher` gains `passes chan struct{}`, with a buffer of 1 and made in `NewEmailDispatcher`. `Dispatch` acquires it with a `select` that also watches `ctx.Done()`, and releases it on return. A second call therefore waits for the first. If its context ends while it waits, it returns `DispatchResult{}` with the context's error and claims nothing.

The `EmailStore.ClaimEmails` godoc gains one sentence: "One owner's claims never overlap: a dispatcher runs one pass at a time, so a store need not keep two concurrent claims by one owner apart." `WithEmailOwner` already says two dispatchers must never share an owner. `sqlstore` needs no change, and PostgreSQL's same-owner read-back is then outside the contract rather than a defect. The audit's store-level test is not landed. The end-to-end test on PostgreSQL is.

*Alternatives:*
- **`TryLock`, returning an empty result at once.** A scheduler would read "nothing due" when a pass was in fact running. Rejected as misleading.
- **A per-claim token in `sqlstore`,** a new `claim_id` column that the read-back matches on. It fixes the store for any caller, but it is a schema change with a documented upgrade for every SQL host. MySQL has no `RETURNING` to lean on. The dispatcher gate makes it unnecessary for the library's own caller. Recorded as a possible later change if a host store needs it.
- **A `sync.Mutex`.** It cannot give up when the context ends.

**Default:** passes on one dispatcher run one after another. **Override:** a host that wants parallel passes constructs several dispatchers with distinct owners (`WithEmailOwner`, or the generated default). Each has its own gate, and claims keep them apart as today.

### D5. A pass stops when its context ends

`Dispatch` checks `ctx.Err()` in three places:
- after acquiring the gate, before claiming: an ended context claims nothing;
- before settling each batch in doubt, and before delivering to each recipient;
- in `send`, immediately before the `SENDING` write, so that an ended context starts no send.

On the first ended check, the pass stops. It skips the purge and returns its result so far with `ctx.Err()`, unwrapped, so that `errors.Is(err, context.Canceled)` holds.

What it had not reached stays `CLAIMED` under its lease, with no attempt counted. It is **not** released: a write with an ended context fails on the SQL stores, and writing with `context.WithoutCancel` would keep working after the host asked the pass to stop. A later pass takes it over once the lease lapses, as it does for a dispatcher that stopped (the existing scenario "A stopped dispatcher's unsent claim is taken over").

`Run` needs no change. It already returns `ctx.Err()` when `Dispatch` errs with the context ended.

*Alternatives:*
- **Return `nil` as before.** A scheduler could not tell a pass that was cut short from one that finished. Rejected: the result is partial, and the error says so.
- **Check only in `send`.** That misses settling a batch in doubt under `AtMostOnce`, which records `ABANDONED` without sending. Loop-level checks stop that too.

*Limit:* a context that ends after the last check and before `Mailer.Send` returns still reaches the sender, which receives the ended context. Its outcome is then recorded as today. The spec states this.

**Default:** the host's context bounds the pass. **Override:** the host chooses the context: `context.WithoutCancel` to finish a pass whatever happens, or a deadline to bound one. `WithEmailLease` bounds how long unreached notifications wait.

### D6. Where the tests go

- The five dispatcher proofs become rows of `TestEmailDispatcherDispatch`, whose `run`/`assert` shape already fits multi-pass scenarios.
  - The table has no `ctx` field. The two context rows build their own context: one is cancelled from inside the `Mailer`, which a modifier applied before the call cannot express. Each says so in a comment.
- The gate needs a state, "the second pass is waiting", that only `testing/synctest` makes durable without a sleep. It gets its own `TestEmailDispatcherSerialisesItsPasses`, with a one-line comment saying why it is not a table row. `recordingEmailStore` gains a `ClaimEmails` counter for it.
- The PostgreSQL end-to-end proof is the only test in a new `sqlstore/email_dispatch_test.go`. It uses `sqlkittest.RunTestPostgres` per `use-testcontainers`.
- No new mocks. The harness's `MailerFunc`, `AddressBookFunc` and the decorated memory store are the doubles the email tests already use. `use-mockgen` has nothing to generate.

## Risks / Trade-offs

- **[A host relied on `Dispatch` returning `nil` after cancellation]** → It could not tell a cut-short pass from a full one. The new error is the context's own, so `errors.Is` works. No tag exists yet. The proposal records the compatibility change.
- **[A host called `Dispatch` concurrently for throughput]** → Those calls now run one after another. The documented way to run in parallel was always several dispatchers. `docs/email.md` says so.
- **[A long pass holds up a waiting `Dispatch`]** → The wait honours the caller's context. `Run` never overlaps itself.
- **[`testing/synctest` and the harness]** → The service starts no goroutines (checked: `service.go`, `hub.go`, `memory.go`), and the harness's clock is a movable one, so the bubble holds. Verified by a probe run on 2026-09-28, which failed only on the assertion.
- **[D3 releases a notification that another dispatcher is about to take over]** → `RecordEmails` checks the owner, so a row another dispatcher took is untouched. A row the pass still owns is released, and whichever claim comes next takes it. Either way it is sent once.
- **[An `AtLeastOnce` host with a flaky sender loses emails as `FAILED`]** → The spec and the docs always said the attempt limit ends it. `WithEmailMaxAttempts` raises the limit.

## Migration Plan

No schema or data change. A host upgrades the module. If its own scheduler calls `Dispatch`, it can treat `context.Canceled` and `context.DeadlineExceeded` as "the pass was cut short", not as a failure. Rollback means reverting the module.
