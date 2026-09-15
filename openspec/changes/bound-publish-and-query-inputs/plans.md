# Bound Publish and Query Inputs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound every caller-supplied input — a draft's title, links and payload, the number of drafts in one publish, and the number of values in one list filter — with documented, replaceable limits, and refuse a query the server cannot parse instead of serving it unfiltered.

**Architecture:** A new `Limits` value carries the bounds. `Draft.Validate`, `ListQuery.Validate` and `CloseRequest.Validate` keep working against the package defaults; each gains a `ValidateWithin(Limits)` form that the `Service` calls with its configured set. Limits reach the service through one option, validated at construction. The HTTP listing handler parses `r.URL.RawQuery` itself so a query the standard library rejects is answered `400` rather than served with its filters silently dropped.

**Tech Stack:** Go 1.26, standard library only in the core module (`net/url` and `strings` are the two new imports), `stretchr/testify` and `uber-go/mock` in tests.

**Spec:** `openspec/changes/bound-publish-and-query-inputs/` — read `proposal.md` (why), `design.md` (D1–D6, the decisions this plan implements), `specs/notification-inbox/spec.md` and `specs/notification-http-api/spec.md` (the requirements every task is checked against), and `tasks.md` (the checklist this plan expands).

## Global Constraints

- Go 1.26; every Go command runs under `GOTOOLCHAIN=go1.26.8` (the repo pins it in the `Makefile` and in `.envrc`; prefix commands explicitly if direnv is not installed).
- The core module imports the standard library only — enforced by `depguard` in `.golangci.yml` and by `make split-check`. No new dependency in `ntfy` itself.
- Table-driven tests follow the project `table-test` skill: a `testCase` struct carrying an `assert` closure (never `want`/`wantErr` fields), `t.Parallel()` on the function and each case, and `t.Context()` rather than `context.Background()`.
- Test doubles come from `use-mockgen` (`ntfy.NewMockStore`, `ntfy.NewMockBroadcaster`, already generated); do not hand-roll fakes.
- `make all` (lint, split-check, test on every module) must pass before the change is done.
- `.claude/rules/prove-errors-with-tests.md`: every claimed defect is proved by a test that is **seen to fail** for its own reason before the fix. A test that fails to compile has not proved anything.
- `.claude/rules/library-design.md`: every default names the option that replaces it in its godoc; a contradictory or meaningless configuration is a `ConfigurationError` from the constructor, before traffic; every limit is stated in the docs, never silently relaxed; tests cover the default **and** at least one consumer override.
- Commit messages are imperative sentence case with no `feat:`/`fix:` prefix and no attribution lines, matching `git log`.

## Mapping to `tasks.md`

`tasks.md` lists the red tests first as a group. This plan interleaves each red test with the fix it proves, so that every task ends with an independently testable deliverable, which is what the writing-plans skill requires. Nothing is dropped:

| `tasks.md` | Plan task |
| --- | --- |
| 2.1, 2.2, 2.3 | Task 1 — the limits exist and are validated at construction |
| 1.1, 3.1 | Task 2 — content limits, red first |
| 1.2, 3.2, 5.2 | Task 3 — link scheme check, red first |
| 1.4, 3.3 | Task 4 — the publish path, red first |
| 3.4 | Task 5 — close successors |
| 1.3, 4.1 | Task 6 — the list filter bound, red first |
| 1.5, 4.2, 4.3 | Task 7 — the listing handler |
| 5.1 | Tasks 2, 4 and 6 (one consumer override beside each limit it belongs to) |
| 6.1, 6.2, 6.3, 6.4 | Task 8 — docs and final verification |

Task 1 comes first for a reason the rules demand: the red tests in Tasks 2, 3, 4 and 6 reference `ntfy.DefaultMaxTitleBytes` and friends. If those constants do not exist, the red tests fail to compile, and a test that fails to compile proves nothing.

## File Structure

**Created**

- `limits.go` — the `Limits` value, the `Default*` constants it defaults to, `Limit`, `DefaultLimits`, `DefaultLinkSchemes`, the unexported accessors that resolve an unset field, and `Limits.validate`. One file because limits are one responsibility; the repo already splits this way (`clock.go`, `id.go`, `codec.go`, `authorize.go`).
- `limits_test.go` — the defaults resolve as documented, and `validate` refuses what it should.

**Modified**

- `notification.go` — `content` gains `title` and `links`; `validateContent` takes a `Limits`; `Draft.ValidateWithin`; `validateLinks`, `checkScheme` and `escapePointer`; the constant block's comment at lines 37–39 corrected.
- `store.go` — `ListQuery.ValidateWithin`, `CloseRequest.ValidateWithin`.
- `service.go` — `Service.limits`, `WithLimits`, validation in `New`, the draft cap in `Publish`, and `ValidateWithin` at the `Publish`, `Close` and `List` call sites.
- `http.go` — `list` parses `r.URL.RawQuery`.
- `notification_test.go` — the `issue` helper moves to file scope; content-limit and link-scheme cases.
- `cursor_test.go` — filter-bound cases beside the existing `TestListQueryValidate`.
- `service_test.go` — construction cases for meaningless and contradictory limits.
- `service_publish_test.go` — the draft cap, and the raised-limit override.
- `service_ops_test.go` — an oversized successor is refused.
- `http_test.go` — the unparseable query, the oversized filter, and the fail-closed stream case.
- `docs/notifications.md` — the defaults-and-overrides table and the stated-limits section.

**Not modified, deliberately:** `sqlstore/read.go`. Per `design.md` D5 the filter bound keeps the `IN` list inside `chunkSize`, so the store needs no chunking change. A keyset-paged listing cannot split its filter across statements.

---

### Task 1: The `Limits` value, its defaults, and construction-time validation

**Files:**
- Create: `limits.go`
- Create: `limits_test.go`
- Modify: `service.go:17-23` (the `Service` struct), `service.go:63-94` (options and `New`)
- Modify: `service_test.go` (new cases in `TestNew`)

**Interfaces:**
- Consumes: nothing.
- Produces — every later task depends on these exact names:
  ```go
  type Limits struct {
      MaxTitleBytes        *int
      MaxDataBytes         *int
      MaxLinks             *int
      MaxLinkRelationBytes *int
      MaxLinkHrefBytes     *int
      MaxDraftsPerPublish  *int
      MaxFilterValues      *int
      LinkSchemes          []string
      AnyLinkScheme        bool
  }

  func Limit(n int) *int
  func DefaultLimits() Limits
  func DefaultLinkSchemes() []string
  func WithLimits(limits Limits) Option

  // unexported, used by the validation paths
  func (l Limits) maxTitleBytes() int
  func (l Limits) maxDataBytes() int
  func (l Limits) maxLinks() int
  func (l Limits) maxLinkRelationBytes() int
  func (l Limits) maxLinkHrefBytes() int
  func (l Limits) maxDraftsPerPublish() int
  func (l Limits) maxFilterValues() int
  func (l Limits) linkSchemes() []string
  func (l Limits) validate() error
  ```
  Constants: `DefaultMaxTitleBytes`, `DefaultMaxDataBytes`, `DefaultMaxLinks`, `DefaultMaxLinkRelationBytes`, `DefaultMaxLinkHrefBytes`, `DefaultMaxDraftsPerPublish`, `DefaultMaxFilterValues`.

Pointer fields are deliberate and match `emailConfig` (`email_dispatcher.go:72-74`): "an explicit zero is a wiring mistake to report, while an absent value is the default". The spec requires a configured limit of **zero** to be a configuration error, which a plain `int` cannot express.

- [ ] **Step 1: Write the failing test for the defaults**

Create `limits_test.go`:

