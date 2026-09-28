## 1. Prove the defect (red)

- [ ] 1.1 Add `TestCursorIsBoundToExactFilters` to `cursor_test.go` in the `table-test` form. Each row names a producer query and a consumer query. It encodes a cursor under the producer with `ntfy.EncodeCursor`, sets it on the consumer, and passes `DecodeCursor(consumer)`'s result to the row's `assert` closure. The rows are:
  - refused: kinds `["a","b"]` → `["a,b"]`;
  - refused: kinds `["a,b","c"]` → `["a","b,c"]`;
  - refused: no kind filter → `[""]`;
  - refused: `[""]` → no kind filter;
  - accepted, as a guard: `Kinds: nil` → `Kinds: []string{}`;
  - accepted, as a guard: kinds `["b","a,c"]` → `["a,c","b"]`.

  Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestCursorIsBoundToExactFilters' -count=1 .`. Confirm the four refused rows fail with `Expected error with "ntfy: not valid" in chain but got nil.`, and that both guard rows pass. A compile error or any other failure does not count.

## 2. Bind cursors to the exact filters (green)

- [ ] 2.1 In `store.go`, replace the joined-string fingerprint with the length-prefixed encoding from `design.md` D1:
  - add `appendField(b []byte, value string) []byte` and `appendFields(b []byte, values []string) []byte`;
  - import `encoding/binary`, and drop `strings`.

  Verify that `GOTOOLCHAIN=go1.26.8 go test -run 'TestCursor|TestListQuery' -count=1 .` passes: all six rows of 1.1, and the existing `TestCursor` rows (order-insensitive filters accepted; another recipient, other states and another subject refused).
- [ ] 2.2 Update the godoc of `ListQuery.Cursor`, `DecodeCursor` and `fingerprint`. Each says a cursor is bound to the same recipient, states, kinds and subject, in any order, whatever bytes they hold. Verify with `GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy ListQuery` and `go doc ... DecodeCursor`.

## 3. Document the rule and its compatibility cost

- [ ] 3.1 In `docs_test.go`, add the needles `"cursor is bound to"` and `"first page"` to the "the HTTP contract and its mounting" row of `TestTheDocumentMatchesTheImplementation`. Watch `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .` fail, saying the "HTTP and mounting" section does not mention them.
- [ ] 3.2 In `docs/notifications.md`, under "HTTP and mounting", add a bullet. It says:
  - a cursor is bound to the listing that produced it: the same caller, `state`, `kind` and `subject`, in any order;
  - under any other filters it is a `400 validation_failed`;
  - a cursor issued before this change is refused once, so the client restarts from the first page.

  Verify with the same command as 3.1, which now passes.

## 4. Verify and hand off

- [ ] 4.1 Run `make all`, and `make store-matrix`, since every store's `List` decodes cursors. Both must pass. Verify with their exit status.
- [ ] 4.2 Run `/code-review` on the branch diff, and fix what it finds, each fix led by a failing test. Verify that `make all` passes again afterwards.
