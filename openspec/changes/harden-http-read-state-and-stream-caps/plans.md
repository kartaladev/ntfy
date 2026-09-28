# Harden HTTP Read State and Stream Caps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close two findings from the 2026-09-28 audit:
- the HTTP handler refuses cross-origin browser requests that would mark notifications read;
- the hub caps streams per acting user, not per followed recipient.

Each fix has a safe default and a named override.

**Architecture:**
- `Handler.acting`, which wraps every route, first runs `net/http.CrossOriginProtection.Check`. It is on by default. `WithTrustedOrigins` adds exact origins, and `WithoutCrossOriginProtection` turns it off.
- `markAllRead` refuses a non-blank body that is not declared `application/json`.
- `Hub.Subscribe` takes the acting user as well as the recipient, and counts open subscriptions per actor in an `opened` map in place of the per-recipient set size. The cap's names become `DefaultMaxStreamsPerActor` and `WithMaxStreamsPerActor`, keeping the value 8.

**Tech Stack:**
- Go 1.26, with the core on the standard library only; `net/http.CrossOriginProtection` is from Go 1.25+.
- `stretchr/testify`, `coder/websocket` in `websocket` tests only, and `gopls` for the renames.
- `golangci-lint` v2 and `openspec`.

**Spec:** `openspec/changes/harden-http-read-state-and-stream-caps/`. Read:
- `proposal.md`: the two findings and the failing audit output;
- `design.md`: D1 the cross-origin guard in `acting`, D2 the JSON body, D3 the per-actor cap and the renames, D4 the WebSocket pass-through;
- `specs/notification-http-api/spec.md` and `specs/notification-realtime/spec.md`: the scenarios this plan must satisfy.

## Global Constraints

- **Go 1.26: export `GOTOOLCHAIN=go1.26.8` before any Go command.** A newer Go may be first on `PATH`. The `make` targets already pin it.
- **The core module imports only the standard library** in production code. Tests may use `testify`, `goleak` and `go.uber.org/mock`. This is enforced by `.golangci.yml` depguard and `make split-check`. This change adds only `mime` and `net/http` symbols, both standard library.
- **Each satellite module may import only `ntfy`, `sqlkit` and its own client library.** `make split-check` is authoritative. `websocket` gains no import.
- **Tests follow the `table-test` skill:**
  - an `assert` closure on every case, never `want`/`wantErr` fields;
  - `t.Context()`, not `context.Background()`;
  - `require` only for preconditions.
  - None of the behaviour under test depends on cancellation, so no table carries a `ctx` field.
