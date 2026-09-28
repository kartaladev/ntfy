## Why

The NATS broadcaster's `Listen` never returns once its connection closes for good. It waits only on its context and on a message channel that nats.go never closes. When the broker stays away longer than the connection's reconnect budget (60 attempts 2 seconds apart by default), or the host closes the connection, the connection is CLOSED and will never deliver again. `Listen` keeps running, so `hub.Run` never returns, `Hub.Running` stays true, and the hub keeps accepting streams that will only ever receive heartbeats. `notification-realtime` forbids exactly that: a stream is refused "rather than opened and left silent", and open streams close "when an instance stops receiving". The Redis broadcaster already returns an error when its subscription ends.

Two smaller findings from the same audit (2026-09-28) travel with it, because both concern what the broadcaster tests can be trusted to say:

- `redis/listen_test.go`'s case "cancelling stops listening and unsubscribes" reads `PUBSUB NUMSUB` the instant `Listen` returns. go-redis's `PubSub.Close` closes the TCP connection without sending `UNSUBSCRIBE`, so the server forgets the subscription when it processes the close, about a millisecond later. The case is therefore flaky, and it has made `make all` fail on `main`.
- `ntfytest.RunBroadcasterSuite` checks readiness, cancellation and nil functions, but not delivery itself. It accepts a broadcaster that hands each signal to only one of several listeners (what a NATS queue group does), one that drops a signal's change and time, and one that delivers only the first signal of a batch. A host's own broadcaster is held to the contract only by this suite.

**Proved by:** each proof was re-run on current `main` (`20967b3`) on 2026-09-28, with `GOTOOLCHAIN=go1.26.8`, NATS 2.12.7 and Redis 8.2.9 in containers.

1. `TestAuditListenEndsWhenTheConnectionClosesForGood` (`nats`), with two companions written for this proposal: `TestAuditListenEndsWhenTheHostClosesTheConnection` and `TestAuditHubStopsWhenTheConnectionCloses`.

   ```
   --- FAIL: TestAuditListenEndsWhenTheConnectionClosesForGood (5.91s)
       Error: the connection is CLOSED, yet Listen is still running 5s later: the instance keeps reporting ready while receiving nothing
   --- FAIL: TestAuditListenEndsWhenTheHostClosesTheConnection (5.73s)
       Error: the host closed the connection, yet Listen is still running 5s later
   --- FAIL: TestAuditHubStopsWhenTheConnectionCloses (5.61s)
       Error: hub still running
       Messages: the connection is CLOSED, yet hub.Running() is true 5s later
   ```

2. `TestAuditListenUnsubscribedWhenListenReturns` (`redis`) asserts what the flaky case asserts, over 200 rounds. Its companion `TestAuditListenSubscriptionGoneShortlyAfterReturn` shows that nothing leaks.

   ```
   audit_listen_unsubscribe_test.go:46: rounds=200 still-subscribed-at-return=9
   --- FAIL: TestAuditListenUnsubscribedWhenListenReturns (0.84s)
       Error: Should be zero, but was 9
   audit_listen_unsubscribe_test.go:73: rounds=200 slowest-unsubscribe-after-return=4.920958ms
   --- PASS: TestAuditListenSubscriptionGoneShortlyAfterReturn (0.70s)
   ```

   The flaky case itself failed 3 times in one run of `go test -C redis -run 'TestListen/cancelling_stops_listening_and_unsubscribes' -count=100 .`:

   ```
   Error Trace: redis/listen_test.go:162
   Error:       Should be zero, but was 1
   Messages:    the channel has no subscriber left
   ```

3. `TestAuditBroadcasterSuiteAcceptsBrokenBroadcasters` (`ntfytest`) runs the suite in a child process against three deliberately broken in-process broadcasters. A correct one is the control. The control passed, and each broken one was accepted:

   ```
   --- FAIL: TestAuditBroadcasterSuiteAcceptsBrokenBroadcasters/one-listener (0.11s)
       RunBroadcasterSuite accepted a "one-listener" broadcaster: PASS
   --- FAIL: TestAuditBroadcasterSuiteAcceptsBrokenBroadcasters/drop-fields (0.11s)
       RunBroadcasterSuite accepted a "drop-fields" broadcaster: PASS
   --- FAIL: TestAuditBroadcasterSuiteAcceptsBrokenBroadcasters/first-only (0.11s)
       RunBroadcasterSuite accepted a "first-only" broadcaster: PASS
   ```

