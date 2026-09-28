## Context

See `proposal.md`, Why. Two stores implement the close, and the suite that holds them to one behaviour lives in a third module:

| Where | What it does today |
| --- | --- |
| `memory.go:223-237`, `MemoryStore.Close` | skips a notification that is `CLOSED`, above `req.Version`, of a kind outside `req.Kinds`, or `req.Except`'s. Everything else closes. |
| `sqlstore/close.go:102-115`, `closingCondition` | the same four conditions as a `WHERE` clause. `closingRecipients` and `closeNotifications` both use it, so the recipients reported and the rows closed cannot disagree. |
| `store.go:199-229`, `CloseRequest.SuccessorInsertions` | stamps the successor for each closed recipient with `SourceID = req.Successor.SourceID`. The store inserts it through its ordinary insert path, where `(SourceID, Recipient)` is the idempotency key. |
| `ntfytest/suite.go:724-738` | the only retry case. It closes `Kinds: ["offer"]` with a `"taken"` successor, so it can never reach its own successor. |
| `service.go:207-224`, `Service.Close` | signals `ChangeClosed` to `result.Recipients`. A retry that re-closes successors therefore signals their recipients a second time. |
| `pruner.go:17-20`, `store.go:469-471` | the retention godoc: "outlives its last notification". |
| `memory.go:553-563`, `sqlstore/prune.go:225-255` | expire a record whose `updatedAt`/`updated_at` is before `Now - WatermarkRetention` and whose subject has no notifications. `updated_at` is set when the record is created (first publish or close) and on every close. |

On a retry the successors created by the first attempt are `ACTIVE`, carry `SubjectVersion = Successor.SubjectVersion`, which is normally the close's `Version`, and are of `Successor.Kind`. Once `Kinds` is empty or contains that kind, the only condition that could exclude them is the version, and it does not.

## Goals / Non-Goals

**Goals**

- A retried close changes nothing, reports no recipients and signals no one, whichever kinds it names, on every store.
- The rule is stated where a host implementing `Store` reads it, and the conformance suite fails a store that lacks it.
- The watermark retention godoc says what every store measures.

**Non-Goals**

- **Making a retried close idempotent against *other* closes' successors.** Suppose close A with no successor is retried after close B, at or above A's version, created successors at or below A's version. A's retry closes B's successors. Nothing in a close request identifies what B created. Only a record of which close produced which notification would answer it, and that is a schema change. This case needs two different closes on one subject to interleave with a retry, and the spec's retry guarantee is about one close retried. It is recorded here as a known limit, not fixed.
- **Changing what the retention measures.** See D3.
- **Making the close record outlive the subject's last notification.** That would need a "last notification deleted" time the stores do not keep.

## Decisions

### D1. A close leaves out its own successor's source

When `req.Successor` is set, the close selects no notification whose `SourceID` equals `req.Successor.SourceID`:
- memory adds one `case` to its selection `switch`;
- SQL adds `AND source_id <> ?` to `closingCondition`, so the recipients read and the rows updated stay the same set.

