## Context

See `proposal.md` — Why, for the two findings and their failing proofs. The code that decides each is:

| Where | What |
| --- | --- |
| `http.go:127-133` | the five routes, each wrapped by `h.acting(...)`, and also returned by `Handler.Routes()` for routers that register routes one at a time |
| `http.go:153-170` | `acting`: resolves the actor and refuses an empty one. **No origin check.** |
| `http.go:285-317` | `markAllRead`: reads up to 64 KiB and `json.Unmarshal`s it. **Ignores `Content-Type`.** |
| `http.go:352` | `h.hub.Subscribe(recipient)`: the actor is known here but not passed on |
| `websocket/handler.go:224, 248` | `checkOrigin` (host-only compare, patterns, `WithAnyOrigin`), then `h.hub.Subscribe(recipient)` |
| `hub.go:363-395` | `Subscribe(recipient)`: instance cap `h.streams >= h.maxTotal`, then **`len(h.subscriptions[recipient]) >= h.maxStreams`** |
| `hub.go:465-481` | `closeLocked`: removes the subscription from its recipient's set and decrements `streams` |

Constraints:

- **The core is standard-library-only** in production code. `net/http.CrossOriginProtection` has been in the standard library since Go 1.25, and the toolchain here is 1.26.8.
- **No tag has been cut** (`docs/releasing.md`: nothing is tagged while `pkg/sqlkit/` exists). Rule 7 of `library-design.md` therefore lets a changed default or a renamed API be recorded rather than versioned.
- **The WebSocket transport already refuses foreign origins by default**, with an allow-list and a named opt-out: `WithOriginPatterns` and `WithAnyOrigin`. Refusing them on HTTP brings the two transports into line.

## Goals / Non-Goals

**Goals**

- By default, a browser page on another origin cannot change a user's notifications through the HTTP contract.
- A follower's streams count against the follower. The default `SelfOnly` behaves exactly as today.
- Every new default has a named override, and every contradictory configuration fails in the constructor.

**Non-Goals**

- CORS. The library sets no `Access-Control-*` headers. A host that serves a cross-origin front end configures CORS in its own middleware, and adds that origin with `WithTrustedOrigins`.
- Changing the WebSocket origin check, which compares hosts only (the Unverified note in the proposal).
- Changing how a subscription policy's error is mapped (the proposal's note, and Open Questions below).
- A cap per recipient on how many followers may follow them. The instance cap bounds fan-out in total; see Risks.
- Authenticating anyone. The host still establishes the acting user.

## Decisions

### D1. Cross-origin writes are refused by `net/http.CrossOriginProtection`, inside `acting`

`NewHandler` builds one `*http.CrossOriginProtection`, unless the host opts out. `acting` calls its `Check(r)` before calling the host's actor function. A refusal is answered by `WriteError` with `fmt.Errorf("%w: a cross-origin browser request may not change notifications", ErrUnauthorized)`, which gives `403 forbidden` in the shared error body.

Why this mechanism:
- **It is the standard library's own implementation of the current guidance.** It uses `Sec-Fetch-Site` first (every major browser has sent it since 2023), then compares the `Origin` host with `Host`. It lets through requests that carry neither header, because those come from non-browser clients or same-origin navigation. `GET`, `HEAD` and `OPTIONS` always pass.
- **No token or cookie is needed.** The library owns no session and sets no cookies (rule 5), so a synchroniser token has nowhere to live.
- **Putting it in `acting` covers every route.** That includes the routes a host registers one at a time from `Routes()`. Checking in `ServeHTTP` alone would miss those. The read routes pass through the check too, and are never refused, because they are safe methods.
- **The origin is checked before the actor is resolved.** So a refused request never reaches the host's actor function, which may cost a session lookup.

