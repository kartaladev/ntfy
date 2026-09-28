## Why

The 2026-09-28 security audit found two faults in the HTTP transport. Both were re-proved on current `main` (`20967b3`) on 2026-09-28.

**1. Cross-site requests can mark a user's notifications read (CSRF). Low to Medium.**
- The two routes that change state, `POST /v1/notifications/{id}/read` and `POST /v1/notifications/read-all`, do not check where a request came from (`http.go:130-131`, `markRead` at `:264`, `markAllRead` at `:285`).
- `markAllRead` parses its body as JSON whatever `Content-Type` says.
- A host that finds the acting user from a cookie session is therefore exposed. A page on `evil.example` can auto-submit a plain HTML form to `/v1/notifications/read-all`, and the victim's notifications are all marked read. The harm is to integrity only: nothing is disclosed, because the attacker cannot read the response.
- Nothing documents this. The WebSocket transport already refuses foreign browser origins by default, so the two transports are inconsistent.
- Go 1.25 added `net/http.CrossOriginProtection`, so the core can close this without leaving the standard library. Rule 1 of `.claude/rules/library-design.md` says the safe default wins, and the convenient behaviour becomes an explicit option.

**2. The stream caps count the recipient being followed, not the user who opens the stream. Low.**
- The per-recipient cap is keyed on `recipient` in `Hub.Subscribe` (`hub.go:371-377`).
- This matters only when the host has replaced the default `SelfOnly` policy with one that lets a user follow someone else, such as `AllowAll` or a supervisor policy. Under such a policy:
  - Mallory can open 8 streams following Alice, and Alice's own stream is then refused with `429`.
  - Mallory can follow many recipients, and alone use up the whole per-instance cap, which denies realtime to everyone on the instance.
- The default `SelfOnly` is unaffected, because under it the actor and the recipient are always the same.

**Proved by:** the audit's proofs, `TestAuditCrossSiteMarkRead` and `TestAuditFollowerExhaustsStreamCaps`, were copied into the root package and run with `GOTOOLCHAIN=go1.26.8 go test -run 'TestAudit' -count=1 .`. Every case failed on the current code:

```
--- FAIL: TestAuditCrossSiteMarkRead/read-all_from_a_cross-site_form
        expected: 403  actual  : 200
        Messages: cross-site POST was served: {"marked":1}
        Messages: a cross-site form marked the victim's notification read
--- FAIL: TestAuditCrossSiteMarkRead/read-all_from_a_cross-site_text/plain_form_carrying_a_JSON_body
        expected: 403  actual  : 200
        Messages: cross-site POST was served: {"marked":1}
--- FAIL: TestAuditCrossSiteMarkRead/mark_one_read_from_a_cross-site_form
        expected: 403  actual  : 200
        Messages: cross-site POST was served: {"id":"…","recipient":"alice",…,"state":"READ",…}
--- FAIL: TestAuditFollowerExhaustsStreamCaps/a_follower_takes_every_slot_of_the_recipient's_own_cap
        expected: 200  actual  : 429
        Messages: alice could not open their own stream because mallory holds the slots:
                  {"error":{"code":"too_many_streams","message":"ntfy: too many streams: 8 streams are already open for this recipient"}}
--- FAIL: TestAuditFollowerExhaustsStreamCaps/one_acting_user_takes_the_whole_instance_cap
        expected: 200  actual  : 429
        Messages: carol could not open their own stream because mallory holds the slots:
                  {"error":{"code":"too_many_streams","message":"ntfy: too many streams: 16 streams are already open on this instance"}}
```

The temporary audit files were deleted after the run. Task 1 lands them in the repository as `TestHandlerRefusesCrossOriginWrites` and `TestHandlerStreamCapsCountTheActingUser`, and adds a WebSocket case to `TestHandshakeRefusals`. Each is red for the reason above, and the red runs were recorded on 2026-09-28 (see `plans.md` Task 1).

### Recorded as notes, not findings

- **A subscription policy's error reaches the client as `403` with its own message.** This is the documented contract: `authorize.go` says the message "must be safe to show the caller", and `subscriptionRefusedError` maps any policy error to forbidden. One consequence is that a transient failure inside a host's policy, such as its database being down, is answered `403` rather than `5xx`, and the client will not retry. **Decision: not addressed in this change.** It is a deliberate contract, not a defect, and changing it would be a behaviour choice for the maintainer (see `design.md`, Open Questions).
- **The WebSocket origin check compares hosts only** (`websocket/handler.go:491-513`), so `http://host` passes on an `https://host` server. **Unverified:** no test shows harm. An attacker able to serve `http://host` already controls the network path to the application's own host name. Not planned here.

