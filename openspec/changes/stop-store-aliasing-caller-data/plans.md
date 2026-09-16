# Store Memory Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every ntfy store keep and return notifications that share no memory with their caller, and hold every store — including a host's own — to that invariant through the shared conformance suite.

**Architecture:** A new conformance group, `runIsolation`, mutates a link map or payload a caller supplied or was returned, then asserts nothing else changed. It runs against `MemoryStore` and against `sqlstore` on every driver and dialect, so the two stores can no longer disagree silently. Two production clones make today's code satisfy it: `CloseRequest.SuccessorInsertions` stops handing every insertion the caller's own map, and `sqlstore` stops returning the value it was handed. The read paths that build notifications from database rows are already independent by construction and are covered by the suite without gaining clones.

**Tech Stack:** Go 1.26 (standard library only in the core module), `stretchr/testify` for assertions, `uber-go/mock` for doubles, `testcontainers-go` via `sqlkittest` for the SQL matrix, `golangci-lint` v2, `openspec` for the change artifacts.

**Spec:** `openspec/changes/stop-store-aliasing-caller-data/` — read `proposal.md` (why), `design.md` (the path-by-path audit and D1–D4), and `specs/notification-inbox/spec.md` (the five scenarios this plan must satisfy).

## Global Constraints

- Go 1.26 everywhere; run every Go command with `GOTOOLCHAIN=go1.26.8` (the `Makefile` exports it; a bare `go` on this machine resolves to 1.27 and breaks the linter).
- The core module `github.com/kartaladev/ntfy` imports **nothing but the standard library** in production code; `stretchr/testify`, `go.uber.org/mock` and `go.uber.org/goleak` are test-only and already allowed by `.golangci.yml` depguard.
- Each satellite module may import only `ntfy`, `sqlkit` and its own client library; `make split-check` is authoritative.
- Table-driven tests follow the project `table-test` skill: an `assert` closure per case (never `want`/`wantErr` fields), `t.Context()` over `context.Background()`, `require` only for preconditions that must halt the case.
- Test doubles come from the `use-mockgen` skill (`//go:generate mockgen -source=… -typed`); never hand-rolled fakes.
- Heavy external services come from the `use-testcontainers` skill via `sqlkittest.RunTestPostgres`/`RunTestMySQL`; `sqlkittest.RunTestSQLite` is a temp file and needs no Docker.
- `.claude/rules/prove-errors-with-tests.md`: every claimed defect is proved by a test that fails first, for the stated reason, and the failing output is recorded. A test that has never been seen to fail has not been shown to test anything.
- `.claude/rules/golang-tdd.md`: red → green → refactor, and **the test and the code that satisfies it land in the same commit**. The discipline is in the order they were written, not in splitting the commit.
- `.claude/rules/library-design.md`: this change adds a contract term with no override; `design.md` D4 states why, and the plan must not add a configuration knob for it.
- `make all` (lint, split-check, test) and `make store-matrix` must pass before the change is done.
- Commit messages are imperative sentence case with no `feat:`/`fix:` prefix and no attribution lines, matching `git log` (`Spin notify out of hmntsk as ntfy`, `Archive spin-out-from-hmntsk and adopt its specs`).

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `ntfytest/isolation.go` | `ntfy/ntfytest` | **Create.** The `runIsolation` conformance group and its helpers. One file so the invariant's cases are read together, matching how `email.go` holds the email group. |
| `ntfytest/suite.go:29-35` | `ntfy/ntfytest` | **Modify.** Register `isolation` in `Run`. **Merge note:** the change `email-delivery-robustness` also extends `ntfytest.Run`'s registration block; these edits land on adjacent lines and will conflict textually, not semantically. |
| `ntfytest/doc.go` | `ntfy/ntfytest` | **Modify.** Name isolation among the properties the suite asserts, so a host writing a store meets it in the package doc. |
| `store.go:155-186` | `ntfy` | **Modify.** `SuccessorInsertions` clones each insertion, and its godoc states the guarantee. |
| `store.go:19-51` | `ntfy` | **Modify.** The `Store` port's godoc gains the isolation obligation and names `Notification.Clone` as the mechanism. |
| `sqlstore/insert.go:79-105` | `ntfy/sqlstore` | **Modify.** Clone on accept, so neither `InsertResult.Created` nor `CloseResult.Successors` shares memory with the values passed to `Insert`. |
| `service_publish_test.go:90-260` | `ntfy` | **Modify.** One row added to the existing `TestServicePublish` table, pinning the clone at `service.go:150`. The `table-test` skill forbids a second `TestXxx` calling `Publish`. |

