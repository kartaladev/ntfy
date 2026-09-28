# notification-inbox Specification

## Purpose
Defines durable, per-recipient notifications that know nothing about the domain that publishes them: how they are published without duplication, how they move between active, read and closed, how a subject's notifications are closed without a late source reopening them, and how a recipient reads them. It behaves identically on every supported store.

## Requirements

### Requirement: A notification belongs to exactly one recipient and is in one of three states

The system SHALL record each notification for exactly one recipient. It SHALL keep each notification in one of three states:

- **ACTIVE**: the recipient has not read it and nothing has closed it;
- **READ**: the recipient has read it;
- **CLOSED**: its subject moved on, with a closed reason recorded.

A notification SHALL never return to ACTIVE once it has left that state. The system SHALL treat a notification's kind, title, links and data as opaque, storing and returning them unchanged.

Opacity SHALL govern what the system does with content it accepts, not whether it accepts it. The system SHALL validate a draft's content against documented size limits and form rules before storing it, and SHALL NOT alter, normalise or reinterpret any content it accepts.

#### Scenario: A new notification is active

- **WHEN** a notification is published for a recipient
- **THEN** it is ACTIVE, with no read or closed time

#### Scenario: Opaque content is returned unchanged

- **WHEN** a notification is published with a kind, title, links by relation name and a JSON data payload
- **THEN** reading it back returns the kind, title, links and data exactly as published, the payload's field order and number literals included

#### Scenario: A read notification cannot become active again

- **WHEN** a READ notification is published again, or targeted by any other operation
- **THEN** it is not ACTIVE afterwards

### Requirement: Publishing is idempotent per source and recipient

The system SHALL identify a published notification by its source identifier together with its recipient. Publishing the same source for the same recipient again SHALL create nothing, SHALL leave the existing notification unchanged whatever its state, and SHALL NOT be an error. A publish result SHALL report which notifications were created, and how many were duplicates.

#### Scenario: A redelivered source creates no duplicate

- **WHEN** a source is published for recipients alice and bob, and the same source is published for alice and bob again
- **THEN** alice and bob each have exactly one notification from that source, and the second publish reports two duplicates and nothing created

#### Scenario: A redelivery does not undo a read

- **WHEN** alice reads a notification and its source is published for her again
- **THEN** the notification is still READ

#### Scenario: One source addresses many recipients

- **WHEN** one source is published for alice, bob and carol
- **THEN** each has their own notification, which each can read without affecting the others

### Requirement: A subject's notifications can be closed by kind and version

The system SHALL close the notifications of a subject in one operation, and SHALL let that operation:

- restrict closing to some kinds, or apply to every kind;
- close only notifications whose subject version is at or below a given version;
- spare one named recipient.

Closing SHALL move ACTIVE and READ notifications to CLOSED with the given reason, SHALL leave CLOSED notifications unchanged, and SHALL report how many notifications it closed and which recipients they belonged to.

#### Scenario: Closing one kind on a subject

- **WHEN** a subject has ACTIVE notifications of kinds "offer" and "assigned", and its "offer" notifications are closed at the subject's current version with reason "taken"
- **THEN** every "offer" notification on the subject is CLOSED with reason "taken", and every "assigned" notification is unchanged

#### Scenario: Sparing one recipient

- **WHEN** a subject's "offer" notifications for alice, bob and carol are closed sparing carol
- **THEN** alice's and bob's are CLOSED, carol's is unchanged, and the result names alice and bob

#### Scenario: A newer notification survives an older close

- **WHEN** a subject has an "assigned" notification at version 7, and the subject's "assigned" notifications are closed at version 6
- **THEN** the version-7 notification is unchanged

#### Scenario: A read notification can still be closed

- **WHEN** a READ notification's subject is closed
- **THEN** the notification is CLOSED, and it keeps the time it was read

### Requirement: A late source never reopens a closed subject

