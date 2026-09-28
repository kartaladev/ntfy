# Bind Cursors to Exact Filters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a listing cursor continue only the listing whose recipient, states, kinds and subject produced it, whatever bytes those values hold.

**Architecture:**
- `ListQuery.fingerprint` in `store.go` stops joining values with `,` and NUL. It hashes a length-prefixed encoding instead: each field is written as a uvarint length followed by its bytes, and each list as a uvarint count followed by length-prefixed values.
- The cursor's wire shape, `EncodeCursor`, `DecodeCursor`'s refusal, and every store stay as they are. Stores reach the fingerprint only through `EncodeCursor` and `DecodeCursor`.

**Tech Stack:**
- Go 1.26, with the core module on the standard library only (`crypto/sha256`, `encoding/binary`, `encoding/hex`).
- `stretchr/testify` in tests.
- `golangci-lint` v2 and `openspec`.

**Spec:** `openspec/changes/bind-cursors-to-exact-filters/`. Read:
- `proposal.md`: why, the failing audit output, and the "Does not reproduce" NUL case;
- `design.md`: D1 length-prefixed encoding, D2 no compatibility path;
- `specs/notification-inbox/spec.md`: the MODIFIED listing requirement and its three new scenarios.

## Global Constraints

- **Go 1.26: export `GOTOOLCHAIN=go1.26.8` before any Go command.** A newer Go may be first on `PATH`. The `make` targets already pin it.
- **The core module imports only the standard library** in production code. Tests may use `testify`, `goleak` and `go.uber.org/mock`. This is enforced by the `.golangci.yml` depguard rules and by `make split-check`. This change adds `encoding/binary`, which is stdlib, and removes `strings` from `store.go`.
- **Satellite modules may import only `ntfy`, `sqlkit` and their own client library.** `make split-check` is authoritative. No satellite changes here.
- **`pkg/sqlkit` changes only through a recorded patch** (`make sqlkit-copy-check`). This change does not touch it.
- **Tests follow the `table-test` skill:**
  - an `assert` closure on every case, never `want`/`wantErr` fields;
  - `t.Context()` where a context is needed;
  - `require` only for preconditions.
  - The cursor table does not vary context, because encoding and decoding take none, so it carries no `ctx` field. It says so in a one-line comment.
- **Test doubles come from the `use-mockgen` skill.** None are needed here.
- **Heavy services come from the `use-testcontainers` skill.** None are needed here, beyond what `make store-matrix` already starts.
- **`.claude/rules/prove-errors-with-tests.md`:** the red step (Task 1, Step 2) is run and its output compared with the expected failure below before any production edit. A compile error does not count as red.
- **`.claude/rules/golang-tdd.md`:** red → green → refactor. The test and the code that satisfies it land in the same commit. Consider `/simplify` on the touched code once green.
- **`.claude/rules/library-design.md`:** the binding is a guarantee with no override (`design.md` D1 says why). Do not add an option. The broken compatibility of in-flight cursors is recorded in `proposal.md` (rule 7: no tag yet).
- **`.claude/rules/performance-benchmark.md`:** makes no performance claim, so no benchmark.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn. This plan has no STOP step: the defect was re-proved on `main` `20967b3` on 2026-09-28 (see `proposal.md`). If Task 1, Step 2 does **not** fail as shown, **stop**, record the output under "Does not reproduce" in `proposal.md`, and do not continue.
- **Navigate Go with `gopls`** (`.claude/rules/gopls-navigation.md`), and invoke `/golang-how-to` before the first Go edit.
- **Done means `make all` and `make store-matrix` pass.** Every store's `List` decodes cursors.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`. They end with the session's attribution lines.

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `cursor_test.go` | `ntfy` | **Modify** (append). `TestCursorIsBoundToExactFilters`: the red proof, plus two guard rows for equalities that must survive. |
| `store.go:1-17` | `ntfy` | **Modify.** Imports: add `encoding/binary`, drop `strings`. |
| `store.go:270-272` | `ntfy` | **Modify.** `ListQuery.Cursor` godoc. |
| `store.go:383-386` | `ntfy` | **Modify.** `DecodeCursor` godoc. |
| `store.go:417-433` | `ntfy` | **Modify.** `fingerprint` uses the length-prefixed encoding; add `appendField` and `appendFields`. |
| `docs_test.go:111-116` | `ntfy` | **Modify.** Two needles for the "HTTP and mounting" row. |
| `docs/notifications.md` (section "HTTP and mounting") | — | **Modify.** One bullet on cursor binding and the one-time refusal after upgrade. |

