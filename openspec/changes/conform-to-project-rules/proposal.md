## Why

The repository does not follow all of its own rules (`.claude/rules/*.md`) and skills (`table-test`, `use-mockgen`, `use-testcontainers`). The 2026-09-28 audit found the gaps. Each one makes the next change harder to review, because code that breaks a rule looks like a precedent for breaking it again.

This change records the gaps that still hold on `main` at `20967b3`, and plans their repair. It is tooling, test and documentation work. **No library behaviour changes**: no exported signature, default, error or wire format. The one exception is the godoc text, which is corrected to match the behaviour that exists. For that reason `.openspec.yaml` sets `skip_specs: true`: no requirement in `openspec/specs/` changes, and a spec delta would have to invent one.

## Evidence

Each item was re-checked on `20967b3` on 2026-09-28, with `GOTOOLCHAIN=go1.26.8`, golangci-lint v2.13.2 and Docker 29.8.0.

**Still holds:**

1. **CI floats the toolchain.** `.github/workflows/ci.yml:13` sets `GO_VERSION: "1.26"`, so `actions/setup-go` installs the newest 1.26.x. The `Makefile` (`GOTOOLCHAIN ?= go1.26.8`) and `.envrc` pin `go1.26.8`.
2. **No module's `go.mod` is tidy.** This is wider than the audit found.
   - In every one of the six modules, `go mod tidy -diff` removes `// indirect` from dependencies the module imports directly:
     - root: testify, goleak, mock;
     - ntfytest: testify;
     - sqlstore: nine drivers and ORMs, plus testify;
     - websocket: coder/websocket, testify, goleak;
     - redis and nats: their client, testify, testcontainers and its module, goleak.
   - Every module is missing the `go.yaml.in/yaml/v3 v3.0.5 // indirect` line that testify needs.
   - `make tidy` cannot be used to fix this today. `go mod tidy` ignores `go.work`, so it resolves the sibling modules from the proxy and writes pseudo-versions such as `github.com/kartaladev/ntfy/ntfytest v0.0.0-20260928002533-20967b357445` into `go.mod`. Each `go.mod` says those requirements are written only when the modules are tagged.
   - With `GOPRIVATE=github.com/kartaladev GOVCS=private:off go mod tidy -e -diff`, the siblings stay out and only the real corrections remain.
3. **Some test doubles are written by hand (`use-mockgen`).**
   - `service_ops_test.go:414` `recording` implements `ntfy.Broadcaster` from scratch, although `MockBroadcaster` exists.
   - `email_dispatch_test.go:1450` `failingClaims` is a stub whose only job is to return `errClaim`.
   - `email_dispatch_test.go:26` `movableClock` implements `ntfy.Clock` on its own.
   - `MockEmailStore`, `MockAddressBook`, `MockEmailTemplate`, `MockMailer` and `MockEmailFilter` are generated but no test uses them.
   - The skill gives no rule for decorators that embed a real implementation: `deletingStore`, `recordingEmailStore`, `storeWithoutEmail`, `customBroadcaster` and ntfytest's `foldingStore`.
4. **Some tables have no `assert` closure (`table-test` rule 1).** Found with an awk scan of every `type testCase struct` and `[]struct{` in test code:
   - `docs_test.go:61`, `example_email_test.go:76`, `limits_test.go:49`;
   - `service_ops_test.go:322` (`TestServiceReads`);
   - `notification_test.go:384` (`TestIdentifiersMustBeWellFormed`), which the audit missed;
   - `ntfytest/isolation.go:167` (`readCase`), which the audit missed;
   - `websocket/authority_test.go:22`, `websocket/readloop_test.go:72`;
   - `sqlstore/email_claim_measure_test.go:499`.
5. **Context-sensitive tables have no `ctx` modifier or cancelled-context row (`table-test` rule 3).** The tables are `TestServicePublish` (`service_publish_test.go:99`), `TestServiceClose` (`service_ops_test.go:96`), `TestServiceMarkRead` (`:185`) and `TestServiceMarkAllRead` (`:271`).
   - `Service.signal` (`service.go:323`) detaches the broadcast from the request with `context.WithoutCancel`, and nothing tests it.
   - **Shown by mutation:** replace that line with `detached := ctx`. Then `go test -count=1 .`, `(cd websocket && go test -count=1 ./...)` and `(cd ntfytest && go test -count=1 ./...)` all still pass. The mutation was reverted.