The system SHALL remember, for each subject and kind, the highest version at which notifications were closed, and for each subject the highest version at which every kind was closed. Publishing a notification whose subject version is below the remembered version for its kind SHALL create nothing, and SHALL report the notification as suppressed rather than failing.

Closing and publishing for the same subject SHALL NOT interleave in a way that leaves an ACTIVE notification below a remembered close version, even when they run concurrently on different application instances.

#### Scenario: A retried older source is suppressed

- **WHEN** a subject's "offer" notifications are closed at version 5, and afterwards a source at version 1 publishes an "offer" on that subject
- **THEN** nothing is created, and the publish reports one suppressed notification

#### Scenario: A newer source still publishes after a close

- **WHEN** a subject's "offer" notifications are closed at version 5, and afterwards a source at version 6 publishes an "offer" on that subject
- **THEN** the offer is created ACTIVE

#### Scenario: Closing every kind suppresses every kind

- **WHEN** a subject's notifications of every kind are closed at version 9, and afterwards a source at version 8 publishes a "taken" notification on that subject
- **THEN** nothing is created

#### Scenario: Concurrent close and late publish

- **WHEN** a close at version 5 and a publish at version 3 for the same subject and kind run concurrently on two connections
- **THEN** once both have finished, no ACTIVE notification of that kind exists on the subject below version 5

### Requirement: A close can tell each recipient it closed what happened, atomically

The system SHALL let a close name a successor notification. In the same transaction as the close, the system SHALL publish the successor to each recipient whose notification that close moved to CLOSED, except recipients the close names to skip. Successors SHALL be subject to the same idempotency and watermark suppression as any publish. A close result SHALL report the successors it created.

A close that is retried after an earlier attempt committed SHALL close nothing further and SHALL create no successors, and the successors the earlier attempt created SHALL remain.

#### Scenario: Closing offers tells the other recipients

- **WHEN** a subject has "offer" notifications for alice, bob and carol, and its "offer" notifications are closed at version 5 with a "taken" successor, skipping carol
- **THEN** all three offers are CLOSED, and alice and bob each have one ACTIVE "taken" notification at version 5, and carol has none

#### Scenario: A retried close creates no further successors

- **WHEN** the same close with a successor runs twice
- **THEN** each recipient has exactly one successor notification from that close

#### Scenario: A successor below a newer watermark is suppressed

- **WHEN** a subject's "taken" notifications are closed at version 8, and afterwards an older close at version 5 closes offers with a "taken" successor at version 5
- **THEN** no "taken" notification is created, and the close result reports the successors as suppressed

### Requirement: A coalescing publish does not repeat an open notification

The system SHALL let a publisher mark a notification as coalescing. A coalescing notification SHALL create nothing when its recipient already has an ACTIVE or READ notification of the same kind on the same subject, and SHALL be reported as coalesced rather than failing. The decision SHALL be made inside the subject's serialised write, so that concurrent publishes cannot both create one.

#### Scenario: An open offer absorbs a coalescing offer

- **WHEN** alice has an ACTIVE "offer" on a subject, and a coalescing "offer" from a new source is published for alice and bob on that subject
- **THEN** alice still has one offer, bob has a new ACTIVE offer, and the publish reports one coalesced and one created

#### Scenario: A read notification also absorbs it

- **WHEN** alice has a READ "offer" on a subject, and a coalescing "offer" is published for her on that subject
- **THEN** nothing is created for alice

#### Scenario: A closed notification does not absorb it

- **WHEN** alice's "offer" on a subject is CLOSED, and a coalescing "offer" at a version above the close is published for her
- **THEN** a new ACTIVE offer is created for alice

### Requirement: A recipient can list, count and mark their notifications read

The system SHALL let a recipient:

- list their own notifications, newest first, filtered by state, kind or subject, in pages that repeat and skip nothing however many notifications arrive between pages;
- count their ACTIVE notifications;
- mark chosen notifications read;
- mark every notification read up to a given instant.