Alternatives considered:
- **Reuse the WebSocket module's `checkOrigin`.** It compares hosts only, ignores `Sec-Fetch-Site`, and lives in another module that the core cannot import. Rejected.
- **Require a custom header**, such as `X-Requested-With`, on writes. That breaks every existing non-browser client for no gain over `Sec-Fetch-Site`. Rejected.
- **Document the risk and leave it to hosts.** Rule 1 says the safe default wins, and the WebSocket transport already refuses. Rejected.

**Default:** on. Only the request's own origin may change state from a browser.
**Override:** `WithTrustedOrigins(origins ...string)` adds exact origins through `AddTrustedOrigin`. `WithoutCrossOriginProtection()` turns the check off.
**Wiring mistakes (rule 6):**
- An origin that `AddTrustedOrigin` rejects, such as one with no scheme, a path, or a query, is a `ConfigurationError` from `NewHandler`, naming the origin.
- Trusted origins together with the opt-out is a `ConfigurationError` too. This mirrors `WithMaxStreamsPerInstance` with `WithoutMaxStreamsPerInstance` in `hub.go:161`.

**Naming.** `WithTrustedOrigins` follows the standard library's term: the value is a full origin, matched exactly. It differs from the WebSocket module's `WithOriginPatterns`, which takes host glob patterns. A different name keeps the two from being mistaken for each other. `WithoutCrossOriginProtection` follows the `Without…` form of `WithoutMaxStreamsPerInstance`.

### D2. A `read-all` body must be declared `application/json`

When the body is not blank, `markAllRead` parses `Content-Type` with `mime.ParseMediaType`. Any media type other than `application/json` is refused with `400 validation_failed`, issue pointer `/`, detail `must be sent with Content-Type application/json`. That includes a missing header, `text/plain`, `application/x-www-form-urlencoded` and `multipart/form-data`. Parameters such as `charset=utf-8` are accepted. An empty body needs no header, as today.

Why:
- `text/plain`, form encoding and multipart are the three types a browser sends cross-site with no CORS preflight.
- A body declared as JSON from another origin forces a preflight, which the library never answers with permission.
- So even with D1 opted out or bypassed, a JSON-bodied write can only come from a same-origin page or a non-browser client.
- It also makes the parse honest: a body is read as what it says it is.

*Alternative considered:* answer `415 Unsupported Media Type`. That is more precise, but it adds a status and a code to the one error vocabulary (`WriteError`) for a case that is plainly a malformed request. Rejected.

**Default:** required. **Override:** none. The body is part of the contract. A client that posts JSON declares it, and a host with a legacy client can wrap the route and set the header itself, which the design does not recommend. This is stated as a limit (rule 4), not relaxed silently.

### D3. The stream cap is keyed on the acting user

- `Hub.Subscribe(recipient string)` becomes `Hub.Subscribe(actor, recipient string)`.
- The hub keeps `opened map[string]int`: open subscriptions per actor. `Subscribe` checks `h.opened[actor] >= h.maxStreams` in place of `len(h.subscriptions[recipient]) >= h.maxStreams`.
- `Subscription` records its `actor`. `closeLocked` decrements `opened[actor]`, and deletes the key at zero. `closeSubscriptions` resets the map with the rest of the run state.
- Delivery is unchanged: it is still indexed by recipient.
- The refusal reads `%d streams are already open for this user`.

Why per-actor, not per-recipient plus something:

| Option | Mallory locks Alice out | Mallory takes the instance | Behaviour under `SelfOnly` |
| --- | --- | --- | --- |
| per-recipient (today) | **yes** | **yes** | — |
| per-recipient + per-actor | **yes**: her streams still fill Alice's recipient cap | no | unchanged |
| per-recipient, counting only the recipient's own streams + per-actor | no | no | unchanged, but two caps, two options and two messages |
| **per-actor only** | **no** | **no** (at most the per-user cap) | **unchanged**, since actor = recipient |

