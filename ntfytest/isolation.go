package ntfytest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
)

// isolationContent returns a link map and a payload, freshly allocated on every
// call, so that a case mutating what it supplied cannot reach another case.
func isolationContent() (map[string]string, json.RawMessage) {
	return map[string]string{"task": "/v1/tasks/task-1"}, json.RawMessage(`{"by":"carol"}`)
}

// isolationClose closes task-1's offers with a successor carrying links and a
// payload the caller still holds.
func isolationClose(links map[string]string, data json.RawMessage) ntfy.CloseRequest {
	return ntfy.CloseRequest{
		Subject: "task-1", Kinds: []string{"offer"}, Version: 5, Reason: "taken",
		Successor: &ntfy.Successor{
			SourceID: "event-5", Kind: "taken", Title: "Taken by carol", SubjectVersion: 5,
			Links: links, Data: data,
		},
	}
}

// hijack mutates links and a payload in place. It first requires that there is
// something to mutate, so that a store which drops content fails the case
// instead of panicking the host's whole test binary.
func hijack(t *testing.T, links map[string]string, data json.RawMessage) {
	t.Helper()

	require.NotNil(t, links, "the notification carries its links")
	require.GreaterOrEqual(t, len(data), 3, "the notification carries its payload")

	links["task"] = "/hijacked"
	data[2] = 'X'
}

// hijacked asserts that hijack took effect on the copy it was applied to, so a
// case cannot pass by mutating nothing.
func hijacked(t *testing.T, links map[string]string, data json.RawMessage) {
	t.Helper()

	assert.Equal(t, "/hijacked", links["task"], "the mutated links did change")
	assert.JSONEq(t, `{"Xy":"carol"}`, string(data), "the mutated payload did change")
}

// intact asserts that links and a payload still carry what isolationContent
// supplied.
func intact(t *testing.T, what string, links map[string]string, data json.RawMessage) {
	t.Helper()

	assert.Equal(t, "/v1/tasks/task-1", links["task"], "%s keeps its links", what)
	assert.JSONEq(t, `{"by":"carol"}`, string(data), "%s keeps its payload", what)
}

// runIsolation asserts that a store shares no memory with its caller: nothing a
// caller mutates after a call changes what the store holds or what it already
// returned, and no two notifications one call returns share a map or a payload.
//
// Every case mutates a value and then asserts that another is unchanged, rather
// than comparing pointers: the contract promises non-interference, not distinct
// object graphs. Each case also asserts that its mutation took effect on the
// copy it was applied to, so a case cannot pass by mutating nothing.
func runIsolation(t *testing.T, factory Factory) {
	parallel(t, "mutating a close request does not change its successors", func(t *testing.T) {
		e := newEnv(t, factory)
		e.insert(false, e.note("alice", "event-1", "task-1", "offer", 1, at(0)))

		links, data := isolationContent()

		result := e.close(isolationClose(links, data), at(4))
		require.Len(t, result.Successors, 1)

		hijack(t, links, data)

		successor := result.Successors[0]
		intact(t, "the reported successor", successor.Links, successor.Data)

		stored := e.get("alice", successor.ID)
		intact(t, "the stored successor", stored.Links, stored.Data)

		hijacked(t, links, data)
	})

	parallel(t, "successors to different recipients are independent", func(t *testing.T) {
		e := newEnv(t, factory)
		e.insert(false,
			e.note("alice", "event-1", "task-1", "offer", 1, at(0)),
			e.note("bob", "event-2", "task-1", "offer", 1, at(0)))

		links, data := isolationContent()

		result := e.close(isolationClose(links, data), at(4))
		require.Len(t, result.Successors, 2)

		reported := map[string]ntfy.Notification{}
		for _, n := range result.Successors {
			reported[n.Recipient] = n
		}

		require.Contains(t, reported, "alice")
		require.Contains(t, reported, "bob")

		alice, bob := reported["alice"], reported["bob"]
		hijack(t, alice.Links, alice.Data)

		intact(t, "bob's reported successor", bob.Links, bob.Data)

		storedBob := e.get("bob", bob.ID)
		intact(t, "bob's stored successor", storedBob.Links, storedBob.Data)

		storedAlice := e.get("alice", alice.ID)
		intact(t, "alice's stored successor", storedAlice.Links, storedAlice.Data)

		hijacked(t, alice.Links, alice.Data)
	})

	parallel(t, "mutating an insertion does not change what a store keeps or reported", func(t *testing.T) {
		e := newEnv(t, factory)

		n := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
		n.Links, n.Data = isolationContent()

		result := e.insert(false, n)
		require.Len(t, result.Created, 1)

		hijack(t, n.Links, n.Data)

		intact(t, "the reported notification", result.Created[0].Links, result.Created[0].Data)

		stored := e.get("alice", n.ID)
		intact(t, "the stored notification", stored.Links, stored.Data)

		hijacked(t, n.Links, n.Data)
	})

	parallel(t, "a reported notification is independent of the store and of its siblings", func(t *testing.T) {
		e := newEnv(t, factory)

		links, data := isolationContent()

		alice := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
		bob := e.note("bob", "event-2", "task-1", "offer", 1, at(0))
		alice.Links, alice.Data = links, data
		bob.Links, bob.Data = links, data

		result := e.insert(false, alice, bob)
		require.Len(t, result.Created, 2)

		first, second := result.Created[0], result.Created[1]
		hijack(t, first.Links, first.Data)

		intact(t, "the other reported notification", second.Links, second.Data)

		stored := e.get(first.Recipient, first.ID)
		intact(t, "the stored copy of the mutated notification", stored.Links, stored.Data)

		hijacked(t, first.Links, first.Data)
	})

	type readCase struct {
		name string
		// read is the first read, whose result the case mutates.
		read func(t *testing.T, e *env, n ntfy.Notification) ntfy.Notification
	}

	reads := []readCase{
		{
			name: "a notification read twice is independent between reads",
			read: func(_ *testing.T, e *env, n ntfy.Notification) ntfy.Notification {
				return e.get(n.Recipient, n.ID)
			},
		},
		{
			name: "a listed notification is independent of a read",
			read: func(t *testing.T, e *env, n ntfy.Notification) ntfy.Notification {
				page := e.list(ntfy.ListQuery{Recipient: n.Recipient})
				require.Len(t, page.Notifications, 1)

				return page.Notifications[0]
			},
		},
	}

	for _, rc := range reads {
		parallel(t, rc.name, func(t *testing.T) {
			e := newEnv(t, factory)

			n := e.note("alice", "event-1", "task-1", "offer", 1, at(0))
			n.Links, n.Data = isolationContent()
			e.insert(false, n)
			e.markRead("alice", at(1), n.ID)

			first := rc.read(t, e, n)
			hijack(t, first.Links, first.Data)
			require.NotNil(t, first.ReadAt, "a read notification carries when it was read")
			*first.ReadAt = at(99)

			second := e.get("alice", n.ID)
			intact(t, "a later read", second.Links, second.Data)
			sameInstant(t, at(1), second.ReadAt, "a later read's read-at")

			hijacked(t, first.Links, first.Data)
		})
	}
}