Marking an ACTIVE notification read SHALL make it READ. Marking a READ notification read SHALL succeed and change nothing. Marking a CLOSED notification read SHALL record the read time and leave it CLOSED. A recipient SHALL NOT be able to read, list, count or mark another recipient's notifications through these operations. A notification of another recipient SHALL be reported as not found.

A listing SHALL bound how many values one filter may carry, as it already bounds a page size, and SHALL refuse a query that exceeds the bound with a validation error naming the filter. The bound SHALL be a documented named constant the host can replace.

#### Scenario: Newest first with exact paging

- **WHEN** a recipient lists with a page size of 2 over five notifications, and a sixth arrives before the second page is requested
- **THEN** the pages together return the original five exactly once, newest first

#### Scenario: Counting counts only active notifications

- **WHEN** a recipient has three ACTIVE, two READ and one CLOSED notification
- **THEN** their count is 3

#### Scenario: Mark all read does not swallow what arrived later

- **WHEN** a recipient marks everything read up to the instant they loaded their list, and a notification created after that instant already exists
- **THEN** that later notification is still ACTIVE

#### Scenario: Another recipient's notification is not found

- **WHEN** bob marks alice's notification read
- **THEN** the operation reports not found, and alice's notification is unchanged

#### Scenario: A filter carrying too many values is refused

- **WHEN** a recipient lists with more kinds, or more states, than the configured filter bound
- **THEN** the query is refused as a validation error naming the filter, and no statement is sent to the store

### Requirement: Every store behaves identically

The system SHALL behave identically on every store it supports: in memory, and on PostgreSQL, MySQL and SQLite through each supported database driver. It SHALL assert this with one shared suite of behavioural cases executed against every combination, not with per-store tests.

#### Scenario: The shared suite passes everywhere

- **WHEN** the shared notification store suite runs against the in-memory store and every supported driver and dialect combination
- **THEN** every case passes on every combination

### Requirement: Notification writes do not join the caller's transaction

The system SHALL perform each notification write in a transaction of its own, and SHALL NOT join a transaction the caller has open for other data. This limit SHALL be documented.

#### Scenario: A rolled-back caller transaction does not roll back notifications

- **WHEN** a caller publishes a notification while holding its own open transaction, and then rolls its transaction back
- **THEN** the notification still exists

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

### Requirement: A draft's content and a publish's size are bounded

The system SHALL validate every draft against documented limits before anything is written, and SHALL refuse a draft that exceeds one with a validation error naming the field. It SHALL NOT truncate, trim or otherwise alter content in order to make it fit.

The system SHALL bound:

- the length of a title;
- the number of links a draft carries, the length of each link's relation name, and the length of each link's href;
- the size of a data payload;
- the number of drafts one publish may carry, because the drafts of one subject are written in a single transaction.

Each limit SHALL be a documented named constant, and the host SHALL be able to replace every one of them. A limit that is not positive, or a set of limits that contradicts itself, SHALL be a configuration error at construction rather than a surprise at first publish.

A draft SHALL be validated against the same limits wherever it is validated, so that a draft accepted by one instance of the library configured a given way is accepted by every other configured the same way.

#### Scenario: An oversized payload is refused

- **WHEN** a draft carries a data payload larger than the configured limit
- **THEN** the publish creates nothing and reports a validation error naming the data field, and the payload is neither stored nor truncated

#### Scenario: An oversized title is refused

- **WHEN** a draft carries a title longer than the configured limit
- **THEN** the publish creates nothing and reports a validation error naming the title

#### Scenario: Too many links are refused

- **WHEN** a draft carries more links than the configured limit, or a link whose relation name or href is longer than its limit
- **THEN** the publish creates nothing and reports a validation error naming the link

#### Scenario: Too many drafts in one publish are refused

- **WHEN** a publisher calls publish with more drafts than the configured limit
- **THEN** nothing is created and the publish reports a validation error, before any transaction is opened

#### Scenario: A host raises a limit

- **WHEN** a host configures a data limit larger than the default and publishes a payload between the two
- **THEN** the notification is created, and reading it back returns the payload byte for byte

