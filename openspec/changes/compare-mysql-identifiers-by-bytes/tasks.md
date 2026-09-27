> **Revised 2026-09-28, during apply:**
> - The fix moved from `utf8mb4_0900_bin`, which needs MySQL 8.0.17, to `VARBINARY` columns. The maintainer ruled that a library cannot make its hosts upgrade their database. See `design.md` D1.
> - 1.1 and 1.2 were done before the revision, and hold unchanged.

## 1. Prove the defect (red)

- [x] 1.1 Land the audit test as `TestMySQLComparesIdentifiersByBytes` in a new `sqlstore/identity_test.go`, in the `table-test` form, over one MySQL container.
  - Recipient cases: `alice` against `alice` + U+200B, `alice` + NUL, and NFC `josé` against NFD `jose` + U+0301. Each asserts `Get` is `ErrNotFound`, `List` is empty and `CountActive` is zero.
  - Source cases: `event-1` against `event-1` + U+200B and `event-1` + NUL for one recipient create two notifications, not a duplicate.

  Verify with `cd sqlstore && go test -run 'TestMySQLComparesIdentifiersByBytes' -count=1 .`. Confirm every case fails because the intruder **was given alice's notification** or the second source **was counted a duplicate**, not because of a compile error or a container failure.
- [x] 1.2 Add `ntfytest/identity.go` with `runIdentity(t, factory)` and register it in `Run` as `t.Run("identity", ...)`. It holds the variant tables from `design.md` D4 (case, trailing space, U+200B, NFC/NFD) for:
  - recipient (`Get`/`List`/`CountActive`/`MarkRead`);
  - source (not a duplicate);
  - subject (close leaves the other open and does not suppress it);
  - kind (list filter).

  Verify with `go test -run 'TestMemoryStoreConformance/identity' -count=1 .` (passes on memory). Then run `cd sqlstore && go test -run 'TestStoreOnStdSQLMySQL/identity' -count=1 .`, and confirm the U+200B and NFC/NFD rows fail on MySQL while the case and trailing-space rows already pass there. Record that split in the commit message.
- [x] 1.3 Add `ntfytest/identity_test.go`, proving the new group rejects a store that folds identifiers.
  - It runs `ntfytest.Run` in a child test process against three `ntfy.NewMemoryStore` wrappers: one lower-casing, one trimming trailing spaces, one stripping U+200B from every identifier. Each child must fail.
  - A plain memory store child is the control, and must pass.

  Verify with `cd ntfytest && go test -run 'TestIdentityRejectsFoldingStores' -count=1 .`. Watch the control pass and all three wrapped stores be rejected. Then temporarily remove the `identity` line from `Run`, confirm the test goes red, and restore it.

## 2. Prove verification misses it (red)

- [x] 2.1 Add a MySQL-only case table, `TestVerifySchemaRequiresByteExactIdentifiersOnMySQL`, in `sqlstore/verify_test.go`. For each of `utf8mb4_0900_as_cs` and `utf8mb4_0900_bin`, it `ALTER`s the notifications table's `recipient` column to `VARCHAR(255)` under that collation. It then requires `VerifySchema` to report `<table>.recipient: collation is "<c>" but must be "binary"`. Verify with `cd sqlstore && go test -run 'TestVerifySchemaRequiresByteExactIdentifiersOnMySQL' -count=1 .`. Both rows fail today:
  - the `utf8mb4_0900_as_cs` row, because verification accepts that collation and reports nothing;
  - the `utf8mb4_0900_bin` row, because the issue it reports names `utf8mb4_0900_as_cs` as expected.

  Even a byte-exact collation fails once the fix is in: identifier columns must be binary strings, so no collation-dependent column passes.
- [x] 2.2 Add `TestSQLKitStillExpectsTheOldMySQLCollation` in `sqlstore/verify_internal_test.go`, package `sqlstore`. It asserts `sqlkit.MySQL.IdentifierCollation() == "utf8mb4_0900_as_cs"`. Its failure message says the vendored sqlkit has changed its MySQL expectation, so `verifyDialect` should be revisited and deleted. Verify it passes now. It is a tripwire, not a red step.

## 3. Compare identifiers byte for byte (green)

