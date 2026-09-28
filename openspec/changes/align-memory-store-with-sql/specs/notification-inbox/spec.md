## ADDED Requirements

### Requirement: A notification identifier is stored once

The system SHALL store at most one notification under each identifier. A publish or a close that would write a notification under an identifier already stored, or under one identifier twice in the same call, SHALL fail with an error and SHALL change nothing. The notification already stored under that identifier SHALL be kept unchanged, whoever its recipient. No other notification of the call SHALL be written, and a close SHALL close nothing and raise no close record. The system SHALL NOT report a notification as created unless it is stored.

Only notifications the call would write SHALL be checked. An insertion that is suppressed, coalesced or a duplicate of its source and recipient writes nothing, and SHALL NOT fail because of its identifier.

Identifiers come from the host's ID generator, whose contract is to mint unique ones. The default generator does. A generator that repeats one SHALL cost the write that received the repeat, never a stored notification.

#### Scenario: A publish reusing a stored identifier is refused

- **WHEN** a notification is stored for alice under an identifier, and a notification for bob on another subject is published under the same identifier
- **THEN** the publish fails, alice's notification is unchanged, and bob has no notification under that identifier

#### Scenario: A publish repeating an identifier within itself writes nothing

- **WHEN** one publish carries notifications for carol, alice and bob, and alice's and bob's share an identifier
- **THEN** the publish fails, and none of the three notifications is stored

#### Scenario: A close whose successor reuses a stored identifier changes nothing

- **WHEN** a subject is closed with a successor, and the identifier minted for the successor is already stored for carol
- **THEN** the close fails, every notification it would have closed is still open, carol's notification is unchanged, and a later publish below the close version is not suppressed

#### Scenario: An insertion that writes nothing is not checked

- **WHEN** a publish repeats a stored notification's source and recipient under an identifier already stored
- **THEN** it is counted as a duplicate, and the publish does not fail

## MODIFIED Requirements

### Requirement: Every store behaves identically

The system SHALL behave identically on every store it supports: in memory, and on PostgreSQL, MySQL and SQLite through each supported database driver. It SHALL assert this with one shared suite of behavioural cases executed against every combination, not with per-store tests.

This SHALL hold for every input the store contract admits, not only for those the service sends. That includes an identifier the host's generator repeats, and an instant finer than the microsecond precision every store keeps. A divergence found between stores SHALL be settled in the shared suite, where it binds a host's own store too, and not in a test of one store.

#### Scenario: The shared suite passes everywhere

- **WHEN** the shared notification store suite runs against the in-memory store and every supported driver and dialect combination
- **THEN** every case passes on every combination

#### Scenario: A host's store is held to the same edges

- **WHEN** a host runs the shared suite against its own store, and that store replaces a notification under a reused identifier or compares a retention cutoff at nanoseconds
- **THEN** the suite fails, naming the case
