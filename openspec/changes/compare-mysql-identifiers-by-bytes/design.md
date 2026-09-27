## Context

See `proposal.md` — Why. The change spans three modules: `ntfy/sqlstore` for the DDL, verification and the upgrade; `ntfy/ntfytest` for the conformance cases; and the vendored `pkg/sqlkit`, which cannot be touched. Two constraints shape the design:

- **sqlkit cannot be edited here.** The expected collation lives in its code.
- **A library cannot make its hosts upgrade their database for a fix.** On 2026-09-28 the maintainer rejected raising the MySQL floor to 8.0.17, which `utf8mb4_0900_bin` would need.

Where identifier comparison is decided today:

| Where | What | Editable here? |
| --- | --- | --- |
| `sqlstore/ddl/mysql.sql:25-31, 52-53` | notification and watermark identifier columns: `VARCHAR(n) COLLATE utf8mb4_0900_as_cs` | yes |
| `sqlstore/ddl/email/mysql.sql` | `notification_id`, `recipient`, `status`, `batch_id`, `owner`: the same | yes |
| `sqlstore/testdata/schema/{mysql,mysql_app_,email_mysql,email_mysql_app_}.sql` | golden copies of the rendered DDL (`TestSchema`, `go test -update`) | yes, regenerated |
| `pkg/sqlkit/dialect.go:130` | `mysql.IdentifierCollation()` returns `"utf8mb4_0900_as_cs"` | **no**: `make sqlkit-copy-check` fails on any edit |
| `pkg/sqlkit/verify.go:193-217` | `columnIssues` flags an identifier column whose reported collation is non-empty and differs from `dialect.IdentifierCollation()`. An empty (NULL) collation is accepted as "the dialect's default" | **no** |
| `sqlstore/store.go:190`, `sqlstore/email.go:126` | `sqlkit.VerifySchema(..., s.dialect, ...)` | yes |
| `notification.go:45-47` | `MaxKindBytes = 100`, `MaxIdentifierBytes = 255`: the core already bounds identifiers **in bytes** | not changed |

Observed on MySQL 8.4.6 on 2026-09-28, with a throwaway probe test:
- `information_schema.COLUMNS` reports `COLLATION_NAME` NULL for a `VARBINARY` column, and for `VARCHAR ... CHARACTER SET binary`, which MySQL turns into `VARBINARY`.
- A `VARCHAR ... COLLATE utf8mb4_0900_as_cs` column reports `utf8mb4_0900_as_cs`.
- `CAST('alice' AS BINARY) = 'alice '` is `0`: binary comparison does not pad.

## Goals / Non-Goals

**Goals**

- Byte-exact identifier comparison on MySQL, matching PostgreSQL, SQLite and memory, on **every MySQL version the library already supports**.
- An existing MySQL host fails verification at startup, and has one documented, tested way to pass it.
- Any store, including a host's own, is held to byte-exact identity by `ntfytest.Run`.

**Non-Goals**

- Raising, or lowering, the documented minimum MySQL (8.0).
  - The new DDL uses no 8.0-only collation, so it may well work on 5.7.
  - It is not tested there, and widening the support claim is its own decision.
- Validating identifiers for NUL or invalid UTF-8 (audit finding 10). That is a change to `Draft.Validate`, separate from this one.
- Recovering anything the old collation already merged.
- Changing sqlkit in its own repository. It is recorded as a follow-up.

## Decisions

### D1. Binary strings (`VARBINARY`), sized to the core's byte limits

| Option | Byte-exact equality | Trailing spaces | Server needed | Cost |
| --- | --- | --- | --- | --- |
| `VARCHAR COLLATE utf8mb4_0900_as_cs` (today) | **no**: ignorables weigh nothing, NFC equals NFD | significant | 8.0 | — |
| `VARCHAR COLLATE utf8mb4_bin` | yes | **ignored** (PAD SPACE) | 5.5+ | leaves `alice` = `alice ` on MySQL alone |
| `VARCHAR COLLATE utf8mb4_0900_bin` | yes | significant | **8.0.17+** | raises the floor; rejected by the maintainer |
| a configurable collation | depends on choice | depends | depends | the default still needs 8.0.17, and a host on an older server must know to opt out |
| **`VARBINARY(n)`** | **yes** | **significant** (observed) | every version | the column type changes; lengths are bytes; the database no longer checks UTF-8 |

`VARBINARY` is the only option that is byte-exact on every server the library supports, with no configuration. Its costs are smaller than they look:

- **Lengths in bytes are the contract already.** Validation refuses an identifier over `MaxIdentifierBytes` (255) or a kind over `MaxKindBytes` (100) bytes. So `VARBINARY(255)` and `VARBINARY(100)` hold exactly what the library accepts. Today's `VARCHAR(255)` counts characters, and can hold up to 1020 bytes that validation never lets through. `id`, `notification_id` and `batch_id` stay at 64 and the state columns at 16, as bytes.
- **Reading is unchanged.** The MySQL driver returns `[]byte` for text and binary columns alike, and `sqlkit.DecodeString` already decodes both (`pkg/sqlkit/codec.go:125`).
- **Writing is unchanged.** The store binds identifiers as Go strings, and MySQL compares a binary column with a string argument as bytes.
- **UTF-8 is no longer checked by the database.** It was never the library's check to rely on: memory and SQLite do not check it either. Finding 10 puts it in validation, where it holds on every store.
- **Keys fit.** The widest index, `(source_id, recipient)`, becomes 510 bytes instead of 2040, well under InnoDB's 3072.

**Default:** MySQL identifier columns are `VARBINARY`. **Override:** none. Byte identity is a guarantee the library makes across stores (rule 4: stated, not silently relaxed). A host that wants case-insensitive recipients normalises the recipient before it reaches ntfy (rule 5). Needing no newer server is what makes a no-override default acceptable under rule 1.

### D2. Verification expects binary columns, through an override in sqlstore

sqlkit's check accepts a column that reports no collation, and flags one whose collation differs from `IdentifierCollation()`. So verification needs an expected value that no character-set collation can equal.

sqlstore passes `sqlkit.VerifySchema` a wrapper whose MySQL answer is `binary`, the name MySQL gives the binary character set's collation:

```go
// verifyDialect is the dialect schema verification checks against. On MySQL
// it expects binary identifier columns, which report no collation, so any
// character-set collation is named; the vendored sqlkit still expects
// utf8mb4_0900_as_cs. It goes once sqlkit's MySQL dialect expects binary
// identifier columns itself.
type verifyDialect struct{ sqlkit.Dialect }

func (d verifyDialect) IdentifierCollation() string {
	if d.Name() == sqlkit.MySQL.Name() {
		return mysqlIdentifierCollation // "binary"
	}

	return d.Dialect.IdentifierCollation()
}
```

- A `VARBINARY` column reports NULL. sqlkit reads that as `""` and accepts it.
- A `VARCHAR` column under any collation (`utf8mb4_0900_as_cs`, `utf8mb4_bin`, even `utf8mb4_0900_bin`) is reported as `collation is "<c>" but must be "binary"`.

The wrapper is used only at the two `VerifySchema` call sites. The store's statements keep the executor's dialect.

*Alternatives considered:*

- **Fix sqlkit first.** That blocks a cross-user data leak on another repository. Rejected by the maintainer in favour of landing now plus a follow-up.
- **Stop verifying MySQL identifiers.** Rejected. Rule 6: a wrong schema should fail at startup, not in traffic.
- **Check `DATA_TYPE` = `varbinary`.** That is more direct, but sqlkit's query does not read `DATA_TYPE`, and duplicating its introspection in sqlstore would fork sqlkit's job. Deferred to the upstream fix.

*Known wart:* sqlkit's issue text ends "or identifiers will compare case-insensitively", which is inaccurate for these columns. It still names the right column and says `must be "binary"`, which is what an operator acts on. The rewording belongs upstream.

**Default:** verification requires binary identifier columns on MySQL; PostgreSQL and SQLite are unchanged. **Override:** none, as today. A host that skips `VerifySchema` skips it for everything.

### D3. The upgrade is a documented `ALTER TABLE ... MODIFY`, proved by a test

`docs/schema.md` gets a section, "Comparing identifiers byte for byte on an existing MySQL host".

- **Upgrading:** one `ALTER TABLE` per table (notifications and watermarks, plus deliveries for a host that emails). Each re-declares every identifier column as `VARBINARY(n)` with its existing nullability.
- **Before upgrading:** a `SELECT` that lists any row with an identifier longer than the new byte length. Only a write that went around the library's validation can have left one. Under MySQL's default strict mode, the `ALTER` refuses such a row and changes nothing. Without strict mode, it would truncate silently, so the doc says to run the check first.
- **Rolling the upgrade back:** the reverse statements back to `VARCHAR(n) COLLATE utf8mb4_0900_as_cs`, with the collision caveat.

A test proves both blocks, in the pattern of `TestTheDocumentedMySQLUpgradeAddsTheEmailClaimIndex`. The test reads the SQL out of the doc, so the doc cannot drift from what was tested. It runs these steps:

1. Migrate a fresh schema and insert alice's notification.
2. Apply the rollback block.
3. Require both verifications to fail, naming the columns.
4. Apply the upgrade block.
5. Require both verifications to pass, alice + U+200B to read nothing, and alice to read her notification unchanged.

`Store.Migrate` does not upgrade an existing schema. Every statement stays `CREATE TABLE IF NOT EXISTS`. Unchanged from today.

**Default:** a new schema is created byte-exact. **Override:** the host runs the upgrade through its own migration pipeline, when it chooses; verification tells it when it has not.

### D4. The conformance cases use valid UTF-8 without NUL

`ntfytest` gains `identity.go` with `runIdentity`, registered in `Run` as `t.Run("identity", ...)`.