- [x] 3.1 In `sqlstore/ddl/mysql.sql`, change each identifier column from `VARCHAR(n) COLLATE utf8mb4_0900_as_cs` to `VARBINARY(n)`: `id` 64, `recipient`/`source_id`/`subject` 255, `kind` 100, `state` 16, and watermark `subject` 255 and `kind` 100. Rewrite the header comment to explain why: bytes, no padding, lengths matching the core's byte limits, and no newer server needed. Verify that `cd sqlstore && go test -run 'TestMySQLComparesIdentifiersByBytes|TestStoreOnStdSQLMySQL/identity' -count=1 .` now passes.
- [x] 3.2 Make the same change in `sqlstore/ddl/email/mysql.sql`, for `notification_id` 64, `recipient` 255, `status` 16, `batch_id` 64 and `owner` 255, and update its header comment. Verify with `cd sqlstore && go test -run 'Email' -count=1 .` on every dialect.
- [x] 3.3 Regenerate the golden schemas with `cd sqlstore && go test -run 'TestSchema$|TestEmailSchema' -update -count=1 .`. Verify with `git diff sqlstore/testdata/schema` that only the four MySQL goldens changed, and only in their identifier column lines and comments. Then confirm `go test -run 'TestSchema|TestEmailSchema' -count=1 .` passes without `-update`.
- [x] 3.4 Add `verifyDialect` and the `mysqlIdentifierCollation = "binary"` constant to `sqlstore/store.go`, as in `design.md` D2.
  - Start with a `verifyDialect` that only delegates. Write `TestVerifyDialect` in `sqlstore/verify_internal_test.go` against it, and watch the MySQL row fail on its value, not on compilation. It has three rows: MySQL gives `binary`, PostgreSQL gives `C`, SQLite gives `BINARY`; each also checks that `Name()` is delegated.
  - Then pass `verifyDialect{s.dialect}` at both verification call sites, `sqlstore/store.go:190` and `sqlstore/email.go:126`.

  Verify that 2.1 now passes on both rows, and that `TestVerifySchemaOn*` and `TestVerifyEmailSchemaOn*` pass on all three dialects.

## 4. Upgrade an existing MySQL schema

- [x] 4.1 Add the section "Comparing identifiers byte for byte on an existing MySQL host" to `docs/schema.md`, after "Adding the email claim index to an existing host". It holds:
  - a pre-check `SELECT` that finds identifiers longer than the new byte lengths;
  - an "Upgrading" `sql` block of `ALTER TABLE ... MODIFY ... VARBINARY(n)` statements for notifications, watermarks and email deliveries, using the `app_` prefix;
  - a note that the rebuild blocks writes;
  - a "Rolling the upgrade back" `sql` block with the collision and invalid-UTF-8 caveat from `design.md` Migration Plan.

  Verify through 4.2.
- [x] 4.2 Add `TestTheDocumentedMySQLUpgradeComparesIdentifiersByBytes` in `sqlstore/email_verify_test.go`, beside the existing upgrade test.
  - Migrate the notification and email schemas, insert `alice`'s notification, and apply the documented rollback block, read through a new `documentedBlock(t, heading, prefix)` helper.
  - Require both verifications to fail, then apply the documented upgrade block.
  - Require both verifications to pass, `alice` + U+200B to get `ErrNotFound`, and `alice` still to read her notification.
  - Also run the documented pre-check and require it to return no rows.

  Verify with `cd sqlstore && go test -run 'TestTheDocumentedMySQLUpgradeComparesIdentifiersByBytes' -count=1 .`. Watch it fail first because the section is missing, then pass.

## 5. Document

- [x] 5.1 Update `docs/schema.md` so it matches the change:
  - the dialect table's MySQL identifier cell becomes `VARBINARY`;
  - the "Identifiers compare…" bullet says byte for byte on every dialect, with no padding, ignorables or normalisation, and says why MySQL uses binary strings rather than a collation;
  - the minimum MySQL version stays 8.0.

  Verify that `go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .` in the root passes, and that `grep -rn '0900_as_cs' docs sqlstore/ddl` prints only the rollback block and the DDL header's explanation.
- [x] 5.2 State the byte-identity guarantee in two places: `ntfytest/doc.go`, beside the isolation paragraph, and the `ntfy.Store` godoc in `store.go`. Verify with `go doc github.com/kartaladev/ntfy.Store` and `go doc github.com/kartaladev/ntfy/ntfytest`.

## 6. Verify and hand off

- [ ] 6.1 Run `make all` and `make store-matrix`. Both must pass, the matrix on all seven driver-and-dialect combinations. Run `make sqlkit-copy-check` and confirm `pkg/sqlkit` is untouched.
- [ ] 6.2 Record the sqlkit follow-up in `plans.md`'s execution record, and give the maintainer a ready-to-file issue text for sqlkit's own repository. The issue asks for three changes:
  - sqlkit's MySQL dialect expects binary identifier columns;
  - `columnIssues` words the issue as byte comparison;
  - the `sqlkittest` MySQL fixtures move to `VARBINARY`.

  Filing it is the maintainer's call. Verify that the text is in the plan's execution record.