**Shared-file note:** `docs_test.go`'s needle table and `docs/notifications.md` are edited by most changes. Another change in flight that adds a needle to the same row conflicts on adjacent lines; resolve by keeping both.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: bind cursors to the exact filters | 1.1, 2.1, 2.2 |
| Task 2: document the rule | 3.1, 3.2 |
| Task 3: verify and hand off | 4.1, 4.2 |

---

### Task 1: Bind cursors to the exact filters

**Files:**
- Modify: `cursor_test.go` (append), `store.go:1-17`, `store.go:270-272`, `store.go:383-386`, `store.go:417-433`

**Interfaces:**
- Consumes (existing, package `ntfy`):
  - `type ListQuery struct { Recipient string; States []State; Kinds []string; Subject string; Limit int; Cursor string }`;
  - `type CursorPosition struct { CreatedAt time.Time; ID string }`;
  - `func EncodeCursor(q ListQuery, position CursorPosition) string`;
  - `func DecodeCursor(q ListQuery) (CursorPosition, bool, error)`;
  - `var ErrValidation` (matched by `errors.Is` on a `*ValidationError`).
- Produces (package-private, `store.go`):
  - `func (q ListQuery) fingerprint() string`, with the same signature and a new encoding;
  - `func appendField(b []byte, value string) []byte`;
  - `func appendFields(b []byte, values []string) []byte`.

- [ ] **Step 1: Write the failing test (tasks 1.1)**

Append to `cursor_test.go` (its imports already hold `testing`, `time`, `assert`, `require` and `ntfy`):

```go
// TestCursorIsBoundToExactFilters proves a cursor continues only the listing
// whose recipient and filters produced it, whatever bytes the filters hold.
// Encoding and decoding take no context, so the table has no ctx field.
func TestCursorIsBoundToExactFilters(t *testing.T) {
	t.Parallel()

	position := ntfy.CursorPosition{CreatedAt: time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC), ID: "n-1"}

	refused := func(t *testing.T, _ ntfy.CursorPosition, _ bool, err error) {
		assert.ErrorIs(t, err, ntfy.ErrValidation, "a cursor produced for another filter set must be refused")
	}

	accepted := func(t *testing.T, got ntfy.CursorPosition, ok bool, err error) {
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "n-1", got.ID)
	}

	type testCase struct {
		name     string
		producer ntfy.ListQuery
		consumer ntfy.ListQuery
		assert   func(t *testing.T, got ntfy.CursorPosition, ok bool, err error)
	}

	cases := []testCase{
		{
			name:     "two kinds are not one kind holding a comma",
			producer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{"a", "b"}},
			consumer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{"a,b"}},
			assert:   refused,
		},
		{
			name:     "a comma moved between kinds is another filter",
			producer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{"a,b", "c"}},
			consumer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{"a", "b,c"}},
			assert:   refused,
		},
		{
			name:     "no kind filter is not the empty kind",
			producer: ntfy.ListQuery{Recipient: "alice"},
			consumer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{""}},
			assert:   refused,
		},
		{
			name:     "the empty kind is not no kind filter",
			producer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{""}},
			consumer: ntfy.ListQuery{Recipient: "alice"},
			assert:   refused,
		},
		{
			name:     "a nil kind filter is the same as an empty one",
			producer: ntfy.ListQuery{Recipient: "alice", Kinds: nil},
			consumer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{}},
			assert:   accepted,
		},
		{
			name:     "kinds holding commas listed in another order are the same filter",
			producer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{"b", "a,c"}},
			consumer: ntfy.ListQuery{Recipient: "alice", Kinds: []string{"a,c", "b"}},
			assert:   accepted,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			consumer := tc.consumer
			consumer.Cursor = ntfy.EncodeCursor(tc.producer, position)

			got, ok, err := ntfy.DecodeCursor(consumer)
			tc.assert(t, got, ok, err)
		})
	}
}
```

- [ ] **Step 2: Run the test and watch it fail for the stated reason**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestCursorIsBoundToExactFilters' -count=1 .`

Expected: FAIL. Exactly the four `refused` rows fail, and each prints:

```
--- FAIL: TestCursorIsBoundToExactFilters/two_kinds_are_not_one_kind_holding_a_comma
        Error:      	Expected error with "ntfy: not valid" in chain but got nil.
        Messages:   	a cursor produced for another filter set must be refused
--- FAIL: TestCursorIsBoundToExactFilters/a_comma_moved_between_kinds_is_another_filter
        Error:      	Expected error with "ntfy: not valid" in chain but got nil.
--- FAIL: TestCursorIsBoundToExactFilters/no_kind_filter_is_not_the_empty_kind
        Error:      	Expected error with "ntfy: not valid" in chain but got nil.
--- FAIL: TestCursorIsBoundToExactFilters/the_empty_kind_is_not_no_kind_filter
        Error:      	Expected error with "ntfy: not valid" in chain but got nil.
```

