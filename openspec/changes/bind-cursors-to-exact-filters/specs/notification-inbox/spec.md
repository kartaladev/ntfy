## MODIFIED Requirements

### Requirement: A recipient can list, count and mark their notifications read

The system SHALL let a recipient:

- list their own notifications, newest first, filtered by state, kind or subject, in pages that repeat and skip nothing however many notifications arrive between pages;
- count their ACTIVE notifications;
- mark chosen notifications read;
- mark every notification read up to a given instant.

Marking an ACTIVE notification read SHALL make it READ. Marking a READ notification read SHALL succeed and change nothing. Marking a CLOSED notification read SHALL record the read time and leave it CLOSED. A recipient SHALL NOT be able to read, list, count or mark another recipient's notifications through these operations. A notification of another recipient SHALL be reported as not found.

A listing SHALL bound how many values one filter may carry, as it already bounds a page size, and SHALL refuse a query that exceeds the bound with a validation error naming the filter. The bound SHALL be a documented named constant the host can replace.

A listing's continuation cursor SHALL be bound to the recipient and the exact filters of the listing that produced it. Those are the same recipient, the same set of states, the same set of kinds and the same subject, where the order in which a filter lists its values does not matter. A cursor presented with any other recipient or filters SHALL be refused as a validation error, and no page SHALL be returned for it. The binding SHALL hold whatever bytes a recipient, kind or subject contains. A character inside one value, such as a comma, SHALL NOT make two different filter sets one. Listing with no kind filter SHALL NOT be the same filter as listing with a filter that names only the empty kind.

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

#### Scenario: A cursor continues only the filters that produced it

- **WHEN** alice lists with the kinds `offer` and `taken` and a page size of 1, and requests the next page with the returned cursor and the same two kinds listed as `taken`, `offer`
- **THEN** the next page is returned
- **AND** the same cursor presented with only the kind `offer`, with another subject, or as bob is refused as a validation error

#### Scenario: A comma inside a kind does not merge two filter sets

- **WHEN** alice's cursor was produced under the two kinds `a` and `b`, and she presents it under the single kind `a,b`
- **THEN** the query is refused as a validation error

#### Scenario: No kind filter is not the empty kind

- **WHEN** alice's cursor was produced with no kind filter, and she presents it under a kind filter naming only the empty kind
- **THEN** the query is refused as a validation error
