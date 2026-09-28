# Refuse Wiring Mistakes at Construction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every configuration that the 2026-09-28 audit showed cannot work must fail in its constructor with a configuration error, instead of at first use or never. The four cases are:
- a table prefix too long for PostgreSQL;
- a base path without a leading slash, or with braces;
- an origin pattern that cannot match a host, or patterns alongside any-origin;
- a reconnect delay that overflows.

Every option given nil is refused the same way, in every module.

**Architecture:** Each check moves to the constructor that already owns the option. The HTTP base path rule lives in one exported function, `ntfy.CleanBasePath`, which both transports call. Bounds are exported constants: `ntfy.MaxReconnectDelay` and `sqlstore.MaxTablePrefixBytes`. Each sits at the line where the guarantee breaks, and a test ties it to that line. Options that can be given nil either assign unconditionally, so the constructor checks the field, or record a pointer, so an explicit nil is told apart from an absent option.

**Tech Stack:**
- Go 1.26; the core module uses the standard library only.
- `stretchr/testify`.
- `testcontainers-go` through `sqlkittest.RunTestPostgres` and `sqlkittest.RunTestMySQL`.
- `golangci-lint` v2 and `openspec`.

**Spec:** `openspec/changes/refuse-wiring-mistakes-at-construction/`. Read these first:
- `proposal.md`, for why, and the failing audit output;
- `design.md`: D1 prefix bound, D2 base path, D3 origin patterns, D4 reconnect ceiling, D5 nil options;
- `specs/notification-http-api/spec.md`, `specs/notification-realtime/spec.md` and `specs/ntfy-modules/spec.md`.

## Global Constraints

- **Pin Go 1.26: prefix every Go command with `GOTOOLCHAIN=go1.26.8`.** A newer Go (1.27.1) is first on `PATH`. The `make` targets already pin it.
- **Run each module's tests from the repository root with `go test -C <module>`:** `websocket`, `sqlstore`, `redis` or `nats`. The core module is `.`.
- **Imports:**
  - The core module imports only the standard library in production code. This plan adds `math` to `hub.go`.
  - Satellite modules may import only `ntfy`, `sqlkit` and their own client library.
  - `make split-check` is authoritative, and `.golangci.yml` depguard warns earlier.
- **`pkg/sqlkit` is not touched.** `make sqlkit-copy-check` must stay green.
- **Tests follow the `table-test` skill:**
  - an `assert` closure on every case, never `want`/`wantErr`;
  - `t.Context()`, not `context.Background()`, except in `t.Cleanup`, where `t.Context()` is already cancelled;
  - `require` only for preconditions.

  None of the constructors here is context-sensitive, so no table carries a `ctx` field. Each new table says so in a one-line comment.
- **Test doubles** come from the `use-mockgen` skill. This plan needs none, and `pruner_test.go` already uses its generated mocks through `mocked(t)`.
- **External services** come from the `use-testcontainers` skill. Call `sqlkittest.RunTestPostgres(t)` and `sqlkittest.RunTestMySQL(t)`, and never write a container helper.
- **`.claude/rules/prove-errors-with-tests.md`:** each red step is run, and its failing output compared with the expected output below, before any production edit. A red caused by compilation, a fixture or a container error does not count.
- **`.claude/rules/golang-tdd.md`:** red → green → refactor, and a test lands in the same commit as the code that satisfies it. Consider `/simplify` after each green.
- **`.claude/rules/library-design.md`:** each option's godoc names its default, its bound and how to keep the default (by omitting the option). `design.md` gives the default and override per decision.
- **`.claude/rules/performance-benchmark.md`:** this change makes no performance claim, so it needs no benchmark.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn.
- **`.claude/rules/gopls-navigation.md`:** find call sites with gopls, not grep. For example, `WithBasePath` callers before changing its semantics.
- **Done means `make all` and `make store-matrix` pass.** A store changes.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`. They end with the session's attribution lines.

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `hub.go` | `ntfy` | **Modify.** `MaxReconnectDelay`, the ceiling check in `NewHub`, and godoc. |
| `hub_test.go` | `ntfy` | **Modify.** Three cases in `TestHubReconnectDelay`, and the `math` import. |
| `http.go` | `ntfy` | **Modify.** `CleanBasePath`, `WithBasePath` recording the raw value, and `NewHandler` validating it. |
| `http_test.go` | `ntfy` | **Modify.** `TestCleanBasePath`, four `TestNewHandler` cases, and `TestHandlerServesUnderItsBasePath`. |
| `service.go` | `ntfy` | **Modify.** The four options assign unconditionally, `New` refuses nil fields, and godoc. |
| `service_test.go` | `ntfy` | **Modify.** Replace "nil options leave the defaults in place" with a skip case and four refusals. |
| `pruner.go` | `ntfy` | **Modify.** `WithPruneErrorHandler` assigns unconditionally, and `validate` refuses nil. |
| `pruner_test.go` | `ntfy` | **Modify.** One refusal case. |
| `websocket/handler.go` | `ntfy/websocket` | **Modify.** Base path through `ntfy.CleanBasePath`, `validateOriginPattern`, the any-origin conflict, and godoc. |
| `websocket/handler_test.go` | `ntfy/websocket` | **Modify.** Eleven `TestNewHandler` cases. |
| `sqlstore/store.go` | `ntfy/sqlstore` | **Modify.** `MaxTablePrefixBytes`, the length check, and godoc. |
| `sqlstore/store_test.go` | `ntfy/sqlstore` | **Modify.** Three `TestNew` cases. |
| `sqlstore/prefix_test.go` | `ntfy/sqlstore` | **Create.** `TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema` (containers) and `TestTablePrefixBoundFitsEveryOwnedName` (no database). |
| `redis/broadcaster.go`, `redis/broadcaster_test.go` | `ntfy/redis` | **Modify.** `onError` becomes a pointer, and nil is refused. |
| `nats/broadcaster.go`, `nats/broadcaster_test.go` | `ntfy/nats` | **Modify.** The same. |
| `docs/schema.md`, `docs/notifications.md`, `docs/realtime-operations.md` | — | **Modify.** The new lines. |

**Shared-file note:** `docs/notifications.md` is asserted by `docs_test.go` (`TestTheDocumentMatchesTheImplementation`), which looks for needles such as `WithBasePath` and `WithPruneErrorHandler`. Keep every needle.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: bound the reconnect delay | 1.1, 1.2 |
| Task 2: the HTTP base path | 2.1, 2.2, 2.3 |
| Task 3: WebSocket base path and origin patterns | 3.1, 3.2 |
| Task 4: bound the table prefix | 4.1, 4.2, 4.3, 4.4 |
| Task 5: core options refuse nil | 5.1, 5.2 |
| Task 6: broadcaster decode handlers refuse nil | 5.3, 5.4 |
| Task 7: documentation | 6.1 |
| Task 8: verify and hand off | 7.1, 7.2 |

Tasks 1 to 6 are independent of each other, except that Task 3 consumes `ntfy.CleanBasePath` from Task 2. Run Task 2 before Task 3.

---

### Task 1: Bound the reconnect delay

**Files:**
- Modify: `hub.go` (imports; the const block at lines 12-29; `WithReconnectDelay` at 110-118; the `NewHub` godoc at 120-125 and switch at 150-171)
- Test: `hub_test.go` (imports; `TestHubReconnectDelay` cases at 821-883)

**Interfaces:**
- Consumes: `ntfy.NewHub(broadcaster Broadcaster, opts ...HubOption) (*Hub, error)`, `ntfy.WithReconnectDelay(d time.Duration) HubOption`, `(*Hub).ReconnectDelay() time.Duration`, `ntfy.NewInProcessBroadcaster()`, and `ntfy.ErrConfiguration`.
- Produces: `const ntfy.MaxReconnectDelay time.Duration = time.Duration(math.MaxInt64 / 2)`.

- [ ] **Step 1: Write the failing cases (tasks 1.1)**

In `hub_test.go`, add `"math"` to the imports, between `"errors"` and `"strconv"`. In `TestHubReconnectDelay`, add these two cases after "a delay under a millisecond is a configuration error":

```go
		{
			name: "a delay past half the range is a configuration error",
			opts: []ntfy.HubOption{ntfy.WithReconnectDelay(time.Duration(math.MaxInt64 / 4 * 3))},
			assert: func(t *testing.T, hub *ntfy.Hub, err error) {
				require.ErrorIs(t, err, ntfy.ErrConfiguration)
				assert.Nil(t, hub)
			},
		},
		{
			name: "the largest representable base never draws below itself",
			opts: []ntfy.HubOption{ntfy.WithReconnectDelay(time.Duration(math.MaxInt64 / 2))},
			assert: func(t *testing.T, hub *ntfy.Hub, err error) {
				require.NoError(t, err)

				for range 1000 {
					assert.GreaterOrEqual(t, hub.ReconnectDelay(), time.Duration(math.MaxInt64/2))
				}
			},
		},
