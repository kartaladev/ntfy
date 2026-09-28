## MODIFIED Requirements

### Requirement: Storage and wire names carry the library's name by default

Every name the library writes outside Go code SHALL use `ntfy`:
- error text SHALL be prefixed `ntfy:`;
- the WebSocket handler SHALL offer the subprotocol `ntfy.v1`;
- the Redis broadcaster SHALL default to the channel `ntfy.signals`, and the NATS broadcaster to the subject `ntfy.signals`;
- the SQL store SHALL default to the tables `ntfy_notifications`, `ntfy_watermarks` and `ntfy_email_deliveries`, and to indexes named after them.

A consumer SHALL be able to replace the Redis channel, the NATS subject and the table prefix through the existing broadcaster and store options. The subprotocol and the error prefix SHALL have no override: the subprotocol is a versioned client contract, and callers SHALL match errors by identity, not by text.

A table prefix SHALL be at most a documented number of bytes: the most that keeps every table, constraint and index name the store owns within PostgreSQL's 63-byte identifier limit, which is also within MySQL's 64. The SQL store SHALL refuse a longer prefix with a configuration error at construction, rather than let the database truncate names so that they collide and indexes are silently skipped. The bound SHALL be the same on every dialect, so that a schema can move between them.

#### Scenario: A store created with no options uses ntfy tables

- **WHEN** a consumer creates the SQL store with no options and applies its schema
- **THEN** the tables created are `ntfy_notifications` and `ntfy_watermarks`, and applying the email schema adds `ntfy_email_deliveries`

#### Scenario: A consumer prefixes the tables

- **WHEN** a consumer creates the SQL store with the table prefix `app_`
- **THEN** the tables are `app_ntfy_notifications` and `app_ntfy_watermarks`

#### Scenario: The longest permitted prefix yields a complete schema

- **WHEN** a consumer creates the SQL store on PostgreSQL with a prefix of the documented maximum length, and applies both schemas
- **THEN** schema verification reports every table and index present, and a notification can be published

#### Scenario: A prefix longer than the limit is refused at construction

- **WHEN** a consumer creates the SQL store with a 44-byte table prefix
- **THEN** construction fails with a configuration error, before any schema is applied

#### Scenario: Broadcasters default to the ntfy channel and subject

- **WHEN** a consumer creates a Redis broadcaster and a NATS broadcaster with no options
- **THEN** signals travel on the Redis channel `ntfy.signals` and the NATS subject `ntfy.signals`

#### Scenario: A consumer replaces the channel

- **WHEN** a consumer creates a Redis broadcaster with the channel `app-one.signals`
- **THEN** signals travel on `app-one.signals` and nothing is published on `ntfy.signals`

#### Scenario: The WebSocket handler offers the ntfy subprotocol

- **WHEN** a client opens a WebSocket connection offering the subprotocol `ntfy.v1`
- **THEN** the handler selects `ntfy.v1`

#### Scenario: Errors are matched by identity, and their text names ntfy

- **WHEN** a lookup fails because a notification does not exist
- **THEN** the error matches `ntfy.ErrNotFound` with `errors.Is`, and its text begins with `ntfy:`

## ADDED Requirements

### Requirement: An option given nothing fails at construction

Every configuration option, in every module, that takes a function or an interface SHALL refuse nil with a configuration error from the constructor it is passed to. It SHALL NOT silently keep the default. The default SHALL be kept by omitting the option. A nil option value itself, rather than an option given nil, SHALL be skipped, so that a host can build its option list conditionally.

#### Scenario: A nil clock is refused

- **WHEN** a host builds a service with the clock option given nil
- **THEN** construction fails with a configuration error and no service is returned

#### Scenario: A nil prune error handler is refused

- **WHEN** a host builds a pruner with the error handler option given nil
- **THEN** construction fails with a configuration error

#### Scenario: A nil decode error handler is refused

- **WHEN** a host builds a Redis or NATS broadcaster with the decode error handler option given nil
- **THEN** construction fails with a configuration error

#### Scenario: A nil option value is skipped

- **WHEN** a host builds a service with a nil value in its option list
- **THEN** the service is built with its defaults

#### Scenario: Omitting an option keeps its default

- **WHEN** a host builds a service with no clock option
- **THEN** the service reads the system clock
