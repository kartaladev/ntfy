## ADDED Requirements

### Requirement: Identifiers are compared byte for byte on every store

The system SHALL treat two identifiers as equal only when they are the same sequence of bytes. This applies to a notification's recipient, source identifier, subject and kind, on every store. A store SHALL NOT fold case, ignore trailing spaces, ignore code points that a collation treats as ignorable, or apply Unicode normalisation when it matches, filters, de-duplicates or closes by an identifier. The system SHALL return every identifier exactly as it was published.

A store supplied by a host SHALL be held to this by the shared conformance suite. The system SHALL NOT require a newer database server than it already supports in order to provide this.

A schema whose identifier columns would compare other than byte for byte SHALL be reported by schema verification at startup, not discovered as a wrong match in traffic.

#### Scenario: A recipient differing by one ignorable code point is another recipient

- **WHEN** a notification is published for `alice`, and `alice` followed by a zero-width space reads, lists and counts their notifications
- **THEN** they are given nothing of alice's: reading the notification is not found, their listing is empty and their count is zero

#### Scenario: Canonically equivalent spellings are different recipients

- **WHEN** a notification is published for `josé` spelled with a precomposed `é`, and `jose` followed by a combining acute accent lists their notifications
- **THEN** the listing is empty

#### Scenario: Recipients differing only by case or a trailing space are different recipients

- **WHEN** a notification is published for `alice`, and `Alice` and `alice ` each list their notifications
- **THEN** each listing is empty

#### Scenario: A source differing only in its bytes is a different source

- **WHEN** a source is published for a recipient, and a source differing from it only by case, a trailing space, a zero-width space or its normalisation form is published for the same recipient
- **THEN** each creates its own notification, and neither is reported as a duplicate

#### Scenario: Closing a subject leaves a subject that differs only in its bytes open

- **WHEN** notifications are published on subject `task-1` and on subject `Task-1`, and `task-1` is closed
- **THEN** only the notifications on `task-1` are closed, and a later publish on `Task-1` below the close version is still created

#### Scenario: A kind filter matches its kind exactly

- **WHEN** a recipient has notifications of kind `offer` and of kind `Offer`, and lists with a filter on `offer`
- **THEN** only the notifications of kind `offer` are listed

#### Scenario: A schema that compares identifiers otherwise fails verification

- **WHEN** schema verification runs against a MySQL schema whose identifier columns are character strings under any collation, rather than binary strings
- **THEN** verification fails at startup and names each such column, and the documented upgrade makes the same schema pass

#### Scenario: Every store is held to this

- **WHEN** the shared conformance suite runs against the in-memory store and every supported driver and dialect combination
- **THEN** the identity cases pass on every combination, and a store that folds identifiers fails them