```go
package ntfy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

func TestDefaultLimits(t *testing.T) {
	t.Parallel()

	limits := ntfy.DefaultLimits()

	require.NotNil(t, limits.MaxTitleBytes)
	assert.Equal(t, 1024, *limits.MaxTitleBytes)
	require.NotNil(t, limits.MaxDataBytes)
	assert.Equal(t, 65536, *limits.MaxDataBytes)
	require.NotNil(t, limits.MaxLinks)
	assert.Equal(t, 16, *limits.MaxLinks)
	require.NotNil(t, limits.MaxLinkRelationBytes)
	assert.Equal(t, ntfy.MaxKindBytes, *limits.MaxLinkRelationBytes)
	require.NotNil(t, limits.MaxLinkHrefBytes)
	assert.Equal(t, 2048, *limits.MaxLinkHrefBytes)
	require.NotNil(t, limits.MaxDraftsPerPublish)
	assert.Equal(t, 1000, *limits.MaxDraftsPerPublish)
	require.NotNil(t, limits.MaxFilterValues)
	assert.Equal(t, 100, *limits.MaxFilterValues)
	assert.Equal(t, []string{"http", "https"}, limits.LinkSchemes)
}

func TestDefaultLinkSchemesIsACopy(t *testing.T) {
	t.Parallel()

	schemes := ntfy.DefaultLinkSchemes()
	schemes[0] = "javascript"

	assert.Equal(t, []string{"http", "https"}, ntfy.DefaultLinkSchemes(), "a caller cannot change the defaults")
}
```

- [ ] **Step 2: Run the test to verify it fails**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestDefaultLimits|TestDefaultLinkSchemesIsACopy' -count=1 ./...
```

Expected: FAIL — `undefined: ntfy.DefaultLimits`. This is the one place a compile failure is the correct red signal, because the test is specifying new API rather than proving a defect.

- [ ] **Step 3: Write `limits.go`**

```go
// The bounds a draft and a list query are validated against, and the value
// that carries them.

package ntfy

import "slices"

// The default limits a service validates against. Each is the value the
// matching [Limits] field takes when a host leaves it unset.
const (
	// DefaultMaxTitleBytes is the longest title a draft may carry. A title is a
	// line a client shows, so a kilobyte is far more than one.
	DefaultMaxTitleBytes = 1 << 10
	// DefaultMaxDataBytes is the largest payload a draft may carry. It is the
	// size of the largest request body the HTTP contract reads, so one
	// notification's payload never outweighs a whole request.
	DefaultMaxDataBytes = 1 << 16
	// DefaultMaxLinks is the most links one draft may carry.
	DefaultMaxLinks = 16
	// DefaultMaxLinkRelationBytes is the longest link relation name. A relation
	// name is a publisher's classification, exactly like a kind, so it shares
	// the kind's limit.
	DefaultMaxLinkRelationBytes = MaxKindBytes
	// DefaultMaxLinkHrefBytes is the longest link href. Beyond it a link stops
	// surviving the intermediaries that have to carry it.
	DefaultMaxLinkHrefBytes = 2 << 10
	// DefaultMaxDraftsPerPublish is the most drafts one publish may carry,
	// because the drafts of one subject are written in a single transaction.
	DefaultMaxDraftsPerPublish = 1000
	// DefaultMaxFilterValues is the most values one list filter may carry. It
	// is the number of values the SQL store binds in one IN list.
	DefaultMaxFilterValues = 100
)

// Limits are the bounds a [Service] validates drafts, closes and list queries
// against. The zero value means every default; a field a host sets replaces
// that one default and leaves the rest alone.
//
// A service is configured with [WithLimits]. Set a field with [Limit]:
//
//	svc, err := ntfy.New(store, ntfy.WithLimits(ntfy.Limits{
//	    MaxDataBytes: ntfy.Limit(1 << 20),
//	}))
//
// A limit that is not positive, or naming link schemes while also accepting
// any scheme, is a [ConfigurationError] from [New].
type Limits struct {
	// MaxTitleBytes is the longest title. Unset means [DefaultMaxTitleBytes].
	MaxTitleBytes *int
	// MaxDataBytes is the largest payload. Unset means [DefaultMaxDataBytes].
	MaxDataBytes *int
	// MaxLinks is the most links one draft carries. Unset means
	// [DefaultMaxLinks].
	MaxLinks *int
	// MaxLinkRelationBytes is the longest relation name. Unset means
	// [DefaultMaxLinkRelationBytes].
	MaxLinkRelationBytes *int
	// MaxLinkHrefBytes is the longest href. Unset means
	// [DefaultMaxLinkHrefBytes].
	MaxLinkHrefBytes *int
	// MaxDraftsPerPublish is the most drafts one publish carries. Unset means
	// [DefaultMaxDraftsPerPublish].
	MaxDraftsPerPublish *int
	// MaxFilterValues is the most values one list filter carries. Unset means
	// [DefaultMaxFilterValues]. Raising it raises the number of values the
	// store binds in one IN list.
	MaxFilterValues *int
	// LinkSchemes are the schemes a link href may use. A relative reference has
	// no scheme and is always permitted. Nil means [DefaultLinkSchemes]. It
	// cannot be combined with AnyLinkScheme.
	LinkSchemes []string
	// AnyLinkScheme stores an href whatever its scheme, including one whose
	// only effect is to run code in a client that follows it. It is the named
	// opt-out from the scheme check, for a host that checks hrefs somewhere
	// else, and it cannot be combined with LinkSchemes.
	AnyLinkScheme bool
}

// Limit returns a pointer to n, for setting a [Limits] field.
func Limit(n int) *int { return &n }

// DefaultLimits returns the limits a service validates against when no option
// replaces them, with every field set.
func DefaultLimits() Limits {
	return Limits{
		MaxTitleBytes:        Limit(DefaultMaxTitleBytes),
		MaxDataBytes:         Limit(DefaultMaxDataBytes),
		MaxLinks:             Limit(DefaultMaxLinks),
		MaxLinkRelationBytes: Limit(DefaultMaxLinkRelationBytes),
		MaxLinkHrefBytes:     Limit(DefaultMaxLinkHrefBytes),
		MaxDraftsPerPublish:  Limit(DefaultMaxDraftsPerPublish),
		MaxFilterValues:      Limit(DefaultMaxFilterValues),
		LinkSchemes:          DefaultLinkSchemes(),
	}
}

// DefaultLinkSchemes returns the schemes a link href may use when a host names
// none: http and https. A relative reference has no scheme and is permitted
// whatever this returns.
func DefaultLinkSchemes() []string { return []string{"http", "https"} }

// maxTitleBytes is the configured title limit, or its default.
func (l Limits) maxTitleBytes() int { return limitOr(l.MaxTitleBytes, DefaultMaxTitleBytes) }

// maxDataBytes is the configured payload limit, or its default.
func (l Limits) maxDataBytes() int { return limitOr(l.MaxDataBytes, DefaultMaxDataBytes) }

// maxLinks is the configured link count limit, or its default.
func (l Limits) maxLinks() int { return limitOr(l.MaxLinks, DefaultMaxLinks) }

// maxLinkRelationBytes is the configured relation name limit, or its default.
func (l Limits) maxLinkRelationBytes() int {
	return limitOr(l.MaxLinkRelationBytes, DefaultMaxLinkRelationBytes)
}

// maxLinkHrefBytes is the configured href limit, or its default.
func (l Limits) maxLinkHrefBytes() int { return limitOr(l.MaxLinkHrefBytes, DefaultMaxLinkHrefBytes) }

// maxDraftsPerPublish is the configured draft limit, or its default.
func (l Limits) maxDraftsPerPublish() int {
	return limitOr(l.MaxDraftsPerPublish, DefaultMaxDraftsPerPublish)
}

// maxFilterValues is the configured filter value limit, or its default.
func (l Limits) maxFilterValues() int { return limitOr(l.MaxFilterValues, DefaultMaxFilterValues) }

// linkSchemes are the configured schemes, or the defaults.
func (l Limits) linkSchemes() []string {
	if l.LinkSchemes == nil {
		return DefaultLinkSchemes()
	}

	return l.LinkSchemes
}

// limitOr is a set limit, or its default when the host set none.
func limitOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}

	return *value
}