The same audit file's store-suite proof, of a case-folding store, is not carried here. `compare-mysql-identifiers-by-bytes` already fixed it on `main`.

Tasks 1.1, 2.1 and 3.1 bring these proofs into the repository as the red steps.

## What Changes

- **NATS `Listen` ends with an error once its connection is closed for good.** "Closed" means the connection used up its reconnect attempts, or the host closed or drained it. The error matches `natsgo.ErrConnectionClosed`. `hub.Run` then returns it, `Hub.Running` turns false, and every open stream and WebSocket is closed, as for any other stop.
  - A connection that is only disconnected and still reconnecting is unchanged: `Listen` keeps running and resumes on reconnect.
  - A host that wants an instance to wait out any outage sets the connection's own `natsgo.MaxReconnects(-1)`. No new option is added.
- **The `ntfy.Broadcaster` contract says so for every implementation.** A `Listen` whose transport can no longer deliver, while its context is live, returns an error instead of running on. `Hub.Running`'s godoc no longer implies it stays true through every connection loss.
- **The Redis cancel test waits for the server to drop the subscription**, within the test's usual bound, instead of reading `PUBSUB NUMSUB` the instant `Listen` returns. Redis `Listen`'s behaviour does not change. Its godoc stops promising that it "unsubscribes" and describes what it does: it closes its subscription's connection, and the server drops the subscription when it sees the close.
- **`ntfytest.RunBroadcasterSuite` checks delivery.** Three new cases:
  - every listener on a channel receives every signal;
  - a signal arrives with its recipient, change and time intact;
  - every signal of one broadcast is delivered, across a batch larger than one message.

  **BREAKING** for a host's broadcaster that the suite passed while failing one of these properties. No tag has been cut, so this is recorded rather than versioned.
- **A child-process test proves the suite rejects each broken broadcaster**, following `TestIdentityRejectsFoldingStores`.
- **Docs:** `docs/realtime-operations.md` says what happens when a broker connection closes for good, and how a host keeps an instance waiting instead. `docs/notifications.md` lists what `RunBroadcasterSuite` checks.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-realtime`:
  - "Signals reach every instance through a replaceable broadcaster" gains the rule that a broadcaster whose connection closes for good ends receiving with an error, so streams are closed rather than left silent.
  - "Signals cross instances over Redis or NATS" states the delivery properties that the shared conformance suite asserts for every broadcaster, a host's own included.

## Impact

- **Code:**
  - `nats/broadcaster.go`: `Listen` and its godoc.
  - `redis/broadcaster.go`: `Listen`'s godoc only.
  - `realtime.go`: the `Broadcaster.Listen` contract godoc.
  - `hub.go`: `Hub.Running`'s godoc.
  - `ntfytest/broadcaster.go`: three cases and two helpers; `ntfytest/doc.go`.
- **Tests:**
  - new `nats/closed_test.go`;
  - `redis/listen_test.go`, the cancel case;
  - new `ntfytest/broadcaster_test.go`, the child-process proof;
  - `nats/docs_test.go`, one more stated string.
- **Docs:** `docs/realtime-operations.md`, `docs/notifications.md`.
- **Dependencies:** none. `Conn.StatusChanged` and `Conn.RemoveStatusListener` are in the pinned nats.go v1.53.1.
- **Compatibility:**
  - A NATS host whose connection closes for good now sees `hub.Run` return. Before, it hung with the hub reporting ready. A host that restarts `hub.Run` on error keeps working. A host that treated a return as fatal now learns about an outage it used to hide.
  - A host's broadcaster may now fail `RunBroadcasterSuite` (see What Changes).
