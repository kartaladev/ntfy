Each group can be applied on its own. Three files are shared between groups, and `design.md` "Order" says which group edits them first. Every Go command runs with `GOTOOLCHAIN=go1.26.8`.

## 1. Toolchain pin and module tidiness

- [ ] 1.1 In `.github/workflows/ci.yml`, set `GO_VERSION: "1.26.8"`, and make the comment beneath it say that the value matches the `Makefile` and `.envrc`. Verify with `grep -n 'GO_VERSION: "1.26.8"' .github/workflows/ci.yml`.
- [ ] 1.2 In the `Makefile`, change `tidy` to run `GOPRIVATE=github.com/kartaladev GOVCS=private:off $(GO) mod tidy -e` in each ntfy module. Add a `tidy-check` target that runs the same command with `-diff` and fails on any output. Add `tidy-check` to `all` and `.PHONY`, and add a `make tidy-check` step to the CI lint job.
  - Before running `make tidy`, verify that `make tidy-check` fails on the current `go.mod` files, and that its diff lists no `github.com/kartaladev` line.
- [ ] 1.3 Run `make tidy`, which drops the `// indirect` markers from direct dependencies and adds `go.yaml.in/yaml/v3 v3.0.5 // indirect` in all six modules. Verify that:
  - `make tidy-check` passes;
  - `git diff -- '*go.mod'` contains no `github.com/kartaladev` requirement;
  - `make build` and `make test` pass.

## 2. Generated test doubles (`use-mockgen`)

- [ ] 2.1 Amend `.claude/skills/use-mockgen/SKILL.md` with the "Decorators over a real implementation" section from `design.md` D2. It covers the one exception, the exported `XxxFunc` adapters, and the one-line comment each decorator carries. Verify with `grep -n 'Decorators over a real implementation' .claude/skills/use-mockgen/SKILL.md`.
- [ ] 2.2 Replace `recording` (`service_ops_test.go:414`) with `ntfy.MockBroadcaster` in `TestServiceOnTheMemoryStore`, using a `DoAndReturn` that records into a mutex-guarded slice, and delete the type. Verify that:
  - `go test -run 'TestServiceOnTheMemoryStore' -count=1 -race .` passes;
  - `grep -n 'type recording' service_ops_test.go` is empty.
- [ ] 2.3 Replace `failingClaims` (`email_dispatch_test.go:1450`) with `ntfy.MockEmailStore`, expecting `ClaimEmails` once and returning `errClaim`, and delete the type.
  - Verify that `go test -run 'TestEmailDispatcherDispatch/a_failed_claim_is_returned' -count=1 .` passes.
  - Then add a temporary `h.email.RecordEmails` call after the claim in the row. Watch gomock fail with an unexpected call, and remove the call.
- [ ] 2.4 Pass `ntfy.ClockFunc(h.clock.Now)` to `ntfy.WithClock` in `newDispatchHarness`. Add the one-line decorator comment to `deletingStore`, `recordingEmailStore`, `storeWithoutEmail`, `customBroadcaster` and ntfytest's `foldingStore`. Verify that `go test -count=1 -race .` and `(cd ntfytest && go test -count=1 ./...)` pass.

## 3. Table-driven tests and contexts (`table-test`)

- [ ] 3.1 In `service_publish_test.go`, give `TestServicePublish` a `ctx` modifier, and add the row "a cancelled request still broadcasts, keeping its values".
  - Red: temporarily change `service.go:323` to `detached := ctx`. Then `go test -run 'TestServicePublish$' -count=1 .` fails on that row with `context canceled`. Restore the line.
  - Green: the same command passes.
- [ ] 3.2 Do the same for `TestServiceClose`, `TestServiceMarkRead` and `TestServiceMarkAllRead` in `service_ops_test.go`: a `ctx` field, and one cancelled-context row each. Verify red and green with the same mutation, running `go test -run 'TestService(Close|MarkRead|MarkAllRead)$' -count=1 .`.
- [ ] 3.3 Give the nine tables in `proposal.md` item 4 an `assert` closure, as `design.md` D4 describes. Verify each package's tests pass:
  - root;
  - `ntfytest`;
  - `websocket`;
  - `sqlstore`, with `NTFY_MEASURE_ROWS=20000 go test -run TestMeasureClaim -count=1 .`, whose thresholds must hold.

  For each rewritten table, invert one row's expectation temporarily, and watch that row fail.
