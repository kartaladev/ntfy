# WebSocket Write Authorization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Marking notifications read over a WebSocket connection acts on the acting user, never on the recipient the connection follows.

**Architecture:** `websocket.Handler.ServeHTTP` already knows the acting user but passes only the connection's recipient down to `serve` → `read` → `answer`, where it becomes the subject of `MarkRead`/`MarkAllRead`. The fix threads the actor down that same chain, leaves the recipient as the subscription's subject for signal delivery, and refuses a mark request outright when the two differ. No exported signature changes; the refusal reuses the contract's existing forbidden code.

**Tech Stack:** Go 1.26 (standard library), `github.com/coder/websocket` (the `websocket` module's only third-party runtime dependency), `stretchr/testify` and `go.uber.org/goleak` in tests.

**Spec:** `openspec/changes/fix-websocket-write-authorization/` — `proposal.md` (why and scope), `design.md` (D1–D5, the decisions this plan implements), `specs/notification-realtime/spec.md` (the two modified requirements and their scenarios), `tasks.md` (the task skeleton this plan expands).

## Global Constraints

- Go 1.26 everywhere; the project pins `GOTOOLCHAIN=go1.26.8` and `.envrc` exports it. Prefix any direct `go`/`golangci-lint` command with `GOTOOLCHAIN=go1.26.8` if the shell has not loaded `.envrc`, or go through `make`.
- The core module (`github.com/kartaladev/ntfy`, repo root) imports the standard library only. The `websocket` module may import only `github.com/kartaladev/ntfy` and `github.com/coder/websocket` (plus testify/goleak in tests). Enforced by `depguard` in `.golangci.yml` and authoritatively by `make split-check`.
- Table-driven tests follow the project `table-test` skill: an `assert`/`act` closure per case, never `want`/`wantErr` struct fields; `t.Context()` rather than `context.Background()`; `t.Parallel()` on the parent and each subtest.
- Test doubles, if any are needed, come from the `use-mockgen` skill. This change needs none — the existing test server in `websocket/helpers_test.go` is the fixture.
- `make all` (lint, split-check, test over every module) must pass before the change is done.
- `.claude/rules/prove-errors-with-tests.md`: every claimed defect is proved by a test that has been **seen to fail** for the stated reason before any fix. A test that has never failed has not been shown to test anything.
- `.claude/rules/library-design.md`: every behaviour decision states its default and how a consumer overrides it, or why it has none. Here: writes act on the acting user (default), refused on a followed connection (default), **no override** — `design.md` D3 records why.
- `.claude/rules/golang-tdd.md`: red → green → refactor, and prefer commits where the test and the code that satisfies it land together.
- Commit messages: imperative sentence case, no `feat:`/`fix:` prefixes, matching `git log` ("Archive spin-out-from-hmntsk and adopt its specs"). No attribution or trailer lines.

## File Structure

| File | Create/Modify | Responsibility |
| --- | --- | --- |
| `websocket/authority_test.go` | Create | Both authority tests: the escalation (`TestMarkOnFollowedConnection`) and the cross-transport guard (`TestTransportsGrantTheSameAuthority`), plus the HTTP mark helper they share. Kept out of `readloop_test.go` because that file covers the protocol's happy path, while this file exists to pin *who may write*. |
| `websocket/handler.go` | Modify `:260`, `:286`, `:295`, `:364`, `:372`, `:403-435`, `:437-451` | Thread the actor through `serve`/`read`/`answer`; refuse a mark on a followed connection; map the refusal to the forbidden code. |
| `websocket/helpers_test.go` | Modify `:36-41`, `:61` | `serverConfig` gains an `sse` field so a test can give both transports the same subscription policy. |
| `authorize.go` | Modify `:8-19`, `:31-34`, `:36-41` | Godoc: a subscription policy grants following only. |
| `websocket/doc.go` | Modify `:9-10` | Package comment: marking read acts on the acting user. |
| `docs/realtime-operations.md` | Modify `:105-108`, `:116`, after `:117` | Transport comparison, the forbidden reply row, and the stated limit with the migration note. |

---

### Task 1: Refuse a mark request on a followed connection

Implements spec scenarios "Following does not grant changing" and "Marking read on a followed connection is refused"; `design.md` D1, D2, D4 (the `answer` comment).

**Files:**
- Create: `websocket/authority_test.go`
- Modify: `websocket/handler.go:260` (call site), `:286-296` (`serve`), `:364-377` (`read`), `:403-435` (`answer`), `:437-451` (`errorReply`)
- Test: `websocket/authority_test.go`

**Interfaces:**
- Consumes (existing test fixtures in package `websocket_test`, reuse by name — do not write new ones): `startServer(t *testing.T, cfg serverConfig) *server`; `serverConfig{stopped bool; hub []ntfy.HubOption; ws []websocket.Option; service []ntfy.Option}`; `(*server).dial(t *testing.T, req dialRequest) (dialed, error)`; `dialRequest{actor, recipient, origin string; onPing func() bool}`; `(*server).publish(t *testing.T, recipient, source string)`; `(*server).idOf(t *testing.T, recipient string) string`; `send(t *testing.T, conn *cws.Conn, message string)`; `readRaw(t *testing.T, conn *cws.Conn) []byte`; `readFrame(t *testing.T, conn *cws.Conn) map[string]any`.
- Produces (Task 2 relies on these exact names): `func (h *Handler) serve(requestCtx context.Context, conn *cws.Conn, subscription *ntfy.Subscription, actor, recipient string)`; `func (h *Handler) read(ctx context.Context, conn *cws.Conn, actor, recipient string, replies chan<- any)`; `func (h *Handler) answer(ctx context.Context, actor, recipient string, data []byte) any`; `var errFollowedConnection error`; `errorReply` answering `code: "forbidden"` for any error matching `ntfy.ErrUnauthorized`.

- [ ] **Step 1: Write the failing escalation test**

Create `websocket/authority_test.go`:

```go
package websocket_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/websocket"
)

// TestMarkOnFollowedConnection pins the rule that a subscription policy grants
// following only: a connection opened for another recipient may read that
// recipient's signals and may change nothing of theirs.
func TestMarkOnFollowedConnection(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name string
		// message is the mark request alice sends over her connection to bob.
		message func(t *testing.T, s *server) string
	}

	cases := []testCase{
		{
			name:    "mark-all-read",
			message: func(*testing.T, *server) string { return `{"type":"mark-all-read","ref":"r2"}` },
		},
		{
			name: "mark-read naming the followed recipient's notification",
			message: func(t *testing.T, s *server) string {
				return `{"type":"mark-read","ref":"r2","ids":["` + s.idOf(t, "bob") + `"]}`
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := startServer(t, serverConfig{
				ws: []websocket.Option{websocket.WithSubscriptionAuthorizer(ntfy.AllowAll)},
			})

			s.publish(t, "bob", "event-1")
			s.publish(t, "alice", "event-2")

			d, err := s.dial(t, dialRequest{actor: "alice", recipient: "bob"})
			require.NoError(t, err)

			send(t, d.conn, tc.message(t, s))

			assert.JSONEq(t,
				`{"type":"error","ref":"r2","code":"forbidden",`+
					`"message":"ntfy: unauthorized: a connection following another recipient may not mark notifications read"}`,
				string(readRaw(t, d.conn)))

			bob, err := s.svc.CountActive(t.Context(), "bob")
			require.NoError(t, err)
			assert.EqualValues(t, 1, bob, "the followed recipient's notifications are unchanged")

			alice, err := s.svc.CountActive(t.Context(), "alice")
			require.NoError(t, err)
			assert.EqualValues(t, 1, alice, "the acting user's own notifications are not marked instead")

			// The subscription was legitimately authorized, so the connection
			// stays open and keeps delivering the followed recipient's signals.
			s.publish(t, "bob", "event-3")
			assert.Equal(t, string(ntfy.ChangeCreated), readFrame(t, d.conn)["change"])
		})
	}
}
```

`net/http` is imported here for Task 2's helper; if the linter objects before Task 2 lands, add it in Step 1 of Task 2 instead.

- [ ] **Step 2: Run the test and confirm it fails for the right reason**

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -run 'TestMarkOnFollowedConnection' -count=1 ./...`

Expected: FAIL on both subtests. The decisive line is the state assertion, which must read:

```
Error:  Not equal: expected: 1  actual: 0
Test:   TestMarkOnFollowedConnection/mark-all-read
Messages: the followed recipient's notifications are unchanged
```

The JSONEq assertion also fails (today the server answers `{"type":"marked",...}` or delivers bob's `unread-changed` signal first). Both are expected — but the escalation is proved by `the followed recipient's notifications are unchanged`. If only the JSONEq assertion fails and the count is still 1, stop: the test is not reproducing the defect and the rest of this plan is built on a false premise.

- [ ] **Step 3: Thread the actor through the connection's call chain**

In `websocket/handler.go`, change the call site at `:260`:

```go
	h.serve(r.Context(), conn, subscription, actor, recipient)
```

the `serve` signature at `:286` and its reader launch at `:295`:

```go
func (h *Handler) serve(requestCtx context.Context, conn *cws.Conn, subscription *ntfy.Subscription, actor, recipient string) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(requestCtx))

	replies := make(chan any, replyBuffer)
	readerDone := make(chan struct{})

	go func() {
		defer close(readerDone)

		h.read(ctx, conn, actor, recipient, replies)
	}()
```

and `read` at `:364` and `:372`:

```go
func (h *Handler) read(ctx context.Context, conn *cws.Conn, actor, recipient string, replies chan<- any) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		select {
		case replies <- h.answer(ctx, actor, recipient, data):
		case <-ctx.Done():
			return
		}
	}
}
```

- [ ] **Step 4: Make `answer` act on the actor and refuse a followed connection**

Replace `websocket/handler.go:403-435` (the comment and body of `answer`) with:

```go
// errFollowedConnection refuses a mark request on a connection that follows
// another recipient. A subscription policy grants following only, so the write
// is refused rather than silently retargeted at the acting user's own inbox.
// Its message names the rule and never the followed recipient.
var errFollowedConnection = fmt.Errorf(
	"%w: a connection following another recipient may not mark notifications read",
	ntfy.ErrUnauthorized,
)

// answer applies one request to the acting user's own notifications and returns
// the reply. A connection may follow another recipient's signals, but the policy
// that permitted that grants no authority to change anything of theirs: a mark
// request on such a connection is refused, not retargeted. Another recipient's
// notification named in a mark request is not found exactly like one that does
// not exist.
func (h *Handler) answer(ctx context.Context, actor, recipient string, data []byte) any {
	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		return errorReply("", &ntfy.ValidationError{Subject: "message", Issues: []ntfy.ValidationIssue{
			{Detail: "must be a JSON object with a type of mark-read or mark-all-read"},
		}})
	}

	if recipient != actor && (req.Type == "mark-read" || req.Type == "mark-all-read") {
		return errorReply(req.Ref, errFollowedConnection)
	}

	var (
		result ntfy.MarkResult
		err    error
	)

	switch req.Type {
	case "mark-read":
		result, err = h.svc.MarkRead(ctx, actor, req.IDs...)
	case "mark-all-read":
		result, err = h.svc.MarkAllRead(ctx, actor, req.Through)
	default:
		err = &ntfy.ValidationError{Subject: "message", Issues: []ntfy.ValidationIssue{
			{Pointer: "/type", Detail: "must be mark-read or mark-all-read"},
		}}
	}

	if err != nil {
		return errorReply(req.Ref, err)
	}

	return markedFrame{Type: "marked", Ref: req.Ref, Marked: result.Marked}
}
```

An unrecognised type on a followed connection stays a validation error: the guard covers the two requests that change something, which is what the spec's malformed-message scenario requires.

- [ ] **Step 5: Map the refusal to the contract's forbidden code**

Replace `websocket/handler.go:437-451` (`errorReply` and its comment) with:

```go
// errorReply maps an error to a reply with the HTTP contract's codes. A
// not-found reply carries a fixed message, so that it cannot tell one missing
// identifier from another, and a forbidden reply names the rule that refused
// the request and never the recipient the connection follows.
func errorReply(ref string, err error) errorFrame {
	frame := errorFrame{Type: "error", Ref: ref, Code: "internal", Message: "the request could not be completed"}

	switch {
	case errors.Is(err, ntfy.ErrValidation):
		frame.Code, frame.Message = "validation_failed", err.Error()
	case errors.Is(err, ntfy.ErrNotFound):
		frame.Code, frame.Message = "not_found", ntfy.ErrNotFound.Error()
	case errors.Is(err, ntfy.ErrUnauthorized):
		frame.Code, frame.Message = "forbidden", err.Error()
	}

	return frame
}
```

- [ ] **Step 6: Run the test and confirm it passes**

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -run 'TestMarkOnFollowedConnection' -count=1 ./...`

Expected: PASS, both subtests.

- [ ] **Step 7: Run the whole module with the race detector**

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./...`

Expected: `ok  github.com/kartaladev/ntfy/websocket`. Every existing `SelfOnly` test still passes untouched — under that policy the connection's recipient *is* the actor, so nothing changes for them. `goleak` (wired in `websocket/main_test.go`) reports no leaked goroutine.

- [ ] **Step 8: Commit**

```bash
git add websocket/handler.go websocket/authority_test.go
git commit -m "Mark notifications read as the acting user over WebSocket

A subscription policy grants following only. A mark request on a connection
following another recipient is refused as forbidden instead of clearing that
recipient's inbox."
```

---

### Task 2: Guard the two transports against drifting apart

Implements the spec scenario "Both transports grant the same authority"; `design.md` D5.

**Files:**
- Modify: `websocket/authority_test.go` (add the cross-transport test and the HTTP helper), `websocket/helpers_test.go:36-41` and `:61`
- Test: `websocket/authority_test.go`

**Interfaces:**
- Consumes: everything Task 1 produced, plus the existing fixtures `refused(status int, code string) func(t *testing.T, d dialed, err error)`, `upgraded(t *testing.T, d dialed, err error)` and `supervisorPolicy ntfy.SubscriptionAuthorizer` (all in `websocket/refusal_test.go`), and `actorHeader = "X-Actor"` (`websocket/helpers_test.go:22`).
- Produces: `serverConfig.sse []ntfy.HandlerOption`; `func (s *server) markReadOverHTTP(t *testing.T, actor, id string) int`.

- [ ] **Step 1: Let a test give both transports the same policy**

In `websocket/helpers_test.go`, add the `sse` field to `serverConfig` (`:36-41`):

```go
// serverConfig is what a test varies about a server.
type serverConfig struct {
	stopped bool
	hub     []ntfy.HubOption
	ws      []websocket.Option
	sse     []ntfy.HandlerOption
	service []ntfy.Option
}
```

and pass it when building the SSE handler (`:61`):

```go
	sseHandler, err := ntfy.NewHandler(svc, hub, append([]ntfy.HandlerOption{ntfy.WithActor(headerActor)}, cfg.sse...)...)
```

- [ ] **Step 2: Write the cross-transport authority test**

Append to `websocket/authority_test.go`:

```go
// TestTransportsGrantTheSameAuthority holds the two transports of one contract
// to the same authority under the same policy: whatever a policy lets a user
// follow, neither transport lets that user change another recipient's
// notifications.
//
// A transport added later that is not driven by this test can reintroduce the
// escalation this change removed — the WebSocket transport had it while the
// HTTP contract did not, and nothing failed. Add the new transport here rather
// than testing its authority on its own.
func TestTransportsGrantTheSameAuthority(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		policy ntfy.SubscriptionAuthorizer
		actor  string
		// follows reports whether the policy lets actor follow bob at all.
		follows bool
	}

	cases := []testCase{
		{name: "the default policy follows nobody else", policy: ntfy.SelfOnly, actor: "alice", follows: false},
		{name: "every subscription permitted", policy: ntfy.AllowAll, actor: "alice", follows: true},
		{name: "a supervisor policy", policy: supervisorPolicy, actor: "sup", follows: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := startServer(t, serverConfig{
				ws:  []websocket.Option{websocket.WithSubscriptionAuthorizer(tc.policy)},
				sse: []ntfy.HandlerOption{ntfy.WithSubscriptionAuthorizer(tc.policy)},
			})

			s.publish(t, "bob", "event-1")
			s.publish(t, tc.actor, "event-2")

			bobs := s.idOf(t, "bob")

			// The HTTP transport: mark bob's notification, named over the contract.
			assert.Equal(t, http.StatusNotFound, s.markReadOverHTTP(t, tc.actor, bobs),
				"the HTTP transport refuses to mark another recipient's notification")

			// The WebSocket transport: the same mark, over a connection that
			// follows bob as far as the policy allows.
			d, err := s.dial(t, dialRequest{actor: tc.actor, recipient: "bob"})

			if tc.follows {
				upgraded(t, d, err)

				send(t, d.conn, `{"type":"mark-read","ref":"r1","ids":["`+bobs+`"]}`)
				assert.Equal(t, "forbidden", readFrame(t, d.conn)["code"],
					"the WebSocket transport refuses the same mark")
			} else {
				refused(http.StatusForbidden, "forbidden")(t, d, err)
			}

			for _, recipient := range []string{"bob", tc.actor} {
				count, err := s.svc.CountActive(t.Context(), recipient)
				require.NoError(t, err)
				assert.EqualValues(t, 1, count, "%s's notifications are unchanged", recipient)
			}
		})
	}
}

// markReadOverHTTP marks a notification read over the HTTP contract as actor,
// and returns the status the contract answered with.
func (s *server) markReadOverHTTP(t *testing.T, actor, id string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.url+"/v1/notifications/"+id+"/read", http.NoBody)
	require.NoError(t, err)

	req.Header.Set(actorHeader, actor)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode
}
```

- [ ] **Step 3: Run the test and confirm it passes**

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -run 'TestTransportsGrantTheSameAuthority' -count=1 ./...`

Expected: PASS, all three subtests.

- [ ] **Step 4: Confirm the test notices the defect it guards against**

A regression test that has never failed proves nothing (`.claude/rules/prove-errors-with-tests.md`). Temporarily invert the fix: in `websocket/handler.go`, comment out the guard added in Task 1 Step 4 —

```go
	// if recipient != actor && (req.Type == "mark-read" || req.Type == "mark-all-read") {
	// 	return errorReply(req.Ref, errFollowedConnection)
	// }
```

— and change the two service calls back to `recipient`:

```go
		result, err = h.svc.MarkRead(ctx, recipient, req.IDs...)
		result, err = h.svc.MarkAllRead(ctx, recipient, req.Through)
```

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -run 'TestTransportsGrantTheSameAuthority' -count=1 ./...`

Expected: FAIL on `every subscription permitted` and `a supervisor policy`, reporting `bob's notifications are unchanged` with actual `0`. Then restore all three lines exactly as Task 1 Step 4 wrote them and re-run to confirm PASS.

- [ ] **Step 5: Run the whole module with the race detector**

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./...`

Expected: `ok  github.com/kartaladev/ntfy/websocket`, no leaked goroutines.

- [ ] **Step 6: Commit**

```bash
git add websocket/authority_test.go websocket/helpers_test.go
git commit -m "Assert both realtime transports grant the same authority

One policy, one request, both transports: neither may change a recipient
other than the acting user."
```

---

### Task 3: Say what a subscription policy grants

Implements `design.md` D4 and the proposal's "the ports say what they grant". The supervisor example in `authorize.go` is what leads a host into the defect, so leaving it unqualified invites the same mistake against any future write port.

**Files:**
- Modify: `authorize.go:8-19` (`SubscriptionAuthorizer`), `:31-34` (`SelfOnly`), `:36-41` (`AllowAll`)
- Modify: `websocket/doc.go:9-10`
- Modify: `docs/realtime-operations.md:105-108`, `:116`, and the paragraph after `:117`
- Test: `websocket/docs_test.go` (existing, unchanged — it guards the guide's stated values)

**Interfaces:**
- Consumes: `errFollowedConnection`'s message from Task 1, quoted in the guide's forbidden-reply row.
- Produces: nothing other code depends on.

- [ ] **Step 1: State the grant on the port**

Replace the doc comment at `authorize.go:8-13`:

```go
// SubscriptionAuthorizer decides whether an acting user may follow a
// recipient's change signals.
//
// It authorizes following only. Permitting an acting user to follow a recipient
// grants no authority to change anything of that recipient's: marking read acts
// on the acting user's own notifications, on every transport.
//
// The default is [SelfOnly]. A host replaces it wholesale, for example with a
// policy that lets a supervisor follow a team member; nothing is chained, so a
// policy that extends the default calls SelfOnly itself.
type SubscriptionAuthorizer interface {
```

- [ ] **Step 2: State it on both shipped policies**

Replace the doc comment at `authorize.go:31-34`:

```go
// SelfOnly is the default subscription policy: a user may follow only their
// own notifications. Every notification is addressed to one recipient, so this
// is everything a user's own client needs. It refuses any subscription when no
// acting user is established. Its refusals match [ErrUnauthorized].
//
// Under it a connection's recipient is always the acting user, so marking read
// over a WebSocket connection behaves exactly as it does over the HTTP contract.
var SelfOnly SubscriptionAuthorizer = SubscriptionAuthorizerFunc(selfOnly)
```

and at `authorize.go:36-38`:

```go
// AllowAll permits every subscription. It is the explicit opt-out from
// [SelfOnly], for a host that authorizes subscriptions somewhere else, and it is
// named so that streaming anyone's signals to anyone is never an accident.
//
// It grants no authority to change anything. An acting user following another
// recipient under this policy still cannot mark that recipient's notifications
// read: such a request is refused as forbidden. A host that needs one user to
// mark another's notifications read does that over the HTTP contract, behind its
// own authorization.
var AllowAll SubscriptionAuthorizer = SubscriptionAuthorizerFunc(func(context.Context, string, string) error {
```

- [ ] **Step 3: Correct the transport's package comment**

In `websocket/doc.go`, replace the sentence at `:9-10` ("Beyond the stream, a client can mark its notifications read over the connection.") so the paragraph ends:

```go
// [github.com/kartaladev/ntfy.Hub], so one per-recipient cap counts
// streams and WebSocket connections together. Beyond the stream, a client can
// mark the acting user's own notifications read over the connection: a policy
// that permits following another recipient grants no authority to change
// anything of theirs.
```

- [ ] **Step 4: Correct the operations guide**

In `docs/realtime-operations.md`, replace lines 105-108:

```markdown
Both authorize the subscription with the same policy (`ntfy.SelfOnly` by
default), refuse while the hub is not running, and count against the same
per-recipient cap of 8 connections per instance. The policy grants following
only: a WebSocket client can also mark notifications read over its connection,
and that always acts on the acting user, never on a followed recipient:
```

Add a row after line 116, the last row of the message table:

```markdown
| server to client | `{"type":"error","ref":"r1","code":"forbidden","message":"..."}` |
```

And insert this paragraph immediately after that table, before `WebSocket defaults:`:

```markdown
**Stated limit: a subscription policy grants following only.** Marking read over
a WebSocket connection always acts on the acting user. A connection opened for
someone else — under `ntfy.AllowAll`, or a policy that lets a supervisor follow a
team member — keeps receiving that recipient's signals, and a mark request on it
is answered `forbidden` and changes nothing. There is no option to widen this: a
host that needs one user to mark another's notifications read does it over the
HTTP contract, behind its own authorization.
```

The phrases `8 connections per instance`, `4096 bytes`, `` `ntfy.v1` ``, `GET /v1/notifications/socket`, `not available on Fiber` and `status 1001` are asserted by `websocket/docs_test.go` — none of these edits removes them.

- [ ] **Step 5: Check the rendered documentation and the guide**

Run: `GOTOOLCHAIN=go1.26.8 go doc . AllowAll && GOTOOLCHAIN=go1.26.8 go doc . SubscriptionAuthorizer`

Expected: both render the new paragraphs, with "grants no authority to change anything" visible on `AllowAll`.

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -run 'TestTheOperationsGuideMatchesTheHandler' -count=1 ./...`

Expected: PASS — the guide still states every value the handler exports.

- [ ] **Step 6: Confirm no comment still describes the old behaviour**

Run: `rg -n "connection's recipient" websocket/ docs/ authorize.go`

Expected: no match in `websocket/handler.go` describing what a request acts on. A match inside `websocket/authority_test.go` or a spec file is fine — those describe the connection's subscription, not its write target. If `handler.go` still matches, fix that comment now.

- [ ] **Step 7: Commit**

```bash
git add authorize.go websocket/doc.go docs/realtime-operations.md
git commit -m "Say that a subscription policy grants following only

The supervisor example invited hosts to read the policy as a write grant."
```

---

### Task 4: Verify the whole change

Spans every earlier task: the module boundary, the other modules, and the guarantee that the new tests still prove the defect.

**Files:**
- Modify: none

**Interfaces:**
- Consumes: the finished state of Tasks 1-3.
- Produces: nothing.

- [ ] **Step 1: Run the project's full gate**

Run: `make all`

Expected: `0 issues.` from `golangci-lint` for each of the six modules, `split-check` clean (the `websocket` module still imports only ntfy and coder/websocket), and every module's tests passing.

- [ ] **Step 2: Run the race detector on both affected modules**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./... && cd websocket && GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./...`

Expected: `ok` for both, no `DATA RACE`, and no goroutine-leak failure from `goleak`.

- [ ] **Step 3: Prove the escalation test still fails without the fix**

Invert the fix exactly as in Task 2 Step 4 (comment out the guard in `answer`, change the two service calls back to `recipient`).

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -run 'TestMarkOnFollowedConnection|TestTransportsGrantTheSameAuthority' -count=1 ./...`

Expected: FAIL, with `the followed recipient's notifications are unchanged` among the failures. Restore the three lines and re-run: PASS. If either test passes with the fix inverted, it is vacuous — fix the test before finishing.

- [ ] **Step 4: Validate the change against its spec**

Run: `openspec validate "fix-websocket-write-authorization" --strict`

Expected: `Change 'fix-websocket-write-authorization' is valid`.

Then check each scenario in `specs/notification-realtime/spec.md` against a test:

| Scenario | Test |
| --- | --- |
| Following does not grant changing | `TestMarkOnFollowedConnection` |
| Marking read on a followed connection is refused | `TestMarkOnFollowedConnection` (forbidden reply, both inboxes unchanged, connection keeps delivering) |
| Both transports grant the same authority | `TestTransportsGrantTheSameAuthority` |
| A user follows their own notifications by default | `TestHandshakeRefusals/a user connects for their own notifications` (existing) |
| Following someone else is refused by default | `TestHandshakeRefusals/following someone else under the default policy is forbidden` (existing) |
| A host policy permits a supervisor | `TestHandshakeRefusals/a host policy permitting a supervisor upgrades` (existing) |
| A missing policy is refused at construction | `TestNewHandler/a nil subscription policy` (existing) |
| Marking a notification read over the connection | `TestReadLoop/mark-read marks the notification...` (existing) |
| Another recipient's notification is not found | `TestReadLoop/another recipient's notification is not found...` (existing) |
| A malformed request keeps the connection open | `TestReadLoop/a malformed message is a validation error...` (existing) |
| An oversized message closes the connection | `TestReadLoop/a message over the read limit...` (existing) |

Expected: every row has a test. Any gap is a missing test, not a missing requirement.

- [ ] **Step 5: Commit anything the gate changed**

`make all` runs `gofumpt`/`goimports` through the linter; if it rewrote a file, commit that.

```bash
git status --porcelain
git add -A
git commit -m "Format after the WebSocket write authorization fix"
```

If `git status --porcelain` is empty, skip this step — the change is complete.

---

## Self-Review

**Spec coverage.** Every scenario in the delta maps to a test in Task 4 Step 4's table; the two new scenarios are covered by the two new tests, and the nine pre-existing scenarios by tests already in the module. `design.md` D1 is Task 1 Steps 3-4, D2 is Steps 4-5, D3 needs no code (it is the absence of an option, recorded in the guide's stated limit in Task 3 Step 4), D4 is Task 1 Step 4 and Task 3, D5 is Task 2. The proposal's "Impact — Docs" line is Task 3 Step 4.

**Placeholder scan.** No TBD, no "add error handling", no "similar to Task N". Every code step carries the full replacement text, every run step the exact command and the expected output. The one judgement call left to the executor is flagged inline (the `net/http` import in Task 1 Step 1, which Task 2 needs) with its resolution.

**Type consistency.** Fixed and checked against the source: `serve(requestCtx context.Context, conn *cws.Conn, subscription *ntfy.Subscription, actor, recipient string)`, `read(ctx context.Context, conn *cws.Conn, actor, recipient string, replies chan<- any)`, `answer(ctx context.Context, actor, recipient string, data []byte) any`, `errorReply(ref string, err error) errorFrame`, `errFollowedConnection`. Test fixtures are used under their real names — `startServer`, `serverConfig`, `dialRequest`, `dialed`, `(*server).dial`, `(*server).publish`, `(*server).idOf`, `send`, `readRaw`, `readFrame`, `refused`, `upgraded`, `supervisorPolicy`, `actorHeader` — verified against `websocket/helpers_test.go`, `readloop_test.go`, `writeloop_test.go` and `refusal_test.go`. `ntfy.HandlerOption` is the core handler's option type, matching `ntfy.NewHandler`'s signature.

**Fixed during review:** the escalation test originally asserted the reply frame before the stored state, which would have made the red run report a frame mismatch rather than the escalation; the state assertions now carry messages that name the defect, and Task 1 Step 2 tells the executor which failure line is decisive and to stop if it does not appear.
