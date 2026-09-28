# End Broadcaster Listen With Its Connection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the NATS broadcaster's `Listen` end with an error once its connection is closed for good, so the hub stops reporting ready and closes its streams. Also stop the Redis cancel test from flaking, and make `ntfytest.RunBroadcasterSuite` check delivery itself.

**Architecture:**
- `nats.Broadcaster.Listen` registers a `CLOSED` status listener on the host's connection before subscribing, and returns an error wrapping `natsgo.ErrConnectionClosed` when it fires. The host's reconnect budget (`natsgo.MaxReconnects`) is the override.
- The core `Broadcaster` contract and `Hub.Running` godocs state the rule for every broadcaster.
- The Redis cancel case waits for the server to drop the subscription. The Redis code does not change.
- `RunBroadcasterSuite` gains three delivery cases. A child-process test proves that they reject three broken broadcasters.

**Tech Stack:**
- Go 1.26, with the core module on the standard library only.
- nats.go v1.53.1 and go-redis v9.
- `stretchr/testify`, `go.uber.org/goleak`, and `testcontainers-go` through `nats.RunTestNATS` and `redis.RunTestRedis`.
- `golangci-lint` v2 and `openspec`.

**Spec:** `openspec/changes/end-broadcaster-listen-with-its-connection/`. Read:
- `proposal.md`: why, and the failing audit output;
- `design.md`: D1 the CLOSED listener, D2 the core godocs, D3 the Redis test, D4 the suite cases, D5 the child-process proof;
- `specs/notification-realtime/spec.md`: two MODIFIED requirements, with the new scenarios this plan must satisfy.

## Global Constraints

- **Go 1.26: export `GOTOOLCHAIN=go1.26.8` before any Go command.** A newer Go may be first on `PATH`. The `make` targets already pin it.
- **The core module imports only the standard library** in production code. Tests may use `testify`, `goleak` and `go.uber.org/mock`. This change adds no production import to the core. `.golangci.yml` depguard and `make split-check` enforce it.
- **Each satellite module may import only `ntfy`, `sqlkit` and its own client library.** `ntfy/nats` uses nats.go and `ntfy/redis` uses go-redis. `ntfytest` imports only `ntfy` and testify. `make split-check` is authoritative.
- **Tests follow the `table-test` skill:**
  - an `assert` closure on every case, never `want`/`wantErr` fields;
  - `t.Context()`, not `context.Background()`, except where an existing helper deliberately outlives the test's context;
  - `require` only for preconditions.
  - These tables do not vary context, so they carry no `ctx` field. Each such table says so in a one-line comment.
- **Test doubles come from the `use-mockgen` skill.** This change needs none. `brokenBroadcaster` is a deliberately broken *implementation under test*, not a double for a collaborator (`design.md` D5).
- **External services come from the `use-testcontainers` skill.** Use `nats.RunTestNATS(t, nats.WithTestURL(&url))` and `redis.RunTestRedis(t)`, and never write a new container helper. Reuse `startProxy` (`nats/reconnect_test.go`) to take a server away.
- **`.claude/rules/prove-errors-with-tests.md`:** run each red step and record its failing output before any production edit. A red caused by compilation, a missing fixture or a container error does not count.
- **`.claude/rules/golang-tdd.md`:** red → green → refactor, and the test lands in the same commit as the code that satisfies it.
- **`.claude/rules/library-design.md`:**
  - D1's default is "a connection closed for good ends `Listen`", and the override is the host's own `natsgo.MaxReconnects`. Add no broadcaster option to keep listening on a closed connection.
  - D4's cases are the contract, with no override.
- **`.claude/rules/performance-benchmark.md`:** this change makes no performance claim, so it needs no benchmark.
- **`.claude/rules/plans-beside-tasks.md`:** mirror any edit to `tasks.md` here in the same turn.
- **`.claude/rules/gopls-navigation.md`:** navigate Go with `gopls` (`$(go env GOPATH)/bin/gopls` if it is not on `PATH`).
- **Done means `make all` and `make store-matrix` pass.** The store suite lives in the `ntfytest` module, which changes.
- **Commit messages** are imperative sentence case with no `feat:`/`fix:` prefix, matching `git log`, and end with the session's attribution lines.

## File Structure

| File | Module | Responsibility |
| --- | --- | --- |
| `nats/closed_test.go` | `ntfy/nats` | **Create.** `TestListenEndsWhenTheConnectionCloses` (table of 3 cases) and `TestHubStopsReceivingWhenTheNATSConnectionCloses`. |
| `nats/broadcaster.go:167-225` | `ntfy/nats` | **Modify.** `Listen`: the CLOSED status listener, a new `select` case, and the godoc. |
| `nats/doc.go` | `ntfy/nats` | **Modify.** One sentence on a connection closed for good. |
| `realtime.go:53-68` | `ntfy` | **Modify.** The `Broadcaster.Listen` contract godoc. |
| `hub.go:296-303` | `ntfy` | **Modify.** The `Hub.Running` godoc. |
| `redis/listen_test.go:145-165` | `ntfy/redis` | **Modify.** The cancel case waits with `require.Eventuallyf`, and is renamed. |
| `redis/broadcaster.go:157-174` | `ntfy/redis` | **Modify.** The `Listen` godoc no longer says "unsubscribes". |
| `ntfytest/broadcaster_test.go` | `ntfy/ntfytest` | **Create.** `brokenBroadcaster`, `TestBroadcasterSuiteChild`, `TestBroadcasterSuiteRejectsBrokenBroadcasters`. |
| `ntfytest/broadcaster.go` | `ntfy/ntfytest` | **Modify.** `BroadcasterBatch`, `everyListenerSignals`, `snapshot`, `awaitCount`, three cases, and the suite godoc. |
| `ntfytest/doc.go` | `ntfy/ntfytest` | **Modify.** The package doc names the broadcaster suite's delivery properties. |
| `nats/docs_test.go` | `ntfy/nats` | **Modify.** Two more strings the guide must state. |
| `docs/realtime-operations.md` | — | **Modify.** A new section, "When a broker connection closes for good". |
| `docs/notifications.md:186-197` | — | **Modify.** The `Running` bullet, and what the suite checks. |