| Option | Retry closes nothing | A later close still closes the successor | Store or schema change | Verdict |
| --- | --- | --- | --- | --- |
| **Exclude `SourceID == Successor.SourceID`** | yes: every successor of this close carries that source, and a retry carries the same successor | yes: a later close names another successor, or none | one condition per store, on a column every store has | **chosen** |
| Exclude notifications at `SubjectVersion == Version` | yes | yes | one condition | **rejected**: a close at version V must close a notification at V (spec: "at or below a given version"; the "Closing one kind" scenario closes at the subject's current version) |
| Exclude the successor's kind | yes | yes | one condition | **rejected**: a close of every kind must close earlier notifications of that kind from other sources |
| Exclude notifications created at or after the first attempt | yes | yes | needs the first attempt's instant, which a retry does not carry | **rejected**: clock-dependent, and a retry's `at` is new |
| Record the closing close on each successor | yes, and covers the other-close case in the Non-Goals | yes | a new column on every dialect, an upgrade for every host | **rejected**: disproportionate to the defect |

Why the source is safe to exclude: `(SourceID, Recipient)` is the idempotency key of every publish. A notification carrying the successor's source for a recipient *is* that recipient's successor, or something the same event already published. The event that closes a subject has no reason to close its own output.

An empty `Successor.SourceID` never reaches a store through the service: `CloseRequest.ValidateWithin` refuses it through `validateContent` (`store.go:177-185`). The condition is written only when `req.Successor != nil`, so a close without a successor is exactly as it was.

**Default:** a close leaves out its own successor's source. **Override:** none, deliberately. It is the spec's retry guarantee, and a guarantee the library makes is stated, not configurable (`library-design.md` rule 4). A consumer who wants a successor closed issues a later close, which does close it (spec scenario "A later close still closes an earlier close's successors").

### D2. The proof lives in the conformance suite, as a table

The existing case "a retried close creates no further successors" becomes a three-row table in `runSuccessors`: one kind (today's case), every kind, and kinds naming the successor's kind. Every row shares one assertion closure:
- the retry closes nothing, reports no recipients and creates nothing;
- each successor from the first attempt is still `ACTIVE`;
- each recipient keeps exactly one successor.

It lives in `ntfytest`, not in a per-store test. The defect is a store contract, and `ntfytest.Run` is how the library holds its own stores and a host's to the contract. The memory store runs it through `TestMemoryStoreConformance`, and SQL through the seven `store-matrix` entry points.

The `ctx` field of the `table-test` skill is left out: closing does not depend on cancellation, and the table says so in a one-line comment. Rows run through the suite's `parallel` helper, as every suite case does.

### D3. Fix the retention godoc, not the stores

| Option | Verdict |
| --- | --- |
| **Godoc says "kept for this long after it last changed, once its subject has no notifications left"** | **chosen**: it matches both stores, `docs/schema.md` (`updated_at`: "watermark retention measures from it") and the spec's "close record is older than that retention" |
| Stores measure from the deletion of the subject's last notification | **rejected**: needs a new per-subject timestamp on every store and dialect, a schema change and an upgrade, to buy a guarantee the retention's purpose does not need. The retention bounds *late redelivery of a source*, and a redelivery's lateness is counted from the event, and so from the close. |

`WithWatermarkRetention`'s godoc already says what expiry means for a late source. It gains the measurement, so that the option names the default it replaces and how it is counted.

The retention spec gets the same sentence and a scenario in which the record goes in the same pass as the subject's last notification. `runRetention` asserts that scenario on every store. It passes on today's code, being a characterisation of intended behaviour. Its red step is a temporary mutation of `MemoryStore.pruneWatermarks`, reverted before commit (`golang-tdd.md`: "invert the implementation temporarily and confirm the test notices").

The godoc wording is guarded by `TestWatermarkRetentionGodocSaysWhatTheStoreMeasures` in `docs_test.go`. It reads the doc comment above each declaration in `pruner.go` and `store.go` as text and asserts on it. The core module's depguard allow-list rejects `go/ast`, `go/parser` and `go/token`, as a dry run on 2026-09-28 showed. This follows the file's existing practice of pinning documented claims to the code, and it is the red step for the godoc defect.

## Risks / Trade-offs

- [A host's own `Store` fails `ntfytest.Run` after upgrading ntfytest] → Intended: that store has the same defect. The proposal's Impact and `Store.Close`'s godoc name the one condition to add.
- [An event that publishes and closes under one `SourceID` can no longer close its own publish] → The same source is already an idempotency key. A close that must close such a notification can omit the successor, or use a distinct successor source. `Store.Close`'s godoc states the rule.
- [The SQL condition changes the close's plan] → `source_id <> ?` is a residual filter on rows already found through `(subject, …)`. No index changes, and no performance claim is made (`performance-benchmark.md` does not apply).
- [The other-close interleaving in the Non-Goals still re-closes successors] → Recorded as a known limit. The spec's retry guarantee is scoped to one close retried.

## Migration Plan

No schema or data change. Deploying the new library is the whole migration. Rolling back restores the old selection; nothing written in between depends on it.
