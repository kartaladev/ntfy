# notification-realtime Specification

## Purpose
Defines how a connected client learns, without polling, that a recipient's notifications changed: who may subscribe to whose changes, what a signal carries, how signals reach every application instance, and how the system protects publishers from slow or disconnected clients.

## Requirements

### Requirement: A signal says that notifications changed, never what they contain

The system SHALL signal a recipient's subscribers whenever that recipient's notifications change. That covers creation, being marked read, being closed, and an active notification evicted by pruning. A signal SHALL carry only the kind of change and when it happened. It SHALL NOT carry a notification's title, links, data, kind or subject. A client SHALL learn the current state by reading the notification store, which remains the source of truth.

#### Scenario: Publishing signals the recipient

- **WHEN** a notification is published for alice while alice has an open stream
- **THEN** alice's stream receives an unread-changed signal naming the change as a creation

#### Scenario: A signal carries no content

- **WHEN** a notification with a title, links and data is published for alice
- **THEN** the signal on alice's stream contains none of the title, the links or the data

### Requirement: Subscriptions are authorized, by default to the acting user's own notifications

The system SHALL authorize every subscription against the acting user the host established, before any signal is sent. By default it SHALL permit a user to subscribe only to their own notifications, and SHALL refuse a subscription when no acting user is established.

The host SHALL be able to replace the default policy with its own, such as one that lets a supervisor follow a team member, or to permit every subscription through an explicitly named opt-out. Supplying no policy SHALL be a configuration error at construction. A refused subscription SHALL be answered as forbidden, with the policy's message.

A subscription policy SHALL grant following only. Permitting an acting user to follow a recipient SHALL NOT permit that acting user to change anything belonging to that recipient, on any transport. Every operation that changes a recipient's notifications SHALL act on the acting user's own notifications, whoever the connection follows.

#### Scenario: A user follows their own notifications by default

- **WHEN** alice opens a stream for her own notifications
- **THEN** the subscription is permitted

#### Scenario: Following someone else is refused by default

- **WHEN** alice opens a stream for bob's notifications under the default policy
- **THEN** the request is refused as forbidden and no stream is opened

#### Scenario: A host policy permits a supervisor

- **WHEN** a host replaces the default with a policy permitting supervisors to follow their reports, and alice, bob's supervisor, opens a stream for bob
- **THEN** the subscription is permitted and alice receives bob's signals

#### Scenario: Following does not grant changing

- **WHEN** a host permits every subscription, and alice, following bob, asks over her connection to mark bob's notifications read
- **THEN** the request is refused, and every notification of bob's keeps the state it had

#### Scenario: Both transports grant the same authority

- **WHEN** the same subscription policy is configured for the stream and for the WebSocket transport, and the same acting user makes the same request on each
- **THEN** both transports permit it or both refuse it, and neither changes a recipient other than the acting user

#### Scenario: A missing policy is refused at construction

- **WHEN** a host configures the realtime handler with no subscription policy
- **THEN** construction fails with a configuration error

### Requirement: Signals reach every instance through a replaceable broadcaster

The system SHALL pass signals between the place a change is written and the instances holding client connections through a broadcaster. With no configuration it SHALL use an in-process broadcaster, which reaches only connections held by the same process. The system SHALL document that a deployment with more than one instance must supply a broadcaster that crosses instances.

Receiving signals from the broadcaster SHALL run only while the host runs it. A stream requested while signals are not being received SHALL be refused as unavailable, rather than opened and left silent.

Signals SHALL count as being received only once the broadcaster has confirmed that its subscription is in place, not merely once the host has started receiving. A stream requested before that confirmation SHALL be refused as unavailable. Once an instance reports that it is receiving, a signal broadcast afterwards SHALL reach the streams open on that instance, within the best-effort delivery the broadcaster offers. The system SHALL let the host wait for that confirmation without polling. A broadcaster whose subscription cannot be confirmed SHALL end receiving with an error rather than report itself ready.

#### Scenario: One instance works with no configuration

- **WHEN** a single-instance host publishes a notification for alice, who has a stream open on that instance, with no broadcaster configured
- **THEN** alice's stream receives the signal

#### Scenario: A cross-instance broadcaster reaches another instance

- **WHEN** a host supplies a broadcaster shared by instances A and B, a notification for alice is published on A, and alice's stream is open on B
- **THEN** alice's stream on B receives the signal

#### Scenario: A stream is refused while signals are not received

- **WHEN** a client opens a stream on an instance whose host has not started receiving signals
- **THEN** the request is refused as unavailable

#### Scenario: A stream is refused until the subscription is confirmed

- **WHEN** the host has started receiving signals, the broadcaster has not yet confirmed its subscription, and a client opens a stream
- **THEN** the request is refused as unavailable

#### Scenario: A signal broadcast right after readiness is delivered