The two `accepted` rows pass. They are guards: they hold equalities the new encoding must keep.

If a `refused` row passes, or the failure is anything else, **stop**. Record the output under "Does not reproduce" in `proposal.md`, and do not change `store.go`.

- [ ] **Step 3: Replace the fingerprint (tasks 2.1)**

In `store.go`'s import block, replace `"strings"` with nothing, and add `"encoding/binary"` after `"encoding/base64"`. The block becomes:

```go
import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"time"
)
```

(Before deleting `strings`, confirm with `gopls` references, or with `go build .` after the edit, that nothing else in `store.go` uses it. On `20967b3` only the fingerprint does.)

Replace `store.go:417-433`, from the `// fingerprint identifies` comment through the closing brace of `fingerprint`, with:

```go
// fingerprint identifies a query's recipient and filters, ignoring the order
// the filters are listed in. Every value is written with its length, and every
// list with its count, so that no byte a value holds can move a boundary: two
// queries share a fingerprint only when their recipient, states, kinds and
// subject are equal. A nil filter and an empty one are the same filter.
func (q ListQuery) fingerprint() string {
	states := make([]string, 0, len(q.States))
	for _, state := range q.States {
		states = append(states, string(state))
	}

	slices.Sort(states)

	kinds := slices.Clone(q.Kinds)
	slices.Sort(kinds)

	var encoded []byte
	encoded = appendField(encoded, q.Recipient)
	encoded = appendFields(encoded, states)
	encoded = appendFields(encoded, kinds)
	encoded = appendField(encoded, q.Subject)

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:8])
}

// appendField appends value to b, prefixed by its length in bytes.
func appendField(b []byte, value string) []byte {
	b = binary.AppendUvarint(b, uint64(len(value)))

	return append(b, value...)
}

// appendFields appends how many values there are, then each value as a field.
func appendFields(b []byte, values []string) []byte {
	b = binary.AppendUvarint(b, uint64(len(values)))
	for _, value := range values {
		b = appendField(b, value)
	}

	return b
}
```

- [ ] **Step 4: Run the new test and the existing cursor tests**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestCursor|TestListQuery' -count=1 .`

Expected: PASS. That covers all six rows of `TestCursorIsBoundToExactFilters`, and every row of the existing `TestCursor`:
- a round trip under the same query;
- filters listed in another order accepted;
- another recipient, other states and another subject refused;
- a foreign cursor refused.

It also covers `TestListQueryValidate` and `TestListQueryValidateFilterLimits`.

To confirm the nil guard guards something, temporarily add `if values == nil { return b }` as the first line of `appendFields`. A nil list then writes nothing, while an empty list writes count `0`. Watch `a_nil_kind_filter_is_the_same_as_an_empty_one` fail with `Received unexpected error: ntfy: request is not valid: /cursor: belongs to another recipient or filter set`. Revert, and re-run to PASS.

- [ ] **Step 5: Update the godoc (tasks 2.2)**

In `store.go`, replace the `Cursor` field comment (lines 270-271) with:

```go
	// Cursor continues from the previous page's NextCursor. A cursor is bound to
	// the listing that produced it: the same recipient, states, kinds and
	// subject, listed in any order, whatever bytes they hold. Under any other
	// recipient or filters it is refused as a [ValidationError].
	Cursor string
```

Replace the `DecodeCursor` comment (lines 383-385) with:

```go
// DecodeCursor reads q's cursor. It reports false when q has no cursor, and a
// [ValidationError] when the cursor is malformed or was produced for another
// recipient, other states, other kinds or another subject. Filters listed in
// another order, and a nil filter in place of an empty one, are the same
// filters.
```

Run: `GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy ListQuery && GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy DecodeCursor`

Expected: both print the new text.

- [ ] **Step 6: Lint and the whole core package**

Run: `GOTOOLCHAIN=go1.26.8 go test -count=1 . && golangci-lint run .`

Expected: PASS, and `0 issues.`

- [ ] **Step 7: Commit**

