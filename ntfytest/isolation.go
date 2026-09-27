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

// runIsolation asserts that a store shares no memory with its caller: nothing a
// caller mutates after a call changes what the store holds or what it already
// returned, and no two notifications one call returns share a map or a payload.
//
// Every case mutates a value and then asserts that another is unchanged, rather
// than comparing pointers: the contract promises non-interference, not distinct
// object graphs. Each case also asserts that its mutation took effect on the
// caller's own copy, so a case cannot pass by mutating nothing.
func runIsolation(t *testing.T, factory Factory) {
	parallel(t, "mutating a close request does not change its successors", func(t *testing.T) {
		e := newEnv(t, factory)
		e.insert(false, e.note("alice", "event-1", "task-1", "offer", 1, at(0)))

		links, data := isolationContent()
		req := isolationClose(links, data)

		result := e.close(req, at(4))
		require.Len(t, result.Successors, 1)

		links["task"] = "/hijacked"
		data[2] = 'X'

		assert.Equal(t, "/v1/tasks/task-1", result.Successors[0].Links["task"],
			"the reported successor keeps the links the close carried")
		assert.JSONEq(t, `{"by":"carol"}`, string(result.Successors[0].Data))

		stored := e.get("alice", result.Successors[0].ID)
		assert.Equal(t, "/v1/tasks/task-1", stored.Links["task"])
		assert.JSONEq(t, `{"by":"carol"}`, string(stored.Data))

		assert.Equal(t, "/hijacked", links["task"], "the caller's own map did change")
		assert.JSONEq(t, `{"Xy":"carol"}`, string(data), "the caller's own payload did change")
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

		reported["alice"].Links["task"] = "/hijacked"
		reported["alice"].Data[2] = 'X'

		assert.Equal(t, "/v1/tasks/task-1", reported["bob"].Links["task"],
			"bob's successor keeps its own links")
		assert.JSONEq(t, `{"by":"carol"}`, string(reported["bob"].Data))
		assert.Equal(t, "/v1/tasks/task-1", e.get("bob", reported["bob"].ID).Links["task"])

		assert.Equal(t, "/hijacked", reported["alice"].Links["task"], "the mutated copy did change")
	})
}
