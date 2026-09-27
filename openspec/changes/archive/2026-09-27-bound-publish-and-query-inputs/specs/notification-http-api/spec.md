## MODIFIED Requirements

### Requirement: Errors map to status codes consistently

The system SHALL answer:

- a malformed request (a bad cursor, an out-of-range page size, an unknown state filter, an unparseable instant, a query string the server cannot parse, a filter carrying more values than the contract allows) with `400`;
- a missing acting user or a refused subscription with `403`;
- an unknown notification, or one belonging to someone else, with `404`, identically, so that identifiers cannot be probed;
- too many streams for one recipient with `429`;
- a stream requested while signals are not being received with `503`;
- anything unanticipated with `500`, without internal detail.

A request the system cannot parse SHALL be refused. The system SHALL NOT answer such a request by acting on the part of it that parsed: in particular, a listing whose query string is rejected SHALL NOT be served with its filters dropped.

Every error SHALL carry a body with a stable, machine-readable code and a human-readable message, in one shape shared by every endpoint and every status.

#### Scenario: Someone else's notification is not found

- **WHEN** bob requests `POST /v1/notifications/{id}/read` for alice's notification
- **THEN** the response is `404`, identical to the response for an identifier that does not exist

#### Scenario: A malformed cursor is a bad request

- **WHEN** alice requests `GET /v1/notifications?cursor=not-a-cursor`
- **THEN** the response is `400` with a validation error code

#### Scenario: An unparseable query is refused, not served unfiltered

- **WHEN** alice requests `GET /v1/notifications` with a query string the server refuses to parse, such as one carrying more parameters than it will accept
- **THEN** the response is `400` with a validation error code, and no listing is returned

#### Scenario: An oversized filter is a bad request

- **WHEN** alice requests `GET /v1/notifications` with more `kind` parameters than the contract allows
- **THEN** the response is `400` with a validation error code naming the filter

#### Scenario: An unanticipated failure hides its detail

- **WHEN** the notification store fails with a driver error while alice lists
- **THEN** the response is `500` with a generic message and no driver detail
