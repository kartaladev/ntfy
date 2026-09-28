> Every red step below was run once on `main` at `20967b3` while this change was proposed. The plan quotes the failing lines it produced. A fix that turns a case green was also tried against every store and then reverted. `plans.md` carries the exact code.

## 1. Refuse a reused notification identifier

- [ ] 1.1 Create `ntfytest/edges.go` with `runEdges(t, factory)`: a `table-test` table of `name` and `assert func(t *testing.T, e *env)`. Register it in `Run` as `t.Run("edges", ...)`. Add four cases:
  - "a publish reusing a stored identifier is refused and keeps what is stored";
  - "a publish repeating an identifier within itself writes nothing";
  - "a close whose successor reuses a stored identifier changes nothing";
  - the guard "an insertion that writes nothing is not checked for its identifier".

  Verify red with `go test -run 'TestMemoryStoreConformance/edges' -count=1 .`: the first three fail with `An error is expected but got nil.`, and the guard passes. Then run `cd sqlstore && go test -run 'TestStoreOnStdSQL(MySQL|Postgres|SQLite)/edges' -count=1 .`: the same three fail on MySQL only.
- [ ] 1.2 In `memory.go`:
  - `insert` returns `(InsertResult, error)`. It refuses an identifier already in `s.notifications`, removes what the call wrote, and returns `ntfy: notification identifier %q is already stored`.
  - `put` loses its replace branch.
  - `Insert` returns at once for an empty call, and propagates the error.

  Rewrite `memory_index_test.go`'s row "after an identifier is reused for another recipient" as "after an insert reusing an identifier is refused". Watch it fail on the old code with `An error is expected but got nil.` Verify that `go test -run 'TestMemoryStoreConformance/edges|TestMemoryStoreRecipientIndexMirrorsTheStore' -count=1 .` passes the two insert cases and the index row. Then move the identifier check above the duplicate check, watch the guard fail, and restore it.
- [ ] 1.3 In `memory.go`, `Close` takes a `snapshot` of the close records it touches and the notifications it closes, before changing anything. It restores them when `insert` refuses a successor. Verify that the close case passes, and that `go test -race -count=1 .` passes.
- [ ] 1.4 In `sqlstore/insert.go`, `writeNotifications` reads back `id, source_id, recipient`. It fails with `sqlstore: notification identifier %q is already stored` when the row under an identifier is another notification's. Verify with `cd sqlstore && go test -run 'TestStoreOnStdSQL(MySQL|Postgres|SQLite)$' -count=1 .`: MySQL's three cases turn green, and nothing else changes.

## 2. Begin a subject's close record on every non-empty insert

- [ ] 2.1 Add the case "a publish that creates nothing still begins its subject's close record" to `runEdges`. Watch it fail on memory with `expected: 1` / `actual  : 0`, and pass on SQLite. Then move `ensureWatermark` out of `insert` into `Insert`, after success, at `normalizeTime(insertions[0].Notification.CreatedAt)`. Verify `go test -run 'TestMemoryStoreConformance' -count=1 .` passes whole.

## 3. Compare retention cutoffs at microseconds

- [ ] 3.1 Add the cases "a notification inactive in the age cutoff's microsecond is kept" and "a close record changed in the watermark cutoff's microsecond is kept" to `runEdges`. Watch them fail on memory, with `Should be zero, but was 1` and `Should be zero, but was 2` respectively. Then apply `normalizeTime` to both cutoffs in `MemoryStore.Prune`. Verify `go test -run 'TestMemoryStoreConformance' -count=1 .` passes, and so does `cd sqlstore && go test -run 'TestStoreOnStdSQLSQLite/edges' -count=1 .`.

## 4. Pin the `EmailStore` edges

- [ ] 4.1 Create `ntfytest/email_edges.go` with `runEmailEdges(t, factory)`, registered in `RunEmail` as `t.Run("edges", ...)`. Add two cases:
  - "a record under the empty owner does not change a released delivery";
  - the guard "a claim under the empty owner can be recorded by it".

  Watch the first fail on memory with `Should be zero, but was 1`, while the guard passes. Then make `RecordEmails` require `delivery.leaseUntil != nil`. Verify that `go test -run 'TestMemoryStoreEmailConformance' -count=1 .` passes whole, the guard included.
- [ ] 4.2 Add three cases to `runEmailEdges`:
  - "a claim with a limit of zero claims nothing";
  - "a claim with a negative limit claims nothing";
  - "a purge with a limit of zero deletes nothing".

  Watch each fail on memory: the claims with `Should be empty, but was [...]`, the purge with `Should be zero, but was 1`. Then make `ClaimEmails` and `PurgeEmailRecords` return at once for a limit ≤ 0. Verify that `go test -run 'TestMemoryStoreEmailConformance' -count=1 .` passes, and so does `cd sqlstore && go test -run 'TestStoreOnStdSQL(MySQL|Postgres|SQLite)/email/edges' -count=1 .`.

## 5. Document the edges

- [ ] 5.1 Update the godoc:
  - `store.go`: `Store.Insert`, `Store.Close` and `Store.Prune`;
  - `email.go`: `EmailStore.ClaimEmails`, `EmailStore.RecordEmails`, `EmailStore.PurgeEmailRecords` and `EmailClaim.Limit`;
  - `id.go`: `IDGenerator.NewID`;
  - `ntfytest/doc.go`: names the `edges` groups.

  Use the wording in `plans.md` Task 5. Verify with `go doc github.com/kartaladev/ntfy.Store`, `go doc github.com/kartaladev/ntfy.EmailStore`, `go doc github.com/kartaladev/ntfy.IDGenerator` and `go doc github.com/kartaladev/ntfy/ntfytest`.

## 6. Verify

- [ ] 6.1 Run `make all`, then `make store-matrix` on all seven combinations. Both must pass. Run `go test -race -count=1 ./...` in the root and in `ntfytest`. Record the results in `plans.md`'s execution record.