Per-actor alone closes both findings, with one cap and one option, and changes nothing for the default policy. What it gives up is a limit on how many followers one recipient's signals fan out to. That was never the per-recipient cap's purpose: its godoc says it bounds "how many streams one recipient may hold open". The instance cap still bounds total fan-out.

Why change the signature rather than add `SubscribeAs`:
- A transport of a host's own that keeps calling `Subscribe(recipient)` would silently keep the defect. Changing the signature makes it a compile error.
- No tag exists, so the break is free (rule 7). It is recorded in the proposal as **BREAKING**.

**Renames:** `DefaultMaxStreamsPerRecipient` becomes `DefaultMaxStreamsPerActor`, and `WithMaxStreamsPerRecipient` becomes `WithMaxStreamsPerActor`, both through `gopls rename` across the workspace. The value stays 8, and "actor" is the term `WithActor` already uses. Keeping the old names over new semantics would make them lie.

**Default:** 8 streams per acting user per instance, across both transports. **Override:** `WithMaxStreamsPerActor(n)`, which must be at least 1. The instance cap must still be at least the per-user cap, as today.

### D4. The WebSocket transport passes the actor, and changes nothing else

`websocket/handler.go:248` becomes `h.hub.Subscribe(actor, recipient)`. Its origin check and options are untouched (Non-Goals). The new red case in `TestHandshakeRefusals` proves that a supervisor's connections no longer fill bob's cap.

## Risks / Trade-offs

- **[A proxy rewrites `Host`, and an old browser sends no `Sec-Fetch-Site`]** → `CrossOriginProtection` then compares `Origin` against the rewritten `Host` and refuses a same-origin write. Every browser shipped since 2023 sends `Sec-Fetch-Site`, which is decided first. A host behind such a proxy adds its public origin with `WithTrustedOrigins`. The docs say so. **Unverified** here: no such proxy is in the test setup.
- **[A host serves its front end from another origin and already relies on CORS]** → Its writes are now refused until it adds `WithTrustedOrigins("https://front.example")`. This is the intended safe default. The docs and the proposal's Hosts section say so.
- **[A client posts a JSON body without `Content-Type`]** → It now gets `400`. Browser `fetch` with a string body sends `text/plain`, so such clients exist. The docs state the rule, and the error detail names the header to send.
- **[One recipient is followed by very many users]** → Nothing limits followers per recipient. Each follower pays from their own per-user cap, and the instance cap bounds the total. A host whose policy allows mass following limits that in the policy itself.
- **[Many distinct actors, a Sybil attack]** → Each gets their own per-user cap. Who counts as an actor is the host's authentication. The instance cap is the backstop. This is unchanged in kind from today.
- **[A host's own transport calls `Hub.Subscribe`]** → It fails to compile until it passes the actor. That is intended (D3).

## Migration Plan

1. **Hosts on the default configuration, same origin:** nothing.
2. **Hosts with a front end on another origin:** add `ntfy.WithTrustedOrigins("https://front.example")` to `NewHandler`.
3. **Hosts whose actor comes only from a bearer header**, and which want no origin checks: optionally add `ntfy.WithoutCrossOriginProtection()`.
4. **Clients posting a `read-all` body:** send `Content-Type: application/json`.
5. **Hosts using the renamed option or `Hub.Subscribe`:** rename to `WithMaxStreamsPerActor` or `DefaultMaxStreamsPerActor`, and pass the acting user as the first argument to `Subscribe`.
6. **Rollback:** the previous commit. There is no stored state to migrate.

## Open Questions

- **Should a subscription policy be able to signal "unavailable" rather than "forbidden"?** Today every policy error is `403` with its message (`subscriptionRefusedError`). A host whose policy fails transiently, for example because its database is down, shows users `403` rather than a retryable `503`. The contract is documented and deliberate, so this change leaves it alone. Answering it could add a scenario to "Subscriptions are authorized…". It changes neither this change's specs nor its tasks, so it is deferred to the maintainer.
