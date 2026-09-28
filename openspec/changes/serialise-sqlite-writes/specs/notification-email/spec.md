## MODIFIED Requirements

### Requirement: Concurrent dispatchers never send the same notification twice

The system SHALL let dispatchers run in several application instances at once, and SHALL ensure that only one of them sends each notification. A dispatcher that stops mid-pass SHALL NOT strand its notifications: once its claim lapses, another dispatcher SHALL take them over, subject to the delivery guarantee for any send already in doubt.

On SQLite, concurrent claims SHALL wait for one another up to the host's busy timeout, whatever transaction mode the host's connection begins transactions in. A claim SHALL NOT fail because another claim holds or is taking the database's write lock.

#### Scenario: Two instances dispatch at the same time

- **WHEN** two dispatchers run passes concurrently over the same qualifying notifications
- **THEN** each notification is sent by exactly one of them

#### Scenario: A stopped dispatcher's unsent claim is taken over

- **WHEN** a dispatcher claims notifications and stops before attempting to send them
- **THEN** after its claim lapses, another dispatcher emails them

#### Scenario: Concurrent claims on SQLite with the driver's default transactions

- **WHEN** sixteen owners claim due notifications at the same time, on SQLite over a connection that sets a busy timeout but no transaction mode
- **THEN** every claim succeeds, and no notification is claimed by two owners
