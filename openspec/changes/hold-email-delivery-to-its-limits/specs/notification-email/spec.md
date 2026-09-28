## MODIFIED Requirements

### Requirement: Email dispatch runs only when the host drives it

The system SHALL NOT email on its own. A dispatch pass SHALL run only when the host invokes it, whether once, from a long-running loop the host starts, or from the host's own scheduler. Stopping a loop the host started SHALL end it without leaking any background work.

A pass SHALL stop when the context the host gave it ends. A pass whose context has already ended SHALL claim nothing. Once the context ends mid-pass, the pass SHALL send nothing further and SHALL record no further send as started. It SHALL return what it did so far together with the context's error. Notifications the pass claimed but had not reached SHALL NOT count an attempt and SHALL NOT be recorded as failed, retried or in doubt: once the claim lapses, a later pass SHALL email them. The system SHALL state as a limit that a context ending between the last check and a send still reaches the sender, which receives the ended context.

#### Scenario: Constructing a dispatcher sends nothing

- **WHEN** a host constructs an email dispatcher and never invokes it
- **THEN** no email is sent, no delivery record is written, and no background work is running

#### Scenario: A loop stops when the host cancels it

- **WHEN** a host runs the dispatch loop and cancels its context
- **THEN** the loop returns, and no background work remains

#### Scenario: A pass stops when its context ends

- **WHEN** a pass has qualifying notifications for alice, bob and carol, and its context is cancelled while alice's message is being sent
- **THEN** no message is sent to bob or carol by that pass, the pass returns the context's error with alice's message counted as sent, and a later pass after the claim lapses emails bob and carol once each

#### Scenario: A pass whose context has already ended

- **WHEN** a host runs a pass with a context that has already been cancelled
- **THEN** nothing is claimed or sent, and the pass returns the context's error

### Requirement: A notification is emailed at most once by default

By default the system SHALL email each notification at most once. A send that may have happened, because the process stopped or the sender could not tell, SHALL NOT be repeated and SHALL be recorded as abandoned. The host SHALL be able to choose at-least-once delivery instead, under which an in-doubt send is repeated with the same idempotency key as the original attempt, so a sender that honours the key delivers it once. Every message SHALL carry an idempotency key that stays the same for every attempt of the same message.

Under at-least-once delivery, a repeated send SHALL cover a subset of the notifications the original attempt covered: the system SHALL NOT add a notification to a message it has already attempted, and SHALL drop any notification that stopped being ACTIVE since, as it does before a first attempt. Where every notification of an in-doubt message has stopped being ACTIVE, the system SHALL send nothing further for it.

A repeated send that fails without being rejected, or whose notifications cannot all be read again before it, SHALL stay in doubt under the same idempotency key and be repeated once its lease lapses, rather than be retried as a new message; it SHALL NOT be sent without a notification it covered that could not be read. The same SHALL hold for a send the sender reports as in doubt, and for a send in doubt whose recipient's address cannot be looked up.

The attempt limit SHALL still apply to every send in doubt. Every send attempted, and every failed address lookup while settling a send in doubt, SHALL count as an attempt. When the attempt that reaches the limit fails, is reported in doubt, or cannot look the address up, the notifications SHALL be recorded as failed and SHALL NOT be claimed again.

The system SHALL state as limits that a repeated send is rendered again at each attempt rather than replayed from a stored copy, and therefore that a message template changed between two attempts produces different content under one idempotency key; that a sender which ignores the idempotency key may deliver both; and that a repeated send which fails is attempted again after the lease rather than after the growing retry delay.

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

#### Scenario: A resend that fails keeps its key

- **WHEN** an in-doubt message is resent under at-least-once delivery, the sender fails transiently, and alice has received a newer notification meanwhile
- **THEN** a later pass sends the message again under the original idempotency key over the original notifications, and the newer notification goes in a separate message

#### Scenario: A resend whose notifications cannot be read waits whole

- **WHEN** an in-doubt message is resent under at-least-once delivery and one of its notifications cannot be read again
- **THEN** nothing is sent for it in that pass, and a later pass sends it under the original idempotency key

#### Scenario: A resend that keeps failing fails at the attempt limit

- **WHEN** an in-doubt message is resent under at-least-once delivery and the sender fails transiently on the attempt that reaches the limit
- **THEN** its notifications are recorded as failed and never claimed again

#### Scenario: A send that stays in doubt fails at the attempt limit

- **WHEN** a dispatcher configured for at-least-once delivery and an attempt limit of 2 sends alice's message, and the sender reports every attempt as in doubt
- **THEN** the message is sent twice under one idempotency key, the second attempt records its notifications as failed, and no later pass claims them

#### Scenario: A lookup that keeps failing on a send in doubt fails at the attempt limit

- **WHEN** a dispatcher configured for at-least-once delivery and an attempt limit of 3 leaves alice's message in doubt, and her address lookup fails on every later pass
- **THEN** the first failed lookup keeps the message in doubt under its key, and the second records its notifications as failed with the lookup failure as the reason

### Requirement: Concurrent dispatchers never send the same notification twice

The system SHALL let dispatchers run in several application instances at once, and SHALL ensure that only one of them sends each notification. A dispatcher that stops mid-pass SHALL NOT strand its notifications: once its claim lapses, another dispatcher SHALL take them over, subject to the delivery guarantee for any send already in doubt.

One dispatcher SHALL run one pass at a time. A pass the host starts while the same dispatcher is running one SHALL wait for it to finish, and SHALL return the context's error without claiming anything if its context ends while it waits. A host that wants passes to run in parallel SHALL use several dispatchers, each with its own owner. This SHALL hold on every supported store.

A pass that loses the claim on some of a message's notifications before sending it SHALL NOT send that message. It SHALL return the notifications it still holds to later passes as unsent, never as a send in doubt, and SHALL report the lost claim to the host. The attempt the pass counted for that message SHALL stay counted. A message already in doubt under at-least-once delivery SHALL stay in doubt under its key instead.

#### Scenario: Two instances dispatch at the same time

- **WHEN** two dispatchers run passes concurrently over the same qualifying notifications
- **THEN** each notification is sent by exactly one of them

#### Scenario: A stopped dispatcher's unsent claim is taken over

- **WHEN** a dispatcher claims notifications and stops before attempting to send them
- **THEN** after its claim lapses, another dispatcher emails them

#### Scenario: One dispatcher's overlapping passes

- **WHEN** a host starts two passes on one dispatcher at the same instant, with a clock that gives both the same reading, over the same qualifying notifications on any supported store
- **THEN** the second pass waits for the first, and each notification is emailed once

#### Scenario: A pass that lost part of its claim loses no email

- **WHEN** a pass under at-most-once delivery claims two of alice's notifications, its claim lapses while it looks her address up, and another dispatcher takes and sends the older one
- **THEN** the first pass sends nothing, the newer notification is not recorded as abandoned, and a later pass emails it
