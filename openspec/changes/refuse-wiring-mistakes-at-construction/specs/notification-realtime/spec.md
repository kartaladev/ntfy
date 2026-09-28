## MODIFIED Requirements

### Requirement: Streams stay alive, and slow clients never slow publishers

The system SHALL send a heartbeat on every open stream at a documented interval, **25 seconds** by default and configurable, so that proxies do not close idle streams. Signalling a recipient SHALL NOT wait for any client to receive the signal. When a client cannot keep up, the system SHALL coalesce its pending signals into one rather than queue them without bound. When a client stops accepting writes for longer than a documented write timeout, **10 seconds** by default and configurable, the system SHALL close that client's stream.

An open stream SHALL last only as long as its instance is receiving signals. When an instance stops receiving, the system SHALL close every stream open on it, rather than leave it open and silent. A stream closed for that reason SHALL NOT be revived if the instance starts receiving again; the client opens a new one. Every open stream SHALL carry a reconnect delay, **1 second** by default and configurable, spread per stream by a jitter that picks a value between the delay and twice it, so that an instance's clients do not reconnect in one wave. A configured delay SHALL be at least a millisecond and at most a documented ceiling, the largest base for which twice the base is still a representable duration; any other SHALL be a configuration error at construction, so that the carried delay is never negative.

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

#### Scenario: The largest reconnect delay never wraps negative

- **WHEN** a host configures the documented ceiling as the reconnect delay and 1,000 delays are drawn
- **THEN** every delay is at least the ceiling

#### Scenario: A reconnect delay above the ceiling is refused at construction

- **WHEN** a host configures a reconnect delay of three quarters of the largest duration
- **THEN** construction fails with a configuration error

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

### Requirement: WebSocket connections from foreign browser origins are refused by default

The system SHALL, by default, refuse a WebSocket connection whose browser origin differs from the host the request was made to. The host SHALL be able to permit a documented list of origin patterns, or to permit every origin through an explicitly named opt-out.

An origin pattern SHALL be matched against the origin's host, and SHALL be able to use shell-style wildcards. A pattern that can never match a host SHALL be a configuration error at construction: an empty pattern, a pattern whose wildcard syntax is malformed, and a pattern containing `/`, such as a full origin with its scheme. Listing origin patterns together with the opt-out that permits every origin SHALL also be a configuration error, because the patterns would have no effect.

#### Scenario: A same-origin connection is accepted

- **WHEN** a browser page served from the application's own host opens a WebSocket connection
- **THEN** the connection is accepted, subject to authorization

#### Scenario: A foreign origin is refused by default

- **WHEN** a page served from another site opens a WebSocket connection with alice's credentials
- **THEN** the request is refused as forbidden and the connection is not upgraded

#### Scenario: A host permits a listed origin

- **WHEN** a host permits the origin pattern `app.example.com` and a page from `app.example.com` opens a connection to `api.example.com`
- **THEN** the connection is accepted, subject to authorization

#### Scenario: A malformed origin pattern is refused at construction

- **WHEN** a host permits the origin pattern `[app.example.com`
- **THEN** construction fails with a configuration error

#### Scenario: An origin pattern written as a full origin is refused at construction

- **WHEN** a host permits the origin pattern `https://app.example.com`
- **THEN** construction fails with a configuration error that says a pattern matches a host, such as `app.example.com`

#### Scenario: Patterns listed beside the any-origin opt-out are refused at construction

- **WHEN** a host both permits every origin and lists the origin pattern `app.example.com`
- **THEN** construction fails with a configuration error

### Requirement: Broadcaster and WebSocket wiring mistakes fail at construction

The system SHALL refuse, at construction and before any traffic:
- a broadcaster built with no client or connection;
- an empty channel or subject;
- a NATS subject containing wildcards or empty tokens;
- a WebSocket endpoint built with no way to establish the acting user, or with no subscription policy;
- a non-positive read limit, heartbeat or write timeout;
- a WebSocket base path that the HTTP contract would refuse: empty, not beginning with `/`, or containing `{` or `}`;
- an origin pattern that can never match a host, or origin patterns listed beside the any-origin opt-out;
- a reconnect delay below a millisecond or above its documented ceiling.

#### Scenario: A NATS broadcaster with a wildcard subject is refused

- **WHEN** a host builds a NATS broadcaster on subject `ntfy.*`
- **THEN** construction fails with a configuration error

#### Scenario: A Redis broadcaster with no client is refused

- **WHEN** a host builds a Redis broadcaster with no client
- **THEN** construction fails with a configuration error

#### Scenario: A WebSocket base path without a leading slash is refused

- **WHEN** a host builds the WebSocket endpoint with the base path `api`
- **THEN** construction fails with a configuration error, rather than returning the route `GET api/notifications/socket`

#### Scenario: The root WebSocket base path serves at the root

- **WHEN** a host builds the WebSocket endpoint with the base path `/`
- **THEN** its route is `GET /notifications/socket`
