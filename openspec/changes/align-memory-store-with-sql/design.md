## Context

See `proposal.md` — Why. Where each divergence lives on `main` at `20967b3`:

| Divergence | Memory store | SQL store |
| --- | --- | --- |
| Reused identifier | `memory.go` `put` removes whatever is stored under `n.ID`, then stores the new notification. `Close` closes, then inserts successors with no way to fail. | `sqlstore/insert.go` `writeNotifications` inserts with `ignoreSuffix()` (`sqlstore/sql.go:71`). PostgreSQL and SQLite target `ON CONFLICT (source_id, recipient) DO NOTHING`, so a primary-key collision is an error. MySQL's `ON DUPLICATE KEY UPDATE id = id` absorbs **every** unique key, the primary key included. The read-back `SELECT id ... WHERE id IN (...)` then finds the old row and reports it as created. |
| Close record on an insert that creates nothing | `insert` calls `ensureWatermark` only after a `put`. | `Insert` calls `ensureWatermark(subject, '*', insertions[0].CreatedAt)` first, because that row is the subject's lock. |
| Retention cutoffs | `Prune` compares `req.Now.Add(-req.MaxAge)` and `req.Now.Add(-req.WatermarkRetention)` as they are. | `pruneAge` and `pruneWatermarks` apply `sqlkit.NormalizeTime` to both cutoffs. |
| Record under the empty owner | `RecordEmails` matches `delivery.owner != record.Owner`. A released delivery has `owner == ""`, so an empty owner matches it. | `WHERE owner = ?`. A released row's owner is NULL, which equals nothing. |
| Claim or purge with limit ≤ 0 | `ClaimEmails` and `PurgeEmailRecords` apply the limit only when it is positive, so ≤ 0 means "everything". | Both return at once when the limit is ≤ 0. |

The dispatcher never reaches the email edges. `WithEmailClaimLimit` must be positive and `WithEmailOwner` must not be empty, both checked at construction. The pruner normalises `Now`, but not the bound it subtracts. The service stamps identifiers from the host's `IDGenerator`, whose contract (`id.go`) already says "unique among every notification". A host reaches all of these by driving a store directly, or by supplying a generator that breaks that contract.

## Goals / Non-Goals

**Goals:**
- One behaviour on every store for each divergence, asserted in `ntfytest` so that a host's store is held to it.
- No option and no change for a host using the service with a conforming generator.

**Non-Goals:**
- A sentinel error for a reused identifier (D1).
- Detecting a repeating generator at construction. Minting to probe would spend a value of a sequence-backed generator, which is `design.md` D2a of `compare-mysql-identifiers-by-bytes`.
- Any other audit finding. The dispatcher findings in the same audit file (cancelled pass, in-doubt attempt limit, partial lease loss, same-owner concurrent claims) are separate changes.

## Decisions

### D1. A reused identifier fails the write, on every store

The behaviour is PostgreSQL's and SQLite's: the write fails and changes nothing. That is the only choice that keeps another recipient's notification, and the only one the SQL schema's primary key allows.

- **Memory store.** `insert` checks each notification it is about to write against `s.notifications`. On a hit it removes what this call has already written and returns an error. That undo is exact, because an insert only ever adds: a notification it wrote carries index entries nothing else shares, since its identifier was free and its source and recipient were not yet taken. A repeat within one call is found the same way, since the first copy is stored by then. `put` loses its replace branch and requires a free identifier.
- **Memory close.** `Close` records the close records it will touch and the notifications it will close before it changes anything. If inserting the successors fails, it puts them back. The alternative, working out in advance which successors would be written, would duplicate suppression, coalescing and idempotency in a second place.
- **SQL store.** The read-back in `writeNotifications` selects `id, source_id, recipient`, not `id` alone. For each notification written:
  - no row under its identifier means a concurrent publish took its source and recipient. It is a duplicate, as today.
  - a row with its own source and recipient means it was written.
  - a row with any other source and recipient means the identifier belongs to another notification. The call fails, and `s.do` rolls the transaction back.

  Only MySQL can reach the third branch. PostgreSQL and SQLite fail at the `INSERT` first, with their driver's unique-key error.
- **The error is not a sentinel.** PostgreSQL and SQLite return their drivers' errors, which differ by dialect and driver. Mapping each into one sentinel would need either a pre-check query on every insert, or driver-specific error inspection in three places. The spec requires failure and no change, not a type. A sentinel can be added later without breaking anyone.

*Alternatives considered:*
- **Keep replacing on the memory store, and document it.** Rejected: it deletes another recipient's notification, and breaks "Every store behaves identically".
- **Replace `ON DUPLICATE KEY UPDATE` on MySQL with `INSERT IGNORE`.** Rejected: it also swallows unrelated errors, which is why `ignoreSuffix` avoids it today. It would also still skip the colliding row silently.
- **A pre-check `SELECT id` on every insert, on every dialect.** Rejected: one more round trip on the publish path, to buy a uniform error type the spec does not ask for.

**Default:** a reused identifier fails the write. **Override:** none. Stored notifications are never overwritten (`library-design.md` rule 4: the line is stated, not relaxed). The consumer controls identifiers through `WithIDGenerator`, whose contract is uniqueness. The default UUIDv7 generator keeps that contract.