// validate reports a meaningless or contradictory set of limits.
func (l Limits) validate() error {
	refuse := func(detail string) error { return &ConfigurationError{Detail: detail} }

	set := []struct {
		name  string
		value *int
	}{
		{"the title limit", l.MaxTitleBytes},
		{"the data limit", l.MaxDataBytes},
		{"the link limit", l.MaxLinks},
		{"the link relation limit", l.MaxLinkRelationBytes},
		{"the link href limit", l.MaxLinkHrefBytes},
		{"the draft limit", l.MaxDraftsPerPublish},
		{"the filter value limit", l.MaxFilterValues},
	}

	for _, limit := range set {
		if limit.value != nil && *limit.value <= 0 {
			return refuse(limit.name + " must be positive; leave it unset to keep the default")
		}
	}

	switch {
	case l.LinkSchemes != nil && l.AnyLinkScheme:
		return refuse("link schemes are both named and unchecked; choose LinkSchemes or AnyLinkScheme")
	case l.LinkSchemes != nil && len(l.LinkSchemes) == 0:
		return refuse("LinkSchemes names no scheme; leave it unset to keep http and https, or set AnyLinkScheme")
	case slices.Contains(l.LinkSchemes, ""):
		return refuse("a permitted link scheme must not be empty")
	}

	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestDefaultLimits|TestDefaultLinkSchemesIsACopy' -count=1 ./...
```

Expected: PASS.

- [ ] **Step 5: Write the failing test for construction-time validation**

Add to `limits_test.go`:

```go
func TestNewRefusesMeaninglessLimits(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		limits ntfy.Limits
		assert func(t *testing.T, svc *ntfy.Service, err error)
	}

	refused := func(t *testing.T, svc *ntfy.Service, err error) {
		t.Helper()

		require.ErrorIs(t, err, ntfy.ErrConfiguration)
		assert.Nil(t, svc)
	}

	cases := []testCase{
		{
			name:   "a zero title limit is refused",
			limits: ntfy.Limits{MaxTitleBytes: ntfy.Limit(0)},
			assert: refused,
		},
		{
			name:   "a negative data limit is refused",
			limits: ntfy.Limits{MaxDataBytes: ntfy.Limit(-1)},
			assert: refused,
		},
		{
			name:   "a zero filter value limit is refused",
			limits: ntfy.Limits{MaxFilterValues: ntfy.Limit(0)},
			assert: refused,
		},
		{
			name:   "a zero draft limit is refused",
			limits: ntfy.Limits{MaxDraftsPerPublish: ntfy.Limit(0)},
			assert: refused,
		},
		{
			name:   "naming schemes and accepting any scheme at once is refused",
			limits: ntfy.Limits{LinkSchemes: []string{"mailto"}, AnyLinkScheme: true},
			assert: refused,
		},
		{
			name:   "naming no scheme at all is refused",
			limits: ntfy.Limits{LinkSchemes: []string{}},
			assert: refused,
		},
		{
			name:   "an empty scheme is refused",
			limits: ntfy.Limits{LinkSchemes: []string{"mailto", ""}},
			assert: refused,
		},
		{
			name:   "unset limits keep the defaults",
			limits: ntfy.Limits{},
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.NoError(t, err)
				assert.NotNil(t, svc)
			},
		},
		{
			name:   "a raised limit is accepted",
			limits: ntfy.Limits{MaxDataBytes: ntfy.Limit(1 << 20)},
			assert: func(t *testing.T, svc *ntfy.Service, err error) {
				require.NoError(t, err)
				assert.NotNil(t, svc)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithLimits(tc.limits))
			tc.assert(t, svc, err)
		})
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestNewRefusesMeaninglessLimits' -count=1 ./...
```

Expected: FAIL — `undefined: ntfy.WithLimits`.

- [ ] **Step 7: Add the field, the option and the check in `New`**

In `service.go`, add to the `Service` struct after `onSignalError`:

```go
	limits        Limits
```

Add the option after `WithSignalErrorHandler`:

```go
// WithLimits replaces the validation limits, [DefaultLimits]. A field the host
// leaves unset keeps its default. A limit that is not positive, or naming link
// schemes while also accepting any scheme, is a [ConfigurationError].
func WithLimits(limits Limits) Option {
	return func(s *Service) { s.limits = limits }
}
```

In `New`, after the option loop and before `return svc, nil`:

```go
	if err := svc.limits.validate(); err != nil {
		return nil, err
	}
```

- [ ] **Step 8: Run it to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestNewRefusesMeaninglessLimits|TestNew$' -count=1 ./...
```

Expected: PASS, including the existing `TestNew`, which builds services with no limits at all.

- [ ] **Step 9: Commit**

```bash
git add limits.go limits_test.go service.go
git commit -m "Add validation limits and the option that replaces them"
```

---

### Task 2: Bound a draft's title, links and payload

**Files:**
- Modify: `notification.go:37-45` (the constant block comment), `notification.go:136-200` (`Draft.Validate`, `content`, `validateContent`)
- Modify: `notification_test.go:28-51` (move the `issue` helper to file scope), plus new tests
- Test: `notification_test.go`

**Interfaces:**
- Consumes: `Limits`, `limitOr` and the accessors from Task 1.
- Produces:
  ```go
  func (d Draft) Validate() error                      // unchanged signature, now defaults-based
  func (d Draft) ValidateWithin(limits Limits) error

  // unexported
  type content struct {
      sourceID       string
      subject        string
      kind           string
      subjectVersion int64
      title          string
      links          map[string]string
      data           json.RawMessage
  }

  func validateContent(c content, withSubject bool, limits Limits) []ValidationIssue
  func validateLinks(links map[string]string, limits Limits) []ValidationIssue
  func escapePointer(token string) string
  ```
  Issue pointers this task produces, which later tests assert on: `/title`, `/data`, `/links`, `/links/<relation>` (RFC 6901 escaped).

- [ ] **Step 1: Move the `issue` helper to file scope**

In `notification_test.go`, delete the local closure declared inside `TestDraftValidate` (lines 37–51) and add this at file scope, after `validDraft`:

```go
// issue asserts that err is a validation error carrying an issue for pointer.
func issue(t *testing.T, err error, pointer string) {
	t.Helper()

	require.ErrorIs(t, err, ntfy.ErrValidation)

	var validation *ntfy.ValidationError
	require.ErrorAs(t, err, &validation)

	pointers := make([]string, 0, len(validation.Issues))
	for _, issue := range validation.Issues {
		pointers = append(pointers, issue.Pointer)
	}

	assert.Contains(t, pointers, pointer)
}
```

The name and signature are identical to the closure it replaces, so no call site in `TestDraftValidate` changes.

- [ ] **Step 2: Run the existing tests to verify the move changed nothing**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestDraftValidate$' -count=1 ./...
```

Expected: PASS, exactly as before.

- [ ] **Step 3: Write the failing content-limit test**

Add to `notification_test.go` (and add `"strconv"` to its imports):

```go
func TestDraftValidateContentLimits(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		draft  func(d *ntfy.Draft)
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name:   "a title longer than the limit is refused",
			draft:  func(d *ntfy.Draft) { d.Title = strings.Repeat("t", ntfy.DefaultMaxTitleBytes+1) },
			assert: func(t *testing.T, err error) { issue(t, err, "/title") },
		},
		{
			name:  "a title of exactly the limit is valid",
			draft: func(d *ntfy.Draft) { d.Title = strings.Repeat("t", ntfy.DefaultMaxTitleBytes) },
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "a payload larger than the limit is refused",
			draft: func(d *ntfy.Draft) {
				d.Data = json.RawMessage(`{"padding":"` + strings.Repeat("p", ntfy.DefaultMaxDataBytes) + `"}`)
			},
			assert: func(t *testing.T, err error) { issue(t, err, "/data") },
		},
		{
			name: "more links than the limit are refused",
			draft: func(d *ntfy.Draft) {
				d.Links = map[string]string{}
				for i := range ntfy.DefaultMaxLinks + 1 {
					d.Links["rel-"+strconv.Itoa(i)] = "/v1/tasks/task-1"
				}
			},
			assert: func(t *testing.T, err error) { issue(t, err, "/links") },
		},
		{
			name: "a relation name longer than the limit is refused",
			draft: func(d *ntfy.Draft) {
				d.Links = map[string]string{strings.Repeat("r", ntfy.DefaultMaxLinkRelationBytes+1): "/v1/tasks/task-1"}
			},
			assert: func(t *testing.T, err error) {
				issue(t, err, "/links/"+strings.Repeat("r", ntfy.DefaultMaxLinkRelationBytes+1))
			},
		},
		{
			name: "an href longer than the limit is refused",
			draft: func(d *ntfy.Draft) {
				d.Links = map[string]string{"task": "/v1/tasks/" + strings.Repeat("x", ntfy.DefaultMaxLinkHrefBytes)}
			},
			assert: func(t *testing.T, err error) { issue(t, err, "/links/task") },
		},
		{
			name: "a relation name carrying a slash is escaped in the pointer",
			draft: func(d *ntfy.Draft) {
				d.Links = map[string]string{"a/b": "/v1/tasks/" + strings.Repeat("x", ntfy.DefaultMaxLinkHrefBytes)}
			},
			assert: func(t *testing.T, err error) { issue(t, err, "/links/a~1b") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			draft := validDraft()
			tc.draft(&draft)

			tc.assert(t, draft.Validate())
		})
	}
}
```

- [ ] **Step 4: Run it to verify it fails for the right reason**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestDraftValidateContentLimits' -count=1 ./...
```

