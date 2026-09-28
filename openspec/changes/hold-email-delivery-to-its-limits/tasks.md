## 1. End a send that stays in doubt at the attempt limit

- [ ] 1.1 Add the row "at least once: a send that stays in doubt fails at the attempt limit" to `TestEmailDispatcherDispatch` in `email_dispatch_test.go`, as in `plans.md` Task 1. Verify with `go test -run 'TestEmailDispatcherDispatch/at_least_once:_a_send_that_stays_in_doubt' -count=1 .` that it fails with `Retried:1, Failed:0` against `Failed:1`, and with `SENDING` against `FAILED`.
- [ ] 1.2 In `send`'s `ErrMailInDoubt` branch under `AtLeastOnce`, add the attempt the `SENDING` record counted to each candidate and call `hold(live, batch, EmailReasonSendInDoubt, err, false)` (`design.md` D1). Verify that 1.1 passes, and that every `TestEmailDispatcher*` test still passes.

## 2. Count a failed lookup on a send in doubt

- [ ] 2.1 Add the row "at least once: a lookup that keeps failing on a send in doubt fails at the attempt limit" to `TestEmailDispatcherDispatch`. Verify that it fails on the third pass with `Retried:1, Failed:0`, and with `SENDING` and an empty reason against `FAILED` and `lookup_failed`.
- [ ] 2.2 In `resolveDoubt`, replace the lookup-error branch with `hold(candidates, batch, EmailReasonLookupFailed, err, true)` (`design.md` D2). Verify that 2.1 passes, and that the whole `TestEmailDispatcher*` set passes.

## 3. Release a new message whose claim was cut short

- [ ] 3.1 Add the row "a pass that loses part of its lease before sending releases the rest unsent, not in doubt" to `TestEmailDispatcherDispatch`. Verify that the final pass fails with `Abandoned:1` against `Sent:1, Messages:1`, and that `h.sent()` has 1 item where 2 are required.
- [ ] 3.2 Add `errSendNotStarted` and `emailPass.release`. When the `SENDING` write of a new message returns false, `send` releases `live` to `CLAIMED`, reports `errSendNotStarted`, and returns (`design.md` D3). A resend keeps today's behaviour. Verify that 3.1 passes, and that `go test -run 'TestEmailDispatcher|TestMemoryStoreEmailConformance' -count=1 .` passes.

## 4. Stop a pass when its context ends

- [ ] 4.1 Add the rows "a pass stops when its context ends, and a later pass emails what it had not reached" and "a pass whose context has already ended claims nothing" to `TestEmailDispatcherDispatch`. Verify both fail. The first fails with `Should be zero, but was 2`, `Expected error with "context canceled" in chain but got nil.`, `Retried:2`, and 5 messages where 3 are required. The second fails with `Expected error with "context canceled" in chain but got nil.`.
- [ ] 4.2 Check `ctx.Err()` in three places: in `Dispatch` before claiming; before each `resolveDoubt` and each `deliver`; and in `send` before the `SENDING` write. The first ended check returns the partial result with `ctx.Err()` and skips the purge (`design.md` D5). Update `Dispatch`'s godoc. Verify that 4.1 passes, and that `TestEmailDispatcherRun` still passes.

## 5. Run one pass at a time on one dispatcher

- [ ] 5.1 Add `claims atomic.Int32` and a counting `ClaimEmails` to `recordingEmailStore`. Add `TestEmailDispatcherSerialisesItsPasses` in `email_dispatch_test.go`, using `testing/synctest`. Verify that it fails with `expected: 1  actual: 2` and `a second pass on one dispatcher waits for the first to finish`.
- [ ] 5.2 Add `TestOneDispatcherNeverEmailsANotificationTwice` in a new `sqlstore/email_dispatch_test.go`, on PostgreSQL through `sqlkittest.RunTestPostgres`. Verify with `cd sqlstore && go test -run 'TestOneDispatcherNeverEmailsANotificationTwice' -count=1 .` that it fails with `Should be empty, but was [...]`: dozens of notifications emailed twice.
- [ ] 5.3 Add the `passes` gate to `EmailDispatcher`. `NewEmailDispatcher` makes it; `Dispatch` acquires it in a `select` that also watches `ctx.Done()`, and releases it on return. Add the one-owner sentence to the `EmailStore.ClaimEmails` godoc (`design.md` D4). Verify that 5.1 and 5.2 pass, and that `go test -race -run 'TestEmailDispatcher' -count=1 .` passes.

## 6. Document

- [ ] 6.1 Add needles to `TestTheEmailDocumentMatchesTheImplementation`:
  - a new "one pass" case on "One pass": "one pass at a time" and "its context ends";
  - "attempt limit" in "Delivery guarantees";
  - "reaches the sender" and "released" in "Stated limits".

  Verify that it fails on the missing needles. Then update `docs/email.md`:
  - the outcome table: `ErrMailInDoubt` under `AtLeastOnce` fails at the limit (`send_in_doubt`), and so does a failed lookup on a send in doubt;
  - "One pass": one pass at a time per dispatcher, and a pass that stops at its context's end;
  - "Delivery guarantees": the attempt limit;
  - "Stated limits": a context ending between the check and the send, and the counted attempt of a released message.

  Verify that the test passes.
- [ ] 6.2 Update the godoc of `WithEmailOwner`: parallel passes need several dispatchers, each with its own owner. Verify with `go doc github.com/kartaladev/ntfy.WithEmailOwner` and `go doc github.com/kartaladev/ntfy.EmailDispatcher.Dispatch`.

## 7. Verify

- [ ] 7.1 Run `make all` and `make store-matrix`. Both must pass; the matrix covers the dispatcher's use of every store. Run `go test -race ./...` in the root. Record the results in `plans.md`'s execution record.