## What Changes

- **The HTTP handler refuses cross-origin browser requests that change state, by default.**
  - Every route is guarded by `net/http.CrossOriginProtection`. `GET`, `HEAD` and `OPTIONS` always pass, so in practice this guards the two `POST` routes.
  - A refused request is answered `403 forbidden` through `WriteError` before the host's actor function runs.
  - Requests from the application's own pages, and from non-browser clients that send neither `Origin` nor `Sec-Fetch-Site`, are served as before.
  - The guard also applies to routes a host registers one at a time from `Handler.Routes()`.
- **New `WithTrustedOrigins(origins ...string)`** permits named browser origins, such as a front end on a sibling subdomain. An origin that is not of the form `scheme://host[:port]` is a configuration error.
- **New `WithoutCrossOriginProtection()`** is the named opt-out, for a host whose acting user never comes from ambient browser credentials, or that checks this in its own middleware. Combining it with `WithTrustedOrigins` is a configuration error.
- **A non-empty `read-all` body must be declared `Content-Type: application/json`**, with parameters such as `charset` allowed. Any other type is a `400 validation_failed`, and nothing is marked. **BREAKING** for a client that posted JSON without the header. Browser `fetch` with a string body sends `text/plain`, which is exactly the type a cross-site form can send.
- **The per-recipient stream cap becomes a per-user cap**, keyed on the acting user who opens the stream, whichever recipient it follows. It is still 8 by default and still counts both transports. **BREAKING**, recorded rather than versioned, since no tag has been cut:
  - `DefaultMaxStreamsPerRecipient` is renamed to `DefaultMaxStreamsPerActor`, and `WithMaxStreamsPerRecipient` to `WithMaxStreamsPerActor`;
  - `Hub.Subscribe(recipient)` becomes `Hub.Subscribe(actor, recipient)`, so a transport of a host's own must say who is subscribing;
  - the refusal reads `N streams are already open for this user`.
- **Docs** (`docs/notifications.md`, `docs/realtime-operations.md`) and godoc state the new defaults, both new options, the JSON body rule and the per-user cap.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-http-api`:
  - adds a requirement that endpoints which change notifications refuse cross-origin browser requests by default, with a trusted-origin list and a named opt-out;
  - adds that the `read-all` body must be declared as JSON;
  - the error mapping names the per-user stream cap and the cross-origin refusal.
- `notification-realtime`: the stream cap counts the acting user's streams rather than the followed recipient's. This modifies the requirements "Streams stay alive, and slow clients never slow publishers" and "Clients can receive change signals over a WebSocket connection".

## Impact

- **Code:**
  - `http.go`: the cross-origin guard in `acting`, `WithTrustedOrigins`, `WithoutCrossOriginProtection`, the constructor checks, and the `Content-Type` check in `markAllRead`.
  - `hub.go`: the `Subscribe(actor, recipient)` signature, the per-actor count, and the renamed constant and option.
  - `errors.go`: the `ErrTooManyStreams` godoc.
  - `websocket/handler.go`: passes the actor to `Subscribe`. `websocket/doc.go`: its godoc.
- **Tests:**
  - `http_test.go`, `hub_test.go` and `websocket/refusal_test.go` gain the landed proofs and the new cases.
  - `hub_test.go`, `http_test.go`, `docs_test.go` and four `websocket/*_test.go` files are updated mechanically for the rename and the new signature.
- **Docs:** `docs/notifications.md`, `docs/realtime-operations.md`.
- **Hosts:**
  - A host that serves a browser front end from another origin adds `WithTrustedOrigins`.
  - A host that uses bearer tokens only may add `WithoutCrossOriginProtection`, but does not need to: a bearer-token client is not a cross-site browser form.
  - Clients that post a `read-all` body send `Content-Type: application/json`.
  - A host calling `WithMaxStreamsPerRecipient`, or `Hub.Subscribe` from its own transport, renames the call or adds the actor.
- **Dependencies:** none. `net/http.CrossOriginProtection` is in the standard library, and the core stays standard-library-only.
