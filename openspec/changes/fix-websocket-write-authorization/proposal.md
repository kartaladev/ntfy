## Why

A `SubscriptionAuthorizer` is documented as deciding whether an acting user may **follow** a recipient's signals, and the library offers `AllowAll` and suggests a supervisor policy as the example. On the WebSocket transport, that follow grant is also a **write** grant: `ServeHTTP` takes the recipient from the query parameter and hands that recipient, not the acting user, to `mark-read` and `mark-all-read`. A supervisor permitted to watch a report's inbox can clear it, and the report has no record that it happened.

The HTTP transport does not have this hole: every endpoint acts on the acting user and refuses a recipient parameter. So the two transports of one contract disagree about who may write, and the current realtime spec codifies the weaker of the two ("apply each request to the connection's recipient only"). Fixing it before the first tag costs nothing; afterwards it is a breaking change to a published contract.

## What Changes

- **A subscription policy grants following only.** Marking read over a WebSocket acts on the **acting user**, never on the followed recipient, matching the HTTP contract.
- **A mark request on a followed connection is refused**, rather than silently retargeted at the acting user's own inbox: a write that quietly acts on something other than what the client named is its own surprise. The refusal reuses the contract's existing forbidden vocabulary.
- **BREAKING** (pre-tag, `ntfy/websocket`): a host whose policy permits following another recipient loses the ability to mark that recipient's notifications read over the connection. No behaviour changes under the default `SelfOnly`, where the recipient and the acting user are the same.
- **The ports say what they grant.** `SubscriptionAuthorizer`, `SelfOnly` and `AllowAll` state that they authorize following only and confer no write authority; `AllowAll`'s godoc names the escalation it does *not* create.
- **Transport authority becomes a tested property, not a convention.** A conformance test asserts that, for the same policy and the same request, the SSE and WebSocket transports grant identical authority.

No new option is introduced. A host that genuinely wants delegated writes has the HTTP contract and its own middleware; if that turns out to be a real requirement, a separate write-authorization port is a later change, not a flag bolted onto a read policy.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-realtime`: the mark-read-over-WebSocket requirement changes from "the connection's recipient" to "the acting user", and the subscription-authorization requirement states that the policy grants following only and confers no authority to change anything.

## Impact

- **Code:** `websocket/handler.go` — `ServeHTTP` passes the actor to `serve`/`read`/`answer` (`:260`, `:286`, `:364`, `:406`), and `answer` refuses a mark request when the connection follows another recipient. Godoc on `authorize.go` (`SubscriptionAuthorizer`, `SelfOnly`, `AllowAll`) and on `websocket/handler.go`'s `answer`, whose comment currently presents the defect as a safety property.
- **APIs:** no signature changes. The WebSocket reply vocabulary gains the contract's forbidden code for a refused mark on a followed connection.
- **Tests:** the escalation as a failing test first (actor follows another recipient under `AllowAll`, sends `mark-all-read`, victim's notifications must be untouched); a cross-transport authority test covering SSE and WebSocket under the same policy.
- **Docs:** `docs/realtime-operations.md`, whose transport comparison currently reads as though one policy covers both following and marking read.
- **Not in scope:** the other findings from the same audit (successor aliasing, unbounded query filters, stopped-hub streams, backoff overflow, the `AtLeastOnce` documentation contradiction) are separate changes.
