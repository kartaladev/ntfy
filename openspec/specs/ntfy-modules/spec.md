# ntfy-modules Specification

## Purpose

Defines the module layout that applications embedding ntfy depend on: the import paths and package names, which modules a consumer must take and which are opt-in, and the dependency boundary that keeps ntfy independent of the task engine it came from.

## Requirements

### Requirement: The library is imported from its own repository under package ntfy

The library SHALL be published as the module `github.com/kartaladev/ntfy`, and its root package SHALL be named `ntfy`, so a consumer imports it without an alias. Every other module SHALL live under that path, and its package name SHALL match the last element of its import path: `github.com/kartaladev/ntfy/ntfytest`, `github.com/kartaladev/ntfy/sqlstore`, `github.com/kartaladev/ntfy/websocket`, `github.com/kartaladev/ntfy/redis` and `github.com/kartaladev/ntfy/nats`.

#### Scenario: A consumer imports the library without an alias

- **WHEN** a consumer adds `import "github.com/kartaladev/ntfy"`
- **THEN** the package is available as `ntfy` without an import alias

#### Scenario: No module is published under the hmntsk path

- **WHEN** the modules of this repository are listed
- **THEN** no module path starts with `github.com/kartaladev/hmntsk`

### Requirement: The core module needs nothing beyond the standard library

The root module's production code SHALL import only the Go standard library. A consumer who takes only `github.com/kartaladev/ntfy` SHALL be able to run a complete single-process setup (the in-memory store, the SSE handlers and the in-process broadcaster) without pulling any database driver, broker client or WebSocket library. Durable storage, WebSocket transport and cross-instance broadcasting SHALL be separate modules that a consumer adds only when they use them.

#### Scenario: The core module builds with no third-party production dependency

- **WHEN** the root module's production (non-test) imports are listed
- **THEN** every import is from the standard library or from the root module itself

#### Scenario: A single-process setup needs only the core module

- **WHEN** a consumer builds a service on the in-memory store with the in-process broadcaster and mounts the HTTP handlers
- **THEN** their module requires `github.com/kartaladev/ntfy` and no other module of this repository

#### Scenario: A consumer opts into durable storage

- **WHEN** a consumer adds `github.com/kartaladev/ntfy/sqlstore` and passes its store to the service in place of the in-memory store
- **THEN** notifications are stored durably, and no other module of this repository is required

### Requirement: ntfy depends on no task-engine module

No module in this repository SHALL import a `github.com/kartaladev` module other than a module of this repository or a `github.com/kartaladev/sqlkit` module, including from test code. The build SHALL fail when one does.

#### Scenario: An import of an hmntsk module fails the build

- **WHEN** any file in any module of this repository, test files included, imports a `github.com/kartaladev/hmntsk` package
- **THEN** the dependency boundary check fails and names the module and the offending import

#### Scenario: A module outside ntfy and sqlkit is refused

- **WHEN** a test in `sqlstore` imports a `github.com/kartaladev` module that is neither ntfy nor sqlkit
- **THEN** the dependency boundary check fails

#### Scenario: Importing sqlkit is permitted

- **WHEN** `sqlstore` imports `github.com/kartaladev/sqlkit` and its executor modules
- **THEN** the dependency boundary check passes

### Requirement: Storage and wire names carry the library's name by default

Every name the library writes outside Go code SHALL use `ntfy`:
- error text SHALL be prefixed `ntfy:`;
- the WebSocket handler SHALL offer the subprotocol `ntfy.v1`;
- the Redis broadcaster SHALL default to the channel `ntfy.signals`, and the NATS broadcaster to the subject `ntfy.signals`;
- the SQL store SHALL default to the tables `ntfy_notifications`, `ntfy_watermarks` and `ntfy_email_deliveries`, and to indexes named after them.

A consumer SHALL be able to replace the Redis channel, the NATS subject and the table prefix through the existing broadcaster and store options. The subprotocol and the error prefix SHALL have no override: the subprotocol is a versioned client contract, and callers SHALL match errors by identity, not by text.

#### Scenario: A store created with no options uses ntfy tables

- **WHEN** a consumer creates the SQL store with no options and applies its schema
- **THEN** the tables created are `ntfy_notifications` and `ntfy_watermarks`, and applying the email schema adds `ntfy_email_deliveries`

#### Scenario: A consumer prefixes the tables

- **WHEN** a consumer creates the SQL store with the table prefix `app_`
- **THEN** the tables are `app_ntfy_notifications` and `app_ntfy_watermarks`

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