```

- [ ] **Step 2: Run it and watch the first case fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestHubReconnectDelay' -count=1 .`

Expected:
```
--- FAIL: TestHubReconnectDelay/a_delay_past_half_the_range_is_a_configuration_error
    Error: Expected error with "ntfy: invalid configuration" in chain but got nil.
```
"the largest representable base never draws below itself" passes. It is the guard that the ceiling chosen in Step 3 does not overflow. To confirm that it can fail, temporarily change its base to `math.MaxInt64 / 4 * 3`, and watch it report negative delays such as `"-1486405h37m13.681891137s" is not greater than or equal to …`. Then restore it.

- [ ] **Step 3: Add the ceiling**

In `hub.go`, add `"math"` to the imports, between `"fmt"` and `"math/rand/v2"`. Add this to the end of the `const (...)` block:

```go
	// MaxReconnectDelay is the largest base [WithReconnectDelay] accepts: half
	// of time.Duration's range, the largest base whose jittered value, below
	// twice the base, is still a representable duration.
	MaxReconnectDelay = time.Duration(math.MaxInt64 / 2)
```

Replace the godoc and body of `WithReconnectDelay` with:

```go
// WithReconnectDelay replaces [DefaultReconnectDelay], the base a stream tells
// its client to wait before reconnecting. It must be at least a millisecond,
// the resolution a server-sent event stream carries, and at most
// [MaxReconnectDelay], above which the jittered delay could not be represented.
//
// The jitter that spreads it is not configurable: a base with no spread returns
// an instance's clients in one wave, which is what the delay exists to prevent.
func WithReconnectDelay(d time.Duration) HubOption {
	return func(c *hubConfig) { c.reconnect = &d }
}
```

In the `NewHub` godoc, replace `or a reconnect delay that is\n// non-positive or under a millisecond, is a [ConfigurationError].` with:

```go
// per-recipient stream cap that is not positive; or a reconnect delay under a
// millisecond or above [MaxReconnectDelay], is a [ConfigurationError]. So is a
```

The surrounding lines stay as they are. In the `NewHub` switch, add this after the `*cfg.reconnect < time.Millisecond` case:

```go
	case cfg.reconnect != nil && *cfg.reconnect > MaxReconnectDelay:
		return nil, &ConfigurationError{
			Detail: "a hub reconnect delay must be at most MaxReconnectDelay, " +
				"so that the delay and its jitter stay below twice it",
		}
```

- [ ] **Step 4: Pin the ceiling (tasks 1.2)**

Add this case to `TestHubReconnectDelay`:

```go
		{
			name: "the ceiling is half the range, and one above it is a configuration error",
			opts: []ntfy.HubOption{ntfy.WithReconnectDelay(ntfy.MaxReconnectDelay + 1)},
			assert: func(t *testing.T, hub *ntfy.Hub, err error) {
				assert.Equal(t, time.Duration(math.MaxInt64/2), ntfy.MaxReconnectDelay)
				require.ErrorIs(t, err, ntfy.ErrConfiguration)
				assert.Nil(t, hub)
			},
		},
```

- [ ] **Step 5: Run and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestHubReconnectDelay|TestNewHub' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy`

- [ ] **Step 6: Commit**

```bash
git add hub.go hub_test.go
git commit -m "Refuse a reconnect delay whose jitter would overflow"
```

---

### Task 2: Validate the HTTP base path

**Files:**
- Modify: `http.go` (`handlerConfig` at 46-52, `WithBasePath` at 61-69, and `NewHandler` at 95-127)
- Test: `http_test.go` (`TestNewHandler` cases at 148-185, and two new test functions after `TestNewHandler`)

**Interfaces:**
- Consumes: `ntfy.NewHandler(svc *Service, hub *Hub, opts ...HandlerOption) (*Handler, error)`, `(*Handler).Routes() []Route`, `ntfy.DefaultBasePath`, and `ntfy.ConfigurationError{Detail string}`. In `http_test.go`: `headerActor`, `newHTTPEnv(t, run bool, hubOpts []ntfy.HubOption, handlerOpts ...ntfy.HandlerOption) *httpEnv` and `(*httpEnv).do(t, method, target, actor string, body io.Reader) *httptest.ResponseRecorder`.
- Produces: `func ntfy.CleanBasePath(path string) (string, error)`. It returns the path without trailing slashes, `""` for the root, or a `*ConfigurationError`. Task 3 consumes it.

- [ ] **Step 1: Extract the trimming, preserving behaviour (tasks 2.1)**

In `http.go`, add this after `WithBasePath`:

```go
// CleanBasePath drops a base path's trailing slashes.
func CleanBasePath(path string) (string, error) {
	return strings.TrimRight(path, "/"), nil
}
```

Replace the body of `WithBasePath` with:

```go
	return func(c *handlerConfig) {
		if trimmed, _ := CleanBasePath(path); trimmed != "" {
			c.basePath = trimmed
		}
	}
```

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestNewHandler' -count=1 .`
Expected: `ok` (unchanged behaviour).

- [ ] **Step 2: Write the failing tests (tasks 2.2)**

In `http_test.go`, add these cases to `TestNewHandler`, after "a base path replaces the default, without a trailing slash":

```go
		{
			name: "a base path without a leading slash is refused", svc: svc, hub: hub,
			opts:   []ntfy.HandlerOption{ntfy.WithActor(headerActor), ntfy.WithBasePath("api")},
			assert: refused,
		},
		{
			name: "a base path the router cannot parse is refused", svc: svc, hub: hub,
			opts:   []ntfy.HandlerOption{ntfy.WithActor(headerActor), ntfy.WithBasePath("/a{")},
			assert: refused,
		},
		{
			name: "an empty base path is refused", svc: svc, hub: hub,
			opts:   []ntfy.HandlerOption{ntfy.WithActor(headerActor), ntfy.WithBasePath("")},
			assert: refused,
		},
		{
			name: "the root base path serves the contract at the root", svc: svc, hub: hub,
			opts: []ntfy.HandlerOption{ntfy.WithActor(headerActor), ntfy.WithBasePath("/")},
			assert: func(t *testing.T, handler *ntfy.Handler, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, handler.Routes())
				assert.Equal(t, "GET /notifications", patterns(handler)[0])
			},
		},
```

Add these two functions after `TestNewHandler`:

```go
// TestCleanBasePath holds the base path rule both transports share. It is a
// pure function, so the table has no ctx field.
func TestCleanBasePath(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		path   string
		assert func(t *testing.T, cleaned string, err error)
	}

	cleansTo := func(want string) func(t *testing.T, cleaned string, err error) {
		return func(t *testing.T, cleaned string, err error) {
			t.Helper()

			require.NoError(t, err)
			assert.Equal(t, want, cleaned)
		}
	}

	refused := func(t *testing.T, cleaned string, err error) {
		t.Helper()

		require.ErrorIs(t, err, ntfy.ErrConfiguration)
		assert.Empty(t, cleaned)
	}

	cases := []testCase{
		{name: "the default is already clean", path: "/v1", assert: cleansTo("/v1")},
		{name: "a trailing slash is dropped", path: "/api/", assert: cleansTo("/api")},
		{name: "a slash alone is the root", path: "/", assert: cleansTo("")},
		{name: "slashes alone are the root", path: "//", assert: cleansTo("")},
		{name: "an empty path is refused", path: "", assert: refused},
		{name: "a path without a leading slash is refused", path: "api", assert: refused},
		{name: "a malformed wildcard is refused", path: "/a{", assert: refused},
		{name: "a well-formed wildcard is refused", path: "/{tenant}", assert: refused},
		{name: "a closing brace is refused", path: "/a}", assert: refused},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cleaned, err := ntfy.CleanBasePath(tc.path)
			tc.assert(t, cleaned, err)
		})
	}
}

// TestHandlerServesUnderItsBasePath proves the routes answer where the base
// path puts them, and only there. The cases do not vary context, so the table
// has no ctx field.
func TestHandlerServesUnderItsBasePath(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		basePath string
		target   string
		assert   func(t *testing.T, rec *httptest.ResponseRecorder)
	}

	answers := func(status int) func(t *testing.T, rec *httptest.ResponseRecorder) {
		return func(t *testing.T, rec *httptest.ResponseRecorder) {
			t.Helper()

			assert.Equal(t, status, rec.Code, rec.Body.String())
		}
	}

	cases := []testCase{
		{name: "a replaced base path", basePath: "/api/", target: "/api/notifications", assert: answers(http.StatusOK)},
		{name: "the root", basePath: "/", target: "/notifications", assert: answers(http.StatusOK)},
		{name: "the root does not also serve the default", basePath: "/", target: "/v1/notifications", assert: answers(http.StatusNotFound)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := newHTTPEnv(t, false, nil, ntfy.WithBasePath(tc.basePath))
			tc.assert(t, env.do(t, http.MethodGet, tc.target, "alice", nil))
		})
	}
}
```

- [ ] **Step 3: Run them and watch them fail, in three runs**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestCleanBasePath' -count=1 .`

Expected: five failures, one for each refused row, each reading:
```
--- FAIL: TestCleanBasePath/a_path_without_a_leading_slash_is_refused
    Error: Expected error with "ntfy: invalid configuration" in chain but got nil.
```
The same appears for `an_empty_path…`, `a_malformed_wildcard…`, `a_well-formed_wildcard…` and `a_closing_brace…`. The four clean rows pass.

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestNewHandler/a_base_path_the_router_cannot_parse_is_refused' -count=1 .`

Expected: a panic, the proved defect:
```
panic: parsing "GET /a{/notifications": at offset 5: bad wildcard segment (must start with '{')
```

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestNewHandler|TestHandlerServesUnderItsBasePath' -skip 'TestNewHandler/a_base_path_the_router_cannot_parse_is_refused' -count=1 .`

Expected:
```
--- FAIL: TestNewHandler/a_base_path_without_a_leading_slash_is_refused
    Error: Expected error with "ntfy: invalid configuration" in chain but got nil.
--- FAIL: TestNewHandler/an_empty_base_path_is_refused
    Error: Expected error with "ntfy: invalid configuration" in chain but got nil.
--- FAIL: TestNewHandler/the_root_base_path_serves_the_contract_at_the_root
    expected: "GET /notifications"
    actual  : "GET /v1/notifications"
--- FAIL: TestHandlerServesUnderItsBasePath/the_root
    expected: 200
    actual  : 404
--- FAIL: TestHandlerServesUnderItsBasePath/the_root_does_not_also_serve_the_default
    expected: 404
    actual  : 200
```

- [ ] **Step 4: Implement the rule (tasks 2.3)**

In `http.go`, add `basePathSet bool` to `handlerConfig`:

```go
type handlerConfig struct {
	actor         func(*http.Request) (string, error)
	basePath      string
	basePathSet   bool
	authorizer    SubscriptionAuthorizer
	authorizerSet bool
}
```

Replace `WithBasePath` and `CleanBasePath` with:

```go
// WithBasePath serves the contract somewhere other than [DefaultBasePath],
// such as "/api". A trailing slash is ignored, and "/" serves the contract at
// the root. The path must begin with "/" and contain no "{" or "}"; anything
// else, including an empty path, is a [ConfigurationError] from [NewHandler].
// Omit the option to keep [DefaultBasePath]. A host that routes by a path
// parameter, such as a tenant, strips it in its own router first.
func WithBasePath(path string) HandlerOption {
	return func(c *handlerConfig) { c.basePath, c.basePathSet = path, true }
}

// CleanBasePath is the rule every transport applies to a base path: trailing
// slashes are dropped, so "/" is the root and cleans to "", and the path must
// otherwise begin with "/" and contain no "{" or "}". Anything else is a
// [ConfigurationError].
//
// A transport calls it for its own base path option, so that its routes sit
// where the HTTP contract's do. A path without a leading slash would be read
// as a host by [http.ServeMux], and a brace as a wildcard, which the routes
// already use for {id}.
func CleanBasePath(path string) (string, error) {
	switch {
	case path == "":
		return "", &ConfigurationError{
			Detail: `a base path must not be empty; omit WithBasePath to keep ` + DefaultBasePath +
				`, or pass "/" to serve at the root`,
		}
	case !strings.HasPrefix(path, "/"):
		return "", &ConfigurationError{
			Detail: fmt.Sprintf(`the base path %q must begin with "/"; a path without one is read as a host by http.ServeMux`, path),
		}
	case strings.ContainsAny(path, "{}"):
		return "", &ConfigurationError{
			Detail: fmt.Sprintf("the base path %q must be a literal path; wildcards belong to the routes, which already use {id}", path),
		}
	}

	return strings.TrimRight(path, "/"), nil
}
```

In `NewHandler`:
- change the first line to `cfg := handlerConfig{authorizer: SelfOnly}`;
- in the godoc, replace `a missing actor, or a nil subscription policy is a [ConfigurationError].` with `a missing actor, a nil subscription policy, or a base path [CleanBasePath] refuses is a [ConfigurationError].`;
- add this after the `switch` and before `h := &Handler{`:

```go
	basePath := DefaultBasePath

	if cfg.basePathSet {
		cleaned, err := CleanBasePath(cfg.basePath)
		if err != nil {
			return nil, err
		}

		basePath = cleaned
	}
```

In the `h := &Handler{...}` literal, change `basePath: cfg.basePath` to `basePath: basePath`.

- [ ] **Step 5: Run and watch every case pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestCleanBasePath|TestNewHandler|TestHandlerServesUnderItsBasePath' -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy`, with no panic.

Then run the whole module: `GOTOOLCHAIN=go1.26.8 go test -count=1 .`. Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add http.go http_test.go
git commit -m "Refuse an HTTP base path the router would misread, and serve / at the root"
```

---

### Task 3: WebSocket base path and origin patterns

**Files:**
- Modify: `websocket/handler.go` (`config` at 52-62; `WithBasePath` at 73-82; `WithOriginPatterns` and `WithAnyOrigin` at 95-108; `NewHandler` at 128-194; the comment on `checkOrigin` at 490-510)
- Test: `websocket/handler_test.go` (`TestNewHandler`, before its closing `}` at about 101)

**Interfaces:**
- Consumes: `ntfy.CleanBasePath(path string) (string, error)` from Task 2, `ntfy.DefaultBasePath`, `ntfy.ConfigurationError{Detail string}`, `websocket.NewHandler(svc *ntfy.Service, hub *ntfy.Hub, opts ...Option) (*Handler, error)` and `(*Handler).Pattern() string`. In the test: `someActor`, `configurationError`, `svc` and `hub`.
- Produces: the unexported `func validateOriginPattern(pattern string) error`.

- [ ] **Step 1: Write the failing cases (tasks 3.1)**

In `websocket/handler_test.go`, add this closure after `configurationError` in `TestNewHandler`:

```go
	accepted := func(t *testing.T, handler *websocket.Handler, err error) {
		t.Helper()

		require.NoError(t, err)
		assert.NotNil(t, handler)
	}
