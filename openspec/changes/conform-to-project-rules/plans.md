# Conform to Project Rules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the repository into line with its own rules (`.claude/rules/*.md`) and skills (`table-test`, `use-mockgen`, `use-testcontainers`), without changing any behaviour a host can observe.

**Architecture:**
- Ten independent groups, one per rule. Each carries its own red → green loop.
- Where no defect exists, the red step is a temporary mutation that the new test or check must notice. The tests landed here guard behaviour that already works.
- The checks that can be automated (tidiness, lint, goleak) join `make all` and CI, so the gaps cannot return.

**Tech Stack:**
- Go 1.26.8, testify, `go.uber.org/mock` v0.6.0 (`mockgen --typed`) and goleak v1.3.0;
- testcontainers-go, through the existing `RunTest*` helpers;
- golangci-lint v2.13.2 and `openspec`.

**Spec:** `openspec/changes/conform-to-project-rules/`. Read these first:
- `proposal.md`: every gap, with the command that shows it, and the list of what is already conformant;
- `design.md`: D1 toolchain and tidy, D2 doubles, D3 contexts, D4 assert values, D5 folds, D6 linters, D7 goleak, D8 docs, D9 records, D10 fuzz, and "Order".

There is no spec delta (`skip_specs: true`).

## Global Constraints

- **Toolchain.** Export `GOTOOLCHAIN=go1.26.8` before any Go command. The `make` targets already pin it.
- **Imports.**
  - The core module's production code imports only the standard library. Tests may use `testify`, `goleak` and `go.uber.org/mock`.
  - Each satellite module imports only `ntfy`, `sqlkit` and its own client library.
  - `make split-check` and depguard enforce this. This change adds no production import.
- **`pkg/sqlkit`** is a copy. It is not edited, linted or tidied here (`make sqlkit-copy-check`).
- **No behaviour change** (`skip_specs: true`).
  - Production code changes only in comments, in `nolint` directives, and in `modernize`'s mechanical rewrites, which are all in test or ntfytest code.
  - Anything else is out of scope.
- **`table-test` skill:**
  - an `assert` closure on every case;
  - a `ctx` modifier and a cancelled row where behaviour depends on the context;
  - `t.Context()`, not `context.Background()`, unless a comment justifies it.
- **`use-mockgen` skill:** doubles are generated with `--typed`, as amended by Task 2 (D2). Never edit a `*_mock_test.go` by hand. After `go generate ./...`, `git status` stays clean.
- **`use-testcontainers` skill:** use `sqlkittest.RunTestPostgres`, `RunTestMySQL` and `RunTestSQLite`, `redis.RunTestRedis` and `nats.RunTestNATS`. Never write a container helper.
- **`.claude/rules/golang-tdd.md`:**
  - Red, then green, then refactor.
  - Where the behaviour already works, the red step inverts the implementation temporarily and watches the test fail.
  - The test and the change it guards land in one commit.
- **`.claude/rules/prove-errors-with-tests.md`:**
  - The only defect this change claims is that the `context.WithoutCancel` path is untested. `proposal.md` item 5 proves it by mutation.
  - Any new defect found while applying is proved by a failing test before it is fixed.
- **`.claude/rules/performance-benchmark.md`:**
  - No performance claim is made.
  - `TestMeasureClaim` and the memory gates are touched only structurally, and are re-run (Tasks 3 and 5) with their thresholds unchanged.
- **`.claude/rules/library-design.md`:** no default changes. Task 4 makes godoc name the defaults that exist.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn.
- **`.claude/rules/gopls-navigation.md`:** use gopls to find references before a rename or fold (`TestVerifySchemaOn*`, `recording`, `failingClaims`).
- **Done means `make all` and `make store-matrix` pass.** sqlstore's tests change, and Docker must be running.
- **Commit messages** are in imperative sentence case with no `feat:` prefix, and end with the session's attribution lines.

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `.github/workflows/ci.yml` | — | **Modify.** `GO_VERSION: "1.26.8"`; a `make tidy-check` step in the lint job. |
| `Makefile` | — | **Modify.** `TIDY_ENV`, the `tidy` change and the new `tidy-check` target, which joins `all`. |
| `go.mod`, `go.sum` in `.`, `ntfytest`, `sqlstore`, `websocket`, `redis`, `nats` | all | **Regenerate** with `make tidy`. |
| `.claude/skills/use-mockgen/SKILL.md` | — | **Modify.** The "Decorators over a real implementation" section. |
| `service_ops_test.go` | `ntfy` | **Modify.** Remove `recording` and use `MockBroadcaster`; add a `ctx` field and cancelled rows to `TestServiceClose`, `TestServiceMarkRead` and `TestServiceMarkAllRead`; give `TestServiceReads` an `assert`; move `TestServiceCloseHonoursConfiguredLimits` out. |
| `service_publish_test.go` | `ntfy` | **Modify.** Add `requestKey`, `cancelledRequest` and `broadcastsDetached`; a `ctx` field and cancelled row in `TestServicePublish`; fold in `TestServicePublishRefusesTooManyDrafts`; replace `TestServicePublishHonoursConfiguredLimits` with the `TestServiceHonoursConfiguredLimits` table. |
| `email_dispatch_test.go` | `ntfy` | **Modify.** `MockEmailStore` replaces `failingClaims`; `ClockFunc`; decorator comments; `slices.Backward`. |
| `email_test.go`, `service_test.go` | `ntfy` | **Modify.** Decorator comments on `storeWithoutEmail` and `customBroadcaster`. |
| `ntfytest/identity_test.go` | `ntfytest` | **Modify.** A decorator comment on `foldingStore`; a `nolint:gosec,paralleltest` on the child. |
| `docs_test.go`, `example_email_test.go` | `ntfy` | **Modify.** The `mentions` assert value. |
| `limits_test.go`, `notification_test.go`, `http_test.go`, `hub_test.go`, `authorize_test.go` | `ntfy` | **Modify.** `assert` closures, folds and contexts. |
| `ntfytest/isolation.go`, `ntfytest/suite.go`, `ntfytest/doc.go` | `ntfytest` | **Modify.** An `assert` on `readCase`; the note that the suite has no `ctx` field. |
| `websocket/authority_test.go`, `websocket/readloop_test.go`, `websocket/helpers_test.go`, `websocket/shutdown_test.go` | `websocket` | **Modify.** `assert` closures and contexts. |
| `redis/listen_test.go`, `redis/multiinstance_test.go`, `nats/listen_test.go`, `nats/multiinstance_test.go`, `nats/reconnect_test.go` | redis, nats | **Modify.** Contexts; noctx fixes. |
| `sqlstore/verify_test.go`, `sqlstore/email_verify_test.go`, `sqlstore/email_claim_measure_test.go`, `sqlstore/harness_test.go` | `sqlstore` | **Modify.** Dialect tables; `assert` closures; `nolint`s. |
| `clock.go`, `email.go`, `email_dispatcher.go`, `store.go`, `websocket/handler.go` | ntfy, websocket | **Modify.** Godoc (Task 4); `nolint`s (Task 5). |
| `hub.go`, `id.go`, `http.go`, `sqlstore/sql.go`, `memory_bench_test.go`, `hub_test.go` | ntfy, sqlstore | **Modify.** `nolint`s and `modernize` fixes. |
| `.golangci.yml` | — | **Modify.** Linters and settings (D6). |
| `sqlstore/main_test.go`, `sqlstore/internal/gormtest/main_test.go` | `sqlstore` | **Create.** `goleak.VerifyTestMain`. |
| `example_test.go` | `ntfy` | **Modify.** Add `ExampleNew`. |
| `sqlstore/example_test.go`, `redis/example_test.go`, `nats/example_test.go`, `ntfytest/example_test.go` | each | **Create.** Examples. |
| `CHANGELOG.md`, `CONTRIBUTING.md` | — | **Create.** |
| `LICENSE`, `README.md` | — | **Create / Modify**, only after the maintainer answers (Task 7). |
| `cursor_fuzz_test.go`, `http_fuzz_test.go`, `codec_fuzz_test.go`, `notification_fuzz_test.go` | `ntfy` | **Create.** Fuzz targets. |
| `openspec/config.yaml`, `.claude/rules/plans-beside-tasks.md` | — | **Modify.** Artifact rules and plan contents (D9). |

**Shared-file note:** `design.md` "Order" governs `service_*_test.go`, `email_dispatch_test.go` and `.golangci.yml`.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: toolchain pin and tidiness | 1.1, 1.2, 1.3 |
| Task 2: generated doubles | 2.1, 2.2, 2.3, 2.4 |
| Task 3: tables and contexts | 3.1, 3.2, 3.3, 3.4, 3.5, 3.6 |
| Task 4: godoc names its defaults | 4.1 |
| Task 5: linters | 5.1, 5.2 |
| Task 6: goleak in sqlstore | 6.1 |
| Task 7: examples and repository documents | 7.1, 7.2, 7.3, 7.4 |
| Task 8: fuzz tests | 8.1, 8.2 |
| Task 9: process rules and templates | 9.1, 9.2 |
| Task 10: verify and hand off | 10.1, 10.2 |

---

### Task 1: Toolchain pin and module tidiness

**Files:**
- Modify: `.github/workflows/ci.yml:13-16,32-38`, `Makefile:40-41` (`.PHONY`, `all`) and the `tidy` target.
- Regenerate: `go.mod` and `go.sum` in `.`, `ntfytest`, `sqlstore`, `websocket`, `redis` and `nats`.

**Interfaces:**
- Consumes: nothing.
- Produces:
  - the `make tidy-check` target, which exits non-zero on any diff;
  - `TIDY_ENV := GOPRIVATE=github.com/kartaladev GOVCS=private:off`.

- [ ] **Step 1: Pin CI**

Replace the `env:` block in `.github/workflows/ci.yml`:

```yaml
env:
  GO_VERSION: "1.26.8"
  # The patch release the Makefile's GOTOOLCHAIN and .envrc pin. actions/setup-go
  # sets GOTOOLCHAIN=local once it is installed, so nothing here downloads a
  # second toolchain; the Makefile's own pin defers to that.
```

- [ ] **Step 2: Add the check, and watch it fail (red)**

In the `Makefile`:
- change `.PHONY` to include `tidy-check`;
- change `all: lint split-check test` to `all: lint split-check tidy-check test`;
- replace the `tidy:` target with:

