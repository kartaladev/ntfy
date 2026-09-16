## Why

`Draft.Validate` bounds a draft's identifiers and kind, and the comment above those constants says the limits "are the widths of the SQL store's columns, applied to every store so that a draft accepted in memory is accepted everywhere". For `Title`, `Links` and `Data` that sentence is untrue: the columns are `text`/`LONGTEXT`, nothing checks a length, and a 100 MB payload is accepted by every store. A default page of 50 such notifications is marshalled whole into memory before a byte is written. The same gap runs through the read path — `ListQuery.Validate` checks that each state is *valid* but never how *many* were given, so a query carrying 100,000 kinds validates clean and reaches the SQL store as one `IN` list.

Two of these are worse than unbounded growth. A `javascript:` href in `Links` is stored and returned to a client that renders it, so the library hands a host an XSS vector while promising only to store what it was given. And a request with more query parameters than Go's standard library will parse makes `r.URL.Query()` return *nothing*, so the listing is served with **every filter silently dropped** instead of being refused — the caller still sees only their own notifications, but not the ones they asked for.

Today a publisher is host code, which makes this a gap rather than a live hole. The planned standalone service accepts publishes over an API, where it is neither. Bounding inputs before the first tag costs nothing; afterwards every limit is a breaking change.

## What Changes

- **A draft's content is bounded.** `Title`, each link's relation name and href, the number of links, and `Data` each get a documented default limit, and an over-limit draft is a `ValidationError` naming the field — never truncated. The comment that already claims these limits exist becomes true.
- **Link hrefs are checked for form, not meaning.** By default a link must be `http`, `https` or a relative reference. A host adds schemes it needs (`mailto`, `tel`, its own app scheme), or opts out of the check entirely through an explicitly named option, so that accepting `javascript:` is a decision rather than the default.
- **One publish is bounded.** `Publish` refuses more than a documented number of drafts, because every draft of one subject is written in a single transaction.
- **A list query's filters are bounded.** `Kinds` and `States` each get a maximum count, which also puts the `IN` list back inside the value count one statement is meant to bind.
- **A query that cannot be parsed is refused, not served unfiltered.** The listing endpoint parses the raw query itself and answers a validation error when the standard library rejects it.
- **Every limit is a named constant with an option that replaces it**, following `DefaultListLimit`/`MaxListLimit` and the pruner's and dispatcher's precedent. A contradictory or non-positive limit is a `ConfigurationError` from the constructor.
- **BREAKING** (pre-tag, core): a host publishing oversized titles or payloads, more than the draft cap in one call, `javascript:` links, or listing with enormous filter sets now receives a validation error where it previously succeeded. Nothing is tagged, so no released contract changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-inbox`: the draft-validation rules gain content limits and the link-scheme rule; the listing requirement gains a bound on how many filter values a query may carry, and a publish gains a bound on how many drafts it may carry.
- `notification-http-api`: a request whose query the server cannot parse is refused as a validation error, rather than served with its filters dropped.

## Impact

- **Code:** `notification.go` — `Draft.Validate` and `validateContent` gain content limits, a `Limits` value carries them, and the constant block's comment is corrected. `store.go` — `ListQuery.Validate` gains filter-count limits. `service.go` — `New` accepts and validates the limits, and `Publish` applies them and caps the draft count. `http.go` — `list` parses `r.URL.RawQuery` itself instead of taking whatever `r.URL.Query()` returns.
- **APIs:** new exported constants and a `Limits` value with one option; `Draft.Validate` keeps its signature and validates against the defaults, with a second form for the configured limits. No existing signature changes.
- **Stores:** none. A store still receives an already-validated request, and `sqlstore` needs no chunking change — a keyset-paged listing cannot split its filter across statements, which is why the bound is a cap rather than chunking (see `design.md` D5).
- **Tests:** each defect as a failing test first (an oversized draft accepted; a `javascript:` link stored and returned; a 100,000-value filter validating clean; a listing served unfiltered when the query has too many parameters), then the limits' own cases and one consumer override per limit.
- **Docs:** `docs/notifications.md` — the defaults-and-overrides table gains the content limits, the link-scheme policy and the draft cap; the stated-limits section gains the product of page size and payload size.
- **Not in scope:** the number of identifiers one `MarkRead` may carry (unbounded, but already chunked by the SQL store and far lower risk); the number of successors one close may create (derived from what the close matched, not from caller input, so bounding it needs batching inside the close transaction); and the other findings from the same audit, each of which is its own change.
