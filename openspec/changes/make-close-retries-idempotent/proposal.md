## Why

A retried close undoes its own work. The inbox spec says "a close that is retried after an earlier attempt committed SHALL close nothing further". But a close of every kind, or one whose `Kinds` include its successor's kind, selects the successors its first attempt created. Those successors are `ACTIVE`, of a kind the close covers, and at a version no higher than the close's, so the retry closes them. Every recipient the first attempt told what happened loses that notification, and the service signals them again. A publisher retries a close on any error it cannot tell from a lost commit, so this is the ordinary retry path, not a corner case.

The memory store and the SQL store both do it:
- memory: `memory.go` `Close` selects every notification that is not `CLOSED` at or below the close version;
- SQL: `sqlstore/close.go` `closingCondition` writes the same condition.

The conformance suite only retries a close with `Kinds: ["offer"]`, which cannot reach its own `"taken"` successor. So it passes every store.

Separately, the godoc of `DefaultWatermarkRetention` and `PruneRequest.WatermarkRetention` promises more than any store does. It says retention is "how long a subject's close record outlives its last notification". Every store measures it from the record's last change: its `updated_at`, which is the subject's latest close. `docs/schema.md` and the retention spec say the same. A host reading the godoc expects a late source to be suppressed for 7 days after the subject's last notification is pruned. With the defaults, the record usually goes in the same pass as that last notification.

**Proved by:** the 2026-09-28 adversarial audit, re-proved on `main` at `20967b3` on 2026-09-28 (Apple M4 Pro, `GOTOOLCHAIN=go1.26.8`, Docker for PostgreSQL and MySQL).

1. **Retried close.** The audit proofs were `TestAuditClose/a_retried_close_of_every_kind_closes_nothing_further` (memory) and `TestAuditRetriedCloseOfEveryKindOnSQL` (SQLite). They were re-proved as a table of three rows in `ntfytest`'s `successors` group, so that every store runs them. Two rows fail on every store tried: memory, and database/sql on SQLite, PostgreSQL and MySQL. The existing single-kind retry row passes.

   ```
   --- PASS: TestMemoryStoreConformance/successors/a_retried_close_of_one_kind_closes_nothing_further
   --- FAIL: TestMemoryStoreConformance/successors/a_retried_close_of_every_kind_closes_nothing_further
           Error:      Should be zero, but was 2
           Messages:   a retried close closes nothing further
           Error:      Should be empty, but was [alice bob]
           Messages:   a retried close tells no recipient
           Error:      Not equal: expected: "ACTIVE" actual  : "CLOSED"
           Messages:   alice keeps the successor the first attempt created
   --- FAIL: TestMemoryStoreConformance/successors/a_retried_close_naming_the_successor's_kind_closes_nothing_further
           Error:      Should be zero, but was 2
   --- FAIL: TestStoreOnStdSQLSQLite/successors/a_retried_close_of_every_kind_closes_nothing_further
   --- FAIL: TestStoreOnStdSQLPostgres/successors/a_retried_close_of_every_kind_closes_nothing_further
   --- FAIL: TestStoreOnStdSQLMySQL/successors/a_retried_close_of_every_kind_closes_nothing_further
           Error:      Should be zero, but was 2
           Error:      Should be empty, but was [alice bob]
   ```

   The three `_naming_the_successor's_kind_` rows fail on the SQL stores in the same way. Task 1 lands this table as the red step.

2. **Retention godoc.** The audit proof `TestAuditCloseRecordOutlivesLastNotification` asserts what the godoc says. It fails on the memory store:

   ```
   --- FAIL: TestAuditCloseRecordOutlivesLastNotification
           Error:      Should be zero, but was 1
           Messages:   the close record outlives the last notification
           Error:      Not equal: expected: 1 actual  : 0
           Messages:   a late source within the retention of the last notification is suppressed
   ```

   The behaviour matches the spec and `docs/schema.md`. The godoc is what is wrong. Task 2 carries the red step for the godoc, and a conformance case that pins the behaviour the corrected godoc describes.

## What Changes

- **A close never closes a notification published from its own successor's source.** When a close names a `Successor`, both stores leave out of the close any notification whose `SourceID` is the successor's `SourceID`. A retry carries the same successor, so it no longer reaches what its first attempt created. It closes nothing, names no recipients and creates nothing, whichever kinds it names.
  - A close with no successor is unchanged.
  - A later close, with another successor or none, still closes an earlier close's successors, as it does today.
- **The conformance suite holds every store to it.** The `successors` group's retry case becomes a table: one kind, every kind, and kinds that include the successor's kind. A host's own store fails `ntfytest.Run` until it applies the same rule.
- **`Store.Close`'s godoc states the rule**, so a host implementing `Store` knows it.
- **The watermark retention godoc says what the stores measure**: the time since the close record last changed, deleted once the subject has no notifications. `DefaultWatermarkRetention`, `PruneRequest.WatermarkRetention` and `WithWatermarkRetention` are corrected. Behaviour does not change.
- **The retention spec says it too**, with a scenario in which a close record expires in the same pass as its subject's last notification. The conformance suite asserts that scenario on every store.

No **BREAKING** change for callers. A host's own `Store` implementation must adopt the successor-source rule to keep passing `ntfytest.Run`. No tag has been cut, so this is recorded rather than versioned.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-inbox`: "A close can tell each recipient it closed what happened, atomically" now says a close never closes a notification from its own successor's source. It adds scenarios for a retried close of every kind, and for one naming the successor's kind.
- `notification-retention`: "Close records outlive late redelivery, then expire" now says retention counts from the record's last change. It adds the scenario in which the record expires in the same pass as the subject's last notification.

## Impact

- **Code:**
  - `memory.go`: `MemoryStore.Close`'s selection.
  - `sqlstore/close.go`: `closingCondition`.
  - `store.go`: the `Store.Close` and `PruneRequest.WatermarkRetention` godoc.
  - `pruner.go`: the `DefaultWatermarkRetention` and `WithWatermarkRetention` godoc.
- **Tests:**
  - `ntfytest/suite.go`: the retried-close table in `runSuccessors`, and a retention case in `runRetention`.
  - `docs_test.go`: a godoc check for the retention wording.
- **Docs:** `docs/notifications.md`, "Successors" and "Retention".
- **Hosts:**
  - With the library's stores, nothing to do.
  - A host with its own `Store` adds the successor-source condition to its close.
- **Data:** none. No schema change. The condition uses `source_id`, which every store already holds.
- **Dependencies:** none.