6. **Some tests call `context.Background()` with no justification (`table-test` rule 4).** The sites are:
   - `authorize_test.go:39`, `hub_test.go:332`;
   - `websocket/helpers_test.go:80,180`, `websocket/authority_test.go:192`, `websocket/shutdown_test.go:40,56,130,159`;
   - `redis/listen_test.go:55`, `redis/multiinstance_test.go:51,110`;
   - `nats/listen_test.go:55`, `nats/multiinstance_test.go:51,136`.

   `example_test.go` and `example_email_test.go` have no `t` to take a context from, so they are exempt.
7. **Some standalone tests call the same SUT as a table beside them (`table-test`):**
   - `notification_test.go:236,253,348`;
   - `service_publish_test.go:300`;
   - `service_publish_test.go:324` together with `service_ops_test.go:49`;
   - `http_test.go:875,906`, `hub_test.go:591,901`;
   - the per-dialect wrappers `sqlstore/verify_test.go:171–193` and `sqlstore/email_verify_test.go:144–166`.
8. **Some godoc does not name its default (`library-design` "How to show it"):**
   - `WithEmailKinds` (`email_dispatcher.go:154`) and `websocket.WithAnyOrigin` (`websocket/handler.go:103`) do not name the default they replace.
   - The `Clock` and `EmailFilter` ports do not say what is used when none is supplied.
   - `AddressBook`, `EmailTemplate` and `Mailer` do not say they are required.
   - The `Store` godoc (`store.go:19`) says "The default is [NewMemoryStore]", but `New(nil)` is a `ConfigurationError` (`service.go:89`).
9. **Some useful linters are not enabled.** A trial run with the configuration in `design.md` D6 found 33 hits. The audit's counts do not reproduce:
   - gosec 9: G404 at `email_dispatcher.go:678` and `hub.go:341`; G115 at `id.go:57,59`; G705 at `http.go:375`; G204 at `ntfytest/identity_test.go:138`; G202 at `sqlstore/email_verify_test.go:322,334,345`.
   - paralleltest 14.
   - modernize 3, once `newexpr` is disabled. It finds 33 with `newexpr` on.
   - containedctx 2: `email_dispatcher.go:487`, `sqlstore/sql.go:81`.
   - sqlclosecheck 2, noctx 2 (`nats/reconnect_test.go:34,65`).
   - nolintlint 1: `http_test.go:546` suppresses revive, which is already excluded for tests.
   - `//nolint:paralleltest` at `redis/listen_test.go:81` and `nats/listen_test.go:82` suppresses a linter that is not enabled.
   - thelper finds 505 hits, 492 of them on the `assert` closures that `table-test` mandates. It is therefore not adopted (D6).
   - The G705 hit is **not a defect**. The stream sets `Content-Type: text/event-stream` (`http.go:361`), and `http_test.go:650` asserts it.
10. **sqlstore does not run goleak.** Neither `sqlstore` nor `sqlstore/internal/gormtest` has a `TestMain`. A trial `goleak.VerifyTestMain` passed in both packages, so nothing leaks today. The change adds a guard, not a fix.
11. **Documentation is missing:**
    - there is no `ExampleNew` for `ntfy` or `sqlstore`;
    - there is no example for `redis.NewBroadcaster`, `nats.NewBroadcaster` or `ntfytest.Run`;
    - there is no `LICENSE`, `CHANGELOG.md` or `CONTRIBUTING.md`.
12. **There are no fuzz tests.** `grep -rn 'func Fuzz'` finds none.
13. **Process records are incomplete:**
    - Only 1 of 8 archived `plans.md` has a "Mapping to `tasks.md`" table.
    - Only 1 of 13 archived proposals has a **Proved by** line.
    - Several Global Constraints sections omit `use-mockgen`, `use-testcontainers` or `performance-benchmark.md`.
    - `scale-email-claim-query` D2 and `email-delivery-robustness` D5 have no Default/Override lines.

