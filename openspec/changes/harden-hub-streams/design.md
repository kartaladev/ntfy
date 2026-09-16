## Context

See `proposal.md` — Why. Both defects are reproducible against the code as it stands:

- `Hub.Running` (hub.go:199) is read only by `Subscribe` (hub.go:251). The stream loop (http.go:359-377) and the WebSocket serve loop (websocket/handler.go:307-343) select on the request context, `subscription.Ready()` and their heartbeat or ping, and on nothing else. After `Hub.Run` returns, an open client keeps heart-beating and receives no signal; a new stream is correctly refused.
- `Subscribe` (hub.go:250-272) bounds `len(h.subscriptions[recipient])` and never the size of `h.subscriptions` itself.

Constraints that shape the fix:

- `Subscription.ready` is a buffered channel written by `offer` (hub.go:292-302) with a non-blocking send, from `deliver` under `h.mu`.
- `endRun` (hub.go:178-190) already runs on every exit from `Run`, under `runMu`, and is where "the hub is no longer receiving" becomes true.
- `Subscription.Close` (hub.go:326-337) is idempotent through a `sync.Once` and frees the recipient's slot.
- The WebSocket transport already closes as going away on shutdown (websocket/handler.go:309-314), so that path exists and is tested.
- The core module is standard library only, and the WebSocket transport is a separate module that imports it. Anything the transports need must be exported from the core.
- Nothing is tagged (`docs/releasing.md`), so a new default limit costs no compatibility.

## Goals / Non-Goals

**Goals**

- An open stream cannot outlive its instance's ability to deliver signals to it.
- A stop disperses its clients rather than returning them in one wave.
- An instance's total streams are bounded by default, with the bound visible and replaceable.
- A third-party transport can implement the lifecycle correctly from the exported contract alone.

**Non-Goals**

- Changing how the broadcaster recovers. A dropped broker connection is resubscribed by the client itself, with the run still alive and `Running` still true; that is the best-effort contract and this change does not touch it.
- Replaying signals missed while a client was away — the store remains the source of truth.
- Reworking the `Ready()`/`Take()` coalescing contract, which is correct and deliberately lossy.
- The per-recipient cap's default of 8.

## Decisions

### D1. Closure is its own channel on `Subscription`, not a change to `Ready()`

`Subscription` gains a channel, closed once, that a transport selects on alongside `Ready()`.

*Alternative considered:* signal closure by closing the existing `ready` channel. Rejected on a concrete race, not on taste: `offer` performs a non-blocking send on `ready` while holding nothing that orders it against a close, so closing `ready` can panic with a send on a closed channel the moment a signal is delivered during a stop.

*Alternative considered:* have `Take()` report closure with a third return value. Rejected: a `select` needs something to wait on, so a transport would still have to poll; and it changes an existing contract that both transports and any host transport already use.

*Alternative considered:* leave the API alone and have each transport check `hub.Running()` on its heartbeat tick. This is the only zero-API-change option, and it is worth stating why it loses: staleness lasts up to a heartbeat interval (25 seconds by default, longer if a host raises it), liveness detection becomes coupled to a setting that exists for proxies, and every transport has to remember to do it.

**Default:** a stopped run closes every subscription it served. **Override:** none. A stream that cannot receive signals but stays open is the defect itself; a host that wants connections to survive a broker blip already has that, because a blip does not end the run.

### D2. `endRun` closes subscriptions, and `Subscribe` observes the stop atomically

`endRun` takes `h.mu`, closes every live subscription, and clears the map, so a later run starts with none. Closing reuses each subscription's existing `sync.Once`, so a subscription the client closed first, or one closed by two stops, is closed once.

Today `Subscribe` reads `Running` outside `h.mu` and then inserts under it. That window is the same defect in miniature: a run that ends between the check and the insert leaves a fresh subscription in a map nobody will deliver to, and the stop that would have closed it has already passed. The running state must therefore be observed under `h.mu`, together with the insert.

Lock order stays `runMu` → `mu`: `endRun` holds `runMu` and takes `mu`, and nothing takes them the other way. `Subscription.Close` continues to take only `mu`, and stays safe against a map the stop already cleared.

**Default:** the two happen atomically; there is nothing to configure.

### D3. What a closed client is told, and when to come back

**Server-sent events.** The stream writes a `retry:` field when it opens, carrying a delay drawn per stream from a base — 1 second by default — and up to twice it. Written at open rather than at close, because the value must survive the cases where the server never gets to write again: a stalled client, a dropped connection, a process that died. A browser's `EventSource` applies the last `retry:` it received.

