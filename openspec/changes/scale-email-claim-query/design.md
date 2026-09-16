## Context

See `proposal.md` — Why. The constraints that shape every decision below:

- **The three branches are disjoint by delivery status.** Branch 1 is `d.notification_id IS NULL`; branch 2 is `d.status = 'SENDING'`; branch 3 is `d.status <> 'SENDING'` ANDed with `writeLapsed`, which narrows it to `CLAIMED`/`RETRY` (`sqlstore/email.go:163-169`). Terminal statuses — `SENT`, `SKIPPED`, `FAILED`, `ABANDONED` — match nothing. Disjointness is what makes `UNION ALL` a behaviour-preserving rewrite rather than an approximation.
- **The branches do not share a notification predicate.** Branches 1 and 3 require `qualifies(n)` — `state = 'ACTIVE'` inside the `[now-maxLag, now-grace]` window (`:181-185`). Branch 2 does not: an in-doubt send is resolved whatever the notification's state now is. Any rewrite that hoists `qualifies` out of the disjunction changes which rows are claimed.
- **A delivery whose notification is gone is not returned.** The current shape drives `FROM notifications LEFT JOIN deliveries`, so orphaned delivery rows are invisible here; `PurgeEmailRecords` removes them (`:410-450`). Branch 2 and 3 rewrites that drive from deliveries must inner-join back to notifications to preserve that.
- **MySQL cannot declare an index idempotently outside `CREATE TABLE`.** It has no `CREATE INDEX IF NOT EXISTS`, which is why every MySQL document here declares indexes inside the table (`ddl/mysql.sql` header, `ddl/email/mysql.sql` header). It also means **no** document in this repo can add an index to an already-existing MySQL table by re-application.
- **`pkg/sqlkit` is an unedited copy** enforced by `make sqlkit-copy-check`. `ApplySchema` runs statements with no error tolerance (`pkg/sqlkit/schema.go:62-74`) and cannot be taught to ignore a duplicate-index error.
- **`VerifySchema` requires indexes by name and tolerates a columns-less expectation.** `indexIssues` returns nothing for a table absent from the live schema and otherwise checks only the listed index names (`pkg/sqlkit/verify.go:224-241`); `columnIssues` over an empty `Columns` list reports nothing. So an expectation entry for `ntfy_notifications` carrying only `Indexes` is mechanically supported — which is what makes D2 possible.

## Goals / Non-Goals

**Goals:**

- Every branch of the claim is served by an index, on all three dialects.
- Pass cost scales with the max-lag window and the number of live delivery records, not with the size of `ntfy_notifications`.
- A pass that claims nothing is cheap.
- Identical results: same candidates, same oldest-first order, same limit, same `recorded` flag.

**Non-Goals:**

- Fairness. `ORDER BY created_at, id` stays global; per-channel or per-tenant fairness belongs to the change that introduces those dimensions. This design must not make it harder — see D5.
- Eliminating the re-examination of already-considered notifications inside the window (D4).
- Any change to lease semantics, take-over, batching or `DispatchResult`.
- Touching the base notification schema's index list, or `VerifySchema`.

## Decisions

### D1. Split the disjunction into three `UNION ALL` branches, each with its own driving table

```
  (branch 1: unrecorded)                 (branch 2: in doubt)            (branch 3: due retry)
  FROM notifications n                   FROM deliveries d               FROM deliveries d
  WHERE state='ACTIVE'                     JOIN notifications n            JOIN notifications n
    AND created_at BETWEEN ? AND ?       WHERE d.status='SENDING'        WHERE d.status IN ('CLAIMED','RETRY')
    AND NOT EXISTS (SELECT 1 FROM          AND lease lapsed                AND lease lapsed AND attempt due
        deliveries d WHERE                                                 AND n.state='ACTIVE'
        d.notification_id = n.id)                                          AND n.created_at BETWEEN ? AND ?
  ORDER BY created_at, id LIMIT k        ORDER BY ... LIMIT k            ORDER BY ... LIMIT k
                    |                            |                               |
                    +------------- UNION ALL ----+-------------------------------+
                                        ORDER BY created_at, id LIMIT k
```

Each branch projects `(id, created_at, recorded)`. `recorded` is a literal per branch — `0` for branch 1, `1` for branches 2 and 3 — which removes the `COALESCE(d.notification_id, '')` trick the current query uses to infer it (`:188`).