```

Add these cases at the end of `cases`:

```go
		{
			name: "a base path without a leading slash", svc: svc, hub: hub,
			opts:   []websocket.Option{websocket.WithActor(someActor), websocket.WithBasePath("api")},
			assert: configurationError,
		},
		{
			name: "an empty base path", svc: svc, hub: hub,
			opts:   []websocket.Option{websocket.WithActor(someActor), websocket.WithBasePath("")},
			assert: configurationError,
		},
		{
			name: "a base path the router cannot parse", svc: svc, hub: hub,
			opts:   []websocket.Option{websocket.WithActor(someActor), websocket.WithBasePath("/a{")},
			assert: configurationError,
		},
		{
			name: "the root base path serves at the root", svc: svc, hub: hub,
			opts: []websocket.Option{websocket.WithActor(someActor), websocket.WithBasePath("/")},
			assert: func(t *testing.T, handler *websocket.Handler, err error) {
				require.NoError(t, err)
				assert.Equal(t, "GET /notifications/socket", handler.Pattern())
			},
		},
		{
			name: "a malformed origin pattern", svc: svc, hub: hub,
			opts:   []websocket.Option{websocket.WithActor(someActor), websocket.WithOriginPatterns("[app.example.com")},
			assert: configurationError,
		},
		{
			name: "an origin pattern written as a full origin", svc: svc, hub: hub,
			opts:   []websocket.Option{websocket.WithActor(someActor), websocket.WithOriginPatterns("https://app.example.com")},
			assert: configurationError,
		},
		{
			name: "an empty origin pattern", svc: svc, hub: hub,
			opts:   []websocket.Option{websocket.WithActor(someActor), websocket.WithOriginPatterns("")},
			assert: configurationError,
		},
		{
			name: "origin patterns after any origin", svc: svc, hub: hub,
			opts: []websocket.Option{
				websocket.WithActor(someActor), websocket.WithAnyOrigin(), websocket.WithOriginPatterns("app.example.com"),
			},
			assert: configurationError,
		},
		{
			name: "any origin after origin patterns", svc: svc, hub: hub,
			opts: []websocket.Option{
				websocket.WithActor(someActor), websocket.WithOriginPatterns("app.example.com"), websocket.WithAnyOrigin(),
			},
			assert: configurationError,
		},
		{
			name: "an empty list of origin patterns", svc: svc, hub: hub,
			opts:   []websocket.Option{websocket.WithActor(someActor), websocket.WithOriginPatterns()},
			assert: accepted,
		},
		{
			name: "wildcard and port origin patterns", svc: svc, hub: hub,
			opts: []websocket.Option{
				websocket.WithActor(someActor), websocket.WithOriginPatterns("*.example.com", "app.example.com:8443"),
			},
			assert: accepted,
		},
```

- [ ] **Step 2: Run them and watch them fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -C websocket -run 'TestNewHandler' -count=1 .`

Expected: eight refusal rows fail, each with this line:
```
Error: Expected error with "ntfy: invalid configuration" in chain but got nil.
```
The eight rows are:
- `a_base_path_without_a_leading_slash`
- `an_empty_base_path`
- `a_base_path_the_router_cannot_parse`
- `a_malformed_origin_pattern`
- `an_origin_pattern_written_as_a_full_origin`
- `an_empty_origin_pattern`
- `origin_patterns_after_any_origin`
- `any_origin_after_origin_patterns`

The root row fails too:
```
--- FAIL: TestNewHandler/the_root_base_path_serves_at_the_root
    expected: "GET /notifications/socket"
    actual  : "GET /v1/notifications/socket"
```
The two `accepted` rows pass.

- [ ] **Step 3: Implement (tasks 3.2)**

In `websocket/handler.go`, add `basePathSet bool` after `basePath string` in `config`. Replace `WithBasePath`, `WithOriginPatterns` and `WithAnyOrigin` with:

```go
// WithBasePath serves the route somewhere other than [ntfy.DefaultBasePath],
// under the same rule as the HTTP contract, [ntfy.CleanBasePath]: a trailing
// slash is ignored, "/" serves at the root, and a path that is empty, does not
// begin with "/" or contains "{" or "}" is a [ntfy.ConfigurationError] from
// [NewHandler]. Omit the option to keep the default.
func WithBasePath(basePath string) Option {
	return func(c *config) { c.basePath, c.basePathSet = basePath, true }
}
```

```go
// WithOriginPatterns permits browser origins beyond the request's own host,
// which is the only one permitted by default. A pattern is matched against the
// origin's host, such as "app.example.com" or "app.example.com:8443", and may
// use the wildcards of [path.Match], such as "*.example.com". A pattern that
// can never match a host — empty, malformed for [path.Match], or containing
// "/" as a full origin such as "https://app.example.com" does — is a
// [ntfy.ConfigurationError] from [NewHandler]. So is combining patterns with
// [WithAnyOrigin].
func WithOriginPatterns(patterns ...string) Option {
	return func(c *config) { c.origins = append(c.origins, patterns...) }
}

// WithAnyOrigin permits every browser origin. It is the explicit opt-out from
// origin checking, for a host that checks origins somewhere else, and it is
// named so that accepting cross-site connections is never an accident. It
// cannot be combined with [WithOriginPatterns], whose patterns it would make
// dead.
func WithAnyOrigin() Option {
	return func(c *config) { c.anyOrigin = true }
}
```

In `NewHandler`:
- change the first line to `cfg := config{authorizer: ntfy.SelfOnly}`;
- in the godoc, replace `not positive is a [ntfy.ConfigurationError].` with `not positive, a base path [ntfy.CleanBasePath] refuses, an origin pattern that can never match a host, or origin patterns combined with [WithAnyOrigin] is a [ntfy.ConfigurationError].`;
- add a case to the `switch`, after the write-timeout case:

```go
	case cfg.anyOrigin && len(cfg.origins) > 0:
		return nil, &ntfy.ConfigurationError{
			Detail: "WithAnyOrigin permits every origin, so the origin patterns would have no effect; choose one",
		}
	}
```

