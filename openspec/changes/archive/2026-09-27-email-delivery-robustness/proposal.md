## Why

`AtLeastOnce` promises something the dispatcher does not deliver. Its godoc says an in-doubt send is repeated "with the same idempotency key over **exactly the same notifications**" (`email.go:272-275`), and the spec repeats it as a scenario ("the resent message covers exactly the original notifications"). In fact `resolveDoubt` routes the resend through `send`, which re-runs `recheck` and drops every notification read, closed or deleted since the first attempt, then re-renders from live state. The same idempotency key can therefore carry a smaller notification set and different content than the attempt it repeats — and a sender that does not honour the key delivers two different digests.

Alongside it sit three smaller defects in the same file: a retry delay that overflows to a negative value under a legitimate configuration, a nil dereference waiting in the option validation, and raw sender errors persisted verbatim into the delivery record, which is how an address gets into a library that otherwise keeps addresses entirely behind the host's `AddressBook`.

None of this is observable under the shipped defaults, which is exactly why it should be fixed before the first tag rather than after.

## What Changes

- **The `AtLeastOnce` contract states what the dispatcher actually guarantees**: the same idempotency key, a notification set that never grows, notifications dropped as they stop being ACTIVE, and content rendered afresh at each attempt. The false "exactly the same notifications" promise is removed from the godoc and from the spec scenario that codified it.
- **BREAKING** (pre-tag, documentation-level): a host that read the old wording as a guarantee of byte-identical resends loses that reading. No behaviour changes — the code already behaves as the new wording describes. What changes is that the contract stops claiming more than the implementation provides.
- **A retry is always scheduled into the future.** The doubling in `delay` is bounded so the computed delay can never overflow, go negative or reach zero; it stays within the configured base and ceiling whatever the attempt count.
- **A recorded failure reason no longer carries the host's data by default.** The delivery record keeps a library-owned reason code, bounded in length. A host that wants detail recorded supplies it through a new option and decides for itself what is safe to store; the full error still reaches the error handler as it does today, unchanged.
- **The option validation stops dereferencing an unset pointer**, and `WithEmailOwner` refuses an owner longer than the delivery record can store, so that a value which works on PostgreSQL and SQLite cannot fail only on MySQL, at runtime.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-email`: the at-most-once/at-least-once requirement restates the at-least-once guarantee truthfully and states the two limits a consumer must plan for; the failure-handling requirement gains a bound on the retry delay; a new requirement governs what a recorded failure reason may contain; the construction requirement covers an owner that cannot be stored.

## Impact

- **Code:** `email_dispatcher.go` — `delay` (`:568-580`), `validate` (`:307`), `fail` (`:521`) and `retry` (`:552`, `:559`) where `cause.Error()` becomes the recorded reason, and a new option alongside the existing ones. `email.go` — the `AtLeastOnce` godoc (`:272-275`) and, if a reason code becomes part of the port vocabulary, the constants beside `EmailSkip*` (`:64-76`).
- **APIs:** one added option for recording failure detail. No signature changes. `EmailRecord.Reason` (`email.go:140`) keeps its shape; what the dispatcher puts in it changes.
- **Stores:** none of the three dialects' DDL changes — `reason` is already `text`/`LONGTEXT`. The bound is enforced by the dispatcher, so every store behaves the same.
- **Tests:** a red test per defect before any fix, per `.claude/rules/prove-errors-with-tests.md`; the conformance suites `ntfytest.RunEmail` and `ntfytest.RunEmailDispatch` gain the cases that pin the new guarantees, so a host store cannot diverge.
- **Docs:** `docs/email.md`, whose "Delivery guarantees" and "Stated limits" sections carry the same "exactly the same notifications" wording.
- **Not in scope:** the dispatcher's throughput and the claim query's cost (`scale-email-claim-query`); validating `IDGenerator` output against the store's identifier column, which is a store-wide concern rather than an email one; and the other audit findings, each of which has its own change.