```make
# TIDY_ENV keeps go mod tidy away from the sibling modules. go.work supplies
# them until they are tagged, and none may be written into a go.mod as a
# pseudo-version: GOVCS=private:off stops the go command fetching them, and -e
# lets tidy finish without them. make build and make test, in workspace mode,
# still fail on a genuinely broken import.
TIDY_ENV := GOPRIVATE=github.com/kartaladev GOVCS=private:off

## tidy: tidy every ntfy module, leaving the go.work siblings out of go.mod.
tidy:
	@set -e; for m in $(NTFY_MODULES); do \
		(cd $$m && $(TIDY_ENV) $(GO) mod tidy -e); \
	done
	$(GO) work sync

## tidy-check: fail when an ntfy module's go.mod or go.sum is not tidy.
tidy-check:
	@fail=0; for m in $(NTFY_MODULES); do \
		echo "==> tidy-check $$m"; \
		(cd $$m && $(TIDY_ENV) $(GO) mod tidy -e -diff) || fail=1; \
	done; \
	if [ $$fail -ne 0 ]; then echo "tidy-check: run make tidy"; exit 1; fi
```

In `.github/workflows/ci.yml`, after `- run: make split-check` in the lint job, add:

```yaml
      # Every go.mod lists what it imports directly, without the go.work siblings.
      - run: make tidy-check
```

Run: `make tidy-check 2>&1 | grep -E '^[-+]' | grep -c kartaladev; make tidy-check >/dev/null 2>&1; echo "exit=$?"`

Expected:
- `0` kartaladev lines;
- `exit=2`, with make failing on the diff;
- the diff shows, for example, `-	github.com/stretchr/testify v1.12.1 // indirect` and `+	go.yaml.in/yaml/v3 v3.0.5 // indirect`.

- [ ] **Step 3: Tidy (green)**

Run: `make tidy && make tidy-check && echo TIDY`

Expected: six `==> tidy-check` lines, then `TIDY`.

Run: `git diff -- '*go.mod' | grep -c 'kartaladev'`

Expected: `0`.

- [ ] **Step 4: Build and test**

Run: `make build && make test`

Expected: every module compiles and passes.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml Makefile go.mod go.sum ntfytest/go.mod ntfytest/go.sum sqlstore/go.mod sqlstore/go.sum websocket/go.mod websocket/go.sum redis/go.mod redis/go.sum nats/go.mod nats/go.sum go.work.sum
git commit -m "Pin CI to go1.26.8 and keep every go.mod tidy"
```

---

### Task 2: Generated test doubles

**Files:**
- Modify: `.claude/skills/use-mockgen/SKILL.md`, after the section "## Rules".
- Modify: `service_ops_test.go:413-452` (`recording`) and `:454-500` (`TestServiceOnTheMemoryStore`).
- Modify: `email_dispatch_test.go:25-26,41-42,100-101,188,1447-1461`.
- Modify: `email_test.go:158`, `service_test.go:15-16`, `ntfytest/identity_test.go` (the `foldingStore` comment).

**Interfaces:**
- Consumes: `ntfy.NewMockBroadcaster(ctrl)`, `ntfy.NewMockEmailStore(ctrl)` (generated), `ntfy.ClockFunc`.
- Produces: nothing new. `failingClaimDispatcher.dispatch(ctx) (ntfy.DispatchResult, error)` keeps its signature.

- [ ] **Step 1: Amend the skill**

Append to `.claude/skills/use-mockgen/SKILL.md`:

```markdown
## Decorators over a real implementation

A type that **embeds a real implementation** of a port, and overrides a method
or two to inject a fault or to observe calls, is not a test double. The test
relies on the real implementation's behaviour for every method it does not
override, and a generated mock would have to re-implement that behaviour with a
delegating `DoAndReturn` per method. Such a decorator stays hand-written, and
carries a one-line comment naming the implementation it decorates:

    // deletingStore decorates a MemoryStore so that reading chosen
    // notifications finds them gone, or fails.

Everything else that stands in for a port is generated:

- a type that implements a port **on its own**, with no embedded real
  implementation, is a double: use the generated `Mock<Name>`;
- a type whose only purpose is to return a canned value or error is a stub,
  even when it embeds a real implementation: use the generated mock, which
  also proves that nothing else is called.

The library's exported function adapters (`ClockFunc`, `MailerFunc`,
`AddressBookFunc`, `EmailTemplateFunc`, `EmailFilterFunc`, `IDGeneratorFunc`,
`SubscriptionAuthorizerFunc`) are public API, not doubles. Use them inline for a
one-function port.
```

Run: `grep -n 'Decorators over a real implementation' .claude/skills/use-mockgen/SKILL.md`

Expected: one line.

- [ ] **Step 2: Replace `recording` with `MockBroadcaster`**

In `TestServiceOnTheMemoryStore`, replace `broadcaster := &recording{}` and the `ntfy.New` line with the code below. Add `"go.uber.org/mock/gomock"` to the imports if the file does not have it.

```go
	ctrl := gomock.NewController(t)
	broadcaster := ntfy.NewMockBroadcaster(ctrl)

	var (
		mu      sync.Mutex
		signals []ntfy.Signal
	)

	broadcaster.EXPECT().Broadcast(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, s []ntfy.Signal) error {
			mu.Lock()
			defer mu.Unlock()

			signals = append(signals, s...)

			return nil
		})
	broadcaster.EXPECT().Listen(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, _ func(ntfy.Signal), ready func()) error {
			ready()
			<-ctx.Done()

			return ctx.Err()
		})

	// changes returns the recorded changes for a recipient, in order.
	changes := func(recipient string) []ntfy.Change {
		mu.Lock()
		defer mu.Unlock()

		var out []ntfy.Change

		for _, signal := range signals {
			if signal.Recipient == recipient {
				out = append(out, signal.Change)
			}
		}

		return out
	}

	svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithBroadcaster(broadcaster))
	require.NoError(t, err)
```

Then:
- change `broadcaster.changes("alice")` and `broadcaster.changes("bob")` to `changes("alice")` and `changes("bob")`;
- delete the `recording` type and its three methods (`service_ops_test.go:413-452`).

Run: `go test -run 'TestServiceOnTheMemoryStore' -count=1 -race . && grep -c 'type recording' service_ops_test.go`

Expected: `ok`, then `0`.

- [ ] **Step 3: Watch the mock notice a missing signal (red check)**

Temporarily change `ntfy.ChangeRead` to `ntfy.ChangeClosed` in the test's expected `alice` slice.

Run: `go test -run 'TestServiceOnTheMemoryStore' -count=1 .`

Expected: FAIL, with a diff showing `ChangeRead` recorded. Revert the edit.

- [ ] **Step 4: Replace `failingClaims` with `MockEmailStore`**

Replace `email_dispatch_test.go:1447-1461` (the `failingClaimDispatcher` type, `failingClaims`, and `dispatch`) with:

```go
// failingClaimDispatcher builds a dispatcher over an email store whose claims
// fail. Only ClaimEmails is expected: a dispatch whose claim fails records and
// purges nothing, and gomock fails the case if it tries.
type failingClaimDispatcher struct{ h *dispatchHarness }

func (f *failingClaimDispatcher) dispatch(ctx context.Context) (ntfy.DispatchResult, error) {
	email := ntfy.NewMockEmailStore(gomock.NewController(f.h.t))
	email.EXPECT().ClaimEmails(gomock.Any(), gomock.Any()).Return(nil, errClaim)

	svc, err := ntfy.New(combinedStore{Store: f.h.store, EmailStore: email})
	require.NoError(f.h.t, err)

	d, err := ntfy.NewEmailDispatcher(svc, noopMailer, noopBook, noopTemplate)
	require.NoError(f.h.t, err)

	return d.Dispatch(ctx)
}
```

Run: `go test -run 'TestEmailDispatcherDispatch/a_failed_claim_is_returned' -count=1 -v . | grep -E -- '--- (PASS|FAIL)'`

Expected: `--- PASS: TestEmailDispatcherDispatch/a_failed_claim_is_returned`.

- [ ] **Step 5: Watch gomock refuse an extra call (red check)**

Temporarily add `email.EXPECT().RecordEmails(gomock.Any(), gomock.Any()).Times(1)` after the `ClaimEmails` expectation.

Run: `go test -run 'TestEmailDispatcherDispatch/a_failed_claim_is_returned' -count=1 .`

Expected: FAIL with `missing call(s) to *ntfy.MockEmailStore.RecordEmails`. Revert the line.

- [ ] **Step 6: Hand the clock over through `ClockFunc`, and label the decorators**

In `newDispatchHarness`, change `ntfy.WithClock(h.clock)` to `ntfy.WithClock(ntfy.ClockFunc(h.clock.Now))`. Then set these comments.

`email_dispatch_test.go:25`:
```go
// movableClock is the time a test moves by hand. The service reads it through
// ntfy.ClockFunc, the Clock port's own adapter.
```

`email_dispatch_test.go:41-42`:
```go
// recordingEmailStore decorates a MemoryStore's email side, keeping every
// record written and, once dropped is set, writing none, as a process that
// stopped would.
```

`email_dispatch_test.go:100-101`:
```go
// deletingStore decorates a MemoryStore so that reading chosen notifications
// finds them gone, or fails.
```

`email_test.go:158`:
```go
// storeWithoutEmail decorates a MemoryStore, hiding its EmailStore methods.
```

`service_test.go:15-16`:
```go
// customBroadcaster decorates an InProcessBroadcaster, told apart from the
// default by its type.
```

`ntfytest/identity_test.go`: prefix the existing `foldingStore` comment with the sentence `foldingStore decorates a MemoryStore, folding every identifier it is given.` and keep the rest of the comment.

Run: `go test -count=1 -race . && (cd ntfytest && go test -count=1 ./...) && go generate ./... && git status --short -- '*_mock_test.go'`

Expected: two `ok` lines and no mock diff.

- [ ] **Step 7: Commit**

```bash
git add .claude/skills/use-mockgen/SKILL.md service_ops_test.go email_dispatch_test.go email_test.go service_test.go ntfytest/identity_test.go
git commit -m "Generate the broadcaster and claim doubles, and state the decorator exception"
```

---

### Task 3: Table-driven tests and contexts

**Files:**
- Modify: `service_publish_test.go`, `service_ops_test.go`, `docs_test.go`, `example_email_test.go`, `limits_test.go`, `notification_test.go`, `http_test.go`, `hub_test.go`, `authorize_test.go`.
- Modify: `ntfytest/isolation.go`, `ntfytest/suite.go`, `ntfytest/doc.go`.
- Modify: `websocket/authority_test.go`, `websocket/readloop_test.go`, `websocket/helpers_test.go`, `websocket/shutdown_test.go`.
- Modify: `redis/listen_test.go`, `redis/multiinstance_test.go`, `nats/listen_test.go`, `nats/multiinstance_test.go`.
- Modify: `sqlstore/verify_test.go`, `sqlstore/email_verify_test.go`, `sqlstore/email_claim_measure_test.go`.

**Interfaces:**
- Consumes: `mocked(t) (*ntfy.Service, *ntfy.MockStore, *ntfy.MockBroadcaster, *signalErrors)`, `insertReturns`, `serviceAt` and `docSection(t, document, heading string) string`, all existing.
- Produces, in package `ntfy_test`:
  - `type requestKey struct{}`;
  - `func cancelledRequest(ctx context.Context) context.Context`;
  - `func broadcastsDetached(t *testing.T, broadcaster *ntfy.MockBroadcaster)`;
  - `func mentions(needles ...string) func(t *testing.T, section string)`;
  - `TestServiceHonoursConfiguredLimits`, `TestDraftValidateWithin`.
- Produces, in `sqlstore_test`: `TestVerifySchema` and `TestVerifyEmailSchema`, which replace `TestVerify(Email)?SchemaOn{Postgres,MySQL,SQLite}`.

- [ ] **Step 1: Add the context helpers, and the cancelled row to `TestServicePublish`**

Add to `service_publish_test.go`, after `mocked`:

```go
// requestKey marks the value a request's context carries.
type requestKey struct{}

