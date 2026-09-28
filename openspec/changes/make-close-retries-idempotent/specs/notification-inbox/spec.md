## MODIFIED Requirements

### Requirement: A close can tell each recipient it closed what happened, atomically

The system SHALL let a close name a successor notification. In the same transaction as the close, the system SHALL publish the successor to each recipient whose notification that close moved to CLOSED, except recipients the close names to skip. Successors SHALL be subject to the same idempotency and watermark suppression as any publish. A close result SHALL report the successors it created.

A close that names a successor SHALL NOT close any notification published from that successor's source, whichever kinds the close names and whatever version the successor carries.

A close that is retried after an earlier attempt committed SHALL close nothing further, SHALL report no recipients and SHALL create no successors, and the successors the earlier attempt created SHALL remain ACTIVE. This SHALL hold whether the close names one kind, several kinds including the successor's kind, or every kind.

#### Scenario: Closing offers tells the other recipients

- **WHEN** a subject has "offer" notifications for alice, bob and carol, and its "offer" notifications are closed at version 5 with a "taken" successor, skipping carol
- **THEN** all three offers are CLOSED, and alice and bob each have one ACTIVE "taken" notification at version 5, and carol has none

#### Scenario: A retried close creates no further successors

- **WHEN** the same close with a successor runs twice
- **THEN** each recipient has exactly one successor notification from that close

#### Scenario: A retried close of every kind closes nothing further

- **WHEN** a subject has "offer" notifications for alice, bob and carol, and a close of every kind at version 5 with a "taken" successor at version 5, skipping carol, runs twice
- **THEN** the second run closes nothing and reports no recipients, and alice's and bob's "taken" successors from the first run are still ACTIVE

#### Scenario: A retried close naming the successor's kind closes nothing further

- **WHEN** a subject has "offer" notifications for alice, bob and carol, and a close of kinds "offer" and "taken" at version 5 with a "taken" successor at version 5, skipping carol, runs twice
- **THEN** the second run closes nothing and reports no recipients, and alice's and bob's "taken" successors from the first run are still ACTIVE

#### Scenario: A later close still closes an earlier close's successors

- **WHEN** a close at version 5 gave alice a "taken" successor, and afterwards a close of every kind at version 6 with no successor runs on the subject
- **THEN** alice's "taken" notification is CLOSED

#### Scenario: A successor below a newer watermark is suppressed

- **WHEN** a subject's "taken" notifications are closed at version 8, and afterwards an older close at version 5 closes offers with a "taken" successor at version 5
- **THEN** no "taken" notification is created, and the close result reports the successors as suppressed
