## Why

A listing cursor is documented as "bound to the recipient and filters that produced it" (`ListQuery.Cursor`), and `DecodeCursor` promises to refuse one that "belongs to another recipient or filter set". It does not keep that promise. `ListQuery.fingerprint` (`store.go:417-433`) joins the sorted kinds with `,` and the four fields with NUL, then hashes the result. A kind may contain a comma, and may be empty, so different filter sets produce the same joined string:

- kinds `["a", "b"]` and kinds `["a,b"]`;
- kinds `["a,b", "c"]` and kinds `["a", "b,c"]`;
- no kind filter (`nil`) and a filter naming only the empty kind (`[""]`). The first lists every kind, and the second lists nothing, because every notification has a non-empty kind.

A cursor taken under one of these filters is accepted under the other, and paging continues from a position taken under a different filter. Over HTTP the pair is `?kind=a&kind=b` and `?kind=a%2Cb`. No data leaks: the listing is still filtered by `q.Recipient`, and a cursor carries only a position. The cost is a page sequence that is not the one the documented rule guarantees.

Severity: **Low**. Found by the 2026-09-28 adversarial audit, and raised again by the code review of PR #11.

**Proved by:** `TestAuditCursorIsBoundToItsFilters`, from the audit. It was adapted to go through `ListQuery.Validate`, the path every service call takes, and re-run on current `main` (`20967b3`) on 2026-09-28 with `GOTOOLCHAIN=go1.26.8 go test -run 'TestAuditCursorIsBoundToItsFilters' -count=1 .`. Three cases fail for the stated reason:

```
--- FAIL: TestAuditCursorIsBoundToItsFilters/two_kinds_and_one_kind_containing_a_comma
        Error:      	Expected error with "ntfy: not valid" in chain but got nil.
        Messages:   	a cursor produced for another filter set must be refused
--- FAIL: TestAuditCursorIsBoundToItsFilters/comma_moved_between_kinds
        Error:      	Expected error with "ntfy: not valid" in chain but got nil.
        Messages:   	a cursor produced for another filter set must be refused
--- FAIL: TestAuditCursorIsBoundToItsFilters/no_kind_filter_and_one_empty_kind
        Error:      	Expected error with "ntfy: not valid" in chain but got nil.
        Messages:   	a cursor produced for another filter set must be refused
```

Task 1 brings these cases into the repository as the red step.

### Does not reproduce

The audit's second case paired recipient `alice` and subject `"\x00task"` with recipient `"alice\x00"` and subject `task`, across the NUL field separator. Since PR #11, validation refuses a NUL byte in any identifier, so neither query can be issued. The re-run stopped at the producer:

```
--- FAIL: TestAuditCursorIsBoundToItsFilters/a_recipient_ending_in_NUL_and_a_subject_starting_with_one
        Error:      	Received unexpected error:
        	            	ntfy: request is not valid: /subject: contains a NUL byte
        Messages:   	producer
```

That failure is the producer being refused, not the cursor being accepted. The case is dropped as a finding. The new encoding still does not rely on it: fields are length-prefixed, so no byte value can move a boundary, validated or not.

## What Changes

- **The cursor fingerprint uses an unambiguous encoding.** Each field is prefixed by its length, and each filter list by its count, before hashing, in place of the `,` and NUL joins. Two filter sets share a fingerprint only when they hold the same recipient, the same states, the same kinds and the same subject, ignoring order.
  - Filters listed in another order still match, as today.
  - A `nil` filter and an empty one still match, since both mean "no filter".
  - The cursor's wire shape (`{"t","i","f"}`, base64url) does not change, and neither does the refusal `DecodeCursor` returns.
- **BREAKING for cursors already issued:** every fingerprint changes, so a cursor issued before the upgrade is refused once, with the usual `400 validation_failed` ("belongs to another recipient or filter set"). The client restarts that listing from the first page. No tag has been cut, so this is recorded here (library-design rule 7) rather than versioned.
- **The binding becomes a spec requirement.** Until now it was stated only in godoc. The listing requirement in `notification-inbox` now states it, with scenarios for a comma inside a kind and for the empty kind.
- **Docs:** `docs/notifications.md` says what a cursor is bound to, and that a cursor used under other filters is a `400`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-inbox`: the requirement "A recipient can list, count and mark their notifications read" now states that a continuation cursor is bound to its exact recipient and filters, whatever bytes they hold.

## Impact

- **Code:** `store.go` (`ListQuery.fingerprint` and two small helpers; the `strings` import goes, `encoding/binary` comes in). There is no change to `MemoryStore`, `sqlstore` or the HTTP handler: they all call `EncodeCursor` and `DecodeCursor`, so they pick up the fix unchanged.
- **Tests:**
  - `cursor_test.go` gets a new table, `TestCursorIsBoundToExactFilters`. It holds the proof as red rows, plus two guard rows: a `nil` filter matches an empty one, and order does not matter.
  - `docs_test.go` gets two needles for the new documentation.
- **Docs:** `docs/notifications.md`, and the godoc of `ListQuery.Cursor` and `DecodeCursor`.
- **Hosts:** a client holding a cursor across the upgrade gets one `400` and starts again from the first page. A host store that builds cursors through `EncodeCursor` gets the fix with no change. A host store with a cursor codec of its own is not affected, and is responsible for its own binding.
- **Dependencies:** none. The core stays on the standard library.
- **Security:** none. A cursor is unsigned, and a client can already build one for any filter it likes. The binding is a consistency guard for honest clients, not an access control, and this change does not make it one.