- **Test doubles come from the `use-mockgen` skill.** This change needs none. The HTTP tests use the real memory store and hub.
- **External services come from the `use-testcontainers` skill.** None is needed: no store changes.
- **`.claude/rules/prove-errors-with-tests.md`:** each red step is run, and its failing output compared with the one written here, before any production edit. A red caused by compilation does not count. Where a new option must exist for a test to compile, it is added first as a no-op (Task 3, Step 1).
- **`.claude/rules/golang-tdd.md`:** red → green → refactor. The test and the code that satisfies it land in the same commit. Consider `/simplify` after each green.
- **`.claude/rules/library-design.md`:** every new behaviour has a default and an override (`design.md` D1–D3); contradictory options fail in `NewHandler` or `NewHub`. The JSON-body rule has no override, and is documented as a limit (D2).
- **`.claude/rules/gopls-navigation.md`:** renames use `gopls rename` (`/Users/zakyalvan/go/bin/gopls`, or `$(go env GOPATH)/bin/gopls`), which renames across every module in `go.work`.
- **`.claude/rules/performance-benchmark.md`:** no performance claim is made, so no benchmark.
- **`.claude/rules/plans-beside-tasks.md`:** any edit to `tasks.md` is mirrored here in the same turn.
- **Done means `make all` passes**, which is lint, split-check and test over every module. `make store-matrix` runs too, as a regression check, although no store changes.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`, and end with the session's attribution lines.

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `http.go` | `ntfy` | **Modify.** Adds the `Handler.crossOrigin` field, the `handlerConfig` fields `trustedOrigins` and `withoutCrossOrigin`, `WithTrustedOrigins`, `WithoutCrossOriginProtection`, the `NewHandler` checks, the guard in `acting`, the `Content-Type` check in `markAllRead`, and `Subscribe(actor, recipient)` in `stream`. |
| `http_test.go` | `ntfy` | **Modify.** Adds `TestHandlerRefusesCrossOriginWrites`, `TestHandlerStreamCapsCountTheActingUser`, two `TestHandlerMarkRead` cases and two `TestNewHandler` cases. `httpEnv.do` sets `Content-Type` on a body. `Subscribe` call sites and the option name are renamed. |
| `hub.go` | `ntfy` | **Modify.** `Subscribe(actor, recipient string)`, the `opened` map, `Subscription.actor`, the renamed constant and option, and per-user wording. |
| `hub_test.go` | `ntfy` | **Modify.** Adds `TestHubCapsCountTheActingUser`. Call sites are updated mechanically. |
| `errors.go` | `ntfy` | **Modify.** The `ErrTooManyStreams` godoc. |
| `docs_test.go` | `ntfy` | **Modify.** Needles for the renamed option (by `gopls`), `WithTrustedOrigins`, `WithoutCrossOriginProtection` and `Content-Type: application/json`. |
| `docs/notifications.md` | — | **Modify.** The Realtime table row and a follower bullet; the HTTP section's cross-origin and JSON bullets; the error table. |
| `docs/realtime-operations.md` | — | **Modify.** The per-user cap sentence and the hub table row. |
| `websocket/handler.go` | `ntfy/websocket` | **Modify.** `h.hub.Subscribe(actor, recipient)`, and the `ServeHTTP` godoc. |
| `websocket/doc.go` | `ntfy/websocket` | **Modify.** "one per-user cap". |
| `websocket/refusal_test.go` | `ntfy/websocket` | **Modify.** Adds one `TestHandshakeRefusals` case. |
| `websocket/{docs,handler,shutdown,writeloop}_test.go` | `ntfy/websocket` | **Modify**, by `gopls rename` only. |

**Shared-file note:** `http_test.go` and `hub_test.go` are large, shared test files, and `docs/notifications.md` is asserted by `docs_test.go`. A change in flight that also edits the HTTP section's bullets or the Realtime table will conflict textually. Resolve by keeping both edits, and re-run `TestTheDocumentMatchesTheImplementation`.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: refuse cross-origin writes | 1.1, 2.1 |
| Task 2: a `read-all` body must be JSON | 1.4, 2.2 |
| Task 3: trusted origins and the opt-out | 3.1, 3.2 |
| Task 4: count stream caps per acting user | 1.2, 1.3, 4.1, 4.2, 4.3, 4.4 |
| Task 5: document the HTTP defaults | 5.1 |
| Task 6: verify and hand off | 1.5, 6.1 |

Task 4 carries two red steps. The first is the HTTP and WebSocket proofs, run against the old API. The second is the hub unit test, run after the signature refactor, because the new signature must exist before a hub test can call it and still fail on behaviour rather than compilation.

---

### Task 1: Refuse cross-origin writes

**Files:**
- Modify: `http.go:32-40` (`Handler`), `http.go:94-140` (`NewHandler`), `http.go:151-170` (`acting`)
- Test: `http_test.go` (append)

**Interfaces:**
- Consumes (existing, `http_test.go`):
  - `newHTTPEnv(t *testing.T, run bool, hubOpts []ntfy.HubOption, handlerOpts ...ntfy.HandlerOption) *httpEnv`, and the fields `svc *ntfy.Service`, `hub *ntfy.Hub`, `handler *ntfy.Handler`;
  - `(*httpEnv).publish(t, drafts ...ntfy.Draft) []ntfy.Notification`;
  - `offer(recipient, source string) ntfy.Draft`, `decodeError(t, rec) errorBody`, and `const actorHeader = "X-Actor"`.
- Produces: the unexported field `crossOrigin *http.CrossOriginProtection` on `Handler`, which is nil only after Task 3's opt-out. The error answered on refusal is `fmt.Errorf("%w: a cross-origin browser request may not change notifications", ErrUnauthorized)`, which gives `403`, code `forbidden`.

- [ ] **Step 1: Write the failing test (tasks 1.1)**

Append to `http_test.go`. It uses no new imports: `net/http`, `net/http/httptest`, `strings`, `testify` and `ntfy` are already imported.

```go
// TestHandlerRefusesCrossOriginWrites is the 2026-09-28 audit's proof: a page
// on another site that submits a plain HTML form to a route that changes
// notifications, carrying the victim's ambient credentials, is refused by
// default, the same default the WebSocket transport applies to a foreign
// origin. Requests from the application's own pages and from non-browser
// clients are served, and a host can trust named origins or opt out.
func TestHandlerRefusesCrossOriginWrites(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		handlerOpts []ntfy.HandlerOption
		method      string
		// target is the route, given the notification published for alice.
		target func(created ntfy.Notification) string
		// header is what the browser, or the client, sends.
		header map[string]string
		body   string
		assert func(t *testing.T, env *httpEnv, rec *httptest.ResponseRecorder)
	}

	crossSite := map[string]string{
		"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site", "Content-Type": "text/plain",
	}

	// sibling is a page on another origin of the same site, such as a web
	// front end calling its API on another subdomain.
	sibling := map[string]string{"Origin": "https://web.example.com", "Sec-Fetch-Site": "same-site"}

	readAll := func(ntfy.Notification) string { return "/v1/notifications/read-all" }
	readOne := func(n ntfy.Notification) string { return "/v1/notifications/" + n.ID + "/read" }

	refused := func(t *testing.T, env *httpEnv, rec *httptest.ResponseRecorder) {
		t.Helper()

		assert.Equal(t, http.StatusForbidden, rec.Code, "the cross-site request was served: %s", rec.Body.String())
		assert.Equal(t, "forbidden", decodeError(t, rec).Error.Code)

		count, err := env.svc.CountActive(t.Context(), "alice")
		require.NoError(t, err)
		assert.EqualValues(t, 1, count, "a cross-site request marked the victim's notification read")
	}

	served := func(t *testing.T, env *httpEnv, rec *httptest.ResponseRecorder) {
		t.Helper()

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		count, err := env.svc.CountActive(t.Context(), "alice")
		require.NoError(t, err)
		assert.Zero(t, count, "the request was served but marked nothing")
	}

	cases := []testCase{
		{
			name: "read-all from a cross-site form is refused", method: http.MethodPost,
			target: readAll, header: crossSite, assert: refused,
		},
		{
			name: "read-all from a cross-site text/plain form carrying a JSON body is refused", method: http.MethodPost,
			target: readAll, header: crossSite, body: `{"through":"2999-01-01T00:00:00Z","x":"="}`, assert: refused,
		},
		{
			name: "marking one read from a cross-site form is refused", method: http.MethodPost,
			target: readOne, header: crossSite, assert: refused,
		},
		{
			name: "a foreign origin from a browser that sends no Sec-Fetch-Site is refused", method: http.MethodPost,
			target: readAll, header: map[string]string{"Origin": "https://evil.example"}, assert: refused,
		},
		{
			name: "a same-origin page is served", method: http.MethodPost,
			target: readAll, header: map[string]string{"Origin": "https://app.example.com", "Sec-Fetch-Site": "same-origin"},
			assert: served,
		},
		{
			name: "a client that is not a browser, sending neither header, is served", method: http.MethodPost,
			target: readOne, assert: served,
		},
		{
			name: "a sibling site the host does not trust is refused", method: http.MethodPost,
			target: readAll, header: sibling, assert: refused,
		},
		{
			name: "a cross-site read changes nothing and is not refused", method: http.MethodGet,
			target: func(ntfy.Notification) string { return "/v1/notifications/count" }, header: crossSite,
			assert: func(t *testing.T, _ *httpEnv, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := newHTTPEnv(t, false, nil, tc.handlerOpts...)
			created := env.publish(t, offer("alice", "a1"))

			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.target(created[0]), strings.NewReader(tc.body))
			req.Host = "app.example.com"
			req.Header.Set(actorHeader, "alice") // the victim's ambient session

			for name, value := range tc.header {
				req.Header.Set(name, value)
			}

			rec := httptest.NewRecorder()
			env.handler.ServeHTTP(rec, req)

			tc.assert(t, env, rec)
		})
	}
}
```

Also append this test. It is standalone because its setup differs: it mounts `Routes()` one at a time on the test's own mux, as a router that is not the standard library's would, and it has one case.

```go
// TestHandlerRoutesRefuseCrossOriginWrites proves the refusal travels with each
// route, for a host that registers Routes one at a time instead of mounting
// the Handler.
func TestHandlerRoutesRefuseCrossOriginWrites(t *testing.T) {
	t.Parallel()

	env := newHTTPEnv(t, false, nil)
	env.publish(t, offer("alice", "a1"))

	mux := http.NewServeMux()
	for _, route := range env.handler.Routes() {
		mux.Handle(route.Method+" "+route.Pattern, route.Handler)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/notifications/read-all", http.NoBody)
	req.Host = "app.example.com"
	req.Header.Set(actorHeader, "alice")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "a route registered on its own served a cross-site write: %s", rec.Body.String())

	count, err := env.svc.CountActive(t.Context(), "alice")
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerRefusesCrossOriginWrites|TestHandlerRoutesRefuseCrossOriginWrites' -count=1 .`

`TestHandlerRoutesRefuseCrossOriginWrites` must fail with `expected: 403 actual: 200`, message `a route registered on its own served a cross-site write: {"marked":1}`. The rest of this step's expected output covers the table.

Expected: FAIL on exactly these five cases, each with `expected: 403` / `actual  : 200` and `a cross-site request marked the victim's notification read`. The following was observed on `main` `20967b3`, 2026-09-28:

```
--- FAIL: TestHandlerRefusesCrossOriginWrites/read-all_from_a_cross-site_form_is_refused
        Messages: the cross-site request was served: {"marked":1}
--- FAIL: TestHandlerRefusesCrossOriginWrites/read-all_from_a_cross-site_text/plain_form_carrying_a_JSON_body_is_refused
--- FAIL: TestHandlerRefusesCrossOriginWrites/marking_one_read_from_a_cross-site_form_is_refused
        Messages: the cross-site request was served: {"id":"…","recipient":"alice",…,"state":"READ",…}
--- FAIL: TestHandlerRefusesCrossOriginWrites/a_foreign_origin_from_a_browser_that_sends_no_Sec-Fetch-Site_is_refused
--- FAIL: TestHandlerRefusesCrossOriginWrites/a_sibling_site_the_host_does_not_trust_is_refused
--- PASS: …/a_same-origin_page_is_served
--- PASS: …/a_client_that_is_not_a_browser,_sending_neither_header,_is_served
--- PASS: …/a_cross-site_read_changes_nothing_and_is_not_refused
```

**STOP** if any refused case passes on unmodified code. The finding would then not reproduce: record it under "Does not reproduce" in the execution record, and ask the maintainer before going on.

- [ ] **Step 3: Add the guard**

In `http.go`, add the field to `Handler`, after `routes`:

```go
	routes     []Route
	// crossOrigin refuses cross-origin browser writes; nil after
	// [WithoutCrossOriginProtection].
	crossOrigin *http.CrossOriginProtection
}
```

In `NewHandler`, replace

```go
	h := &Handler{
		svc: svc, hub: hub, actor: cfg.actor, authorizer: cfg.authorizer,
		basePath: cfg.basePath, mux: http.NewServeMux(),
	}
```

with

```go
	h := &Handler{
		svc: svc, hub: hub, actor: cfg.actor, authorizer: cfg.authorizer,
		basePath: cfg.basePath, mux: http.NewServeMux(),
		crossOrigin: http.NewCrossOriginProtection(),
	}
```

Replace `acting` and its comment with:

```go
// acting wraps an endpoint with refusing a cross-origin browser write and
// establishing the acting user, answering forbidden when either fails. The
// origin is checked first, so a refused request never reaches the host's actor
// function.
func (h *Handler) acting(serve func(w http.ResponseWriter, r *http.Request, actor string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.crossOrigin != nil {
			if err := h.crossOrigin.Check(r); err != nil {
				WriteError(w, fmt.Errorf("%w: a cross-origin browser request may not change notifications", ErrUnauthorized))

				return
			}
		}

		actor, err := h.actor(r)
		if err != nil {
			WriteError(w, err)

			return
		}

		if actor == "" {
			WriteError(w, fmt.Errorf("%w: no acting user is established", ErrUnauthorized))

			return
		}

		serve(w, r, actor)
	})
}
```

Replace the second paragraph of `NewHandler`'s godoc with:

```go
// With no options beyond the required [WithActor] it serves under
// [DefaultBasePath], authorizes streams with [SelfOnly], and refuses a
// cross-origin browser request to mark notifications read. A nil service or
// hub, a missing actor, or a nil subscription policy is a [ConfigurationError].
```

- [ ] **Step 4: Run it and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerRefusesCrossOriginWrites|TestHandlerRoutesRefuseCrossOriginWrites' -count=1 .` and then `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .`

Expected: PASS, and the whole package green. The existing tests send no `Origin` and no `Sec-Fetch-Site`, so they pass through the guard.

- [ ] **Step 5: Commit**

```bash
git add http.go http_test.go
git commit -m "Refuse cross-origin browser requests that mark notifications read"
```

---

### Task 2: A `read-all` body must be declared as JSON

**Files:**
- Modify: `http.go:3-14` (imports), `http.go:283-305` (`markAllRead`)
- Test: `http_test.go:76-88` (`httpEnv.do`), and `TestHandlerMarkRead`'s case table (before its "no acting user is forbidden" case)

**Interfaces:**
- Consumes: `(*httpEnv).do(t, method, target, actor string, body io.Reader) *httptest.ResponseRecorder`.
- Produces: a `ValidationError{Subject: "request", Issues: []ValidationIssue{{Pointer: "/", Detail: "must be sent with Content-Type application/json"}}}`, answered `400 validation_failed`.

- [ ] **Step 1: Make the helper declare JSON, and write the failing cases (tasks 1.4)**

In `httpEnv.do`, after the actor header:

```go
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
```

In `TestHandlerMarkRead`, insert before the `"no acting user is forbidden"` case:

```go
		{
			name: "a body not declared as JSON is a bad request, and marks nothing",
			assert: func(t *testing.T, env *httpEnv) {
				env.publish(t, offer("alice", "a1"))

				for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
					req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/notifications/read-all",
						strings.NewReader(`{"through":"2999-01-01T00:00:00Z"}`))
					req.Header.Set(actorHeader, "alice")

					if contentType != "" {
						req.Header.Set("Content-Type", contentType)
					}

					rec := httptest.NewRecorder()
					env.handler.ServeHTTP(rec, req)

					assert.Equalf(t, http.StatusBadRequest, rec.Code, "%q: %s", contentType, rec.Body.String())
				}

				count, err := env.svc.CountActive(t.Context(), "alice")
				require.NoError(t, err)
				assert.EqualValues(t, 1, count, "a body that was not declared as JSON marked alice's notification read")
			},
		},
		{
			name: "a JSON body with a charset parameter is accepted",
			assert: func(t *testing.T, env *httpEnv) {
				env.publish(t, offer("alice", "a1"))

				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/notifications/read-all",
					strings.NewReader(`{"through":"2999-01-01T00:00:00Z"}`))
				req.Header.Set(actorHeader, "alice")
				req.Header.Set("Content-Type", "application/json; charset=utf-8")

				rec := httptest.NewRecorder()
				env.handler.ServeHTTP(rec, req)

				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				assert.JSONEq(t, `{"marked":1}`, rec.Body.String())
			},
		},
```

- [ ] **Step 2: Run it and watch it fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerMarkRead' -count=1 .`

Expected: FAIL only in `a_body_not_declared_as_JSON_is_a_bad_request,_and_marks_nothing`, with `expected: 400 actual: 200` four times. The first, with no header, marks the notification and answers `{"marked":1}`. The rest answer `{"marked":0}`. Then comes `a body that was not declared as JSON marked alice's notification read`. This was observed on 2026-09-28. Every other case, including the charset one, passes.

- [ ] **Step 3: Check the media type**

Add `"mime"` to `http.go`'s imports, between `"io"` and `"net/http"`. Replace `markAllRead`'s comment and its body-parsing block:

```go
// markAllRead answers POST /notifications/read-all. The optional body
// {"through": RFC 3339} bounds what is marked; without it, everything so far. A
// body must be declared as application/json: a browser sends any other type
// from a plain cross-site form, without asking the server first.
func (h *Handler) markAllRead(w http.ResponseWriter, r *http.Request, actor string) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		WriteError(w, &ValidationError{Subject: "request", Issues: []ValidationIssue{{Detail: "the body could not be read"}}})

		return
	}

	var body struct {
		Through time.Time `json:"through"`
	}

	if len(bytes.TrimSpace(raw)) > 0 {
		if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
			WriteError(w, &ValidationError{Subject: "request", Issues: []ValidationIssue{
				{Pointer: "/", Detail: "must be sent with Content-Type application/json"},
			}})

			return
		}

		if err := json.Unmarshal(raw, &body); err != nil {
			WriteError(w, &ValidationError{Subject: "request", Issues: []ValidationIssue{
				{Pointer: "/through", Detail: "must be an RFC 3339 instant in a JSON object"},
			}})

			return
		}
	}
```

The rest of the function, from `result, err := h.svc.MarkAllRead(...)` on, is unchanged.

- [ ] **Step 4: Run it and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .`

Expected: PASS. The existing `"an unparseable instant or body is a bad request"` case still answers `400`, now for its JSON, because `do` declares the type.

- [ ] **Step 5: Commit**

```bash
git add http.go http_test.go
git commit -m "Require a read-all body to be declared as JSON"
```

---

### Task 3: Let the host trust origins or opt out

**Files:**
- Modify: `http.go:45-51` (`handlerConfig`), after `WithSubscriptionAuthorizer` (new options), `NewHandler` (checks and construction)
- Test: `http_test.go`: `TestHandlerRefusesCrossOriginWrites` (two cases), `TestNewHandler` (two cases)

**Interfaces:**
- Consumes: `Handler.crossOrigin` from Task 1.
- Produces:
  - `func WithTrustedOrigins(origins ...string) HandlerOption`;
  - `func WithoutCrossOriginProtection() HandlerOption`;
  - the `handlerConfig` fields `trustedOrigins []string` and `withoutCrossOrigin bool`.

- [ ] **Step 1: Add the options as no-ops, with their final godoc (tasks 3.1)**

Add the fields to `handlerConfig`:

```go
	authorizer    SubscriptionAuthorizer
	authorizerSet bool
	// trustedOrigins are the origins WithTrustedOrigins permits.
	trustedOrigins []string
	// withoutCrossOrigin is set by WithoutCrossOriginProtection.
	withoutCrossOrigin bool
}
```

After `WithSubscriptionAuthorizer`, add:

```go
// WithTrustedOrigins permits state-changing requests from browser pages served
// by these origins, in addition to the request's own origin, which is the only
// one permitted by default. Each is a full origin of the form
// "scheme://host[:port]", such as "https://app.example.com", matched exactly;
// anything else is a [ConfigurationError] from [NewHandler]. It cannot be
// combined with [WithoutCrossOriginProtection].
func WithTrustedOrigins(origins ...string) HandlerOption {
	return func(c *handlerConfig) { c.trustedOrigins = append(c.trustedOrigins, origins...) }
}