**Shared-file note:** `ntfytest/broadcaster.go`'s case list in `RunBroadcasterSuite` is a registration point. A change in flight that also adds a broadcaster case conflicts on adjacent lines; resolve by keeping both.

## Mapping to `tasks.md`

| Plan task | `tasks.md` |
| --- | --- |
| Task 1: NATS `Listen` ends with its connection | 1.1, 1.2 |
| Task 2: the contract says it for every broadcaster | 1.3 |
| Task 3: the Redis cancel case waits for the server | 2.1, 2.2 |
| Task 4: the broadcaster suite checks delivery | 3.1, 3.2, 3.3 |
| Task 5: documentation | 4.1, 4.2 |
| Task 6: verify and hand off | 5.1, 5.2 |

---

### Task 1: NATS `Listen` ends with its connection (tasks 1.1, 1.2)

**Files:**
- Create: `nats/closed_test.go`
- Modify: `nats/broadcaster.go:167-225`

**Interfaces:**
- Consumes (existing, package `nats_test`):
  - `nats.RunTestNATS(t *testing.T, opts ...nats.TestOption) *natsgo.Conn` and `nats.WithTestURL(url *string) nats.TestOption`;
  - `startProxy(t *testing.T, target string) *proxy`, with `(*proxy).url() string`, `(*proxy).dropAll()` and the field `listener net.Listener` (`nats/reconnect_test.go`);
  - `listen(t *testing.T, conn *natsgo.Conn, subject string) *listening`, whose `*listening` has `done chan error` (buffered 1) and `cancel context.CancelFunc`, and `const testWait = 10 * time.Second` (`nats/listen_test.go`);
  - `ntfy.NewHub(b ntfy.Broadcaster, opts ...ntfy.HubOption) (*ntfy.Hub, error)`, `(*Hub).Run(ctx) error`, `Ready() <-chan struct{}`, `Running() bool`, `Subscribe(recipient string) (*ntfy.Subscription, error)`, `(*Subscription).Done() <-chan struct{}`, `Close()`; `ntfy.ErrUnavailable`;
  - nats.go: `(*natsgo.Conn).StatusChanged(statuses ...natsgo.Status) chan natsgo.Status`, `RemoveStatusListener(ch chan natsgo.Status)`, `IsClosed() bool`, `IsReconnecting() bool`, `natsgo.CLOSED`, `natsgo.ErrConnectionClosed`.
- Produces: `const closeWait = 5 * time.Second` in `nats_test`. `Listen`'s signature is unchanged: `func (b *Broadcaster) Listen(ctx context.Context, deliver func(ntfy.Signal), ready func()) error`.

- [ ] **Step 1: Write the failing tests**

Create `nats/closed_test.go`:

```go
package nats_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/nats"
)

// closeWait bounds how long a connection closed for good may take to end
// Listen. nats.go reports the close at once; the bound turns a hang into a
// failure.
const closeWait = 5 * time.Second

// TestListenEndsWhenTheConnectionCloses holds Listen to ending once its
// connection is closed for good, and to running on while the connection is
// still reconnecting. Each case reaches the server through a proxy of its own,
// so taking the server away from one case leaves the others alone. The cases
// do not vary context, so the table has no ctx field.
func TestListenEndsWhenTheConnectionCloses(t *testing.T) {
	t.Parallel()

	var serverURL string

	nats.RunTestNATS(t, nats.WithTestURL(&serverURL))

	type testCase struct {
		name string
		// connect is what the host dials with.
		connect []natsgo.Option
		// act takes the server away from the connection, or closes it.
		act    func(t *testing.T, p *proxy, conn *natsgo.Conn)
		assert func(t *testing.T, conn *natsgo.Conn, l *listening)
	}

	outage := func(t *testing.T, p *proxy, _ *natsgo.Conn) {
		t.Helper()

		require.NoError(t, p.listener.Close())
		p.dropAll()
	}

	endsClosed := func(t *testing.T, conn *natsgo.Conn, l *listening) {
		t.Helper()

		select {
		case err := <-l.done:
			require.ErrorIs(t, err, natsgo.ErrConnectionClosed)
			assert.True(t, conn.IsClosed(), "Listen ended because its connection closed")
		case <-time.After(closeWait):
			require.FailNowf(t, "Listen is still running",
				"the connection is CLOSED (%v), yet Listen is still running %s later", conn.IsClosed(), closeWait)
		}
	}

	cases := []testCase{
		{
			name:    "the connection gives up reconnecting",
			connect: []natsgo.Option{natsgo.ReconnectWait(50 * time.Millisecond), natsgo.MaxReconnects(2)},
			act:     outage,
			assert:  endsClosed,
		},
		{
			name: "the host closes the connection",
			act: func(_ *testing.T, _ *proxy, conn *natsgo.Conn) {
				conn.Close()
			},
			assert: endsClosed,
		},
		{
			name:    "a connection reconnecting without limit keeps listening",
			connect: []natsgo.Option{natsgo.ReconnectWait(50 * time.Millisecond), natsgo.MaxReconnects(-1)},
			act:     outage,
			assert: func(t *testing.T, conn *natsgo.Conn, l *listening) {
				// Longer than the two-attempt case above takes to give up.
				select {
				case err := <-l.done:
					require.FailNowf(t, "Listen ended while its connection was still reconnecting", "%v", err)
				case <-time.After(time.Second):
				}

				assert.True(t, conn.IsReconnecting(), "the connection is still trying")
			},
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := startProxy(t, strings.TrimPrefix(serverURL, "nats://"))

			conn, err := natsgo.Connect(p.url(), tc.connect...)
			require.NoError(t, err)
			t.Cleanup(conn.Close)

			l := listen(t, conn, fmt.Sprintf("test.closed.%d", i))

			tc.act(t, p, conn)
			tc.assert(t, conn, l)
		})
	}
}

// TestHubStopsReceivingWhenTheNATSConnectionCloses proves what a closed
// connection means for an instance: its hub stops running, closes the streams
// it holds and refuses new ones, instead of accepting streams it can never feed.
func TestHubStopsReceivingWhenTheNATSConnectionCloses(t *testing.T) {
	t.Parallel()

	var serverURL string

	nats.RunTestNATS(t, nats.WithTestURL(&serverURL))

	conn, err := natsgo.Connect(serverURL)
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	b, err := nats.NewBroadcaster(conn, nats.WithSubject("test.closed.hub"))
	require.NoError(t, err)

	hub, err := ntfy.NewHub(b)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	runErr := make(chan error, 1)

	go func() { runErr <- hub.Run(ctx) }()

	select {
	case <-hub.Ready():
	case err := <-runErr:
		require.FailNowf(t, "the hub stopped before it was ready", "%v", err)
	case <-time.After(testWait):
		require.FailNow(t, "the hub never became ready")
	}

	alice, err := hub.Subscribe("alice")
	require.NoError(t, err)
	t.Cleanup(alice.Close)

	conn.Close()

	select {
	case err := <-runErr:
		require.ErrorIs(t, err, natsgo.ErrConnectionClosed)
	case <-time.After(closeWait):
		require.FailNowf(t, "the hub is still running",
			"the connection is CLOSED, yet hub.Running() is %v %s later", hub.Running(), closeWait)
	}

	assert.False(t, hub.Running(), "the hub no longer reports that it receives signals")

	select {
	case <-alice.Done():
	default:
		assert.Fail(t, "alice's stream was left open on an instance that receives nothing")
	}

	_, err = hub.Subscribe("bob")
	assert.ErrorIs(t, err, ntfy.ErrUnavailable, "a new stream is refused as unavailable")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -C nats -run 'TestListenEndsWhenTheConnectionCloses|TestHubStopsReceivingWhenTheNATSConnectionCloses' -count=1 .`