// cancelledRequest is a request context that carries a value and is already
// cancelled, as a client hanging up the instant after a commit leaves it.
func cancelledRequest(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(context.WithValue(ctx, requestKey{}, "req-1"))
	cancel()

	return ctx
}

// broadcastsDetached expects one broadcast, and asserts that its context has
// been detached from the request's cancellation but keeps the request's values.
func broadcastsDetached(t *testing.T, broadcaster *ntfy.MockBroadcaster) {
	t.Helper()

	broadcaster.EXPECT().Broadcast(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ []ntfy.Signal) error {
			assert.NoError(t, ctx.Err(), "the broadcast is detached from the request's cancellation")
			assert.Equal(t, "req-1", ctx.Value(requestKey{}), "the broadcast keeps the request's values")

			return nil
		})
}
```

In `TestServicePublish`:
- add the field `ctx func(ctx context.Context) context.Context // nil means t.Context()` after `drafts`;
- replace the loop body's call with:

```go
			ctx := t.Context()
			if tc.ctx != nil {
				ctx = tc.ctx(ctx)
			}

			result, err := svc.Publish(ctx, tc.drafts...)
			tc.assert(t, result, err, handler.all())
```

Add this row:

```go
		{
			name:   "a cancelled request still broadcasts, keeping its values",
			drafts: []ntfy.Draft{draft("alice", "task-1")},
			ctx:    cancelledRequest,
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().Insert(gomock.Any(), "task-1", gomock.Any()).DoAndReturn(
					insertReturns(func(ins []ntfy.Insertion) ntfy.InsertResult {
						return ntfy.InsertResult{Created: []ntfy.Notification{ins[0].Notification}}
					}))
				broadcastsDetached(t, broadcaster)
			},
			assert: func(t *testing.T, result ntfy.PublishResult, err error, handled []error) {
				require.NoError(t, err)
				assert.Len(t, result.Created, 1)
				assert.Empty(t, handled, "no broadcast error reached the handler")
			},
		},
```

- [ ] **Step 2: Watch it fail against the mutation (red)**

Temporarily change `service.go:323` from `detached := context.WithoutCancel(ctx)` to `detached := ctx`.

Run: `go test -run 'TestServicePublish$' -count=1 .`

Expected: FAIL in `a_cancelled_request_still_broadcasts,_keeping_its_values`, with `Received unexpected error: context canceled` and the message `the broadcast is detached from the request's cancellation`.

Restore the line.

- [ ] **Step 3: Green**

Run: `go test -run 'TestServicePublish$' -count=1 . && git diff --quiet -- service.go && echo CLEAN`

Expected: `ok`, then `CLEAN`.

- [ ] **Step 4: The same row for Close, MarkRead and MarkAllRead**

In each of `TestServiceClose`, `TestServiceMarkRead` and `TestServiceMarkAllRead`:
- add `ctx func(ctx context.Context) context.Context // nil means t.Context()`;
- derive the context in the loop, exactly as in Step 1;
- pass it to `svc.Close`, `svc.MarkRead` or `svc.MarkAllRead`.

Add these rows.

`TestServiceClose`:
```go
		{
			name: "a cancelled request still broadcasts the close, keeping its values",
			req:  ntfy.CloseRequest{Subject: "task-1", Kinds: []string{"offer"}, Version: 5, Reason: "taken"},
			ctx:  cancelledRequest,
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().Close(gomock.Any(), gomock.Any(), serviceAt, gomock.Any()).
					Return(ntfy.CloseResult{Closed: 1, Recipients: []string{"alice"}}, nil)
				broadcastsDetached(t, broadcaster)
			},
			assert: func(t *testing.T, result ntfy.CloseResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, []string{"alice"}, result.Recipients)
			},
		},
```

`TestServiceMarkRead`:
```go
		{
			name:      "a cancelled request still broadcasts the read, keeping its values",
			recipient: "alice",
			ids:       []string{"n-1"},
			ctx:       cancelledRequest,
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().MarkRead(gomock.Any(), "alice", []string{"n-1"}, serviceAt).
					Return(ntfy.MarkResult{Marked: 1}, nil)
				broadcastsDetached(t, broadcaster)
			},
			assert: func(t *testing.T, result ntfy.MarkResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, int64(1), result.Marked)
			},
		},
```

