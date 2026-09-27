## ADDED Requirements

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

## MODIFIED Requirements

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
