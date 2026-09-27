package ntfy

import (
	"encoding/json"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// State is where a notification is in its life. A notification never returns
// to [StateActive] once it has left it.
type State string

// The three states.
const (
	// StateActive is a notification its recipient has not read and nothing has
	// closed.
	StateActive State = "ACTIVE"
	// StateRead is a notification its recipient has read.
	StateRead State = "READ"
	// StateClosed is a notification whose subject moved on. Its ClosedReason
	// says why.
	StateClosed State = "CLOSED"
)

// Valid reports whether s is one of the three states.
func (s State) Valid() bool {
	switch s {
	case StateActive, StateRead, StateClosed:
		return true
	default:
		return false
	}
}

// The identifier limits every store enforces. They are the widths of the SQL
// store's columns, applied everywhere so that a draft accepted in memory is
// accepted in SQL too. A draft's title, links and payload are bounded
// separately, by [Limits], because their columns are unbounded text.
const (
	// MaxKindBytes is the longest kind.
	MaxKindBytes = 100
	// MaxIdentifierBytes is the longest recipient, source and subject.
	MaxIdentifierBytes = 255
)

// Notification is one notification for one recipient.
//
// Kind, Title, Links and Data belong to the publisher. The library stores them
// and returns them exactly as published, and interprets none of them.
type Notification struct {
	// ID identifies the notification.
	ID string `json:"id"`
	// Recipient is who the notification is for.
	Recipient string `json:"recipient"`
	// SourceID identifies what produced the notification, such as an event.
	// Together with Recipient it makes publishing idempotent.
	SourceID string `json:"sourceId"`
	// Subject is what the notification is about.
	Subject string `json:"subject"`
	// SubjectVersion is the subject's version the notification describes.
	SubjectVersion int64 `json:"subjectVersion"`
	// Kind is the publisher's classification, such as "offer".
	Kind string `json:"kind"`
	// State is where the notification is in its life.
	State State `json:"state"`
	// ClosedReason says why a CLOSED notification was closed.
	ClosedReason string `json:"closedReason,omitempty"`
	// Title is a line a client can show.
	Title string `json:"title,omitempty"`
	// Links are hrefs by relation name, such as "task".
	Links map[string]string `json:"links,omitempty"`
	// Data is the publisher's JSON payload, byte for byte.
	Data json.RawMessage `json:"data,omitempty"`
	// CreatedAt is when the notification was published, UTC at microsecond
	// precision.
	CreatedAt time.Time `json:"createdAt"`
	// ReadAt is when the recipient first read it.
	ReadAt *time.Time `json:"readAt,omitempty"`
	// ClosedAt is when it was closed.
	ClosedAt *time.Time `json:"closedAt,omitempty"`
	// InactiveAt is when it first left ACTIVE, by being read or closed. The age
	// retention bound is measured from it.
	InactiveAt *time.Time `json:"inactiveAt,omitempty"`
}

// Clone returns a copy that shares no links, data or times with n, so that a
// store and its caller cannot change each other's copy.
func (n Notification) Clone() Notification {
	n.Links = maps.Clone(n.Links)
	n.Data = slices.Clone(n.Data)
	n.ReadAt = clonePtr(n.ReadAt)
	n.ClosedAt = clonePtr(n.ClosedAt)
	n.InactiveAt = clonePtr(n.InactiveAt)

	return n
}

// timePtr returns a pointer to a copy of an instant.
func timePtr(instant time.Time) *time.Time { return &instant }

// clonePtr copies an optional instant.
func clonePtr(instant *time.Time) *time.Time {
	if instant == nil {
		return nil
	}

	return timePtr(*instant)
}

// Draft is what a publisher supplies to create a notification. The library
// assigns the identifier, the creation time and the state.
type Draft struct {
	// Recipient is who the notification is for. Required.
	Recipient string
	// SourceID identifies what produced it. Required.
	SourceID string
	// Subject is what it is about. Required.
	Subject string
	// Kind is the publisher's classification. Required.
	Kind string
	// Title is a line a client can show.
	Title string
	// SubjectVersion is the subject's version it describes. It must not be
	// negative.
	SubjectVersion int64
	// Links are hrefs by relation name.
	Links map[string]string
	// Data is a JSON payload, stored byte for byte.
	Data json.RawMessage
	// Coalesce creates nothing when the recipient already has an ACTIVE or READ
	// notification of this kind on this subject.
	Coalesce bool
}

// Validate reports every problem with the draft as a [ValidationError], or nil.
// It validates content against [DefaultLimits]; [Draft.ValidateWithin]
// validates against a service's configured limits.
func (d Draft) Validate() error { return d.ValidateWithin(Limits{}) }

// ValidateWithin reports every problem with the draft, bounding its content by
// limits. An unset limit keeps its default. It expects limits [New] would
// accept; behaviour is unspecified for one [New] would refuse to construct
// with.
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

	if limit := limits.maxTitleBytes(); len(c.title) > limit {
		issues = append(issues, ValidationIssue{
			Pointer: "/title", Detail: "is longer than " + strconv.Itoa(limit) + " bytes",
		})
	}

	issues = append(issues, validateLinks(c.links, limits)...)

	// The size is checked first, so that an oversized payload is refused
	// without scanning it for well-formed JSON.
	switch limit := limits.maxDataBytes(); {
	case len(c.data) > limit:
		issues = append(issues, ValidationIssue{
			Pointer: "/data", Detail: "is longer than " + strconv.Itoa(limit) + " bytes",
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

	// An over-count map is reported once, and never scanned link by link: a
	// count issue on top of one per link would report thousands of issues for
	// one oversized publish.
	if limit := limits.maxLinks(); len(links) > limit {
		return []ValidationIssue{{
			Pointer: "/links", Detail: "carries more than " + strconv.Itoa(limit) + " links",
		}}
	}

	var issues []ValidationIssue

	for _, relation := range slices.Sorted(maps.Keys(links)) {
		pointer := "/links/" + escapePointer(relation)

		if limit := limits.maxLinkRelationBytes(); len(relation) > limit {
			issues = append(issues, ValidationIssue{
				Pointer: pointer, Detail: "has a relation name longer than " + strconv.Itoa(limit) + " bytes",
			})
		}

		if limit := limits.maxLinkHrefBytes(); len(links[relation]) > limit {
			issues = append(issues, ValidationIssue{
				Pointer: pointer, Detail: "has an href longer than " + strconv.Itoa(limit) + " bytes",
			})

			continue
		}

		if detail := checkScheme(links[relation], limits); detail != "" {
			issues = append(issues, ValidationIssue{Pointer: pointer, Detail: detail})
		}
	}

	return issues
}

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

	if slices.ContainsFunc(limits.linkSchemes(), func(scheme string) bool {
		return strings.EqualFold(scheme, parsed.Scheme)
	}) {
		return ""
	}

	return "has an href using the " + strings.ToLower(parsed.Scheme) + " scheme, which is not permitted"
}

// escapePointer escapes a relation name for an RFC 6901 JSON Pointer.
func escapePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

// validateIdentifier checks a required identifier and its length.
func validateIdentifier(pointer, value string) []ValidationIssue {
	switch {
	case value == "":
		return []ValidationIssue{{Pointer: pointer, Detail: "is required"}}
	case len(value) > MaxIdentifierBytes:
		return []ValidationIssue{{
			Pointer: pointer, Detail: "is longer than " + strconv.Itoa(MaxIdentifierBytes) + " bytes",
		}}
	default:
		return nil
	}
}
