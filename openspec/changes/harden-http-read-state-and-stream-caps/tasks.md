## 1. Prove the defects (red)

- [ ] 1.1 Land the audit's CSRF proof as `TestHandlerRefusesCrossOriginWrites` in `http_test.go`, in the `table-test` form. It has nine cases:
  - refused: read-all from a cross-site form; read-all from a cross-site `text/plain` form with a JSON body; marking one read from a cross-site form; a foreign `Origin` without `Sec-Fetch-Site`; a `same-site` sibling origin that is not trusted;
  - served: a same-origin page; a client that sends neither header; a cross-site `GET` of the count.

  Also add the standalone `TestHandlerRoutesRefuseCrossOriginWrites`. It mounts `Handler.Routes()` one at a time on a fresh mux and sends a cross-site read-all.

  Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerRefusesCrossOriginWrites|TestHandlerRoutesRefuseCrossOriginWrites' -count=1 .`. The five refused cases and the routes test must fail with `expected: 403 actual: 200`, and "a cross-site request marked the victim's notification read". The three served cases must pass.
- [ ] 1.2 Land the audit's stream-cap proof as `TestHandlerStreamCapsCountTheActingUser` in `http_test.go`, with two cases: a follower takes the recipient's cap, and one actor takes the instance cap (`WithMaxStreamsPerRecipient(8)`, `WithMaxStreamsPerInstance(16)`). Mallory's streams are opened with `openStream`, which does not require `200`, so the test stays valid once her extra streams are refused. Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerStreamCapsCountTheActingUser' -count=1 .`. Both must fail with `expected: 200 actual: 429`, and their messages `8 streams are already open for this recipient` and `16 streams are already open on this instance`.
- [ ] 1.3 Add the case "a supervisor's connections leave the followed recipient's own cap untouched" to `TestHandshakeRefusals` in `websocket/refusal_test.go`. `sup` dials bob `DefaultMaxStreamsPerRecipient` times, then bob dials for himself. Verify with `cd websocket && GOTOOLCHAIN=go1.26.8 go test -run 'TestHandshakeRefusals' -count=1 .`. It must fail with `failed to WebSocket dial: expected handshake response status code 101 but got 429`.
- [ ] 1.4 Add two cases to `TestHandlerMarkRead`:
  - "a body not declared as JSON is a bad request, and marks nothing", over no header, `text/plain`, form encoding and multipart;
  - "a JSON body with a charset parameter is accepted".

  Make `httpEnv.do` set `Content-Type: application/json` whenever it sends a body. Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerMarkRead' -count=1 .`. The first case must fail with `expected: 400 actual: 200` for each type, and every existing case must still pass.
- [ ] 1.5 Record the four red outputs from 1.1–1.4 in `plans.md`'s execution record. Each red test is committed with the code that turns it green (tasks 2–4), per `golang-tdd.md`. Verify that the record names each test and its failing line.

## 2. Refuse cross-origin writes (green)

- [ ] 2.1 Add the `crossOrigin *http.CrossOriginProtection` field to `Handler`, build it in `NewHandler`, and call `Check` at the top of `acting`. A refusal is answered `WriteError(w, fmt.Errorf("%w: a cross-origin browser request may not change notifications", ErrUnauthorized))`, before the host's actor function runs. Update the `NewHandler` and `acting` godoc. Verify that `TestHandlerRefusesCrossOriginWrites` passes, and that `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .` is green apart from `TestHandlerMarkRead`'s 1.4 case.
- [ ] 2.2 In `markAllRead`, refuse a non-blank body whose media type, parsed with `mime.ParseMediaType`, is not `application/json`. Answer it with a `ValidationError` issue at pointer `/`, detail `must be sent with Content-Type application/json`. Update the godoc. Verify that `TestHandlerMarkRead` passes in full.

## 3. Let the host override cross-origin protection

