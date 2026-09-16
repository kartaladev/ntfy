## 1. Prove the defect (red)

- [ ] 1.1 Add an aliasing case to `ntfytest` (a new `runIsolation` group registered from `Run`) that closes a subject with a successor carrying `Links` and `Data`, mutates the caller's `req.Successor.Links` and `Data` after `Close` returns, then asserts `CloseResult.Successors` and the notifications read back are unchanged. Verify with `go test -run 'TestStore/isolation' -count=1 ./...` in `sqlstore/` that it fails on the SQL store because the returned successor changed, and confirm the same case passes on `MemoryStore` — that divergence is the finding (see `.claude/rules/prove-errors-with-tests.md`).
- [ ] 1.2 Add the sibling case for successors to two recipients: mutate the link map of the successor reported for alice and assert bob's is unchanged. Verify it fails on both stores today, since `SuccessorInsertions` gives every insertion the same map (`store.go:178-182`).
- [ ] 1.3 Add a case that mutates a caller's draft `Links`/`Data` after `Publish` and asserts the stored notification is unchanged, and verify it passes on both stores today — it pins the behaviour `service.go:143-150` already provides so a later refactor cannot silently drop it.

## 2. Stop handing stores aliased values (green)

- [ ] 2.1 Clone each insertion in `CloseRequest.SuccessorInsertions` (`store.go:178-182`), matching what `Service.Publish` does at `service.go:150`. Verify the 1.2 case now passes on both stores.
- [ ] 2.2 State the guarantee in `SuccessorInsertions`' godoc: the insertions it returns share nothing with the request or with each other. Verify with `go doc ntfy.CloseRequest.SuccessorInsertions` that the rendered text says so.

## 3. Make the stores keep the invariant (green)

- [ ] 3.1 Clone on accept and on return in `sqlstore` (`insert.go:103`, `:115`) so `InsertResult.Created` and `CloseResult.Successors` share nothing with the values passed to `Insert`. Verify the 1.1 case now passes on every dialect.
- [ ] 3.2 Add the isolation obligation to the `Store` port's godoc (`store.go:27-51`): a store copies what it retains and what it returns, and `Notification.Clone` is the mechanism. Verify by reading `go doc ntfy.Store` that an implementer learns the obligation without reading the suite.
- [ ] 3.3 Confirm no clone is added to the paths the design audited as safe by construction (`sqlstore/sql.go:251` decode, `memory.go:258`, `:297`), and verify with `rg -n "Clone\(\)" memory.go memory_email.go store.go sqlstore/` that the call sites match the design's table.

## 4. Hold every store to it

- [ ] 4.1 Extend the isolation group with the remaining directions from the spec: a notification read twice, and a list followed by a read, where mutating the first result must not change the second. Follow the `table-test` skill for the per-path cases. Verify with `go test -run 'TestStore/isolation' -race -count=1 ./...` in the root and `sqlstore/`.
- [ ] 4.2 Assert in each case that the mutation actually took effect on the caller's own copy, so a case cannot pass by mutating nothing (design D2). Verify by temporarily removing the mutation and confirming the case fails.
- [ ] 4.3 Document the invariant where a host implementing a store will meet it: the `ntfytest` package comment or `Run`'s godoc names isolation as one of the properties the suite asserts. Verify with `go doc ntfytest.Run`.

## 5. Verify the whole change

- [ ] 5.1 Run `make all` (lint, split-check, test) and verify it passes on every module.
- [ ] 5.2 Run `make store-matrix` and verify the isolation cases pass on every driver and dialect combination (spec scenario "Every store is held to this").
- [ ] 5.3 Re-run the 1.1 and 1.2 cases against a temporarily reverted fix and verify each fails again, confirming the cases still prove the defect rather than passing vacuously.
- [ ] 5.4 Run `openspec validate "stop-store-aliasing-caller-data" --strict` and verify the change is valid and every spec scenario has a corresponding case.
