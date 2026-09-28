## MODIFIED Requirements

### Requirement: Email delivery state is durable and identical on every store

The system SHALL record email delivery state durably, so that a restart or redeploy neither resends a sent notification nor loses track of an unsent one. The claiming, recording and clean-up of email delivery state SHALL behave identically on every supported store, asserted by one shared suite rather than per-store tests. A host that does not use email SHALL NOT need the storage email delivery requires.

This SHALL hold for inputs the dispatcher never sends, on every store:

- a claim whose limit is zero or less SHALL claim nothing and take no lease;
- a purge whose limit is zero or less SHALL delete nothing;
- a record SHALL change only deliveries a claim holds under the record's owner. A released delivery is held by no owner, the empty owner included, so no record changes it;
- a claim made under the empty owner SHALL be held by it, like any other owner.

#### Scenario: A redeploy does not resend

- **WHEN** a dispatcher sends alice's message and records it, and the application is redeployed
- **THEN** no later pass sends that message again

#### Scenario: Email storage is optional

- **WHEN** a host uses notifications without email and has not applied the email delivery schema
- **THEN** notifications, retention and realtime work, and schema verification for notifications succeeds

#### Scenario: A claim with no positive limit claims nothing

- **WHEN** a notification is due for email, and a claim is made with a limit of zero, then with a limit of minus one
- **THEN** each claim returns nothing, and a later claim with a positive limit still receives the notification

#### Scenario: A purge with no positive limit deletes nothing

- **WHEN** a delivery record is left orphaned by retention, and a purge runs with a limit of zero
- **THEN** it deletes nothing, and a later purge with a positive limit deletes the record

#### Scenario: A record under the empty owner does not change a released delivery

- **WHEN** a delivery is claimed and recorded as sent, and a record under the empty owner then names it with another status
- **THEN** the record changes nothing, and the delivery is never claimed again

#### Scenario: A claim under the empty owner can be recorded by it

- **WHEN** a notification is claimed under the empty owner, and a record under the empty owner marks it sent
- **THEN** the record changes it, and it is never claimed again