`TestServiceMarkAllRead` (`through` is the table's existing local):
```go
		{
			name:    "a cancelled request still broadcasts the read, keeping its values",
			through: through,
			ctx:     cancelledRequest,
			expect: func(t *testing.T, store *ntfy.MockStore, broadcaster *ntfy.MockBroadcaster) {
				store.EXPECT().MarkAllRead(gomock.Any(), "alice", through, serviceAt).
					Return(ntfy.MarkResult{Marked: 2}, nil)
				broadcastsDetached(t, broadcaster)
			},
			assert: func(t *testing.T, result ntfy.MarkResult, err error) {
				require.NoError(t, err)
				assert.Equal(t, int64(2), result.Marked)
			},
		},
```

Run the Step 2 mutation again, with `go test -run 'TestService(Close|MarkRead|MarkAllRead)$' -count=1 .`

Expected: three FAILs, one per new row, each with `context canceled`. Restore `service.go`.

Run: `go test -run 'TestService' -count=1 -race .`

Expected: `ok`.

- [ ] **Step 5: Commit the context rows**

```bash
git add service_publish_test.go service_ops_test.go
git commit -m "Prove a cancelled request still broadcasts what it changed"
```

- [ ] **Step 6: The `mentions` assert value, in the two docs tables**

Add to `docs_test.go`, after `docSection`:

```go
// mentions is the assert of a documentation row: the section names every
// needle.
func mentions(needles ...string) func(t *testing.T, section string) {
	return func(t *testing.T, section string) {
		for _, needle := range needles {
			assert.Containsf(t, section, needle, "the section mentions %s", needle)
		}
	}
}
```

In `TestTheDocumentMatchesTheImplementation` (`docs_test.go:61`) and `TestTheEmailDocumentMatchesTheImplementation` (`example_email_test.go:76`):
- replace the field `needles []string` with `assert func(t *testing.T, section string)`;
- in every row, replace `needles: []string{…},` with `assert: mentions(…),`, keeping the same values;
- replace the loop body with:

```go
			tc.assert(t, docSection(t, document, tc.section))
```

Red check: temporarily change one needle to `"no such text"`, and run `go test -run 'Document' -count=1 .`. Expected: FAIL on that row with `the section mentions no such text`. Revert.

- [ ] **Step 7: `TestServiceReads`, the inline-closure pattern**

Replace the whole of `TestServiceReads` (`service_ops_test.go:319-411`) with:

```go
func TestServiceReads(t *testing.T) {
	t.Parallel()

	// The reads do not signal, so no case depends on cancellation, and the
	// table has no ctx field.
	type testCase struct {
		name   string
		expect func(store *ntfy.MockStore)
		call   func(ctx context.Context, svc *ntfy.Service) (any, error)
		assert func(t *testing.T, result any, err error)
	}

	none := func(*ntfy.MockStore) {}
	refused := func(t *testing.T, _ any, err error) { assert.ErrorIs(t, err, ntfy.ErrValidation) }

	cases := []testCase{
		{
			name: "get passes through",
			expect: func(store *ntfy.MockStore) {
				store.EXPECT().Get(gomock.Any(), "alice", "n-1").Return(ntfy.Notification{ID: "n-1"}, nil)
			},
			call: func(ctx context.Context, svc *ntfy.Service) (any, error) { return svc.Get(ctx, "alice", "n-1") },
			assert: func(t *testing.T, result any, err error) {
				require.NoError(t, err)
				assert.Equal(t, "n-1", result.(ntfy.Notification).ID)
			},
		},
		{
			name:   "get needs a recipient",
			expect: none,
			call:   func(ctx context.Context, svc *ntfy.Service) (any, error) { return svc.Get(ctx, "", "n-1") },
			assert: refused,
		},
		{
			name:   "get needs an identifier",
			expect: none,
			call:   func(ctx context.Context, svc *ntfy.Service) (any, error) { return svc.Get(ctx, "alice", "") },
			assert: refused,
		},
		{
			name: "list passes a valid query through",
			expect: func(store *ntfy.MockStore) {
				store.EXPECT().List(gomock.Any(), ntfy.ListQuery{Recipient: "alice", Limit: 10}).
					Return(ntfy.Page{NextCursor: "next"}, nil)
			},
			call: func(ctx context.Context, svc *ntfy.Service) (any, error) {
				return svc.List(ctx, ntfy.ListQuery{Recipient: "alice", Limit: 10})
			},
			assert: func(t *testing.T, result any, err error) {
				require.NoError(t, err)
				assert.Equal(t, "next", result.(ntfy.Page).NextCursor)
			},
		},
		{
			name:   "list refuses an invalid query without asking the store",
			expect: none,
			call: func(ctx context.Context, svc *ntfy.Service) (any, error) {
				return svc.List(ctx, ntfy.ListQuery{Recipient: "alice", Limit: ntfy.MaxListLimit + 1})
			},
			assert: refused,
		},
		{
			name: "count passes through",
			expect: func(store *ntfy.MockStore) {
				store.EXPECT().CountActive(gomock.Any(), "alice").Return(int64(7), nil)
			},
			call: func(ctx context.Context, svc *ntfy.Service) (any, error) { return svc.CountActive(ctx, "alice") },
			assert: func(t *testing.T, result any, err error) {
				require.NoError(t, err)
				assert.Equal(t, int64(7), result)
			},
		},
		{
			name:   "count needs a recipient",
			expect: none,
			call:   func(ctx context.Context, svc *ntfy.Service) (any, error) { return svc.CountActive(ctx, "") },
			assert: refused,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, store, _, _ := mocked(t)
			tc.expect(store)

			result, err := tc.call(t.Context(), svc)
			tc.assert(t, result, err)
		})
	}
}
```

Red check: temporarily change `int64(7)` in the assert to `int64(8)`, and run `go test -run 'TestServiceReads' -count=1 .`. Expected: FAIL on `count_passes_through`. Revert.

- [ ] **Step 8: The remaining seven tables without `assert`**

Apply the same pattern: move what the loop asserts into a per-row `assert` closure, or into a shared value where every row asserts the same thing (D4).

1. **`limits_test.go:49` `TestWithLimitsSnapshotsHostValues`:**
   - Add `assert func(t *testing.T, err error)`.
   - Both rows get `assert: stillEnforced`, where:
     ```go
     stillEnforced := func(t *testing.T, err error) {
     	assert.ErrorIs(t, err, ntfy.ErrValidation, "the service keeps enforcing the limits it was given, not the host's later mutation")
     }
     ```
   - The loop ends with `tc.assert(t, err)`.
2. **`notification_test.go:384` `TestIdentifiersMustBeWellFormed`:**
   - Replace the `pointer string` field with `assert func(t *testing.T, err error)`.
   - In every row, replace `pointer: "/x",` with `assert: refusedAt("/x"),`, keeping the same pointer. `refusedAt` is declared before `cases`, and holds the assertions the loop body has today:
     ```go
     // refusedAt is the assert of a row: the identifier is refused, and an
     // issue points at it.
     refusedAt := func(pointer string) func(t *testing.T, err error) {
     	return func(t *testing.T, err error) {
     		require.ErrorIs(t, err, ntfy.ErrValidation)

     		var validation *ntfy.ValidationError
     		require.ErrorAs(t, err, &validation)

     		pointers := make([]string, 0, len(validation.Issues))
     		for _, issue := range validation.Issues {
     			pointers = append(pointers, issue.Pointer)
     		}

     		assert.Contains(t, pointers, pointer)
     	}
     }
     ```
   - Keep the loop's inner iteration over the NUL and invalid-UTF-8 values. The subtest body becomes `tc.assert(t, tc.validate(bad.value))`.
3. **`ntfytest/isolation.go:167` `readCase`:**
   - Add `assert func(t *testing.T, e *env, first ntfy.Notification)`.
   - Move the lines after `hijack(...)`/`*first.ReadAt = at(99)` (the `second := e.get(...)`, `intact`, `sameInstant` and `hijacked` calls) into a shared `independent` value, assigned to both rows.
4. **`websocket/authority_test.go:22` `TestMarkOnFollowedConnection`:**
   - Add `assert func(t *testing.T, s *server, reply string)`.
   - Move everything after `reply := string(readRaw(t, d.conn))` into a shared `changesNothing` value, assigned to both rows.
   - The loop ends with `tc.assert(t, s, reply)`.
5. **`websocket/readloop_test.go:72` `TestReadLoop`:**
   - Rename the field `act` to `assert`, since each row's closure already acts and asserts.
   - Change `tc.act(t, s, d.conn)` to `tc.assert(t, s, d.conn)`.
   - Rename `act:` to `assert:` in every row.
6. **`sqlstore/email_claim_measure_test.go:499` `TestMeasureClaim`:**
   - Add `assert func(t *testing.T, dialect sqlkit.Dialect, plan, idle string, empty, grown time.Duration)`.
   - Declare `holdsTheBound` before `cases`, holding the two thresholds that end the loop body today, and assign it to all three rows:
     ```go
     // holdsTheBound is every row's assert: design.md D3 of
     // scale-email-claim-query states the same thresholds for every dialect.
     holdsTheBound := func(t *testing.T, dialect sqlkit.Dialect, plan, idle string, empty, grown time.Duration) {
     	// Threshold 1: no branch scans the notifications table in full.
     	assert.Empty(t, fullScans(t, dialect, plan), "a full scan of the notifications table with work available")
     	assert.Empty(t, fullScans(t, dialect, idle), "a full scan of the notifications table on an idle pass")

     	// Threshold 2: the empty pass costs what the window holds, not what the
     	// table holds.
     	limit := time.Duration(float64(empty)*growthRatio) + growthSlack
     	assert.LessOrEqualf(t, grown, limit,
     		"an empty pass went from %s to %s when rows were added outside the window", empty, grown)
     }
     ```
   - The loop body ends with `tc.assert(t, tc.dialect, plan, idle, empty, grown)` in place of the two threshold blocks.
   - Re-run `cd sqlstore && NTFY_MEASURE_ROWS=20000 go test -run TestMeasureClaim -count=1 -timeout 30m -v .`, with Docker. Expected: `--- PASS` for all three dialects.
7. **`example_email_test.go:76`:** done in Step 6.

For each of the seven, invert one row's expectation temporarily, watch that row alone fail, and revert.

Run: `go test -count=1 . && (cd ntfytest && go test -count=1 ./...) && (cd websocket && go test -count=1 ./...)`

Expected: three `ok` lines.

- [ ] **Step 9: The note that the store suite has no ctx field**

Append this paragraph to `Run`'s godoc (`ntfytest/suite.go:27`) and to `ntfytest/doc.go`:

```go
// The Store contract makes no behaviour depend on context cancellation, so the
// suite's tables carry no ctx field and no cancelled-context case: a store that
// honours cancellation and one that does not both conform.
```

Run: `go doc github.com/kartaladev/ntfy/ntfytest Run | grep -c cancellation`

Expected: `1`.

- [ ] **Step 10: Commit the tables**

```bash
git add docs_test.go example_email_test.go service_ops_test.go limits_test.go notification_test.go ntfytest/isolation.go ntfytest/suite.go ntfytest/doc.go websocket/authority_test.go websocket/readloop_test.go sqlstore/email_claim_measure_test.go
git commit -m "Give every table an assert closure"
```

- [ ] **Step 11: Replace `context.Background()` in tests**

Change each site as follows:

| Site | Change |
| --- | --- |
| `authorize_test.go:34,39` | `func(ctx context.Context, actor, recipient string) error { ... ntfy.SelfOnly.AuthorizeSubscription(ctx, actor, recipient) }` |
| `hub_test.go:332` | `broadcaster.Broadcast(t.Context(), []ntfy.Signal{last})` |
| `websocket/shutdown_test.go:56,159` | `conn.Read(t.Context())` |
| `websocket/helpers_test.go:80,180`, `websocket/authority_test.go:192`, `websocket/shutdown_test.go:40,130` | `context.WithCancel(t.Context())` |
| `redis/listen_test.go:55`, `redis/multiinstance_test.go:51,110` | `context.WithCancel(t.Context())` |
| `nats/listen_test.go:55`, `nats/multiinstance_test.go:51,136` | `context.WithCancel(t.Context())` |

Run each package 20 times under the race detector:

```bash
go test -run 'TestSubscriptionAuthorizers|TestHubDelivery' -count=20 -race .
(cd websocket && go test -count=20 -race ./...)
(cd redis && go test -run 'TestListen|TestSignalsCrossInstances' -count=20 -race ./...)
(cd nats && go test -run 'TestListen|TestSignalsCrossInstances' -count=20 -race ./...)
```

Expected: `ok` for each.

If a site fails, restore `context.Background()` there, with a comment of this form:

```go
	// Background, not t.Context(): t.Context() is cancelled before cleanups
	// run, and this hub must outlive the servers the cleanups close first.
	ctx, cancel := context.WithCancel(context.Background())
```

Run: `grep -rn 'context.Background()' --include='*_test.go' . | grep -v '^./pkg\|^./.claude\|/example'`

Expected: only sites carrying such a comment on the line above.

- [ ] **Step 12: Commit the contexts**

```bash
git add authorize_test.go hub_test.go websocket redis nats
git commit -m "Tie test contexts to t.Context"
```

- [ ] **Step 13: Fold the standalone tests into their tables**

Before editing, record the subtest counts:

```bash
go test -run 'TestDraftValidate|TestServicePublish|TestServiceClose|TestHandlerStream|TestHubStop|TestHubCaps|TestHubInstanceCap' -v -count=1 . | grep -c -- '--- PASS'
```

Fold each test as follows. In every case, the test's body moves into the row's `assert` closure, unchanged except for the names of its inputs:

1. **`TestDraftValidateOversizedLinksSkipsPerLinkChecks`** (`notification_test.go:236`) becomes the row "an over-count link map reports only the count issue" of `TestDraftValidateContentLimits`.
2. **`TestDraftValidateWithinRaisedLimits`** (`:253`) becomes the new table:
   ```go
   func TestDraftValidateWithin(t *testing.T) {
   	t.Parallel()

   	payload := json.RawMessage(`{"padding":"` + strings.Repeat("p", ntfy.DefaultMaxDataBytes) + `"}`)

   	type testCase struct {
   		name   string
   		limits ntfy.Limits
   		assert func(t *testing.T, err error)
   	}

   	cases := []testCase{
   		{
   			name:   "the default limit refuses an oversized payload",
   			limits: ntfy.Limits{},
   			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, ntfy.ErrValidation) },
   		},
   		{
   			name:   "a host that raises the limit accepts it",
   			limits: ntfy.Limits{MaxDataBytes: ntfy.Limit(1 << 20)},
   			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
   		},
   	}

   	for _, tc := range cases {
   		t.Run(tc.name, func(t *testing.T) {
   			t.Parallel()

   			draft := validDraft()
   			draft.Data = payload

   			tc.assert(t, draft.ValidateWithin(tc.limits))
   		})
   	}
   }
   ```
3. **`TestDraftValidateDoesNotRewriteAnAcceptedHref`** (`:348`) becomes a row of `TestDraftValidateLinkSchemes`. Its assert also checks that `draft.Links["task"]` is unchanged.
4. **`TestServicePublishRefusesTooManyDrafts`** (`service_publish_test.go:300`) becomes the row "too many drafts are refused before any transaction" of `TestServicePublish`, with `expect` doing nothing.
5. **`TestServicePublishHonoursConfiguredLimits`** (`service_publish_test.go:324`) and **`TestServiceCloseHonoursConfiguredLimits`** (`service_ops_test.go:49`) become one table in `service_publish_test.go`:
   ```go
   func TestServiceHonoursConfiguredLimits(t *testing.T) {
   	t.Parallel()

   	payload := json.RawMessage(`{"padding":"` + strings.Repeat("p", ntfy.DefaultMaxDataBytes) + `"}`)
   	raised := []ntfy.Option{ntfy.WithLimits(ntfy.Limits{MaxDataBytes: ntfy.Limit(1 << 20)})}

   	publish := func(ctx context.Context, svc *ntfy.Service) (any, error) {
   		return svc.Publish(ctx, ntfy.Draft{
   			Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer", Data: payload,
   		})
   	}

   	publishAndClose := func(ctx context.Context, svc *ntfy.Service) (any, error) {
   		if _, err := svc.Publish(ctx, ntfy.Draft{
   			Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer",
   		}); err != nil {
   			return nil, err
   		}

   		return svc.Close(ctx, ntfy.CloseRequest{
   			Subject: "task-1", Version: 1,
   			Successor: &ntfy.Successor{SourceID: "event-2", Kind: "taken", SubjectVersion: 2, Data: payload},
   		})
   	}

   	refused := func(t *testing.T, _ *ntfy.Service, _ any, err error) { assert.ErrorIs(t, err, ntfy.ErrValidation) }

   	type testCase struct {
   		name   string
   		opts   []ntfy.Option
   		call   func(ctx context.Context, svc *ntfy.Service) (any, error)
   		assert func(t *testing.T, svc *ntfy.Service, result any, err error)
   	}

   	cases := []testCase{
   		{name: "the default payload limit refuses an oversized draft", call: publish, assert: refused},
   		{
   			name: "a raised payload limit accepts a draft and returns it byte for byte",
   			opts: raised,
   			call: publish,
   			assert: func(t *testing.T, svc *ntfy.Service, result any, err error) {
   				require.NoError(t, err)
   				created := result.(ntfy.PublishResult).Created
   				require.Len(t, created, 1)

   				stored, err := svc.Get(t.Context(), "alice", created[0].ID)
   				require.NoError(t, err)
   				assert.Equal(t, []byte(payload), []byte(stored.Data))
   			},
   		},
   		{name: "the default payload limit refuses an oversized successor", call: publishAndClose, assert: refused},
   		{
   			name: "a raised payload limit accepts a successor and returns it byte for byte",
   			opts: raised,
   			call: publishAndClose,
   			assert: func(t *testing.T, _ *ntfy.Service, result any, err error) {
   				require.NoError(t, err)
   				successors := result.(ntfy.CloseResult).Successors
   				require.Len(t, successors, 1)
   				assert.Equal(t, []byte(payload), []byte(successors[0].Data))
   			},
   		},
   	}

   	for _, tc := range cases {
   		t.Run(tc.name, func(t *testing.T) {
   			t.Parallel()

   			svc, err := ntfy.New(ntfy.NewMemoryStore(), tc.opts...)
   			require.NoError(t, err)

   			result, err := tc.call(t.Context(), svc)
   			tc.assert(t, svc, result, err)
   		})
   	}
   }
   ```
   `TestServicePublishHonoursConfiguredLimits` also has two draft-cap subtests. They become two more rows, with these helpers declared beside `publish`:
   ```go
   	// drafts builds n valid drafts for alice, each with its own source.
   	drafts := func(n int) []ntfy.Draft {
   		out := make([]ntfy.Draft, 0, n)
   		for i := range n {
   			out = append(out, ntfy.Draft{
   				Recipient: "alice", SourceID: "event-" + strconv.Itoa(i), Subject: "task-1", Kind: "offer",
   			})
   		}

   		return out
   	}

   	publishN := func(n int) func(ctx context.Context, svc *ntfy.Service) (any, error) {
   		return func(ctx context.Context, svc *ntfy.Service) (any, error) { return svc.Publish(ctx, drafts(n)...) }
   	}
   ```
   and these rows:
   ```go
   		{
   			name:   "a lowered draft cap refuses a publish the default would accept",
   			opts:   []ntfy.Option{ntfy.WithLimits(ntfy.Limits{MaxDraftsPerPublish: ntfy.Limit(1)})},
   			call:   publishN(2),
   			assert: refused,
   		},
   		{
   			name: "a raised draft cap accepts a publish the default would refuse",
   			opts: []ntfy.Option{ntfy.WithLimits(ntfy.Limits{MaxDraftsPerPublish: ntfy.Limit(ntfy.DefaultMaxDraftsPerPublish + 1)})},
   			call: publishN(ntfy.DefaultMaxDraftsPerPublish + 1),
   			assert: func(t *testing.T, _ *ntfy.Service, result any, err error) {
   				require.NoError(t, err)
   				assert.Len(t, result.(ntfy.PublishResult).Created, ntfy.DefaultMaxDraftsPerPublish+1)
   			},
   		},
   ```
   The old raised-cap subtest also asserted that the default cap refuses those drafts. `TestServicePublish`'s "too many drafts" row (item 4) already covers that. Copy any further assertion in the old subtest's tail into this row's `assert`. Then delete both old functions.
6. **`TestHandlerStreamEndsWhenTheHubStops`** (`http_test.go:875`) becomes a `TestHandlerStream` row with `run: false`. Its assert starts with `stop := runHub(t, env.hub)`, and then keeps the old body from `s := openStream(...)` on.
7. **`TestHandlerStreamCarriesAReconnectDelay`** (`http_test.go:906`) becomes a `TestHandlerStream` row with `run: true`.
8. **`TestHubStopReleasesSubscriptions`** (`hub_test.go:591`) becomes a row of `TestHubStopClosesOpenSubscriptions`, which gains `opts []ntfy.HubOption`, passed to `ntfy.NewHub`. The row sets `opts: []ntfy.HubOption{ntfy.WithMaxStreamsPerRecipient(2)}`.
9. **`TestHubCapsStreamsPerInstance`** (`hub_test.go:901`) becomes the `TestHubInstanceCapConfiguration` row "the default cap refuses the stream after 10,000", with `opts: nil`.
10. **`sqlstore/verify_test.go:171-193`** become the table below. `sqlstore/email_verify_test.go:144-166` become the same shape, named `TestVerifyEmailSchema`, calling `runVerifyEmailSchema`.
    ```go
    // TestVerifySchema runs the verification cases on each dialect. The dialect
    // decides only how the database is opened, so the rows carry no assert of
    // their own: runVerifySchema's table holds the assertions.
    func TestVerifySchema(t *testing.T) {
    	t.Parallel()

    	type testCase struct {
    		name string
    		open func(t *testing.T) sqlkit.Executor
    	}

    	cases := []testCase{
    		{name: "PostgreSQL", open: func(t *testing.T) sqlkit.Executor {
    			return stdsqlExecutor(t, openSQL(t, "postgres", sqlkittest.RunTestPostgres(t)), sqlkit.PostgreSQL)
    		}},
    		{name: "MySQL", open: func(t *testing.T) sqlkit.Executor {
    			return stdsqlExecutor(t, openSQL(t, "mysql", sqlkittest.RunTestMySQL(t)), sqlkit.MySQL)
    		}},
    		{name: "SQLite", open: func(t *testing.T) sqlkit.Executor {
    			return stdsqlExecutor(t, openSQL(t, "sqlite", sqlkittest.RunTestSQLite(t)), sqlkit.SQLite)
    		}},
    	}

    	for _, tc := range cases {
    		t.Run(tc.name, func(t *testing.T) {
    			t.Parallel()

    			runVerifySchema(t, tc.open(t))
    		})
    	}
    }
    ```
    These rows have no `assert`, because the dialect is the table's *setup* dimension, and the asserting table is `runVerifySchema`'s own. The skill allows this under "structurally different setup". The comment above the function states it, as the skill requires.

Run: the count command above.

Expected: the same number as before, or more (the split "get needs …" rows add one).

Run: `make test && (cd sqlstore && go test -run 'TestVerify(Email)?Schema$' -count=1 .)`

Expected: `ok`.

Run: `grep -rn 'TestVerify\(Email\)\?SchemaOn\|HonoursConfiguredLimits(t\|RefusesTooManyDrafts\|ReleasesSubscriptions\|CapsStreamsPerInstance\|EndsWhenTheHubStops\|CarriesAReconnectDelay' --include='*.go' . Makefile .github`

Expected: only the new `TestServiceHonoursConfiguredLimits`.

- [ ] **Step 14: Commit the folds**

```bash
git add notification_test.go service_publish_test.go service_ops_test.go http_test.go hub_test.go sqlstore/verify_test.go sqlstore/email_verify_test.go
git commit -m "Fold same-SUT tests into their tables"
```

---

### Task 4: Godoc names its defaults

**Files:**
- Modify: `store.go:19-21`, `clock.go:5-6`, `email.go:202,240,276,290-291`, `email_dispatcher.go:154-156`, `websocket/handler.go:103-105`.

**Interfaces:**
- Consumes: nothing.
- Produces: nothing. This is comments only.

- [ ] **Step 1: Write the comments**

`store.go:19-21`:
```go
// Store is where notifications live. It is required: [New] has no default
// store and refuses a nil one with a [ConfigurationError]. [NewMemoryStore]
// serves a single process; ntfy/sqlstore stores notifications in PostgreSQL,
// MySQL or SQLite; a host may supply its own, provided it passes the ntfytest
// conformance suite.
```

`clock.go:5-6`:
```go
// Clock is the library's source of time. Its only method is Now, so a host's
// existing clock abstraction usually satisfies it unchanged. When none is
// supplied the library uses [SystemClock]; [WithClock] replaces it.
```

`email.go:202`:
```go
// AddressBook finds a recipient's email address. It belongs to the host and is
// required: [NewEmailDispatcher] has no default and refuses a nil one with a
// [ConfigurationError].
```

`email.go:240`:
```go
// EmailTemplate renders a batch into a message. It belongs to the host and is
// required: [NewEmailDispatcher] has no default and refuses a nil one with a
// [ConfigurationError].
```

`email.go:276`:
```go
// Mailer sends messages. It belongs to the host and is required:
// [NewEmailDispatcher] has no default and refuses a nil one with a
// [ConfigurationError].
```

`email.go:290-291`:
```go
// EmailFilter decides which notifications are emailed. It is where a host's
// preferences, quiet hours and unsubscribes plug in. When none is supplied the
// dispatcher emails every kind; [WithEmailFilter] or [WithEmailKinds] replaces
// that.
```

`email_dispatcher.go:154-156`:
```go
// WithEmailKinds replaces the default, which emails every kind, with emailing
// notifications of the named kinds only, through [EmailKinds]. It needs at
// least one kind, and cannot be combined with [WithEmailFilter].
```

`websocket/handler.go:103-105`:
```go
// WithAnyOrigin replaces the default, which accepts only browser origins on the
// request's own host (widened by [WithOriginPatterns]), and permits every
// browser origin. It is the explicit opt-out from origin checking, for a host
// that checks origins somewhere else, and it is named so that accepting
// cross-site connections is never an accident.
```

- [ ] **Step 2: Verify**

Run:
```bash
go doc github.com/kartaladev/ntfy Store | grep -c 'It is required'
go doc github.com/kartaladev/ntfy WithEmailKinds | grep -c 'replaces the default'
(cd websocket && go doc . WithAnyOrigin | grep -c 'replaces the default')
make lint && go test -run 'Document' -count=1 ./...
```

Expected: `1`, `1`, `1`, then lint and tests pass.

- [ ] **Step 3: Commit**

```bash
git add store.go clock.go email.go email_dispatcher.go websocket/handler.go
git commit -m "Name every default, and every required port, in godoc"
```

---

### Task 5: Linters

**Files:**
- Modify: `.golangci.yml`.
- Modify, with `nolint`s or fixes: `email_dispatcher.go:487,678`, `hub.go:341`, `id.go:57,59`, `http.go:375`, `http_test.go:546`, `memory_bench_test.go:155,228,360,365`, `hub_test.go:739`, `email_dispatch_test.go:86`.
- Modify: `ntfytest/identity_test.go:116,138`, `ntfytest/email_dispatch.go:347`.
- Modify: `sqlstore/sql.go:81`, `sqlstore/email_verify_test.go:275,322,334,345,383`, `sqlstore/email_claim_measure_test.go:496`, `sqlstore/harness_test.go:80-81,92-93,104-105,117-118`.
- Modify: `nats/reconnect_test.go:34,54-65`.

Line numbers are from `20967b3`. After Tasks 2 and 3, re-locate each one with `make lint`.

**Interfaces:**
- Consumes: nothing.
- Produces: `func (p *proxy) accept(ctx context.Context)` in `nats/reconnect_test.go`, which replaces `accept()`.

- [ ] **Step 1: Enable the linters (red)**

In `.golangci.yml`:
- add to `linters.enable`, keeping the list alphabetical: `containedctx`, `gosec`, `modernize`, `noctx`, `nolintlint`, `paralleltest`, `rowserrcheck`, `sqlclosecheck`;
- add under `linters.settings`:

```yaml
    modernize:
      disable:
        # Limit and timePtr are documented, readable helpers; new(expr) at every
        # call site trades a name for a builtin (design.md D6).
        - newexpr
    nolintlint:
      require-explanation: true
      require-specific: true
      allow-unused: false
```

Do **not** enable `thelper` (D6).

Run: `make lint 2>&1 | grep -oE '\((containedctx|gosec|modernize|noctx|nolintlint|paralleltest|sqlclosecheck)\)$' | sort | uniq -c`

Expected, on `20967b3`, before Tasks 2 and 3:
```
   2 (containedctx)
   9 (gosec)
   3 (modernize)
   2 (noctx)
   1 (nolintlint)
  14 (paralleltest)
   2 (sqlclosecheck)
```

- [ ] **Step 2: Apply the mechanical fixes**

Run: `for m in . ntfytest; do (cd $m && golangci-lint run --fix --enable-only modernize ./...); done`

Expected:
- `email_dispatch_test.go:86` and `ntfytest/email_dispatch.go:347` use `slices.Backward`;
- `hub_test.go:739` uses `wg.Go(...)`.

In `http_test.go:546`, change `for range s.lines { //nolint:revive // drain until the reader exits` to:

```go
		// Drain until the reader exits.
		for range s.lines {
```

In `nats/reconnect_test.go`:
- replace `listener, err := net.Listen("tcp", "127.0.0.1:0")` with `listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")`;
- replace `go p.accept()` with `go p.accept(t.Context())`;
- change the method to:

```go
// accept forwards every accepted connection until the listener closes.
func (p *proxy) accept(ctx context.Context) {
	defer p.wg.Done()

	var dialer net.Dialer

	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}

		server, err := dialer.DialContext(ctx, "tcp", p.target)
		if err != nil {
			_ = client.Close()

			continue
		}
```

Keep the rest of the loop unchanged, and add `"context"` to the imports.

- [ ] **Step 3: The justified suppressions**

Add each directive at the end of the flagged line, or on the line above a flagged declaration:

| Site | Directive |
| --- | --- |
| `email_dispatcher.go:678` | `//nolint:gosec // G404: retry jitter only spreads load; predictability does no harm` |
| `hub.go:341` | `//nolint:gosec // G404: reconnect jitter only spreads load; predictability does no harm` |
| `id.go:57` | `//nolint:gosec // G115: lastMS starts at 0 and only grows (tick), so it is never negative` |
| `id.go:59` | `//nolint:gosec // G115: counter>>8&0x0F is at most 15` |
| `http.go:375` | `//nolint:gosec // G705: the body is text/event-stream (set above, asserted in TestHandlerStream), never rendered as HTML` |
| `email_dispatcher.go:487` | `//nolint:containedctx // an emailPass lives for one Dispatch call and never escapes it` |
| `sqlstore/sql.go:81` | `//nolint:containedctx // ownContext is itself a context.Context, wrapping its parent` |
| `ntfytest/identity_test.go:138` | `//nolint:gosec // G204: the command is this test binary, os.Args[0], with fixed flags` |
| `ntfytest/identity_test.go:116` | `//nolint:paralleltest // a child process's entry point; it runs alone by construction` |
| `sqlstore/email_verify_test.go:322,334,345` | `//nolint:gosec // G202: prefix is the test's own table prefix, never input` |
| `sqlstore/email_verify_test.go:275,383` | `//nolint:sqlclosecheck // closed explicitly so the test can assert Close's error` |
| `memory_bench_test.go:155,360` | `//nolint:paralleltest // a measurement gate runs alone; see the comment above` |
| `memory_bench_test.go:228,365` | `//nolint:paralleltest // the operations share the store's mutex; see the comment above` |
| `sqlstore/email_claim_measure_test.go:496` | `//nolint:paralleltest // a measurement runs alone, so that other tests do not load the database it times` |
| `sqlstore/harness_test.go:80-81,92-93,104-105,117-118` | `//nolint:paralleltest // every conformance case calls t.Parallel itself` |

- [ ] **Step 4: Green**

Run: `make lint`

Expected: every module prints `0 issues.`

Run: `make test && (cd nats && go test -run 'Reconnect' -count=3 -race ./...) && (cd sqlstore && NTFY_MEASURE_ROWS=20000 go test -run TestMeasureClaim -count=1 -timeout 30m .) && NTFY_MEASURE_MEMORY=1 go test -count=1 -run 'TestMemoryStore(ReadsDoNotScale|PruneCountWithinBound)' .`

Expected: `ok` for each. Both measurement gates hold their thresholds.

- [ ] **Step 5: Watch nolintlint reject an unexplained directive (red check)**

Temporarily change the `hub.go:341` directive to `//nolint:gosec`.

Run: `make lint`

Expected: `directive //nolint:gosec should provide explanation such as //nolint:gosec // this is why (nolintlint)`. Restore the directive.

- [ ] **Step 6: Commit**

```bash
git add .golangci.yml email_dispatcher.go hub.go id.go http.go http_test.go memory_bench_test.go hub_test.go email_dispatch_test.go ntfytest sqlstore nats/reconnect_test.go
git commit -m "Enable gosec, paralleltest, modernize and the SQL, context and nolint linters"
```

---

### Task 6: goleak in sqlstore

**Files:**
- Create: `sqlstore/main_test.go`, `sqlstore/internal/gormtest/main_test.go`.

**Interfaces:**
- Consumes: `go.uber.org/goleak` v1.3.0, already required by sqlstore after Task 1.
- Produces: `TestMain(m *testing.M)` in packages `sqlstore_test` and `gormtest_test`.

- [ ] **Step 1: Add the guard**

`sqlstore/main_test.go`:
```go
package sqlstore_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when any test leaves a goroutine running: every
// store, pool and container a test opens must be closed by its cleanup.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

`sqlstore/internal/gormtest/main_test.go` is the same, with `package gormtest_test`.

If `go.uber.org/goleak` is not yet a direct requirement of `sqlstore/go.mod`, run `make tidy`.

- [ ] **Step 2: Plant a leak (red)**

Add `sqlstore/leak_canary_test.go`:
```go
package sqlstore_test

import "testing"

func TestLeakCanary(t *testing.T) {
	go func() { select {} }()
}
```

Run: `cd sqlstore && go test -run 'TestLeakCanary' -count=1 .`

Expected: FAIL with `found unexpected goroutines`, and a stack naming `sqlstore_test.TestLeakCanary.func1`.

Delete `sqlstore/leak_canary_test.go`.

- [ ] **Step 3: Green**

Run: `cd sqlstore && go test -count=1 -timeout 30m ./...`, with Docker.

Expected: `ok` for `sqlstore`, `sqlstore/internal/gormtest` and the others. The 2026-09-28 trial passed in 25 s and 20 s respectively.

If CI later reports a goroutine that the local run did not, add `goleak.IgnoreTopFunction("<that exact function>")` to `VerifyTestMain`, with a comment naming the library that owns it. Never use `IgnoreCurrent`.

- [ ] **Step 4: Commit**

```bash
git add sqlstore/main_test.go sqlstore/internal/gormtest/main_test.go sqlstore/go.mod sqlstore/go.sum
git commit -m "Fail sqlstore's tests on a leaked goroutine"
```

---

### Task 7: Examples and repository documents

**Files:**
- Modify: `example_test.go`.
- Create: `sqlstore/example_test.go`, `redis/example_test.go`, `nats/example_test.go`, `ntfytest/example_test.go`, `CHANGELOG.md`, `CONTRIBUTING.md`.
- Create after the maintainer answers: `LICENSE`. Modify then: `README.md`.

**Interfaces:**
- Consumes these existing functions:
  - `ntfy.New`, `ntfy.NewMemoryStore`, `ntfy.NewHub`, `ntfy.WithBroadcaster`;
  - `sqlstore.New(executor sqlkit.Executor, opts ...Option) (*Store, error)`, `(*Store).Migrate(ctx) error`, `(*Store).VerifySchema(ctx) error`;
  - `stdsqlexec.New(db *sql.DB, dialect sqlkit.Dialect)`;
  - `redis.NewBroadcaster(client goredis.UniversalClient, opts ...Option)`, `redis.WithChannel`;
  - `nats.NewBroadcaster(conn *natsgo.Conn, opts ...Option)`, `nats.WithSubject`;
  - `ntfytest.Run(t *testing.T, factory Factory)`.
- Produces: `ExampleNew` (ntfy and sqlstore), `ExampleNewBroadcaster` (redis and nats), `ExampleRun` (ntfytest).

- [ ] **Step 1: The runnable examples (red, then green)**

Append to `example_test.go`:
```go
// ExampleNew builds a service over the memory store, publishes a notification
// and counts the recipient's unread ones.
func ExampleNew() {
	svc, err := ntfy.New(ntfy.NewMemoryStore())
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	published, err := svc.Publish(ctx, ntfy.Draft{
		Recipient: "alice", SourceID: "order-42-shipped", Subject: "order-42", Kind: "shipped",
		Title: "Your order has shipped",
	})
	if err != nil {
		panic(err)
	}

	count, err := svc.CountActive(ctx, "alice")
	if err != nil {
		panic(err)
	}

	fmt.Println(len(published.Created), count)
	// Output: 1 1
}
```

Create `sqlstore/example_test.go`:
```go
package sqlstore_test

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/sqlstore"
	"github.com/kartaladev/sqlkit"
	stdsqlexec "github.com/kartaladev/sqlkit/stdsql"
)