Expected: FAIL, for the stated reason. Record the output. These lines match the audit's run on 2026-09-28:

```
--- FAIL: TestListenEndsWhenTheConnectionCloses/the_connection_gives_up_reconnecting
    Error:    Listen is still running
    Messages: the connection is CLOSED (true), yet Listen is still running 5s later
--- FAIL: TestListenEndsWhenTheConnectionCloses/the_host_closes_the_connection
    Error:    Listen is still running
    Messages: the connection is CLOSED (true), yet Listen is still running 5s later
--- PASS: TestListenEndsWhenTheConnectionCloses/a_connection_reconnecting_without_limit_keeps_listening
--- FAIL: TestHubStopsReceivingWhenTheNATSConnectionCloses
    Error:    the hub is still running
    Messages: the connection is CLOSED, yet hub.Running() is true 5s later
```

A failure of any other kind is not the red step. That includes a compile error, a container error, or `Listen did not become ready`. Fix the test, then run it again.

- [ ] **Step 3: Implement D1 in `nats/broadcaster.go`**

Replace the body of `Listen` from `messages := make(...)` to the end of the loop with:

```go
	// Registered before subscribing, so that no close is missed: one before this
	// makes ChanSubscribe fail, and one after lands in the channel's buffer.
	closed := b.conn.StatusChanged(natsgo.CLOSED)
	defer b.conn.RemoveStatusListener(closed)

	messages := make(chan *natsgo.Msg, listenBuffer)

	sub, err := b.conn.ChanSubscribe(b.subject, messages)
	if err != nil {
		return fmt.Errorf("nats: subscribe to subject %q: %w", b.subject, err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	if err := b.confirm(ctx); err != nil {
		return err
	}

	ready()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-closed:
			return fmt.Errorf("nats: listen on subject %q: %w", b.subject, natsgo.ErrConnectionClosed)
		case msg := <-messages:
			signals, err := ntfy.DecodeSignals(msg.Data)
			if err != nil {
				b.onError(ctx, fmt.Errorf("nats: a message on subject %q: %w", b.subject, err))

				continue
			}

			for _, signal := range signals {
				deliver(signal)
			}
		}
	}
```

In `Listen`'s godoc, replace the sentence "After a reconnect the connection resubscribes on its own, and signals published while it was away are not replayed." with:

```go
// After a reconnect the connection resubscribes on its own, and signals
// published while it was away are not replayed. A connection closed for good,
// because it used up its reconnect attempts or because the host closed or
// drained it, can deliver nothing more: Listen then returns an error matching
// [natsgo.ErrConnectionClosed], so that the hub stops reporting itself running
// and closes its streams. How long a connection keeps reconnecting is the
// host's choice, made when it connects: [natsgo.MaxReconnects] with -1 keeps
// an instance listening through any outage.
```

Keep the final sentence about a nil `deliver` or `ready` unchanged.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=go1.26.8 go test -C nats -run 'TestListenEndsWhenTheConnectionCloses|TestHubStopsReceivingWhenTheNATSConnectionCloses' -count=1 .`
Expected: PASS (all four).

Run: `GOTOOLCHAIN=go1.26.8 go test -C nats -race -count=1 .`
Expected: `ok`. This includes `TestListen`'s goleak checks and `TestListenResumesAfterTheConnectionDrops`: a dropped but reconnecting connection must still resume. A spike of this exact code passed both on 2026-09-28.

- [ ] **Step 5: Invert to confirm the tests notice**

Temporarily delete the `case <-closed:` branch, re-run Step 4's first command, and watch the three closing assertions fail again. Then restore the branch.

- [ ] **Step 6: Commit**

```bash
git add nats/closed_test.go nats/broadcaster.go
git commit -m "End NATS Listen once its connection is closed for good

Listen waited only on its context and on a channel nats.go never closes,
so a connection that used up its reconnect attempts, or that the host
closed, left the hub reporting ready and accepting silent streams. Listen
now watches the connection's CLOSED status and returns an error matching
natsgo.ErrConnectionClosed. natsgo.MaxReconnects(-1) keeps an instance
listening through any outage.

