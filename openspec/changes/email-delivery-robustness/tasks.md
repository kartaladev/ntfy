## 1. Prove the defects (red)

- [ ] 1.1 Add a failing test that pins the at-least-once resend: a dispatcher configured `AtLeastOnce` leaves a message in doubt covering three of alice's notifications, she reads one, and a later pass resolves the doubt after the lease lapses. Assert what the resent message covers and that its `IdempotencyKey` is the original batch. Verify with `go test -run 'TestAtLeastOnceResend' -count=1 ./...` that it fails against the documented promise in `email.go:272-275` — the resent message carries two notifications, not the three the godoc claims (see `.claude/rules/prove-errors-with-tests.md`).
- [ ] 1.2 Add a failing table test for the retry delay covering the values in `design.md` — Context: base `time.Hour`, ceiling `math.MaxInt64`, attempts 22, 23, 25 and 55. Assert every delay is positive and no greater than the ceiling plus jitter. Verify it fails at attempts 23 (negative) and 55 (zero), and confirm the configuration it uses passes `NewEmailDispatcher` today, so the test proves a reachable defect rather than an impossible one.
- [ ] 1.3 Add a failing test for the option-validation guard: a configuration that sets a backoff base without a ceiling reaches `case c.backoff != nil && *c.ceiling < *c.backoff` (`email_dispatcher.go:307`). Since no current option sets one without the other, drive `emailConfig.validate` directly, or add the base-only option first; verify the test panics with a nil dereference before the fix.
- [ ] 1.4 Add a failing test for the recorded reason: a `Mailer` returning an error whose text contains an email address, asserting the reason stored on the delivery record contains no part of that text and that the unmodified error still reaches the `WithEmailErrorHandler` handler. Verify it fails today because `fail` records `cause.Error()` verbatim (`:521`). Note in the test's comment that real SMTP rejections embedding the address is **unverified** provider behaviour; the test pins only the library's own handling.

## 2. Correct the at-least-once contract

- [ ] 2.1 Rewrite the `AtLeastOnce` godoc (`email.go:272-275`) to state the same key, a set that never grows, notifications dropped as they stop being ACTIVE, and content rendered afresh at each attempt. Verify with `go doc ./... | grep -A6 AtLeastOnce` that the rendered text no longer claims "exactly the same notifications".
- [ ] 2.2 Adjust the test from 1.1 to assert the corrected contract and verify it passes with no change to `email_dispatcher.go`, confirming the code was already right and only the contract was wrong.
- [ ] 2.3 Add a test for the empty-resend case: every notification of an in-doubt message is read before the doubt is resolved; assert no message is sent and all are recorded skipped (spec scenario "Nothing is resent once every notification has been read"). Verify it passes.
- [ ] 2.4 Update the "Delivery guarantees" and "Stated limits" sections of `docs/email.md`, which carry the same wording, and add the two stated limits from the spec (re-rendering across a template change; a sender that ignores the key). Verify by re-reading: no sentence promises identical resent content.

## 3. Bound the retry delay

- [ ] 3.1 Stop the doubling in `delay` (`email_dispatcher.go:568-580`) before it can overflow, and clamp into `[base, ceiling]` before jitter, per `design.md` D2. Verify the table test from 1.2 passes.
- [ ] 3.2 Add cases to the same table for the shipped defaults (base one minute, ceiling one hour) at attempts 1 through 10, asserting the delays are unchanged from today's behaviour. Verify they pass, proving the fix did not alter non-degenerate configurations.
- [ ] 3.3 Add a dispatcher-level test asserting a scheduled `NextAttemptAt` is strictly after the pass's instant under the largest accepted ceiling (spec scenario "A very large ceiling never schedules a retry in the past"). Verify it passes and that the delivery is not re-claimed by an immediately following pass.

## 4. Record a reason the library owns

- [ ] 4.1 Define the reason classification as named constants beside the existing `EmailSkip*` values in `email.go`, covering the outcomes a delivery can end in, and verify the set is exhaustive by a test that walks every terminal path in a dispatch pass.
- [ ] 4.2 Record the classification instead of `cause.Error()` in `fail` (`:521`) and `retry` (`:552`, `:559`), leaving `report`/`onError` untouched. Verify the test from 1.4 passes and that the existing error-handler tests still see the full error.
- [ ] 4.3 Add the option that lets a host supply the text to record, defaulting to the classification, and truncate what it returns to the documented maximum. Verify with tests for the two spec scenarios "A host records its own detail" and "Recorded detail is bounded".
- [ ] 4.4 Assert the skip reasons are unaffected (spec scenario "Skip reasons are unaffected") and verify the existing skip tests pass unchanged.
- [ ] 4.5 Document the maximum length and the option in `docs/email.md`, and verify the stated limit appears in its "Stated limits" section.

## 5. Close the construction gaps

- [ ] 5.1 Guard both pointers in the ceiling-versus-base check (`email_dispatcher.go:307`) and verify the test from 1.3 passes with a configuration error rather than a panic.
- [ ] 5.2 Add the owner length bound to `WithEmailOwner` validation, naming the limit in the error. Verify with a test for the spec scenario "An owner too long to store", and verify the same configuration is refused identically on every store by running it in the shared suite rather than per store.

## 6. Pin the guarantees in the shared suite

- [ ] 6.1 Add the at-least-once subset behaviour and the empty-resend case to `ntfytest.RunEmailDispatch`, so a host-supplied store cannot diverge. Verify by running the suite against both `NewMemoryStore` and `sqlstore`.
- [ ] 6.2 Add the recorded-reason expectations to `ntfytest.RunEmailDispatch`, not `ntfytest.RunEmail`: `EmailCandidate` (`email.go:115-126`) carries `Status`, `BatchID` and `Attempts` but no `Reason`, so a stored reason cannot be read back through the `EmailStore` port on any store, and asserting it there would mean widening that port for nothing the library needs. Drive a real dispatcher over the factory's store and verify both stores record the classification identically, on every driver and dialect.

## 7. Verify the whole change

- [ ] 7.1 Run `make all` and verify lint, split-check and tests pass on every module.
- [ ] 7.2 Run `make store-matrix` and verify the email conformance suites pass on every driver and dialect, including MySQL for the owner bound.
- [ ] 7.3 Re-run each red test from group 1 against its temporarily reverted fix and verify each fails again, confirming none passes vacuously.
- [ ] 7.4 Run `openspec validate "email-delivery-robustness" --strict` and verify every spec scenario has a corresponding test.
