## Why

Every email dispatch pass starts with one query, `dueEmails` (`sqlstore/email.go:173-207`), and that query cannot use an index. It drives from `ntfy_notifications` LEFT JOINed to `ntfy_email_deliveries`, filters on a three-branch top-level `OR`, and orders by `n.created_at, n.id` — an ordering no index provides, because every `created_at` index leads with `recipient` (`sqlstore/ddl/postgres.sql`). There is no index on `(state, created_at)` at all.

The cost is paid on **every** pass, including passes that claim nothing, and it grows with the whole table rather than with the work available. A host running `EmailDispatcher.Run` every minute pays it 1,440 times a day against a table that retention lets grow for 90 days by default. Nothing is tagged yet, so the index list and the query shape can still change freely.

## What Changes

- **The claim query is restructured** so each of its three disjoint branches is served by an index: notifications with no delivery record, in-doubt `SENDING` records whose lease lapsed, and due `CLAIMED`/`RETRY` records. The rows returned, their order and the claim's semantics are unchanged.
- **One index is added to `ntfy_notifications`**, covering the ACTIVE-in-window drive the first branch needs. It is declared in the **email** DDL documents, not the base ones, because only a host that emails needs it — with a stated MySQL exception, since MySQL has no `CREATE INDEX IF NOT EXISTS`.
- **`VerifyEmailSchema` requires the new index**; `VerifySchema` does not. A host that never emails neither needs it nor fails startup over it.
- **The change is measured before and after.** Per `.claude/rules/prove-errors-with-tests.md` the claim "this query cannot use an index" is a claim about query plans and must be proved: the first tasks capture `EXPLAIN` output and pass timings on all three dialects at a stated row count, and the last re-run them against a stated threshold.
- **No behaviour changes.** Same candidates, same oldest-first order, same limit, same lease and take-over semantics, same `DispatchResult`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This change sets `skip_specs: true`.

The reasoning, since the boundary is not obvious. Two existing requirements touch this area and **neither** changes:

- `notification-email` — "Email delivery state is durable and identical on every store", whose scenario *Email storage is optional* requires that a host without the email schema still passes notification schema verification. That is why the new index is required by `VerifyEmailSchema` and not by `VerifySchema`: the base store's statements do not use it, and the scenario keeps holding unchanged.
- `ntfy-modules` — "the SQL store SHALL default to the tables `ntfy_notifications`, `ntfy_watermarks` and `ntfy_email_deliveries`, and to indexes named after them." The new index follows that convention, so the requirement already covers it.

Every observable behaviour the specs describe — which notifications a pass claims, in what order, under what lease, and what it reports — is identical before and after. An index that changes which rows are returned would be a bug, not a spec change. Inventing a requirement to satisfy `openspec validate` would be worse than the marker.

## Impact

- **Code:** `sqlstore/email.go` — `dueEmails` (`:173-207`) restructured; `writeLapsed` (`:163-169`) reused per branch; `emailSchemaExpectation` (`:36-45`) gains a `ntfy_notifications` entry carrying only the new index; `MigrateEmail` (`:71-73`) must stay usable on MySQL, where the new statement is not idempotent.
- **Schema:** `sqlstore/ddl/email/{postgres,mysql,sqlite}.sql` gain one index on `ntfy_notifications`. `pkg/sqlkit` is an unedited copy (`make sqlkit-copy-check`) and cannot be changed to help, which constrains how the MySQL statement is written.
- **Migration:** hosts apply DDL through their own pipeline. Adding an index to an existing table is not something these documents can do on MySQL by re-application, so the migration note must give the explicit `ALTER TABLE`. Rollback must also drop the index, which `docs/schema.md`'s "Rolling back" section does not currently cover for the email schema.
- **Tests:** a seeded measurement harness under `sqlstore` capturing `EXPLAIN` and pass timings per dialect; `make store-matrix` must stay green across all seven driver-and-dialect combinations.
- **Docs:** `docs/schema.md` — the index table, the "Every statement is `CREATE ... IF NOT EXISTS`" promise (now with one stated MySQL exception), and the rollback section.
- **Not in scope:** the global `ORDER BY created_at, id` means one backlog can starve another once channels or tenants exist. This change must not make that worse, and does not fix it — it belongs with the work that introduces those dimensions. Also out of scope: the residual cost of re-examining already-considered notifications inside the max-lag window, recorded in `design.md` as a follow-up with the condition that would trigger it.
