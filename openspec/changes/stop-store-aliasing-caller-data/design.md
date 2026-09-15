## Context

See `proposal.md` — Why. The change spans three modules (`ntfy`, `ntfy/sqlstore`, `ntfy/ntfytest`), which is what makes it worth a design note: the fix in this repository is two clones, but the invariant it establishes binds every store a host may write.

Every path that carries a caller's `Links` map or `Data` slice into or out of a store, audited before writing this:

| Path | Store | Today | Evidence |
| --- | --- | --- | --- |
| `Service.Publish` → `Insertion` | — | **Safe**, clones | `service.go:143-150` |
| `CloseRequest.SuccessorInsertions` → `Insertion` | — | **Aliased**: all insertions share the caller's map and slice | `store.go:178-182` |
| `Insert` → stored state | memory | Safe, clones on put | `memory.go:105` |
| `Insert` → `InsertResult.Created` | memory | Safe, clones again | `memory.go:106` |
| `Get`, `List` | memory | Safe, clones on return | `memory.go:258`, `:297` |
| `Close` → `CloseResult.Successors` | memory | Safe, via the insert path's clones | `memory.go:240-241` |
| `ClaimEmails` → `EmailCandidate.Notification` | memory | Safe, clones | `memory_email.go:59` |
| `Insert` → accepted, then `InsertResult.Created` | sql | **Aliased**: returns the value it was given | `sqlstore/insert.go:103`, `:115` |
| `Close` → `CloseResult.Successors` | sql | **Aliased**, inherits both defects above | `sqlstore/close.go:55` |
| `Get`, `List`, `claimedEmails` | sql | Safe by construction: decoded from rows | `sqlstore/sql.go:251` |

Two notes the audit settled, which narrow the live defect:

- **sqlstore cannot be retroactively corrupted.** It serialises values into binds inside the write transaction (`sqlstore/insert.go:268`), so a later mutation by the caller cannot reach the database. Its defect is what it **returns**, not what it stores.
- **Publish callers are already protected on both stores**, because `Service.Publish` clones before the store sees anything. The live defect reaches a caller only through the close path, where nothing clones.

## Goals / Non-Goals

**Goals**

- One invariant, stated once, that a host implementing `Store` can read and satisfy.
- The conformance suite fails for any store that breaks it, including the two shipped here.
- The library never hands a store aliased values, even though a correct store would survive it.

**Non-Goals**

- Defensive copying on paths that cannot alias, such as rows decoded from SQL.
- Any change to what is stored or returned as a *value*; this is purely about identity.
- Protecting a caller from mutating a value it still shares with itself, outside a store call.
- The other audit findings, each of which is its own change.

## Decisions

### D1. Enforce at both ends, not one

`SuccessorInsertions` clones what it builds, **and** each store clones what it retains and what it returns.

Either alone would make today's code correct, and both are still wanted, for different reasons:

- Cloning only in `SuccessorInsertions` leaves the obligation implicit. A host store that keeps what it is given stays one careless caller away from the same bug, and nothing tells it so.
- Cloning only in the stores leaves an exported function (`SuccessorInsertions`) that hands out aliased values as its documented contract, which is a trap for the third-party stores it exists to serve.

*Alternative considered:* declare the caller responsible and document it, keeping stores free of copies. Rejected: it makes the cheap, correct thing (accepting values as given) the wrong thing, and no test can hold a caller to it. A store is the narrow point where the invariant can actually be checked.

**Default:** stores and library code copy what crosses the boundary. **Override:** none — see D4.

### D2. The suite proves aliasing by mutation, not by pointer identity

A conformance case mutates a map or payload it supplied or was returned, then reads back and asserts the other value is unchanged. It does not compare pointers or use `reflect`/`unsafe` to detect sharing.

*Rationale:* mutation tests the property the contract actually promises. A store that returns a shared-but-immutable value, or that copies lazily, is not broken, and a pointer-identity assertion would fail it for the wrong reason. Mutation also reads as the scenario a host would hit in production.

*Trade-off:* mutation cannot detect sharing that nothing happens to mutate. Accepted — the contract is about observable interference, not about object graphs.

### D3. Leave the read paths that are safe by construction alone

`sqlstore`'s `Get`, `List` and `claimedEmails` build notifications from database rows (`sqlstore/sql.go:251`), so each result is already a fresh value. They get conformance coverage but no added clones.

*Alternative considered:* clone uniformly everywhere, so the rule is "always clone" with no exceptions to remember. Rejected: it adds an allocation per notification on the listing path — the hottest read in an inbox — to defend against sharing that cannot occur. The suite, not a convention, is what keeps these paths honest.

### D4. The invariant binds third-party stores, with no way to opt out

A host store that passes the conformance suite satisfies this; one that does not, fails it.

Library rule 2 asks that every default be replaceable. This is not a default but a contract term, and rule 4 is the one that applies: the limit is stated rather than silently assumed. A store that shares memory with its caller cannot keep the library's existing promise that content is "stored and returned unchanged" — the value is only unchanged until someone else's mutation reaches it. There is nothing to configure here, and an opt-out would mean a store whose returned notifications may change under the caller, which no consumer could program against.

**Default:** the invariant holds. **Override:** none, for the reason above.

## Risks / Trade-offs

- **A host store already ships that breaks the invariant** → It fails the suite the first time it runs after this change, with a case naming exactly what is shared. Nothing is tagged yet, so no released contract changes.
- **The mutation-based cases could pass vacuously** if a future refactor stops them mutating what they think they mutate → Each case asserts the *unmutated* value is intact **and** that the mutation took effect on the value the caller holds, so a no-op mutation fails the case.
- **Clone cost on large payloads at close time** → Bounded by the successors a close creates, which the close already writes as rows; a clone is cheap beside the insert.
- **The invariant is stated but not exhaustively tested**, since no test enumerates every field → `Notification.Clone` is the single mechanism, and it is the place to extend when a mutable field is added. The godoc says so.

## Migration Plan

1. No data migration, no schema change, no configuration change.
2. Hosts using the shipped stores: nothing to do. No API signature changes, and no value returned by the library changes.
3. Hosts with their own `Store`: run `ntfytest.Run` after upgrading. A failure names the aliased path; the fix is to clone what the store retains and returns.
4. Rollback is reverting the change; nothing persisted depends on it.
