## Why

The inbox spec requires every store to behave identically. The 2026-09-28 adversarial audit found five places where the memory store answers differently from the SQL store, on inputs the `Store` and `EmailStore` contracts do not exclude. Re-proving them on current `main` (after PR #11) found three more of the same kind, and one place where MySQL disagrees with PostgreSQL and SQLite. Each is **Low** on its own: the service never sends most of these inputs. But a host that drives a store directly, or supplies its own ID generator, sees a different store behind the same interface. And the conformance suite, which is meant to hold every store to one behaviour, catches none of them.

What diverges today:

1. **A reused notification identifier.**
   - The memory store silently replaces the stored notification, even another recipient's.
   - PostgreSQL and SQLite refuse the insert with a unique-key error and keep what they hold.
   - MySQL reports the notification as created, but stores nothing. Its `ON DUPLICATE KEY UPDATE` swallows the primary-key collision, and the read-back finds the old row under that identifier.
   - The same holds for an identifier repeated within one insert, and for a close successor that reuses a stored identifier. The memory store then closes the subject anyway, and MySQL reports a successor it never wrote.
2. **An insert that creates nothing on a fresh subject.** The SQL store writes the subject's `'*'` close record on every non-empty insert, because that row is its lock. The memory store writes one only when a notification is created. A later pass then reports a different `WatermarksDeleted`.
3. **Retention cutoffs at nanosecond precision.** The memory store computes `Now - MaxAge` and `Now - WatermarkRetention` without truncating to microseconds. Every SQL dialect truncates. A row inside the cutoff's own microsecond is deleted by one and kept by the other.
4. **`EmailStore` inputs outside the dispatcher's.**
   - `RecordEmails` with an empty `Owner` changes a released delivery on memory. On SQL it changes nothing, because a released row's owner is NULL.
   - `ClaimEmails` with `Limit` ≤ 0 claims everything on memory, and nothing on SQL.
   - `PurgeEmailRecords` with a limit ≤ 0 purges every orphan on memory, and nothing on SQL.

**Proved by:** the audit tests, re-run on `main` at `20967b3` on 2026-09-28 with `GOTOOLCHAIN=go1.26.8`. Each case runs one scenario on the memory store and on a SQLite store, and asserts that they observe the same thing. The expected value is SQL's; the actual value is memory's:

```
cd sqlstore && go test -run 'TestAuditMemoryMatchesSQL|TestAuditEmailStoreDivergence' -count=1 .

--- FAIL: TestAuditMemoryMatchesSQL/an_insert_reusing_a_stored_identifier
        expected: map[string]bool{"alice's notification kept":true, "insert refused":true}
        actual  : map[string]bool{"alice's notification kept":false, "insert refused":false}
--- FAIL: TestAuditMemoryMatchesSQL/an_insert_repeating_an_identifier_within_itself
        expected: map[string]bool{"alice stored":false, "bob stored":false, "insert refused":true}
        actual  : map[string]bool{"alice stored":false, "bob stored":true, "insert refused":false}
--- FAIL: TestAuditMemoryMatchesSQL/a_close_whose_successor_reuses_a_stored_identifier
        expected: map[string]interface {}{"alice's state":"ACTIVE", "carol's kept":true, "close refused":true}
        actual  : map[string]interface {}{"alice's state":"CLOSED", "carol's kept":false, "close refused":false}
--- FAIL: TestAuditMemoryMatchesSQL/an_insert_that_creates_nothing_on_a_fresh_subject
        expected: 1
        actual  : 0
--- FAIL: TestAuditMemoryMatchesSQL/a_notification_inactive_in_the_age_cutoff's_microsecond
        expected: 0
        actual  : 1
--- FAIL: TestAuditMemoryMatchesSQL/a_close_record_changed_in_the_retention_cutoff's_microsecond
        expected: 0
        actual  : 1
--- FAIL: TestAuditEmailStoreDivergence/a_record_with_an_empty_owner_changes_a_released_delivery
        expected: 0
        actual  : 1
--- FAIL: TestAuditEmailStoreDivergence/a_claim_with_no_limit
        expected: 0
        actual  : 1
--- FAIL: TestAuditEmailStoreDivergence/a_purge_with_no_limit
        expected: 0
        actual  : 1
```

The audit's four findings are the rows reusing a stored identifier, creating nothing on a fresh subject, the age cutoff, the empty owner and the claim with no limit. The within-insert, close-successor, watermark-cutoff and purge rows were added while re-proving, as hypotheses from reading the code, and each was seen to fail before it was recorded here.

The MySQL half of item 1 was proved by a probe against MySQL 8.4.6 and PostgreSQL in containers:

```
PostgreSQL reuse:    insertErr=sqlkit: pq: duplicate key value violates unique constraint "t1_ntfy_notifications_pkey" (23505)
PostgreSQL in-batch: insertErr=sqlkit: pq: duplicate key value violates unique constraint "t2_ntfy_notifications_pkey" (23505)
PostgreSQL close:    closeErr=sqlkit: pq: duplicate key value violates unique constraint "t3_ntfy_notifications_pkey" (23505)
MySQL reuse:         insertErr=<nil> created=1 bobStored=false
MySQL in-batch:      insertErr=<nil> created=2 aliceStored=true bobStored=false
MySQL close:         closeErr=<nil> successors=1 aliceState=CLOSED aliceSuccessorStored=false
```

A guard that already passes was also run. A claim and a record under the same empty owner change the delivery on both stores. The fix for item 4 must keep that.

Task 1 lands every one of these as conformance cases in `ntfytest`, and watches them fail, before any store changes.

**Not in this change**, because PR #11 already fixed them on `main`:
- generated identifiers outside 1–64 bytes;
- NUL or invalid UTF-8 in identifiers;
- a memory close whose successor cannot be minted.

The audit's first `TestAuditMemoryMatchesSQL` case, "a close whose successor cannot be stamped", passes on `main` and was dropped.

## What Changes

- **A reused notification identifier is refused on every store.** An `Insert` or `Close` that would write a notification under an identifier already stored, or twice in one call, fails with an error and changes nothing. Suppressed, coalesced and duplicate insertions are never written, so their identifiers are never checked.
  - The memory store checks before it writes, and a close undoes its own changes when its successors are refused.
  - The SQL store's read-back now compares each written identifier's source and recipient. That makes MySQL fail the transaction instead of reporting a notification it did not store.
  - PostgreSQL and SQLite already refuse. They keep returning their driver's unique-key error.
- **Every non-empty insert starts the subject's close record**, on the memory store as on SQL: at version −1, stamped with the first insertion's creation time, whether or not anything is created.
- **The memory store truncates both retention cutoffs to microseconds**, the precision every store keeps.
- **The `EmailStore` edges are pinned to the SQL behaviour:**
  - a record changes only deliveries that some claim holds, so an empty owner never matches a released delivery;
  - a claim or a purge with a limit of zero or less does nothing.
- **The conformance suites carry every case.** Each case goes in `ntfytest.Run` or `ntfytest.RunEmail`, so a host's own store is held to them too.
- **The `Store` and `EmailStore` godoc state these edges**, and `IDGenerator` says what a repeat costs.

No option is added and no default changes for a host using the service. **BREAKING** only for a caller that relied on the memory store replacing a notification under a reused identifier. No tag has been cut, so this is recorded rather than versioned.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-inbox`: adds a requirement that a notification identifier is stored once, and that a write reusing one fails and changes nothing. The "Every store behaves identically" requirement names the store-contract edges the shared suite now covers.
- `notification-retention`: close records start with the subject's first publish, whatever it creates. Retention cutoffs are compared at microsecond precision.
- `notification-email`: pins what a claim or purge with no positive limit does, and what a record under an owner that holds nothing does, on every store.

## Impact

- **Code:**
  - `memory.go`: `Insert`, `insert`, `put`, `Close` and `Prune`.
  - `memory_email.go`: `ClaimEmails`, `RecordEmails` and `PurgeEmailRecords`.
  - `sqlstore/insert.go`: `writeNotifications` reads back source and recipient.
  - Godoc in `store.go`, `email.go` and `id.go`.
- **Tests:**
  - new cases in `ntfytest/suite.go` (`runInbox`, `runSuccessors`, `runRetention`) and `ntfytest/email.go` (`runEmailEligibility`, `runEmailOutcomes`, `runEmailPurge`);
  - the reused-identifier row of `TestMemoryStoreRecipientIndexMirrorsTheStore` in `memory_index_test.go` is rewritten: it asserted the replacement this change removes.
- **Hosts:**
  - A host on the service sees no change, unless its ID generator repeats identifiers. The publish or close that repeats one then fails, instead of overwriting (memory) or reporting a phantom notification (MySQL).
  - A host with its own store runs `ntfytest.Run` and `ntfytest.RunEmail`, and fixes any new case that fails.
- **Data:** nothing to migrate. No schema changes.
- **Dependencies:** nothing new.
