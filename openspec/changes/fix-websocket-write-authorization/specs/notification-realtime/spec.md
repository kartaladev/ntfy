## MODIFIED Requirements

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
