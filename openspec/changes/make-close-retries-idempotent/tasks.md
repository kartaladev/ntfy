## 1. Prove a retried close undoes its own work (red)

- [ ] 1.1 In `ntfytest/suite.go` `runSuccessors`, replace the case "a retried close creates no further successors" with a three-row table (`design.md` D2):
  - "a retried close of one kind closes nothing further" (`Kinds: ["offer"]`, today's case);
  - "a retried close of every kind closes nothing further" (`Kinds: nil`);
  - "a retried close naming the successor's kind closes nothing further" (`Kinds: ["offer", "taken"]`).

  Every row closes the three offers at version 5 with a `"taken"` successor at version 5, skipping carol, twice. It asserts that the retry closes nothing, reports no recipients and creates nothing. It also asserts that alice's and bob's successors from the first run are still `ACTIVE`, and that each has exactly one `"taken"` notification.

  Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/successors/a_retried' -count=1 .`: the one-kind row passes, and the other two fail with `Should be zero, but was 2` / `a retried close closes nothing further`. Then run `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite/successors/a_retried' -count=1 .` and see the same two rows fail. A failure from compilation does not count.
- [ ] 1.2 In the same group, add the case "a later close still closes an earlier close's successors". A close at version 5 gives alice and bob a `"taken"` successor. A close of every kind at version 6 with no successor then closes both. It passes on today's code, and it is there to fail an over-broad fix. Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance/successors/a_later_close' -count=1 .`. Then temporarily add `case n.Kind == "taken": continue` to `MemoryStore.Close`'s selection, confirm the case fails, and revert the mutation.

## 2. A close leaves out its own successor's source (green)

- [ ] 2.1 In `memory.go` `MemoryStore.Close`, add a selection case that skips a notification whose `SourceID` is `req.Successor.SourceID` when `req.Successor != nil` (`design.md` D1). Verify that `GOTOOLCHAIN=go1.26.8 go test -run 'TestMemoryStoreConformance' -count=1 .` passes in full.
- [ ] 2.2 In `sqlstore/close.go` `closingCondition`, append `AND source_id <> ?`, binding `req.Successor.SourceID`, when `req.Successor != nil`. Update the comments of `closingCondition` and `Store.Close`. Verify that `cd sqlstore && GOTOOLCHAIN=go1.26.8 go test -run 'TestStoreOnStdSQLSQLite' -count=1 .` passes in full.
- [ ] 2.3 State the rule where it is read:
  - the `Close` method's godoc on the `Store` interface in `store.go`: a close never closes a notification published from its own successor's source, so a retry changes nothing;
  - the `Successor` field's godoc on `CloseRequest`;
  - the "Successors" paragraph of `docs/notifications.md`.

  Verify that `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .` passes.

## 3. The watermark retention godoc says what the stores measure

- [ ] 3.1 Add `TestWatermarkRetentionGodocSaysWhatTheStoreMeasures` to `docs_test.go`. It reads `pruner.go` and `store.go` as text, because `go/parser` is outside the core module's depguard allow-list. It checks the doc comments of `DefaultWatermarkRetention`, `WithWatermarkRetention` and `PruneRequest.WatermarkRetention`. Each must not contain "outlives its last notification", and must contain "last changed". Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestWatermarkRetentionGodocSaysWhatTheStoreMeasures' -count=1 .`. All three rows must fail on the godoc's wording, not on a parse error or a missing declaration.
- [ ] 3.2 Rewrite the three doc comments (`design.md` D3). Also change the retention table row in `docs/notifications.md` to "7 days after the record last changed". Verify that 3.1 and `TestTheDocumentMatchesTheImplementation` pass.
- [ ] 3.3 In `ntfytest/suite.go` `runRetention`, add the case "a close record can expire with its subject's last notification", which is the spec scenario:
  - a notification at version 2 on a subject, closed at version 5 ninety-five days before `now`;
  - one pass with `MaxAge: days(90)` and `WatermarkRetention: days(7)` deletes one notification for age and one close record;
  - a source at version 1 afterwards is created.

  Verify that it passes on memory and on SQLite. Then temporarily change `MemoryStore.pruneWatermarks` to skip every record, confirm the case fails, and revert.

## 4. Verify and hand off

- [ ] 4.1 Run `make all`. It must pass: lint, split-check, and every module's tests.
- [ ] 4.2 Run `make store-matrix` with Docker running. All seven driver-by-dialect entry points must pass, including the new `successors` and `retention` cases.
- [ ] 4.3 Run `/code-review` on the branch's diff. Answer each finding under `superpowers:receiving-code-review`. Record any fix in `tasks.md` and `plans.md` in the same turn.