Expected: FAIL on every case except "a title of exactly the limit is valid", each with testify's

```
Error:  Target error should be in err chain:
        expected: "ntfy: not valid"
        in chain:
```

because the draft was **accepted**. That is the defect. It must not fail with a compile error — Task 1 put the constants in place for exactly this reason.

- [ ] **Step 5: Add the limits to `validateContent`**

In `notification.go`, add `"strings"` to the imports (`escapePointer` uses it), then replace `Draft.Validate`, `content` and `validateContent`. `"net/url"` is **not** added here — nothing uses it until Task 3, and an unused import does not compile:

```go
// Validate reports every problem with the draft as a [ValidationError], or nil.
// It validates content against [DefaultLimits]; [Draft.ValidateWithin]
// validates against a service's configured limits.
func (d Draft) Validate() error { return d.ValidateWithin(Limits{}) }

// ValidateWithin reports every problem with the draft, bounding its content by
// limits. An unset limit keeps its default.
func (d Draft) ValidateWithin(limits Limits) error {
	issues := validateContent(content{
		sourceID: d.SourceID, subject: d.Subject, kind: d.Kind,
		subjectVersion: d.SubjectVersion, title: d.Title, links: d.Links, data: d.Data,
	}, true, limits)

	issues = append(issues, validateIdentifier("/recipient", d.Recipient)...)

	return invalid("draft", issues)
}

// content is what a draft and a close successor have in common.
type content struct {
	sourceID       string
	subject        string
	kind           string
	subjectVersion int64
	title          string
	links          map[string]string
	data           json.RawMessage
}

// validateContent checks the fields a draft and a successor share. withSubject
// is false for a successor, whose subject comes from the close.
func validateContent(c content, withSubject bool, limits Limits) []ValidationIssue {
	var issues []ValidationIssue

	issues = append(issues, validateIdentifier("/sourceId", c.sourceID)...)

	if withSubject {
		issues = append(issues, validateIdentifier("/subject", c.subject)...)
	}

	switch {
	case c.kind == "":
		issues = append(issues, ValidationIssue{Pointer: "/kind", Detail: "is required"})
	case len(c.kind) > MaxKindBytes:
		issues = append(issues, ValidationIssue{
			Pointer: "/kind", Detail: "is longer than " + strconv.Itoa(MaxKindBytes) + " bytes",
		})
	}

	if c.subjectVersion < 0 {
		issues = append(issues, ValidationIssue{Pointer: "/subjectVersion", Detail: "must not be negative"})
	}

	if max := limits.maxTitleBytes(); len(c.title) > max {
		issues = append(issues, ValidationIssue{
			Pointer: "/title", Detail: "is longer than " + strconv.Itoa(max) + " bytes",
		})
	}

	issues = append(issues, validateLinks(c.links, limits)...)

	// The size is checked first, so that an oversized payload is refused
	// without scanning it for well-formed JSON.
	switch max := limits.maxDataBytes(); {
	case len(c.data) > max:
		issues = append(issues, ValidationIssue{
			Pointer: "/data", Detail: "is longer than " + strconv.Itoa(max) + " bytes",
		})
	case len(c.data) > 0 && !json.Valid(c.data):
		issues = append(issues, ValidationIssue{Pointer: "/data", Detail: "is not valid JSON"})
	}

	return issues
}

// validateLinks checks how many links a draft carries and the form of each, in
// relation order so that the same draft always reports the same issues.
func validateLinks(links map[string]string, limits Limits) []ValidationIssue {
	if len(links) == 0 {
		return nil
	}

	var issues []ValidationIssue

	if max := limits.maxLinks(); len(links) > max {
		issues = append(issues, ValidationIssue{
			Pointer: "/links", Detail: "carries more than " + strconv.Itoa(max) + " links",
		})
	}

	for _, relation := range slices.Sorted(maps.Keys(links)) {
		pointer := "/links/" + escapePointer(relation)

		if max := limits.maxLinkRelationBytes(); len(relation) > max {
			issues = append(issues, ValidationIssue{
				Pointer: pointer, Detail: "has a relation name longer than " + strconv.Itoa(max) + " bytes",
			})
		}

		if max := limits.maxLinkHrefBytes(); len(links[relation]) > max {
			issues = append(issues, ValidationIssue{
				Pointer: pointer, Detail: "has an href longer than " + strconv.Itoa(max) + " bytes",
			})
		}
	}

	return issues
}

// escapePointer escapes a relation name for an RFC 6901 JSON Pointer.
func escapePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}
```

The scheme check joins `validateLinks` in Task 3; this task bounds sizes only, so the package still compiles and every test passes at the end of it.

- [ ] **Step 6: Correct the constant block's comment**

Replace `notification.go:37-39` with:

```go
// The identifier limits every store enforces. They are the widths of the SQL
// store's columns, applied everywhere so that a draft accepted in memory is
// accepted in SQL too. A draft's title, links and payload are bounded
// separately, by [Limits], because their columns are unbounded text.
```

- [ ] **Step 7: Run the tests to verify they pass**

```sh
GOTOOLCHAIN=go1.26.8 go test -count=1 ./...
```

Expected: PASS, including `TestDraftValidate`, whose existing cases are unaffected — `validDraft` carries a short title, one short link and a 15-byte payload.

- [ ] **Step 8: Add the consumer override case**

Add to `TestDraftValidateContentLimits`, changing the runner to take limits per case. Replace the `cases` runner at the end of the function with:

```go
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			draft := validDraft()
			tc.draft(&draft)

			tc.assert(t, draft.Validate())
		})
	}
}

func TestDraftValidateWithinRaisedLimits(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{"padding":"` + strings.Repeat("p", ntfy.DefaultMaxDataBytes) + `"}`)

	draft := validDraft()
	draft.Data = payload

	require.Error(t, draft.Validate(), "the default limit refuses it")

	limits := ntfy.Limits{MaxDataBytes: ntfy.Limit(1 << 20)}
	assert.NoError(t, draft.ValidateWithin(limits), "a host that raises the limit accepts it")
}
```

- [ ] **Step 9: Run it to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestDraftValidate' -count=1 ./...
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add notification.go notification_test.go
git commit -m "Bound a draft's title, links and payload"
```

---

### Task 3: Refuse a link href whose scheme is not permitted

**Files:**
- Modify: `notification.go` (`validateLinks`, new `checkScheme`)
- Test: `notification_test.go`

**Interfaces:**
- Consumes: `validateLinks`, `escapePointer` (Task 2); `Limits.linkSchemes`, `Limits.AnyLinkScheme` (Task 1).
- Produces:
  ```go
  // unexported
  func checkScheme(href string, limits Limits) string // "" when permitted, otherwise the issue detail
  ```

