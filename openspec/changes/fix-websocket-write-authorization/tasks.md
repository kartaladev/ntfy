## 1. Prove the defect (red)

- [ ] 1.1 Add a failing test in the `websocket` module that reproduces the escalation: a handler wired with `ntfy.AllowAll`, an actor connecting with `?recipient=<victim>`, sending `{"type":"mark-all-read"}`, and asserting the victim's notifications keep the state they had. Verify with `go test -run 'TestMarkOnFollowedConnection' -count=1 ./...` inside `websocket/` and confirm it fails because the victim's notifications were marked read — not because of a wiring or compile error (see `.claude/rules/prove-errors-with-tests.md`).
- [ ] 1.2 Extend the same test to the named form (`mark-read` naming one of the victim's notification identifiers) and verify it fails the same way, so both write paths are covered before either is fixed.

## 2. Make writes act on the acting user (green)

- [ ] 2.1 Thread the acting user from `ServeHTTP` through `serve`, `read` and `answer` in `websocket/handler.go`, keeping the connection's recipient for signal delivery only, and verify the package still compiles with `go build ./...`.
- [ ] 2.2 Call `svc.MarkRead` and `svc.MarkAllRead` with the actor, and refuse a mark request with a forbidden reply echoing the request reference when the connection's recipient is not the actor, per `design.md` D2. Verify the tests from 1.1 and 1.2 now pass.
- [ ] 2.3 Add the forbidden case to `errorReply` so the reply carries the contract's existing forbidden code and a message that does not name the followed recipient, and verify with a test asserting the reply's code, reference and message.
- [ ] 2.4 Add a test asserting the connection stays open and still delivers the followed recipient's signals after a refused mark request, and verify it passes (spec scenario "Marking read on a followed connection is refused").

## 3. Guard against the transports drifting apart

- [ ] 3.1 Add a cross-transport authority test in the `websocket` module that drives the SSE handler and the WebSocket handler with the same policy and the same request, asserting both permit or both refuse and neither changes a recipient other than the actor (spec scenario "Both transports grant the same authority"). Follow the `table-test` skill for the policy cases (`SelfOnly`, `AllowAll`, a supervisor policy). Verify the whole `websocket` module passes with `go test -race -count=1 ./...`.
- [ ] 3.2 Name the test and comment it so a future transport is added to it rather than tested in isolation, and verify by reading it back: the comment must say what breaks if a new transport skips this test.
- [ ] 3.3 Confirm the default path is unchanged: run the existing `websocket` suite and verify every `SelfOnly` mark-read test still passes untouched.

## 4. Make the ports say what they grant

- [ ] 4.1 Update the godoc on `SubscriptionAuthorizer`, `SelfOnly` and `AllowAll` in `authorize.go` to state that a policy authorizes following only and grants no authority to change anything, with `AllowAll` naming the escalation it does not create. Verify with `go doc ./... | grep -A5 AllowAll` (or `gopls`) that the rendered documentation reads correctly.
- [ ] 4.2 Correct the comment on `answer` in `websocket/handler.go`, which currently presents acting on the connection's recipient as a safety property, and verify the file has no remaining comment describing the old behaviour (`rg -n "connection's recipient" websocket/`).
- [ ] 4.3 Update `docs/realtime-operations.md` where the transport comparison implies one policy covers both following and marking read, and add the migration note from `design.md` for hosts using `AllowAll` or a supervisor policy. Verify by re-reading the section: it must state that marking read always acts on the acting user.

## 5. Verify the whole change

- [ ] 5.1 Run `make all` (lint, split-check, test) and verify it passes on every module.
- [ ] 5.2 Run `go test -race -count=1 ./...` in the root and `websocket` modules and verify no data races and no goroutine leaks (`goleak` is already wired in `websocket/main_test.go`).
- [ ] 5.3 Re-run the 1.1 test against a temporarily reverted fix and verify it fails again, confirming the test still proves the defect rather than passing vacuously.
- [ ] 5.4 Run `openspec validate "fix-websocket-write-authorization" --strict` and verify the change is valid and every spec scenario has a corresponding test.