<attribution lines>"
```

---

### Task 2: The contract says it for every broadcaster (task 1.3)

**Files:**
- Modify: `realtime.go:53-68`, `hub.go:296-303`, `nats/doc.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: godoc only; no signature changes.

This is a documentation change to an interface contract. `golang-tdd.md` exempts pure documentation, and a generic test of it cannot be written against a `PairFactory` (`design.md` D2, Non-Goals). The behaviour it describes is proved per adapter: in Task 1 for NATS, and by Redis's existing `Listen`.

- [ ] **Step 1: Extend the `Broadcaster.Listen` godoc in `realtime.go`**

Insert, after the paragraph ending "leaves the hub refusing every stream.":

```go
	//
	// A Listen whose transport can no longer deliver while ctx is still live,
	// such as one whose connection is closed for good rather than reconnecting,
	// returns an error instead of running on. [Hub] then stops reporting itself
	// running and closes its streams, rather than accept streams it cannot feed.
```

- [ ] **Step 2: Rewrite `Hub.Running`'s second paragraph in `hub.go`**

Replace:

```go
// After a broker connection drops, the broadcaster's client resubscribes on its
// own and Running stays true meanwhile; signals in that gap are lost, which the
// best-effort contract allows.
```

with:

```go
// While a broadcaster's client reconnects after a dropped connection, Running
// stays true, and signals in that gap are lost, which the best-effort contract
// allows. When the broadcaster gives up, its Listen returns an error, the run
// ends, and Running reports false.
```

- [ ] **Step 3: Add to `nats/doc.go`**

After the paragraph "Listening reports ready only once the server has confirmed the subscription, within a subscribe timeout, so a signal broadcast from then on reaches it.", add:

```go
//
// Listening ends with an error once the connection is closed for good, having
// used up its reconnect attempts or been closed by the host, so an instance
// never reports ready while it can receive nothing. The host sets how long the
// connection keeps trying when it connects.
```

- [ ] **Step 4: Verify**

Run: `GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy Broadcaster` and `GOTOOLCHAIN=go1.26.8 go doc github.com/kartaladev/ntfy Hub.Running`
Expected: the new paragraphs appear.

Run: `make lint`
Expected: no issues.

- [ ] **Step 5: Commit**

```bash
git add realtime.go hub.go nats/doc.go
git commit -m "State that a broadcaster ends Listen when its transport ends

<attribution lines>"
```

---

### Task 3: The Redis cancel case waits for the server (tasks 2.1, 2.2)

**Files:**
- Modify: `redis/listen_test.go:145-165`, `redis/broadcaster.go:157-174`
- Temporary, never committed: `redis/audit_listen_unsubscribe_test.go`

**Interfaces:**
- Consumes (existing, package `redis_test`):
  - `redis.RunTestRedis(t *testing.T, opts ...redis.TestOption) *goredis.Client`;
  - `listen(t, client goredis.UniversalClient, channel string) *listening`, whose `*listening` has `broadcaster *redis.Broadcaster`, `done chan error` and `cancel context.CancelFunc`;
  - `testWait = 10 * time.Second`, `testTick = 50 * time.Millisecond`;
  - `(*redis.Broadcaster).Channel() string`, and go-redis `PubSubNumSub(ctx, channels ...string) *MapStringIntCmd`.
- Produces: nothing new.

- [ ] **Step 1: Write the red proof (not committed)**

Create `redis/audit_listen_unsubscribe_test.go`:

```go
package redis_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kartaladev/ntfy/redis"
)

// TestAuditListenUnsubscribedWhenListenReturns asserts what the cancel case
// asserts today, 200 times: the channel has no subscriber at the instant
// Listen returns. It proves that assertion is racy, and is deleted afterwards.
func TestAuditListenUnsubscribedWhenListenReturns(t *testing.T) {
	client := redis.RunTestRedis(t)

	stale := 0

	for i := range 200 {
		l := listen(t, client, fmt.Sprintf("audit.unsub.now.%d", i))
		l.cancel()
		require.ErrorIs(t, <-l.done, context.Canceled)

		counts, err := client.PubSubNumSub(t.Context(), l.broadcaster.Channel()).Result()
		require.NoError(t, err)

		if counts[l.broadcaster.Channel()] != 0 {
			stale++
		}
	}

	t.Logf("rounds=200 still-subscribed-at-return=%d", stale)
	require.Zero(t, stale, "the channel still had a subscriber right after Listen returned")
}
```

- [ ] **Step 2: Run it to watch it fail**

Run: `GOTOOLCHAIN=go1.26.8 go test -C redis -run 'TestAuditListenUnsubscribedWhenListenReturns' -count=1 -v .`

Expected: FAIL, for example:

```
audit_listen_unsubscribe_test.go:NN: rounds=200 still-subscribed-at-return=9
--- FAIL: TestAuditListenUnsubscribedWhenListenReturns
    Error:    Should be zero, but was 9
```

The number varies, and was 9 and 17 in two audit runs. Copy the line into the commit message.

**STOP:** if it reports `still-subscribed-at-return=0` in three consecutive runs, the race does not reproduce on this machine. Record the three outputs under "Does not reproduce" in `tasks.md` 2.1 and here, and do not change the test.

- [ ] **Step 3: Delete the proof**

```bash
rm redis/audit_listen_unsubscribe_test.go
```

It asserts a premise that stays false after the fix (`design.md` D3), so it could only ever be red.

- [ ] **Step 4: Fix the case in `redis/listen_test.go`**

Replace the whole "cancelling stops listening and unsubscribes" case with:

```go
		{
			name: "cancelling stops listening and releases the subscription",
			act: func(t *testing.T, l *listening) error {
				l.cancel()

				select {
				case err := <-l.done:
					return err
				case <-time.After(testWait):
					require.FailNow(t, "Listen did not return after its context was cancelled")

					return nil
				}
			},
			assert: func(t *testing.T, l *listening, err error) {
				require.ErrorIs(t, err, context.Canceled, "Listen returns its context's error, as the ntfy contract says")

				// go-redis closes the subscription's connection without sending
				// UNSUBSCRIBE, so the server drops the subscription when it
				// processes the close, shortly after Listen returns.
				require.Eventuallyf(t, func() bool {
					counts, countErr := client.PubSubNumSub(t.Context(), l.broadcaster.Channel()).Result()

					return countErr == nil && counts[l.broadcaster.Channel()] == 0
				}, testWait, testTick, "the channel still has a subscriber %s after Listen returned", testWait)
			},
		},
```

