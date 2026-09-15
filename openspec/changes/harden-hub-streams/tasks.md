## 1. Prove the defects (red)

- [ ] 1.1 Add a failing test in the root module that opens a stream, stops the hub's run, and asserts the stream ends: with the code as it stands the subscription stays live and the client keeps receiving only heartbeats. Verify with `go test -run 'TestStreamEndsWhenHubStops' -count=1 ./...` and confirm it fails because the stream is still open, not because of a wiring or compile error (see `.claude/rules/prove-errors-with-tests.md`).
- [ ] 1.2 Add a failing test that subscribes across many distinct recipients on one hub and asserts a total cap is enforced, proving today's `Subscribe` bounds only the per-recipient count. Verify it fails because every subscription was accepted.
- [ ] 1.3 Add a failing test for the stop-during-subscribe window: with the run ending concurrently with `Subscribe`, assert no subscription is handed out that is already orphaned (it must either be refused as unavailable or be closed). Run it with `-race -count=100` and confirm it fails or flakes against the current code, which reads the running state outside the subscription lock (`design.md` D2).
- [ ] 1.4 Add failing transport tests: an SSE stream and a WebSocket connection that are open when the instance stops receiving, asserting the stream returns and the connection closes as going away. Verify both fail today with the client still connected.

## 2. End a stream when its instance stops (green)

- [ ] 2.1 Add the closure channel to `Subscription` in `hub.go`, closed exactly once through the existing `sync.Once` it shares with `Close`, and export the accessor a transport selects on. Verify `go build ./...` and that the godoc states a transport must select on it (`design.md` D1).
- [ ] 2.2 Close every live subscription from `endRun` and clear the subscription map, holding `h.mu` inside `runMu`. Verify the 1.1 test passes and `go test -race -count=1 ./...` shows no lock-order problem.
- [ ] 2.3 Observe the running state under `h.mu` in `Subscribe`, together with the insert, so a run ending mid-subscribe cannot leave an orphaned subscription. Verify the 1.3 test passes under `-race -count=100`.
- [ ] 2.4 Add a test that a subscription closed by a stop is not revived when the hub runs again, and that a stream opened after the new run receives signals normally (spec scenario "A stream closed by a stop is not revived by the next run").
- [ ] 2.5 Add a test that `Subscription.Close` after a stop has already cleared the map is safe and frees nothing twice, and verify it passes under `-race`.

## 3. Tell the client when to come back

- [ ] 3.1 Add the reconnect delay to the hub with its default of 1 second and an option that replaces it, per `design.md` D3. Verify a construction test covers the default, an override, and a non-positive value failing as a configuration error.
- [ ] 3.2 Write the `retry:` field when an SSE stream opens, with a per-stream jitter picking a value between the delay and twice it. Verify with a test asserting the value is within `[1s, 2s)` (spec scenario "A stream tells the client when to reconnect").
- [ ] 3.3 Add a test that 100 streams opened on one instance do not all carry the same reconnect delay (spec scenario "Two streams are told to reconnect at different times").
- [ ] 3.4 Close a WebSocket connection as going away with a reason that distinguishes a stopped instance from a shutdown, reusing the existing close path, and verify the 1.4 WebSocket test passes and asserts the close code.

## 4. Bound an instance's total streams

- [ ] 4.1 Add the instance-wide cap to the hub with its default of 10,000, counting every transport, refusing beyond it with `ErrTooManyStreams` and a message naming the instance cap. Verify the 1.2 test passes and that the refusal maps to 429 through the existing error mapping.
- [ ] 4.2 Add the option that replaces the cap and the explicitly named opt-out that removes it, following the project's naming idiom. Verify with table-driven construction tests covering the default, a raised cap, and the removal (spec scenarios "A host raises the instance cap" and "A host removes the instance cap"), using the `table-test` skill.
- [ ] 4.3 Refuse at construction a total cap below the per-recipient cap, and a cap both set and removed. Verify both with construction tests asserting `ErrConfiguration` (spec scenarios "A total cap below the per-recipient cap is refused at construction" and "A total cap both set and removed is refused at construction").
- [ ] 4.4 Add a test that the instance cap counts streams and WebSocket connections together, in the `websocket` module where both transports are available (spec scenario "The instance cap counts both transports").
- [ ] 4.5 Correct `ErrTooManyStreams`'s godoc in `errors.go`, which describes only a per-recipient cap, to name both caps. Verify with `go doc` that the rendered text distinguishes them.

## 5. Documentation

- [ ] 5.1 Rewrite the "Shutting down" section of `docs/realtime-operations.md` so the `BaseContext` and `RegisterOnShutdown` recipe is stated as covering both transports, explaining that `http.Server.Shutdown` alone ends neither. Verify by re-reading: a host serving only server-sent events must be able to tell the section applies to them.
- [ ] 5.2 Add the instance cap and the reconnect delay to the defaults tables, each with its override, and note that a broker outage now ends streams rather than silencing them. Verify every new option appears with its default.

## 6. Verify the whole change

- [ ] 6.1 Run `make all` (lint, split-check, test) and verify it passes on every module.
- [ ] 6.2 Run `go test -race -count=1 ./...` in the root and `websocket` modules and verify no data races and no goroutine leaks (`goleak` is wired in `main_test.go` in both).
- [ ] 6.3 Re-run the 1.1, 1.2 and 1.4 tests against a temporarily reverted fix and verify each fails again, confirming they still prove the defects rather than passing vacuously.
- [ ] 6.4 Run `openspec validate "harden-hub-streams" --strict` and verify the change is valid and every spec scenario has a corresponding test.
