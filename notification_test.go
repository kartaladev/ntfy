package ntfy_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// validDraft is a draft that passes validation, for cases to break one field of.
func validDraft() ntfy.Draft {
	return ntfy.Draft{
		Recipient:      "alice",
		SourceID:       "event-1",
		Subject:        "task-1",
		SubjectVersion: 3,
		Kind:           "offer",
		Title:          "A task is available",
		Links:          map[string]string{"task": "/v1/tasks/task-1"},
		Data:           json.RawMessage(`{"b":1,"a":2.50}`),
	}
}

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

func TestDraftValidate(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		draft  func(d *ntfy.Draft)
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name:  "a complete draft is valid",
			draft: func(*ntfy.Draft) {},
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:  "a draft with no title, links or data is valid",
			draft: func(d *ntfy.Draft) { d.Title, d.Links, d.Data = "", nil, nil },
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:   "a recipient is required",
			draft:  func(d *ntfy.Draft) { d.Recipient = "" },
			assert: func(t *testing.T, err error) { issue(t, err, "/recipient") },
		},
		{
			name:   "a source is required",
			draft:  func(d *ntfy.Draft) { d.SourceID = "" },
			assert: func(t *testing.T, err error) { issue(t, err, "/sourceId") },
		},
		{
			name:   "a subject is required",
			draft:  func(d *ntfy.Draft) { d.Subject = "" },
			assert: func(t *testing.T, err error) { issue(t, err, "/subject") },
		},
		{
			name:   "a kind is required",
			draft:  func(d *ntfy.Draft) { d.Kind = "" },
			assert: func(t *testing.T, err error) { issue(t, err, "/kind") },
		},
		{
			name:   "a negative subject version is refused",
			draft:  func(d *ntfy.Draft) { d.SubjectVersion = -1 },
			assert: func(t *testing.T, err error) { issue(t, err, "/subjectVersion") },
		},
		{
			name:   "a kind longer than 100 bytes is refused",
			draft:  func(d *ntfy.Draft) { d.Kind = strings.Repeat("k", 101) },
			assert: func(t *testing.T, err error) { issue(t, err, "/kind") },
		},
		{
			name:  "a kind of exactly 100 bytes is valid",
			draft: func(d *ntfy.Draft) { d.Kind = strings.Repeat("k", 100) },
			assert: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:   "a subject longer than 255 bytes is refused",
			draft:  func(d *ntfy.Draft) { d.Subject = strings.Repeat("s", 256) },
			assert: func(t *testing.T, err error) { issue(t, err, "/subject") },
		},
		{
			name:   "a source longer than 255 bytes is refused",
			draft:  func(d *ntfy.Draft) { d.SourceID = strings.Repeat("s", 256) },
			assert: func(t *testing.T, err error) { issue(t, err, "/sourceId") },
		},
		{
			name:   "a recipient longer than 255 bytes is refused",
			draft:  func(d *ntfy.Draft) { d.Recipient = strings.Repeat("r", 256) },
			assert: func(t *testing.T, err error) { issue(t, err, "/recipient") },
		},
		{
			name:   "data that is not JSON is refused",
			draft:  func(d *ntfy.Draft) { d.Data = json.RawMessage(`{"open":`) },
			assert: func(t *testing.T, err error) { issue(t, err, "/data") },
		},
		{
			name:  "every problem is reported at once",
			draft: func(d *ntfy.Draft) { d.Recipient, d.Kind = "", "" },
			assert: func(t *testing.T, err error) {
				issue(t, err, "/recipient")
				issue(t, err, "/kind")
			},
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
		{
			name: "a relation name carrying a tilde is escaped in the pointer",
			draft: func(d *ntfy.Draft) {
				d.Links = map[string]string{"a~b": "/v1/tasks/" + strings.Repeat("x", ntfy.DefaultMaxLinkHrefBytes)}
			},
			assert: func(t *testing.T, err error) { issue(t, err, "/links/a~0b") },
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

// TestDraftValidateOversizedLinksSkipsPerLinkChecks proves that a link map
// over the count limit reports only the count issue, not one issue per link on
// top of it. Every link here also fails the scheme check, so a per-link issue
// for each would appear if the per-link loop still ran.
func TestDraftValidateOversizedLinksSkipsPerLinkChecks(t *testing.T) {
	t.Parallel()

	links := make(map[string]string, ntfy.DefaultMaxLinks+1)
	for i := range ntfy.DefaultMaxLinks + 1 {
		links["r"+strconv.Itoa(i)] = "javascript:alert(1)"
	}

	draft := validDraft()
	draft.Links = links

	var validation *ntfy.ValidationError
	require.ErrorAs(t, draft.Validate(), &validation)
	require.Len(t, validation.Issues, 1, "an over-count link map reports only the count issue")
	assert.Equal(t, "/links", validation.Issues[0].Pointer)
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
			name:   "a host-configured scheme is matched whatever its case",
			links:  map[string]string{"task": "mailto:alice@example.test"},
			limits: ntfy.Limits{LinkSchemes: []string{"http", "https", "MAILTO"}},
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

func TestDraftValidateDoesNotRewriteAnAcceptedHref(t *testing.T) {
	t.Parallel()

	href := "https://x.test/a%2Fb?q=1&q=2#frag"

	draft := validDraft()
	draft.Links = map[string]string{"task": href}

	require.NoError(t, draft.Validate())
	assert.Equal(t, href, draft.Links["task"], "validation never normalises an href it accepts")
}

func TestNotificationJSONKeepsOpaqueContentExact(t *testing.T) {
	t.Parallel()

	n := ntfy.Notification{
		ID: "n-1", Recipient: "alice", SourceID: "event-1", Subject: "task-1", SubjectVersion: 3,
		Kind: "offer", State: ntfy.StateActive, Data: json.RawMessage(`{"b":1,"a":2.50}`),
	}

	encoded, err := json.Marshal(n)
	require.NoError(t, err)

	assert.Contains(t, string(encoded), `"data":{"b":1,"a":2.50}`)
	assert.Contains(t, string(encoded), `"state":"ACTIVE"`)
	assert.NotContains(t, string(encoded), `"readAt"`)
}

// TestIdentifiersMustBeWellFormed refuses an identifier holding a NUL byte or
// invalid UTF-8 wherever one is validated. PostgreSQL cannot store either, so
// accepting them would let the same draft succeed on one store and fail with a
// raw driver error on another. The cases do not vary context, so the table has
// no ctx field.
func TestIdentifiersMustBeWellFormed(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		validate func(bad string) error
		pointer  string
	}

	draft := func(set func(d *ntfy.Draft, bad string)) func(string) error {
		return func(bad string) error {
			d := validDraft()
			set(&d, bad)

			return d.Validate()
		}
	}

	closing := func(set func(r *ntfy.CloseRequest, bad string)) func(string) error {
		return func(bad string) error {
			r := ntfy.CloseRequest{
				Subject: "task-1", Version: 1, Reason: "done",
				Successor: &ntfy.Successor{SourceID: "event-2", Kind: "done"},
			}
			set(&r, bad)

			return r.Validate()
		}
	}

	listing := func(set func(q *ntfy.ListQuery, bad string)) func(string) error {
		return func(bad string) error {
			q := ntfy.ListQuery{Recipient: "alice"}
			set(&q, bad)

			return q.Validate()
		}
	}

	cases := []testCase{
		{name: "a draft's recipient", validate: draft(func(d *ntfy.Draft, bad string) { d.Recipient = bad }), pointer: "/recipient"},
		{name: "a draft's source", validate: draft(func(d *ntfy.Draft, bad string) { d.SourceID = bad }), pointer: "/sourceId"},
		{name: "a draft's subject", validate: draft(func(d *ntfy.Draft, bad string) { d.Subject = bad }), pointer: "/subject"},
		{name: "a draft's kind", validate: draft(func(d *ntfy.Draft, bad string) { d.Kind = bad }), pointer: "/kind"},
		{name: "a close's subject", validate: closing(func(r *ntfy.CloseRequest, bad string) { r.Subject = bad }), pointer: "/subject"},
		{
			name:     "a close's kind",
			validate: closing(func(r *ntfy.CloseRequest, bad string) { r.Kinds = []string{"offer", bad} }),
			pointer:  "/kinds/1",
		},
		{name: "a close's exception", validate: closing(func(r *ntfy.CloseRequest, bad string) { r.Except = bad }), pointer: "/except"},
		{
			name:     "a close's successor skip",
			validate: closing(func(r *ntfy.CloseRequest, bad string) { r.SuccessorSkip = []string{bad} }),
			pointer:  "/successorSkip/0",
		},
		{
			name:     "a close's successor source",
			validate: closing(func(r *ntfy.CloseRequest, bad string) { r.Successor.SourceID = bad }),
			pointer:  "/successor/sourceId",
		},
		{
			name:     "a close's successor kind",
			validate: closing(func(r *ntfy.CloseRequest, bad string) { r.Successor.Kind = bad }),
			pointer:  "/successor/kind",
		},
		{name: "a listing's recipient", validate: listing(func(q *ntfy.ListQuery, bad string) { q.Recipient = bad }), pointer: "/recipient"},
		{name: "a listing's subject", validate: listing(func(q *ntfy.ListQuery, bad string) { q.Subject = bad }), pointer: "/subject"},
		{
			name:     "a listing's kind",
			validate: listing(func(q *ntfy.ListQuery, bad string) { q.Kinds = []string{bad} }),
			pointer:  "/kinds/0",
		},
	}

	for _, tc := range cases {
		for _, bad := range []struct{ name, value string }{
			{name: "with a NUL byte", value: "alice\x00"},
			{name: "with invalid UTF-8", value: "alice\xff"},
		} {
			t.Run(tc.name+" "+bad.name, func(t *testing.T) {
				t.Parallel()

				err := tc.validate(bad.value)
				require.ErrorIs(t, err, ntfy.ErrValidation, "%s holding %q is refused", tc.name, bad.value)

				var validation *ntfy.ValidationError
				require.ErrorAs(t, err, &validation)

				pointers := make([]string, 0, len(validation.Issues))
				for _, issue := range validation.Issues {
					pointers = append(pointers, issue.Pointer)
				}

				assert.Contains(t, pointers, tc.pointer)
			})
		}
	}
}