(This replaces the switch's closing `}`.) Then add this after the switch:

```go
	for _, pattern := range cfg.origins {
		if err := validateOriginPattern(pattern); err != nil {
			return nil, err
		}
	}

	basePath := ntfy.DefaultBasePath

	if cfg.basePathSet {
		cleaned, err := ntfy.CleanBasePath(cfg.basePath)
		if err != nil {
			return nil, err
		}

		basePath = cleaned
	}
```

In the `h := &Handler{...}` literal, change `pattern: http.MethodGet + " " + cfg.basePath + "/notifications/socket",` to `pattern: http.MethodGet + " " + basePath + "/notifications/socket",`.

Add this after `NewHandler`:

```go
// validateOriginPattern refuses a pattern that can never match an origin's
// host: an empty one, one containing "/" (a host never does, so the pattern is
// most likely a full origin with its scheme), and one [path.Match] cannot
// parse.
func validateOriginPattern(pattern string) error {
	switch {
	case pattern == "":
		return &ntfy.ConfigurationError{Detail: "an origin pattern must not be empty"}
	case strings.Contains(pattern, "/"):
		return &ntfy.ConfigurationError{
			Detail: fmt.Sprintf(`the origin pattern %q contains "/" and can never match a host; `+
				`write the host, such as "app.example.com"`, pattern),
		}
	}

	if _, err := path.Match(pattern, ""); err != nil {
		return &ntfy.ConfigurationError{Detail: fmt.Sprintf("the origin pattern %q is malformed: %v", pattern, err)}
	}

	return nil
}
```

In `checkOrigin`, put this comment above the `for _, pattern := range h.origins {` loop:

```go
	// Every pattern was parsed by validateOriginPattern at construction, so
	// path.Match cannot report a malformed one here.
```

- [ ] **Step 4: Run and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -C websocket -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy/websocket`. That includes `refusal_test.go`'s origin cases, which use `app.example.com` and `WithAnyOrigin()` separately.

- [ ] **Step 5: Commit**

```bash
git add websocket/handler.go websocket/handler_test.go
git commit -m "Refuse WebSocket base paths and origin patterns that can never match"
```

---

### Task 4: Bound the table prefix

**Files:**
- Create: `sqlstore/prefix_test.go`
- Modify: `sqlstore/store.go` (imports; the constants; `WithTablePrefix` at 56-61; `New` at 68-118), `sqlstore/store_test.go` (`TestNew` cases), `docs/schema.md:127-137`

**Interfaces:**
- Consumes:
  - in `sqlstore_test`: `openSQL(t, driver, dsn string) *sql.DB`, `stdsqlExecutor(t, db *sql.DB, dialect sqlkit.Dialect) sqlkit.Executor` and `newExecutor(t, dialect sqlkit.Dialect) sqlkit.Executor`;
  - `sqlkittest.RunTestPostgres(t) string` and `sqlkittest.RunTestMySQL(t) string`;
  - `sqlkit.DropTables(ctx, execer sqlkit.Execer, dialect sqlkit.Dialect, tables []string) error`;
  - on `*sqlstore.Store`: `Migrate`, `MigrateEmail`, `VerifySchema`, `VerifyEmailSchema`, `Insert`, `Tables`, `EmailTables`, `Schema` and `EmailSchema`.
- Produces: `const sqlstore.MaxTablePrefixBytes = 31`.

- [ ] **Step 1: Land the audit proof (tasks 4.1)**

Create `sqlstore/prefix_test.go`:

```go
package sqlstore_test

// The table prefix's length bound, against the databases whose identifier
// limits it exists for. The cases do not vary context, so the tables carry no
// ctx field.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/sqlstore"
	"github.com/kartaladev/sqlkit"
	"github.com/kartaladev/sqlkit/sqlkittest"
)

// TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema holds New to prefixes the
// database can hold: a prefix is refused at construction, or every table and
// index exists after migrating and a notification can be published. PostgreSQL
// truncates identifiers at 63 bytes without saying so, so a longer prefix makes
// index names collide and CREATE INDEX IF NOT EXISTS skip them.
func TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema(t *testing.T) {
	t.Parallel()

	postgres := stdsqlExecutor(t, openSQL(t, "postgres", sqlkittest.RunTestPostgres(t)), sqlkit.PostgreSQL)

	type testCase struct {
		name     string
		executor sqlkit.Executor
		prefix   string
		assert   func(t *testing.T, executor sqlkit.Executor, store *sqlstore.Store, err error)
	}

	// prefixOf is n bytes, ending in an underscore; each case uses its own
	// letter so that cases sharing a database never share a name.
	prefixOf := func(letter string, n int) string { return strings.Repeat(letter, n-1) + "_" }

	refusedOrWorking := func(t *testing.T, executor sqlkit.Executor, store *sqlstore.Store, err error) {
		t.Helper()

		if err != nil {
			require.ErrorIs(t, err, ntfy.ErrConfiguration)
			assert.Nil(t, store)

			return
		}

		t.Cleanup(func() {
			// t.Context is already cancelled when cleanup runs.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			execer := executor.(sqlkit.Execer)
			_ = sqlkit.DropTables(ctx, execer, executor.Dialect(), store.EmailTables())
			_ = sqlkit.DropTables(ctx, execer, executor.Dialect(), store.Tables())
		})

		require.NoError(t, store.Migrate(t.Context()), "Migrate")
		assert.NoError(t, store.VerifySchema(t.Context()), "VerifySchema after a successful Migrate")

		n := ntfy.Notification{
			ID: "n-1", Recipient: "alice", SourceID: "event-1", Subject: "task-1", SubjectVersion: 1,
			Kind: "offer", State: ntfy.StateActive, CreatedAt: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
		}
		_, err = store.Insert(t.Context(), n.Subject, []ntfy.Insertion{{Notification: n}})
		assert.NoError(t, err, "a store New accepted and Migrate reported ready cannot publish")

		require.NoError(t, store.MigrateEmail(t.Context()), "MigrateEmail")
		assert.NoError(t, store.VerifyEmailSchema(t.Context()), "VerifyEmailSchema after a successful MigrateEmail")
	}

	cases := []testCase{
		{name: "PostgreSQL, 44 bytes", executor: postgres, prefix: prefixOf("a", 44), assert: refusedOrWorking},
		{name: "PostgreSQL, 32 bytes", executor: postgres, prefix: prefixOf("b", 32), assert: refusedOrWorking},
		{name: "PostgreSQL, 31 bytes", executor: postgres, prefix: prefixOf("c", 31), assert: refusedOrWorking},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store, err := sqlstore.New(tc.executor, sqlstore.WithTablePrefix(tc.prefix))
			tc.assert(t, tc.executor, store, err)
		})
	}
}
```

- [ ] **Step 2: Run it and watch it fail for the stated reason**

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema' -count=1 .`

Expected (Docker must be running):
```
--- FAIL: TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema/PostgreSQL,_44_bytes
    Error: … index aaaa…a_ntfy_notifications_source_key is missing; … (six indexes)
    Messages: VerifySchema after a successful Migrate
    Error: sqlkit: pq: there is no unique or exclusion constraint matching the ON CONFLICT specification (42P10)
    Messages: a store New accepted and Migrate reported ready cannot publish
    Error: … relation "aaaa…a_ntfy_email_deliveri" already exists (42P07)
    Messages: MigrateEmail
--- FAIL: TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema/PostgreSQL,_32_bytes
    Error: … index bbbb…b_ntfy_notifications_recipient_idx is missing
    Messages: VerifySchema after a successful Migrate
--- PASS: TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema/PostgreSQL,_31_bytes
```
A container that fails to start is not a red. Fix the environment and re-run.

- [ ] **Step 3: Write the failing constructor cases (tasks 4.2)**

In `sqlstore/store_test.go`, add `"strings"` to the imports. Then add these cases to `TestNew` after "a prefix that is not a plain identifier is a configuration error":

```go
		{
			name:     "a 32-byte prefix is a configuration error",
			executor: executor,
			opts:     []sqlstore.Option{sqlstore.WithTablePrefix(strings.Repeat("p", 32))},
			assert: func(t *testing.T, store *sqlstore.Store, err error) {
				require.ErrorIs(t, err, ntfy.ErrConfiguration)
				assert.Nil(t, store)
			},
		},
		{
			name:     "a 44-byte prefix is a configuration error",
			executor: executor,
			opts:     []sqlstore.Option{sqlstore.WithTablePrefix(strings.Repeat("p", 44))},
			assert: func(t *testing.T, store *sqlstore.Store, err error) {
				require.ErrorIs(t, err, ntfy.ErrConfiguration)
				assert.Nil(t, store)
			},
		},
```

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestNew$' -count=1 .`

Expected:
```
--- FAIL: TestNew/a_32-byte_prefix_is_a_configuration_error
    Error: Expected error with "ntfy: invalid configuration" in chain but got nil.
--- FAIL: TestNew/a_44-byte_prefix_is_a_configuration_error
    Error: Expected error with "ntfy: invalid configuration" in chain but got nil.
```

- [ ] **Step 4: Add the bound (tasks 4.3)**