// ExampleNew stores notifications in SQLite through database/sql. A host
// applies Store.Schema through its own migrations and calls VerifySchema at
// startup; Migrate exists for tests and development.
func ExampleNew() {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// One connection: every connection to :memory: is a database of its own.
	db.SetMaxOpenConns(1)

	executor, err := stdsqlexec.New(db, sqlkit.SQLite)
	if err != nil {
		panic(err)
	}

	store, err := sqlstore.New(executor)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	if err := store.Migrate(ctx); err != nil {
		panic(err)
	}

	if err := store.VerifySchema(ctx); err != nil {
		panic(err)
	}

	svc, err := ntfy.New(store)
	if err != nil {
		panic(err)
	}

	if _, err := svc.Publish(ctx, ntfy.Draft{
		Recipient: "alice", SourceID: "order-42-shipped", Subject: "order-42", Kind: "shipped",
	}); err != nil {
		panic(err)
	}

	count, err := svc.CountActive(ctx, "alice")
	if err != nil {
		panic(err)
	}

	fmt.Println(count)
	// Output: 1
}
```

Red check: temporarily change each `// Output:` value (`1 1` to `1 2`, and `1` to `2`), and run `go test -run 'ExampleNew$' -count=1 .` and `cd sqlstore && go test -run 'ExampleNew$' -count=1 .`. Expected: FAIL with `got: 1 1 want: 1 2` and `got: 1 want: 2`. Restore both.