Untouched on purpose (`design.md` D3): `sqlstore/sql.go:251` and the rest of the decode path, `memory.go:258`, `memory.go:297`, `memory_email.go:59`. They are already independent by construction; the suite covers them, and adding clones would cost an allocation per notification on the listing path to defend against sharing that cannot occur.

---

### Task 1: The isolation group and the close path

**Files:**
- Create: `ntfytest/isolation.go`
- Modify: `ntfytest/suite.go:29-35` (register the group)
- Modify: `store.go:155-186` (`SuccessorInsertions`)
- Test: `ntfytest/isolation.go` is itself the test; it runs from `memory_test.go:10` and `sqlstore/harness_test.go:96`

**Interfaces:**
- Consumes: `ntfytest.Factory = func(t *testing.T) ntfy.Store`; the `env` helpers `newEnv(t, factory) *env`, `(*env).insert(coalesce bool, notifications ...ntfy.Notification) ntfy.InsertResult`, `(*env).close(req ntfy.CloseRequest, when time.Time) ntfy.CloseResult`, `(*env).get(recipient, id string) ntfy.Notification`, `(*env).note(recipient, source, subject, kind string, version int64, created time.Time) ntfy.Notification`, `parallel(t *testing.T, name string, fn func(t *testing.T))`, `at(n int) time.Time`.
- Produces: `func runIsolation(t *testing.T, factory Factory)` (package-private, called from `Run`); `func isolationContent() (map[string]string, json.RawMessage)`; `func isolationClose(links map[string]string, data json.RawMessage) ntfy.CloseRequest`. `CloseRequest.SuccessorInsertions(recipients []string, at time.Time, ids IDGenerator) ([]Insertion, error)` keeps its signature and gains a documented guarantee.

- [ ] **Step 1: Write the failing conformance cases**

Create `ntfytest/isolation.go`:

```go
package ntfytest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// isolationContent returns a link map and a payload, freshly allocated on every
// call, so that a case mutating what it supplied cannot reach another case.
func isolationContent() (map[string]string, json.RawMessage) {
	return map[string]string{"task": "/v1/tasks/task-1"}, json.RawMessage(`{"by":"carol"}`)
}

// isolationClose closes task-1's offers with a successor carrying links and a
// payload the caller still holds.
func isolationClose(links map[string]string, data json.RawMessage) ntfy.CloseRequest {
	return ntfy.CloseRequest{
		Subject: "task-1", Kinds: []string{"offer"}, Version: 5, Reason: "taken",
		Successor: &ntfy.Successor{
			SourceID: "event-5", Kind: "taken", Title: "Taken by carol", SubjectVersion: 5,
			Links: links, Data: data,
		},
	}
}

// runIsolation asserts that a store shares no memory with its caller: nothing a
// caller mutates after a call changes what the store holds or what it already
// returned, and no two notifications one call returns share a map or a payload.
//
// Every case mutates a value and then asserts that another is unchanged, rather
// than comparing pointers: the contract promises non-interference, not distinct
// object graphs. Each case also asserts that its mutation took effect on the
// caller's own copy, so a case cannot pass by mutating nothing.
func runIsolation(t *testing.T, factory Factory) {
	parallel(t, "mutating a close request does not change its successors", func(t *testing.T) {
		e := newEnv(t, factory)
		e.insert(false, e.note("alice", "event-1", "task-1", "offer", 1, at(0)))

		links, data := isolationContent()
		req := isolationClose(links, data)

		result := e.close(req, at(4))
		require.Len(t, result.Successors, 1)

		links["task"] = "/hijacked"
		data[2] = 'X'

		assert.Equal(t, "/v1/tasks/task-1", result.Successors[0].Links["task"],
			"the reported successor keeps the links the close carried")
		assert.JSONEq(t, `{"by":"carol"}`, string(result.Successors[0].Data))

		stored := e.get("alice", result.Successors[0].ID)
		assert.Equal(t, "/v1/tasks/task-1", stored.Links["task"])
		assert.JSONEq(t, `{"by":"carol"}`, string(stored.Data))

		assert.Equal(t, "/hijacked", links["task"], "the caller's own map did change")
		assert.JSONEq(t, `{"Xy":"carol"}`, string(data), "the caller's own payload did change")
	})

	parallel(t, "successors to different recipients are independent", func(t *testing.T) {
		e := newEnv(t, factory)
		e.insert(false,
			e.note("alice", "event-1", "task-1", "offer", 1, at(0)),
			e.note("bob", "event-2", "task-1", "offer", 1, at(0)))

		links, data := isolationContent()

		result := e.close(isolationClose(links, data), at(4))
		require.Len(t, result.Successors, 2)

		reported := map[string]ntfy.Notification{}
		for _, n := range result.Successors {
			reported[n.Recipient] = n
		}

		require.Contains(t, reported, "alice")
		require.Contains(t, reported, "bob")

		reported["alice"].Links["task"] = "/hijacked"
		reported["alice"].Data[2] = 'X'

		assert.Equal(t, "/v1/tasks/task-1", reported["bob"].Links["task"],
			"bob's successor keeps its own links")
		assert.JSONEq(t, `{"by":"carol"}`, string(reported["bob"].Data))
		assert.Equal(t, "/v1/tasks/task-1", e.get("bob", reported["bob"].ID).Links["task"])

		assert.Equal(t, "/hijacked", reported["alice"].Links["task"], "the mutated copy did change")
	})
}
```

