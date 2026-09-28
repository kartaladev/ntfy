## Context

See `proposal.md`, under Why. The change spans four modules: `ntfy/nats` for the fix, `ntfy/redis` for a test and a godoc, `ntfy/ntfytest` for the suite, and the core `ntfy` for two godocs.

Current state, read on `main` at `20967b3`:

| Where | What |
| --- | --- |
| `nats/broadcaster.go:189-225` | `Listen`: `ChanSubscribe` into a buffered channel, `confirm` by flush, `ready()`, then `select` on `ctx.Done()` and the message channel only |
| `redis/broadcaster.go:175-225` | `Listen`: returns `"redis: the subscription to channel %q ended"` when go-redis closes its message channel, which it does when the client is closed |
| `redis/broadcaster.go:157-160` | godoc: "until ctx is done, when it unsubscribes and returns ctx's error" |
| `redis/listen_test.go:145-165` | the cancel case reads `PUBSUB NUMSUB` straight after `Listen` returns |
| `ntfytest/broadcaster.go:46-73` | `RunBroadcasterSuite`: four cases (nil functions, ready once, delivered after ready, cancelled) |
| `realtime.go:53-68` | the `Broadcaster.Listen` contract says nothing about a transport that ends |
| `hub.go:296-303` | `Hub.Running` godoc: "Running stays true" after a connection drops |
| `hub.go:217-233` | `hub.Run` returns whatever `Listen` returns, and a returned run closes every subscription |

What nats.go v1.53.1 (the pinned version) does when a connection closes, read from its source:
- `Conn.close` marks every subscription closed. It closes the message channel only for a *synchronous* subscription. A `ChanSubscribe` channel belongs to the caller and is never closed. `Subscription.StatusChanged` does not fire either: the connection's close path never calls `changeSubStatus`.
- `Conn.close` does call `changeConnStatus(CLOSED)`. Listeners registered with `Conn.StatusChanged(natsgo.CLOSED)` get the event by a non-blocking send into a channel buffered at 10.
- `Conn.RemoveStatusListener(ch)` unregisters a listener.
- `ChanSubscribe` on a closed connection returns `natsgo.ErrConnectionClosed`.

A spike on 2026-09-28 applied D1 to `nats/broadcaster.go` and then reverted it. Under `-race`, the three NATS audit proofs and the whole `nats` package passed with it.

## Goals / Non-Goals

**Goals**

- A NATS instance whose connection is closed for good stops reporting that it receives signals, and closes its streams, exactly as it does when `hub.Run`'s context ends.
- Every `ntfy.Broadcaster` is told, in its contract, to do the same.
- The Redis cancel case stops failing `make all` at random, and still proves that `Listen` leaves no subscription behind.
- `RunBroadcasterSuite` fails a broadcaster that loses listeners, fields or signals, and the repository proves that it does.

**Non-Goals**

- Restarting `hub.Run` for the host. Whether and when to run it again, on which connection, stays the host's decision, as it already is for an unconfirmed subscription.
- A subscription the server ends while the connection lives, such as a permissions violation. nats.go reports that to the connection's async error handler, which the host owns. Not observed, not proved, and not planned.
- Changing how go-redis ends a subscription (D3).
- Checking signal order, or duplicate delivery, in the suite. The contract promises neither.
- Holding the suite's new cases to anything a `PairFactory` cannot express. A property that needs a broker taken away, such as D1's, is tested in the adapter's own module.

## Decisions

### D1. NATS `Listen` watches the connection's CLOSED status

`Listen` registers `closed := b.conn.StatusChanged(natsgo.CLOSED)` before it subscribes, and removes it on return with `defer b.conn.RemoveStatusListener(closed)`. Its receive loop gains one `case <-closed:`, which returns:

```go
fmt.Errorf("nats: listen on subject %q: %w", b.subject, natsgo.ErrConnectionClosed)
```

Registering before `ChanSubscribe` leaves no window. A close before registration makes `ChanSubscribe` fail with `ErrConnectionClosed`. A close after it lands in the channel's buffer, even while `Listen` is still in `confirm`, and `confirm`'s flush fails first anyway.