- **WHEN** instance B reports that it is receiving, alice opens a stream on B, and a notification for alice is published on instance A immediately afterwards with no delay
- **THEN** alice's stream on B receives the signal

#### Scenario: A host waits for readiness without polling

- **WHEN** a host starts receiving signals and waits for the instance to report readiness
- **THEN** the wait ends once the broadcaster has confirmed its subscription, and not before

#### Scenario: A subscription that cannot be confirmed ends receiving with an error

- **WHEN** the cross-instance broker does not confirm the subscription within the broadcaster's subscribe timeout
- **THEN** receiving ends with an error, and the instance never reports that it is receiving

#### Scenario: A host-supplied broadcaster decides when it is ready

- **WHEN** a host supplies its own broadcaster, and that broadcaster reports readiness only after its own subscription is confirmed
- **THEN** streams on that instance are refused as unavailable until it does, and accepted afterwards

### Requirement: Streams stay alive, and slow clients never slow publishers

The system SHALL send a heartbeat on every open stream at a documented interval, **25 seconds** by default and configurable, so that proxies do not close idle streams. Signalling a recipient SHALL NOT wait for any client to receive the signal. When a client cannot keep up, the system SHALL coalesce its pending signals into one rather than queue them without bound. When a client stops accepting writes for longer than a documented write timeout, **10 seconds** by default and configurable, the system SHALL close that client's stream.

An open stream SHALL last only as long as its instance is receiving signals. When an instance stops receiving, the system SHALL close every stream open on it, rather than leave it open and silent. A stream closed for that reason SHALL NOT be revived if the instance starts receiving again; the client opens a new one. Every open stream SHALL carry a reconnect delay, **1 second** by default and configurable, spread per stream by a jitter that picks a value between the delay and twice it, so that an instance's clients do not reconnect in one wave.

The system SHALL cap the open streams per recipient on each instance, **8** by default and configurable, and SHALL also cap the total open streams on each instance across every recipient, **10,000** by default, configurable, and removable through an explicitly named opt-out. Both caps SHALL count every transport. The system SHALL refuse a stream beyond either cap as too many requests, and the refusal SHALL say which cap was reached.

Configuring a total cap below the per-recipient cap, which would leave the per-recipient cap unreachable, SHALL be a configuration error at construction, and so SHALL be setting and removing the total cap at once.

#### Scenario: An idle stream receives heartbeats

- **WHEN** a stream is open and nothing changes for 60 seconds with the default heartbeat
- **THEN** the client receives at least two heartbeats

#### Scenario: A stalled client does not block publishing

- **WHEN** alice's client has stopped reading its stream and 1,000 notifications are published for alice
- **THEN** every publish completes without waiting on the client, and alice's stream holds at most one pending signal

#### Scenario: A stalled client is disconnected

- **WHEN** alice's client accepts no writes for longer than the write timeout while a signal is pending
- **THEN** the system closes alice's stream

#### Scenario: An open stream ends when the instance stops receiving signals

- **WHEN** alice holds an open stream on an instance and that instance stops receiving signals
- **THEN** alice's stream is closed, and alice's client is not left with an open stream that receives only heartbeats

#### Scenario: A stream closed by a stop is not revived by the next run

- **WHEN** alice's stream is closed because her instance stopped receiving signals, the instance starts receiving again, and a notification is published for alice
- **THEN** the closed stream receives nothing, and alice receives the signal only on a stream she opens afterwards

#### Scenario: A stream tells the client when to reconnect

- **WHEN** alice opens a stream with the default reconnect delay
- **THEN** the stream carries a reconnect delay of at least 1 second and less than 2 seconds

#### Scenario: Two streams are told to reconnect at different times

- **WHEN** 100 streams are opened on one instance with the default reconnect delay
- **THEN** they do not all carry the same reconnect delay

#### Scenario: Too many streams for one recipient are refused

- **WHEN** alice already has the maximum number of streams open on an instance and opens another
- **THEN** the request is refused as too many requests

#### Scenario: Too many streams on one instance are refused

- **WHEN** an instance already holds its total stream cap, spread across many recipients, and a recipient under the per-recipient cap opens a stream
- **THEN** the request is refused as too many requests, naming the instance cap rather than the per-recipient cap

#### Scenario: A host raises the instance cap

- **WHEN** a host configures a total cap of 50,000 and an instance holds 20,000 open streams
- **THEN** a further stream is accepted

#### Scenario: A host removes the instance cap

- **WHEN** a host removes the total cap through the named opt-out, because it bounds connections elsewhere
- **THEN** streams are refused only by the per-recipient cap

#### Scenario: A total cap below the per-recipient cap is refused at construction

- **WHEN** a host configures a per-recipient cap of 8 and a total cap of 4
- **THEN** construction fails with a configuration error

#### Scenario: A total cap both set and removed is refused at construction