- [ ] **Step 2: Register the group**

In `ntfytest/suite.go`, add one line to `Run` after the `successors` group:

```go
	t.Run("successors", func(t *testing.T) { runSuccessors(t, factory) })
	t.Run("isolation", func(t *testing.T) { runIsolation(t, factory) })
	t.Run("coalescing", func(t *testing.T) { runCoalescing(t, factory) })
```

- [ ] **Step 3: Run the cases against the memory store — they pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/isolation' -count=1 -v .`
Expected: PASS for both cases. `MemoryStore` clones on `put` and on `Created` (`memory.go:105-106`), so it already satisfies the invariant. This is the half of the divergence that hides the defect.

- [ ] **Step 4: Run the same cases against sqlstore on SQLite — they fail**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/isolation' -count=1 -v .`
Expected: FAIL on both cases, at the *reported successor*, not at the stored one — `sqlstore` serialises into binds inside the transaction, so the database is correct and only what it returns aliases. Expected output shape:

```
--- FAIL: TestStoreOnStdSQLSQLite/isolation/mutating_a_close_request_does_not_change_its_successors
    isolation.go:58:
        	Error:      	Not equal:
        	            	expected: "/v1/tasks/task-1"
        	            	actual  : "/hijacked"
        	Messages:   	the reported successor keeps the links the close carried
--- FAIL: TestStoreOnStdSQLSQLite/isolation/successors_to_different_recipients_are_independent
    isolation.go:88:
        	Error:      	Not equal:
        	            	expected: "/v1/tasks/task-1"
        	            	actual  : "/hijacked"
        	Messages:   	bob's successor keeps its own links
```

Record that output in the commit message body or the task notes — this is the proof the rule requires.

- [ ] **Step 5: Clone each successor insertion**

In `store.go`, inside `SuccessorInsertions`' loop, clone the notification exactly as `Service.Publish` does at `service.go:150`:

```go
		out = append(out, Insertion{
			// Cloned, so that the insertions share nothing with the request or
			// with each other: a store may keep what it is given.
			Notification: Notification{
				ID: id, Recipient: recipient, SourceID: successor.SourceID, Subject: r.Subject,
				SubjectVersion: successor.SubjectVersion, Kind: successor.Kind, State: StateActive,
				Title: successor.Title, Links: successor.Links, Data: successor.Data, CreatedAt: at,
			}.Clone(),
		})
```

- [ ] **Step 6: State the guarantee in the godoc**

Replace the last sentence of `SuccessorInsertions`' doc comment so an implementer learns what it hands them:

```go
// SuccessorInsertions stamps the request's successor for each recipient a close
// closed, skipping SuccessorSkip, in the order recipients are given. A store
// calls it inside its close transaction, once the recipients are known, and
// inserts the result through its ordinary insert path. It returns nothing when
// the request names no successor.
//
// Every insertion it returns shares no links or payload with the request or
// with the other insertions, so a store may keep what it is given. A store must
// not rely on that: see [Store].
```