**WebSocket.** The connection closes as going away, with a reason distinguishing a stopped instance from a shutdown. A close code is the only channel a WebSocket offers; the client's own backoff is the client's business, and the docs say to jitter it.

Why a delay at all, rather than reconnecting at once: after a stop the hub is not receiving, so a reconnect to the same instance is refused as unavailable. The delay is what stops that refusal from becoming a hot loop, and the jitter is what stops an instance's whole population from arriving together — behind a load balancer some of them will land on a healthy instance and stay.

**Default:** 1 second, jittered to less than 2. **Override:** an option on the hub replaces the base; the jitter is not separately configurable, because a base with no spread re-creates the wave this exists to prevent. That is a stated limit, not an oversight.

### D4. The instance cap defaults to 10,000 streams

Every open stream costs a connection, a file descriptor, goroutine stacks and the hub's own bookkeeping. 10,000 is chosen to sit far above what a healthy deployment reaches and below what exhausts a modest instance: it is 1,250 recipients at the full per-recipient cap of 8, or 10,000 ordinary users holding one stream each, and its per-stream cost is small enough that an instance hitting the cap is in trouble for some other reason first.

It is a default, not a judgement about any particular host: a host that knows its descriptor limits raises it with an option, and a host that bounds connections at its proxy or gateway removes it through an explicitly named opt-out, in the style the library already uses for `AllowAll` and `WithoutMaxAge`. Setting and removing it at once, or setting a total below the per-recipient cap — which would make the per-recipient cap unreachable and its error message a lie — are configuration errors at construction.

*Alternative considered:* derive the cap from the process's descriptor limit at construction. Rejected: the same code would behave differently on a developer's machine, in a container and in CI, and a limit nobody can predict is a limit nobody can test.

Refusal reuses `ErrTooManyStreams`, so the mapping to 429 already holds. Not `ErrUnavailable`/503: the instance is healthy and the client should come back or land elsewhere, whereas unavailable means this instance is not receiving signals at all. The message names which cap was reached, and `ErrTooManyStreams`'s godoc — which today says only "a recipient over the per-instance stream cap" — is corrected to describe both.

**Default:** 10,000 total per instance. **Override:** an option to raise or lower it, and a named opt-out to remove it.

### D5. The docs describe one shutdown recipe for both transports

`http.Server.Shutdown` ends neither transport on its own: a WebSocket is hijacked, and an SSE stream's request context is never cancelled, so the loop's `r.Context().Done()` case never fires. The documented `BaseContext` plus `RegisterOnShutdown` pair does end both, because the stream's request context derives from the base context — the section simply never says so, and a host serving only streams reads it as WebSocket advice. This is a documentation fix; no behaviour changes, so it stays out of the spec delta.

## Risks / Trade-offs

- **An instance legitimately serving more than 10,000 streams starts refusing them** → It fails loudly with an existing error and status, pre-tag, and one option raises or removes the cap. Recorded as a default change under the project's compatibility rule.
- **A stop now disconnects every client on the instance at once** → Jittered reconnect delays spread the return, and a load balancer routes some of them to healthy instances. The alternative is today's behaviour, where nobody is disconnected and nobody receives anything.
- **Reconnects arrive while the hub is still down and are refused** → Expected and correct: refusal is cheap, the client has a delay, and the store is still readable over the ordinary endpoints, which is what a client falls back to.
- **A host transport ignores the closure channel and stays stale** → The exported godoc states that a transport must select on it, and the same channel is what tells a transport its slot was freed. A shared transport conformance suite would close this properly; that is deferred with the one in `fix-websocket-write-authorization` D5 until a second host transport exists.
- **`endRun` closing subscriptions while a transport is mid-write** → Closing only wakes the loop; it touches no connection. Each transport's own write deadline still bounds the write in flight.

## Migration Plan

1. No data migration, no schema change.
2. Hosts below the default cap and using `SelfOnly`: nothing to do. Streams now end on a broker outage instead of going silent, which is the fix.
3. Hosts serving more than 10,000 concurrent streams per instance: set the option to their real bound, or remove the cap explicitly if they bound connections upstream.
4. Clients: an SSE client needs no change (the browser honours `retry:`); a WebSocket client should treat going away as "reconnect with jittered backoff", which the docs state.
5. Rollback is reverting the change; nothing is persisted that a rollback would strand.

## Open Questions

- Whether the reconnect delay should also be written as the stream ends, when the client is still readable, in addition to at open. Deferrable: it changes neither the spec's requirement nor the task breakdown, and the value at open already covers every closure path.
