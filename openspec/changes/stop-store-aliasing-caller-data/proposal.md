## Why

`Service.Publish` clones every draft before it reaches a store, with the comment "Cloned, so that a caller mutating its links or payload after publishing cannot change what is stored" (`service.go:143-150`). The close path does not: `CloseRequest.SuccessorInsertions` copies `Links` and `Data` by reference (`store.go:178-182`), so every insertion it builds shares one map and one byte slice — with each other, and with the caller's own `req.Successor`.

Whether that becomes observable depends on the store, which is the deeper problem. `MemoryStore` clones defensively on the way in and on the way out (`memory.go:105-106`), so it is safe. `sqlstore` does not (`sqlstore/insert.go:103`, `:115`), so a close against PostgreSQL, MySQL or SQLite returns a `CloseResult.Successors` whose notifications share one map and one slice with each other **and with the caller's request** — mutate one and the others change under you.

The contract never states the invariant, so neither store is wrong by the spec and the divergence is invisible: the conformance suite checks successor counts, recipients and field values (`ntfytest/suite.go:652-740`) but never whether two returned notifications are the same object. `SuccessorInsertions` is exported and documented as something "a store calls inside its close transaction" (`store.go:155-159`), so every third-party store inherits the same trap with no way to know it exists.

## What Changes

- **A stated invariant, in both directions.** A store never retains a caller's mutable value without copying it, and never returns one it shares with its own state, with the caller's request, or with another returned notification. `Notification.Clone` (`notification.go:89-97`) already exists as the mechanism; what is missing is the contract that obliges a store to use it.
- **`SuccessorInsertions` clones**, so the insertions it builds are independent of each other and of `req.Successor` before any store sees them. This is the one line that makes today's stores correct, but it is not the fix — a store must not depend on its caller having cloned.
- **`sqlstore` clones what it returns** (`insert.go:103`, `:115`), so `InsertResult.Created` and `CloseResult.Successors` share nothing with the values handed to `Insert`.
- **The conformance suite asserts the invariant**, so every store — including a host's own — is held to it, and the memory/SQL divergence cannot reappear. This is the real deliverable.
- No API signature changes, no behaviour a correct caller can observe today beyond the aliasing itself, and nothing for a host to configure.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-inbox`: adds a requirement that stored and returned notifications share no memory with the caller or with each other.

The existing requirement "A notification belongs to exactly one recipient and is in one of three states" already says a store treats content as opaque, "storing and returning them unchanged". That is about **value fidelity** — what comes back equals what went in. This change is about **reference isolation** — that nobody can change it afterwards through a shared pointer. They are different properties with different failure modes and different tests, so this is an added requirement rather than a sentence bolted onto that one. "Every store behaves identically" is not the home either: it governs how conformance is asserted, not what is asserted, and it needs no amendment for the suite to gain cases.

## Impact

- **Code:** `store.go:178-182` (`SuccessorInsertions` clones each insertion); `sqlstore/insert.go:103`, `:115` (clone on accept and on return). `memory.go` and both stores' read paths already satisfy the invariant — see `design.md` for the path-by-path audit.
- **APIs:** no signature changes. `SuccessorInsertions` gains a documented guarantee about what it returns; the `Store` port gains a documented obligation.
- **Tests:** `ntfytest` gains aliasing cases that run against every store and dialect, starting with the one that fails today on `sqlstore` and passes on `MemoryStore`.
- **Modules:** `ntfy`, `ntfy/sqlstore`, `ntfy/ntfytest`.
- **Performance:** cloning N successors adds N map-and-slice copies per close, on a path that already writes N rows. The read paths are not touched, so listing cost is unchanged.
- **Not in scope:** the other findings from the same audit (unbounded publish and query inputs, stopped-hub streams, email dispatch robustness, the claim query's scan, memory-store read cost) are separate changes.