```bash
git add cursor_test.go store.go
git commit -m "$(cat <<'EOF'
Bind cursors to their exact filters

The cursor fingerprint joined kinds with "," and fields with NUL, so a
cursor from kinds ["a","b"] continued a listing of kinds ["a,b"], and one
from no kind filter continued a listing of [""]. Each value is now
length-prefixed before hashing, so two queries share a fingerprint only
when their recipient, states, kinds and subject are equal, in any order.

Cursors issued before this change are refused once; clients restart
from the first page.

Co-Authored-By: <the session's attribution lines>
EOF
)"
```

---

### Task 2: Document the rule

**Files:**
- Modify: `docs_test.go:111-116`, `docs/notifications.md` (section "HTTP and mounting")

**Interfaces:**
- Consumes: `TestTheDocumentMatchesTheImplementation` and its `docSection(t, document, heading string) string`, both existing in `docs_test.go`.
- Produces: nothing that code consumes.

- [ ] **Step 1: Write the failing test (tasks 3.1)**

In `docs_test.go`, in the `"the HTTP contract and its mounting"` row, extend `needles`:

```go
			needles: []string{
				ntfy.DefaultBasePath, strconv.Itoa(ntfy.DefaultListLimit), strconv.Itoa(ntfy.MaxListLimit),
				funcName(ntfy.NewHandler), funcName(ntfy.WithActor), funcName(ntfy.WithBasePath),
				funcName(ntfy.WithSubscriptionAuthorizer), funcName(ntfy.WriteError),
				"gin.WrapH", "adaptor.HTTPHandler", "X-Accel-Buffering",
				"cursor is bound to", "first page",
			},
```

- [ ] **Step 2: Run it and watch it fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .`

Expected: FAIL in `TestTheDocumentMatchesTheImplementation/the_HTTP_contract_and_its_mounting`, with two messages:

```
Messages:   	the "HTTP and mounting" section mentions cursor is bound to
Messages:   	the "HTTP and mounting" section mentions first page
```

- [ ] **Step 3: Add the bullet (tasks 3.2)**

In `docs/notifications.md`, under `## HTTP and mounting`, insert this bullet after the one that begins `- Every endpoint acts on the acting user's own notifications.`:

```markdown
- A cursor is bound to the listing that produced it: the same caller and the
  same `state`, `kind` and `subject` values, listed in any order. Presented
  with any other filters it is a `400` with `validation_failed`, whatever the
  values hold: `?kind=a&kind=b` and `?kind=a%2Cb` are different listings.
  Cursors issued before the upgrade that made this exact are refused once, and
  the client starts again from the first page.
```

- [ ] **Step 4: Run it and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add docs_test.go docs/notifications.md
git commit -m "$(cat <<'EOF'
Document what a listing cursor is bound to

Co-Authored-By: <the session's attribution lines>
EOF
)"
```

---

### Task 3: Verify and hand off

**Files:** none, unless the review finds something.

**Interfaces:** none.

- [ ] **Step 1: Run the full gate (tasks 4.1)**

Run: `make all`

Expected: exit status 0 (lint, `split-check` and every module's tests).

- [ ] **Step 2: Run every store (tasks 4.1)**

Run: `make store-matrix`

Expected: exit status 0. All seven store combinations pass `ntfytest.Run`, including the paging cases in `ntfytest/suite.go` (`newest first with exact paging while notifications arrive`, `notifications created at the same instant page exactly`), which round-trip cursors through the new fingerprint.

- [ ] **Step 3: Review (tasks 4.2)**

Run `/code-review` on the branch diff against `main`. Answer each finding under `superpowers:receiving-code-review`. Every accepted defect gets a failing test first (`.claude/rules/prove-errors-with-tests.md`), then its fix, then `make all` again. Record accepted findings as new tasks in `tasks.md`, and mirror them here in the same turn.

- [ ] **Step 4: Tick `tasks.md`**

Mark 1.1–4.2 done in `tasks.md`. Those boxes are authoritative; this plan's boxes are working notes.

---

## Self-Review

- **Spec coverage:**
  - "A cursor continues only the filters that produced it": existing `TestCursor` rows (order, recipient, states, subject) plus the store-matrix paging cases.
  - "A comma inside a kind does not merge two filter sets": Task 1, rows 1 and 2.
  - "No kind filter is not the empty kind": Task 1, rows 3 and 4.
  - "Whatever bytes": covered by construction (D1), and by rows holding `,`.
  - The documentation and compatibility note are in Task 2.
- **Placeholders:** none. The commit trailer is the session's attribution, filled in at commit time.
- **Names:** `fingerprint`, `appendField` and `appendFields` are used the same way in the Interfaces block, the code and `design.md` D1. `TestCursorIsBoundToExactFilters` matches `tasks.md` 1.1.