- [ ] **Step 1: Write the failing scheme test**

Add to `notification_test.go`:

```go
func TestDraftValidateLinkSchemes(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		links  map[string]string
		limits ntfy.Limits
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name:   "a javascript href is refused by default",
			links:  map[string]string{"task": "javascript:alert(1)"},
			assert: func(t *testing.T, err error) { issue(t, err, "/links/task") },
		},
		{
			name:   "the scheme is matched however it is cased",
			links:  map[string]string{"task": "JavaScript:alert(1)"},
			assert: func(t *testing.T, err error) { issue(t, err, "/links/task") },
		},
		{
			name:   "a data href is refused by default",
			links:  map[string]string{"task": "data:text/html,<script>alert(1)</script>"},
			assert: func(t *testing.T, err error) { issue(t, err, "/links/task") },
		},
		{
			name:   "a mailto href is refused by default",
			links:  map[string]string{"task": "mailto:alice@example.test"},
			assert: func(t *testing.T, err error) { issue(t, err, "/links/task") },
		},
		{
			name:  "http, https and relative hrefs are accepted by default",
			links: map[string]string{"a": "http://x.test/a", "b": "https://x.test/b", "c": "/v1/tasks/task-1"},
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:   "an href that is not a URL reference is refused",
			links:  map[string]string{"task": "/v1/tasks/%zz"},
			assert: func(t *testing.T, err error) { issue(t, err, "/links/task") },
		},
		{
			name:   "a host permits another scheme",
			links:  map[string]string{"task": "mailto:alice@example.test"},
			limits: ntfy.Limits{LinkSchemes: []string{"http", "https", "mailto"}},
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:   "a host opts out of the check",
			links:  map[string]string{"task": "javascript:alert(1)"},
			limits: ntfy.Limits{AnyLinkScheme: true},
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			draft := validDraft()
			draft.Links = tc.links

			tc.assert(t, draft.ValidateWithin(tc.limits))
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestDraftValidateLinkSchemes' -count=1 ./...
```

Expected: FAIL on the `javascript`, `JavaScript`, `data`, `mailto` and `%zz` cases — each href is accepted today. The three cases that should pass (the default-accepted hrefs, the named `mailto`, the opt-out) already pass, which is what proves the test is measuring the scheme rule and nothing else.

- [ ] **Step 3: Add the scheme check**

In `notification.go`, add `"net/url"` to the imports — it is used for the first time here — then add the href check inside `validateLinks`'s loop, replacing the href length block:

```go
		if max := limits.maxLinkHrefBytes(); len(links[relation]) > max {
			issues = append(issues, ValidationIssue{
				Pointer: pointer, Detail: "has an href longer than " + strconv.Itoa(max) + " bytes",
			})

			continue
		}

		if detail := checkScheme(links[relation], limits); detail != "" {
			issues = append(issues, ValidationIssue{Pointer: pointer, Detail: detail})
		}
```

and add after `validateLinks`:

```go
// checkScheme reports why an href is refused, or an empty string when it is
// permitted. It checks form only: it never resolves an href, never asks where
// it points, and never rewrites one it accepts.
func checkScheme(href string, limits Limits) string {
	if limits.AnyLinkScheme {
		return ""
	}

	parsed, err := url.Parse(href)
	if err != nil {
		return "has an href that is not a URL reference"
	}

	// A relative reference carries no scheme and is always permitted.
	if parsed.Scheme == "" {
		return ""
	}

	if slices.Contains(limits.linkSchemes(), strings.ToLower(parsed.Scheme)) {
		return ""
	}

	return "has an href using the " + strings.ToLower(parsed.Scheme) + " scheme, which is not permitted"
}
```

`url.Parse` already lowercases a scheme; `strings.ToLower` states the intent and keeps the comparison correct if that ever changes.

- [ ] **Step 4: Run it to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestDraftValidate' -count=1 ./...
```

Expected: PASS.

- [ ] **Step 5: Add the byte-for-byte case**

The spec requires an accepted href be returned exactly as published. Add to `notification_test.go`:

```go
func TestDraftValidateDoesNotRewriteAnAcceptedHref(t *testing.T) {
	t.Parallel()

	href := "https://x.test/a%2Fb?q=1&q=2#frag"

	draft := validDraft()
	draft.Links = map[string]string{"task": href}

	require.NoError(t, draft.Validate())
	assert.Equal(t, href, draft.Links["task"], "validation never normalises an href it accepts")
}
```

- [ ] **Step 6: Run it to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestDraftValidateDoesNotRewriteAnAcceptedHref' -count=1 ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add notification.go notification_test.go
git commit -m "Refuse a link href whose scheme is not permitted"
```

---

### Task 4: The publish path enforces the service's limits and the draft cap

**Files:**
- Modify: `service.go:119-174` (`Publish`)
- Test: `service_publish_test.go`

**Interfaces:**
- Consumes: `Draft.ValidateWithin` (Task 2), `Limits.maxDraftsPerPublish` (Task 1), `Service.limits` (Task 1).
- Produces: `Publish` refuses an over-limit call with a `*ValidationError` whose `Subject` is `"publish"` and whose issue pointer is `/drafts`, before any store call.

- [ ] **Step 1: Write the failing draft-cap test**

Add to `service_publish_test.go`:

```go
func TestServicePublishRefusesTooManyDrafts(t *testing.T) {
	t.Parallel()

	// No Insert is expected: the refusal happens before any transaction opens.
	svc, _, _, _ := mocked(t)

	drafts := make([]ntfy.Draft, 0, ntfy.DefaultMaxDraftsPerPublish+1)
	for i := range ntfy.DefaultMaxDraftsPerPublish + 1 {
		drafts = append(drafts, ntfy.Draft{
			Recipient: "alice", SourceID: "event-" + strconv.Itoa(i), Subject: "task-1", Kind: "offer",
		})
	}

	result, err := svc.Publish(t.Context(), drafts...)

	require.ErrorIs(t, err, ntfy.ErrValidation)
	assert.Empty(t, result.Created)

	var validation *ntfy.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Len(t, validation.Issues, 1)
	assert.Equal(t, "/drafts", validation.Issues[0].Pointer)
}
```

- [ ] **Step 2: Run it to verify it fails**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestServicePublishRefusesTooManyDrafts' -count=1 ./...
```

Expected: FAIL with gomock's

```
Unexpected call to *ntfy.MockStore.Insert(...)
```

because the publish reaches the store today. That unexpected call **is** the defect: a thousand-and-one drafts opened a transaction.

- [ ] **Step 3: Add the cap and the configured limits to `Publish`**

In `service.go`, replace the head of `Publish` down to the existing `if len(drafts) == 0` block:

```go
func (s *Service) Publish(ctx context.Context, drafts ...Draft) (PublishResult, error) {
	if max := s.limits.maxDraftsPerPublish(); len(drafts) > max {
		return PublishResult{}, &ValidationError{Subject: "publish", Issues: []ValidationIssue{{
			Pointer: "/drafts", Detail: "carries more than " + strconv.Itoa(max) + " drafts",
		}}}
	}

	var issues []ValidationIssue

	for i, draft := range drafts {
		issues = append(issues, prefixed("/drafts/"+strconv.Itoa(i), draft.ValidateWithin(s.limits))...)
	}
```

The rest of `Publish` is unchanged.

- [ ] **Step 4: Run it to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestServicePublish' -count=1 ./...
```

Expected: PASS, including the existing `TestServicePublish` cases.

- [ ] **Step 5: Add the consumer override cases**

Add to `service_publish_test.go`:

```go
func TestServicePublishHonoursConfiguredLimits(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{"padding":"` + strings.Repeat("p", ntfy.DefaultMaxDataBytes) + `"}`)

	draft := ntfy.Draft{
		Recipient: "alice", SourceID: "event-1", Subject: "task-1", Kind: "offer", Data: payload,
	}

	t.Run("the default payload limit refuses an oversized draft", func(t *testing.T) {
		t.Parallel()

		svc, err := ntfy.New(ntfy.NewMemoryStore())
		require.NoError(t, err)

		_, err = svc.Publish(t.Context(), draft)
		assert.ErrorIs(t, err, ntfy.ErrValidation)
	})

	t.Run("a raised payload limit accepts it and returns it byte for byte", func(t *testing.T) {
		t.Parallel()

		svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithLimits(ntfy.Limits{
			MaxDataBytes: ntfy.Limit(1 << 20),
		}))
		require.NoError(t, err)

		result, err := svc.Publish(t.Context(), draft)
		require.NoError(t, err)
		require.Len(t, result.Created, 1)

		stored, err := svc.Get(t.Context(), "alice", result.Created[0].ID)
		require.NoError(t, err)
		assert.Equal(t, []byte(payload), []byte(stored.Data))
	})

	t.Run("a lowered draft cap refuses a publish the default would accept", func(t *testing.T) {
		t.Parallel()

		svc, err := ntfy.New(ntfy.NewMemoryStore(), ntfy.WithLimits(ntfy.Limits{
			MaxDraftsPerPublish: ntfy.Limit(1),
		}))
		require.NoError(t, err)

		second := draft
		second.Data = nil
		second.SourceID = "event-2"

		first := draft
		first.Data = nil

		_, err = svc.Publish(t.Context(), first, second)
		assert.ErrorIs(t, err, ntfy.ErrValidation)
	})
}
```

Add `"strings"` to the file's imports; `"encoding/json"` and `"strconv"` are needed too — `strconv` is already imported, `encoding/json` is not.

- [ ] **Step 6: Run it to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestServicePublishHonoursConfiguredLimits' -count=1 ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add service.go service_publish_test.go
git commit -m "Apply the service's limits and a draft cap to publishing"
```