### D2. Every non-empty insert begins the subject's close record

The memory store follows the SQL store. `Insert` returns at once for an empty call, as SQL does. Otherwise, after `insert` succeeds, it calls `ensureWatermark(subject, '*', normalizeTime(insertions[0].Notification.CreatedAt))`, whether or not anything was created. `insert` itself no longer touches close records. Its other caller, `Close`, already ensures the record at the close's instant before it inserts successors.

A failed insert leaves no close record on either store: SQL rolls it back, and memory creates it only after success.

*Alternative considered:* make the SQL store skip the record when nothing is created. Rejected: the row is taken first so that every write on the subject serialises on it. Taking it conditionally needs another lock.

**Default / Override:** internal bookkeeping with no option. What a host sees is `PruneResult.WatermarksDeleted`, which now agrees across stores.

### D3. Memory retention cutoffs are truncated to microseconds

`Prune` applies `normalizeTime` to both cutoffs, as `sqlkit.NormalizeTime` does in `sqlstore/prune.go`. `normalizeTime` is already the core's statement of the precision every store keeps (`clock.go`). This is the only change; `pruneWatermarks` keeps its signature.

**Default / Override:** none. Precision is a guarantee, not a policy.

### D4. The `EmailStore` edges follow SQL

- **A record changes only a held delivery.** The memory store's `RecordEmails` requires `delivery.leaseUntil != nil`, as well as a matching owner. On both stores a claim sets owner and lease together; every status but SENDING clears both; SENDING keeps both. So "has a lease" on memory is exactly "owner is not NULL" on SQL. A claim under the empty owner still holds its delivery, and a record under the empty owner still changes it. A guard case keeps that true.
- **A limit of zero or less does nothing.** `ClaimEmails` returns no candidates and takes no lease. `PurgeEmailRecords` deletes nothing.
  - This is the safe reading. An unbounded claim would lease a whole backlog to one owner. An unbounded purge would hold the memory store's lock over every delivery.
  - It is also what `EmailClaim.Limit` ("caps how many notifications one claim returns") and the dispatcher's own refusal of a non-positive limit already imply.

*Alternatives considered:*
- **Refuse an empty owner or a non-positive limit with an error.** Rejected here: that would change the SQL store too, and the contract as well. The dispatcher already refuses both at construction, where `library-design.md` rule 6 puts wiring mistakes.
- **Treat ≤ 0 as "no limit".** Rejected: unsafe by default (rule 1), and SQL would need an unbounded query.

**Default:** as above. **Override:** none at the store. The dispatcher's `WithEmailClaimLimit` and `WithEmailOwner` remain the consumer's controls, and are unchanged.

### D5. The cases live in two new conformance groups

- `ntfytest/edges.go` holds `runEdges(t, factory)`, registered in `Run` as `t.Run("edges", ...)`. It has seven cases:
  - three for a reused identifier: across calls, within one call, and by a close successor;
  - a guard for an insertion that writes nothing;
  - the close record begun by an insert that creates nothing;
  - the two microsecond cutoffs.
- `ntfytest/email_edges.go` holds `runEmailEdges(t, factory)`, registered in `RunEmail` as `t.Run("edges", ...)`. It has five cases:
  - a claim with limit 0, and with limit −1;
  - a purge with limit 0;
  - a record under the empty owner on a released delivery;
  - the guard for a claim under the empty owner.

Each group is one table in the `table-test` form: `name` and an `assert func(t *testing.T, e *env)` closure (`*emailEnv` for email). There is no `ctx` field, because nothing here depends on cancellation. Each file says so in a one-line comment.

New files keep the edits to the shared `suite.go` and `email.go` to one registration line each. Those are the usual conflict points, per `development-workflow.md`.

The audit tests in `sqlstore` compared memory with SQL directly. They are not landed. The conformance cases state each expected value outright, so that every store is checked against the spec, not against SQLite.

`memory_index_test.go`'s row "after an identifier is reused for another recipient" asserted the replacement. It becomes "after an insert reusing an identifier is refused", and asserts the index is unchanged.

## Risks / Trade-offs

- **[A host's generator repeats identifiers, and its publishes now fail where memory overwrote]** → That is the intended change, and memory was losing data. The `IDGenerator` godoc says a repeat fails the write that received it.
- **[The MySQL read-back reads two more columns]** → The same statement, the same rows and the same index (primary key), with wider rows. No performance claim is made, so `performance-benchmark.md` requires no benchmark. **Unverified:** that the difference is measurable at all.
- **[The memory close's undo misses some state]** → A close changes only close records and the closed notifications' state fields; indexes are keyed by recipient, subject and source, which a close does not change. The conformance case checks all three observable effects: the offers are still open, the other notification is kept, and a later publish is not suppressed.
- **[A concurrent MySQL publish takes the same source, recipient and identifier]** → The read-back finds a row with its own source and recipient, and reports it as created. That is the same as today, and needs a generator repeating an identifier concurrently for one source.

## Migration Plan

Nothing to migrate: no schema or data changes.

- A host on the service changes nothing.
- A host with its own store runs `ntfytest.Run` and `ntfytest.RunEmail` after upgrading, and fixes any `edges` case that fails.

Rollback is a plain revert.
