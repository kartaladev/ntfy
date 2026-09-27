package ntfytest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// identityVariant is a pair of identifiers that differ only in their bytes, in
// a way some collation ignores.
type identityVariant struct {
	name  string
	of    string // the identifier published
	other string // the identifier that must not match it
}

// identityVariants returns the ways an identifier can differ from another only
// in its bytes. Every string is valid UTF-8 without NUL, so every supported
// database can store it; NUL is covered by sqlstore's MySQL test instead.
func identityVariants(identifier string) []identityVariant {
	return []identityVariant{
		{name: "differing in case", of: identifier, other: strings.ToUpper(identifier[:1]) + identifier[1:]},
		{name: "with a trailing space", of: identifier, other: identifier + " "},
		{name: "with a zero-width space", of: identifier, other: identifier + "\u200b"},
		{name: "in another normalisation form", of: identifier + "-jos\u00e9", other: identifier + "-jose\u0301"},
	}
}

// runIdentity asserts that a store compares recipient, source, subject and kind
// byte for byte. The cases do not vary context, so the table has no ctx field.
func runIdentity(t *testing.T, factory Factory) {
	type testCase struct {
		name    string
		variant identityVariant
		assert  func(t *testing.T, e *env, v identityVariant)
	}

	var cases []testCase

	for _, group := range []struct {
		identifier string
		assert     func(t *testing.T, e *env, v identityVariant)
	}{
		{identifier: "alice", assert: recipientIsItsBytes},
		{identifier: "event-1", assert: sourceIsItsBytes},
		{identifier: "task-1", assert: subjectIsItsBytes},
		{identifier: "offer", assert: kindIsItsBytes},
	} {
		for _, v := range identityVariants(group.identifier) {
			cases = append(cases, testCase{name: group.identifier + " " + v.name, variant: v, assert: group.assert})
		}
	}

	for _, tc := range cases {
		parallel(t, tc.name, func(t *testing.T) {
			tc.assert(t, newEnv(t, factory), tc.variant)
		})
	}
}

// recipientIsItsBytes: another recipient reads, lists, counts and marks
// nothing of the published recipient's.
func recipientIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	published := e.note(v.of, "event-1", "task-1", "offer", 1, at(0))
	e.insert(false, published)

	_, err := e.store.Get(t.Context(), v.other, published.ID)
	require.ErrorIsf(t, err, ntfy.ErrNotFound, "reading as %+q finds %+q's notification", v.other, v.of)

	assert.Emptyf(t, e.list(ntfy.ListQuery{Recipient: v.other}).Notifications, "%+q lists %+q's notification", v.other, v.of)
	assert.Zerof(t, e.count(v.other), "%+q counts %+q's notification", v.other, v.of)

	_, err = e.store.MarkRead(t.Context(), v.other, []string{published.ID}, at(1))
	require.ErrorIsf(t, err, ntfy.ErrNotFound, "%+q marks %+q's notification read", v.other, v.of)

	stored := e.get(v.of, published.ID)
	assert.Equal(t, ntfy.StateActive, stored.State, "the published recipient's notification is untouched")
	assert.Equal(t, v.of, stored.Recipient, "the recipient is returned exactly as published")
}

// sourceIsItsBytes: a source differing only in its bytes is a new source, not
// a redelivery.
func sourceIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	require.Len(t, e.insert(false, e.note("alice", v.of, "task-1", "offer", 1, at(0))).Created, 1)

	result := e.insert(false, e.note("alice", v.other, "task-1", "offer", 1, at(1)))
	assert.Lenf(t, result.Created, 1, "%+q is taken for a redelivery of %+q", v.other, v.of)
	assert.Zero(t, result.Duplicates)
}

// subjectIsItsBytes: closing a subject leaves the one differing only in its
// bytes open, and does not suppress a later publish on it.
func subjectIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	closing := e.note("alice", "event-1", v.of, "offer", 1, at(0))
	open := e.note("alice", "event-2", v.other, "offer", 1, at(1))
	e.insert(false, closing)
	e.insert(false, open)

	e.close(ntfy.CloseRequest{Subject: v.of, Version: 5, Reason: "done"}, at(2))

	assert.Equal(t, ntfy.StateClosed, e.get("alice", closing.ID).State)
	assert.Equalf(t, ntfy.StateActive, e.get("alice", open.ID).State, "closing %+q closed %+q", v.of, v.other)

	late := e.insert(false, e.note("alice", "event-3", v.other, "offer", 2, at(3)))
	assert.Lenf(t, late.Created, 1, "closing %+q suppressed a publish on %+q", v.of, v.other)
	assert.Zero(t, late.Suppressed)
}

// kindIsItsBytes: a kind filter matches its kind exactly.
func kindIsItsBytes(t *testing.T, e *env, v identityVariant) {
	t.Helper()

	wanted := e.note("alice", "event-1", "task-1", v.of, 1, at(0))
	e.insert(false, wanted, e.note("alice", "event-2", "task-1", v.other, 1, at(1)))

	listed := e.list(ntfy.ListQuery{Recipient: "alice", Kinds: []string{v.of}}).Notifications
	assert.Equalf(t, []string{wanted.ID}, idsOf(listed), "a filter on %+q returned %+q too", v.of, v.other)
}