- [ ] **Step 7: Run both stores — the cases pass everywhere**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/isolation' -count=1 . && cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/isolation' -count=1 .`
Expected: PASS, PASS.

- [ ] **Step 8: Verify the cases are not vacuous**

Temporarily revert Step 5 (drop `.Clone()`), re-run the SQLite command from Step 7, confirm both cases fail again, then restore the clone.
Expected: FAIL then, PASS after restoring.

- [ ] **Step 9: Commit**

```bash
git add ntfytest/isolation.go ntfytest/suite.go store.go
git commit -m "Keep a close's successors independent of its request"
```

---

### Task 2: sqlstore returns what it does not share

**Files:**
- Modify: `ntfytest/isolation.go` (two cases appended to `runIsolation`)
- Modify: `sqlstore/insert.go:79-105`
- Test: `ntfytest/isolation.go`, run from `memory_test.go:10` and `sqlstore/harness_test.go:96`

**Interfaces:**
- Consumes: `runIsolation` and the helpers from Task 1; `(*env).insert`, `(*env).get`.
- Produces: no exported change. `sqlstore.Store.Insert(ctx context.Context, subject string, insertions []ntfy.Insertion) (ntfy.InsertResult, error)` keeps its signature; `InsertResult.Created` and, through `Close`, `CloseResult.Successors` become independent of the caller's values.

- [ ] **Step 1: Write the failing cases**

Append to `runIsolation` in `ntfytest/isolation.go`:

```go
	parallel(t, "mutating an insertion does not change what a store keeps or reported", func(t *testing.T) {
		e := newEnv(t, factory)

		n := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
		n.Links, n.Data = isolationContent()

		result := e.insert(false, n)
		require.Len(t, result.Created, 1)

		n.Links["task"] = "/hijacked"
		n.Data[2] = 'X'

		assert.Equal(t, "/v1/tasks/task-1", result.Created[0].Links["task"],
			"the reported notification keeps the links it was inserted with")
		assert.JSONEq(t, `{"by":"carol"}`, string(result.Created[0].Data))

		stored := e.get("alice", n.ID)
		assert.Equal(t, "/v1/tasks/task-1", stored.Links["task"])
		assert.JSONEq(t, `{"by":"carol"}`, string(stored.Data))

		assert.Equal(t, "/hijacked", n.Links["task"], "the caller's own map did change")
	})

	parallel(t, "insertions sharing one map are reported independently", func(t *testing.T) {
		e := newEnv(t, factory)

		links, data := isolationContent()

		alice := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
		bob := e.note("bob", "event-2", "task-1", "offer", 1, at(0))
		alice.Links, alice.Data = links, data
		bob.Links, bob.Data = links, data

		result := e.insert(false, alice, bob)
		require.Len(t, result.Created, 2)

		result.Created[0].Links["task"] = "/hijacked"
		result.Created[0].Data[2] = 'X'

		assert.Equal(t, "/v1/tasks/task-1", result.Created[1].Links["task"],
			"the second notification keeps its own links")
		assert.JSONEq(t, `{"by":"carol"}`, string(result.Created[1].Data))
		assert.Equal(t, "/hijacked", result.Created[0].Links["task"], "the mutated copy did change")
	})
```

- [ ] **Step 2: Run against the memory store — they pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/isolation' -count=1 -v .`
Expected: PASS. `memory.go:105-106` clones once into storage and once into `Created`, so each returned notification already has its own map.

- [ ] **Step 3: Run against sqlstore on SQLite — they fail**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/isolation' -count=1 -v .`
Expected: FAIL on both new cases. `sqlstore/insert.go:103` appends the value it was handed and `:115` returns it, so `result.Created[0].Links` *is* the caller's map:

```
--- FAIL: TestStoreOnStdSQLSQLite/isolation/mutating_an_insertion_does_not_change_what_a_store_keeps_or_reported
    isolation.go:112:
        	Error:      	Not equal:
        	            	expected: "/v1/tasks/task-1"
        	            	actual  : "/hijacked"
        	Messages:   	the reported notification keeps the links it was inserted with
--- FAIL: TestStoreOnStdSQLSQLite/isolation/insertions_sharing_one_map_are_reported_independently
    isolation.go:136:
        	Error:      	Not equal:
        	            	expected: "/v1/tasks/task-1"
        	            	actual  : "/hijacked"
        	Messages:   	the second notification keeps its own links
```

- [ ] **Step 4: Clone on accept**

In `sqlstore/insert.go`, inside `insert`'s decision loop, clone the accepted notification:

```go
		// Cloned, so that what this store reports — and what a close reports as
		// its successors — shares no links or payload with the caller's
		// insertions or with each other. The stored copy needs no clone: the
		// values are serialised into binds inside this transaction.
		accepted = append(accepted, n.Clone())
```

Leave `result.Created = append(result.Created, n)` at `:115` as it is: `n` now ranges over `accepted`, whose entries are already one clone each, so `Created` is independent of the caller and of itself. `tasks.md` 3.1 names both `:103` and `:115`; only `:103` is needed, because unlike `memory.go` — which must separate the stored copy from the returned one — sqlstore's stored copy is the database.

- [ ] **Step 5: Run both stores — the cases pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/isolation' -count=1 . && cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/isolation' -count=1 .`
Expected: PASS, PASS.

- [ ] **Step 6: Run the whole SQLite conformance suite for regressions**

Run: `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite' -count=1 .`
Expected: PASS — the clone must not disturb insert results, coalescing, watermarks or retention.

- [ ] **Step 7: Commit**

```bash
git add ntfytest/isolation.go sqlstore/insert.go
git commit -m "Return notifications sqlstore shares with nobody"
```

---

### Task 3: Pin the paths that are already correct, and state the obligation

**Files:**
- Modify: `ntfytest/isolation.go` (two read-path cases)
- Modify: `service_publish_test.go:90-260` (one row in the existing table)
- Modify: `store.go:19-51` (the `Store` port godoc)
- Modify: `ntfytest/doc.go`
- Test: `ntfytest/isolation.go`, `service_publish_test.go`

**Interfaces:**
- Consumes: `runIsolation` and its helpers; `(*env).list(q ntfy.ListQuery) ntfy.Page`; from `service_publish_test.go`, `mocked(t) (*ntfy.Service, *ntfy.MockStore, *ntfy.MockBroadcaster, *signalErrors)` and `createAll(insertions []ntfy.Insertion) ntfy.InsertResult`.
- Produces: no new symbols. The `Store` interface godoc gains the isolation obligation; `ntfytest`'s package doc names isolation among the asserted properties.

- [ ] **Step 1: Write the read-path cases**

Append to `runIsolation` in `ntfytest/isolation.go`:

```go
	parallel(t, "a notification read twice is independent between reads", func(t *testing.T) {
		e := newEnv(t, factory)

		n := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
		n.Links, n.Data = isolationContent()
		e.insert(false, n)

		first := e.get("alice", n.ID)
		first.Links["task"] = "/hijacked"
		first.Data[2] = 'X'

		second := e.get("alice", n.ID)
		assert.Equal(t, "/v1/tasks/task-1", second.Links["task"], "a later read carries what was stored")
		assert.JSONEq(t, `{"by":"carol"}`, string(second.Data))
		assert.Equal(t, "/hijacked", first.Links["task"], "the mutated copy did change")
	})

	parallel(t, "a listed notification is independent of a read", func(t *testing.T) {
		e := newEnv(t, factory)

		n := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
		n.Links, n.Data = isolationContent()
		e.insert(false, n)

		page := e.list(ntfy.ListQuery{Recipient: "alice"})
		require.Len(t, page.Notifications, 1)

		page.Notifications[0].Links["task"] = "/hijacked"
		page.Notifications[0].Data[2] = 'X'

		read := e.get("alice", n.ID)
		assert.Equal(t, "/v1/tasks/task-1", read.Links["task"], "a read carries what was stored")
		assert.JSONEq(t, `{"by":"carol"}`, string(read.Data))
		assert.Equal(t, "/hijacked", page.Notifications[0].Links["task"], "the mutated copy did change")
	})
```