In `sqlstore/store.go`, add `"strconv"` to the imports, between `"regexp"` and `"strings"`. Add this after the table constants:

```go
// MaxTablePrefixBytes is the longest table prefix [WithTablePrefix] accepts:
// PostgreSQL's 63-byte identifier limit less the longest name the store owns,
// ntfy_notifications_recipient_idx (32 bytes). MySQL's limit is 64 and SQLite
// has none; one bound serves every dialect, so that a schema can move between
// them. Above it, PostgreSQL truncates names without saying so, and indexes
// whose truncated names collide are silently skipped.
const MaxTablePrefixBytes = 31
```

Replace the `WithTablePrefix` godoc with:

```go
// WithTablePrefix prefixes every table and index name the store owns, so that
// it can share a database whose names would otherwise collide. The default is
// no prefix. A prefix may contain only letters, digits and underscores, and be
// at most [MaxTablePrefixBytes] bytes; [New] refuses any other.
```

In the `New` godoc, replace `a prefix that is not a plain identifier is a\n// [ntfy.ConfigurationError].` with:

```go
// requirement, or a prefix that is not a plain identifier or is longer than
// [MaxTablePrefixBytes], is a [ntfy.ConfigurationError].
```

Add this case to `New`'s switch, after the `plainPrefix` case:

```go
	case len(cfg.prefix) > MaxTablePrefixBytes:
		return nil, &ntfy.ConfigurationError{
			Detail: "sqlstore: a table prefix may be at most " + strconv.Itoa(MaxTablePrefixBytes) +
				" bytes, so that every table and index name fits PostgreSQL's 63-byte identifiers; this one is " +
				strconv.Itoa(len(cfg.prefix)),
		}
```

In `sqlstore/store_test.go`, add this case to `TestNew`:

```go
		{
			name:     "a prefix of exactly MaxTablePrefixBytes is accepted",
			executor: executor,
			opts:     []sqlstore.Option{sqlstore.WithTablePrefix(strings.Repeat("p", sqlstore.MaxTablePrefixBytes))},
			assert: func(t *testing.T, store *sqlstore.Store, err error) {
				require.NoError(t, err)
				assert.Equal(t, 31, sqlstore.MaxTablePrefixBytes)
				assert.NotNil(t, store)
			},
		},
```

In `sqlstore/prefix_test.go`, add `"regexp"` to the imports. Add a MySQL executor after `postgres :=`:

```go
	mysql := stdsqlExecutor(t, openSQL(t, "mysql", sqlkittest.RunTestMySQL(t)), sqlkit.MySQL)
```

Add a row to `cases`:

```go
		{name: "MySQL, the longest permitted", executor: mysql, prefix: prefixOf("m", sqlstore.MaxTablePrefixBytes), assert: refusedOrWorking},
```

Append the guard to the file:

```go
// TestTablePrefixBoundFitsEveryOwnedName ties MaxTablePrefixBytes to the
// published DDL: at the longest prefix, every name the store owns fits
// PostgreSQL's 63-byte identifiers, and the longest reaches it exactly, so the
// bound is neither too loose nor needlessly tight. An index added with a longer
// name fails here. The cases vary only the dialect, so the table has no ctx
// field.
func TestTablePrefixBoundFitsEveryOwnedName(t *testing.T) {
	t.Parallel()

	// identifierLimit is PostgreSQL's, the smaller of the two dialects that
	// have one.
	const identifierLimit = 63

	quoted := regexp.MustCompile("[\"`]([A-Za-z0-9_]+)[\"`]")

	type testCase struct {
		name    string
		dialect sqlkit.Dialect
		assert  func(t *testing.T, names []string)
	}

	fits := func(t *testing.T, names []string) {
		t.Helper()

		require.NotEmpty(t, names)

		longest := ""

		for _, name := range names {
			assert.LessOrEqualf(t, len(name), identifierLimit, "%s is %d bytes", name, len(name))

			if len(name) > len(longest) {
				longest = name
			}
		}

		assert.Lenf(t, longest, identifierLimit, "the bound is tight: the longest name, %s, reaches the limit", longest)
	}

	cases := []testCase{
		{name: "PostgreSQL", dialect: sqlkit.PostgreSQL, assert: fits},
		{name: "MySQL", dialect: sqlkit.MySQL, assert: fits},
		{name: "SQLite", dialect: sqlkit.SQLite, assert: fits},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store, err := sqlstore.New(newExecutor(t, tc.dialect),
				sqlstore.WithTablePrefix(strings.Repeat("p", sqlstore.MaxTablePrefixBytes)))
			require.NoError(t, err)

			var names []string
			for _, match := range quoted.FindAllStringSubmatch(store.Schema()+store.EmailSchema(), -1) {
				names = append(names, match[1])
			}

			tc.assert(t, names)
		})
	}
}
```

- [ ] **Step 5: Run and watch it pass, then prove the guard can fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestNew$|TestTablePrefixBoundFitsEveryOwnedName|TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema' -count=1 .`

Expected: `ok  	github.com/kartaladev/ntfy/sqlstore`. The 44- and 32-byte PostgreSQL rows now pass by refusal, and the 31-byte and MySQL rows by a working schema.

To check that the guard can fail, temporarily set `MaxTablePrefixBytes = 30` and run `-run TestTablePrefixBoundFitsEveryOwnedName`. It must fail with `the bound is tight: the longest name, …, reaches the limit` and `should have 63 item(s), but has 62`. Then set it to `32`: the guard fails with `ppp…_ntfy_notifications_recipient_idx is 64 bytes`, and the `TestNew` "exactly" row fails on its pinned `31`. Restore `31`.

- [ ] **Step 6: Correct the schema doc (tasks 4.4)**

In `docs/schema.md`, replace the last bullet of "## Table prefix":

```markdown
- PostgreSQL truncates identifiers at 63 bytes and MySQL refuses names over 64.
  The longest index name is `ntfy_notifications_recipient_idx`, 34 bytes, so
  keep a prefix to 29 bytes or fewer.
```

with:

```markdown
- A prefix may be at most `sqlstore.MaxTablePrefixBytes`, 31 bytes. PostgreSQL
  truncates identifiers at 63 bytes without saying so, and the longest name the
  store owns, `ntfy_notifications_recipient_idx`, is 32 bytes. With a longer
  prefix, truncated index names would collide and be skipped. MySQL refuses
  names over 64 bytes, and SQLite has no limit, but the bound is the same on
  every dialect. A longer prefix is a configuration error from `sqlstore.New`.
- A host already running with a longer prefix renames its tables and indexes to
  a shorter one with its database's own `ALTER ... RENAME` statements before
  upgrading.
```

Run: `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestTheDocumented' -count=1 .`
Expected: `ok`. These tests read their SQL from other sections of this document, and must be unaffected.

- [ ] **Step 7: Commit**

```bash
git add sqlstore/store.go sqlstore/store_test.go sqlstore/prefix_test.go docs/schema.md
git commit -m "Refuse a table prefix too long for PostgreSQL's identifiers"
```

---

### Task 5: Core options refuse nil

**Files:**
- Modify: `service.go:29-72` (four options) and `service.go:85-116` (`New`); `pruner.go:99-109` (`WithPruneErrorHandler`) and `pruner.go:176-199` (`validate`)
- Test: `service_test.go:79-93` (`TestNew`), `pruner_test.go:98-102` (`TestNewPruner`)

**Interfaces:**
- Consumes: `ntfy.New(store Store, opts ...Option) (*Service, error)`, `ntfy.NewPruner(svc *Service, opts ...PruneOption) (*Pruner, error)`, `ntfy.WithClock`, `ntfy.WithIDGenerator`, `ntfy.WithBroadcaster`, `ntfy.WithSignalErrorHandler` and `ntfy.WithPruneErrorHandler`. In `pruner_test.go`: `refused`.
- Produces: no new names. The options' nil behaviour changes.

