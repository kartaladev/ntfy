## Context

`proposal.md` lists each gap and the command that shows it. This design settles how each group is repaired so that the tasks can be applied in any order. Every group touches its own files except these three:

- `.golangci.yml`, touched by groups 5 and 3: the new linters flag code that group 3 rewrites;
- `email_dispatch_test.go`, touched by groups 2, 3 and 5;
- `service_ops_test.go` and `service_publish_test.go`, touched by groups 2 and 3.

The rule for those files is below, under "Order".

Constraints that shape the approach:

- **No behaviour change** (`skip_specs: true`). A decision that would change what a host observes is out of scope, even where a rule seems to invite it. D3 is the case in point.
- **The core module imports only the standard library** in production code. Test code may use testify, goleak and mock.
- **`pkg/sqlkit` is a copy** and is not linted, tidied or edited here.
- **The sibling modules are not required by each other's `go.mod` until the tag.** They are resolved through `go.work`.

## Goals / Non-Goals

**Goals:**
- Every item under "Still holds" in `proposal.md` is either repaired or given a stated, reviewable exception at the site.
- Where a rule can be checked by a tool (lint, tidiness, goleak), the check runs in `make all` and in CI, so the gap cannot come back unnoticed.
- The rules and templates that failed to prevent the gaps are tightened, so the next change is written conformant.

**Non-Goals:**
- Rewriting archived OpenSpec records (D9).
- Making stores honour cancellation (D3).
- Choosing a licence (D8).
- Removing the stale `speed-up-memory-store-reads` worktree and branch. That is repository housekeeping, not a change.

## Decisions

### D1. The toolchain is pinned to a patch release in CI, and tidiness is checked without resolving the sibling modules

- **CI:** `GO_VERSION: "1.26.8"`. setup-go then installs exactly what the Makefile and `.envrc` pin.
  - The alternative was `go-version-file: go.mod`. It reads `go 1.26.0`, a language version rather than a toolchain pin, so it would install 1.26.0, not the patch release everyone else runs.
- **`make tidy`** runs `GOPRIVATE=github.com/kartaladev GOVCS=private:off go mod tidy -e` in each ntfy module.
  - `GOVCS=private:off` stops the go command from fetching any `github.com/kartaladev` module.
  - `-e` lets tidy finish without them, so the go.work siblings are left out while every other module is tidied normally.
  - Measured on `sqlstore`: the diff drops only the wrong `// indirect` markers and adds `go.yaml.in/yaml/v3`. No `kartaladev` line appears.
  - `-e` would also hide a genuinely broken import. `make build` and `make test` run in workspace mode and still catch one.
- **`make tidy-check`** runs the same command with `-diff`. It fails on any difference, and joins `all` and the CI lint job.
- **Alternatives considered:**
  - Hand-editing the markers with no check. It would drift back.
  - A script comparing `go list` imports with `go mod edit -json`. It re-implements tidy.
  - `GOPROXY=off`. On a cold CI cache, tidy could not download real dependencies, and `-e` would turn that into a spurious diff.
- **Default / Override:** this is not library behaviour, so there is no host-facing override. `make tidy` keeps its name and purpose.

### D2. Doubles are generated; a decorator over a real implementation is not a double

The `use-mockgen` skill is amended with one stated exception, and the rest of the code is brought to the rule.

- **Replaced by generated mocks:**
  - **`recording`** becomes `MockBroadcaster`. `Broadcast` is expected `AnyTimes()`, with a `DoAndReturn` that appends to a slice the test reads. `Listen` is expected `AnyTimes()`, with a `DoAndReturn` that calls `ready()` and blocks on `ctx.Done()`.
  - **`failingClaims`** becomes `MockEmailStore`. Only `ClaimEmails` is expected, returning `errClaim`. `Dispatch` returns before it touches the other methods (`email_dispatcher.go:396–401`). gomock therefore also proves that nothing is recorded after a failed claim, which the stub could not.
- **Handed over through the port's adapter:** `movableClock` keeps its `set`/`advance` state. The harness passes `ntfy.ClockFunc(h.clock.Now)`, which uses the library's own `ClockFunc` adapter.
  - Exported `XxxFunc` adapters are part of the public API. Tests that use them inline (`noopMailer`, `ntfy.MailerFunc` in the harness) are not hand-written doubles.
- **The stated exception:** a type that embeds a *real* implementation of the port, and overrides methods to inject a fault or observe calls, is not a double. The test depends on the real implementation's behaviour, and a mock would have to re-implement it. Every such type carries a one-line comment naming the real implementation it decorates. The instances are:
  - `deletingStore`, `recordingEmailStore` and `storeWithoutEmail` over `MemoryStore`;
  - `customBroadcaster` over `InProcessBroadcaster`;
  - ntfytest's `foldingStore`, which is a deliberately broken implementation under test.
