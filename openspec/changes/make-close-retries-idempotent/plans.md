# Idempotent Close Retries Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A retried close changes nothing on every store, whichever kinds it names. The watermark retention's godoc says what the stores measure.

**Architecture:**
- Both stores leave out of a close any notification whose `SourceID` is the close's own `Successor.SourceID`:
  - memory adds one `case` to `MemoryStore.Close`'s selection;
  - SQL adds one condition to `closingCondition`, which feeds both the recipients read and the rows updated.
- The proof is a table in the `ntfytest` conformance suite, so the memory store, all seven SQL driver-by-dialect entry points and any host's own store are held to it.
- The retention change is documentation only. A text-reading docs test is its red step, and a conformance case pins the behaviour the corrected godoc describes.

**Tech Stack:**
- Go 1.26, with the core module on the standard library only.
- `stretchr/testify`.
- `testcontainers-go` through `sqlkittest.RunTestPostgres` and `sqlkittest.RunTestMySQL`, used by `make store-matrix`; `sqlkittest.RunTestSQLite` needs no Docker.
- `golangci-lint` v2.13.2 and `openspec`.

**Spec:** `openspec/changes/make-close-retries-idempotent/`. Read:
- `proposal.md`, for why, and the failing output re-proved on `main` at `20967b3`;
- `design.md`: D1 on excluding the successor's source, D2 on the proof in the suite, D3 on fixing the godoc rather than the stores;
- `specs/notification-inbox/spec.md` and `specs/notification-retention/spec.md`, for the scenarios this plan must satisfy.

## Global Constraints

