## Context

See `proposal.md` — Why. Four defects in `email_dispatcher.go`, one of which is a contract decision and three of which are bounded fixes.

The constraints that shape the contract decision:

- `resolveDoubt` (`:583-607`) hands an in-doubt batch to `send` (`:661`), which begins with `recheck` (`:722-750`). `recheck` reads each notification again and drops the ones that are no longer ACTIVE or no longer exist. Only then is the message rendered (`:667`) and sent with `IdempotencyKey: batch` and `NotificationIDs: candidateIDs(live)` (`:689-692`).
- So the resent set can only **shrink**. `resolveDoubt` groups strictly by `BatchID` (`:379-387`), and fresh candidates are delivered by a separate path (`deliver`, `:612`), so a resend cannot absorb a newer notification. The existing spec scenario asserting that is correct and stays.
- A notification's content is immutable after publish — only `State`, `ReadAt`, `ClosedAt` and `InactiveAt` change. Re-rendering the same set therefore yields the same content **unless** the template itself changed between attempts, or the template renders state-dependent text.
- Dropping a notification that has been read is not an accident: the requirement "A notification that stops being active before sending is not emailed" demands it, and `EmailSkipInactive` exists for it (`email.go:72-73`).
- The host already receives every error in full through `onError` (`report`, `:456-464`), which deliberately names only the recipient identifier and never the address.

Verified for the delay defect, using `delay`'s arithmetic with jitter fixed at 1.0, base `time.Hour` and ceiling `math.MaxInt64` — a configuration `validate` accepts today:

```
attempts=22  delay=2097152h0m0s
attempts=23  delay=-929791h34m33.709551616s   <- negative
attempts=25  delay=1404929h16m18.871345152s   <- positive again: the wrap oscillates
attempts=55  delay=0s
```

A negative or zero delay puts `NextAttemptAt` at or before now, so the delivery is re-claimed by the very next pass: a hot retry loop against the host's sender.

## Goals / Non-Goals

**Goals**

- The at-least-once contract describes what the dispatcher does, and the dispatcher keeps doing what the rest of the spec requires of it.
- A retry delay is always in the future, whatever a host configures and however many attempts have been made.
- A failure reason that reaches durable storage is the library's own, not the host's data.
- The nil dereference in option validation cannot be reintroduced by adding an option.

**Non-Goals**

- Storing rendered message content, in any form. See D1.
- Changing when a notification is dropped from a message: `recheck`'s behaviour is a requirement, not a defect.
- Changing the delivery record's schema. `reason` is already `text`/`LONGTEXT` on all three dialects; the bound is applied before the write.
- Dispatcher throughput, the claim query's cost, and per-address delivery — other changes.

## Decisions

### D1. At-least-once means the same key over a set that never grows, re-rendered

The contract is corrected to match the implementation, because the implementation is right and the wording is wrong. A consumer can rely on three things: the idempotency key is stable across attempts; a resend never adds a notification; and a notification already read, closed or deleted is never emailed. What a consumer cannot rely on is byte-identical content across attempts.

*Alternatives considered:*

- **Freeze the rendered message and its set at the first `SENDING`, and replay that snapshot.** This is what the old wording promises, and it is the wrong thing to build. It contradicts a requirement the project deliberately made — that a notification read before sending is not emailed — so a replayed snapshot would email someone about something they had already dealt with. It also forces rendered message bodies into the delivery record, which is the exact PII spread D4 exists to stop, and makes every row as large as an email.
- **Resend only when the set is unchanged, and abandon otherwise.** This preserves the literal old wording by refusing to send anything else. Rejected as a default: a recipient who read one of five notifications would get no email about the other four, and those four would be recorded abandoned. A host choosing at-least-once has said it prefers a duplicate over a loss; silently converting a partial overlap into a total loss inverts that preference.

**Default:** repeat with the same key over the still-ACTIVE subset. **Override:** none as an option — the guarantee boundary is stated instead (library rule 4), because the protection a host actually needs is a sender that honours the idempotency key, which makes the second render unobservable. Should "abandon on a changed set" turn out to be a real requirement, it is additive and can be added later without breaking anyone.

### D2. The doubling is bounded, and a large ceiling stays legal