// WithoutCrossOriginProtection serves state-changing requests from every
// browser origin. It is the explicit opt-out from the default, which refuses a
// cross-origin browser request to mark notifications read, for a host whose
// acting user never comes from ambient browser credentials such as a cookie,
// or that refuses cross-site requests in its own middleware. It is named so
// that accepting cross-site writes is never an accident.
func WithoutCrossOriginProtection() HandlerOption {
	return func(c *handlerConfig) { c.withoutCrossOrigin = true }
}
```

`NewHandler` does not read either field yet.

- [ ] **Step 2: Write the failing cases**

In `TestHandlerRefusesCrossOriginWrites`, add these cases after `"a sibling site the host does not trust is refused"`:

```go
		{
			name:        "a sibling site the host trusts is served",
			handlerOpts: []ntfy.HandlerOption{ntfy.WithTrustedOrigins("https://web.example.com")},
			method:      http.MethodPost, target: readAll, header: sibling, assert: served,
		},
		{
			name:        "every origin is served after the explicit opt-out",
			handlerOpts: []ntfy.HandlerOption{ntfy.WithoutCrossOriginProtection()},
			method:      http.MethodPost, target: readOne, header: crossSite, assert: served,
		},
```

In `TestNewHandler`, add these cases before `"a nil service is refused"`:

```go
		{
			name: "a trusted origin that is not an origin is refused", svc: svc, hub: hub,
			opts:   []ntfy.HandlerOption{ntfy.WithActor(headerActor), ntfy.WithTrustedOrigins("app.example.com")},
			assert: refused,
		},
		{
			name: "trusted origins with the cross-origin opt-out are refused", svc: svc, hub: hub,
			opts: []ntfy.HandlerOption{
				ntfy.WithActor(headerActor), ntfy.WithTrustedOrigins("https://app.example.com"),
				ntfy.WithoutCrossOriginProtection(),
			},
			assert: refused,
		},