- **The unused mocks for `AddressBook`, `EmailTemplate`, `Mailer` and `EmailFilter` stay.** They come from the one `email.go` directive, and cost nothing outside `_test.go`. Removing them would need `-exclude_interfaces` and a second directive, which is more to maintain than the dead code.
- **Alternative considered:** replacing the decorators with `MockStore` or `MockEmailStore` and a `DoAndReturn` that delegates. That is eight delegating expectations per store for the same behaviour. It also loses the recording helpers (`statusesOf`, `lastRecordOf`) that the dispatch table reads.
- **Default / Override:** none. This is test code.

### D3. The cancelled-context rows go where behaviour depends on the context, and the store suite gets none

- **What rule 3 covers:** components whose behaviour depends on context cancellation. `Service.signal` is one: it detaches with `context.WithoutCancel` (`service.go:323`), so a request cancelled after commit must still broadcast, with the request's values.
  - `TestServicePublish`, `TestServiceClose`, `TestServiceMarkRead` and `TestServiceMarkAllRead` gain `ctx func(context.Context) context.Context`.
  - Each gains one row that cancels the context and puts a value on it. The row asserts that `Broadcast` receives a context whose `Err()` is nil, and which still carries the value.
  - This is the test the mutation in `proposal.md` item 5 showed was missing. It is its red step.
- **The `Store` contract says nothing about cancellation**, and the memory store never reads `ctx.Err()`. A cancelled-context conformance case would add a requirement every host store must meet. That is a spec change, and it belongs in a change of its own.
  - Here, `ntfytest.Run`'s godoc and `ntfytest/doc.go` say that the suite's tables carry no `ctx` field for that reason. That is the rule's own escape: the component is not context-sensitive.
- **`context.Background()` sites** become `context.WithCancel(t.Context())` where the context must also be cancellable early, and `t.Context()` where it need not.
  - `t.Context()` is cancelled *before* `t.Cleanup` functions run. That can reorder shutdown where a cleanup stops a hub after closing servers.
  - A site whose tests fail at `-count=20 -race` after the change keeps `context.Background()`, with a comment naming the ordering it protects.
  - `authorize_test.go:39` is a plain bug in the test: the closure receives a `ctx` and ignores it. It passes the `ctx` on instead.

### D4. Uniform tables get a shared assert value, not a copied closure

Rule 1 says every case populates `assert`, with no default. A table whose rows all assert the same thing uses one named closure value, assigned in every row.

- In `docs_test.go` and `example_email_test.go` that value is `mentions(needles ...string)`, which returns the `assert` func.
- In `TestMeasureClaim` it is `holdsTheBound`.

The rule is met, and the table still reads as data. Where rows differ, the closure is written inline, as `TestServiceReads` does after its rewrite.

`TestMeasureClaim` is a performance gate. `.claude/rules/performance-benchmark.md` requires it to be re-run when it changes, with `NTFY_MEASURE_ROWS=20000` (it is package `sqlstore`, internal), and its thresholds must still pass. The refactor must not move them.

### D5. Standalone tests are folded into the table beside them, or split with a divergence comment

Each standalone test named in `proposal.md` item 7 becomes a row of the table that already calls its SUT:

- `TestDraftValidateOversizedLinksSkipsPerLinkChecks` joins `TestDraftValidateContentLimits`.
- `TestDraftValidateWithinRaisedLimits` becomes a row of a new `TestDraftValidateWithin` table, because it calls `ValidateWithin`, not `Validate`.
- `TestDraftValidateDoesNotRewriteAnAcceptedHref` joins `TestDraftValidateLinkSchemes`.
- `TestServicePublishRefusesTooManyDrafts` joins `TestServicePublish`.
- `TestServicePublishHonoursConfiguredLimits` and `TestServiceCloseHonoursConfiguredLimits` become one `TestServiceHonoursConfiguredLimits` table over a memory store. Each row gives `opts` and a `call` that publishes, or publishes and then closes.
- `TestHandlerStreamEndsWhenTheHubStops` and `TestHandlerStreamCarriesAReconnectDelay` join `TestHandlerStream`. The first uses `run: false` and starts the hub in its own closure, to keep the `stop` handle.
- `TestHubStopReleasesSubscriptions` joins `TestHubStopClosesOpenSubscriptions`, which gains an `opts []ntfy.HubOption` field.
- `TestHubCapsStreamsPerInstance` joins `TestHubInstanceCapConfiguration`, as the row with no options.
- `TestVerifySchemaOn{Postgres,MySQL,SQLite}` become `TestVerifySchema`, and `TestVerifyEmailSchemaOn*` become `TestVerifyEmailSchema`. Each row has an `open func(t *testing.T) sqlkit.Executor`.
  - Nothing outside the files refers to the old names. `grep -rn 'TestVerify\(Email\)\?SchemaOn' Makefile .github` is empty.

