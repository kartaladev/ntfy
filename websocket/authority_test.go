package websocket_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/websocket"
)

// TestMarkOnFollowedConnection pins the rule that a subscription policy grants
// following only: a connection opened for another recipient may read that
// recipient's signals and may change nothing of theirs.
func TestMarkOnFollowedConnection(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name string
		// message is the mark request alice sends over her connection to bob.
		message func(t *testing.T, s *server) string
	}

	cases := []testCase{
		{
			name:    "mark-all-read",
			message: func(*testing.T, *server) string { return `{"type":"mark-all-read","ref":"r2"}` },
		},
		{
			name: "mark-read naming the followed recipient's notification",
			message: func(t *testing.T, s *server) string {
				return `{"type":"mark-read","ref":"r2","ids":["` + s.idOf(t, "bob") + `"]}`
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := startServer(t, serverConfig{
				ws: []websocket.Option{websocket.WithSubscriptionAuthorizer(ntfy.AllowAll)},
			})

			s.publish(t, "bob", "event-1")
			s.publish(t, "alice", "event-2")

			d, err := s.dial(t, dialRequest{actor: "alice", recipient: "bob"})
			require.NoError(t, err)

			send(t, d.conn, tc.message(t, s))
			reply := string(readRaw(t, d.conn))

			bob, err := s.svc.CountActive(t.Context(), "bob")
			require.NoError(t, err)
			assert.EqualValues(t, 1, bob, "the followed recipient's notifications are unchanged")

			alice, err := s.svc.CountActive(t.Context(), "alice")
			require.NoError(t, err)
			assert.EqualValues(t, 1, alice, "the acting user's own notifications are not marked instead")

			assert.JSONEq(t,
				`{"type":"error","ref":"r2","code":"forbidden",`+
					`"message":"ntfy: unauthorized: a connection following another recipient may not mark notifications read"}`,
				reply)

			// The subscription was legitimately authorized, so the connection
			// stays open and keeps delivering the followed recipient's signals.
			s.publish(t, "bob", "event-3")
			assert.Equal(t, string(ntfy.ChangeCreated), readFrame(t, d.conn)["change"])
		})
	}
}

// TestTransportsGrantTheSameAuthority holds the two transports of one contract
// to the same authority under the same policy: whatever a policy lets a user
// follow, neither transport lets that user change another recipient's
// notifications.
//
// A transport added later that is not driven by this test can reintroduce the
// escalation this change removed — the WebSocket transport had it while the
// HTTP contract did not, and nothing failed. Add the new transport here rather
// than testing its authority on its own.
func TestTransportsGrantTheSameAuthority(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		policy ntfy.SubscriptionAuthorizer
		actor  string
		// assert checks the WebSocket transport's answer to actor connecting
		// for bob and marking bob's notification read.
		assert func(t *testing.T, d dialed, err error, bobs string)
	}

	// refusesTheMark asserts a connection the policy permitted, whose mark
	// request naming bob's notification is refused.
	refusesTheMark := func(t *testing.T, d dialed, err error, bobs string) {
		t.Helper()

		upgraded(t, d, err)

		send(t, d.conn, `{"type":"mark-read","ref":"r1","ids":["`+bobs+`"]}`)
		assert.Equal(t, "forbidden", readFrame(t, d.conn)["code"],
			"the WebSocket transport refuses the same mark")
	}

	cases := []testCase{
		{
			name:   "the default policy follows nobody else",
			policy: ntfy.SelfOnly,
			actor:  "alice",
			assert: func(t *testing.T, d dialed, err error, _ string) {
				refused(http.StatusForbidden, "forbidden")(t, d, err)
			},
		},
		{name: "every subscription permitted", policy: ntfy.AllowAll, actor: "alice", assert: refusesTheMark},
		{name: "a supervisor policy", policy: supervisorPolicy, actor: "sup", assert: refusesTheMark},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := startServer(t, serverConfig{
				ws:  []websocket.Option{websocket.WithSubscriptionAuthorizer(tc.policy)},
				sse: []ntfy.HandlerOption{ntfy.WithSubscriptionAuthorizer(tc.policy)},
			})

			s.publish(t, "bob", "event-1")
			s.publish(t, tc.actor, "event-2")

			bobs := s.idOf(t, "bob")

			// The HTTP transport: mark bob's notification, named over the contract.
			assert.Equal(t, http.StatusNotFound, s.markReadOverHTTP(t, tc.actor, bobs),
				"the HTTP transport refuses to mark another recipient's notification")

			// The WebSocket transport: the same mark, over a connection that
			// follows bob as far as the policy allows.
			d, err := s.dial(t, dialRequest{actor: tc.actor, recipient: "bob"})
			tc.assert(t, d, err, bobs)

			for _, recipient := range []string{"bob", tc.actor} {
				count, err := s.svc.CountActive(t.Context(), recipient)
				require.NoError(t, err)
				assert.EqualValues(t, 1, count, "%s's notifications are unchanged", recipient)
			}
		})
	}
}

// markReadOverHTTP marks a notification read over the HTTP contract as actor,
// and returns the status the contract answered with.
func (s *server) markReadOverHTTP(t *testing.T, actor, id string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.url+"/v1/notifications/"+id+"/read", http.NoBody)
	require.NoError(t, err)

	req.Header.Set(actorHeader, actor)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode
}