```

- [ ] **Step 3: Run them and watch them fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerRefusesCrossOriginWrites|TestNewHandler' -count=1 .`

Expected: FAIL in exactly four cases:
- `…/a_sibling_site_the_host_trusts_is_served` and `…/every_origin_is_served_after_the_explicit_opt-out`: `Error: Not equal: expected: 200 actual: 403`, because the options are ignored;
- `TestNewHandler/a_trusted_origin_that_is_not_an_origin_is_refused` and `TestNewHandler/trusted_origins_with_the_cross-origin_opt-out_are_refused`: `An error is expected but got nil.`

- [ ] **Step 4: Implement both options**

In `NewHandler`, add a case to the `switch`, after the nil-policy case:

```go
	case cfg.withoutCrossOrigin && len(cfg.trustedOrigins) > 0:
		return nil, &ConfigurationError{
			Detail: "trusted origins were given with WithoutCrossOriginProtection; " +
				"choose WithTrustedOrigins or WithoutCrossOriginProtection",
		}
```

Then replace the Task 1 construction:

```go
	h := &Handler{
		svc: svc, hub: hub, actor: cfg.actor, authorizer: cfg.authorizer,
		basePath: cfg.basePath, mux: http.NewServeMux(),
		crossOrigin: http.NewCrossOriginProtection(),
	}
```

