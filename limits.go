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
// [New] refuses, with a [ConfigurationError]: a limit that is not positive; a
// LinkSchemes that names no scheme, an empty slice rather than nil, when a
// host means to keep the default schemes; a LinkSchemes containing an empty
// scheme name; and naming link schemes while also setting AnyLinkScheme.
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

// snapshot copies every pointer field and the scheme slice, so that a host
// mutating the [Limits] value it gave [WithLimits] — or the slice or the
// values a pointer field points at — after construction cannot change an
// already-built service's policy.
func (l Limits) snapshot() Limits {
	l.MaxTitleBytes = clonedLimit(l.MaxTitleBytes)
	l.MaxDataBytes = clonedLimit(l.MaxDataBytes)
	l.MaxLinks = clonedLimit(l.MaxLinks)
	l.MaxLinkRelationBytes = clonedLimit(l.MaxLinkRelationBytes)
	l.MaxLinkHrefBytes = clonedLimit(l.MaxLinkHrefBytes)
	l.MaxDraftsPerPublish = clonedLimit(l.MaxDraftsPerPublish)
	l.MaxFilterValues = clonedLimit(l.MaxFilterValues)
	l.LinkSchemes = slices.Clone(l.LinkSchemes)

	return l
}

// clonedLimit copies a limit pointer, or returns nil for one that is unset.
func clonedLimit(value *int) *int {
	if value == nil {
		return nil
	}

	return Limit(*value)
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