| Option | Verdict |
| --- | --- |
| **`Conn.StatusChanged(CLOSED)`** | chosen: a public API made for this, per `Listen` call, removable, leaves the host's handlers alone |
| `natsgo.ClosedHandler` / `Conn.SetClosedHandler` | rejected: there is one slot, and it belongs to the host. Setting it would silently replace the host's handler |
| `Subscription.StatusChanged(SubscriptionClosed)` | rejected: never fires on a connection close (see Context) |
| poll `conn.IsClosed()` on a ticker | rejected: adds latency and a timer to every listener, for an event nats.go already reports |
| a sync subscription, whose channel nats.go does close | rejected: needs a goroutine for `NextMsg`, or a rewrite of the loop, for the same result |

**The error** wraps `natsgo.ErrConnectionClosed` rather than a new sentinel. A host already matches that error for every other operation on a closed connection. `ntfy/nats` adds no API, and the host matches with `errors.Is(err, natsgo.ErrConnectionClosed)`.

**Default:** a connection closed for good ends `Listen`. With nats.go's defaults, that is after 60 reconnect attempts 2 seconds apart, about two minutes of outage, or at once when the host closes or drains the connection.

**Override:** the host owns the connection, and with it the reconnect budget:
- `natsgo.MaxReconnects(-1)` keeps an instance waiting out any outage, and `Listen` never sees CLOSED from an outage;
- `natsgo.MaxReconnects(n)` and `natsgo.ReconnectWait(d)` tune how long it waits.

The broadcaster adds no option of its own. An option to "keep listening on a closed connection" would reintroduce the silent stream the spec forbids (`library-design.md` point 4: the line is stated, not relaxed).

### D2. The core contract and `Hub.Running` say it for every broadcaster

`realtime.go`'s `Broadcaster.Listen` godoc gains a paragraph: "A Listen whose transport can no longer deliver, such as a connection closed for good rather than one reconnecting, returns an error while ctx is still live, so that the hub stops reporting itself running."

`Hub.Running`'s paragraph becomes: "While the broadcaster's client reconnects after a dropped connection, Running stays true, and signals in that gap are lost, which the best-effort contract allows. When the broadcaster gives up, its Listen returns an error, the run ends, and Running reports false."

These are godoc changes only. A generic test of "ends when the transport ends" cannot be written against a `PairFactory` (Non-Goals), so each adapter proves it in its own module: the new NATS test, and Redis's existing `Listen` behaviour.

### D3. The Redis cancel case waits for the server; `Listen` does not send UNSUBSCRIBE

| Option | Verdict |
| --- | --- |
| **the test waits for `PUBSUB NUMSUB` to reach 0, within `testWait`** | chosen |
| `Listen` sends `UNSUBSCRIBE` and awaits the confirmation before returning | rejected |