- [ ] **Step 5: Correct the `Listen` godoc in `redis/broadcaster.go`**

Replace the opening paragraph:

```go
// Listen implements [ntfy.Broadcaster]. It subscribes to the channel, waits
// for the broker to confirm the subscription, calls ready once, and then calls
// deliver with every signal of every message, until ctx is done, when it
// unsubscribes and returns ctx's error.
```

with:

```go
// Listen implements [ntfy.Broadcaster]. It subscribes to the channel, waits
// for the broker to confirm the subscription, calls ready once, and then calls
// deliver with every signal of every message, until ctx is done, when it
// closes its subscription's connection and returns ctx's error. The broker
// drops the subscription when it sees the close, shortly after Listen returns;
// nothing is delivered after Listen returns.
```

- [ ] **Step 6: Verify it is stable**

Run: `GOTOOLCHAIN=go1.26.8 go test -C redis -run 'TestListen$' -count=20 .`
Expected: `ok`. Before the fix, 3 failures in 100 at `listen_test.go:162` were observed (`proposal.md`).

Run: `GOTOOLCHAIN=go1.26.8 go test -C redis -race -count=1 .`
Expected: `ok`.

- [ ] **Step 7: Commit**

```bash
git add redis/listen_test.go redis/broadcaster.go
git commit -m "Wait for Redis to drop a cancelled subscription in the Listen test

go-redis closes a subscription's connection without sending UNSUBSCRIBE,
so the server forgets it about a millisecond after Listen returns. The
case read PUBSUB NUMSUB at once and failed now and then (red proof:
rounds=200 still-subscribed-at-return=<N>). It now waits, within
testWait, and the godoc says what Listen does instead of 'unsubscribes'.

<attribution lines>"
```

---

### Task 4: The broadcaster suite checks delivery (tasks 3.1, 3.2, 3.3)

**Files:**
- Create: `ntfytest/broadcaster_test.go`
- Modify: `ntfytest/broadcaster.go`

**Interfaces:**
- Consumes (existing, package `ntfytest`):
  - `type PairFactory func(t *testing.T) (publisher, listener ntfy.Broadcaster)`;
  - `func RunBroadcasterSuite(t *testing.T, newPair PairFactory)`;
  - `const broadcasterWait = 5 * time.Second`;
  - `startListening(t *testing.T, listener ntfy.Broadcaster, onReady func()) *listening`, with `(*listening).awaitReady(t)`, `awaitRecipient(recipient string) bool`, and the fields `mu sync.Mutex`, `received []ntfy.Signal` and `arrived chan struct{}`.
  - Core: `ntfy.Signal{Recipient, Change, At}`, `ntfy.ChangeCreated`, `ChangeRead`, `ChangeClosed`, `ChangePruned`, `ntfy.ConfigurationError{Detail string}`.
- Produces:
  - `const BroadcasterBatch = 1201` (exported) and `const everyListenerSignals = 4`;
  - `func (l *listening) snapshot() []ntfy.Signal` and `func (l *listening) awaitCount(n int) bool`;
  - `func assertEveryListener(t *testing.T, publisher, listener ntfy.Broadcaster)`, `func assertSignalIntact(...)` and `func assertWholeBatch(...)`, all with the same signature;
  - in `ntfytest_test`: `type brokenBroadcaster struct`, `TestBroadcasterSuiteChild` and `TestBroadcasterSuiteRejectsBrokenBroadcasters`.

- [ ] **Step 1: Write the failing proof**

Create `ntfytest/broadcaster_test.go`:

```go
package ntfytest_test

// The broadcaster suite fails its own test when a broadcaster breaks it, so the
// only way to assert that it rejects one is to run it in a child process and
// watch that process fail.

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

// broadcasterChildEnv names the defect a child process's broadcaster has, and
// marks a process as a child.
const broadcasterChildEnv = "NTFYTEST_BROADCASTER_CHILD"

// The defects a broadcaster can have, and "sound" for none.
const (
	sound       = "sound"
	oneListener = "one-listener" // hands each signal to one listener in turn, as a NATS queue group does
	dropFields  = "drop-fields"  // delivers only the recipient, losing the change and the time
	firstOnly   = "first-only"   // delivers only the first signal of each broadcast
)

// brokenBroadcaster is an in-process broadcaster with one selectable defect. It
// is a broken implementation under test, not a double.
type brokenBroadcaster struct {
	mode string

	mu        sync.Mutex
	listeners []*func(ntfy.Signal)
	next      int
}

func (b *brokenBroadcaster) Broadcast(_ context.Context, signals []ntfy.Signal) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.listeners) == 0 {
		return nil
	}

	if b.mode == firstOnly && len(signals) > 1 {
		signals = signals[:1]
	}

	for _, signal := range signals {
		if b.mode == dropFields {
			signal = ntfy.Signal{Recipient: signal.Recipient}
		}

		if b.mode == oneListener {
			(*b.listeners[b.next%len(b.listeners)])(signal)
			b.next++

			continue
		}

		for _, deliver := range b.listeners {
			(*deliver)(signal)
		}
	}

	return nil
}

func (b *brokenBroadcaster) Listen(ctx context.Context, deliver func(ntfy.Signal), ready func()) error {
	if deliver == nil || ready == nil {
		return &ntfy.ConfigurationError{Detail: "deliver and ready are required"}
	}

	b.mu.Lock()
	b.listeners = append(b.listeners, &deliver)
	b.mu.Unlock()

	ready()
	<-ctx.Done()

	b.mu.Lock()
	for i, l := range b.listeners {
		if l == &deliver {
			b.listeners = append(b.listeners[:i], b.listeners[i+1:]...)

			break
		}
	}
	b.mu.Unlock()

	return ctx.Err()
}

// TestBroadcasterSuiteChild runs the broadcaster suite against a broadcaster
// with the defect its environment names. It does nothing unless
// TestBroadcasterSuiteRejectsBrokenBroadcasters started it.
func TestBroadcasterSuiteChild(t *testing.T) {
	mode, ok := os.LookupEnv(broadcasterChildEnv)
	if !ok {
		t.Skip("runs only as a child of TestBroadcasterSuiteRejectsBrokenBroadcasters")
	}

	ntfytest.RunBroadcasterSuite(t, func(*testing.T) (ntfy.Broadcaster, ntfy.Broadcaster) {
		b := &brokenBroadcaster{mode: mode}

		return b, b
	})
}

// runBroadcasterChild runs the suite in a child process whose broadcaster has
// the defect mode, and reports whether it passed and what it printed.
func runBroadcasterChild(t *testing.T, mode string) (passed bool, output string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run", "^TestBroadcasterSuiteChild$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), broadcasterChildEnv+"="+mode)
	out, err := cmd.CombinedOutput()

	return err == nil, string(out)
}

// TestBroadcasterSuiteRejectsBrokenBroadcasters proves the suite fails a
// broadcaster that loses listeners, fields or signals, each in the case aimed at
// it, and passes a sound one. The cases do not vary context, so the table has
// no ctx field.
func TestBroadcasterSuiteRejectsBrokenBroadcasters(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		mode   string
		assert func(t *testing.T, passed bool, output string)
	}

	// rejectedBy requires the named suite case itself to have failed, so that a
	// child failing for any other reason, such as a panic, is not taken for one.
	rejectedBy := func(suiteCase string) func(t *testing.T, passed bool, output string) {
		return func(t *testing.T, passed bool, output string) {
			assert.Falsef(t, passed, "RunBroadcasterSuite accepted a broken broadcaster:\n%s", output)
			assert.Containsf(t, output, "--- FAIL: TestBroadcasterSuiteChild/"+suiteCase,
				"the case aimed at this defect, not something else, rejected it:\n%s", output)
		}
	}

	cases := []testCase{
		{
			name: "a sound broadcaster passes",
			mode: sound,
			assert: func(t *testing.T, passed bool, output string) {
				assert.Truef(t, passed, "RunBroadcasterSuite failed a sound broadcaster:\n%s", output)
				assert.Contains(t, output, "--- PASS: TestBroadcasterSuiteChild/a_signal_broadcast_right_after_ready_is_delivered",
					"the child ran the suite, so passing is not vacuous")
			},
		},
		{
			name:   "a broadcaster reaching one listener of several fails",
			mode:   oneListener,
			assert: rejectedBy("a_signal_reaches_every_listener_on_the_channel"),
		},
		{
			name:   "a broadcaster dropping the change and time fails",
			mode:   dropFields,
			assert: rejectedBy("a_signal_arrives_with_its_recipient,_change_and_time"),
		},
		{
			name:   "a broadcaster delivering part of a broadcast fails",
			mode:   firstOnly,
			assert: rejectedBy("every_signal_of_a_broadcast_is_delivered"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			passed, output := runBroadcasterChild(t, tc.mode)
			tc.assert(t, passed, output)
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `GOTOOLCHAIN=go1.26.8 go test -C ntfytest -run 'TestBroadcasterSuiteRejectsBrokenBroadcasters' -count=1 .`

Expected: the control passes, and each broken row FAILs because the child **passed**. The audit's run on 2026-09-28 showed:

```
--- FAIL: TestBroadcasterSuiteRejectsBrokenBroadcasters/a_broadcaster_reaching_one_listener_of_several_fails
    Error:    Should be false
    Messages: RunBroadcasterSuite accepted a broken broadcaster: ... PASS
--- FAIL: TestBroadcasterSuiteRejectsBrokenBroadcasters/a_broadcaster_dropping_the_change_and_time_fails
--- FAIL: TestBroadcasterSuiteRejectsBrokenBroadcasters/a_broadcaster_delivering_part_of_a_broadcast_fails
```

If the control row fails, the harness is wrong, and this is not the red step. Fix the harness first.

- [ ] **Step 3: Add the constants to `ntfytest/broadcaster.go`**

After `const broadcasterWait = 5 * time.Second`, add:

```go
// BroadcasterBatch is how many signals the batch case broadcasts in one call.
// It is more than twice the 500 signals ntfy/redis and ntfy/nats put in one
// message, so a broadcaster that splits a broadcast into several messages is
// checked across the split.
const BroadcasterBatch = 1201

// everyListenerSignals is how many signals the every-listener case broadcasts,
// one per call. A broadcaster handing each signal to one listener of two, as a
// NATS queue group does, cannot deliver them all to both.
const everyListenerSignals = 4
```

- [ ] **Step 4: Register the cases and update the suite godoc**

In `RunBroadcasterSuite`'s `cases`, after the `assertCancelled` line, add:

```go
		{name: "a signal reaches every listener on the channel", assert: assertEveryListener},
		{name: "a signal arrives with its recipient, change and time", assert: assertSignalIntact},
		{name: "every signal of a broadcast is delivered", assert: assertWholeBatch},
```

Append these bullets to the `RunBroadcasterSuite` godoc list:

```go
//   - every listener on the channel receives every signal: a Listen on each
//     broadcaster of the pair, as two instances hold, is delivered all of them;
//   - a signal arrives with its recipient, its change and its time;
//   - every signal of one broadcast is delivered, [BroadcasterBatch] of them,
//     more than one broker message carries.
```

- [ ] **Step 5: Add the helpers and cases at the end of `ntfytest/broadcaster.go`**

```go
// snapshot returns a copy of every signal delivered so far.
func (l *listening) snapshot() []ntfy.Signal {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]ntfy.Signal(nil), l.received...)
}

// awaitCount waits until at least n signals have been delivered.
func (l *listening) awaitCount(n int) bool {
	deadline := time.After(broadcasterWait)

	for len(l.snapshot()) < n {
		select {
		case <-l.arrived:
		case <-deadline:
			return false
		}
	}

	return true
}

