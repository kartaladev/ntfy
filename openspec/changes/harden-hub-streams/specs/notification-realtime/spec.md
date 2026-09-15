## MODIFIED Requirements

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