#### Scenario: A meaningless limit is refused at construction

- **WHEN** a host configures a content limit of zero or a negative number
- **THEN** construction fails with a configuration error and no traffic is served

### Requirement: A link's href is checked for form before it is stored

The system SHALL check the form of every link href a draft carries, and SHALL refuse a draft whose href uses a scheme the configuration does not permit. By default it SHALL permit only hrefs that are relative references or use the `http` or `https` scheme, so that a scheme whose only effect is to execute code in a recipient's client is never stored by default.

The host SHALL be able to permit further schemes by naming them, and SHALL be able to disable the check entirely through an explicitly named opt-out, so that storing any scheme is a decision the host records rather than a default. Naming schemes and opting out of the check at the same time SHALL be a configuration error at construction.

The check SHALL be on form only. The system SHALL NOT interpret where a link points, SHALL NOT rewrite or normalise an accepted href, and SHALL return every accepted href exactly as published.

#### Scenario: A script-bearing link is refused by default

- **WHEN** a draft carries a link whose href uses the `javascript` scheme
- **THEN** the publish creates nothing and reports a validation error naming the link

#### Scenario: Ordinary links are accepted by default

- **WHEN** a draft carries an absolute `https` href and a relative href
- **THEN** both are stored, and reading the notification back returns each href exactly as published

#### Scenario: A host permits another scheme

- **WHEN** a host configures the permitted schemes to include `mailto` and publishes a draft carrying a `mailto` href
- **THEN** the notification is created and the href is returned unchanged

#### Scenario: A host opts out of the check

- **WHEN** a host disables the scheme check through the named opt-out and publishes a draft carrying any scheme
- **THEN** the notification is created and the href is returned unchanged

#### Scenario: Naming schemes and opting out at once is refused at construction

- **WHEN** a host both names the permitted schemes and disables the check
- **THEN** construction fails with a configuration error

### Requirement: Identifiers are compared byte for byte on every store

The system SHALL treat two identifiers as equal only when they are the same sequence of bytes. This applies to a notification's recipient, source identifier, subject and kind, on every store. A store SHALL NOT fold case, ignore trailing spaces, ignore code points that a collation treats as ignorable, or apply Unicode normalisation when it matches, filters, de-duplicates or closes by an identifier. The system SHALL return every identifier exactly as it was published.

A store supplied by a host SHALL be held to this by the shared conformance suite. The system SHALL NOT require a newer database server than it already supports in order to provide this.

A schema whose identifier columns would compare other than byte for byte SHALL be reported by schema verification at startup, not discovered as a wrong match in traffic.

An identifier holding a NUL byte or a byte sequence that is not valid UTF-8 SHALL be refused by validation with an error naming the field, before anything is written, wherever an identifier is validated. At least one supported store cannot hold either, and the same request SHALL fail the same way on every store.

The identifiers a host's ID generator mints SHALL be between 1 and a documented number of bytes. The system SHALL refuse any other with a configuration error, writing nothing under it. A close whose successor cannot be given an identifier SHALL change nothing. An email that cannot be given a message identifier for this reason SHALL NOT spend a delivery attempt.

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

#### Scenario: An identifier holding a NUL byte or invalid UTF-8 is refused

- **WHEN** a draft's recipient, source, subject or kind, a listing's recipient, subject or kind, or a close's subject, kinds, exception or successor skips holds a NUL byte or invalid UTF-8
- **THEN** validation refuses the request with an error naming the field, and nothing is written on any store

#### Scenario: A close whose successor cannot be given an identifier changes nothing

- **WHEN** a subject is closed with a successor, and the host's ID generator fails or mints an identifier longer than the documented limit
- **THEN** the close reports the error, every notification it would have closed is still open, no successor is written, and a later publish below the close version is not suppressed

#### Scenario: Every store is held to this

- **WHEN** the shared conformance suite runs against the in-memory store and every supported driver and dialect combination
- **THEN** the identity cases pass on every combination, and a store that folds identifiers fails them