func assertEveryListener(t *testing.T, publisher, listener ntfy.Broadcaster) {
	t.Helper()

	// Two instances on one channel: a Listen on each broadcaster of the pair. For
	// a broadcaster that reaches only its own process, the two are one value.
	listeners := []*listening{startListening(t, publisher, nil), startListening(t, listener, nil)}
	for _, l := range listeners {
		l.awaitReady(t)
	}

	at := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	for i := range everyListenerSignals {
		signal := ntfy.Signal{Recipient: fmt.Sprintf("every-%d", i), Change: ntfy.ChangeCreated, At: at}
		require.NoError(t, publisher.Broadcast(t.Context(), []ntfy.Signal{signal}))
	}

	for n, l := range listeners {
		for i := range everyListenerSignals {
			if !l.awaitRecipient(fmt.Sprintf("every-%d", i)) {
				t.Fatalf("listener %d of %d never received every-%d: each signal must reach every listener, got %v",
					n+1, len(listeners), i, l.snapshot())
			}
		}
	}
}

func assertSignalIntact(t *testing.T, publisher, listener ntfy.Broadcaster) {
	t.Helper()

	l := startListening(t, listener, nil)
	l.awaitReady(t)

	// Whole seconds, compared with time.Equal, so that neither a host format's
	// precision nor a time.Location decides the case.
	want := []ntfy.Signal{
		{Recipient: "intact-created", Change: ntfy.ChangeCreated, At: time.Date(2026, 3, 1, 9, 0, 1, 0, time.UTC)},
		{Recipient: "intact-read", Change: ntfy.ChangeRead, At: time.Date(2026, 3, 1, 9, 0, 2, 0, time.UTC)},
		{Recipient: "intact-closed", Change: ntfy.ChangeClosed, At: time.Date(2026, 3, 1, 9, 0, 3, 0, time.UTC)},
		{Recipient: "intact-pruned", Change: ntfy.ChangePruned, At: time.Date(2026, 3, 1, 9, 0, 4, 0, time.UTC)},
	}

	for _, signal := range want {
		require.NoError(t, publisher.Broadcast(t.Context(), []ntfy.Signal{signal}))
	}

	require.Truef(t, l.awaitCount(len(want)), "only %d of %d signals arrived", len(l.snapshot()), len(want))

	got := make(map[string]ntfy.Signal)
	for _, signal := range l.snapshot() {
		got[signal.Recipient] = signal
	}

	for _, w := range want {
		g, ok := got[w.Recipient]
		if !assert.Truef(t, ok, "no signal for %s", w.Recipient) {
			continue
		}

		assert.Equalf(t, w.Change, g.Change, "the change of %s's signal", w.Recipient)
		assert.Truef(t, w.At.Equal(g.At), "the time of %s's signal: want %s, got %s", w.Recipient, w.At, g.At)
	}
}

func assertWholeBatch(t *testing.T, publisher, listener ntfy.Broadcaster) {
	t.Helper()

	l := startListening(t, listener, nil)
	l.awaitReady(t)

	at := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	batch := make([]ntfy.Signal, BroadcasterBatch)

	for i := range batch {
		batch[i] = ntfy.Signal{Recipient: fmt.Sprintf("batch-%d", i), Change: ntfy.ChangeCreated, At: at}
	}

	require.NoError(t, publisher.Broadcast(t.Context(), batch))

	if !l.awaitCount(len(batch)) {
		t.Fatalf("%d of the %d signals of one broadcast were delivered", len(l.snapshot()), len(batch))
	}

	delivered := make(map[string]bool, len(batch))
	for _, signal := range l.snapshot() {
		delivered[signal.Recipient] = true
	}

	for _, signal := range batch {
		assert.Truef(t, delivered[signal.Recipient], "%s was not delivered", signal.Recipient)
	}
}
```

- [ ] **Step 6: Run the proof to verify it passes**

Run: `GOTOOLCHAIN=go1.26.8 go test -C ntfytest -run 'TestBroadcasterSuiteRejectsBrokenBroadcasters' -count=1 -v .`
Expected: PASS for all four rows. The `one-listener` and `first-only` children take about 5s each, which is `broadcasterWait` expiring in the case that rejects them.

- [ ] **Step 7: Confirm each case is what rejects its broadcaster**

For each of the three registration lines added in Step 4, one at a time:
1. comment the line out;
2. run Step 6's command, and watch that row, and only that row, go red;
3. restore the line.

- [ ] **Step 8: Run the suite against the library's broadcasters**

Run:
- `GOTOOLCHAIN=go1.26.8 go test -race -run TestInProcessBroadcasterConformance -count=3 .`
- `GOTOOLCHAIN=go1.26.8 go test -C redis -race -run TestBroadcasterConformance -count=3 .`
- `GOTOOLCHAIN=go1.26.8 go test -C nats -race -run TestBroadcasterConformance -count=3 .`

Expected: `ok` for all three. The spike on 2026-09-28 passed each with the new cases taking 10–20 ms.

- [ ] **Step 9: Commit**

```bash
git add ntfytest/broadcaster_test.go ntfytest/broadcaster.go
git commit -m "Hold every broadcaster to delivery in RunBroadcasterSuite

The suite passed a broadcaster that reached one listener of several, one
that dropped a signal's change and time, and one that delivered only the
first signal of a broadcast. Three cases now check every listener, every
field and every signal of a batch larger than one message, and a
child-process test proves each rejects its broken broadcaster.

<attribution lines>"
```

---

### Task 5: Documentation (tasks 4.1, 4.2)

**Files:**
- Modify: `nats/docs_test.go`, `docs/realtime-operations.md`, `docs/notifications.md:186-197`, `ntfytest/doc.go`

**Interfaces:**
- Consumes: `TestTheOperationsGuideMatchesTheBroadcaster` in `nats/docs_test.go`, and its `stated` string slice.
- Produces: nothing new.

- [ ] **Step 1: Write the failing doc test**

In `nats/docs_test.go`, add to the `stated` slice:

```go
		"natsgo.ErrConnectionClosed",
		"natsgo.MaxReconnects(-1)",