- [ ] **Step 1: Write the failing cases (tasks 5.1)**

In `service_test.go`, add this closure to `TestNew` after the `testCase` type:

```go
	refused := func(t *testing.T, svc *ntfy.Service, err error) {
		t.Helper()

		require.ErrorIs(t, err, ntfy.ErrConfiguration)
		assert.Nil(t, svc)
	}
```

Replace the case "nil options leave the defaults in place" with:

```go
		{
			name:  "a nil option value is skipped",
			store: ntfy.NewMemoryStore(),
			opts:  []ntfy.Option{nil},
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.NoError(t, err)
				assert.IsType(t, &ntfy.InProcessBroadcaster{}, svc.Broadcaster())

				_, err = svc.Publish(context.WithoutCancel(t.Context()), draft)
				assert.NoError(t, err)
			},
		},
		{name: "a nil clock is a configuration error", store: ntfy.NewMemoryStore(), opts: []ntfy.Option{ntfy.WithClock(nil)}, assert: refused},
		{
			name: "a nil ID generator is a configuration error", store: ntfy.NewMemoryStore(),
			opts: []ntfy.Option{ntfy.WithIDGenerator(nil)}, assert: refused,
		},
		{
			name: "a nil broadcaster is a configuration error", store: ntfy.NewMemoryStore(),
			opts: []ntfy.Option{ntfy.WithBroadcaster(nil)}, assert: refused,
		},
		{
			name: "a nil signal error handler is a configuration error", store: ntfy.NewMemoryStore(),
			opts: []ntfy.Option{ntfy.WithSignalErrorHandler(nil)}, assert: refused,
		},
```

In `pruner_test.go`, add this case to `TestNewPruner` before "a nil option is ignored":

```go
		{name: "a nil error handler is refused", opts: []ntfy.PruneOption{ntfy.WithPruneErrorHandler(nil)}, assert: refused},
```

- [ ] **Step 2: Run them and watch them fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestNew$|TestNewPruner' -count=1 .`

Expected: five failures, each reading `Error: Expected error with "ntfy: invalid configuration" in chain but got nil.`:
```
--- FAIL: TestNew/a_nil_clock_is_a_configuration_error
--- FAIL: TestNew/a_nil_ID_generator_is_a_configuration_error
--- FAIL: TestNew/a_nil_broadcaster_is_a_configuration_error
--- FAIL: TestNew/a_nil_signal_error_handler_is_a_configuration_error
--- FAIL: TestNewPruner/a_nil_error_handler_is_refused
```
"a nil option value is skipped" passes.

- [ ] **Step 3: Implement (tasks 5.2)**

In `service.go`, replace the four options with:

```go
// WithClock replaces the default [SystemClock]. Nil is a [ConfigurationError]
// from [New]; omit the option to read the system clock. A typed nil inside the
// interface cannot be told from a working clock, and is not detected.
func WithClock(clock Clock) Option {
	return func(s *Service) { s.clock = clock }
}

// WithIDGenerator replaces the default [UUIDv7Generator]. Nil is a
// [ConfigurationError] from [New]; omit the option to mint UUIDv7 identifiers.
// Every identifier it mints must be 1 to [MaxIDBytes] bytes; a write that
// mints one outside that fails with a [ConfigurationError] and writes nothing
// under it.
func WithIDGenerator(generator IDGenerator) Option {
	return func(s *Service) { s.ids = generator }
}

// WithBroadcaster replaces the default [InProcessBroadcaster], which reaches
// only the process it runs in. A deployment of more than one instance needs a
// broadcaster that crosses instances. Nil is a [ConfigurationError] from
// [New]; omit the option to broadcast in process.
func WithBroadcaster(broadcaster Broadcaster) Option {
	return func(s *Service) { s.broadcaster = broadcaster }
}

// WithSignalErrorHandler receives errors from broadcasting a change. Such an
// error never fails the operation: the change is already stored, and a client
// re-reads the store when it reconnects. The default handler does nothing,
// which is safe but silent; a host should supply one that logs. Nil is a
// [ConfigurationError] from [New]; omit the option to keep the default.
func WithSignalErrorHandler(handler func(ctx context.Context, err error)) Option {
	return func(s *Service) { s.onSignalError = handler }
}
```

Replace the `New` godoc and add the check:

```go
// New builds a service over a store. With no options it reads the system clock,
// mints UUIDv7 identifiers and broadcasts in process. A nil store, or an option
// given nil, is a [ConfigurationError]; a nil option value is skipped.
```

```go
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}

	switch {
	case svc.clock == nil:
		return nil, &ConfigurationError{Detail: "WithClock was given nil; omit it to read the system clock"}
	case svc.ids == nil:
		return nil, &ConfigurationError{Detail: "WithIDGenerator was given nil; omit it to mint UUIDv7 identifiers"}
	case svc.broadcaster == nil:
		return nil, &ConfigurationError{Detail: "WithBroadcaster was given nil; omit it to broadcast in process"}
	case svc.onSignalError == nil:
		return nil, &ConfigurationError{
			Detail: "WithSignalErrorHandler was given nil; omit it to keep the default, which ignores broadcast failures",
		}
	}

	if err := svc.limits.validate(); err != nil {
```

In `pruner.go`, replace `WithPruneErrorHandler` with:

```go
// WithPruneErrorHandler receives the error of each pass [Pruner.Run] runs. The
// default handler does nothing, which is safe but silent; a host should supply
// one that logs. Nil is a [ConfigurationError] from [NewPruner]; omit the
// option to keep the default.
func WithPruneErrorHandler(handler func(ctx context.Context, err error)) PruneOption {
	return func(c *pruneConfig) { c.onError = handler }
}
```

Make this the first case of `pruneConfig.validate`'s switch:

```go
	case c.onError == nil:
		return refuse("WithPruneErrorHandler was given nil; omit it to keep the default, which ignores pass errors")
```

In the `NewPruner` godoc, change `or an unknown strategy is a\n// [ConfigurationError].` to `an unknown strategy, or a nil error handler is a\n// [ConfigurationError].`

- [ ] **Step 4: Run and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy`. Use gopls references on the four options and `WithPruneErrorHandler` to confirm that no production caller passes nil. The only nil callers were the replaced test case.

- [ ] **Step 5: Commit**

```bash
git add service.go service_test.go pruner.go pruner_test.go
git commit -m "Refuse a service or pruner option given nil"
```

---

### Task 6: Broadcaster decode handlers refuse nil

**Files:**
- Modify: `redis/broadcaster.go:44-121`, `nats/broadcaster.go:49-128`
- Test: `redis/broadcaster_test.go:48-60`, `nats/broadcaster_test.go:51-58`

**Interfaces:**
- Consumes: `redis.NewBroadcaster(client goredis.UniversalClient, opts ...Option) (*Broadcaster, error)`, `nats.NewBroadcaster(conn *natsgo.Conn, opts ...Option) (*Broadcaster, error)`, `redis.WithDecodeErrorHandler` and `nats.WithDecodeErrorHandler` (both `func(handler func(ctx context.Context, err error)) Option`), `redis.ConfigurationError`, `nats.ConfigurationError` and `ErrConfiguration`. In the tests: `configurationError`, `client` and `conn`.
- Produces: no new names. `config.onError` becomes `*func(ctx context.Context, err error)` in both packages.

- [ ] **Step 1: Write the failing Redis case (tasks 5.3)**

In `redis/broadcaster_test.go`, add `"context"` to the imports. In "a channel and a timeout replace the defaults", replace `redis.WithDecodeErrorHandler(nil),` with `redis.WithDecodeErrorHandler(func(context.Context, error) {}),`. Add this case after "a negative timeout":

```go
		{
			name:   "a nil decode error handler",
			client: client,
			opts:   []redis.Option{redis.WithDecodeErrorHandler(nil)},
			assert: configurationError,
		},