**Already conformant, or reclassified:**

- **The ntfytest store suite has no cancelled-context case, and needs none.** The `Store` contract does not make any behaviour depend on cancellation, and the memory store never reads `ctx.Err()`. Rule 3 applies only to context-sensitive components. Adding such a case would be a new requirement, so it is out of scope. Task 3.4 records why the suite has none.
- **id.go is not a fuzz target.** `UUIDv7Generator.NewID` takes no input.
- **CI and local lint use the same golangci-lint**, v2.13.2 in both.
- **goleak already guards four modules.** It runs in the root and websocket modules (`TestMain`), and per test in redis and nats.
- **The leftover worktree is housekeeping.** `.claude/worktrees/speed-up-memory-store-reads` and its branch remain after #10 merged. Removing them is not part of this change.

## What Changes

- **Toolchain and modules:**
  - CI pins `GO_VERSION: "1.26.8"`.
  - `make tidy` stops fetching the sibling modules.
  - A new `make tidy-check` target joins `make all` and the CI lint job.
  - Every `go.mod` is tidied.
- **Test doubles:**
  - `recording` and `failingClaims` become generated mocks.
  - `movableClock` is handed over through `ntfy.ClockFunc`.
  - The `use-mockgen` skill states the one exception: a decorator that embeds a real implementation.
- **Tables:**
  - Every table gets an `assert` closure.
  - The service tables get a `ctx` modifier and a cancelled-context row, which pins `context.WithoutCancel`.
  - Each `context.Background()` in a test becomes a context derived from `t.Context()`, or carries a one-line justification.
  - The standalone tests are folded into the tables beside them.
- **Godoc:** eight comments name their default, or say that the port is required.
- **Linters:**
  - `.golangci.yml` enables `gosec`, `nolintlint`, `paralleltest`, `sqlclosecheck`, `rowserrcheck`, `containedctx`, `modernize` (without `newexpr`) and `noctx`.
  - Each of the 33 hits is fixed, or suppressed with a specific, explained `nolint`.
- **goleak:** `TestMain` in `sqlstore` and in `sqlstore/internal/gormtest`.
- **Documentation:**
  - runnable examples for `ntfy.New` and `sqlstore.New`;
  - compile-only examples for the two broadcasters and `ntfytest.Run`;
  - `CHANGELOG.md` and `CONTRIBUTING.md`;
  - `LICENSE` only once the maintainer has chosen a licence. This change does not choose one.
- **Fuzzing:** `FuzzDecodeCursor` and `FuzzHandlerRequests`, landed from the audit, plus `FuzzDecodeSignals` and `FuzzDraftValidate`.
- **Process:**
  - `openspec/config.yaml` gains artifact rules: a **Proved by** or **Unverified** line for every defect, and Default/Override lines for every design decision.
  - `.claude/rules/plans-beside-tasks.md` requires the mapping table.
  - Archived records are left as written (D9).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. `skip_specs: true`: the change adds tests, documentation and tooling, and corrects godoc to match existing behaviour.

## Impact

- **Code:**
  - Test files in every module.
  - `ntfytest/isolation.go` and `ntfytest/suite.go`, which are the conformance suite's own code.
  - Comment-only or `nolint`-only edits to `email_dispatcher.go`, `hub.go`, `id.go`, `http.go` and `sqlstore/sql.go`.
  - Godoc in `clock.go`, `email.go`, `email_dispatcher.go`, `store.go` and `websocket/handler.go`.
- **Tooling:**
  - `.golangci.yml`, `Makefile`, `.github/workflows/ci.yml`;
  - six `go.mod` and `go.sum` files;
  - `.claude/skills/use-mockgen/SKILL.md`, `.claude/rules/plans-beside-tasks.md`, `openspec/config.yaml`.
- **Docs:** `CHANGELOG.md` and `CONTRIBUTING.md` (new), and `LICENSE`, which is blocked on the maintainer's choice.
- **Hosts:** nothing changes. No exported API, default or behaviour changes.
- **Dependencies:** none are added. `go.yaml.in/yaml/v3` becomes an explicit indirect requirement. It is already in the build, through testify.