- [ ] **Step 2: Run them on both stores**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/isolation' -count=1 . && cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/isolation' -count=1 .`
Expected: PASS on both. These pin behaviour that is already correct — `memory.go:258,297` clone and `sqlstore` decodes each row afresh (`sqlstore/sql.go:251`) — rather than proving a defect, so no red step is expected here.

- [ ] **Step 3: Prove the read-path cases are not vacuous**

Temporarily change `memory.go:258` from `return n.Clone(), nil` to `return n, nil`, re-run the memory command from Step 2, confirm "a notification read twice is independent between reads" fails, then restore it.
Expected: FAIL then, PASS after restoring.

- [ ] **Step 4: Pin the publish path in the service table**

In `service_publish_test.go`, add `"encoding/json"` to the imports, declare the case's values just above `cases := []testCase{`, and add one row. The `table-test` skill forbids a second `TestXxx` calling `Publish`, so this is a row, not a function:

```go
	// publishedLinks and publishedData belong to the isolation case below: it
	// mutates them after Publish returns, so the insertion the store was handed
	// must already be a copy.
	publishedLinks := map[string]string{"task": "/v1/tasks/task-1"}
	publishedData := json.RawMessage(`{"by":"carol"}`)
	published := new([]ntfy.Insertion)
```

```go
		{
			name: "a draft mutated after publishing does not change what the store was handed",
			drafts: []ntfy.Draft{{
				Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer",
				Links: publishedLinks, Data: publishedData,
			}},
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().Insert(gomock.Any(), "task-1", gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, insertions []ntfy.Insertion) (ntfy.InsertResult, error) {
						*published = insertions

						return createAll(insertions), nil
					})
				broadcaster.EXPECT().Broadcast(gomock.Any(), gomock.Any()).Return(nil)
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, _ []error) {
				require.NoError(t, err)
				require.Len(t, result.Created, 1)

				publishedLinks["task"] = "/hijacked"
				publishedData[2] = 'X'

				require.Len(t, *published, 1)
				assert.Equal(t, "/v1/tasks/task-1", (*published)[0].Notification.Links["task"],
					"the store was handed a copy of the draft's links")
				assert.JSONEq(t, `{"by":"carol"}`, string((*published)[0].Notification.Data))
				assert.Equal(t, "/hijacked", publishedLinks["task"], "the caller's own map did change")
			},
		},
```

- [ ] **Step 5: Run the service table**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestServicePublish' -count=1 -v .`
Expected: PASS, including the new row.

- [ ] **Step 6: Prove the publish row is not vacuous**

Temporarily remove `.Clone()` from `service.go:150`, re-run Step 5, confirm the new row fails with `expected: "/v1/tasks/task-1" actual: "/hijacked"`, then restore it.
Expected: FAIL then, PASS after restoring.

- [ ] **Step 7: State the obligation on the Store port**

In `store.go`, extend the `Store` interface's doc comment:

```go
// Store is where notifications live. The default is [NewMemoryStore];
// ntfy/sqlstore stores them in PostgreSQL, MySQL or SQLite; a host may supply
// its own, provided it passes the ntfytest conformance suite.
//
// Every method is its own transaction, and never joins a transaction the caller
// holds for other data. Callers are the [Service] and the [Pruner]: requests
// reach a store already validated, and notifications reach Insert already
// stamped.
//
// A store shares no memory with its caller. It copies what it retains, so that
// a caller mutating a link map or payload afterwards cannot change what is
// stored, and what it returns shares nothing with its own state, with the
// caller's request, or with another notification returned by the same call.
// [Notification.Clone] is the mechanism, and ntfytest asserts the invariant on
// every store. A store whose results are decoded afresh from its backing
// storage satisfies it without copying.
```

- [ ] **Step 8: Name isolation in the suite's package doc**

In `ntfytest/doc.go`, add a sentence so a host writing a store learns what the suite holds them to:

```go
// Package ntfytest is the conformance suite every [ntfy.Store] must pass.
//
// One suite, run against the in-memory store and against ntfy/sqlstore on
// every supported driver and dialect, is what makes "identical on every store"
// a fact rather than an intention. A host that writes its own store runs the
// same suite against it.
//
// Beyond behaviour, the suite asserts memory isolation: a store copies what it
// retains and returns nothing it shares with its caller, with its own state, or
// with another notification from the same call.
package ntfytest
```

- [ ] **Step 9: Check the doc renders**

Run: `GOTOOLCHAIN=go1.26.8 go doc ./... 2>/dev/null | head -5 && GOTOOLCHAIN=go1.26.8 go doc ntfy.Store`
Expected: the `Store` doc shows the isolation paragraph; an implementer learns the obligation without reading the suite.

- [ ] **Step 10: Confirm no clone crept into the paths that need none**

Run: `rg -n 'Clone\(\)' memory.go memory_email.go store.go service.go sqlstore/`
Expected: exactly the call sites in `design.md`'s table plus the two added here — `service.go:150`, `store.go` (successor insertions), `memory.go:105`, `:106`, `:258`, `:297`, `memory_email.go:59`, `sqlstore/insert.go` (accept). No clone in `sqlstore/sql.go`, `sqlstore/read.go` or `sqlstore/close.go`.