```

Run: `GOTOOLCHAIN=go1.26.8 go test -C redis -run 'TestNewBroadcaster' -count=1 .`

Expected:
```
--- FAIL: TestNewBroadcaster/a_nil_decode_error_handler
    Error: Expected error with "redis: invalid configuration" in chain but got nil.
```

- [ ] **Step 2: Implement for Redis**

In `redis/broadcaster.go`, update `config` and its comment:

```go
// config is what the options set. The timeout and the handler are pointers
// because an explicit zero or nil is a wiring mistake to report, while an
// absent value is the default.
type config struct {
	channel string
	timeout *time.Duration
	onError *func(ctx context.Context, err error)
}
```

```go
// WithDecodeErrorHandler receives messages on the channel that could not be
// read, such as one in a signal format version this library does not know
// (matching [ntfy.ErrUnknownSignalFormat]). No signal is delivered for such a
// message, and receiving continues. The default handler does nothing, which is
// safe but silent; a host should supply one that logs. Nil is a
// [ConfigurationError]; omit the option to keep the default.
func WithDecodeErrorHandler(handler func(ctx context.Context, err error)) Option {
	return func(c *config) { c.onError = &handler }
}
```

In `NewBroadcaster`, add this case to the switch after the timeout case:

```go
	case cfg.onError != nil && *cfg.onError == nil:
		return nil, &ConfigurationError{
			Detail: "WithDecodeErrorHandler was given nil; omit it to keep the default, which drops unreadable messages silently",
		}
```

Replace `if cfg.onError != nil { b.onError = cfg.onError }` with:

```go
	if cfg.onError != nil {
		b.onError = *cfg.onError
	}
```

In the `NewBroadcaster` godoc, change `A nil client, an empty channel or a timeout that is\n// not positive is a [ConfigurationError].` to `A nil client, an empty channel, a timeout that is\n// not positive, or a nil decode error handler is a [ConfigurationError].`

Run: `GOTOOLCHAIN=go1.26.8 go test -C redis -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy/redis`

- [ ] **Step 3: Write the failing NATS case (tasks 5.4)**

In `nats/broadcaster_test.go`, add `"context"` to the imports. In "a subject replaces the default", replace `nats.WithDecodeErrorHandler(nil)` with `nats.WithDecodeErrorHandler(func(context.Context, error) {})`. Add this case after it:

```go
		{
			name:   "a nil decode error handler",
			conn:   conn,
			opts:   []nats.Option{nats.WithDecodeErrorHandler(nil)},
			assert: configurationError,
		},
```

Run: `GOTOOLCHAIN=go1.26.8 go test -C nats -run 'TestNewBroadcaster' -count=1 .`

Expected:
```
--- FAIL: TestNewBroadcaster/a_nil_decode_error_handler
    Error: Expected error with "nats: invalid configuration" in chain but got nil.
```

- [ ] **Step 4: Implement for NATS**

In `nats/broadcaster.go`, change `config.onError` to `onError *func(ctx context.Context, err error)`, and extend the comment above `config` to say that the handler is a pointer for the same reason. Replace `WithDecodeErrorHandler` with:

```go
// WithDecodeErrorHandler receives messages on the subject that could not be
// read, such as one in a signal format version this library does not know
// (matching [ntfy.ErrUnknownSignalFormat]). No signal is delivered for such a
// message, and receiving continues. The default handler does nothing, which is
// safe but silent; a host should supply one that logs. Nil is a
// [ConfigurationError]; omit the option to keep the default.
func WithDecodeErrorHandler(handler func(ctx context.Context, err error)) Option {
	return func(c *config) { c.onError = &handler }
}
```

In `NewBroadcaster`, add this after the subscribe-timeout check:

```go
	if cfg.onError != nil && *cfg.onError == nil {
		return nil, &ConfigurationError{
			Detail: "WithDecodeErrorHandler was given nil; omit it to keep the default, which drops unreadable messages silently",
		}
	}
```

Replace `if cfg.onError != nil { b.onError = cfg.onError }` with:

```go
	if cfg.onError != nil {
		b.onError = *cfg.onError
	}
```

Add "or a nil decode error handler" to the list of configuration errors in the `NewBroadcaster` godoc.

Run: `GOTOOLCHAIN=go1.26.8 go test -C nats -count=1 .`
Expected: `ok  	github.com/kartaladev/ntfy/nats`

- [ ] **Step 5: Commit**

```bash
git add redis/broadcaster.go redis/broadcaster_test.go nats/broadcaster.go nats/broadcaster_test.go
git commit -m "Refuse a broadcaster decode error handler given nil"
```

---

### Task 7: Documentation

**Files:**
- Modify: `docs/notifications.md:57-58` and `:212`; `docs/realtime-operations.md:135` and `:147`

**Interfaces:**
- Consumes: the names produced by Tasks 1–6: `ntfy.CleanBasePath`, `ntfy.MaxReconnectDelay` and `sqlstore.MaxTablePrefixBytes`.
- Produces: nothing.

- [ ] **Step 1: Update `docs/notifications.md` (tasks 6.1)**

Replace:

```markdown
A nil option is ignored and keeps the default. A nil store is a configuration
error from `New`, before any traffic.
```

with:

```markdown
To keep a default, omit its option. An option given nil — `WithClock(nil)`,
`WithBroadcaster(nil)` and the like — is a configuration error from `New`, as is
a nil store, before any traffic. A nil option value in the list is skipped, so
that a host can build its options conditionally.
```

Replace `- `WithBasePath` replaces `/v1`.` with:

```markdown
- `WithBasePath` replaces `/v1`, and `"/"` serves the contract at the root. A
  base path must begin with `/` and contain no `{` or `}`; an empty one, or any
  other, is a configuration error from `NewHandler`. `ntfy.CleanBasePath` is the
  rule, and the WebSocket handler applies the same one.
```

- [ ] **Step 2: Update `docs/realtime-operations.md`**

Replace the origins row with:

```markdown
| Browser origins | the request's own host only | `websocket.WithOriginPatterns` with hosts such as `app.example.com` or `*.example.com`, or `websocket.WithAnyOrigin`, but not both; a pattern that can never match a host is refused by `websocket.NewHandler` |
```

Replace the reconnect-delay row with:

```markdown
| Reconnect delay | 1s, spread per stream to less than 2s (server-sent events only) | `ntfy.WithReconnectDelay`, from 1ms to `ntfy.MaxReconnectDelay` |
```

- [ ] **Step 3: Run the docs tests**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .`
Expected: `ok`. The `WithBasePath` needle is still present in "HTTP and mounting".

- [ ] **Step 4: Commit**

```bash
git add docs/notifications.md docs/realtime-operations.md
git commit -m "Document the wiring mistakes the constructors now refuse"
```

---

### Task 8: Verify and hand off

**Files:** none new.

**Interfaces:** none.

- [ ] **Step 1: The full gate (tasks 7.1)**

Run: `make all`
Expected: lint, split-check and every module's tests pass, ending without `FAIL` or `Error`.

Run: `make store-matrix`
Expected: all seven driver-by-dialect conformance runs pass.

Run: `openspec validate refuse-wiring-mistakes-at-construction --strict`
Expected: `Change 'refuse-wiring-mistakes-at-construction' is valid`

- [ ] **Step 2: Review (tasks 7.2)**

Run `/code-review` on the branch diff. Answer it under `superpowers:receiving-code-review`: verify each finding before agreeing, and prove any claimed defect with a failing test (`.claude/rules/prove-errors-with-tests.md`) before fixing it. If a finding changes the work, update `tasks.md` and this plan in the same turn. Re-run Step 1.

- [ ] **Step 3: Mark `tasks.md`**

Tick every box in `tasks.md` whose verification ran green. It is the authoritative progress record, not this plan's boxes.
