## 1. NATS `Listen` ends with its connection

- [ ] 1.1 Add `nats/closed_test.go` in the `table-test` form: `TestListenEndsWhenTheConnectionCloses`, over one NATS container, reaching the server through the existing `startProxy`. It has three cases:
  - "the connection gives up reconnecting": `MaxReconnects(2)`, `ReconnectWait(50ms)`, the proxy closed. `Listen` returns an error matching `natsgo.ErrConnectionClosed` within 5s.
  - "the host closes the connection": `Listen` returns the same error within 5s.
  - "a connection reconnecting without limit keeps listening": `MaxReconnects(-1)`, the same outage. After 1s `Listen` is still running and the connection is reconnecting.

  Add `TestHubStopsReceivingWhenTheNATSConnectionCloses`. It runs a hub with a subscription open, then closes the connection, and requires:
  - `hub.Run` returns an error matching `natsgo.ErrConnectionClosed`;
  - `Hub.Running` is false;
  - the subscription's `Done` is closed;
  - `hub.Subscribe` fails with `ntfy.ErrUnavailable`.

  Verify with `GOTOOLCHAIN=go1.26.8 go test -C nats -run 'TestListenEndsWhenTheConnectionCloses|TestHubStopsReceivingWhenTheNATSConnectionCloses' -count=1 .`. Confirm that both closing cases and the hub test fail because `Listen` is **still running 5s later**, and that the unlimited case passes.
- [ ] 1.2 In `nats/broadcaster.go`, make `Listen` register `b.conn.StatusChanged(natsgo.CLOSED)` before `ChanSubscribe`, remove it on return, and return `fmt.Errorf("nats: listen on subject %q: %w", b.subject, natsgo.ErrConnectionClosed)` from the loop when it fires (`design.md` D1). Extend `Listen`'s godoc with the closed-connection rule, and the `natsgo.MaxReconnects(-1)` override. Verify that the 1.1 command passes, and that `GOTOOLCHAIN=go1.26.8 go test -C nats -race -count=1 .` passes, `TestListen`'s goleak checks included.
- [ ] 1.3 Update the `Broadcaster.Listen` godoc in `realtime.go` and the `Hub.Running` godoc in `hub.go` as in `design.md` D2. Also update the `nats/doc.go` package doc paragraph on listening. Verify with `GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy Broadcaster` and `make lint`.

## 2. The Redis cancel case waits for the server

- [ ] 2.1 Red, not committed: copy the audit's `TestAuditListenUnsubscribedWhenListenReturns` into `redis/`. Run `GOTOOLCHAIN=go1.26.8 go test -C redis -run 'TestAuditListenUnsubscribedWhenListenReturns' -count=1 .` and record its `still-subscribed-at-return=N` line (N > 0; 9 on 2026-09-28) in the commit message. Then delete the file. It asserts the racy premise, which stays false after the fix (`design.md` D3).
- [ ] 2.2 In `redis/listen_test.go`, make the "cancelling stops listening and unsubscribes" case wait with `require.Eventuallyf` (`testWait`, `testTick`) for `PUBSUB NUMSUB` of the channel to reach 0. Rename the case "cancelling stops listening and releases the subscription". In `redis/broadcaster.go`, replace "unsubscribes" in `Listen`'s godoc with what it does (`design.md` D3). Verify that `GOTOOLCHAIN=go1.26.8 go test -C redis -run 'TestListen' -count=20 .` passes.

## 3. The broadcaster suite checks delivery

- [ ] 3.1 Add `ntfytest/broadcaster_test.go` (`design.md` D5): `brokenBroadcaster` with the modes `sound`, `one-listener`, `drop-fields` and `first-only`, plus `TestBroadcasterSuiteChild` and the table `TestBroadcasterSuiteRejectsBrokenBroadcasters`. Each broken row requires the child to fail **in its targeted case**. Verify with `GOTOOLCHAIN=go1.26.8 go test -C ntfytest -run 'TestBroadcasterSuiteRejectsBrokenBroadcasters' -count=1 .`. Confirm that the control passes and the three broken rows fail because the child **passed**.
- [ ] 3.2 In `ntfytest/broadcaster.go`, add:
  - the exported constant `BroadcasterBatch = 1201` and the constant `everyListenerSignals = 4`;
  - the helpers `snapshot` and `awaitCount`;
  - the cases `assertEveryListener`, `assertSignalIntact` and `assertWholeBatch`, registered in `RunBroadcasterSuite`, whose godoc lists them (`design.md` D4).

  Verify that the 3.1 command passes. Then check that each new case is what rejects its broken broadcaster: remove each case's registration line in turn, confirm its row goes red, and restore it.
- [ ] 3.3 Run the three conformance tests against the library's own broadcasters:
  - `GOTOOLCHAIN=go1.26.8 go test -race -run TestInProcessBroadcasterConformance -count=3 .`
  - `GOTOOLCHAIN=go1.26.8 go test -C redis -race -run TestBroadcasterConformance -count=3 .`
  - `GOTOOLCHAIN=go1.26.8 go test -C nats -race -run TestBroadcasterConformance -count=3 .`

  All pass.

## 4. Documentation

- [ ] 4.1 In `nats/docs_test.go`, add `"natsgo.ErrConnectionClosed"` and `"natsgo.MaxReconnects(-1)"` to the strings the operations guide must state. Watch `GOTOOLCHAIN=go1.26.8 go test -C nats -run TestTheOperationsGuideMatchesTheBroadcaster -count=1 .` fail. Then add a section on connections that close for good to `docs/realtime-operations.md`, covering the default, the override, and re-running `hub.Run`. Verify that the test passes.
- [ ] 4.2 In `docs/notifications.md`, list what `ntfytest.RunBroadcasterSuite` checks and say that `Running` turns false once a broadcaster gives up, and add the delivery properties to the `ntfytest/doc.go` package doc. Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .` and `make lint`.

## 5. Verify and hand off

- [ ] 5.1 Run `make all` and `make store-matrix`, the latter because `ntfytest`, the module that holds the store suite, changes. Both pass. Also confirm that `git status` shows no stray `audit_*` file.
- [ ] 5.2 Run `/code-review` on the branch diff, answer its findings under `superpowers:receiving-code-review`, and record the answers in `tasks.md` and `plans.md` together.