- **Go 1.26: export `GOTOOLCHAIN=go1.26.8` before any Go command.** A newer Go (1.27.1 on the author's machine) is first on `PATH`. The `make` targets already pin it.
- **The core module imports only the standard library** in production code. Tests may use `testify`, `goleak` and `go.uber.org/mock`. Enforced by `.golangci.yml` depguard and `make split-check`.
  - depguard's `$gostd` list **rejects `go/ast`, `go/parser` and `go/token`**, observed 2026-09-28. The docs test in Task 2 reads source as text for that reason. Do not "improve" it to use `go/parser`.
- **Each satellite module (`ntfytest`, `sqlstore`, …) may import only `ntfy`, `sqlkit` and its own client library.** `make split-check` is authoritative. This change adds no import to any module except `slices` in the core module's `docs_test.go`.
- **Tests follow the `table-test` skill:**
  - an `assert` closure on every case, never `want`/`wantErr` fields;
  - `t.Context()`, never `context.Background()`;
  - `require` only for preconditions.
  - Neither table in this plan varies context: closing and reading godoc are not cancellation-sensitive. Each table says so in a one-line comment, and carries no `ctx` field.
- **Test doubles come from the `use-mockgen` skill.** This change needs none.
- **External services come from the `use-testcontainers` skill.** Use the existing `sqlkittest.RunTest*` helpers through the existing `TestStoreOn*` entry points. Never write a container helper.
- **`.claude/rules/prove-errors-with-tests.md`:** each red step is run, and its failing output compared with the output recorded here, before any production edit. A red caused by compilation or a container error does not count.
- **`.claude/rules/golang-tdd.md`:**
  - red → green → refactor;
  - the test and the code that satisfies it land in the same commit;
  - a test that passes on today's code gets a temporary mutation that turns it red, reverted before commit.
- **`.claude/rules/library-design.md`:** the successor-source rule is a spec guarantee with no override (`design.md` D1 says why). Do not add an option for it.
- **`.claude/rules/performance-benchmark.md`:** no performance claim is made, so no benchmark.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn.
- **Done means `make all` and `make store-matrix` pass**, since two stores change.
  - `make all` runs lint, split-check and test.
  - `make store-matrix` runs seven driver-by-dialect entry points and needs Docker.
- **Run golangci-lint with `GOTOOLCHAIN=go1.26.8`,** as `make lint` does. Under a newer local toolchain, golangci-lint reports a spurious `ntfytest/isolation.go:15:1: unnamedResult` (gocritic). Checked 2026-09-28: with the pinned toolchain, `ntfytest` lints with `0 issues.` on `main` at `20967b3`.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`. They end with the session's attribution lines.

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `ntfytest/suite.go:724-739` | `ntfy/ntfytest` | **Modify.** Replace the single retry case in `runSuccessors` with the three-row `retriedClose` table, and add "a later close still closes an earlier close's successors". |
| `ntfytest/suite.go:1060` (before) | `ntfy/ntfytest` | **Modify.** Add "a close record can expire with its subject's last notification" to `runRetention`. |
| `memory.go:226-233` | `ntfy` | **Modify.** `MemoryStore.Close` skips the close's own successor source. |
| `sqlstore/close.go:100-115` | `ntfy/sqlstore` | **Modify.** `closingCondition` adds `source_id <> ?`, and its comment says so. |
| `store.go:46-49`, `store.go:127-128` | `ntfy` | **Modify.** The godoc of `Store.Close` and `CloseRequest.Successor` states the rule. |
| `store.go:469-470` | `ntfy` | **Modify.** `PruneRequest.WatermarkRetention` godoc. |
| `pruner.go:17-19`, `pruner.go:88-90` | `ntfy` | **Modify.** `DefaultWatermarkRetention` and `WithWatermarkRetention` godoc. |
| `docs_test.go` | `ntfy` | **Modify.** Add `docComment` and `TestWatermarkRetentionGodocSaysWhatTheStoreMeasures`. |
| `docs/notifications.md` | — | **Modify.** The "Successors" paragraph, and the retention table row. |

**Shared-file note:** `ntfytest/suite.go` is the usual conflict point for changes in flight. This plan edits the middle of `runSuccessors` and the end of `runRetention`, not `Run`'s registration block.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: a retried close changes nothing | 1.1, 1.2, 2.1, 2.2, 2.3 |
| Task 2: the retention godoc says what the stores measure | 3.1, 3.2, 3.3 |
| Task 3: verify and hand off | 4.1, 4.2, 4.3 |

Task 1 gathers five `tasks.md` items into one commit. The red rows in `ntfytest` fail on both stores, so neither store's fix can land green without the other.

---

### Task 1: A retried close changes nothing

**Files:**
- Modify: `ntfytest/suite.go:724-739`, `memory.go:226-233`, `sqlstore/close.go:100-115`, `store.go:46-49`, `store.go:127-128`, `docs/notifications.md` ("Successors" paragraph)

**Interfaces:**
- Consumes (existing, in package `ntfytest`, file `ntfytest/suite.go`):
  - `func newEnv(t *testing.T, factory Factory) *env`;
  - `func (e *env) close(req ntfy.CloseRequest, when time.Time) ntfy.CloseResult`;
  - `func (e *env) get(recipient, id string) ntfy.Notification`;
  - `func (e *env) list(q ntfy.ListQuery) ntfy.Page`;
  - `func parallel(t *testing.T, name string, fn func(t *testing.T))` and `func at(n int) time.Time`;
  - inside `runSuccessors`, the closures `offers := func(e *env)`, which inserts `"offer"` v1 for alice, bob and carol on `task-1`, and `taken := func(skip ...string) ntfy.CloseRequest`, which returns `Kinds: ["offer"]`, `Version: 5` and a `"taken"` successor from `"event-5"` at v5.
  - In `ntfy`: `CloseRequest{Subject, Kinds []string, Version int64, Reason, Except string, Successor *Successor, SuccessorSkip []string}`; `Successor{SourceID, Kind, Title string, SubjectVersion int64, …}`; `CloseResult{Closed int64, Recipients []string, Successors []Notification, SuccessorsSuppressed int}`.
  - In `sqlstore`: `func (s *Store) closingCondition(w *sqlkit.Writer, req ntfy.CloseRequest)`, `s.quote(string) string`, `w.Bind(any) string`, `w.Write(...string)`.
- Produces:
  - In `ntfytest`, local to `runSuccessors`: `type retriedClose struct{ name string; kinds []string; assert func(t *testing.T, e *env, first, second ntfy.CloseResult) }`, and the closure `retryHeldToNothing`.
  - No exported API changes. The behaviour change is to `Store.Close` on `*ntfy.MemoryStore` and `*sqlstore.Store`.

- [ ] **Step 1: Write the red table (tasks 1.1)**

In `ntfytest/suite.go`, delete the case that starts `parallel(t, "a retried close creates no further successors", func(t *testing.T) {` (lines 724-739, through its closing `})` and the blank line after it). Put this in its place, directly before `parallel(t, "a successor below a newer watermark is suppressed", …`:

```go
	// A retried close is held to nothing whichever kinds it names, including
	// every kind, and the kind of the successor its first attempt created at
	// the close's own version. Closing is not context-sensitive, so the table
	// has no ctx field.
	type retriedClose struct {
		name string
		// kinds is the close's Kinds; nil closes every kind.
		kinds  []string
		assert func(t *testing.T, e *env, first, second ntfy.CloseResult)
	}

	retryHeldToNothing := func(t *testing.T, e *env, first, second ntfy.CloseResult) {
		t.Helper()

		require.Len(t, first.Successors, 2)
		assert.Zero(t, second.Closed, "a retried close closes nothing further")
		assert.Empty(t, second.Recipients, "a retried close tells no recipient")
		assert.Empty(t, second.Successors, "a retried close creates no further successors")

		for _, successor := range first.Successors {
			got := e.get(successor.Recipient, successor.ID)
			assert.Equalf(t, ntfy.StateActive, got.State,
				"%s keeps the successor the first attempt created", successor.Recipient)
		}

		for _, recipient := range []string{"alice", "bob"} {
			page := e.list(ntfy.ListQuery{Recipient: recipient, Kinds: []string{"taken"}})
			assert.Lenf(t, page.Notifications, 1, "%s keeps exactly one successor", recipient)
		}
	}

	retries := []retriedClose{
		{name: "a retried close of one kind closes nothing further", kinds: []string{"offer"}, assert: retryHeldToNothing},
		{name: "a retried close of every kind closes nothing further", kinds: nil, assert: retryHeldToNothing},
		{
			name:   "a retried close naming the successor's kind closes nothing further",
			kinds:  []string{"offer", "taken"},
			assert: retryHeldToNothing,
		},
	}

	for _, tc := range retries {
		parallel(t, tc.name, func(t *testing.T) {
			e := newEnv(t, factory)
			offers(e)

			req := taken("carol")
			req.Kinds = tc.kinds

			first := e.close(req, at(4))
			second := e.close(req, at(6))

			tc.assert(t, e, first, second)
		})
	}

```

- [ ] **Step 2: Watch it fail on the memory store**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/successors/a_retried' -count=1 -v .`

Expected, as observed on 2026-09-28, abridged:

```
--- FAIL: TestMemoryStoreConformance (0.00s)
    --- FAIL: TestMemoryStoreConformance/successors (0.00s)
        --- PASS: TestMemoryStoreConformance/successors/a_retried_close_of_one_kind_closes_nothing_further (0.00s)
        --- FAIL: TestMemoryStoreConformance/successors/a_retried_close_of_every_kind_closes_nothing_further (0.00s)
        --- FAIL: TestMemoryStoreConformance/successors/a_retried_close_naming_the_successor's_kind_closes_nothing_further (0.00s)
        	Error:      	Should be zero, but was 2
        	Messages:   	a retried close closes nothing further
        	Error:      	Should be empty, but was [alice bob]
        	Messages:   	a retried close tells no recipient
        	Error:      	Not equal:
        	            	expected: "ACTIVE"
        	            	actual  : "CLOSED"
        	Messages:   	alice keeps the successor the first attempt created
FAIL
```

**STOP** if the two rows fail for any other reason (compilation, a `require` precondition), or if either passes. Then the defect no longer reproduces as stated. Record the output under "Does not reproduce" in `proposal.md`, and do not continue to Step 6.

- [ ] **Step 3: Watch it fail on SQL**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/successors/a_retried' -count=1 -v .`

Expected: the same split. `a_retried_close_of_one_kind_…` passes, and the `…of_every_kind…` and `…naming_the_successor's_kind…` rows fail with `Should be zero, but was 2` and `Should be empty, but was [alice bob]`. On 2026-09-28 the same split was also seen on `TestStoreOnStdSQLPostgres` and `TestStoreOnStdSQLMySQL`, using Docker.

- [ ] **Step 4: Add the guard against an over-broad fix (tasks 1.2)**

Directly after the `for _, tc := range retries { … }` loop from Step 1, and before `parallel(t, "a successor below a newer watermark is suppressed", …`, add:

```go
	parallel(t, "a later close still closes an earlier close's successors", func(t *testing.T) {
		e := newEnv(t, factory)
		offers(e)

		first := e.close(taken("carol"), at(4))
		require.Len(t, first.Successors, 2)

		later := e.close(ntfy.CloseRequest{Subject: "task-1", Version: 6, Reason: "done"}, at(6))

		assert.Equal(t, int64(2), later.Closed, "the later close closes both successors")
		assert.Equal(t, []string{"alice", "bob"}, later.Recipients)

		for _, successor := range first.Successors {
			assert.Equalf(t, ntfy.StateClosed, e.get(successor.Recipient, successor.ID).State,
				"%s's successor is closed by the later close", successor.Recipient)
		}
	})

```

- [ ] **Step 5: Show the guard can fail, then revert the mutation**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/successors/a_later' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy`. It passes on today's code.

Temporarily add a wrong fix to `memory.go`'s selection, after the `req.Except` case:

```go
		case n.Kind == "taken":
			continue
```

Run the same command. Expected, as observed:

```
	Error:      	Not equal:
	Messages:   	the later close closes both successors
	Messages:   	alice's successor is closed by the later close
	Messages:   	bob's successor is closed by the later close
FAIL
```

Remove the two mutation lines. Then run `git diff memory.go` and expect no output.

- [ ] **Step 6: Make the memory store skip its own successor (tasks 2.1)**

In `memory.go`, `MemoryStore.Close`, the selection `switch` becomes:

```go
		switch {
		case n.State == StateClosed || n.SubjectVersion > req.Version:
			continue
		case len(req.Kinds) > 0 && !slices.Contains(req.Kinds, n.Kind):
			continue
		case req.Except != "" && n.Recipient == req.Except:
			continue
		case req.Successor != nil && n.SourceID == req.Successor.SourceID:
			// The close's own successor, created by an earlier attempt of
			// this close: a retry must not undo it.
			continue
		}
```

- [ ] **Step 7: Run the memory conformance suite**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy`

- [ ] **Step 8: Make the SQL store skip its own successor (tasks 2.2)**

In `sqlstore/close.go`, replace `closingCondition` and its comment with:

```go
// closingCondition writes the WHERE clause selecting the notifications a close
// closes: the subject's open ones at or below the close's version, of the kinds
// it names, sparing its excepted recipient and its own successor's source.
func (s *Store) closingCondition(w *sqlkit.Writer, req ntfy.CloseRequest) {
	w.Write(" WHERE ", s.quote("subject"), " = ", w.Bind(req.Subject),
		" AND ", s.quote("state"), " <> ", w.Bind(string(ntfy.StateClosed)),
		" AND ", s.quote("subject_version"), " <= ", w.Bind(req.Version))

	if len(req.Kinds) > 0 {
		w.Write(" AND ", s.quote("kind"), " IN (", w.BindAll(anys(req.Kinds)...), ")")
	}

	if req.Except != "" {
		w.Write(" AND ", s.quote("recipient"), " <> ", w.Bind(req.Except))
	}

	// The close's own successor, created by an earlier attempt of this close:
	// a retry must not undo it.
	if req.Successor != nil {
		w.Write(" AND ", s.quote("source_id"), " <> ", w.Bind(req.Successor.SourceID))
	}
}
```

In the same file, the second paragraph of `Store.Close`'s comment becomes:

```go
// In one transaction, after locking the subject's '*' watermark row: raise the
// watermark for each kind closed, select the recipients the close will close,
// close them, and publish the successor to those recipients through the same
// insert path a publish takes, so that a successor below a newer watermark is
// suppressed like any late publish. A close never closes a notification from
// its own successor's source, so a retry leaves its first attempt's successors
// open.
```

- [ ] **Step 9: Run the SQLite conformance suite**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy/sqlstore`

- [ ] **Step 10: State the rule where it is read (tasks 2.3)**

In `store.go`, the `Close` method on the `Store` interface:

```go
	// Close closes a subject's notifications by kind and version, raises the
	// subject's watermark, and publishes req.Successor to the recipients it
	// closed, all in one transaction. ids stamps each successor. It never
	// closes a notification whose SourceID is req.Successor's, so a close
	// retried after an earlier attempt committed closes nothing further and
	// leaves that attempt's successors open.
	Close(ctx context.Context, req CloseRequest, at time.Time, ids IDGenerator) (CloseResult, error)
```

In `store.go`, the `Successor` field of `CloseRequest`:

```go
	// Successor, when set, is published to each recipient this close closed.
	// The close never closes a notification from the successor's SourceID.
	Successor *Successor
```

In `docs/notifications.md`, the "Successors" paragraph becomes:

```markdown
**Successors.** A close can name a `Successor`: a notification published, in the
same transaction, to every recipient the close moved to `CLOSED`, except those in
`SuccessorSkip`. A publisher uses it to tell everyone else who was offered
something that it has been taken, sparing the person who took it. A close never
closes a notification from its own successor's `SourceID`, so a retried close
closes nothing further and creates no second successor, even when it closes
every kind or names the successor's kind. A later close, with another successor
or none, still closes them. A successor below a newer watermark is suppressed
like any late publish.
```

- [ ] **Step 11: Run the core module's tests and lint the three modules**

Run: `GOTOOLCHAIN=go1.26.8 go test -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy`. This includes `TestTheDocumentMatchesTheImplementation`, whose "Publishing and closing" needles `SourceID`, `Successor`, `SuccessorSkip` and `Except` are all still present.

Run: `golangci-lint run ./...`, then `cd sqlstore && golangci-lint run ./...`.
Expected: `0 issues.` for each.

Run: `cd ntfytest && golangci-lint run ./...`.
Expected: `0 issues.` (with `GOTOOLCHAIN=go1.26.8`; see Global Constraints).

- [ ] **Step 12: Commit**

```bash
git add ntfytest/suite.go memory.go sqlstore/close.go store.go docs/notifications.md
git commit -m "$(cat <<'EOF'
Keep a retried close from closing its own successors

A close of every kind, or one naming its successor's kind, closed on retry
the successors its first attempt created. Both stores now leave out of a
close any notification from its own successor's source, and the
conformance suite retries closes of one kind, every kind and the
successor's kind on every store.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A
EOF
)"
```

Then tick 1.1, 1.2, 2.1, 2.2 and 2.3 in `tasks.md`.

---

### Task 2: The retention godoc says what the stores measure

**Files:**
- Modify: `docs_test.go` (the imports, and an append), `pruner.go:17-19`, `pruner.go:88-90`, `store.go:469-470`, `docs/notifications.md` (the retention table), `ntfytest/suite.go` (`runRetention`, before `"close records never expire without a retention"`)

**Interfaces:**
- Consumes (existing):
  - In `ntfy_test` (`docs_test.go`): `func funcName(fn any) string`, which returns an exported function's bare name.
  - In `ntfytest`: `env.insert(coalesce bool, notifications ...ntfy.Notification) ntfy.InsertResult`, `env.note(recipient, source, subject, kind string, version int64, created time.Time) ntfy.Notification`, `env.close`, and `env.prune(req ntfy.PruneRequest) ntfy.PruneResult`, which defaults `Batch` and `Strategy`; `days(n int) time.Duration`.
  - Inside `runRetention`: `now := base.Add(days(365))`.
  - `ntfy.PruneResult{DeletedForAge, WatermarksDeleted int64, …}`.
- Produces:
  - In `ntfy_test`: `func docComment(t *testing.T, file, declaration string) string`, and `func TestWatermarkRetentionGodocSaysWhatTheStoreMeasures(t *testing.T)`.

- [ ] **Step 1: Write the red docs test (tasks 3.1)**

In `docs_test.go`, add `"slices"` to the imports after `"runtime"`:

```go
import (
	"os"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
```

Append to the end of `docs_test.go`:

```go

// docComment returns, on one line, the doc comment above the first line of a
// Go file in this package that starts with declaration once indentation is
// trimmed: "Name =" for a constant, "func Name(" for a function, "Field Type"
// for a struct field. It reads the source as text: go/parser is outside the
// module's depguard allow-list.
func docComment(t *testing.T, file, declaration string) string {
	t.Helper()

	raw, err := os.ReadFile(file)
	require.NoErrorf(t, err, "read %s", file)

	lines := strings.Split(string(raw), "\n")

	at := slices.IndexFunc(lines, func(line string) bool {
		return strings.HasPrefix(strings.TrimSpace(line), declaration)
	})
	require.GreaterOrEqualf(t, at, 0, "%s declares %q", file, declaration)

	var doc []string

	for i := at - 1; i >= 0; i-- {
		text, isComment := strings.CutPrefix(strings.TrimSpace(lines[i]), "//")
		if !isComment {
			break
		}

		doc = append([]string{strings.TrimSpace(text)}, doc...)
	}

	require.NotEmptyf(t, doc, "%s documents %q", file, declaration)

	// One line, so that a phrase wrapped across comment lines still matches.
	return strings.Join(strings.Fields(strings.Join(doc, " ")), " ")
}

// TestWatermarkRetentionGodocSaysWhatTheStoreMeasures keeps the watermark
// retention's godoc to what every store does: a close record is kept for the
// retention after it last changed, once its subject has no notifications left,
// not for the retention after the subject's last notification is deleted.
// Documentation is not context-sensitive, so the table has no ctx field.
func TestWatermarkRetentionGodocSaysWhatTheStoreMeasures(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		file        string
		declaration string
		assert      func(t *testing.T, doc string)
	}

	measuredFromTheLastChange := func(t *testing.T, doc string) {
		t.Helper()

		assert.NotContains(t, doc, "outlives its last notification",
			"no store keeps a close record for the retention after its subject's last notification")
		assert.Contains(t, doc, "last changed", "the retention is measured from the record's last change")
	}

	cases := []testCase{
		{
			name: "the default", file: "pruner.go", declaration: "DefaultWatermarkRetention =",
			assert: measuredFromTheLastChange,
		},
		{
			name: "the option", file: "pruner.go", declaration: "func " + funcName(ntfy.WithWatermarkRetention) + "(",
			assert: measuredFromTheLastChange,
		},
		{
			name: "the store's request", file: "store.go", declaration: "WatermarkRetention time.Duration",
			assert: measuredFromTheLastChange,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, docComment(t, tc.file, tc.declaration))
		})
	}
}
```

- [ ] **Step 2: Watch it fail on the wording**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestWatermarkRetentionGodocSaysWhatTheStoreMeasures' -count=1 .`

Expected, as observed on 2026-09-28, abridged:

```
--- FAIL: TestWatermarkRetentionGodocSaysWhatTheStoreMeasures (0.00s)
    --- FAIL: TestWatermarkRetentionGodocSaysWhatTheStoreMeasures/the_option (0.00s)
        Error:      "WithWatermarkRetention replaces [DefaultWatermarkRetention]. A source redelivered after its subject's close record expired can create notifications again. It must be positive." does not contain "last changed"
    --- FAIL: TestWatermarkRetentionGodocSaysWhatTheStoreMeasures/the_store's_request (0.00s)
        Error:      "WatermarkRetention is how long a subject's close record outlives its last notification. Zero means close records never expire." should not contain "outlives its last notification"
        Error:      "WatermarkRetention is how long … never expire." does not contain "last changed"
    --- FAIL: TestWatermarkRetentionGodocSaysWhatTheStoreMeasures/the_default (0.00s)
        Error:      "DefaultWatermarkRetention is how long a subject's close record outlives its last notification. The relay's default … hundredfold." should not contain "outlives its last notification"
        Error:      "DefaultWatermarkRetention is how long … hundredfold." does not contain "last changed"
FAIL
```

A failure on `%s declares %q` or `%s documents %q` means the declaration string does not match the source, not that the godoc is wrong. Fix the declaration string, not the assertion.

- [ ] **Step 3: Rewrite the three doc comments (tasks 3.2)**

In `pruner.go`, in the `const` block:

```go
	// DefaultWatermarkRetention is how long a subject's close record is kept
	// after it last changed, which is at the subject's latest close. A pass
	// deletes the record only once the subject has no notifications left, and
	// may do so in the same pass that deletes the last of them. The relay's
	// default retries finish within about half an hour; this covers a host
	// that raised them a hundredfold.
	DefaultWatermarkRetention = 7 * 24 * time.Hour
```

In `pruner.go`:

```go
// WithWatermarkRetention replaces [DefaultWatermarkRetention]: how long a
// subject's close record is kept after it last changed, once the subject has
// no notifications left. A source redelivered after its subject's close record
// expired can create notifications again. It must be positive.
func WithWatermarkRetention(retention time.Duration) PruneOption {
```

In `store.go`, in `PruneRequest`:

```go
	// WatermarkRetention is how long a subject's close record is kept after it
	// last changed. A store deletes a record older than that only once its
	// subject has no notifications left. Zero means close records never expire.
	WatermarkRetention time.Duration
```

In `docs/notifications.md`, the retention table row:

```markdown
| Close records of empty subjects | 7 days after the record last changed | `WithWatermarkRetention` |
```

- [ ] **Step 4: Run the docs tests**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestWatermarkRetentionGodocSaysWhatTheStoreMeasures|TestTheDocumentMatchesTheImplementation' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy`. The "Retention" section still contains the needle `7 days`.

- [ ] **Step 5: Pin the measured behaviour in the suite (tasks 3.3)**

In `ntfytest/suite.go` `runRetention`, directly before `parallel(t, "close records never expire without a retention", …`, add:

```go
	parallel(t, "a close record can expire with its subject's last notification", func(t *testing.T) {
		e := newEnv(t, factory)
		closed := now.Add(-days(95))

		e.insert(false, e.note("alice", "event-2", "task-1", "offer", 2, closed))
		e.close(ntfy.CloseRequest{Subject: "task-1", Version: 5, Reason: "done"}, closed)

		result := e.prune(ntfy.PruneRequest{Now: now, MaxAge: days(90), WatermarkRetention: days(7)})

		assert.Equal(t, int64(1), result.DeletedForAge, "the pass deletes the subject's last notification")
		assert.Equal(t, int64(1), result.WatermarksDeleted,
			"retention counts from the close, so the record goes in the same pass")

		late := e.insert(false, e.note("alice", "event-1", "task-1", "offer", 1, now))
		assert.Len(t, late.Created, 1, "with the record gone, a late source is created")
	})

```

- [ ] **Step 6: Run it, then show it can fail and revert the mutation**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/retention/a_close_record_can' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy`

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/retention/a_close_record_can' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy/sqlstore`

Temporarily make `MemoryStore.pruneWatermarks` in `memory.go` skip every record, by adding `continue` as the first statement of its loop:

```go
	for key, mark := range s.watermarks {
		continue

		if _, occupied := s.subjects[key.subject]; occupied || !mark.updatedAt.Before(cutoff) {
```

Run the memory command again. Expected, as observed:

```
	Error:      	Not equal:
	Messages:   	retention counts from the close, so the record goes in the same pass
	Error:      	"[]" should have 1 item(s), but has 0
	Messages:   	with the record gone, a late source is created
FAIL
```

Remove the mutation, then run `git diff memory.go` and expect no output.

- [ ] **Step 7: Lint**

Run: `golangci-lint run ./...`
Expected: `0 issues.` A depguard finding means `go/*` crept into `docs_test.go`; see Global Constraints.

- [ ] **Step 8: Commit**

```bash
git add docs_test.go pruner.go store.go docs/notifications.md ntfytest/suite.go
git commit -m "$(cat <<'EOF'
Document watermark retention as counted from the record's last change

The godoc said a close record outlives its subject's last notification,
but every store measures from the record's updated_at, as the schema
document and the spec say. The godoc now says so, a docs test holds it
to that, and the conformance suite pins a record expiring in the same
pass as its subject's last notification.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01RJafobH6gZay9kpSiSAS6A
EOF
)"
```

Then tick 3.1, 3.2 and 3.3 in `tasks.md`.

---

### Task 3: Verify and hand off

**Files:** none changed, unless the code review asks for changes.

**Interfaces:**
- Consumes: everything from Tasks 1 and 2.
- Produces: nothing new.

- [ ] **Step 1: Run the full gate (tasks 4.1)**

Run: `make all`
Expected: each module prints `==> lint …`, `==> split-check …` and `==> test …`, and the target exits 0.

- [ ] **Step 2: Run the store matrix (tasks 4.2)**

Run, with Docker running: `make store-matrix`
Expected: seven `==> sqlstore… TestStoreOn…` headers, each followed by `ok`. That covers database/sql on PostgreSQL, MySQL and SQLite, pgx on PostgreSQL, and GORM on PostgreSQL, MySQL and SQLite. Every one runs the new `successors` and `retention` cases.

- [ ] **Step 3: Review (tasks 4.3)**

Run `/code-review` over the branch's diff against `main`, and answer each finding under `superpowers:receiving-code-review`. If a finding changes the work, add it to `tasks.md` as a new numbered item and to this plan as a new task, in the same turn. Then tick 4.1, 4.2 and 4.3.

---

## Self-Review

- **Spec coverage:**
  - `notification-inbox`, "A close can tell each recipient it closed what happened, atomically":
    - the rule and "A retried close of every kind…" / "…naming the successor's kind…": Task 1, Steps 1-9;
    - "A later close still closes an earlier close's successors": Task 1, Steps 4-5;
    - the unchanged scenarios stay covered by the existing `runSuccessors` cases.
  - `notification-retention`, "Close records outlive late redelivery, then expire":
    - the measurement sentence: Task 2, Steps 1-4;
    - "A close record can expire with its subject's last notification": Task 2, Steps 5-6.
- **Placeholders:** none. Every code step carries the code, and every run step carries the command and its observed output.
- **Names:**
  - `retriedClose`, `retryHeldToNothing`, `docComment` and `TestWatermarkRetentionGodocSaysWhatTheStoreMeasures` are used consistently.
  - The case names in the `-run` patterns match the `parallel` names with spaces as underscores.