Run the same two commands again. Expected: `ok` for both.

- [ ] **Step 2: The compile-only examples**

`redis/example_test.go`:
```go
package redis_test

import (
	"context"

	goredis "github.com/redis/go-redis/v9"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/redis"
)

// ExampleNewBroadcaster carries signals between instances through Redis
// pub/sub. Every instance of one deployment names the same channel. It needs a
// Redis server, so it has no Output and is compiled, not run.
func ExampleNewBroadcaster() {
	client := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
	defer client.Close()

	broadcaster, err := redis.NewBroadcaster(client, redis.WithChannel("myapp.ntfy.signals"))
	if err != nil {
		panic(err)
	}

	svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithBroadcaster(broadcaster))
	if err != nil {
		panic(err)
	}

	hub, err := ntfy.NewHub(svc.Broadcaster())
	if err != nil {
		panic(err)
	}

	// Run returns when its context ends; a host ties it to the process's life.
	_ = hub.Run(context.Background())
}
```

`nats/example_test.go`:
```go
package nats_test

import (
	"context"

	natsgo "github.com/nats-io/nats.go"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/nats"
)

// ExampleNewBroadcaster carries signals between instances through a NATS
// subject. Every instance of one deployment names the same subject. It needs a
// NATS server, so it has no Output and is compiled, not run.
func ExampleNewBroadcaster() {
	conn, err := natsgo.Connect(natsgo.DefaultURL)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	broadcaster, err := nats.NewBroadcaster(conn, nats.WithSubject("myapp.ntfy.signals"))
	if err != nil {
		panic(err)
	}

	svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithBroadcaster(broadcaster))
	if err != nil {
		panic(err)
	}

	hub, err := ntfy.NewHub(svc.Broadcaster())
	if err != nil {
		panic(err)
	}

	_ = hub.Run(context.Background())
}
```

