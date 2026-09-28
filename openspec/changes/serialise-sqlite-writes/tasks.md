## 1. Prove the defect (red)

- [ ] 1.1 Add `DeferredSQLiteDSN(t *testing.T) string` to `sqlstore/internal/harness/harness.go`. It returns a DSN for a fresh file in `t.TempDir()` with `foreign_keys(1)`, `journal_mode(WAL)` and `busy_timeout(10000)`, and deliberately no `_txlock`. Add `sqlstore/sqlite_writes_test.go` with `TestSQLiteWritesWaitForEachOther` in the `table-test` form over that DSN, with three cases:
  - 16 concurrent `MarkRead`s;
  - 8 `MarkRead`s racing 8 `Insert`s on another subject;
  - 16 concurrent `ClaimEmails` by distinct owners over 64 due notifications, `Limit` 4.

  Each case asserts that no call failed and asserts the outcome (all READ; no notification claimed twice). Verify with `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestSQLiteWritesWaitForEachOther' -count=1 .`. All three rows must fail with `… of 16 … failed; first: sqlkit: database is locked (5) (SQLITE_BUSY)` or `(517)`, not with a compile error. **STOP** if any row passes before the fix: record the numbers in `design.md` under "Does not reproduce" and do not go on to 2.1 for that path.
- [ ] 1.2 Add `TestStoreOnStdSQLSQLiteDeferred` to `sqlstore/harness_test.go` and `TestStoreOnGormSQLiteDeferred` to `sqlstore/internal/gormtest/store_test.go`. Each runs `ntfytest.Run`, `RunEmail` and `RunEmailDispatch` over `harness.DeferredSQLiteDSN(t)`. Verify with `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestStoreOnStdSQLSQLiteDeferred' -count=1 .` and `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestStoreOnGormSQLiteDeferred' -count=1 ./internal/gormtest`. Both must fail in their `email` and `email-dispatch` groups, including `email/concurrency/two_owners_claiming_concurrently_never_both_receive_a_notification`, with `sqlkit: database is locked (5) (SQLITE_BUSY)`.
- [ ] 1.3 Add `func (s *Store) writeLock() sqlkit.Statement` to `sqlstore/sql.go` as a stub returning the zero Statement, and not yet called. Add `sqlstore/lock_internal_test.go` (package `sqlstore`) with `TestWriteLock`, a table over SQLite (no prefix, and prefix `app_`), PostgreSQL and MySQL. Verify with `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestWriteLock' -count=1 .`. Both SQLite rows must fail on the statement's text; the PostgreSQL and MySQL rows pass.

## 2. Take SQLite's write lock first (green)

- [ ] 2.1 Make `writeLock` return `UPDATE <notifications table> SET "id" = "id" WHERE 0` on SQLite and the zero Statement elsewhere. Make `Store.do` execute it as the first statement of every transaction it opens, then run `fn`. Update both godocs to say why. Verify that 1.1, 1.2 and 1.3 now pass, and that `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -race -count=1 ./...` passes.

## 3. Wire the matrix and document

- [ ] 3.1 Add `sqlstore:TestStoreOnStdSQLSQLiteDeferred` and `sqlstore/internal/gormtest:TestStoreOnGormSQLiteDeferred` to `STORE_MATRIX` in the `Makefile`. Reword the target's comment from "all seven combinations" to "all seven driver-and-dialect combinations, and SQLite with the driver's default transaction mode". Verify with `sed -n '/^STORE_MATRIX/,/^$/p' Makefile | grep -o 'TestStoreOn[A-Za-z]*' | wc -l`, which prints `7` before the edit and `9` after.
- [ ] 3.2 In `docs/schema.md`, add the subsection "SQLite connections" under "Transactions" (`design.md` D4): the transaction mode does not matter; a busy timeout is required; WAL is recommended; an example DSN. Change the intro's "all seven driver-and-dialect combinations" to name the two transaction-mode runs too. Add one sentence to `sqlstore/doc.go`. Verify by reading the rendered `go doc github.com/kartaladev/ntfy/sqlstore`, and confirm with `grep -n '_txlock' docs/schema.md` that the doc says `_txlock` is not needed.

## 4. Verify

- [ ] 4.1 Run `make all`, `make store-matrix` (all nine entries) and `make sqlkit-copy-check`. All must pass, and `git diff --stat main -- pkg/sqlkit` must print nothing. Record the results in `plans.md`'s execution record.