If a row cannot express a test's setup without a flag that only it reads, the test stays standalone. A one-line comment at the top of its file then says why, as the skill allows.

### D6. The linters enabled, and the one that is not

- **Enabled:** `gosec`, `nolintlint` (with `require-explanation`, `require-specific` and `allow-unused: false`), `paralleltest`, `sqlclosecheck`, `rowserrcheck`, `containedctx`, `modernize` and `noctx`.
- **`modernize` runs without `newexpr`.** That analyser rewrites every `ntfy.Limit(n)` and `timePtr(t)` into `new(n)` and `new(t)`, which is legal from Go 1.26.
  - `Limit` is an exported, documented helper that host code calls by name. Inlining it at every call site trades a readable name for a builtin, and marks the helper as redundant.
  - The other analysers stay on: `slicesbackward` ×2 and `waitgroupgo` ×1 are real simplifications.
- **`paralleltest` is enabled because the project's convention is all-parallel.** It also makes the existing `//nolint:paralleltest` directives in redis and nats real suppressions, instead of noise. Its 14 hits are deliberate:
  - measurement gates that must run alone;
  - the ntfytest identity child process;
  - `TestMeasureClaim`;
  - the container suites' email groups, whose cases call `t.Parallel` themselves.

  Each hit gets a specific `nolint` that repeats the reason already written in the test's comment.
- **`thelper` is not enabled.** 492 of its 505 hits are `assert` and `run` closures, which `table-test` mandates. `t.Helper()` in an `assert` closure would report the failure at the table loop's `tc.assert(...)` line instead of the line that failed. The 13 named hits are ntfytest's `run*` group functions, which are `t.Run` parents, not helpers.
- **Triage of each hit:**
  - **gosec G404 ×2:** jitter, where predictability does not matter. `nolint:gosec` with that reason.
  - **gosec G115 ×2, `id.go`:**
    - `lastMS` starts at 0 and only grows (`tick`, `id.go:67–85`), so `uint64(ms)` cannot wrap.
    - `counter>>8&0x0F` fits in a byte.
    - A `nolint:gosec` states both.
  - **gosec G705, `http.go:375`:** a false positive. The stream is `text/event-stream`, asserted at `http_test.go:650`. The `nolint` cites that test.
  - **gosec G204, `ntfytest`:** the command is the test binary itself (`os.Args[0]`). `nolint` with that reason.
  - **gosec G202 ×3:** the concatenated value is the test's own table prefix. `nolint` with that reason.
  - **containedctx:**
    - `ownContext` *is* a `context.Context` implementation;
    - `emailPass` lives for one `Dispatch` call and never escapes it.
    - Both get a `nolint`. Refactoring `emailPass` to pass `ctx` through each method is churn with no gain in safety.
  - **sqlclosecheck ×2:** `rows.Close()` is called explicitly so the test can assert its error. `nolint` with that reason.
  - **noctx ×2:** fixed. `(&net.ListenConfig{}).Listen(t.Context(), …)`, and `(&net.Dialer{}).DialContext(ctx, …)` with `ctx` passed into `accept`.
  - **nolintlint ×1:** fixed by removing the `//nolint:revive` at `http_test.go:546`, whose explanation stays as a plain comment.
  - **modernize ×3:** fixed with `golangci-lint run --fix --enable-only modernize`.
- **Default / Override:** not library behaviour.

### D7. goleak in sqlstore is a guard, proved by a planted leak

`TestMain` with `goleak.VerifyTestMain(m)` goes in `sqlstore/main_test.go` and `sqlstore/internal/gormtest/main_test.go`.

- A trial run of both packages, with Docker, passed without any ignore options. The testcontainers reaper and the database pools leave nothing behind once their cleanups run.
- There is no defect to prove, so the red step is a temporary test that starts a goroutine and never stops it. The package must fail, naming that goroutine. The test is then removed.
- If CI's Docker leaves a goroutine that the local run did not, the ignore option is added naming that exact function, never `IgnoreCurrent`.

### D8. The examples compile everywhere and run where no service is needed; the licence is the maintainer's decision

