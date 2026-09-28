# Patches ntfy carries on its sqlkit copy

`pkg/sqlkit` is a copy of sqlkit (see [`README.md`](README.md) and
[`SOURCE`](SOURCE)). Every change ntfy makes to it is listed here and kept as a
patch in [`patches/`](patches/), against the source commit after the one path
rewrite. `make sqlkit-copy-check` applies the patches to a fresh checkout of the
source and fails on any other difference, so nothing changes here without being
recorded.

Each patch is meant to be sent to sqlkit's own repository. Once a sqlkit release
carries it and ntfy's copy is refreshed from that release, the patch and its
entry here are deleted.

## 0001: MySQL identifier columns compare byte for byte

**Patch:** [`patches/0001-compare-mysql-identifiers-by-bytes.patch`](patches/0001-compare-mysql-identifiers-by-bytes.patch)
**Made in:** ntfy's OpenSpec change `compare-mysql-identifiers-by-bytes`, on 2026-09-28.
**Status:** carried by ntfy; not yet sent upstream.

### The defect

sqlkit promises that identifier columns compare the same way on every dialect,
and pins how each dialect gets there. For MySQL it pinned the collation
`utf8mb4_0900_as_cs`, and verified identifier columns by that collation. Two
things are wrong with that.

1. **`utf8mb4_0900_as_cs` does not compare byte for byte.** It compares Unicode
   collation weights, so it ignores code points such as U+200B (zero-width
   space) and U+0000, and treats the NFC and NFD spellings of a character as
   equal. On MySQL, a row stored for `alice` is therefore found by `alice` +
   U+200B, by `alice` + NUL, and so on, while PostgreSQL (`C`) and SQLite
   (`BINARY`) keep them apart. In ntfy this let one recipient read another's
   notifications (proved by ntfy's `TestMySQLComparesIdentifiersByBytes`).
2. **Verification judged identifier columns by collation, which cannot tell the
   columns that do compare byte for byte from those that do not.** A binary
   string reports no collation, and verification accepted any column reporting
   none. That included `BINARY(n)`, which pads a value with NUL so a stored
   identifier stops matching the one it was stored as, and `BLOB`. The issue it
   reported for a wrong collation always ended "or identifiers will compare
   case-insensitively", which is false for `utf8mb4_0900_as_cs`,
   `utf8mb4_0900_bin` and every other case-sensitive collation.

### Why a type, and why `VARBINARY`

| MySQL column | Byte-exact equality | Trailing spaces | Needs |
| --- | --- | --- | --- |
| `VARCHAR COLLATE utf8mb4_0900_as_cs` (before) | no: ignorables weigh nothing, NFC = NFD | count | 8.0 |
| `VARCHAR COLLATE utf8mb4_bin` | yes | **ignored** (PAD SPACE) | 5.5 |
| `VARCHAR COLLATE utf8mb4_0900_bin` | yes | count | **8.0.17** |
| `BINARY(n)` | **no**: pads with NUL | — | any |
| `VARBINARY(n)` (after) | yes | count | any |

`VARBINARY` is the only choice that compares byte for byte on every MySQL
sqlkit supports, so a library built on sqlkit need not ask its users to upgrade
their database. Its lengths are bytes, not characters. Measured on MySQL 8.4.6:
a `VARBINARY` column reports `COLLATION_NAME` NULL and `DATA_TYPE` `varbinary`,
and `CAST('alice' AS BINARY) = 'alice '` is 0.

### What the patch changes

- **`Dialect` gains `IdentifierType() string`**, the column type identifier
  columns must have, as introspection names it, or empty when a column carrying
  `IdentifierCollation` will do. Every dialect pins exactly one of the two.
  - MySQL: `IdentifierType()` is `varbinary`; `IdentifierCollation()` is now
    empty. The godoc on both says why.
  - PostgreSQL (`C`) and SQLite (`BINARY`) are unchanged; their
    `IdentifierType()` is empty.
  - **Breaking** for any implementation of `Dialect` outside sqlkit: it must add
    the method. `IdentifierCollation()`'s contract changes from "never empty" to
    "empty exactly when `IdentifierType()` is not".
- **`SchemaQuery` reads each column's type as a fourth column**: `data_type` on
  PostgreSQL, `DATA_TYPE` on MySQL, `pragma_table_info`'s `type` on SQLite.
- **`VerifySchema` judges an identifier column by the dialect's pin**: by type
  where `IdentifierType()` is set, otherwise by collation as before. A wrong
  one is reported as
  `type is "varchar" collated "utf8mb4_0900_as_cs" but must be "varbinary", or identifiers will not compare byte for byte`,
  or, for a collation dialect,
  `collation is "en_US" but must be "C", or identifiers will not compare byte for byte`.
  No other column's type is checked.
- **`sqlkittest`**: the MySQL fixture's identifier columns are `VARBINARY`, and
  the executor suite's "identifiers compare case-sensitively" case becomes
  "identifiers compare byte for byte". It stores owners differing only by case,
  a trailing space, U+200B, and NFC versus NFD, and requires each to match only
  itself, on every dialect.
- **`docs/README.md`**: the identifier bullet, the `VerifySchema` summary and
  the stated verification limit say the above.

### Proof

Each claim was seen failing before the change and passing after it.

- `TestDialectFragments/mysql` failed on `expected: "" actual: "utf8mb4_0900_as_cs"`
  and `expected: "varbinary" actual: ""`.
- `TestVerifySchema` failed on:
  - `a MySQL identifier column that is text under a collation is named, whatever the collation`
    and `... of fixed-width BINARY ...` (`An error is expected but got nil`);
  - `a case-insensitive collation on an identifier column is named`
    (the message did not say "byte for byte").
- `TestExecutorOnMySQL/schema/...` (stdsql, against MySQL 8.4.6) failed once
  verification changed and before the fixture did, naming each fixture
  identifier column `type is "varchar" collated "utf8mb4_0900_as_cs"`.
- `TestExecutorOnMySQL/values/identifiers_compare_byte_for_byte`, run against
  the old fixture, failed on `"alice"`, `"alice\u200b"` and `"jos\u00e9"`
  matching more than themselves. It passes on the new fixture, and on
  PostgreSQL and SQLite with theirs.
- After the change: `go test ./...` passes in `sqlkit`, `sqlkit/stdsql`,
  `sqlkit/pgx` and `sqlkit/gorm`, with PostgreSQL 17.6, MySQL 8.4.6 and SQLite.

### Applying it to sqlkit's own repository

The patch's paths are relative to the sqlkit directory, and it was made after
the copy's one path rewrite (`github.com/kartaladev/hmntsk/sqlkit` →
`github.com/kartaladev/sqlkit`). Some hunks' context lines include import
paths. In a repository still using the old path, reverse the rewrite on the way
in:

```sh
sed 's#github.com/kartaladev/sqlkit#github.com/kartaladev/hmntsk/sqlkit#g' \
    0001-compare-mysql-identifiers-by-bytes.patch |
    git apply --directory=sqlkit
```

This was checked on 2026-09-28: it applies cleanly to
`github.com/kartaladev/hmntsk` at `32e7763297a4ac0107c0702da5a5b497c9029083`,
changing exactly the eight files listed in the patch.

In `github.com/kartaladev/sqlkit`, where the path is already the new one, apply
it at the repository root with `git apply` as it is.

A store built on sqlkit that already has MySQL identifier columns declared
`VARCHAR ... COLLATE utf8mb4_0900_as_cs` will fail `VerifySchema` until its
columns are `VARBINARY`. ntfy documents its own upgrade in its `docs/schema.md`,
"Comparing identifiers byte for byte on an existing MySQL host", including a
pre-check for identifiers longer than the new byte lengths.
