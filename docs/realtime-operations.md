# Running realtime notifications

`ntfy` tells a client that its notifications changed, as a signal: the kind of
change and when it happened, never the notification itself. The client then
re-reads its unread count or list from the store, which is the source of truth.
This guide covers the choices a deployment makes: which broadcaster carries
signals between instances, which transport reaches the browser, and what the
infrastructure in between has to allow.

## Choosing a broadcaster

A signal is produced on the instance that stored the change. A client may be
connected to any instance. The broadcaster is what carries the signal from one
to the other.

| Deployment | Broadcaster | Wiring |
| --- | --- | --- |
| One instance | `ntfy.NewInProcessBroadcaster()` (the default) | nothing |
| Several instances, Redis available | `ntfy/redis` | `redis.NewBroadcaster(client)` |
| Several instances, NATS available | `ntfy/nats` | `nats.NewBroadcaster(conn)` |

**Stated limit:** with the in-process default, a signal reaches only connections
on the instance that produced it. A deployment of more than one instance needs a
cross-instance broadcaster, on every instance.

Redis:

```go
client := goredis.NewClient(&goredis.Options{Addr: "redis:6379"})

broadcaster, err := redis.NewBroadcaster(client,
    redis.WithDecodeErrorHandler(logError),
)
svc, err := ntfy.New(store,
    ntfy.WithBroadcaster(broadcaster),
    ntfy.WithSignalErrorHandler(logError),
)
hub, err := ntfy.NewHub(svc.Broadcaster())
go hub.Run(ctx)
```

NATS:

```go
conn, err := natsgo.Connect("nats://nats:4222",
    natsgo.ErrorHandler(logSlowConsumer), // slow-consumer drops are reported here
)

broadcaster, err := nats.NewBroadcaster(conn, nats.WithDecodeErrorHandler(logError))
```

| Setting | Default | Override |
| --- | --- | --- |
| Redis channel | `ntfy.signals` | `redis.WithChannel` |
| Redis publish timeout | 5s | `redis.WithPublishTimeout` |
| NATS subject | `ntfy.signals` | `nats.WithSubject` (no wildcards, whitespace or empty tokens) |
| NATS subscribe timeout | 5s | `nats.WithSubscribeTimeout` |
| Signals per message | 500 | none: a larger broadcast is split into several messages |

Instances see each other's signals only when they share a channel or subject.
Two applications on one broker use different ones.

The NATS subscription uses no queue group, on purpose: a queue group hands each
message to one instance, and every instance has to receive every signal.

Before the hub reports ready, the NATS broadcaster confirms its subscription
with a round trip to the server, within the subscribe timeout. A server that
does not answer in time, such as one unreachable when the hub starts, ends
`hub.Run` with an error rather than leaving the hub refusing streams with no
explanation; the host decides when to run it again.

## Best effort, and what that means

- A broker that cannot be reached never fails the notification write. The
  notification is stored, and the broadcast error goes to the service's signal
  error handler (`ntfy.WithSignalErrorHandler`), which is silent by default.
- Signals broadcast while an instance is disconnected are not replayed to it.
  Both clients reconnect and resubscribe on their own, without the host
  restarting anything.
- A message in a format version this library does not know, or one that cannot
  be decoded, delivers nothing and goes to the broadcaster's decode error
  handler. Receiving continues.
- A client that reconnects, or that missed a signal for any reason, loses
  nothing: it re-reads the store.

A message carries recipients, changes and times, and nothing else: never a
notification's title, links, data, kind or subject. **Recipient identifiers do
travel on the broker.** Where they are sensitive, run the broker on a private
network with TLS and authentication, configured on the client the host passes
in.

These broadcasters carry ephemeral signals, not durable events: nothing is
retried or replayed, and the notification store stays the source of truth. A
host that also needs durable events should publish them to a Redis stream or a
JetStream subject of its own; the broadcasters' default channel and subject are
namespaced under `ntfy.` so they do not collide with one.

## Choosing a transport

| Transport | Endpoint | Frameworks |
| --- | --- | --- |
| Server-sent events | `GET /v1/notifications/stream` (`ntfy.NewHandler`) | net/http, Gin, Fiber |
| WebSocket | `GET /v1/notifications/socket` (`websocket.NewHandler`) | net/http, Gin |

Both authorize the subscription with the same policy (`ntfy.SelfOnly` by
default), refuse while the hub is not running, and count against the same
per-recipient cap of 8 connections per instance and the same instance-wide cap
of 10000 streams per instance. Both are closed when the instance stops
receiving signals. The policy grants following only: a WebSocket client can
also mark the acting user's own notifications read over a connection opened
for the acting user, and a connection that follows anyone else refuses mark
requests altogether:

| Direction | Message |
| --- | --- |
| server to client | `{"type":"unread-changed","change":"created","at":"2026-09-14T10:00:00.000000Z"}` |
| client to server | `{"type":"mark-read","ref":"r1","ids":["..."]}` |
| client to server | `{"type":"mark-all-read","ref":"r2","through":"..."}` (`through` optional) |
| server to client | `{"type":"marked","ref":"r1","marked":1}` |
| server to client | `{"type":"error","ref":"r1","code":"not_found","message":"..."}` |
| server to client | `{"type":"error","ref":"r1","code":"forbidden","message":"..."}` |

**Stated limit: a subscription policy grants following only.** Marking read over
a WebSocket connection always acts on the acting user. A connection opened for
someone else — under `ntfy.AllowAll`, or a policy that lets a supervisor follow a
team member — keeps receiving that recipient's signals, and a mark request on it
is answered `forbidden` and changes nothing. There is no option to widen this: a
host that needs one user to mark another's notifications read does it over the
HTTP contract, behind its own authorization.

WebSocket defaults:

| Setting | Default | Override |
| --- | --- | --- |
| Browser origins | the request's own host only | `websocket.WithOriginPatterns`, or `websocket.WithAnyOrigin` |
| Largest client message | 4096 bytes (the connection is closed with 1009 beyond it) | `websocket.WithReadLimit` |
| Ping interval | the hub's heartbeat, 25s | `websocket.WithPingInterval` |
| Write timeout | the hub's write timeout, 10s | `websocket.WithWriteTimeout` |
| Subprotocol offered | `ntfy.v1` | none |

Hub defaults, which both transports share:

| Setting | Default | Override |
| --- | --- | --- |
| Streams per recipient | 8 per instance | `ntfy.WithMaxStreamsPerRecipient` |
| Streams per instance | 10000 across every recipient | `ntfy.WithMaxStreamsPerInstance`, or `ntfy.WithoutMaxStreamsPerInstance` |
| Reconnect delay | 1s, spread per stream up to 2s | `ntfy.WithReconnectDelay` |

**Stated limit:** the spread on the reconnect delay is not configurable. A base
with no spread returns an instance's clients in one wave, which is what the
delay exists to prevent.

When an instance stops receiving signals — `hub.Run` returned, because its
context was cancelled or its broadcaster gave up — every stream and connection
on it is closed: a server-sent event stream ends, and a WebSocket closes with
status 1001 and a reason saying the instance stopped receiving. Clients
reconnect after their delay and re-read the store. An instance that is up but
not receiving refuses new streams as unavailable, so a client that returns too
early is refused cheaply rather than left silent.

Mounting:

```go
mux.Handle(handler.Pattern(), handler)                          // net/http
router.GET("/v1/notifications/socket", gin.WrapH(handler))      // Gin
```

**Stated limit: WebSocket is not available on Fiber.** Fiber's adaptor cannot
hand over the hijacked connection a WebSocket needs. Fiber hosts use the
server-sent event stream, which carries the same signals.

## Proxies and load balancers

- **Idle timeouts** must be longer than the 25s heartbeat, or the proxy closes
  quiet connections: nginx `proxy_read_timeout`, the AWS ALB idle timeout (60s
  by default), and their equivalents.
- **WebSocket upgrades** need the `Upgrade` and `Connection` headers forwarded
  (nginx: `proxy_http_version 1.1`, `proxy_set_header Upgrade $http_upgrade`,
  `proxy_set_header Connection "upgrade"`).
- **Server-sent events** need buffering and compression off. The stream sends
  `X-Accel-Buffering: no` for nginx; other proxies need it configured.
- **HTTP/2** is preferable for server-sent events: browsers allow only six
  HTTP/1.1 connections per origin, and a stream holds one open.
- **Sticky sessions are not required** once a cross-instance broadcaster is
  configured. Without one, only a single-instance deployment receives every
  signal.
- **Redis** disconnects a pub/sub subscriber that falls behind its
  `client-output-buffer-limit pubsub`. The hub delivers without waiting on
  clients, so a listener keeps up; if the limit is still reached, the client
  reconnects.

## Shutting down

`http.Server.Shutdown` ends neither transport on its own. A WebSocket is a
hijacked connection, which `Shutdown` does not touch. A server-sent event
stream is an ordinary request that never returns, and `Shutdown` does not
cancel in-flight request contexts, so the stream's loop never wakes.

Cancel the server's base context as well. Every request context derives from
it, so one cancellation ends both: each WebSocket closes with status 1001
(going away) and releases its slot in the hub's caps, and each stream returns.

```go
baseCtx, cancelBase := context.WithCancel(context.Background())
server := &http.Server{
    Handler:     mux,
    BaseContext: func(net.Listener) context.Context { return baseCtx },
}
server.RegisterOnShutdown(cancelBase)
```

A client that was closed reconnects, to this instance or another, and re-reads
the store.

Stopping the hub is the other half: cancel the context passed to `hub.Run` and
every stream on the instance is closed the same way, whether or not the HTTP
server is going down.