Why sending `UNSUBSCRIBE` is rejected:
- The confirmation arrives on the same stream that go-redis's `Channel()` goroutine is reading, so awaiting it needs a second reader or a rewrite of the receive loop.
- It adds a round trip to every stop, and a stop during a broker outage would block until a new timeout expires, exactly when a host wants a fast shutdown.
- Nothing a host can observe improves. After `Listen` returns, `deliver` is never called again (the suite's cancel case proves it). A message published into the ~1 ms before the server drops the subscription goes to a closed socket, and no signal reaches anything. The audit measured the gap as at most 4.9 ms over 200 rounds.

The case keeps its strength:
- it still requires the subscription to be gone, now within `testWait` (10 s) rather than instantly;
- `goleak` still checks the client side.

`Listen`'s godoc drops "unsubscribes" and says what happens: "until ctx is done, when it closes its subscription's connection and returns ctx's error; the server drops the subscription when it sees the close, shortly after Listen returns."

No behaviour changes, so there is no default to override.

The audit's `TestAuditListenUnsubscribedWhenListenReturns` is the red step (task 2.1), run and recorded but **not committed**. It asserts the racy premise, which stays false after the fix, so it could only ever be red. The committed guard is the corrected case itself.

### D4. Three delivery cases in `RunBroadcasterSuite`

| Case | How | Fails |
| --- | --- | --- |
| `a signal reaches every listener on the channel` | one `Listen` on the publisher **and** one on the listener, the two-instance shape. Broadcast `everyListenerSignals` (4) signals, one call each. Both must receive all four | a broadcaster that hands each signal to one listener, like a queue group. Each listener sees 2 of 4 |
| `a signal arrives with its recipient, change and time` | broadcast one signal per `Change` value, each with its own whole-second UTC `At`. Every one must arrive with an equal `Change` and an `At` that is `Equal` | a broadcaster that drops fields |
| `every signal of a broadcast is delivered` | one `Broadcast` of `BroadcasterBatch` (1201) signals with distinct recipients. All must arrive | a broadcaster that delivers only a batch's first signal, or loses a message when a batch splits (1201 > 2 × 500, the per-message size of `ntfy/redis` and `ntfy/nats`) |

Details:
- **Listening on both broadcasters of the pair** is the only way to get two listeners on one channel from a `PairFactory`. A second factory call returns an *isolated* pair. For the in-process default, both are one value, so this is two `Listen` calls on one broadcaster, which it supports.
- **Order and duplicates are not asserted.** The contract promises neither.
- **Whole seconds and `time.Equal`** keep the time case independent of a host format's precision and of `time.Location`. The ntfy wire format carries microseconds, but a host broadcaster need not.
- **`BroadcasterBatch` is exported**, like `BroadcasterIterations`, so that a host knows the size the suite broadcasts in one call.
- A spike on 2026-09-28 added these cases and then reverted them. The in-process (`-race`), Redis and NATS conformance runs passed three times each, and the audit test rejected all three broken broadcasters.

**Default:** every broadcaster run through the suite is checked for these properties. **Override:** none, because they are the contract. `docs/notifications.md` names them, so that a host knows what passing means.

### D5. Proving the suite rejects broken broadcasters: a child process, as for identity

`ntfytest/broadcaster_test.go` mirrors `ntfytest/identity_test.go`:
- `TestBroadcasterSuiteChild` skips unless `NTFYTEST_BROADCASTER_CHILD` is set, and then runs `RunBroadcasterSuite` against `brokenBroadcaster{mode}`.
- `TestBroadcasterSuiteRejectsBrokenBroadcasters` is a table:
  - `sound`, the control, must pass and show a suite case passing (`--- PASS: TestBroadcasterSuiteChild/a_signal_broadcast_right_after_ready_is_delivered`), so that a child that runs nothing cannot pass. The case named already exists, so the control holds in the red step too;
  - each broken mode must fail **in the case that targets it**, which `assert.Contains` checks on the child's output. A child failing for any other reason, such as a panic, is not taken for a rejection.

`brokenBroadcaster` is a broken *implementation under test*, like `foldingStore`, not a double for a collaborator, so `use-mockgen` does not apply.

## Risks / Trade-offs

- **A host that treated `hub.Run`'s return as fatal now exits after a long NATS outage, where it used to hang reporting ready.** → This is the fix. Before, it was silently broken: accepting streams that never received a signal. `docs/realtime-operations.md` names `natsgo.MaxReconnects(-1)` for a host that would rather wait, and shows re-running `hub.Run` on a new connection.
- **A status event is dropped if the listener channel is full.** nats.go sends status events without blocking. → The channel is registered for CLOSED only, and CLOSED happens once per connection, so its buffer of 10 cannot fill.
- **The every-listener case doubles the listeners a pair holds.** → Each case builds its own pair, and the case costs about 10 ms against Redis and NATS (spike).
- **The batch case waits `broadcasterWait` (5 s) before failing a broadcaster that loses signals.** → That only slows a failing run. A passing run took about 20 ms (spike).
- **A host broadcaster that passed before may now fail.** → It was breaking the contract. Recorded as **BREAKING** in the proposal, before any tag.

## Migration Plan

Nothing to migrate. No schema, no configuration and no API is removed. A NATS host that wants the old "never end" behaviour sets `natsgo.MaxReconnects(-1)`, which is also the only way the old behaviour was ever correct.
