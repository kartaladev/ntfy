## ADDED Requirements

### Requirement: Requests that change notifications are refused from foreign browser origins by default

The system SHALL, by default, refuse a browser request that would change notifications when it comes from a page on an origin other than the one the request was made to. This covers marking one notification read and marking all read. The refusal SHALL be answered as forbidden, SHALL happen before the acting user is established, and SHALL change nothing. The refusal SHALL apply equally when the host registers the contract's routes one at a time.

The system SHALL continue to serve:
- requests from the application's own pages;
- requests from clients that are not browsers, which send no browser origin information;
- requests that only read, such as listing, counting and opening the stream.

The host SHALL be able to permit a documented list of trusted origins, each written as `scheme://host[:port]`, or to permit every origin through an explicitly named opt-out. A trusted origin not written in that form SHALL be a configuration error at construction. So SHALL naming trusted origins and opting out at once.

#### Scenario: A cross-site form cannot mark all read

- **WHEN** alice's browser, carrying the session the host reads her identity from, submits a form from a page on `evil.example` to `POST /v1/notifications/read-all`
- **THEN** the response is `403` with the `forbidden` code, and every one of alice's notifications keeps its state

#### Scenario: A cross-site form cannot mark one notification read

- **WHEN** a page on `evil.example` submits a form to `POST /v1/notifications/{id}/read` for alice's notification, with her session
- **THEN** the response is `403`, and the notification is still ACTIVE

#### Scenario: A browser that does not send Sec-Fetch-Site is judged by its origin

- **WHEN** a browser sends a request to mark all read with the origin `https://evil.example` to the host `app.example.com`, without a `Sec-Fetch-Site` header
- **THEN** the response is `403`, and nothing is marked

#### Scenario: The application's own page is served

- **WHEN** a page served from the application's own origin marks all read
- **THEN** the request is served as before

#### Scenario: A client that is not a browser is served

- **WHEN** a client that sends neither an origin nor a fetch-site header marks a notification read
- **THEN** the request is served as before

#### Scenario: A cross-site read is not refused

- **WHEN** a page on another site requests alice's unread count
- **THEN** the request is not refused by this rule

#### Scenario: A host trusts a sibling origin

- **WHEN** a host trusts `https://web.example.com`, and a page from that origin marks all read on `app.example.com`
- **THEN** the request is served

#### Scenario: A host opts out

- **WHEN** a host opts out of cross-origin protection through the named option, and a page on another site marks a notification read
- **THEN** the request is served

#### Scenario: A malformed trusted origin is refused at construction

- **WHEN** a host trusts `app.example.com`, which has no scheme
- **THEN** construction fails with a configuration error

#### Scenario: Trusting origins and opting out together is refused at construction

- **WHEN** a host both names trusted origins and opts out of cross-origin protection
- **THEN** construction fails with a configuration error

## MODIFIED Requirements

### Requirement: The contract exposes list, count, mark read, mark all read and stream

Under a base path, `/v1` by default and configurable, the system SHALL expose:

- `GET /notifications`: the acting user's notifications, newest first. Optional filters are state, kind and subject. The page size defaults to 50 and is capped at 500. Paging uses a continuation cursor.
- `GET /notifications/count`: the number of the acting user's ACTIVE notifications.
- `POST /notifications/{id}/read`: mark one notification read.
- `POST /notifications/read-all`: mark everything read up to an instant the client may supply, defaulting to the time of the request.
- `GET /notifications/stream`: a server-sent event stream of the acting user's change signals and heartbeats.

A notification SHALL be returned with its identifier, kind, state, closed reason, title, links, data and timestamps. Its data SHALL be returned exactly as published.

A `read-all` request that carries a body SHALL declare it as `application/json`; media type parameters such as a charset are allowed. A body declared as anything else, or not declared at all, SHALL be refused as a malformed request, and nothing SHALL be marked. A request with no body needs no declaration.

#### Scenario: Listing a page with a cursor

- **WHEN** alice requests `GET /v1/notifications?limit=2` over five notifications, then requests the returned cursor
- **THEN** the first response holds her two newest notifications and a cursor, and the second holds the next two

#### Scenario: Counting unread notifications

- **WHEN** alice, with three ACTIVE notifications, requests `GET /v1/notifications/count`
- **THEN** the response is `200` with a count of 3

#### Scenario: Marking one notification read

- **WHEN** alice requests `POST /v1/notifications/{id}/read` for her ACTIVE notification
- **THEN** the response is `200` with the notification now READ

#### Scenario: Opening the stream

- **WHEN** alice requests `GET /v1/notifications/stream`
- **THEN** the response is a server-sent event stream that delivers her change signals and heartbeats until she disconnects

#### Scenario: A read-all body not declared as JSON is refused

- **WHEN** alice requests `POST /v1/notifications/read-all` with the body `{"through":"2999-01-01T00:00:00Z"}` declared as `text/plain`, as a form encoding, or not declared
- **THEN** the response is `400` with a validation error code, and none of her notifications is marked read

#### Scenario: A read-all body declared as JSON with a charset is accepted

- **WHEN** alice requests `POST /v1/notifications/read-all` with a JSON body declared as `application/json; charset=utf-8`
- **THEN** the response is `200`, and her notifications up to the instant are marked read

### Requirement: Errors map to status codes consistently

The system SHALL answer:

- a malformed request (a bad cursor, an out-of-range page size, an unknown state filter, an unparseable instant, a query string the server cannot parse, a filter carrying more values than the contract allows, a `read-all` body not declared as JSON) with `400`;
- a missing acting user, a refused subscription, or a browser request from a foreign origin that would change notifications with `403`;
- an unknown notification, or one belonging to someone else, with `404`, identically, so that identifiers cannot be probed;
- too many streams for one acting user, or for the instance, with `429`;
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

#### Scenario: A cross-origin write is forbidden in the shared error shape

- **WHEN** a page on another site requests `POST /v1/notifications/read-all` with alice's session
- **THEN** the response is `403` with the `forbidden` code and a message, in the same body shape as every other error

#### Scenario: An unanticipated failure hides its detail

- **WHEN** the notification store fails with a driver error while alice lists
- **THEN** the response is `500` with a generic message and no driver detail