with

```go
	h := &Handler{
		svc: svc, hub: hub, actor: cfg.actor, authorizer: cfg.authorizer,
		basePath: cfg.basePath, mux: http.NewServeMux(),
	}

	if !cfg.withoutCrossOrigin {
		h.crossOrigin = http.NewCrossOriginProtection()

		for _, origin := range cfg.trustedOrigins {
			if err := h.crossOrigin.AddTrustedOrigin(origin); err != nil {
				return nil, &ConfigurationError{
					Detail: fmt.Sprintf("the trusted origin %q is not of the form scheme://host[:port]", origin),
				}
			}
		}
	}
```

Replace the `NewHandler` godoc paragraph from Task 1 with:

```go
// With no options beyond the required [WithActor] it serves under
// [DefaultBasePath], authorizes streams with [SelfOnly], and refuses a
// cross-origin browser request to mark notifications read. A nil service or
// hub, a missing actor, a nil subscription policy, a trusted origin that is
// not an origin, or trusted origins combined with
// [WithoutCrossOriginProtection] is a [ConfigurationError].
```

- [ ] **Step 5: Run and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add http.go http_test.go
git commit -m "Let a host trust origins or opt out of cross-origin protection"
```

---

### Task 4: Count stream caps per acting user

**Files:**
- Modify: `hub.go` (the constant, `Hub`, the option, `NewHub`, `closeSubscriptions`, `Subscribe`, `Subscription`, `Close`, `closeLocked`), `errors.go:22-24`, `http.go:352`, `websocket/handler.go:196-208, 248`, `websocket/doc.go:8`, `docs/notifications.md:165`, `docs/realtime-operations.md:106, 145`
- Test: `http_test.go` (append), `websocket/refusal_test.go` (one case), `hub_test.go` (append, and mechanical updates), plus `docs_test.go`, `websocket/{docs,handler,shutdown,writeloop}_test.go` (renamed by `gopls`)

**Interfaces:**
- Consumes:
  - `openStream(t *testing.T, server *httptest.Server, actor, query string) *stream` (`http_test.go`), which does not require `200`;
  - `runHub(t *testing.T, hub *ntfy.Hub) (stop func() error)` (`hub_test.go`);
  - in `websocket_test`: `startServer(t, serverConfig) *server`, `(*server).dial(t, dialRequest) (dialed, error)`, `upgraded`, and `supervisorPolicy` (`"sup"` may follow anyone).
- Produces:
  - `func (h *Hub) Subscribe(actor, recipient string) (*Subscription, error)`;
  - `const DefaultMaxStreamsPerActor = 8`;
  - `func WithMaxStreamsPerActor(n int) HubOption`;
  - the refusal message `"%d streams are already open for this user"`.

- [ ] **Step 1: Land the HTTP proof (tasks 1.2)**

Append to `http_test.go`. It uses the pre-rename option name, which `gopls` rewrites in Step 5.

```go
// TestHandlerStreamCapsCountTheActingUser is the 2026-09-28 audit's proof: when
// a host's policy lets one user follow another, a follower must not lock the
// recipient out of their own stream, nor take the whole instance cap alone.
func TestHandlerStreamCapsCountTheActingUser(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		hubOpts []ntfy.HubOption
		// followed are the recipients mallory opens eight streams each for,
		// taking as many as the hub gives her.
		followed []string
		// victim then opens their own stream.
		victim string
		assert func(t *testing.T, rec *httptest.ResponseRecorder)
	}

	served := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()

		assert.Equal(t, http.StatusOK, rec.Code,
			"the victim could not open their own stream because mallory holds the slots: %s", rec.Body.String())
	}

	cases := []testCase{
		{
			name:     "a follower takes every slot of the recipient's own cap",
			followed: []string{"alice"},
			victim:   "alice",
			assert:   served,
		},
		{
			name: "one acting user takes the whole instance cap",
			hubOpts: []ntfy.HubOption{
				ntfy.WithMaxStreamsPerRecipient(8), ntfy.WithMaxStreamsPerInstance(16),
			},
			followed: []string{"alice", "bob"},
			victim:   "carol",
			assert:   served,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := newHTTPEnv(t, true, tc.hubOpts, ntfy.WithSubscriptionAuthorizer(ntfy.AllowAll))
			server := httptest.NewServer(env.handler)
			t.Cleanup(server.Close)

			for _, recipient := range tc.followed {
				for range 8 {
					openStream(t, server, "mallory", "?recipient="+recipient)
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()

			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/notifications/stream", http.NoBody)
			req.Header.Set(actorHeader, tc.victim)

			rec := httptest.NewRecorder()
			env.handler.ServeHTTP(rec, req)

			tc.assert(t, rec)
		})
	}
}
```

- [ ] **Step 2: Land the WebSocket proof (tasks 1.3)**

In `websocket/refusal_test.go`, `TestHandshakeRefusals`, insert before the `"a hub that is not receiving signals is unavailable"` case:

```go
		{
			name:    "a supervisor's connections leave the followed recipient's own cap untouched",
			server:  serverConfig{ws: []websocket.Option{websocket.WithSubscriptionAuthorizer(supervisorPolicy)}},
			request: dialRequest{actor: "bob"},
			before: func(t *testing.T, s *server) {
				for range ntfy.DefaultMaxStreamsPerRecipient {
					_, err := s.dial(t, dialRequest{actor: "sup", recipient: "bob"})
					require.NoError(t, err)
				}
			},
			assert: upgraded,
		},
