package websocket_test

import (
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
