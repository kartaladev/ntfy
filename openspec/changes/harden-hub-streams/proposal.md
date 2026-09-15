## Why

A hub that stops receiving signals refuses *new* streams as unavailable, but says nothing to the ones already open. `Hub.Running` is consulted only by `Subscribe`; neither transport's loop re-checks it. After `redis.Listen` or `nats.Listen` ends — a broker outage, a cancelled run — every connected client keeps its connection, keeps receiving heartbeats, and receives no signal ever again. The library's stated recovery, "a client that was closed reconnects … and re-reads the store", never fires, because nothing closes the client. Every open badge stays stale until some later change happens to arrive.

The same hub bounds streams per recipient, at 8, and not at all per instance. `Subscribe` is the only gate and `subscriptions` grows with every distinct recipient, so nothing bounds an instance's total streams, file descriptors or memory. One cap protects a single user from a runaway client; nothing protects the instance, and the planned multi-tenant standalone service makes that gap structural rather than theoretical.

## What Changes

- **A stream ends when its instance stops receiving signals.** A subscription is closed when the hub's run ends, and both transports notice and end the stream: the server-sent event stream returns, and the WebSocket connection closes as going away, exactly as it already does on shutdown. The client then reconnects and re-reads, which is the recovery the docs already promise.
- **A closed stream says when to come back.** The server-sent event stream carries a reconnect delay, jittered per connection, so an instance's clients do not return in one wave. The delay has a default and an override.
- **A new default bounds an instance's total streams**, counting both transports, alongside the existing per-recipient cap. Beyond it a stream is refused as too many requests, the same vocabulary the per-recipient cap uses.
- **BREAKING** (pre-tag, `ntfy` and `ntfy/websocket`): an instance serving more than the default total of streams begins refusing them. A host at that scale raises the cap or removes it explicitly.
- **Wiring mistakes about the caps fail at construction:** a total cap below the per-recipient cap makes the per-recipient cap unreachable, and a cap both set and removed is contradictory. Both are configuration errors.
- **The shutdown recipe covers both transports in the docs.** `http.Server.Shutdown` alone ends neither a WebSocket (hijacked) nor a server-sent event stream (its request context is never cancelled); the documented `BaseContext` and `RegisterOnShutdown` pair ends both. The current section explains it only in terms of WebSocket, so a host serving only streams skips it.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-realtime`: the stream-lifecycle requirement gains the rule that an open stream ends when its instance stops receiving signals, the reconnect hint, and an instance-wide stream cap with its default, override and construction errors. The WebSocket requirement gains the same lifecycle rule and states that the instance-wide cap counts both transports.

## Impact

- **Code:** `hub.go` — `Subscription` gains a closure channel closed by `endRun` (`:178-190`); `Subscribe` (`:250-272`) enforces the instance cap and must observe the stop atomically with its insert, since today it checks `Running` outside `h.mu` and could hand out a live subscription to a hub that stopped in between. `http.go` — the stream loop (`:359-377`) selects on the closure channel and writes the reconnect hint. `websocket/handler.go` — the serve loop (`:307-343`) selects on it and closes with going away.
- **APIs:** additive on the core — one method on `Subscription`, and options for the instance cap and the reconnect delay. No signature changes. `ErrTooManyStreams`'s godoc, which today describes only "a recipient over the per-instance stream cap", must distinguish the two caps.
- **Tests:** both defects proved first — a client that receives nothing and stays open after the hub stops, and subscriptions across distinct recipients growing without bound. Then the cap's default, an override, a removal, and the construction errors; `goleak` already guards the transports.
- **Docs:** `docs/realtime-operations.md` — the shutdown section, the defaults tables, and the operational note that a broker outage now ends streams rather than silencing them.
- **Not in scope:** the per-recipient cap's default, the broadcaster's own reconnection behaviour (it already resubscribes on its own), and the other audit findings, each of which is its own change. The write-authorization defect on the WebSocket transport is `fix-websocket-write-authorization`.