Branch 1 is served by the new index (D2) with an anti-join probe on the deliveries primary key. Branches 2 and 3 are served by the existing `(status, lease_until)` and `(status, next_attempt_at)` indexes, both narrow because terminal statuses dominate a mature table and `CLAIMED`/`RETRY`/`SENDING` are transient.

*Alternatives considered:*

- **Keep one query and add indexes.** Rejected: an index cannot serve a top-level disjunction across an outer join, whatever indexes exist. This is the claim to be proved in task 1; if measurement shows a planner handling it well on all three dialects, this change should be abandoned rather than argued.
- **Drive everything from `ntfy_email_deliveries`.** Rejected: branch 1 is exactly the set with no delivery row.
- **Insert a delivery row at publish time**, making the claim purely delivery-driven and removing branch 1 altogether. Rejected: it welds the two tables together and breaks the `notification-email` requirement that a host which does not email needs none of this storage. It also puts email's write cost on every publish.
- **Three separate round trips** instead of `UNION ALL`. Rejected: the limit must be applied across the union, so the branches would need their own limit arithmetic and would see three different snapshots inside one claim transaction.

The per-branch `LIMIT k` plus the outer `LIMIT k` is what keeps the union bounded. `k` is the claim limit.

### D2. Add `(state, created_at, id)` on `ntfy_notifications`, declared in the **email** documents, required only by `VerifyEmailSchema`

Name: `ntfy_notifications_email_idx`, following the `ntfy_<table>_<purpose>_idx` convention the `ntfy-modules` spec already fixes.

A plain composite index, not a partial or filtered one. That is the decision that keeps the three dialects on the same shape: MySQL has no partial indexes at all, and SQLite's differ from PostgreSQL's. A `WHERE state = 'ACTIVE'` partial index would be smaller on PostgreSQL, at the cost of a per-dialect story this library does not want.

Placement is the interesting half. Three options:

| | Base documents, base expectation | Base documents, email expectation | **Email documents, email expectation** |
|---|---|---|---|
| Who carries the index | every host | every host | only hosts that email |
| Who fails `VerifySchema` on an old schema | every host | nobody | nobody |
| Who fails `VerifyEmailSchema` | — | email hosts | email hosts |
| Write cost on a non-email host | pays for an index nothing queries | same | none |
| MySQL idempotence | preserved | preserved | **one non-idempotent statement** |

The third is chosen. `notification-email` states that "A host that does not use email SHALL NOT need the storage email delivery requires", and an index on `ntfy_notifications` that exists solely to make email claiming fast is exactly that storage. Making every publish maintain it on hosts that never send an email contradicts the principle the email schema is built on.

The price is one MySQL statement that is not `IF NOT EXISTS`, because MySQL offers no such form and `sqlkit` cannot be taught to tolerate the duplicate-index error. Consequences, all of which must be written down rather than discovered:

- `ddl/email/mysql.sql` gains a standalone `CREATE INDEX` on `ntfy_notifications`. Re-applying that document to a database that already has the index fails with error 1061.
- `docs/schema.md`'s "Every statement is `CREATE ... IF NOT EXISTS`" promise gains its one stated exception, in the same place the promise is made.
- `MigrateEmail` exists for tests and development and is expected to be re-runnable. It is `sqlstore` code, not `sqlkit` code, so it may check `information_schema.STATISTICS` for the index on MySQL and skip the statement when present. That keeps the harness and `store-matrix` working without touching the frozen copy.

*Alternative considered:* declare the index in the base documents but require it only in `emailSchemaExpectation` (the middle column). It buys MySQL idempotence and costs every non-email host an unused index on the hottest table in the schema. Worth revisiting only if the MySQL exception proves more painful in practice than the write amplification.

### D3. The acceptance threshold

A performance change with no threshold cannot be verified, so the implementer must meet all four, on PostgreSQL, MySQL and SQLite:

1. **No full scan of `ntfy_notifications` in any branch.** From `EXPLAIN` output: no `Seq Scan` (PostgreSQL), no `type: ALL` (MySQL), no `SCAN ntfy_notifications` without an index (SQLite).
2. **Cost scales with the window, not the table.** Seed 1M notifications with ~50k inside the max-lag window, measure an empty pass, then add 1M more rows *outside* the window and measure again: the second measurement must be within noise of the first. This is the criterion that actually captures the defect, and it is dialect-independent.
3. **An empty pass — the common case for an idle deployment — completes in under 100 ms** on PostgreSQL at that size, with the before-figure recorded alongside it.
4. **A pass claiming the default 500 candidates stays under 500 ms**, so the rewrite does not trade idle cost for working cost.

