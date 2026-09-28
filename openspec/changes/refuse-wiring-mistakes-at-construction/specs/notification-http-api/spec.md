## MODIFIED Requirements

### Requirement: The handlers mount on any standard-library-compatible router

The system SHALL provide its handlers as standard library HTTP handlers the host mounts on its own router, behind its own middleware. The system SHALL document how to mount them on the standard library, Gin and Fiber, and SHALL document that on a framework not built on the standard library's HTTP types they run through that framework's adaptor.

The handlers SHALL serve under `/v1` by default, and the host SHALL be able to replace that base path. A trailing slash on a base path SHALL be ignored, and the base path `/` SHALL serve the contract at the root. A base path that does not begin with `/`, an empty base path, or one containing `{` or `}` SHALL be a configuration error at construction, never a route that serves nothing and never a panic. The same rule SHALL apply to every transport the library ships.

#### Scenario: Mounting on the standard library

- **WHEN** a host mounts the handlers on a standard library mux under `/v1`
- **THEN** every endpoint of the contract answers under `/v1`

#### Scenario: A host replaces the base path

- **WHEN** a host builds the handler with the base path `/api/`
- **THEN** alice's `GET /api/notifications` lists her notifications

#### Scenario: The root base path serves the contract at the root

- **WHEN** a host builds the handler with the base path `/`
- **THEN** alice's `GET /notifications` lists her notifications

#### Scenario: A base path without a leading slash is refused at construction

- **WHEN** a host builds the handler with the base path `api`
- **THEN** construction fails with a configuration error, rather than serving a route that answers `GET /api/notifications` with 404

#### Scenario: A base path the router cannot parse is refused at construction

- **WHEN** a host builds the handler with the base path `/a{`
- **THEN** construction fails with a configuration error and does not panic

#### Scenario: An empty base path is refused at construction

- **WHEN** a host builds the handler with an empty base path
- **THEN** construction fails with a configuration error that says to omit the option to keep `/v1`