| Variant of each identifier | Why |
| --- | --- |
| upper-cased first letter | the case-folding guard; the suite had none |
| trailing space | catches a PAD SPACE collation such as `utf8mb4_bin` |
| appended U+200B | an ignorable code point under `0900_as_cs` |
| NFD spelling of `josé` against NFC | canonical equivalence under `0900_as_cs` |

- NUL is excluded because PostgreSQL rejects it (finding 10). It is covered by the MySQL-only `TestMySQLComparesIdentifiersByBytes`.
- Each identifier is exercised through the operation where it matters:
  - recipient: `Get`, `List`, `CountActive` and `MarkRead` see nothing of the other recipient's notification;
  - source: the other string is not a duplicate;
  - subject: closing one leaves the other open and unsuppressed;
  - kind: a filter on one does not return the other.

*Alternative considered:* the full product of variants and identifiers through every operation, which is 16 × 4 × several operations. Rejected: one variant set per identifier through the operation that exercises it catches a folding store as surely, and keeps a failure pointing at one behaviour.

**Default:** every store is held to byte identity. **Override:** none, as for D1.

### D5. Email-table identifier columns become binary too

`notification_id` (64), `recipient` (255), `status` (16), `batch_id` (64) and `owner` (255) become `VARBINARY`, and `VerifyEmailSchema` requires it through the same wrapper.

- `owner` is compared by the lease check (`RecordEmails`, `sqlstore/email.go:498`). Owner strings that differ only by an ignorable code point would otherwise share a lease.
- The others move so that "identifier column" means one thing in every table.

**Default / Override:** as D1.

## Risks / Trade-offs

- **[A driver or executor returns something other than `[]byte` or `string` for `VARBINARY`]** → sqlkit decodes both. The MySQL conformance suite runs on database/sql and GORM (`internal/gormtest`), and fails on any decoding error. pgx does not reach MySQL.
- **[A statement compares an identifier column with a collation-bearing expression]**, such as `LOWER(recipient)` or a `COLLATE` clause. A binary column would then error or compare differently. → `grep -n 'COLLATE\|LOWER(\|UPPER(' sqlstore/*.go` finds none today, and the full MySQL suite runs green before the change is done.
- **[The upgrade rebuilds tables and blocks writes]** → Changing a column's type is a copying `ALTER` in MySQL. That is **unverified** for 8.4 here; it is the documented online-DDL behaviour. The upgrade section says so and points to a maintenance window or an online schema-change tool. The test proves correctness, not duration.
- **[The upgrade meets an over-long identifier]** → Validation has bounded identifiers in bytes since the limits change. A longer one can exist only if written around the library. The documented pre-check query finds it. Strict mode, MySQL's default, refuses the `ALTER` rather than truncate. **Unverified:** a server with strict mode off would truncate; the doc says to run the pre-check.
- **[A unique key collides during the upgrade]** → It cannot. Rows distinct under `0900_as_cs` stay distinct under byte comparison, which only splits values the old collation merged. The upgrade test runs against a table with rows.
- **[Existing keyset cursors move across the upgrade]** → Page order is `(created_at, id)`. Default UUIDv7 identifiers are lowercase hex and hyphens, which order the same under both. A custom `IDGenerator` producing mixed-case identifiers could see one page repeat or skip an item across the upgrade. The docs say so.
- **[Invalid UTF-8 is now storable on MySQL]** → It already was on memory and SQLite, and finding 10's change validates it on every store. The proposal names the dependency.
- **[The override outlives its reason]** → `TestSQLKitStillExpectsTheOldMySQLCollation` asserts that the vendored sqlkit still expects `utf8mb4_0900_as_cs`. A refreshed copy with the upstream fix fails it, and the failure says to delete the wrapper.
- **[A host's own store folds identifiers on purpose]** → It now fails `ntfytest.Run`. That is the point (D4).

## Migration Plan

1. **New hosts:** nothing. The DDL creates binary identifier columns.
2. **Existing MySQL hosts:** before deploying, run the documented pre-check. Then run the upgrade statements through the migration pipeline. Without them, `VerifySchema` fails at startup, naming every identifier column. No MySQL upgrade is needed.
3. **PostgreSQL and SQLite hosts:** nothing.
4. **Hosts with their own store:** run `ntfytest.Run`, and fix any identity case that fails.
5. **Rollback:** the documented reverse statements, then the previous release. This reintroduces the defect. It can also fail: rows written after the upgrade that are byte-distinct but equal under the old collation collide on a unique key. An example is `event-1` and `event-1` + U+200B for one recipient. It can likewise fail on bytes that are not valid UTF-8. In both cases MySQL refuses the `ALTER` and changes nothing. The doc says so. The collision is **unverified**: rollback is not a supported path, and the test runs the reverse statements only on a table without such rows.

## Open Questions

None that change the specs, the approach or the tasks. The sqlkit follow-up's timing is up to that repository.
