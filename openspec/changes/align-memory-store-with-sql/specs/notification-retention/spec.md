## ADDED Requirements

### Requirement: Retention cutoffs are compared at microsecond precision

The system SHALL compare the age cutoff (the pass's instant minus the age bound) and the watermark cutoff (the pass's instant minus the watermark retention) at microsecond precision, the precision every store keeps its instants at. A cutoff finer than a microsecond SHALL be truncated to the microsecond before it is compared, on every store, so that a notification or close record changed inside the cutoff's own microsecond is kept on every store alike.

#### Scenario: A notification inactive in the age cutoff's microsecond is kept

- **WHEN** a notification became inactive at a whole second, and a pass runs one hour later with an age bound of one hour less 500 nanoseconds
- **THEN** the pass deletes nothing for age, on every store

#### Scenario: A close record changed in the watermark cutoff's microsecond is kept

- **WHEN** an empty subject's close record last changed at a whole second, and a pass runs 24 hours and 500 nanoseconds later with a watermark retention of 24 hours
- **THEN** the pass deletes no close record, on every store

## MODIFIED Requirements

### Requirement: Close records outlive late redelivery, then expire

The system SHALL keep the record of the version at which a subject was closed at least as long as a documented watermark retention, **7 days** by default, and the host SHALL be able to change it. Once a subject has no notifications left and its close record is older than that retention, a pruning pass SHALL delete the record. The system SHALL document that a source redelivered after its subject's close record expired can create notifications again.

A subject's close record SHALL begin with the first publish or close on that subject, on every store. A publish that carries at least one notification SHALL begin it whether or not it creates any: every insertion may be suppressed, coalesced or a duplicate. A publish carrying no notification SHALL NOT begin one. A pass SHALL therefore report the same number of expired close records on every store.

#### Scenario: A recent close record is kept

- **WHEN** a subject's notifications were all deleted and its close record is 2 days old
- **THEN** a pruning pass keeps the close record

#### Scenario: An expired close record of an empty subject is deleted

- **WHEN** a subject has no notifications and its close record is 8 days old, with the default watermark retention
- **THEN** a pruning pass deletes the close record

#### Scenario: A publish that creates nothing still begins its subject's close record

- **WHEN** a source is published for alice on one subject, then published again for alice on a fresh subject, where it is a duplicate, and a pass runs 30 days later with a watermark retention of one day
- **THEN** the pass reports one expired close record deleted, for the fresh subject, on every store
