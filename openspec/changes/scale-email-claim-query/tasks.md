## 1. Prove the claim before changing anything

- [ ] 1.1 Add a seeding helper under `sqlstore` (test-only, alongside `internal/harness`) that fills `ntfy_notifications` and `ntfy_email_deliveries` to a stated shape: 1M notifications, ~50k inside the default max-lag window, ~90% of those already carrying a terminal delivery record. Verify it by counting rows per state and per delivery status after a seed and asserting the shape it promised.
- [ ] 1.2 Capture the plan of today's `dueEmails` on PostgreSQL (`EXPLAIN (ANALYZE, BUFFERS)`), MySQL (`EXPLAIN FORMAT=JSON`) and SQLite (`EXPLAIN QUERY PLAN`) against the seeded database, and record the output in the change directory. Verify the claim from `design.md` — Context: the output must show a full scan of `ntfy_notifications` plus a join and a sort, on each dialect. **If any dialect does not show it, stop and report**: the premise of this change is wrong and the rest of the tasks do not apply (`.claude/rules/prove-errors-with-tests.md`).
- [ ] 1.3 Record the baseline timings on the same seed: an empty pass (nothing claimable) and a pass claiming the default 500, each repeated enough times to quote a median, with the machine and seed noted. Verify by re-running: the medians must be stable within noise.
- [ ] 1.4 Record the window-versus-table baseline: add 1M notifications *outside* the max-lag window and re-time the empty pass. Verify the defect is visible — the pass must get materially slower even though no new work became claimable (threshold 2 in `design.md` — D3).

## 2. Add the index

- [ ] 2.1 Add `ntfy_notifications_email_idx (state, created_at, id)` to `sqlstore/ddl/email/postgres.sql` and `ddl/email/sqlite.sql` as `CREATE INDEX IF NOT EXISTS`, with a header comment saying why an index on the notifications table lives in the email document. Verify by applying each document twice to a fresh database: the second application must be a no-op.
- [ ] 2.2 Add the same index to `ddl/email/mysql.sql` as a standalone `CREATE INDEX`, and state in that document's header that this one statement is not idempotent and why (no `CREATE INDEX IF NOT EXISTS` in MySQL). Verify by applying to a fresh MySQL database and confirming the index exists in `information_schema.STATISTICS`.
- [ ] 2.3 Guard `MigrateEmail` on MySQL so it skips the index statement when the index is already present, keeping the dev and test path re-runnable without touching `pkg/sqlkit`. Verify by calling `MigrateEmail` twice against MySQL in a test and asserting the second call returns no error; verify `make sqlkit-copy-check` still passes.
- [ ] 2.4 Add a `ntfy_notifications` entry carrying only `Indexes: ["ntfy_notifications_email_idx"]` to `emailSchemaExpectation` (`sqlstore/email.go:36-45`), leaving `schemaExpectation` in `store.go` untouched. Verify with two tests: `VerifyEmailSchema` fails naming the missing index when only the base schema is applied, and `VerifySchema` still succeeds in that same state (the `notification-email` scenario *Email storage is optional*).

## 3. Restructure the claim query

- [ ] 3.1 Rewrite `dueEmails` (`sqlstore/email.go:173-207`) as the three-branch `UNION ALL` of `design.md` — D1, each branch projecting `(id, created_at, recorded)` with `recorded` as a per-branch literal, each with its own `LIMIT`, wrapped in an outer `ORDER BY created_at, id LIMIT`. Keep `writeLapsed` as the single source of the lease predicate. Verify the `sqlstore` package builds and its own unit tests pass.
- [ ] 3.2 Update the row reader from 2 columns to the new projection and drop the `COALESCE(d.notification_id, '')` inference of `recorded`. Verify with a test asserting `dueEmail.recorded` is true for a lapsed record and false for an unrecorded notification.
- [ ] 3.3 Confirm the branch semantics the rewrite must preserve, each with a test: an in-doubt `SENDING` record is claimed even when its notification is no longer ACTIVE; a `CLAIMED`/`RETRY` record is claimed only while its notification still qualifies; a delivery row whose notification was deleted is not returned; terminal statuses are never returned.
- [ ] 3.4 Verify no behaviour moved: run `ntfytest.RunEmail` and `RunEmailDispatch` through the conformance entry points unchanged. If a test needs editing to pass, stop — that means the rewrite changed what is claimed.

## 4. Re-measure against the threshold

- [ ] 4.1 Re-capture `EXPLAIN` on all three dialects against the same seed and verify threshold 1: no `Seq Scan` on PostgreSQL, no `type: ALL` on MySQL, no unindexed `SCAN ntfy_notifications` on SQLite, in any branch.
- [ ] 4.2 Re-run the window-versus-table measurement from 1.4 and verify threshold 2: adding 1M rows outside the window must leave the empty-pass time within noise of the measurement taken without them.
- [ ] 4.3 Re-run the pass timings and verify thresholds 3 and 4: an empty pass under 100 ms and a 500-candidate pass under 500 ms on PostgreSQL at the seeded size, quoting before-and-after medians. Report any dialect that misses a threshold rather than adjusting the threshold.
- [ ] 4.4 Measure the index's cost on the write side: time a bulk publish with and without `ntfy_notifications_email_idx` present, and record it so the trade-off is on the record (`design.md` — Risks).
- [ ] 4.5 Write the measurements — plans, medians, machine and seed — into the change directory so the archived change carries its own evidence.

## 5. Documentation and full verification

- [ ] 5.1 Update `docs/schema.md`: add the index to the index table noting it belongs to the email schema, add the one stated MySQL exception where the "Every statement is `CREATE ... IF NOT EXISTS`" promise is made, and extend the rollback section to cover the email schema including this index on a table that outlives it. Verify by re-reading: a host must be able to find the MySQL `ALTER TABLE` without reading the DDL.
- [ ] 5.2 Add the migration note of `design.md` — Migration Plan to the docs, per dialect, stating that on MySQL re-applying the email document is not the upgrade path. Verify the `ALTER TABLE` given there runs cleanly against a MySQL database holding the old schema.
- [ ] 5.3 Run `make all` and verify lint, split-check and tests pass on every module.
- [ ] 5.4 Run `make store-matrix` and verify all seven driver-and-dialect combinations pass.