- [ ] 3.1 Add `WithTrustedOrigins(origins ...string)` and `WithoutCrossOriginProtection()` as options that do nothing yet, with their final godoc. Add two cases to `TestHandlerRefusesCrossOriginWrites`: "a sibling site the host trusts is served" and "every origin is served after the explicit opt-out". Add two cases to `TestNewHandler`: "a trusted origin that is not an origin is refused" and "trusted origins with the cross-origin opt-out are refused". Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerRefusesCrossOriginWrites|TestNewHandler' -count=1 .`. The two served cases must fail with `expected: 200 actual: 403`, and the two constructor cases with `An error is expected but got nil`.
- [ ] 3.2 Implement both options. `NewHandler` refuses the combination, adds each trusted origin through `AddTrustedOrigin`, and turns an error into a `ConfigurationError` naming the origin. The opt-out leaves `crossOrigin` nil. Verify that the four cases pass, then `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .`.

## 4. Count stream caps per acting user

- [ ] 4.1 Refactor with no change in behaviour:
  - `gopls rename` `DefaultMaxStreamsPerRecipient` to `DefaultMaxStreamsPerActor`, and `WithMaxStreamsPerRecipient` to `WithMaxStreamsPerActor`, across the workspace;
  - change `Hub.Subscribe(recipient)` to `Hub.Subscribe(actor, recipient)`, still counting by recipient;
  - pass the actor from `http.go` and `websocket/handler.go`;
  - rewrite the test call sites mechanically.

  Verify that both modules build, and that `go test -count=1 .` and `cd websocket && go test -count=1 ./...` fail only on 1.2, 1.3 and `TestTheDocumentMatchesTheImplementation`. The doc needles are fixed in 4.4.
- [ ] 4.2 Add `TestHubCapsCountTheActingUser` to `hub_test.go`, three cases under `WithMaxStreamsPerActor(2)`:
  - a follower's streams leave the recipient's own cap untouched;
  - a follower is capped across every recipient they follow;
  - closing a follower's stream frees the follower's slot.

  Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestHubCapsCountTheActingUser' -count=1 .`. The first case must fail with `2 streams are already open for this recipient`, and the second with `Expected error with "ntfy: too many streams" in chain but got nil`.
- [ ] 4.3 Count per actor in `hub.go`:
  - add the `opened map[string]int` field and the `Subscription.actor` field;
  - check `h.opened[actor] >= h.maxStreams`, with the message `%d streams are already open for this user`;
  - increment in `Subscribe`, decrement and delete at zero in `closeLocked`, and reset in `closeSubscriptions`;
  - reword the godoc and configuration errors from "per-recipient" to "per-user", including `errors.go`'s `ErrTooManyStreams`.

  Verify that 1.2, 1.3 and 4.2 pass, then run `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .` and the same in `websocket`.
- [ ] 4.4 Rename the cap in the docs, so the commit lands green:
  - in `docs/notifications.md`, the Realtime table row becomes "Streams per acting user on one instance" with `WithMaxStreamsPerActor`;
  - in `docs/realtime-operations.md`, the "per-user cap of 8 connections per instance" sentence and the hub table row;
  - `websocket/doc.go`, and the `ServeHTTP` godoc in `websocket/handler.go`.

  Verify that `GOTOOLCHAIN=go1.26.8 go test -count=1 .` and `cd websocket && go test -count=1 ./...` are fully green.

## 5. Document

- [ ] 5.1 Update `docs/notifications.md`:
  - Realtime: a bullet saying a follower's streams count against the follower;
  - HTTP section: a bullet on cross-origin protection naming `WithTrustedOrigins` and `WithoutCrossOriginProtection`, one on `Content-Type: application/json`, and error rows for "too many streams for one user or for the instance" and "a cross-origin browser write".

  Add needles to `docs_test.go`: `funcName(ntfy.WithTrustedOrigins)`, `funcName(ntfy.WithoutCrossOriginProtection)`, `"Content-Type: application/json"`. Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .`: red on the new needles before the doc edit, green after. `cd websocket && go test -run 'TestTheOperationsGuideMatchesTheHandler' -count=1 .` must pass too.

## 6. Verify and hand off

- [ ] 6.1 Run `make all` and `make store-matrix`, and record the results in `plans.md`'s execution record. No store changes, so the matrix is a regression check only. Verify that `grep -rn 'MaxStreamsPerRecipient\|per-recipient cap\|for this recipient' --include='*.go' --include='*.md' . | grep -v openspec/changes/archive` prints nothing outside `openspec/specs/`, which archive syncs.
