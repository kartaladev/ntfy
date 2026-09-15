## Context

See `proposal.md` — Why. The defect is reproducible today: with `WithSubscriptionAuthorizer(ntfy.AllowAll)`, an actor opening a WebSocket connection with `?recipient=<victim>` and sending `{"type":"mark-all-read"}` clears the victim's unread state, while the same actor is answered `404` by the HTTP contract.

The relevant constraints:

- `websocket/handler.go:227-236` resolves the recipient from the query parameter, authorizes it as a *subscription*, and then carries that recipient — not the actor — through `serve` → `read` → `answer`, where it becomes the subject of `MarkRead` and `MarkAllRead` (`:421`, `:423`).
- The actor is already known at `:208` and is simply not passed down.
- `http.go:237-290` is correct and needs no change; it never accepts a recipient parameter for a write.
- `SubscriptionAuthorizer` lives in the core module (`authorize.go`), and the WebSocket transport is a separate module that imports it.
- Nothing is tagged yet (`docs/releasing.md`), so a behaviour change here costs no compatibility.

## Goals / Non-Goals

**Goals**

- Write authority on a WebSocket connection is the acting user's, on every policy.
- The two transports of one contract cannot drift apart again without a test failing.
- The ports state what authority they grant, so a host reading the godoc cannot reach the wrong conclusion.

**Non-Goals**

- Delegated writes in any form. No option, flag or port in this change.
- The SSE `?recipient=` parameter, which is read-only and stays as it is.
- The other audit findings (successor aliasing, unbounded query filters, stopped-hub streams, backoff overflow, the `AtLeastOnce` documentation contradiction). Each is its own change.
- Any change to `SelfOnly`'s behaviour, which is already correct.

## Decisions

### D1. Writes act on the acting user, never on the followed recipient

The actor is threaded from `ServeHTTP` into `serve`, `read` and `answer`. `answer` calls the service with the actor; the connection's recipient stays what it always was — the subscription's subject, used for signal delivery only.

*Alternative considered:* keep recipient-scoped writes and add a `WriteAuthorizer` port now, so a host can opt into delegation. Rejected: there is no demonstrated need, and a second policy port invites exactly the conflation being removed — two policies that look interchangeable, one of which is destructive. It can be added later without breaking anyone, because widening authority is additive.

**Default:** writes act on the acting user. **Override:** none, deliberately — see D3.

### D2. A mark request on a followed connection is refused, not silently retargeted

When the connection's recipient is not the actor, `answer` returns a forbidden reply echoing the request reference. The connection stays open and keeps delivering the followed recipient's signals, because following was legitimately authorized.

*Alternatives considered:*

- **Silently apply to the actor's own inbox.** Rejected: the client asked to mark one inbox and a different one changes, with a success reply. A quiet retarget is a second surprise layered on the first.
- **Close the connection.** Rejected: the subscription is valid; a client bug on the write path should not end a legitimate read stream.

The forbidden code already exists in the contract's error vocabulary (`http.go:431-433`), so this adds a case to `errorReply`, not a new concept.

**Default:** refused. **Override:** none — a host that wants delegated marking uses the HTTP contract behind its own authorization.

### D3. No override point, and why

Library rule 2 says every default is replaceable without forking, and rule 4 says a limit on flexibility is stated rather than silently imposed. This decision takes the second path: the flexibility is removed and the line is documented.

An override here would be an option that re-enables writing to another recipient's inbox from a policy whose documented purpose is following. That is the defect, re-offered as configuration. A host with a genuine delegation requirement has the HTTP contract, where it supplies its own authorization; if the requirement turns out to be common, the answer is a distinct write-authorization port with its own name, godoc and default — a separate decision, made on evidence, not a flag added here.

### D4. The godoc is part of the fix

`SubscriptionAuthorizer`, `SelfOnly` and `AllowAll` state that they authorize following only and grant no authority to change anything. `AllowAll` names the escalation it does not create. The comment on `answer` (`websocket/handler.go:404-405`), which currently presents the defect as a safety property, is corrected.

This is not cosmetic: the supervisor example in `authorize.go:13` is what leads a host into the hole, and a fix that leaves the example unqualified invites the same mistake against a future write port.

### D5. The cross-transport test lives in the WebSocket module

It needs both transports, and that module already imports the core. Putting it in `ntfytest` would mean `ntfytest` importing `websocket`, which inverts the dependency direction that `docs/releasing.md` relies on for release order.

*Alternative considered:* a shared exported suite so third-party transports can assert the same property. Deferred — there is one satellite transport today, and the suite can be extracted when a second appears.

## Risks / Trade-offs

- **A host is relying on delegated writes today** → It breaks loudly, with a forbidden reply naming the reason, not silently. Nothing is tagged, so no released contract changes. The migration note gives the HTTP alternative.
- **A client sends mark-read unconditionally on every connection** → On a followed connection it now receives an error frame it may not handle. Mitigated by the reply reusing an existing code and by the connection staying open; noted in the docs.
- **The same conflation returns in a future transport** → The cross-transport authority test (D5) is the guard. A third transport must be added to it, which the test's name and comment say explicitly.
- **The refusal is the wrong call for a real delegation use case** → Reversible: turning a refusal into an authorized write later is additive, whereas shipping delegation now and withdrawing it is not.

## Migration Plan

1. No data migration, no schema change, no configuration change.
2. Hosts using the default `SelfOnly`: nothing to do; behaviour is identical.
3. Hosts using `AllowAll` or a custom policy that permits following another recipient: connections following someone else can no longer mark that recipient's notifications read. Move any such flow to the HTTP contract, authorized by the host's own middleware.
4. Rollback is reverting the change; no state is written that a rollback would strand.

## Open Questions

- Whether a distinct write-authorization port is eventually wanted, and what its default would be. Deferrable: adding it later changes neither these specs nor this task breakdown, and the decision should rest on a real host requirement rather than on this defect.