---

### Task 5: A close's successor is bounded by the same limits

**Files:**
- Modify: `store.go:121-153` (`CloseRequest.Validate`)
- Modify: `service.go:180-197` (`Service.Close`)
- Test: `service_ops_test.go`

**Interfaces:**
- Consumes: `validateContent` with the `title`/`links` fields (Task 2), `Service.limits` (Task 1).
- Produces:
  ```go
  func (r CloseRequest) Validate() error                      // unchanged signature
  func (r CloseRequest) ValidateWithin(limits Limits) error
  ```
  Successor issue pointers are prefixed `/successor`, so an oversized successor payload reports `/successor/data`.

- [ ] **Step 1: Write the failing successor test**

Add to `service_ops_test.go`:

```go
func TestCloseRequestValidateBoundsItsSuccessor(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{"padding":"` + strings.Repeat("p", ntfy.DefaultMaxDataBytes) + `"}`)

	req := ntfy.CloseRequest{
		Subject: "task-1", Version: 5,
		Successor: &ntfy.Successor{SourceID: "event-5", Kind: "taken", Data: payload},
	}

	err := req.Validate()
	require.ErrorIs(t, err, ntfy.ErrValidation)

	var validation *ntfy.ValidationError
	require.ErrorAs(t, err, &validation)

	pointers := make([]string, 0, len(validation.Issues))
	for _, issue := range validation.Issues {
		pointers = append(pointers, issue.Pointer)
	}

	assert.Contains(t, pointers, "/successor/data")
}
```

Add `"encoding/json"` and `"strings"` to the file's imports — it currently has neither.

- [ ] **Step 2: Run it to verify it fails**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestCloseRequestValidateBoundsItsSuccessor' -count=1 ./...
```

Expected: FAIL — `Target error should be in err chain: expected "ntfy: not valid"` — the oversized successor validates clean today.

- [ ] **Step 3: Add `ValidateWithin` to `CloseRequest`**

In `store.go`, replace `CloseRequest.Validate` with:

```go
// Validate reports every problem with the request as a [ValidationError], or
// nil. It validates the successor's content against [DefaultLimits];
// [CloseRequest.ValidateWithin] validates against a service's configured
// limits.
func (r CloseRequest) Validate() error { return r.ValidateWithin(Limits{}) }

// ValidateWithin reports every problem with the request, bounding its
// successor's content by limits. An unset limit keeps its default.
func (r CloseRequest) ValidateWithin(limits Limits) error {
	issues := validateIdentifier("/subject", r.Subject)

	if r.Version < 0 {
		issues = append(issues, ValidationIssue{Pointer: "/version", Detail: "must not be negative"})
	}

	for i, kind := range r.Kinds {
		if kind == "" || len(kind) > MaxKindBytes {
			issues = append(issues, ValidationIssue{
				Pointer: "/kinds/" + strconv.Itoa(i),
				Detail:  "must be between 1 and " + strconv.Itoa(MaxKindBytes) + " bytes",
			})
		}
	}

	if len(r.Reason) > MaxIdentifierBytes {
		issues = append(issues, ValidationIssue{
			Pointer: "/reason", Detail: "is longer than " + strconv.Itoa(MaxIdentifierBytes) + " bytes",
		})
	}

	if s := r.Successor; s != nil {
		for _, issue := range validateContent(content{
			sourceID: s.SourceID, kind: s.Kind, subjectVersion: s.SubjectVersion,
			title: s.Title, links: s.Links, data: s.Data,
		}, false, limits) {
			issue.Pointer = "/successor" + issue.Pointer
			issues = append(issues, issue)
		}
	}

	return invalid("close", issues)
}
```

- [ ] **Step 4: Use the service's limits in `Close`**

In `service.go`, change the first line of `Close`:

```go
	if err := req.ValidateWithin(s.limits); err != nil {
		return CloseResult{}, err
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestCloseRequest|TestServiceClose' -count=1 ./...
```

Expected: PASS, including the existing `TestServiceClose` cases.

- [ ] **Step 6: Commit**

```bash
git add store.go service.go service_ops_test.go
git commit -m "Bound a close successor's content by the same limits"
```

---

### Task 6: Bound how many values one list filter carries

**Files:**
- Modify: `store.go:232-257` (`ListQuery.Validate`)
- Modify: `service.go:215-221` (`Service.List`)
- Test: `cursor_test.go`

**Interfaces:**
- Consumes: `Limits.maxFilterValues` (Task 1).
- Produces:
  ```go
  func (q ListQuery) Validate() error                      // unchanged signature
  func (q ListQuery) ValidateWithin(limits Limits) error
  ```
  Issue pointers `/kinds` and `/states`.

- [ ] **Step 1: Write the failing filter-bound test**

Add to `cursor_test.go` (and add `"strconv"` to its imports):

```go
func TestListQueryValidateFilterLimits(t *testing.T) {
	t.Parallel()

	kinds := func(n int) []string {
		out := make([]string, 0, n)
		for i := range n {
			out = append(out, "kind-"+strconv.Itoa(i))
		}

		return out
	}

	states := func(n int) []ntfy.State {
		out := make([]ntfy.State, 0, n)
		for range n {
			out = append(out, ntfy.StateActive)
		}

		return out
	}

	type testCase struct {
		name   string
		query  ntfy.ListQuery
		limits ntfy.Limits
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name:  "kinds at the bound are accepted",
			query: ntfy.ListQuery{Recipient: "alice", Kinds: kinds(ntfy.DefaultMaxFilterValues)},
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:  "more kinds than the bound are refused",
			query: ntfy.ListQuery{Recipient: "alice", Kinds: kinds(ntfy.DefaultMaxFilterValues + 1)},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ntfy.ErrValidation)

				var validation *ntfy.ValidationError
				require.ErrorAs(t, err, &validation)
				require.Len(t, validation.Issues, 1, "one issue names the filter, not one per value")
				assert.Equal(t, "/kinds", validation.Issues[0].Pointer)
			},
		},
		{
			name:  "more states than the bound are refused",
			query: ntfy.ListQuery{Recipient: "alice", States: states(ntfy.DefaultMaxFilterValues + 1)},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ntfy.ErrValidation)

				var validation *ntfy.ValidationError
				require.ErrorAs(t, err, &validation)
				require.Len(t, validation.Issues, 1)
				assert.Equal(t, "/states", validation.Issues[0].Pointer)
			},
		},
		{
			name:   "a host raises the bound",
			query:  ntfy.ListQuery{Recipient: "alice", Kinds: kinds(ntfy.DefaultMaxFilterValues + 1)},
			limits: ntfy.Limits{MaxFilterValues: ntfy.Limit(ntfy.DefaultMaxFilterValues + 10)},
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, tc.query.ValidateWithin(tc.limits))
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestListQueryValidateFilterLimits' -count=1 ./...
```

