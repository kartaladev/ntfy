## MODIFIED Requirements

### Requirement: Signals reach every instance through a replaceable broadcaster

The system SHALL pass signals between the place a change is written and the instances holding client connections through a broadcaster. With no configuration it SHALL use an in-process broadcaster, which reaches only connections held by the same process. The system SHALL document that a deployment with more than one instance must supply a broadcaster that crosses instances.

Receiving signals from the broadcaster SHALL run only while the host runs it. A stream requested while signals are not being received SHALL be refused as unavailable, rather than opened and left silent.

Signals SHALL count as being received only once the broadcaster has confirmed that its subscription is in place, not merely once the host has started receiving. A stream requested before that confirmation SHALL be refused as unavailable. Once an instance reports that it is receiving, a signal broadcast afterwards SHALL reach the streams open on that instance, within the best-effort delivery the broadcaster offers. The system SHALL let the host wait for that confirmation without polling. A broadcaster whose subscription cannot be confirmed SHALL end receiving with an error rather than report itself ready.

Signals SHALL stop counting as received once the broadcaster can no longer deliver them. A broadcaster whose connection to the broker is closed for good, because it gave up reconnecting or because the host closed it, SHALL end receiving with an error. It SHALL NOT keep reporting that it is receiving. A connection that has dropped and is still reconnecting SHALL NOT end receiving. How long a connection keeps reconnecting before it gives up SHALL remain the host's choice, made on the client connection the host supplies.

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

#### Scenario: A connection that gives up reconnecting ends receiving with an error

- **WHEN** an instance receives signals over NATS, and the broker stays unreachable for longer than the connection's reconnect attempts last, so the connection closes for good
- **THEN** receiving ends with an error that says the connection is closed, the instance stops reporting that it is receiving, and alice's open stream on it is closed

#### Scenario: A connection the host closes ends receiving with an error

- **WHEN** an instance receives signals over NATS and the host closes the NATS connection
- **THEN** receiving ends with an error that says the connection is closed, and a stream requested afterwards is refused as unavailable

#### Scenario: A host keeps an instance waiting out an outage

- **WHEN** a host configures its NATS connection to reconnect without limit, and the broker stays unreachable for longer than a connection limited to two attempts takes to give up
- **THEN** receiving does not end while the connection keeps trying

### Requirement: Signals cross instances over Redis or NATS

The system SHALL offer two broadcasters that pass change signals between application instances: one over Redis publish/subscribe and one over NATS core subjects. Each SHALL deliver a signal broadcast on any instance to every instance receiving signals through the same channel or subject. Each SHALL use a documented default channel or subject that the host can replace, distinct from the ones used for durable event delivery.

The shared broadcaster conformance suite SHALL hold every broadcaster to this, the in-process default, both cross-instance broadcasters and any broadcaster a host supplies. It SHALL fail a broadcaster that:
- delivers a signal to only some of the listeners on a channel;
- delivers a signal without its recipient, its kind of change or when it happened;
- delivers only part of a single broadcast, including one larger than a single broker message carries.

#### Scenario: A signal written on one instance reaches a stream on another over Redis

- **WHEN** instances A and B share a Redis broadcaster, a notification for alice is published on A, and alice's stream is open on B
- **THEN** alice's stream on B receives the signal

#### Scenario: A signal written on one instance reaches a WebSocket on another over NATS

- **WHEN** instances A and B share a NATS broadcaster, a notification for alice is published on A, and alice's WebSocket connection is open on B
- **THEN** alice's connection on B receives the signal

#### Scenario: Instances on different channels do not see each other

- **WHEN** instance A broadcasts on channel `app-one.signals` and instance B receives on `app-two.signals`
- **THEN** B receives no signal from A

#### Scenario: The suite fails a broadcaster that reaches one listener of several

- **WHEN** the conformance suite runs against a broadcaster that hands each signal to one of its listeners in turn, as a NATS queue group does
- **THEN** the suite fails

#### Scenario: The suite fails a broadcaster that loses a signal's change or time

- **WHEN** the conformance suite runs against a broadcaster that delivers only each signal's recipient
- **THEN** the suite fails

#### Scenario: The suite fails a broadcaster that delivers part of a broadcast

- **WHEN** the conformance suite runs against a broadcaster that delivers only the first signal of each broadcast
- **THEN** the suite fails

#### Scenario: The library's broadcasters pass the suite

- **WHEN** the conformance suite runs against the in-process, Redis and NATS broadcasters
- **THEN** each passes every case, a broadcast of more signals than one broker message carries included