```

- [ ] **Step 3: Run both and watch them fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerStreamCapsCountTheActingUser' -count=1 .`

Expected, as observed on `main` `20967b3`, 2026-09-28:

```
--- FAIL: TestHandlerStreamCapsCountTheActingUser/a_follower_takes_every_slot_of_the_recipient's_own_cap
        expected: 200  actual  : 429
        Messages: the victim could not open their own stream because mallory holds the slots:
                  {"error":{"code":"too_many_streams","message":"ntfy: too many streams: 8 streams are already open for this recipient"}}
--- FAIL: TestHandlerStreamCapsCountTheActingUser/one_acting_user_takes_the_whole_instance_cap
        expected: 200  actual  : 429
        Messages: … "16 streams are already open on this instance"
```

Run: `cd websocket && GOTOOLCHAIN=go1.26.8 go test -run 'TestHandshakeRefusals' -count=1 .`

Expected:

```
--- FAIL: TestHandshakeRefusals/a_supervisor's_connections_leave_the_followed_recipient's_own_cap_untouched
        Error: Received unexpected error:
               failed to WebSocket dial: expected handshake response status code 101 but got 429
```

**STOP** if either passes on unmodified code (see Task 1, Step 2).

- [ ] **Step 4: Rename the cap with gopls (tasks 4.1)**

```bash
export GOTOOLCHAIN=go1.26.8
gopls rename -w hub.go:22:2 DefaultMaxStreamsPerActor
gopls rename -w hub.go:91:6 WithMaxStreamsPerActor
git status --short   # hub.go, hub_test.go, http_test.go, docs_test.go, websocket/{docs,handler,shutdown,writeloop,refusal}_test.go
```

The positions are `DefaultMaxStreamsPerRecipient = 8` at `hub.go:22:2` and `func WithMaxStreamsPerRecipient` at `hub.go:91:6`. Confirm them with `sed -n '22p;91p' hub.go` first, because an earlier edit to `hub.go` would move them.

- [ ] **Step 5: Give `Subscribe` the actor, still counting by recipient**

In `hub.go`:

```go
func (h *Hub) Subscribe(actor, recipient string) (*Subscription, error) {
```

In `http.go` `stream` and in `websocket/handler.go` `ServeHTTP`:

```go
	subscription, err := h.hub.Subscribe(actor, recipient)
```

Rewrite the test call sites mechanically. Every existing test subscribes a user to their own signals:

```bash
perl -pi -e 's/\.Subscribe\(("[^"]*")\)/.Subscribe($1, $1)/g' hub_test.go http_test.go
perl -pi -e 's/^(\s*)_, err := hub\.Subscribe\("recipient-" \+ strconv\.Itoa\(i\)\)/$1recipient := "recipient-" + strconv.Itoa(i)\n$1_, err := hub.Subscribe(recipient, recipient)/' hub_test.go
grep -n 'Subscribe(' hub_test.go http_test.go | grep -v ', ' # prints only "func TestHubSubscribe"
```

Run: `GOTOOLCHAIN=go1.26.8 go build ./... && GOTOOLCHAIN=go1.26.8 go vet . && (cd websocket && GOTOOLCHAIN=go1.26.8 go vet ./...)`. Expected: no output.

Run: `GOTOOLCHAIN=go1.26.8 go test -count=1 . 2>&1 | grep -E '^--- FAIL'`. Expected: exactly `TestHandlerStreamCapsCountTheActingUser` and `TestTheDocumentMatchesTheImplementation`. The doc still names `WithMaxStreamsPerRecipient`, and Step 10 fixes it.

- [ ] **Step 6: Write the failing hub test (tasks 4.2)**

Append to `hub_test.go`:

```go
// TestHubCapsCountTheActingUser proves the per-user cap is keyed on the user
// who opens a stream, not on the recipient the stream follows: a follower
// cannot use up the recipient's own streams, and cannot hold more than the cap
// however many recipients they follow.
func TestHubCapsCountTheActingUser(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		assert func(t *testing.T, hub *ntfy.Hub)
	}

	cases := []testCase{
		{
			name: "a follower's streams leave the recipient's own cap untouched",
			assert: func(t *testing.T, hub *ntfy.Hub) {
				for range 2 {
					_, err := hub.Subscribe("mallory", "alice")
					require.NoError(t, err)
				}

				for range 2 {
					_, err := hub.Subscribe("alice", "alice")
					assert.NoError(t, err, "alice's own stream was refused because mallory follows her")
				}
			},
		},
		{
			name: "a follower is capped across every recipient they follow",
			assert: func(t *testing.T, hub *ntfy.Hub) {
				_, err := hub.Subscribe("mallory", "alice")
				require.NoError(t, err)

				_, err = hub.Subscribe("mallory", "bob")
				require.NoError(t, err)

				_, err = hub.Subscribe("mallory", "carol")
				require.ErrorIs(t, err, ntfy.ErrTooManyStreams, "mallory holds more than the cap by following many recipients")
				assert.Contains(t, err.Error(), "2 streams are already open for this user")
			},
		},
		{
			name: "closing a follower's stream frees the follower's slot",
			assert: func(t *testing.T, hub *ntfy.Hub) {
				first, err := hub.Subscribe("mallory", "alice")
				require.NoError(t, err)

				_, err = hub.Subscribe("mallory", "bob")
				require.NoError(t, err)

				first.Close()

				_, err = hub.Subscribe("mallory", "carol")
				assert.NoError(t, err, "the closed stream's slot is free again")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hub, err := ntfy.NewHub(ntfy.NewInProcessBroadcaster(), ntfy.WithMaxStreamsPerActor(2))
			require.NoError(t, err)

			runHub(t, hub)

			tc.assert(t, hub)
		})
	}
}
```