- [ ] 3.4 In `ntfytest/suite.go` (`Run`'s godoc) and `ntfytest/doc.go`, add a sentence saying that the `Store` contract makes no behaviour depend on cancellation, so the suite's tables carry no `ctx` field. Verify with `go doc github.com/kartaladev/ntfy/ntfytest Run`.
- [ ] 3.5 Replace the 15 `context.Background()` sites in `proposal.md` item 6, as `design.md` D3 describes.
  - Each package must pass `go test -count=20 -race` on the affected tests, including redis and nats with Docker.
  - A site that fails keeps `context.Background()`, with a one-line justification.
  - Verify that `grep -rn 'context.Background()' --include='*_test.go' . | grep -v '^./pkg\|^./.claude\|example'` lists only justified sites.
- [ ] 3.6 Fold the standalone tests of `proposal.md` item 7 into their tables, as `design.md` D5 describes. Verify that:
  - the combined subtest count per SUT is unchanged or higher (`go test -run '<Table>' -v -count=1 . | grep -c -- '--- PASS'`, before and after);
  - `make test` passes;
  - `cd sqlstore && go test -run 'TestVerify(Email)?Schema$' -count=1 .` passes with Docker.

## 4. Godoc names its defaults (`library-design`)

- [ ] 4.1 Rewrite the godoc of the following, using the text in `plans.md` Task 4:
  - `Store` (`store.go:19`);
  - `Clock` (`clock.go:5`);
  - `EmailFilter`, `AddressBook`, `EmailTemplate` and `Mailer` (`email.go`);
  - `WithEmailKinds` (`email_dispatcher.go:154`);
  - `websocket.WithAnyOrigin` (`websocket/handler.go:103`).

  Verify that `go doc` shows each one, and that `make lint` and the docs tests (`go test -run 'Document' -count=1 ./...`) pass.

## 5. Linters

- [ ] 5.1 Add the linters and settings of `design.md` D6 to `.golangci.yml`. Verify that `make lint` reports the 33 hits of `proposal.md` item 9, and no others. Any extra hit comes from code changed since, and is triaged in 5.2.
- [ ] 5.2 Fix or suppress each hit as `design.md` D6 triages it, following the list in `plans.md` Task 5. Verify that `make lint` passes with zero issues, and that `make test` passes.

## 6. goleak in sqlstore

- [ ] 6.1 Add `sqlstore/main_test.go` and `sqlstore/internal/gormtest/main_test.go`, each with `goleak.VerifyTestMain(m)`.
  - Red: add a temporary test that starts `go func() { select {} }()`. `cd sqlstore && go test -run 'TestLeakCanary' -count=1 .` must fail, naming the goroutine. Remove the test.
  - Green: `cd sqlstore && go test -count=1 ./...` passes with Docker.

## 7. Examples and repository documents

- [ ] 7.1 Add `ExampleNew` to `example_test.go` (ntfy) and a new `sqlstore/example_test.go`, each with `// Output:`. Verify with `go test -run 'ExampleNew' -count=1 .` and `cd sqlstore && go test -run 'ExampleNew' -count=1 .`.
- [ ] 7.2 Add the compile-only examples `redis/example_test.go` `ExampleNewBroadcaster`, `nats/example_test.go` `ExampleNewBroadcaster` and `ntfytest/example_test.go` `ExampleRun`. Verify that `go vet ./...` passes in each module, and that `go doc -all` lists each example's package.
- [ ] 7.3 Add `CHANGELOG.md` and `CONTRIBUTING.md` with the content in `plans.md` Task 7. Verify that both files exist, and that `make all` passes.
- [ ] 7.4 Ask the maintainer which licence the repository is published under. Do not choose one. Once answered, add the licence's standard `LICENSE` text and a "Licence" section in `README.md`. Verify that `LICENSE` exists, and that its first line names the chosen licence.

## 8. Fuzz tests

- [ ] 8.1 Land `FuzzDecodeCursor` (`cursor_fuzz_test.go`) and `FuzzHandlerRequests` (`http_fuzz_test.go`) from the audit.
  - Verify that `go test -run 'Fuzz' -count=1 .` passes on the seeds, and that `go test -run '^$' -fuzz '^FuzzDecodeCursor$' -fuzztime 30s .` finds nothing.
  - Red: temporarily make `DecodeCursor` return `errors.New("x")` for an empty cursor, and watch the seed run fail. Revert.
- [ ] 8.2 Add `FuzzDecodeSignals` (`codec_fuzz_test.go`) and `FuzzDraftValidate` (`notification_fuzz_test.go`).
  - Verify each seed run passes, and a `-fuzztime 30s` run finds nothing.
  - Red: temporarily make each function return a plain error on one seed, and watch the run fail. Revert.

## 9. Process rules and templates

- [ ] 9.1 Add the `rules:` block of `design.md` D9 to `openspec/config.yaml`. Verify that `openspec instructions proposal --change conform-to-project-rules --json` shows the rules, and that `openspec validate --all --strict` passes.
- [ ] 9.2 In `.claude/rules/plans-beside-tasks.md` "What the plan must contain", add the Mapping to `tasks.md` table and the explicit skill and rule list for Global Constraints. Verify with `grep -n 'Mapping to' .claude/rules/plans-beside-tasks.md`.

## 10. Verify and hand off

- [ ] 10.1 Run `make all` and `make store-matrix`. Both must pass, and `git status` must be clean after `make tidy` and `go generate ./...` in each module.
- [ ] 10.2 Run `/code-review` on the branch, and fix what it finds. Any finding claimed as a defect is proved by a failing test first, per `.claude/rules/prove-errors-with-tests.md`.