Expected: FAIL — `undefined: (ntfy.ListQuery).ValidateWithin`. Before Step 3, also prove the underlying defect against today's API by running:

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestListQueryValidateFilterLimits/more_kinds' -count=1 ./... 2>&1 | head -5
```

and, once `ValidateWithin` exists but before the bound is added, confirm the two "refused" cases fail with "An error is expected but got nil" — that is the defect: 101 values validate clean.

- [ ] **Step 3: Add the bound**

In `store.go`, replace `ListQuery.Validate`:

```go
// Validate reports every problem with the query as a [ValidationError], or nil.
// It bounds filters by [DefaultLimits]; [ListQuery.ValidateWithin] bounds them
// by a service's configured limits.
func (q ListQuery) Validate() error { return q.ValidateWithin(Limits{}) }

// ValidateWithin reports every problem with the query, bounding how many values
// each filter carries by limits. An unset limit keeps its default.
func (q ListQuery) ValidateWithin(limits Limits) error {
	issues := validateIdentifier("/recipient", q.Recipient)

	if q.Limit < 0 || q.Limit > MaxListLimit {
		issues = append(issues, ValidationIssue{
			Pointer: "/limit", Detail: "must be between 1 and " + strconv.Itoa(MaxListLimit),
		})
	}

	max := limits.maxFilterValues()

	for _, filter := range []struct {
		pointer string
		values  int
	}{
		{"/kinds", len(q.Kinds)},
		{"/states", len(q.States)},
	} {
		if filter.values > max {
			issues = append(issues, ValidationIssue{
				Pointer: filter.pointer, Detail: "carries more than " + strconv.Itoa(max) + " values",
			})
		}
	}

	// Each state is checked only within the bound, so that an oversized filter
	// reports one issue rather than one per value.
	if len(q.States) <= max {
		for i, state := range q.States {
			if !state.Valid() {
				issues = append(issues, ValidationIssue{
					Pointer: "/states/" + strconv.Itoa(i), Detail: "is not ACTIVE, READ or CLOSED",
				})
			}
		}
	}

	if len(issues) == 0 {
		if _, _, err := DecodeCursor(q); err != nil {
			return err
		}
	}

	return invalid("request", issues)
}
```

- [ ] **Step 4: Use the service's limits in `List`**

In `service.go`:

```go
// List returns a page of a recipient's notifications, newest first.
func (s *Service) List(ctx context.Context, q ListQuery) (Page, error) {
	if err := q.ValidateWithin(s.limits); err != nil {
		return Page{}, err
	}

	return s.store.List(ctx, q)
}
```

- [ ] **Step 5: Run the tests to verify they pass, cursor behaviour included**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestListQuery|TestCursor' -count=1 ./...
```

Expected: PASS. `TestCursor` matters here: the fingerprint binds a cursor to its filter set, and bounding the filter must not change how a cursor encodes or decodes.

- [ ] **Step 6: Commit**

```bash
git add store.go service.go cursor_test.go
git commit -m "Bound how many values one list filter carries"
```

---

### Task 7: The listing handler parses its own query

**Files:**
- Modify: `http.go:178-219` (`list`), imports
- Test: `http_test.go`

**Interfaces:**
- Consumes: `ListQuery.ValidateWithin` through `Service.List` (Task 6).
- Produces: `GET /v1/notifications` answers `400` with code `validation_failed` for a query string `net/url` refuses, and never serves such a request.

- [ ] **Step 1: Write the failing unparseable-query test**

Add a case to `TestHandlerListAndCount`'s `cases` slice in `http_test.go`:

```go
		{
			name: "a query the server cannot parse is refused, not served unfiltered",
			assert: func(t *testing.T, env *httpEnv) {
				env.publish(t, offer("alice", "a1"), offer("alice", "a2"))

				// One more parameter than net/url will parse. Today
				// r.URL.Query() returns nothing for such a request and the
				// listing is served with every filter dropped, returning 200
				// and both notifications. That is the defect this proves.
				target := "/v1/notifications?" + strings.Repeat("kind=nomatch&", 10001)

				rec := env.do(t, http.MethodGet, target, "alice", nil)
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
				assert.Equal(t, "validation_failed", decodeError(t, rec).Error.Code)
			},
		},
		{
			name: "a filter carrying more values than the bound is a bad request",
			assert: func(t *testing.T, env *httpEnv) {
				target := "/v1/notifications?" + strings.Repeat("kind=x&", ntfy.DefaultMaxFilterValues+1)

				rec := env.do(t, http.MethodGet, target, "alice", nil)
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

				body := decodeError(t, rec)
				assert.Equal(t, "validation_failed", body.Error.Code)
				require.NotEmpty(t, body.Error.Issues)
				assert.Equal(t, "/kinds", body.Error.Issues[0].Pointer)
			},
		},
```

`strings` is already imported in `http_test.go`.

- [ ] **Step 2: Run it to verify it fails**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerListAndCount' -count=1 ./...
```

Expected: FAIL on "a query the server cannot parse" with

```
Error:  Not equal:
        expected: 400
        actual  : 200
```

and the body carrying both of alice's notifications — the listing was served with its filters dropped. The oversized-filter case passes already once Task 6 landed; it is here to hold the HTTP mapping of that bound.

- [ ] **Step 3: Parse the raw query in the handler**

In `http.go`, add `"net/url"` to the imports and replace the first line of `list`:

```go
func (h *Handler) list(w http.ResponseWriter, r *http.Request, actor string) {
	// Parsed here rather than through r.URL.Query(), which discards every value
	// and the error when the query cannot be parsed, leaving the listing to be
	// served with its filters silently dropped.
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		WriteError(w, &ValidationError{Subject: "request", Issues: []ValidationIssue{
			{Pointer: "/query", Detail: "could not be parsed"},
		}})

		return
	}
```

The rest of `list` is unchanged, including the existing `limit` handling, which already returns its own validation error.

- [ ] **Step 4: Run it to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerListAndCount' -count=1 ./...
```

Expected: PASS, including the existing filter, paging and cursor cases, which prove an ordinary query still parses.

- [ ] **Step 5: Add the fail-closed case for the stream**

Add a case to `TestHandlerStream`'s `cases` slice:

```go
		{
			name: "a stream whose query cannot be parsed follows the acting user, never someone else",
			run:  true,
			assert: func(t *testing.T, env *httpEnv, server *httptest.Server) {
				// The recipient parameter is unreachable in a query the server
				// cannot parse, so the stream falls back to the acting user.
				query := "?recipient=bob&" + strings.Repeat("x=1&", 10001)

				s := openStream(t, server, "alice", query)
				require.Equal(t, http.StatusOK, s.resp.StatusCode)

				s.expect(t, "the connected comment", equals(": connected"))

				env.publish(t, offer("alice", "a1"))
				s.expect(t, "an unread-changed event", equals("event: unread-changed"))
			},
		},
```

- [ ] **Step 6: Run it to verify it passes**

```sh
GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerStream$' -count=1 ./...
```

Expected: PASS. The stream reads one parameter and falls back to the actor, so it was never the endpoint that failed open — this case pins that.

- [ ] **Step 7: Commit**

```bash
git add http.go http_test.go
git commit -m "Refuse a listing whose query string cannot be parsed"
```

---

### Task 8: Document the limits and verify the whole change

**Files:**
- Modify: `docs/notifications.md:43-52` (the defaults-and-overrides table), `docs/notifications.md:261+` (stated limits)
- Test: the whole suite

**Interfaces:**
- Consumes: everything above.
- Produces: nothing in code.

- [ ] **Step 1: Add the limits to the defaults-and-overrides table**