- **WHEN** a host both configures a total cap and removes it
- **THEN** construction fails with a configuration error

### Requirement: A reconnecting client re-reads rather than replays

The system SHALL NOT replay signals missed while a client was disconnected. The system SHALL document that a client, on connecting or reconnecting, reads its unread count and any open list from the notification store before relying on signals.

#### Scenario: Signals missed while disconnected are not replayed

- **WHEN** alice's stream is closed, three notifications are published for her, and she reconnects
- **THEN** her new stream receives no signal for those three, and reading her unread count returns them

### Requirement: Clients can receive change signals over a WebSocket connection

The system SHALL offer a WebSocket endpoint that delivers a recipient's change signals, as an alternative to the server-sent event stream. It SHALL apply the same rules as the stream:
- the subscription is authorized against the acting user by the same replaceable policy, and a missing policy is a configuration error at construction;
- a signal carries only the kind of change and when it happened;
- connections are refused while signals are not being received;
- an open connection is closed when its instance stops receiving signals;
- a stalled client never slows a publisher;
- idle connections are kept alive at the same default interval;
- the per-recipient connection cap and the instance-wide cap both count WebSocket connections and streams together.

A refusal SHALL be answered as an HTTP status before the connection is upgraded:
- forbidden for a missing acting user or a refused subscription;
- too many requests beyond either cap;
- unavailable while signals are not being received.

A connection closed because its instance stopped receiving signals SHALL be closed as going away, the same way as a connection closed by a shutdown, so that a client tells the two apart from a protocol error and reconnects.

#### Scenario: A user receives their own signals over WebSocket

- **WHEN** alice opens a WebSocket connection for her own notifications and a notification is published for her
- **THEN** alice's connection receives an unread-changed message naming the change as a creation, with no title, links, data, kind or subject

#### Scenario: Following someone else is refused before upgrading

- **WHEN** alice requests a WebSocket connection for bob's notifications under the default policy
- **THEN** the request is answered as forbidden and the connection is not upgraded

#### Scenario: The cap counts streams and WebSocket connections together

- **WHEN** alice already holds the maximum number of connections on an instance, some as streams and some as WebSocket connections, and requests another WebSocket connection
- **THEN** the request is answered as too many requests and the connection is not upgraded

#### Scenario: The instance cap counts both transports

- **WHEN** an instance holds its total cap of open connections, some as streams and some as WebSocket connections, and a recipient under the per-recipient cap requests another
- **THEN** the request is answered as too many requests and the connection is not upgraded

#### Scenario: A connection is refused while signals are not received

- **WHEN** a client requests a WebSocket connection on an instance whose host has not started receiving signals
- **THEN** the request is answered as unavailable and the connection is not upgraded

#### Scenario: An open connection is closed when the instance stops receiving signals

- **WHEN** alice holds an open WebSocket connection and her instance stops receiving signals
- **THEN** the connection is closed as going away, and alice's client is not left with an open connection that receives only pings

#### Scenario: An idle connection is kept alive

- **WHEN** a WebSocket connection is open and nothing changes for 60 seconds with the default heartbeat
- **THEN** the server has sent at least two keep-alive pings on the connection

#### Scenario: A stalled WebSocket client is disconnected without slowing publishers

- **WHEN** alice's client stops reading its WebSocket connection, 1,000 notifications are published for her, and the client accepts no writes for longer than the write timeout
- **THEN** every publish completes without waiting on the client, at most one signal is pending for the connection, and the system closes the connection

### Requirement: A WebSocket client can mark notifications read over its connection

The system SHALL accept, on an open WebSocket connection, requests to mark named notifications read and to mark all notifications read. It SHALL apply each request to the **acting user's** own notifications, with the same outcome as the equivalent HTTP operation, and SHALL answer each request with a reply that echoes the client's request reference.

A request naming a notification that does not exist or belongs to another recipient SHALL be answered as not found, indistinguishably. A request arriving on a connection that follows a recipient other than the acting user SHALL be refused as forbidden, and SHALL change nothing; such a connection SHALL continue to deliver signals. A malformed request SHALL be answered as a validation error without closing the connection. A message larger than the documented read limit SHALL close the connection.

#### Scenario: Marking a notification read over the connection

- **WHEN** alice sends a mark-read request with reference `r1` naming her active notification
- **THEN** the notification is READ, alice receives a reply referencing `r1` with one notification marked, and an unread-changed signal naming the change as a read

#### Scenario: Another recipient's notification is not found

- **WHEN** alice sends a mark-read request naming bob's notification
- **THEN** alice receives a not-found reply identical to the one for an identifier that does not exist, and bob's notification is unchanged

#### Scenario: Marking read on a followed connection is refused