- [ ] **Step 7: Run it and watch it fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestHubCapsCountTheActingUser' -count=1 .`

Expected, as observed in a trial run on 2026-09-28:

```
--- FAIL: TestHubCapsCountTheActingUser/a_follower's_streams_leave_the_recipient's_own_cap_untouched
        Error: Received unexpected error:
               ntfy: too many streams: 2 streams are already open for this recipient
        Messages: alice's own stream was refused because mallory follows her
--- FAIL: TestHubCapsCountTheActingUser/a_follower_is_capped_across_every_recipient_they_follow
        Error: Expected error with "ntfy: too many streams" in chain but got nil.
```

The third case passes already. It is the guard for the decrement in Step 8.

- [ ] **Step 8: Count per actor (tasks 4.3)**

In `hub.go`, make these edits.

The constant:

```go
	// DefaultMaxStreamsPerActor is how many streams one acting user may hold
	// open on one instance, across every transport and whichever recipients
	// they follow.
	DefaultMaxStreamsPerActor = 8
```

The `Hub` godoc's first paragraph:

```go
// Hub routes signals from a [Broadcaster] to the subscriptions of the recipient
// each signal is for. Every transport, SSE and WebSocket alike, subscribes
// through it, so one per-user cap counts all the streams an acting user opens,
// whoever they follow, and one instance-wide cap counts everyone's together.
```

The `Hub` fields, after `subscriptions`:

```go
	subscriptions map[string]map[*Subscription]struct{}
	// opened counts each acting user's open subscriptions, which is what the
	// per-user cap limits.
	opened map[string]int
}
```

The option's godoc:

```go
// WithMaxStreamsPerActor replaces [DefaultMaxStreamsPerActor], the number of
// streams one acting user may hold open on one instance. It counts the streams
// the user opens, whichever recipient each follows, so a follower never uses up
// the recipient's own streams. It must be at least one.
```

In the godoc of `WithMaxStreamsPerInstance`, `WithoutMaxStreamsPerInstance` and `NewHub`, and in the two configuration error details, replace `per-recipient` with `per-user`, and `"a hub must allow at least one stream per recipient"` with `"a hub must allow at least one stream per user"`:

```bash
perl -pi -e 's/per-recipient/per-user/g; s/at least one stream per recipient/at least one stream per user/' hub.go
```

In `NewHub`'s literal, add `opened: make(map[string]int),` after `subscriptions: ...`. In `closeSubscriptions`, add `h.opened = make(map[string]int)` after `h.subscriptions = ...`.

Replace `Subscribe`'s godoc and the recipient-cap check:

```go
// Subscribe opens a subscription for an acting user to a recipient's signals.
// The actor is who opens the stream and the recipient is whose signals it
// follows; they are the same unless a [SubscriptionAuthorizer] let the actor
// follow someone else.
//
// It does not authorize: a transport asks its [SubscriptionAuthorizer] first.
// A hub that is not receiving signals refuses with an error matching
// [ErrUnavailable]. A stream beyond the actor's per-user cap, which counts every
// stream the actor holds whoever it follows, or beyond the instance-wide cap,
// is refused with an error matching [ErrTooManyStreams], whose message says
// which. Every subscription must be closed, and a subscription is closed for the
// caller when the hub's run ends.
func (h *Hub) Subscribe(actor, recipient string) (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.receiving {
		return nil, fmt.Errorf("%w: the hub is not receiving signals", ErrUnavailable)
	}

	if h.maxTotal > 0 && h.streams >= h.maxTotal {
		return nil, fmt.Errorf("%w: %d streams are already open on this instance", ErrTooManyStreams, h.maxTotal)
	}

	if h.opened[actor] >= h.maxStreams {
		return nil, fmt.Errorf("%w: %d streams are already open for this user", ErrTooManyStreams, h.maxStreams)
	}

	held := h.subscriptions[recipient]
	if held == nil {
		held = make(map[*Subscription]struct{})
		h.subscriptions[recipient] = held
	}

	subscription := &Subscription{
		hub:       h,
		actor:     actor,
		recipient: recipient,
		ready:     make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	held[subscription] = struct{}{}
	h.opened[actor]++
	h.streams++

	return subscription, nil
}
```

Add `actor string` to `Subscription`, between `hub` and `recipient`. Change `Close`'s godoc first sentence to `// Close ends the subscription and frees its slot in its actor's cap.`. In `closeLocked`, after `s.hub.streams--`:

```go
		s.hub.opened[s.actor]--
		if s.hub.opened[s.actor] == 0 {
			delete(s.hub.opened, s.actor)
		}
```

It is two statements rather than an `if` with an init, because `gocritic`'s `initClause` rejects the init form.

In `errors.go`:

```go
	// ErrTooManyStreams reports a stream refused by a cap: an acting user
	// already holding the per-user cap on this instance, or an instance already
	// holding its total. The message says which.
```

In `hub_test.go`, the comments and case names from `TestHubInstanceCapConfiguration` down say `per-recipient`:

```bash
perl -pi -e 's/per-recipient/per-user/g' hub_test.go
```

- [ ] **Step 9: Run and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -race -run 'TestHubCapsCountTheActingUser|TestHandlerStreamCapsCountTheActingUser|TestHub|TestHandlerStream' -count=1 .` and `cd websocket && GOTOOLCHAIN=go1.26.8 go test -race -count=1 ./...`

Expected: PASS. Under `SelfOnly`, every existing cap test has actor = recipient, so none changes.

- [ ] **Step 10: Rename the cap in the docs (tasks 4.4)**