`ntfytest/example_test.go`:
```go
package ntfytest_test

import (
	"testing"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

// ExampleRun holds a host's own store to the conformance suite. In the store's
// package, the test is:
//
//	func TestMyStoreConformance(t *testing.T) {
//		ntfytest.Run(t, func(t *testing.T) ntfy.Store { return newMyStore(t) })
//	}
//
// The factory is called once per case and returns a fresh, empty store. The
// suite needs a *testing.T, so this example is compiled, not run.
func ExampleRun() {
	conformance := func(t *testing.T) {
		ntfytest.Run(t, func(*testing.T) ntfy.Store { return ntfy.NewMemoryStore() })
	}

	_ = conformance
}
```

Run: `for m in redis nats ntfytest; do (cd $m && go vet ./... && go test -run 'Example' -count=1 ./...); done`

Expected: no vet output, and `ok` for each module. The examples with no Output are compiled, not run.

- [ ] **Step 3: `CHANGELOG.md` and `CONTRIBUTING.md`**

`CHANGELOG.md`:
```markdown
# Changelog

All notable changes to this project are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/) once the first tag is cut.

## [Unreleased]

### Added

- The notification inbox: publish, close with successors, list, count, mark
  read, over a `Store` port with an in-memory default (`notify-core`).
- Retention and pruning (`notify-core`).
- The HTTP API and server-sent event streams, and the realtime hub
  (`notify-core`, `notify-listen-readiness`).
- Email delivery through a host's `Mailer`, `AddressBook` and `EmailTemplate`
  (`notify-email`).
- `ntfy/sqlstore` on PostgreSQL, MySQL and SQLite through sqlkit, and the
  `ntfytest` conformance suite.
- `ntfy/websocket`, `ntfy/redis` and `ntfy/nats` (`notify-realtime-adapters`).

### Changed

- Publish and query inputs are bounded (`bound-publish-and-query-inputs`).
- Email delivery retries are bounded, and record the library's classification
  of a failure (`email-delivery-robustness`).
- The email claim query scales with the due window, not the table
  (`scale-email-claim-query`).
- The memory store reads through a recipient index
  (`speed-up-memory-store-reads`).
- MySQL identifier columns are `VARBINARY`, and compare byte for byte. **Breaking**
  for existing MySQL schemas; see `docs/schema.md`
  (`compare-mysql-identifiers-by-bytes`).

### Fixed

- A WebSocket connection following another recipient can no longer mark that
  recipient's notifications (`fix-websocket-write-authorization`).
- Streams are bounded per recipient and per instance, and end when the hub stops
  (`harden-hub-streams`).
- Stores no longer alias a caller's links or data (`stop-store-aliasing-caller-data`).

Each name in parentheses is an archived change under `openspec/changes/archive/`.
```

`CONTRIBUTING.md`:
```markdown
# Contributing

## Before you start

- Go 1.26.8. The `Makefile` and `.envrc` pin `GOTOOLCHAIN=go1.26.8`.
- golangci-lint v2.13.2, `mockgen` v0.6.0 or later, and `openspec`.
- Docker, for the PostgreSQL, MySQL, Redis and NATS tests.

## How work is done here

Every change is an OpenSpec change under `openspec/changes/`. Its proposal,
design, spec delta and tasks say what is built, and its `plans.md` says how.
`.claude/rules/development-workflow.md` gives the loop. The other rules in
`.claude/rules/` apply to every change:

- test first, red then green (`golang-tdd.md`);
- a claimed defect is proved by a failing test (`prove-errors-with-tests.md`);
- a performance claim is proved by a benchmark and a failing threshold
  (`performance-benchmark.md`);
- every default is documented and replaceable (`library-design.md`).

Tests follow the `table-test`, `use-mockgen` and `use-testcontainers` skills
under `.claude/skills/`.

## Checks

    make all            # lint, split-check, tidy-check, test
    make store-matrix   # the store conformance suite on every driver and dialect
    make tidy           # tidy every module, leaving the go.work siblings out

`make all` must pass before a pull request, and `make store-matrix` must pass
when a store changes.

## Fuzzing

`go test` runs the fuzz targets' seeds only. To fuzz one:

    go test -run '^$' -fuzz '^FuzzDecodeCursor$' -fuzztime 60s .

A crasher is saved under `testdata/fuzz/` and becomes a seed. Commit it with the
fix.
```

Run: `test -f CHANGELOG.md && test -f CONTRIBUTING.md && make all`

Expected: `make all` passes.

- [ ] **Step 4: Commit**

```bash
git add example_test.go sqlstore/example_test.go redis/example_test.go nats/example_test.go ntfytest/example_test.go CHANGELOG.md CONTRIBUTING.md
git commit -m "Add examples for every constructor, a changelog and contributing notes"
```

- [ ] **Step 5: Ask for the licence (blocking)**

Ask the maintainer, and only the maintainer:

> "Which licence should github.com/kartaladev/ntfy be published under? For example MIT, Apache-2.0 or BSD-3-Clause. I will add its standard text as LICENSE and name it in the README. I will not choose one."

Stop here until they answer. Tasks 8 to 10 do not depend on the answer, and can proceed.

- [ ] **Step 6: Once answered, add the licence**

- Write the licence's standard text, from the SPDX licence list, to `LICENSE`, with the copyright line the maintainer gives.
- Append this to `README.md`, substituting the SPDX identifier they chose:

```markdown
## Licence

Released under the <SPDX identifier> licence; see [LICENSE](LICENSE).
```

Run: `head -1 LICENSE`

Expected: the chosen licence's title line.

```bash
git add LICENSE README.md
git commit -m "Add the licence"
```

---

### Task 8: Fuzz tests

**Files:**
- Create: `cursor_fuzz_test.go`, `http_fuzz_test.go`, `codec_fuzz_test.go`, `notification_fuzz_test.go`.

**Interfaces:**
- Consumes these existing functions:
  - `ntfy.EncodeCursor(q ListQuery, p CursorPosition) string`, `ntfy.DecodeCursor(q ListQuery)`;
  - `ntfy.EncodeSignals([]Signal) ([]byte, error)`, `ntfy.DecodeSignals([]byte) ([]Signal, error)`;
  - `(ntfy.Draft).Validate() error`;
  - `headerActor` and `actorHeader`, from `http_test.go`.
