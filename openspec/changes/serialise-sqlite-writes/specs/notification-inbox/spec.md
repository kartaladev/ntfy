## MODIFIED Requirements

### Requirement: Every store behaves identically

The system SHALL behave identically on every store it supports: in memory, and on PostgreSQL, MySQL and SQLite through each supported database driver. It SHALL assert this with one shared suite of behavioural cases executed against every combination, not with per-store tests.

On SQLite, the system SHALL behave identically whatever transaction mode the host's connection begins transactions in, deferred (the driver's default) or immediate. Concurrent writes SHALL wait for one another up to the host's busy timeout, and SHALL NOT fail because another write holds or is taking the database's write lock. The host SHALL NOT need to choose a transaction mode for correctness. What a SQLite host must configure (a busy timeout) and what it need not (a transaction mode) SHALL be documented.

#### Scenario: The shared suite passes everywhere

- **WHEN** the shared notification store suite runs against the in-memory store and every supported driver and dialect combination
- **THEN** every case passes on every combination

#### Scenario: The shared suite passes on SQLite with the driver's default transactions

- **WHEN** the shared notification store suite runs on SQLite through each supported driver, over a connection that sets a busy timeout but no transaction mode
- **THEN** every case passes, as it does over a connection that begins transactions immediately

#### Scenario: Concurrent mark-read calls on SQLite all succeed

- **WHEN** sixteen callers each mark a different one of a recipient's notifications read at the same time, on SQLite over a connection that sets a busy timeout but no transaction mode
- **THEN** every call succeeds and every notification is READ

#### Scenario: A mark-read racing a publish on SQLite succeeds

- **WHEN** callers mark notifications read while others publish to another subject at the same time, on SQLite over a connection that sets a busy timeout but no transaction mode
- **THEN** every call succeeds
