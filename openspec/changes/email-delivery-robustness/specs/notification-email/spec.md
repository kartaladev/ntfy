## MODIFIED Requirements

### Requirement: A notification is emailed at most once by default

By default the system SHALL email each notification at most once. A send that may have happened, because the process stopped or the sender could not tell, SHALL NOT be repeated and SHALL be recorded as abandoned. The host SHALL be able to choose at-least-once delivery instead, under which an in-doubt send is repeated with the same idempotency key as the original attempt, so a sender that honours the key delivers it once. Every message SHALL carry an idempotency key that stays the same for every attempt of the same message.

Under at-least-once delivery, a repeated send SHALL cover a subset of the notifications the original attempt covered: the system SHALL NOT add a notification to a message it has already attempted, and SHALL drop any notification that stopped being ACTIVE since, as it does before a first attempt. Where every notification of an in-doubt message has stopped being ACTIVE, the system SHALL send nothing further for it.

The system SHALL state as limits that a repeated send is rendered again at each attempt rather than replayed from a stored copy, and therefore that a message template changed between two attempts produces different content under one idempotency key; and that a sender which ignores the idempotency key may deliver both.

#### Scenario: A crash after sending does not resend by default

- **WHEN** a dispatcher sends alice's message and stops before recording it, and a later pass runs after its lease expired
- **THEN** the message is not sent again, and the notifications are recorded as abandoned

#### Scenario: A host chooses at-least-once delivery

- **WHEN** a dispatcher configured for at-least-once delivery sends alice's message and stops before recording it, and a later pass runs after its lease expired
- **THEN** the message is sent again for those of the original notifications that are still ACTIVE, with the same idempotency key

#### Scenario: A resend does not absorb newer notifications

- **WHEN** an in-doubt message is resent under at-least-once delivery while alice has received a newer notification
- **THEN** the resent message covers none of the newer notification, and the newer one goes in a separate message

#### Scenario: A resend drops what the recipient has read since

- **WHEN** an in-doubt message covering three of alice's notifications is resent under at-least-once delivery after she has read one of them
- **THEN** the resent message covers the two still ACTIVE, carries the original idempotency key, and the notification she read is recorded as skipped and never emailed

#### Scenario: Nothing is resent once every notification has been read

- **WHEN** an in-doubt message is resent under at-least-once delivery after the recipient has read every notification it covered
- **THEN** no message is sent, and those notifications are recorded as skipped

### Requirement: Send failures are classified, retried within a budget, and reported

The system SHALL treat a send the host's sender reports as rejected as a permanent failure, recorded as failed and never retried. The system SHALL retry any other failure that means the message was not sent, after a delay that grows with the attempts already made, up to an attempt limit, after which it SHALL record the notifications as failed. A failure to render a message SHALL be a permanent failure. Every failure SHALL be reported to the host rather than silently swallowed, and SHALL NOT abandon the rest of the pass.

Every retry SHALL be scheduled strictly later than the failure that caused it. Whatever the attempts made, the configured delay and the configured ceiling, the delay SHALL be positive and SHALL NOT exceed the ceiling by more than the jitter the system applies. No combination of accepted configuration and attempt count SHALL produce a delay that is zero, negative, or shorter than an earlier attempt's ceiling-bounded delay.

#### Scenario: The sender rejects a message

- **WHEN** the host's sender reports alice's message as rejected
- **THEN** her notifications are recorded as failed, are not retried, and the error reaches the host

#### Scenario: A transient failure is retried until the limit

- **WHEN** the host's sender fails transiently for alice on every attempt
- **THEN** her notifications are attempted again with growing delays, and are recorded as failed once the attempt limit is reached

#### Scenario: One recipient's failure does not stop the pass

- **WHEN** rendering fails for alice and succeeds for bob in the same pass
- **THEN** bob is emailed, and the failure for alice reaches the host

#### Scenario: A very large ceiling never schedules a retry in the past

- **WHEN** a host configures the largest ceiling the system accepts and a notification fails transiently on attempt after attempt
- **THEN** every retry is scheduled after the failure that caused it, no attempt is scheduled at or before the instant it was computed, and no notification is re-claimed sooner than the ceiling allows

### Requirement: Wiring mistakes fail at construction

The system SHALL refuse to construct a dispatcher that has no sender, no address lookup or no template, whose notification store does not record email deliveries, or whose durations, caps or limits are zero or negative where a positive value is required. It SHALL also refuse a value it could not store, such as a dispatcher owner longer than the delivery record admits, so that a configuration accepted on one supported store cannot fail at run time on another.

#### Scenario: A dispatcher without a sender

- **WHEN** a host constructs a dispatcher without a sender
- **THEN** construction fails with a configuration error

#### Scenario: A store that cannot record email deliveries

- **WHEN** a host constructs a dispatcher over a notification store that does not support email delivery records
- **THEN** construction fails with a configuration error

#### Scenario: A negative grace delay

- **WHEN** a host configures a negative grace delay
- **THEN** construction fails with a configuration error

#### Scenario: An owner too long to store

- **WHEN** a host configures a dispatcher owner longer than the delivery record admits
- **THEN** construction fails with a configuration error naming the limit, on every supported store alike

## ADDED Requirements

### Requirement: A recorded failure reason carries no host data by default

With no configuration, the system SHALL record, against a failed, retried or skipped delivery, a reason it owns itself: the classification of the outcome, and never text obtained from the host's sender, template or address lookup. The full error SHALL continue to reach the host's error handler unchanged, where the host applies its own logging and redaction policy.

The host SHALL be able to record detail of its own choosing by supplying a rule that turns a failure into the text to record. The system SHALL bound what is recorded to a documented maximum length, truncating beyond it, so that a delivery record cannot grow without limit whatever the host returns.

#### Scenario: A sender's error text is not stored by default

- **WHEN** the host's sender fails with an error whose text contains the recipient's email address, and a dispatch pass records the outcome
- **THEN** the stored reason is the system's own classification, contains no part of the sender's error text, and the unmodified error still reaches the host's error handler

#### Scenario: A host records its own detail

- **WHEN** a host supplies a rule that maps a failure to text it considers safe to store, and a send fails
- **THEN** the stored reason is the text that rule returned

#### Scenario: Recorded detail is bounded

- **WHEN** a host's rule returns text longer than the documented maximum
- **THEN** the stored reason is truncated to that maximum and the delivery is otherwise recorded normally

#### Scenario: Skip reasons are unaffected

- **WHEN** a notification is skipped because its recipient has no address, or because the selection rejected it
- **THEN** the stored reason is the system's existing skip reason, unchanged