- **`ExampleNew` (ntfy) and `ExampleNew` (sqlstore) run, with `// Output:`.** sqlstore uses in-process SQLite (`modernc.org/sqlite`, `file::memory:`, one open connection).
- **`ExampleNewBroadcaster` (redis, nats) and `ExampleRun` (ntfytest) compile but do not run**, since they have no `// Output:` line. The broadcasters need a server, and `ntfytest.Run` needs a `*testing.T`. `go vet` still type-checks them, so they cannot rot.
- **`CHANGELOG.md`** starts with `## [Unreleased]`, in Keep a Changelog form. It lists the capabilities built so far, from the archived changes.
- **`CONTRIBUTING.md`** points at `make all`, `make store-matrix`, the rules in `.claude/rules/` and the OpenSpec workflow.
- **`LICENSE` is not written by this change.** Choosing a licence is a legal decision for the maintainer. Task 7.4 asks for the choice and blocks until it is given. `README.md` then gains a Licence section naming it.

### D9. Archived records stay as written; the templates that let the gaps through are tightened

An archived change is the record of what was decided at the time. Adding a Default/Override line or a **Proved by** line after the fact would put words in the record that were not there when it was reviewed. The gaps are listed in `proposal.md` item 13 and nowhere else.

Future changes are held to the rules by where the rules are read:

- **`openspec/config.yaml` gains `rules:`**, which `openspec instructions` hands to whoever writes each artifact:
  - proposal: every defect claim carries **Proved by:** with the failing test, or **Unverified:** with the reason;
  - design: every decision that introduces or changes behaviour has **Default:** and **Override:** lines, or says why there is no override;
  - tasks: a change that fixes a defect opens with the red task.
- **`.claude/rules/plans-beside-tasks.md`** adds two items to "What the plan must contain":
  - the **Mapping to `tasks.md`** table that `development-workflow.md` already expects;
  - the explicit list of skills and rules that Global Constraints must name.

### D10. Fuzz targets are the parsers that take bytes from outside

- **`FuzzDecodeCursor` and `FuzzHandlerRequests`** land from the audit's `audit_fuzz_test.go`, renamed without `Audit`, in `cursor_fuzz_test.go` and `http_fuzz_test.go`.
  - The audit ran about 1.25M inputs through them with no panic.
- **`FuzzDecodeSignals`** (`codec_fuzz_test.go`): no input panics `DecodeSignals`. When it decodes, encoding the result and decoding again gives the same signals.
  - The redis and nats adapters feed it bytes from the broker.
- **`FuzzDraftValidate`** (`notification_fuzz_test.go`): no draft panics `Validate`. Every refusal is a `*ValidationError`.
- **The fuzz tests are seeds-only in `go test`.** A fuzzing run is manual: `go test -run '^$' -fuzz '^FuzzX$' -fuzztime 60s .`, as `CONTRIBUTING.md` documents.
  - They pass against today's code. The red step is a temporary mutation that panics on a seed, or returns a non-validation error. The test must notice it, and the mutation is reverted.

## Order

Groups are independent, but three shared files are edited in this order:

- **`service_ops_test.go` and `service_publish_test.go`:** group 2 (the `recording` swap) goes before group 3 (ctx rows and folds).
- **`email_dispatch_test.go`:** group 2, then group 3, then group 5's `slicesbackward` fix.
- **`.golangci.yml`:** group 5 lands last among the code groups, so that its triage runs over the rewritten tests.

A group applied out of order rebases on these files. The conflicts are textual and local.

## Risks / Trade-offs

- **[Risk]** The `t.Context()` swap reorders shutdown in helpers that stop a hub at cleanup, and a websocket, redis or nats test becomes flaky. → **Mitigation:** each site is run with `-count=20 -race`, and a failing site keeps `context.Background()` with a justification comment (D3).
- **[Risk]** `make tidy-check` with `-e` passes a module whose import is genuinely broken. → **Mitigation:** `make build` and `make test` in workspace mode fail on the same import. `tidy-check` only guards the markers.
- **[Risk]** `paralleltest` and `nolintlint` turn future harmless code red. → **Mitigation:** that is the intent. Each suppression must say why, which is what the rules ask.
- **[Trade-off]** Four generated mocks stay unused (D2). Accepted, as cheaper than a second directive.
- **[Risk]** CI's Docker leaves a goroutine that goleak flags in sqlstore. → **Mitigation:** a named ignore option, never `IgnoreCurrent` (D7).
- **[Trade-off]** The archived records keep their gaps (D9). The proposal lists them, so they are known rather than hidden.

## Migration Plan

None for hosts. For contributors:

- `make all` now includes `tidy-check`.
- The next `make tidy` leaves the sibling modules out of `go.mod`, as intended.

## Open Questions

- **Which licence?** Task 7.4 asks. The answer does not change any other task.