In `docs/notifications.md`, add these rows to the table under "Defaults and overrides":

```markdown
| Title length | 1 KiB | `WithLimits(ntfy.Limits{MaxTitleBytes: ntfy.Limit(n)})` |
| Payload size | 64 KiB | `WithLimits(ntfy.Limits{MaxDataBytes: ntfy.Limit(n)})` |
| Links per notification | 16, each relation name at most 100 bytes and each href at most 2 KiB | `WithLimits(ntfy.Limits{MaxLinks: …, MaxLinkRelationBytes: …, MaxLinkHrefBytes: …})` |
| Link schemes | `http`, `https` and relative references | `WithLimits(ntfy.Limits{LinkSchemes: […]})`, or `AnyLinkScheme: true` to store any scheme |
| Drafts per publish | 1000 | `WithLimits(ntfy.Limits{MaxDraftsPerPublish: ntfy.Limit(n)})` |
| Values per list filter | 100 | `WithLimits(ntfy.Limits{MaxFilterValues: ntfy.Limit(n)})` |
```

- [ ] **Step 2: Add the stated limits**

Add to the "Stated limits" section:

```markdown
- **A page is bounded by two limits, not one.** A response carries at most
  `MaxListLimit` notifications of at most `MaxDataBytes` each, and the body is
  marshalled whole before a byte is written: 500 × 64 KiB is 32 MB. A host
  storing large payloads lowers one of the two.
- **An over-limit draft is refused, never truncated.** Content is returned
  exactly as published, so there is nothing sensible to store for a draft that
  does not fit.
- **A limit bounds what is published from here on.** Rows stored before a limit
  was lowered are untouched and are still returned in full.
- **A link's href is checked for form only.** Nothing resolves it, follows it,
  or asks where it points.
```

- [ ] **Step 3: Verify the docs against the code**

```sh
GOTOOLCHAIN=go1.26.8 go doc ./... 2>/dev/null | grep -A2 'DefaultMax'
```

Read the table back: every row names a default and the override beside it, and every number matches the constant in `limits.go`.

- [ ] **Step 4: Run the whole suite**

```sh
make all
```

Expected: lint, split-check and tests pass on every module. `split-check` matters here — the core gained `net/url` and `strings`, both standard library.

- [ ] **Step 5: Prove each test still catches its defect**

For each of the five defects, revert the fix temporarily, confirm the test fails, then restore it:

```sh
# 1. content limits: comment out the title/data/link checks in validateContent
GOTOOLCHAIN=go1.26.8 go test -run 'TestDraftValidateContentLimits' -count=1 ./...   # expect FAIL
# 2. schemes: make checkScheme return "" unconditionally
GOTOOLCHAIN=go1.26.8 go test -run 'TestDraftValidateLinkSchemes' -count=1 ./...     # expect FAIL
# 3. draft cap: remove the cap block from Publish
GOTOOLCHAIN=go1.26.8 go test -run 'TestServicePublishRefusesTooManyDrafts' -count=1 ./...  # expect FAIL
# 4. filter bound: remove the filter loop from ValidateWithin
GOTOOLCHAIN=go1.26.8 go test -run 'TestListQueryValidateFilterLimits' -count=1 ./... # expect FAIL
# 5. query parsing: restore values := r.URL.Query() in list
GOTOOLCHAIN=go1.26.8 go test -run 'TestHandlerListAndCount' -count=1 ./...          # expect FAIL
git checkout -- notification.go service.go store.go http.go
```

- [ ] **Step 6: Validate the change**

```sh
openspec validate "bound-publish-and-query-inputs" --strict
```

Expected: `Change 'bound-publish-and-query-inputs' is valid`. Then check each spec scenario against a test:

| Scenario | Test |
| --- | --- |
| An oversized payload is refused | `TestDraftValidateContentLimits` |
| An oversized title is refused | `TestDraftValidateContentLimits` |
| Too many links are refused | `TestDraftValidateContentLimits` |
| Too many drafts in one publish are refused | `TestServicePublishRefusesTooManyDrafts` |
| A host raises a limit | `TestServicePublishHonoursConfiguredLimits` |
| A meaningless limit is refused at construction | `TestNewRefusesMeaninglessLimits` |
| A script-bearing link is refused by default | `TestDraftValidateLinkSchemes` |
| Ordinary links are accepted by default | `TestDraftValidateLinkSchemes`, `TestDraftValidateDoesNotRewriteAnAcceptedHref` |
| A host permits another scheme | `TestDraftValidateLinkSchemes` |
| A host opts out of the check | `TestDraftValidateLinkSchemes` |
| Naming schemes and opting out at once is refused | `TestNewRefusesMeaninglessLimits` |
| A filter carrying too many values is refused | `TestListQueryValidateFilterLimits` |
| An unparseable query is refused, not served unfiltered | `TestHandlerListAndCount` |
| An oversized filter is a bad request | `TestHandlerListAndCount` |

- [ ] **Step 7: Commit**

```bash
git add docs/notifications.md
git commit -m "Document the publish and query limits and their overrides"
```

---

## Self-Review

Run against the spec after the plan was written; issues found were fixed inline and are recorded here.

**1. Spec coverage.** Every requirement in both delta specs maps to a task, and every scenario maps to a test (the table in Task 8 Step 6). Two gaps were found and fixed:

- The scenario "A meaningless limit is refused at construction" says a limit of **zero** must fail. An `int`-valued `Limits` with "zero means default" could not express that, so Task 1 uses pointer fields and the `Limit` helper, matching `emailConfig`'s documented rationale (`email_dispatcher.go:72-74`).
- The requirement "The system SHALL NOT rewrite or normalise an accepted href" had no test. Added as Task 3 Step 5.

A third issue was found by checking that each task compiles on its own: Task 2 originally added `"net/url"` alongside `"strings"`, but nothing uses `net/url` until Task 3, and Go does not compile an unused import. Task 2 now adds `"strings"` only, and Task 3 adds `"net/url"` where it is first used. The same check confirmed `notification.go` already imports `maps` and `slices`, which `validateLinks` needs, and that `service_ops_test.go` imports neither `encoding/json` nor `strings`, which Task 5 now states outright rather than conditionally.

**2. Placeholder scan.** No "TBD", no "add appropriate validation", no "similar to Task N". Every code step carries compilable Go, every run step a real command and its expected output. The one deliberately non-literal step is Task 8 Step 5, which describes five temporary reverts rather than printing the reverted code; the command and expected result for each are explicit.

**3. Type consistency.** Checked across tasks: `Limits` field names are identical in Tasks 1, 2, 3, 4, 6 and the docs table; `ValidateWithin` has the same signature on `Draft`, `ListQuery` and `CloseRequest`; accessor names (`maxTitleBytes`, `maxDataBytes`, `maxLinks`, `maxLinkRelationBytes`, `maxLinkHrefBytes`, `maxDraftsPerPublish`, `maxFilterValues`, `linkSchemes`) are used exactly as declared in Task 1; issue pointers (`/title`, `/data`, `/links`, `/links/<relation>`, `/kinds`, `/states`, `/drafts`, `/successor/data`, `/query`) match between the implementation steps and the tests that assert them.

One collision check was run against the tree before naming anything: `escapePointer`, `validateLinks`, `checkScheme`, `limitOr`, `Limit`, `DefaultLimits`, `WithLimits`, `ValidateWithin` and `linkSchemes` are all unused in the repository today. `setInt` in `email_dispatcher.go:340` has a different signature and is not touched.

**4. Behaviour verified before it was written into the plan.** `url.Parse` lowercases a scheme (`JAVASCRIPT:` → `javascript`), `//host/path` parses with an empty scheme and so counts as a relative reference, `%zz` is a parse error, and `url.ParseQuery` on 10001 parameters returns `number of URL query parameters exceeded limit`. Each was run under `GOTOOLCHAIN=go1.26.8` rather than recalled.

**Open question inherited from `design.md`**, deliberately not planned: whether `MarkRead`'s identifier list deserves the same bound. It changes neither the specs nor this breakdown.
