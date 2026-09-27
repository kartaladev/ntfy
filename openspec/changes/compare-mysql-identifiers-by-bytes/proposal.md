## Why

On MySQL, two different recipient strings can be the same recipient, so one user's notifications are returned to another. The schema pins identifier columns to `utf8mb4_0900_as_cs`, which compares Unicode collation weights rather than bytes. Ignorable code points such as U+200B and U+0000 weigh nothing, and the NFC and NFD spellings of `josé` weigh the same. A notification published for `alice` is therefore returned by `Get` and `List` to `alice​` and to `alice\x00`.

PostgreSQL (`COLLATE "C"`), SQLite (`BINARY`) and the memory store keep these strings apart. `docs/schema.md` also promises that identifiers "compare case-sensitively and sort in byte order" on every dialect. MySQL breaks both that promise and the requirement that every store behaves identically.

**Proved by:** `TestAuditMySQLRecipientsAreComparedByBytes`, from the 2026-09-28 adversarial audit, run against MySQL 8.4.6 in a container. All three cases fail on the current code:

```
--- FAIL: TestAuditMySQLRecipientsAreComparedByBytes/a_zero-width_space_is_a_different_recipient
        Messages: Get as the intruder returned "alice"'s notification
--- FAIL: TestAuditMySQLRecipientsAreComparedByBytes/a_NUL_byte_is_a_different_recipient
        Messages: Get as the intruder returned "alice"'s notification
--- FAIL: TestAuditMySQLRecipientsAreComparedByBytes/NFD_is_a_different_recipient_from_NFC
        Messages: Get as the intruder returned "josé"'s notification
```

Task 1 brings this test into the repository as the red step.

The same collation is on `source_id`, `subject` and `kind`, and it merges them too. Tasks 1.1 and 1.2 proved this on 2026-09-28:
- a publish of `event-1` + U+200B or `event-1` + NUL is counted as a duplicate of `event-1`;
- closing `task-1` closes `task-1` + U+200B and suppresses later publishes on it;
- a kind filter on `offer` returns `offer` + U+200B;
- the NFC and NFD spellings of each identifier merge the same way.

Case and trailing-space variants are already distinct on MySQL.

The ntfytest conformance suite could not catch any of this. It passes a store that matches recipients case-insensitively (audit test `TestAuditStoreSuiteAcceptsCaseFoldingStore`), so nothing holds a host's own store to byte-exact identity either.

## What Changes

- **MySQL identifier columns become binary strings.** In the notification DDL and the email DDL, every identifier column changes from `VARCHAR(n) COLLATE utf8mb4_0900_as_cs` to `VARBINARY(n)`. A binary string compares and sorts by its bytes and pads nothing, so every byte counts.
  - The column lengths become byte lengths, which match the core's own byte limits: `MaxIdentifierBytes` = 255, `MaxKindBytes` = 100.
- **The minimum supported MySQL does not change.** It stays at 8.0. No collation is needed, so nothing asks for a newer server. A library cannot require its hosts to upgrade their database for a fix, and this one does not.
- **`Store.VerifySchema` and `Store.VerifyEmailSchema` require binary identifier columns on MySQL.** Any identifier column that reports a character-set collation is named at startup, whichever collation it is. An existing MySQL schema therefore fails verification until it is upgraded. **BREAKING** for existing MySQL hosts; no tag has been cut, so this is recorded rather than versioned.
  - sqlkit hard-codes the expected collation, and its copy in `pkg/sqlkit` may not be edited here. So sqlstore passes verification a dialect that overrides the expectation on MySQL only.
  - A follow-up fixes the value in sqlkit's own repository, and the override is then deleted.
- **A documented, tested upgrade for existing MySQL schemas.** `ALTER TABLE ... MODIFY` statements in `docs/schema.md`, proved by a test that starts from the old columns, applies them, and requires the schema to verify.
  - It comes with a query that finds any row the conversion would reject. Such a row holds an identifier longer than the core's byte limits, which could only have been written around the library's validation.
- **The conformance suite holds every store to byte-exact identifiers.** New `ntfytest` cases for recipient, source, subject and kind. Each pair differs only by case, trailing space, a zero-width space, or NFC versus NFD. The pairs are chosen to be valid UTF-8 without NUL, so PostgreSQL can store them. NUL is covered by a MySQL-specific test in sqlstore.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-inbox`: adds a requirement that every store compares recipient, source, subject and kind identifiers byte for byte, asserted by the shared conformance suite.

## Impact

- **Code:**
  - `sqlstore/ddl/mysql.sql`, `sqlstore/ddl/email/mysql.sql` and their golden files under `sqlstore/testdata/schema/`.
  - `sqlstore/store.go` and `sqlstore/email.go`: the verification dialect.
  - New tests in `sqlstore/`.
  - A new `ntfytest/identity.go`, wired into `ntfytest.Run`.
- **Docs:**
  - `docs/schema.md`: the dialect table, the identifier bullet, and a new upgrade section. The minimum MySQL version is unchanged.
  - The header comments of both MySQL DDL documents.
- **Hosts:**
  - A MySQL host applies the documented `ALTER TABLE` statements before deploying. MySQL rebuilds each table, which blocks writes to it for the duration.
  - PostgreSQL and SQLite hosts do nothing.
  - A host with its own `Store` runs `ntfytest.Run`; a store that folds case, pads or normalises identifiers fails.
- **Data:** existing rows are kept, byte for byte, because converting UTF-8 text to a binary string keeps its bytes. Strings that were distinct stay distinct, and no unique key can collide, because byte comparison only splits what the old collation merged. Anything the old collation already merged, such as a publish suppressed as a duplicate, is not recovered.
- **Out of scope:** validating identifiers for NUL and invalid UTF-8 (audit finding 10) is its own change. A binary column now stores what PostgreSQL would refuse, so that change matters more after this one.
- **Dependencies:**
  - Nothing new.
  - Follow-up outside this repository: sqlkit's MySQL dialect should expect binary identifier columns. Then refresh `pkg/sqlkit`, and delete the override.
