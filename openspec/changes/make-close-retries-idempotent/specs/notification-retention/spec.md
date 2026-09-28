## MODIFIED Requirements

### Requirement: Close records outlive late redelivery, then expire

The system SHALL keep the record of the version at which a subject was closed at least as long as a documented watermark retention, **7 days** by default, and the host SHALL be able to change it. The retention SHALL be measured from the record's last change, which is the subject's latest close, or its first publish when it was never closed. It SHALL NOT be measured from the deletion of the subject's last notification. Once a subject has no notifications left and its close record is older than that retention, a pruning pass SHALL delete the record, including in the same pass that deleted the subject's last notification. The system SHALL document that a source redelivered after its subject's close record expired can create notifications again.

#### Scenario: A recent close record is kept

- **WHEN** a subject's notifications were all deleted and its close record is 2 days old
- **THEN** a pruning pass keeps the close record

#### Scenario: An expired close record of an empty subject is deleted

- **WHEN** a subject has no notifications and its close record is 8 days old, with the default watermark retention
- **THEN** a pruning pass deletes the close record

#### Scenario: A close record can expire with its subject's last notification

- **WHEN** a subject's only notification was closed at version 5 ninety-five days ago, and a pruning pass runs with a 90-day age bound and the default watermark retention
- **THEN** the pass deletes both the notification and the close record, and a source at version 1 published afterwards on that subject is created