```bash
perl -pi -e 's/^\| Streams per recipient on one instance \| 8, counted across SSE and WebSocket \| `WithMaxStreamsPerRecipient` \|$/| Streams per acting user on one instance | 8, counted across SSE and WebSocket, whoever the user follows | `WithMaxStreamsPerActor` |/' docs/notifications.md
perl -pi -e 's/^per-recipient cap of 8 connections per instance and the same instance-wide cap$/per-user cap of 8 connections per instance and the same instance-wide cap/; s/^\| Streams per recipient \| 8 per instance \| `ntfy.WithMaxStreamsPerRecipient` \|$/| Streams per acting user | 8 per instance, whoever the user follows | `ntfy.WithMaxStreamsPerActor` |/' docs/realtime-operations.md
perl -pi -e 's/so one per-recipient cap counts/so one per-user cap counts/' websocket/doc.go
perl -pi -e 's/signals is unavailable, and a recipient over the hub.s cap is too many/signals is unavailable, and an acting user over either of the hub'"'"'s caps is too many/' websocket/handler.go
grep -rn 'MaxStreamsPerRecipient\|per-recipient cap' docs websocket/*.go *.go   # prints nothing
```

Run: `GOTOOLCHAIN=go1.26.8 go test -race -count=1 .` and `cd websocket && GOTOOLCHAIN=go1.26.8 go test -count=1 ./...`

Expected: both green. `TestTheOperationsGuideMatchesTheHandler` still finds `8 connections per instance`.

- [ ] **Step 11: Lint and commit**

Run: `GOTOOLCHAIN=go1.26.8 golangci-lint run ./... && (cd websocket && GOTOOLCHAIN=go1.26.8 golangci-lint run ./...)`. Expected: `0 issues.` for each, as observed in the 2026-09-28 trial.

```bash
git add hub.go hub_test.go http.go http_test.go errors.go docs_test.go docs/notifications.md docs/realtime-operations.md websocket/
git commit -m "Count stream caps against the acting user, not the followed recipient"
```

---

### Task 5: Document the HTTP defaults

**Files:**
- Modify: `docs/notifications.md` (the Realtime bullets, the HTTP and mounting bullets, the error table), `docs_test.go:113-121` (the HTTP section's needles)

**Interfaces:**
- Consumes: `WithTrustedOrigins` and `WithoutCrossOriginProtection` (Task 3); `funcName(fn any) string` and `docSection(t, document, heading string) string` (`docs_test.go`).
- Produces: nothing new in code.

- [ ] **Step 1: Write the failing needles (tasks 5.1)**

In `docs_test.go`, case `"the HTTP contract and its mounting"`, extend `needles`:

```go
			needles: []string{
				ntfy.DefaultBasePath, strconv.Itoa(ntfy.DefaultListLimit), strconv.Itoa(ntfy.MaxListLimit),
				funcName(ntfy.NewHandler), funcName(ntfy.WithActor), funcName(ntfy.WithBasePath),
				funcName(ntfy.WithSubscriptionAuthorizer), funcName(ntfy.WriteError),
				funcName(ntfy.WithTrustedOrigins), funcName(ntfy.WithoutCrossOriginProtection),
				"Content-Type: application/json", "Sec-Fetch-Site",
				"gin.WrapH", "adaptor.HTTPHandler", "X-Accel-Buffering",
			},
```

In case `"realtime defaults, policies and limits"`, add `"whoever the user follows"` to its `needles`.

- [ ] **Step 2: Run it and watch it fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .`

Expected: FAIL in `the_HTTP_contract_and_its_mounting`, for `WithTrustedOrigins`, `WithoutCrossOriginProtection`, `Content-Type: application/json` and `Sec-Fetch-Site`. `realtime defaults…` passes, because Task 4 put the phrase in the table row.

- [ ] **Step 3: Write the docs**

In `docs/notifications.md`, Realtime bullets, after the `**A slow client never slows a publisher.**` bullet:

```markdown
- **A follower pays for their own streams.** The per-user cap counts every
  stream an acting user opens, whoever they follow. A user whom a host policy
  lets follow others can never use up the followed recipient's own streams, nor
  more than the per-user cap of the instance.
```

In "HTTP and mounting", after the bullet that begins ``- The stream's policy defaults to `SelfOnly`.``:

```markdown
- **Cross-origin browser writes are refused.** A `POST` from a page on another
  origin, detected by the browser's `Sec-Fetch-Site` header or by comparing
  `Origin` with `Host`, is answered `403` before `WithActor` runs. So another
  site cannot use a cookie session to mark notifications read. Same-origin pages,
  non-browser clients and every `GET` are served. `WithTrustedOrigins` permits
  named origins, each written `scheme://host[:port]`, such as a front end on
  another subdomain; behind a proxy that rewrites `Host`, trust the public origin.
  `WithoutCrossOriginProtection` turns the check off, for a host that checks this
  itself. Combining the two is a configuration error.
- A `read-all` body must be sent with `Content-Type: application/json`; any other
  type, or none, is `400`. A request with no body needs no header.
```

In the error table, replace two rows:

```markdown
| no acting user, a refused subscription, or a cross-origin browser write | 403 | `forbidden` |
```

```markdown
| too many streams for one user, or for the instance | 429 | `too_many_streams` |
```

- [ ] **Step 4: Run and watch it pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -count=1 .`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/notifications.md docs_test.go
git commit -m "Document cross-origin protection, JSON bodies and the per-user stream cap"
```

---

### Task 6: Verify and hand off

**Files:** none changed, except `plans.md`'s execution record and `tasks.md` checkboxes.

**Interfaces:** none.

- [ ] **Step 1: Run the gates (tasks 6.1)**

```bash
make all
make store-matrix
grep -rn 'MaxStreamsPerRecipient\|per-recipient cap\|for this recipient' --include='*.go' --include='*.md' . | grep -v 'openspec/'
```

Expected: `make all` and `make store-matrix` pass. The grep prints nothing. `openspec/specs/` still says per-recipient until `/opsx:archive` syncs the delta.

- [ ] **Step 2: Record the evidence (tasks 1.5)**

Append an "Execution record" section to this file. It names each red test with its failing line: Task 1 Step 2, Task 2 Step 2, Task 3 Step 3, Task 4 Steps 3 and 7. It also gives the `make all` and `make store-matrix` results and the date. Tick `tasks.md`.

- [ ] **Step 3: Commit**

```bash
git add openspec/changes/harden-http-read-state-and-stream-caps/
git commit -m "Record the execution of harden-http-read-state-and-stream-caps"
```

Then go on with `development-workflow.md` steps 5–7: `/code-review`, then `/opsx:archive harden-http-read-state-and-stream-caps`, then the PR.