```

- [ ] **Step 2: Run it to verify it fails**

Run: `GOTOOLCHAIN=go1.26.8 go test -C nats -run TestTheOperationsGuideMatchesTheBroadcaster -count=1 .`
Expected: FAIL with `the guide states "natsgo.ErrConnectionClosed"` and `the guide states "natsgo.MaxReconnects(-1)"`.

- [ ] **Step 3: Add the section to `docs/realtime-operations.md`**

Insert after the paragraph ending "the host decides when to run it again.", before `## Best effort, and what that means`:

````markdown
### When a broker connection closes for good

A dropped connection reconnects on its own, and the hub keeps running
meanwhile. A connection that is **closed for good** can deliver nothing
more. That happens when it used up its reconnect attempts, or when the
host closed or drained it. Then `hub.Run` returns an error, `hub.Running()`
reports false, and every stream and WebSocket on the instance is closed, so
that no client is left with a silent connection.

| Broadcaster | Closed for good when | Error |
| --- | --- | --- |
| NATS | the connection used up `natsgo.MaxReconnects` attempts (60, 2s apart, by default), or was closed or drained | matches `natsgo.ErrConnectionClosed` |
| Redis | the host closed the client (go-redis reconnects a subscription without limit) | `redis: the subscription to channel "…" ended` |

How long to keep trying is the host's choice, made on the connection. To
keep an instance waiting through any outage, connect with
`natsgo.MaxReconnects(-1)`. To recover instead, run the hub again on a
healthy connection:

```go
for {
    err := hub.Run(ctx)
    if ctx.Err() != nil {
        return
    }
    logError(ctx, err)
    // reconnect, or replace the broadcaster, then run again
}
```
````

- [ ] **Step 4: Run it to verify it passes**

Run: `GOTOOLCHAIN=go1.26.8 go test -C nats -run TestTheOperationsGuideMatchesTheBroadcaster -count=1 .`
Expected: PASS.

- [ ] **Step 5: Update `docs/notifications.md`**

Replace the two bullets "After a broker connection drops…" and "A broadcaster of your own…" with:

```markdown
- **After a broker connection drops**, the broadcaster's client resubscribes on
  its own and `Running` stays true meanwhile; signals in that gap are lost, and
  clients recover by re-reading. A connection **closed for good** ends `Run`
  with an error instead, and `Running` turns false (see
  `docs/realtime-operations.md`).
- **A broadcaster of your own** implements `Listen(ctx, deliver, ready)`: it calls
  `ready` once its subscription is confirmed, and never calling it leaves the hub
  refusing every stream. It returns an error once its transport can deliver
  nothing more. `ntfytest.RunBroadcasterSuite` checks it against the contract:
  - readiness, and a signal broadcast right after it;
  - cancellation;
  - every listener receiving every signal;
  - each signal's recipient, change and time;
  - every signal of a broadcast larger than one message.
```

- [ ] **Step 6: Update `ntfytest/doc.go`**

Append to the package doc, before `package ntfytest`:

```go
//
// [RunBroadcasterSuite] holds an [ntfy.Broadcaster] to its contract:
// readiness, cancellation, and delivery. Every listener on a channel receives
// every signal, with its recipient, change and time, including every signal
// of a broadcast larger than one broker message.
```

- [ ] **Step 7: Verify**

Run: `GOTOOLCHAIN=go1.26.8 go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .` and `make lint`
Expected: `ok`, and no lint issues.

- [ ] **Step 8: Commit**

```bash
git add nats/docs_test.go docs/realtime-operations.md docs/notifications.md ntfytest/doc.go
git commit -m "Document what ends a hub's run and what the broadcaster suite checks

<attribution lines>"
```

---

### Task 6: Verify and hand off (tasks 5.1, 5.2)

**Files:** none new.

**Interfaces:** none.

- [ ] **Step 1: Run the full gate**

Run: `make all`
Expected: lint, split-check and every module's tests pass.

Run: `make store-matrix`
Expected: the store suite passes on every driver and dialect. The `ntfytest` module changed.

- [ ] **Step 2: No stray proofs**

Run: `git status --short` and `git ls-files | grep audit_`
Expected: a clean tree and no output. No `audit_*_test.go` from the proposal is committed.

- [ ] **Step 3: Tick `tasks.md`**

Mark 1.1–5.1 `[x]` in `openspec/changes/end-broadcaster-listen-with-its-connection/tasks.md`. Commit with the plan.

- [ ] **Step 4: Review**

Run `/code-review` on the branch diff. Answer each finding under `superpowers:receiving-code-review`: verify it before agreeing, and prove any defect it claims with a failing test first. Add the answers as new tasks in `tasks.md` and matching steps here, in the same turn. Then tick 5.2.

## Self-Review

- **Spec coverage:**
  - "Signals reach every instance through a replaceable broadcaster":
    - "A connection that gives up reconnecting ends receiving with an error" → Task 1, case 1, and the hub test;
    - "A connection the host closes ends receiving with an error" → Task 1, case 2, and the hub test's `Subscribe("bob")` → `ErrUnavailable`;
    - "A host keeps an instance waiting out an outage" → Task 1, case 3.
  - "Signals cross instances over Redis or NATS": the three "suite fails" scenarios → Task 4, Steps 1–7; "The library's broadcasters pass the suite" → Task 4, Step 8.
  - Proposal finding 2 → Task 3.
- **Placeholders:** `<attribution lines>` stands for the session's attribution trailer, which the executor supplies. `<N>` is the measured count from Task 3, Step 2. There are no others.
- **Names:** `closeWait`, `listen`, `startProxy`, `BroadcasterBatch`, `everyListenerSignals`, `snapshot`, `awaitCount`, `assertEveryListener`, `assertSignalIntact`, `assertWholeBatch`, `brokenBroadcaster` and `runBroadcasterChild` are each defined once, and used with the same signatures.