- **WHEN** a host permits every subscription, alice holds a connection following bob, and alice sends a mark-all-read request with reference `r2`
- **THEN** alice receives a forbidden reply referencing `r2`, none of bob's notifications is marked read, none of alice's own is marked read, and the connection stays open and keeps delivering bob's signals

#### Scenario: A malformed request keeps the connection open

- **WHEN** alice sends a message that is not a recognised request
- **THEN** alice receives a validation error reply and the connection stays open

#### Scenario: An oversized message closes the connection

- **WHEN** alice sends a message larger than the read limit
- **THEN** the system closes the connection as a message too big

### Requirement: WebSocket connections from foreign browser origins are refused by default

The system SHALL, by default, refuse a WebSocket connection whose browser origin differs from the host the request was made to. The host SHALL be able to permit a documented list of origin patterns, or to permit every origin through an explicitly named opt-out.

#### Scenario: A same-origin connection is accepted

- **WHEN** a browser page served from the application's own host opens a WebSocket connection
- **THEN** the connection is accepted, subject to authorization

#### Scenario: A foreign origin is refused by default

- **WHEN** a page served from another site opens a WebSocket connection with alice's credentials
- **THEN** the request is refused as forbidden and the connection is not upgraded

#### Scenario: A host permits a listed origin

- **WHEN** a host permits the origin pattern `app.example.com` and a page from `app.example.com` opens a connection to `api.example.com`
- **THEN** the connection is accepted, subject to authorization

### Requirement: Signals cross instances over Redis or NATS

The system SHALL offer two broadcasters that pass change signals between application instances: one over Redis publish/subscribe and one over NATS core subjects. Each SHALL deliver a signal broadcast on any instance to every instance receiving signals through the same channel or subject. Each SHALL use a documented default channel or subject that the host can replace, distinct from the ones used for durable event delivery.

#### Scenario: A signal written on one instance reaches a stream on another over Redis

- **WHEN** instances A and B share a Redis broadcaster, a notification for alice is published on A, and alice's stream is open on B
- **THEN** alice's stream on B receives the signal

#### Scenario: A signal written on one instance reaches a WebSocket on another over NATS

- **WHEN** instances A and B share a NATS broadcaster, a notification for alice is published on A, and alice's WebSocket connection is open on B
- **THEN** alice's connection on B receives the signal

#### Scenario: Instances on different channels do not see each other

- **WHEN** instance A broadcasts on channel `app-one.signals` and instance B receives on `app-two.signals`
- **THEN** B receives no signal from A

### Requirement: Cross-instance broadcasting is best effort and carries no content

A broadcaster SHALL carry only the recipient, the kind of change and when it happened. It SHALL NOT carry a notification's title, links, data, kind or subject.

Broadcasting SHALL be best effort:
- a broker that cannot be reached SHALL NOT fail or roll back the notification write that produced the signal;
- signals broadcast while an instance is disconnected SHALL NOT be replayed to it;
- an instance SHALL resume receiving signals once its connection to the broker recovers, without the host restarting it.

The notification store SHALL remain the source of truth.

#### Scenario: A broker outage does not fail publishing

- **WHEN** the broker is unreachable and a notification is published for alice
- **THEN** the notification is stored and the publish succeeds, and the broadcast failure is reported to the host's handler

#### Scenario: Receiving resumes after the broker recovers

- **WHEN** an instance's connection to the broker drops and recovers, and a notification for alice is then published on another instance
- **THEN** alice's stream on the recovered instance receives the signal

#### Scenario: A broadcast message carries no content

- **WHEN** a notification with a title, links and data is published and broadcast
- **THEN** the message on the broker contains the recipient, the change and its time, and none of the title, links, data, kind or subject

### Requirement: Unrecognised broadcast messages are dropped, not delivered

The system SHALL tag every broadcast message with a format version. An instance receiving a message with an unknown version, or one it cannot decode, SHALL deliver no signal for it, SHALL report it to the host's handler, and SHALL keep receiving.

#### Scenario: A message from a newer format is ignored

- **WHEN** an instance receives a broadcast message tagged with a format version it does not know
- **THEN** no stream receives a signal for it, the host's handler is told, and later valid messages are still delivered

### Requirement: Broadcaster and WebSocket wiring mistakes fail at construction

The system SHALL refuse, at construction and before any traffic:
- a broadcaster built with no client or connection;
- an empty channel or subject;
- a NATS subject containing wildcards or empty tokens;
- a WebSocket endpoint built with no way to establish the acting user, or with no subscription policy;
- a non-positive read limit, heartbeat or write timeout.

#### Scenario: A NATS broadcaster with a wildcard subject is refused

- **WHEN** a host builds a NATS broadcaster on subject `ntfy.*`
- **THEN** construction fails with a configuration error

#### Scenario: A Redis broadcaster with no client is refused

- **WHEN** a host builds a Redis broadcaster with no client
- **THEN** construction fails with a configuration error
