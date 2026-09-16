## ADDED Requirements

### Requirement: A store shares no memory with its caller

The system SHALL keep every notification a store holds independent of the values its caller supplied, and SHALL keep every notification a store returns independent of the store's own state, of the caller's request, and of every other notification returned by the same call. A caller SHALL be able to mutate a link map or a data payload it supplied, or one it was returned, without changing anything else.

This SHALL hold on every store, and SHALL be asserted by the shared conformance suite, so that a store supplied by a host is held to it too. Where the system builds notifications on a store's behalf from a caller's values, it SHALL hand the store values that are already independent; a store SHALL NOT rely on that, and SHALL copy what it retains.

#### Scenario: Mutating a published payload does not change what is stored

- **WHEN** a notification is published with a link map and a data payload, and the publisher mutates that map and that payload afterwards
- **THEN** reading the notification back returns the links and data as they were at publication

#### Scenario: Mutating a close request does not change its successors

- **WHEN** a subject is closed with a successor carrying links and data, and the caller mutates the successor's link map and payload after the close returns
- **THEN** the successors the close reports, and the successors read back from the store, carry the links and data as they were at the close

#### Scenario: Successors to different recipients are independent

- **WHEN** a close with a successor creates notifications for alice and bob, and the caller mutates the link map of the successor reported for alice
- **THEN** the successor reported for bob is unchanged, and so is the one read back for alice

#### Scenario: A returned notification is independent of the store

- **WHEN** a recipient's notification is read twice, or listed and then read, and the caller mutates the links and data of the first result
- **THEN** the second result carries the links and data as they were stored

#### Scenario: Every store is held to this

- **WHEN** the shared conformance suite runs against the in-memory store and every supported driver and dialect combination
- **THEN** the isolation cases pass on every combination