- Produces: `func isValidation(err error) bool`, in `cursor_fuzz_test.go`, for the other fuzz files.

- [ ] **Step 1: Land the audit's targets**

`cursor_fuzz_test.go`:
```go
package ntfy_test

import (
	"errors"
	"testing"

	"github.com/kartaladev/ntfy"
)

// isValidation reports whether err is a validation error.
func isValidation(err error) bool {
	var v *ntfy.ValidationError

	return errors.As(err, &v)
}

// FuzzDecodeCursor: no cursor a client sends panics the decoder, and every
// refusal is a validation error.
func FuzzDecodeCursor(f *testing.F) {
	f.Add(ntfy.EncodeCursor(ntfy.ListQuery{Recipient: "alice"}, ntfy.CursorPosition{ID: "x"}))
	f.Add("not-a-cursor")
	f.Add("eyJ0IjoiIiwiaSI6IiIsImYiOiIifQ")

	f.Fuzz(func(t *testing.T, cursor string) {
		q := ntfy.ListQuery{Recipient: "alice", Cursor: cursor}
		if _, _, err := ntfy.DecodeCursor(q); err != nil && !isValidation(err) {
			t.Fatalf("a malformed cursor was not a validation error: %v", err)
		}
	})
}
```

`http_fuzz_test.go`:
```go
package ntfy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kartaladev/ntfy"
)

// FuzzHandlerRequests: no query string or path identifier panics the handler
// or makes it answer 5xx.
func FuzzHandlerRequests(f *testing.F) {
	f.Add("limit=10&state=ACTIVE&kind=a&cursor=zz", "id")
	f.Add("limit=-1&state=%zz", "%2F..")
	f.Add("limit=99999999999999999999;x", "")

	svc, err := ntfy.New(ntfy.NewMemoryStore())
	if err != nil {
		f.Fatal(err)
	}

	hub, err := ntfy.NewHub(svc.Broadcaster())
	if err != nil {
		f.Fatal(err)
	}

	handler, err := ntfy.NewHandler(svc, hub, ntfy.WithActor(headerActor))
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, rawQuery, id string) {
		requests := []*http.Request{
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody),
			httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody),
		}
		requests[0].URL.Path = "/v1/notifications"
		requests[1].URL.Path = "/v1/notifications/" + id + "/read"

		for _, req := range requests {
			req.URL.RawQuery = rawQuery
			req.Header.Set(actorHeader, "alice")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code >= http.StatusInternalServerError {
				t.Fatalf("%s %s?%s answered %d: %s", req.Method, req.URL.Path, rawQuery, rec.Code, rec.Body.String())
			}
		}
	})
}
```

Run: `go test -run 'FuzzDecodeCursor|FuzzHandlerRequests' -count=1 . && go test -run '^$' -fuzz '^FuzzDecodeCursor$' -fuzztime 30s . && go test -run '^$' -fuzz '^FuzzHandlerRequests$' -fuzztime 30s .`

Expected: `ok`, then two fuzzing runs ending in `PASS`.

- [ ] **Step 2: Watch the cursor target notice a wrong error (red check)**

Temporarily add `if q.Cursor == "not-a-cursor" { return CursorPosition{}, false, errors.New("x") }` as the first line of `DecodeCursor(q ListQuery) (CursorPosition, bool, error)`, at `store.go:386`.

Run: `go test -run 'FuzzDecodeCursor' -count=1 .`

Expected: FAIL, `a malformed cursor was not a validation error: x`. Revert `store.go`, and check `git diff --quiet -- store.go`.

- [ ] **Step 3: The two new targets**

`codec_fuzz_test.go`:
```go
package ntfy_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/kartaladev/ntfy"
)

// FuzzDecodeSignals: no bytes a broker delivers panic the decoder, and what it
// decodes survives a second encode and decode unchanged.
func FuzzDecodeSignals(f *testing.F) {
	seed, err := ntfy.EncodeSignals([]ntfy.Signal{{
		Recipient: "alice", Change: ntfy.ChangeCreated, At: time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		f.Fatal(err)
	}

	f.Add(seed)
	f.Add([]byte("[]"))
	f.Add([]byte("{"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := ntfy.DecodeSignals(data)
		if err != nil {
			return
		}

		once, err := ntfy.EncodeSignals(decoded)
		if err != nil {
			t.Fatalf("decoded signals do not encode: %v", err)
		}

		again, err := ntfy.DecodeSignals(once)
		if err != nil {
			t.Fatalf("encoded signals do not decode: %v", err)
		}

		twice, err := ntfy.EncodeSignals(again)
		if err != nil {
			t.Fatalf("re-decoded signals do not encode: %v", err)
		}

		if !bytes.Equal(once, twice) {
			t.Fatalf("a round trip changed the signals:\n%s\n%s", once, twice)
		}
	})
}
```

`notification_fuzz_test.go`:
```go
package ntfy_test

import (
	"encoding/json"
	"testing"

	"github.com/kartaladev/ntfy"
)

// FuzzDraftValidate: no draft panics validation, and every refusal is a
// validation error.
func FuzzDraftValidate(f *testing.F) {
	f.Add("alice", "event-1", "task-1", "offer", "A title", "https://x.test/a", []byte(`{"k":1}`))
	f.Add("", "", "", "", "", "javascript:alert(1)", []byte("{"))
	f.Add("alice\x00", "event-1​", "task-1", "offer", "", "", []byte(nil))

	f.Fuzz(func(t *testing.T, recipient, source, subject, kind, title, href string, data []byte) {
		draft := ntfy.Draft{
			Recipient: recipient, SourceID: source, Subject: subject, Kind: kind, Title: title,
			Links: map[string]string{"link": href}, Data: json.RawMessage(data),
		}

		if err := draft.Validate(); err != nil && !isValidation(err) {
			t.Fatalf("a refusal was not a validation error: %v", err)
		}
	})
}
```

Run: `go test -run 'FuzzDecodeSignals|FuzzDraftValidate' -count=1 . && go test -run '^$' -fuzz '^FuzzDecodeSignals$' -fuzztime 30s . && go test -run '^$' -fuzz '^FuzzDraftValidate$' -fuzztime 30s .`

Expected: `ok`, then two fuzzing runs ending in `PASS`.

- [ ] **Step 4: Red checks**

1. Temporarily make `Draft.Validate` return `errors.New("x")` when `d.Recipient == ""`. Then `go test -run 'FuzzDraftValidate' -count=1 .` fails with `a refusal was not a validation error: x`. Revert.
2. Temporarily make `DecodeSignals` append `"x"` to every decoded signal's `Recipient` before returning. Then `go test -run 'FuzzDecodeSignals' -count=1 .` fails on the seed with `a round trip changed the signals`, because `alicex` becomes `alicexx`. Revert.

Run: `git diff --quiet -- notification.go codec.go && echo CLEAN`

Expected: `CLEAN`.

- [ ] **Step 5: Commit**

```bash
git add cursor_fuzz_test.go http_fuzz_test.go codec_fuzz_test.go notification_fuzz_test.go
git commit -m "Fuzz the cursor, the HTTP handler, the signal codec and draft validation"
```

---

### Task 9: Process rules and templates

**Files:**
- Modify: `openspec/config.yaml` (append after the comment block), `.claude/rules/plans-beside-tasks.md`, in the section "What the plan must contain".

**Interfaces:**
- Consumes: nothing.
- Produces: `rules.proposal`, `rules.design` and `rules.tasks` in `openspec/config.yaml`.

- [ ] **Step 1: Add the artifact rules**

Append to `openspec/config.yaml`:

```yaml
rules:
  proposal:
    - Every claim that existing code is wrong carries a **Proved by:** line naming the failing test and quoting its failing output, or an **Unverified:** line saying what stops it being tested (.claude/rules/prove-errors-with-tests.md).
    - Every claim that code is slow, allocates too much or does not scale names its benchmark and a threshold stated before measuring, or is labelled **Unverified** (.claude/rules/performance-benchmark.md).
  design:
    - Every decision that introduces or changes behaviour ends with a **Default:** line and an **Override:** line, or says why there is no override (.claude/rules/library-design.md).
  tasks:
    - A change that fixes a defect opens with the task that lands the failing test and watches it fail for the stated reason.
    - Writing or changing tasks.md means writing or updating plans.md beside it in the same turn (.claude/rules/plans-beside-tasks.md).
```

Run: `openspec instructions design --change conform-to-project-rules --json | grep -c 'Default:'`

Expected: at least `1`.

Run: `openspec validate --all --strict`

Expected: every item valid.

- [ ] **Step 2: Tighten the plan rule**

In `.claude/rules/plans-beside-tasks.md`, replace the Global Constraints bullet under "What the plan must contain" with the two bullets below:

```markdown
- a **Global Constraints** section carrying this project's rules with exact values, one line
  each: the Go toolchain pin, the module import limits, `make all` (and `make store-matrix`
  where a store changes), the `table-test`, `use-mockgen` and `use-testcontainers` skills,
  and **every** rule in this directory by file name — `golang-tdd.md`,
  `prove-errors-with-tests.md`, `performance-benchmark.md`, `library-design.md`,
  `plans-beside-tasks.md` — saying how each applies, or that it does not and why;
- a **Mapping to `tasks.md`** table with one row per plan task, naming the `tasks.md` items
  it covers, so that every item appears at least once;
```

Run: `grep -n 'Mapping to' .claude/rules/plans-beside-tasks.md`

Expected: one line.

- [ ] **Step 3: Commit**

```bash
git add openspec/config.yaml .claude/rules/plans-beside-tasks.md
git commit -m "Hold every proposal, design and plan to the rules where they are written"
```

---

### Task 10: Verify and hand off

**Files:** none new.

**Interfaces:** none.

- [ ] **Step 1: Full verification**

Run: `make all && make store-matrix`

Expected: both pass, and lint reports `0 issues.` in every module.

Run: `make tidy && for m in . ntfytest sqlstore websocket redis nats; do (cd $m && go generate ./...); done && git status --short`

Expected: no output.

Run: `openspec validate conform-to-project-rules --strict`

Expected: `Change 'conform-to-project-rules' is valid`.

- [ ] **Step 2: Review**

Run `/code-review` on the branch. Answer each finding per `superpowers:receiving-code-review`. A finding claimed as a defect is proved by a failing test first. Mirror any change to `tasks.md` in this plan in the same turn.

- [ ] **Step 3: Tick `tasks.md`**

Mark every completed item `- [x]` in `tasks.md`. Item 7.4 stays open until the maintainer names the licence.

```bash
git add openspec/changes/conform-to-project-rules/tasks.md
git commit -m "Record conform-to-project-rules as applied"
```