`delay` stops doubling before it can overflow: once `wait` exceeds half the ceiling, the next value is the ceiling. The result is clamped into `[base, ceiling]` before jitter, and jitter of ±20% on a positive value cannot reach zero.

*Alternative considered:* refuse a ceiling above some safe maximum at construction, per library rule 6. Rejected as the primary fix: `math.MaxInt64` is not a wiring mistake, it is how a host says "no cap", and refusing it would be answering a reasonable configuration with an error. Bounding the arithmetic gives that configuration the meaning the host intended. The construction-time check is kept for values that genuinely cannot work — see D5.

**Default:** delays grow from base to ceiling and stop there. **Override:** `WithEmailBackoff`, unchanged.

### D3. The invariant is stated in the spec, not just fixed in the code

The requirement gains "every retry is scheduled strictly later than the failure that caused it". That is what a host depends on; the arithmetic is one way of satisfying it. A future change to the backoff curve has to keep it, and the conformance scenario says so.

### D4. A recorded reason is the library's classification; the host opts into detail

By default the delivery record stores a reason the library owns — the outcome's classification — and never text produced by the host's `Mailer`, `EmailTemplate` or `AddressBook`. The full error is unaffected: it reaches `onError` exactly as today, where the host's logging policy applies.

This is possible precisely because the host already has the error. The durable column is for answering "what happened to this delivery", which a classification answers; diagnosis belongs in the host's logs, under the host's retention and redaction rules.

*Alternatives considered:*

- **Truncate the raw error.** Rejected: an SMTP rejection puts the address at the front, so truncation keeps the PII and loses the diagnosis.
- **Drop the reason entirely.** Rejected: it is the only durable record of why a delivery ended, and operators need it.
- **Redact by pattern (strip anything that looks like an address).** Rejected: a denylist that must recognise every provider's error format will be wrong quietly, which is the worst failure mode for a privacy control.

**Default:** classification only. **Override:** a host-supplied rule that maps the failure to the text to record, bounded by a documented maximum length — a stated limit so a delivery row cannot grow without bound whatever the host returns.

Existing skip reasons (`EmailSkipNoAddress`, `EmailSkipFiltered`, `EmailSkipInactive`, `EmailSkipDeleted`) are already library-owned constants and are unaffected.

### D5. Validation guards its own pointers, and refuses an unstorable owner

The `*c.ceiling` dereference guarded only by `c.backoff` (`:307`) becomes a guard on both. The test for it asserts the contradictory-configuration error rather than the panic, so it stays meaningful after the fix.

`WithEmailOwner` gains a length bound matching what the delivery record admits, because an owner accepted on PostgreSQL and SQLite that fails only on MySQL, at run time, is exactly the surprise library rule 6 exists to prevent.

*Out of scope:* validating `IDGenerator` output against the identifier column. The generator is shared with notifications and is not the email dispatcher's to police; it belongs with a store-level concern.

## Risks / Trade-offs

- **A host read the old at-least-once wording as a guarantee** → It never held, and the correction is documentation plus a spec scenario, not a behaviour change. The stated limits tell them what to rely on instead: a key-honouring sender.
- **A host's operational tooling greps the `reason` column for sender error text** → It stops finding it. Mitigated by the override, which lets them record exactly what they choose, and by the error handler still receiving everything.
- **The classification vocabulary becomes API** → Once these codes are stored and read back, changing them is a compatibility decision. Mitigated by keeping the set small, naming it as constants beside the existing `EmailSkip*` values, and settling it before the first tag.
- **Bounding the doubling changes computed delays for an existing configuration** → Only for configurations that overflow today, whose current delays are negative or zero. Every non-degenerate configuration produces the same numbers as before.

## Migration Plan

1. No schema change, no data migration. Existing `reason` values stay as they are; new records use the new policy.
2. Hosts on defaults: retry timing is unchanged; stored reasons become classifications.
3. Hosts wanting the old reason text: add the recording rule, returning the error's text if they accept storing it.
4. Rollback is reverting the change; nothing written under it is unreadable afterwards.

## Open Questions

None. The classification vocabulary's exact member names are a naming matter for implementation, not a design decision: they change neither the specs nor the task breakdown.