Numbers 3 and 4 are anchors for a specific seeded shape, not a promise about arbitrary hardware; the report records the machine and the seed. If a threshold cannot be met on one dialect, that is a finding to report rather than a number to lower quietly.

### D4. The residual: already-considered notifications are re-examined, and that is accepted here

Branch 1 walks ACTIVE notifications in the window in `created_at` order and probes the deliveries primary key for each. A notification that was emailed an hour ago is still ACTIVE, still inside the 24-hour window, and still has to be probed and discarded. An empty pass therefore walks the whole window.

That is O(window), not O(table), and the window is bounded by `DefaultEmailMaxLag`. Threshold 2 is what makes the distinction explicit.

Removing the residual needs a way to say "everything before here has been considered" — a frontier cursor in the email schema, advanced only over a fully processed contiguous prefix. It is deliberately **not** in this change: it is correctness-sensitive (a notification committing late, or an instance's clock ahead of another's, could fall behind an advanced cursor and never be emailed), and it should be justified by a measurement rather than by anticipation. The trigger to revisit: a host whose max-lag window holds enough ACTIVE notifications that threshold 3 is breached by branch 1 alone.

### D5. Ordering stays global, and the shape must not entrench it

`ORDER BY created_at, id` across the union preserves today's oldest-first claim exactly. It also preserves today's starvation risk: one backlog can crowd out everything else once channels or tenants exist.

The `UNION ALL` shape is chosen partly because it does not make that worse. Adding a channel or tenant predicate later means adding a leading column to the new index and a predicate to each branch, not unpicking a disjunction. Anything that would require the branches to be merged back into one scan should be rejected for that reason.

## Risks / Trade-offs

- **The rewrite silently changes which rows are claimed** → The conformance suite (`ntfytest.RunEmail`, `RunEmailDispatch`) covers claim semantics across every store and dialect and must pass untouched; `make store-matrix` runs all seven combinations. Any test edit needed to make the new query pass is a signal that behaviour moved, not that the test was wrong.
- **Measurement is done on a seeded shape that does not match a real host** → The threshold is written as a *relative* invariant (2) as well as absolute numbers (3, 4), so the conclusion survives different hardware.
- **MySQL's non-idempotent statement bites a host re-running the email document** → Stated in the document header, in `docs/schema.md`, and in the migration note; `MigrateEmail` guards it so the repo's own flows stay re-runnable.
- **A planner regresses on one dialect** (for example MySQL choosing the wrong index for branch 3, where two are applicable) → `EXPLAIN` is captured per dialect in tasks 1 and 4, not on PostgreSQL alone.
- **The new index slows publishing** on hosts that email, since every insert maintains one more index → measured in the same seeded harness as part of task 4, and cheap relative to what a pass currently costs.
- **The residual (D4) turns out to dominate** → Threshold 2 will show it as window-proportional cost, and the follow-up is already described.

## Migration Plan

1. **Fresh hosts:** apply `Store.Schema()` then `Store.EmailSchema()` as today; the index is created with the email schema. Nothing else to do.
2. **Existing hosts that email**, per dialect, before deploying the upgrade — `VerifyEmailSchema` fails at startup until it is done, which is the intended signal:
   - PostgreSQL / SQLite: re-apply the email document, or run the `CREATE INDEX IF NOT EXISTS` statement from it.
   - MySQL: run the explicit `ALTER TABLE <prefix>ntfy_notifications ADD KEY <prefix>ntfy_notifications_email_idx (state, created_at, id)`. Re-applying the document is *not* the path on MySQL, and the note must say so.
3. **Hosts that do not email:** nothing. `VerifySchema` is unchanged.
4. **Rollback:** revert the code, then optionally drop the index. `docs/schema.md`'s rollback section currently covers dropping the two base tables only; it gains the email schema's own rollback, including this index, which lives on a table that survives the email table being dropped.
5. Nothing rewrites data, so a rollback strands no state.

## Open Questions

- Whether the frontier cursor of D4 is ever needed, and at what window size. Deferrable: it changes neither this query's contract nor the task breakdown, and the measurement produced here is what should decide it.