- [ ] **Step 11: Commit**

```bash
git add ntfytest/isolation.go ntfytest/doc.go store.go service_publish_test.go
git commit -m "Hold every store to memory isolation and say so in the port"
```

---

### Task 4: Verify across every module and dialect

**Files:**
- Modify: none. This task is verification spanning Tasks 1–3 and the whole store matrix.

**Interfaces:**
- Consumes: everything Tasks 1–3 produced.
- Produces: evidence that the invariant holds on all seven driver-and-dialect combinations.

- [ ] **Step 1: Lint, split-check and test every module**

Run: `make all`
Expected: `0 issues.` for each of the six ntfy modules, split-check silent, tests pass.

- [ ] **Step 2: Run the race detector where the suite is concurrent**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 . && cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -race -run 'TestStoreOnStdSQLSQLite' -count=1 .`
Expected: PASS with no `DATA RACE` — the isolation cases run under `parallel`, and a clone that shared a map would show up here too.

- [ ] **Step 3: Run the full store matrix (needs Docker)**

Run: `make store-matrix`
Expected: PASS for all seven entries — `TestStoreOnStdSQLPostgres`, `TestStoreOnStdSQLMySQL`, `TestStoreOnStdSQLSQLite`, `TestStoreOnPgxPostgres`, and the three GORM entry points in `sqlstore/internal/gormtest`. This is the spec scenario "Every store is held to this".

- [ ] **Step 4: Re-prove the defect against the finished tree**

Revert both clones (`store.go`'s `SuccessorInsertions` and `sqlstore/insert.go`'s accept), run `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/isolation' -count=1 .`, confirm four cases fail, then restore both with `git checkout -- store.go sqlstore/insert.go`.
Expected: 4 FAIL, then PASS after restoring. This is the rule's requirement that the tests still prove the defect rather than passing vacuously.

- [ ] **Step 5: Validate the change artifacts**

Run: `openspec validate "stop-store-aliasing-caller-data" --strict`
Expected: `Change 'stop-store-aliasing-caller-data' is valid`, and every scenario in `specs/notification-inbox/spec.md` maps to a case: publish → `service_publish_test.go` row; close request → Task 1 case 1; different recipients → Task 1 case 2; returned notification independent of the store → Task 3 cases; every store → Step 3.

---

## Plan self-review

**Spec coverage.** All five scenarios in `specs/notification-inbox/spec.md` map to a step: "Mutating a published payload does not change what is stored" → Task 3 Step 4; "Mutating a close request does not change its successors" → Task 1 Step 1 (first case); "Successors to different recipients are independent" → Task 1 Step 1 (second case); "A returned notification is independent of the store" → Task 3 Step 1 (both cases); "Every store is held to this" → Task 4 Step 3.

**Two corrections to `tasks.md`, made deliberately:**

1. `tasks.md` 1.2 expects the two-recipient case to "fail on both stores today". It fails on **sqlstore only**. `MemoryStore.insert` calls `n.Clone()` once per notification for storage and once for `InsertResult.Created` (`memory.go:105-106`), so each recipient's successor already has its own map even though `SuccessorInsertions` handed both the same one. The plan states the true expectation, because a step that predicts the wrong failure teaches the implementer to distrust the plan.
2. `tasks.md` 3.1 names two clone sites in `sqlstore` (`insert.go:103` and `:115`). Only `:103` is needed: `result.Created` ranges over `accepted`, which after the accept-time clone already holds one independent copy per notification, and sqlstore's stored copy is the database rather than an in-memory value. `memory.go` needs both clones because its stored copy and its returned copy are two live values. Task 2 Step 4 records this.

**Placeholder scan.** No "TBD", no "add error handling", no "similar to Task N"; every code step carries the Go it needs, every command carries its expected output, and the failure text in Task 1 Step 4 and Task 2 Step 3 is the real assertion shape `testify` prints.

**Type consistency.** `runIsolation`, `isolationContent` and `isolationClose` are defined once in Task 1 and used unchanged in Tasks 2 and 3. The `env` helpers are used with the signatures read from `ntfytest/suite.go:60-175`. `createAll`, `mocked` and the `testCase` field names in Task 3 Step 4 match `service_publish_test.go:52-104`. `Notification.Clone` is the only copying mechanism named anywhere in the plan.
