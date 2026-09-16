## Context

See `proposal.md` — Why. Every defect here is reproducible against the code as it stands: a draft with a megabyte title validates clean (`notification.go:159-186` checks only `json.Valid` on the payload and nothing at all on `Title` or `Links`), a `ListQuery` carrying 100,000 kinds and 100,000 states validates clean (`store.go:233-257` checks each state's validity and the page size, never a count), and a listing whose query string Go refuses to parse is served with every filter dropped (`http.go:180` takes `r.URL.Query()`, which returns an empty set on a parse error it does not surface).

The constraints that shape the approach:

- **`Draft.Validate` is an exported method with no access to configuration** (`notification.go:137`). Every limit the library already enforces is a package constant (`MaxKindBytes`, `MaxIdentifierBytes`), so there is no existing path by which a host-configured limit reaches validation.
- **The store contract says requests arrive already validated** (`store.go:27`), so a bound enforced in the core is a bound every store may rely on.
- **A keyset-paged listing cannot split its filter across statements.** `MarkRead` chunks its `IN` list at 100 (`sqlstore/read.go:114,130`) because each chunk is an independent update; `List` (`sqlstore/read.go:55-57`) computes one ordered page, so the same treatment is not available to it.
- **The project already has numbers for this class of decision**: `DefaultListLimit = 50` / `MaxListLimit = 500` (`store.go:207-211`), `DefaultPruneBatch = 1000` ("keeps each transaction short", `pruner.go:21-23`), `maxBodyBytes = 1 << 16` (`http.go:19-21`), and `chunkSize = 100` ("caps the values one IN list binds", `sqlstore/sql.go:18-20`).
- Nothing is tagged (`docs/releasing.md`), so changing what validation accepts is free today and a breaking change tomorrow.

## Goals / Non-Goals

**Goals:**

- No caller-supplied input reaches a store or a response buffer without a documented bound.
- The comment at `notification.go:37-39` becomes true for every field it claims to cover.
- A `javascript:` href is refused by default, and permitted only by a host that says so.
- Every bound is replaceable, and a meaningless bound fails at construction.

**Non-Goals:**

- Interpreting what a link means, where it points, or whether its target exists.
- Bounding `MarkRead`'s identifier list or a close's successor count — see the proposal's scope note.
- Changing `MaxListLimit`, `DefaultListLimit` or `maxBodyBytes`, which are already bounds and already documented.
- Response-size accounting (a running byte budget across a page). The page's size follows from the per-notification bound and the page bound; a third mechanism would be a third thing to explain.
- The other audit findings, each of which is its own change.

## Decisions

### D1. The limits, and where each number comes from

| Limit | Default | Where the number comes from |
| --- | --- | --- |
| Title | 1 KiB | A title is documented as "a line a client can show" (`notification.go:71`). A kilobyte is far more than a line and still negligible next to a page. |
| Data payload | 64 KiB | The same number the contract already uses for the largest body it will read, `maxBodyBytes = 1 << 16`. One notification's payload should not outweigh an entire request. |
| Links per draft | 16 | A notification carrying more relations than this is a document. Generous against the one documented example (`"task"`). |
| Link relation name | `MaxKindBytes` (100) | A relation name is a publisher's classification, exactly like a kind; reusing the constant keeps one rule rather than two. |
| Link href | 2 KiB | The length at which URLs stop surviving intermediaries in practice. A link nobody can follow is not worth storing. |
| Drafts per publish | 1000 | `DefaultPruneBatch`'s reasoning applies unchanged: the drafts of one subject are one transaction, and this is the project's existing number for "as much as one transaction should carry". |
| Values per list filter | 100 | `chunkSize`, the number of values the SQL store is built to bind in one `IN` list. See D5. |

**Default:** the table. **Override:** each is a named exported constant, and a `Limits` value carries the set a service is configured with (D3).

These numbers are opinionated, not derived: no measurement says 64 KiB is right. They are chosen to be generous for the documented purpose of each field and small enough that the product of page size and payload stays bounded (see Risks).

### D2. An over-limit draft is refused, never truncated

An over-limit draft is a `ValidationError` naming the field, exactly like an over-long kind today. Truncation was considered and rejected outright: the library's central promise is that content is returned exactly as published (`notification-inbox`, "Opaque content is returned unchanged"), and silently storing a shortened payload breaks that promise precisely when a publisher is least able to notice. Rule 4 — a limit is stated, never silently relaxed — points the same way.

**Default:** refuse. **Override:** none; the override is the limit itself, which the host raises.

### D3. Limits are a value on the service, and `Draft.Validate` keeps working

`Draft.Validate()` stays, and validates against the package defaults. A second form takes a `Limits` value, and `Service.Publish` calls that with the service's configured limits. `Limits` is plain data: a struct of the numbers in D1 plus the link-scheme policy, with an unset field meaning the default.

*Alternatives considered:*

- **One functional option per limit** (`WithMaxTitleBytes`, `WithMaxDataBytes`, …). Rejected: seven new options on a constructor that has four, each needing its own godoc, validation and test, to configure one coherent thing. The pruner and the dispatcher earn their option lists because their settings are independent; these are one policy.
- **Package-level variables** a host mutates at init. Rejected: process-global, racy, and it makes two services in one process impossible to configure differently — which the planned multi-tenant service needs.
- **Leave `Draft.Validate()` as the only entry point and make limits constants.** Rejected: rule 2, no override without forking.

Contradictory or non-positive limits are a `ConfigurationError` from `New`, in the same style as the dispatcher's and pruner's `validate` (`email_dispatcher.go:287-324`, `pruner.go:176-199`) — rule 6.

**Default:** the constants in D1 when no `Limits` is supplied. **Override:** one option carrying a `Limits` value.

### D4. Link hrefs are checked for form, and that is not interpretation

Rule 5 says the engine does not interpret what the consumer owns. This check does not: it never asks what a link means, where it resolves, or whether the target exists. It refuses a *shape* — a scheme whose only effect in a browser is to execute the rest of the href — in the same way the library already refuses a kind of the wrong length without asking what the kind means.

The default is an allow-list of `http`, `https` and relative references, because rule 1 makes the safe option the default when safe and convenient disagree. A deny-list was considered and rejected: `javascript`, `data` and `vbscript` are the ones anybody remembers, and the next one is not.

The escape hatches mirror what the codebase already does for the same shape of decision — `AllowAll` for subscriptions (`authorize.go:39`) and `WithAnyOrigin` for WebSocket origins (`websocket/handler.go:106`): a host names additional schemes, or disables the check through an option whose name says what it does. Doing both is a wiring contradiction and fails at construction.

**Default:** `http`, `https`, relative. **Override:** name the permitted schemes, or opt out of the check by name.

### D5. The filter bound replaces chunking, because chunking is not available here

The audit observed that `List` binds `q.Kinds` into one `IN` list while `MarkRead` chunks the same shape. The asymmetry is not an oversight to correct by adding chunking: `MarkRead`'s chunks are independent updates whose results add up, whereas `List` computes a single ordered page with a keyset cursor. Splitting its filter across statements would produce several partial pages that would then have to be merged and re-sorted in memory, which is exactly what keyset paging exists to avoid — and the cursor's fingerprint (`store.go:341`) binds a cursor to the filter set that produced it, so a split would have to be invisible to it.

Bounding the filter at `chunkSize` instead keeps the `IN` list inside the size the store is built for, with no change to `sqlstore` at all.

**Default:** 100 values per filter. **Override:** part of `Limits`. A host that raises it is raising the size of one `IN` list, and the godoc says so.

### D6. The handler parses the query itself

`list` parses `r.URL.RawQuery` and answers a validation error when parsing fails, instead of taking `r.URL.Query()`, which discards both the values and the error. The other endpoints read at most one parameter and fail closed when parsing fails — `stream` falls back to the acting user, `markRead` takes its identifier from the path — so `list` is the one that currently fails open.

To be precise about what that failure is: the listing is still scoped to the acting user, so this is not a cross-recipient leak. It returns *more of the caller's own notifications than they asked for*, silently. That is a correctness defect, and it is the kind that becomes a capacity defect when a filtered query that was expected to match three rows matches thirty thousand.

**Default:** refuse. **Override:** none — serving a request the server could not parse is not a behaviour worth offering.

## Risks / Trade-offs

- **The numbers are guesses, and someone's payload is 65 KiB** → They break loudly at publish time with a validation error naming the field, not silently at read time; and the limit is one option away. Pre-tag, so no released contract changes.
- **Page size times payload size is still a large response** — 500 notifications at 64 KiB is 32 MB, and the response is marshalled whole before a byte is written (`http.go:445-455`) → Both factors are now bounded and documented, and the product is stated in `docs/notifications.md` so a host storing large payloads knows to lower one of them. Streaming the response would remove the ceiling entirely but is a different change, on a different requirement.
- **A host has been publishing `javascript:` links deliberately**, for an internal client that handles them → The named opt-out exists for exactly that, and the migration note points at it. Refusing by default is the right way round: the hosts that need it know they need it.
- **`Limits` becomes a dumping ground** for every future bound → Accepted, and preferable to the same bounds as loose options. The godoc names each field's default and the constant it replaces.
- **A limit enforced in the core is not enforced by a third-party store** → No change from today: the store contract already says requests arrive validated, and the conformance suite tests behaviour, not validation.

## Migration Plan

1. No data migration, no schema change. Existing rows are untouched, including any that exceed the new limits — the bounds apply to what is published from here on.
2. Hosts publishing within the defaults: nothing to do.
3. Hosts publishing larger content, more than 1000 drafts per call, or non-`http`/`https` links: configure `Limits` at construction. The validation error names the field and the limit it exceeded, so the first failing publish says what to set.
4. Clients listing with very large filter sets: split the query, or raise the bound.
5. Rollback is reverting the change; nothing is written that a rollback would strand.

## Open Questions

- Whether `MarkRead`'s identifier list deserves the same treatment. Deferrable: it is already chunked by the SQL store, so the exposure is a long transaction rather than an oversized statement, and adding a bound later changes neither these specs nor this task breakdown.
