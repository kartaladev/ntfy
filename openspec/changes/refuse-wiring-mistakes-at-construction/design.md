## Context

See `proposal.md` — Why. The change touches five modules: the core `ntfy`, `ntfy/websocket`, `ntfy/sqlstore`, `ntfy/redis` and `ntfy/nats`. None of it changes a schema or a wire format. Every fix moves a check from first use, or from nowhere, to the constructor.

Where each check stands today, on `main` at `20967b3`:

| Where | What it does now | Observed on 2026-09-28 |
| --- | --- | --- |
| `sqlstore/store.go:66,100` | `plainPrefix` checks characters, not length | a 44-byte prefix: `Migrate` succeeds, six indexes are skipped, `Insert` fails with `42P10`, and `MigrateEmail` fails with `42P07`. A 32-byte prefix: `recipient_idx` is silently missing. A 31-byte prefix works. |
| `sqlstore/ddl/*.sql` | the longest owned name is `ntfy_notifications_recipient_idx`, 32 bytes | `docs/schema.md:136` says 34, and advises a prefix of at most 29 |
| `http.go:63-69` | `WithBasePath` trims trailing `/`, and replaces the default unless the result is empty | `"api"` gives `GET api/notifications`, answering 404. `"/"` silently keeps `/v1`. `"/a{"` panics inside `http.ServeMux.Handle`. |
| `websocket/handler.go:76-82, 179` | the same trimming, and `pattern` is concatenated without checking | `"api"` gives `GET api/notifications/socket` |
| `websocket/handler.go:99-108, 505-507` | patterns are appended unchecked, and `path.Match`'s error is discarded when an origin is checked | `[app.example.com`, `https://app.example.com` and `""` are accepted. `WithAnyOrigin` + patterns is accepted. |
| `hub.go:116, 155, 341` | the base must be at least 1ms; `base + rand.Int64N(base)` | a base of ¾·MaxInt64 draws negative delays |
| `service.go:30-72`, `pruner.go:103-109` | the option ignores nil | accepted, and the default is kept |
| `redis/broadcaster.go:71`, `nats/broadcaster.go:78` | `onError` is a plain func field, and nil means "not set" | accepted, and the default is kept |
| `email_dispatcher.go:344-357`, `http.go:74`, `websocket/handler.go:88` | an explicit nil is detected (a pointer or a `set` flag) and refused | already a configuration error |

`path.Match(p, "")` returns `path.ErrBadPattern` for `[app.example.com`, `a\` and `[a-`. It returns nil for `*.example.com` and `app.example.com:8080`. This was checked on go1.26.8 on 2026-09-28.

## Goals / Non-Goals

**Goals**

- Every configuration the four findings describe fails in the constructor, with a `ConfigurationError` whose detail names the option and the line.
- Each line sits where the guarantee actually breaks, not at an arbitrary smaller number (rule 4).
- One rule for nil across every module.

**Non-Goals**

- **Validating a base path beyond what is proved to break.** A base path with a space, such as `/a b`, is accepted by `http.ServeMux` today, and nothing shows that it fails.
- **Detecting a typed nil** (`(*MyClock)(nil)` in an interface). Go cannot tell it from a working value without reflection. It is stated as a limit in godoc.
- **Changing `ReconnectDelay`'s distribution, or making the jitter configurable** (still a stated limit).
- **Refusing `WithOriginPatterns()` called with no patterns.** A host spreading a configured list that happens to be empty permits no extra origins, which is correct.
- **Probing a live database for its identifier limit.** The limit is a property of the published DDL and of PostgreSQL's documented `NAMEDATALEN`, not of a server.

## Decisions

### D1. A table prefix is at most `MaxTablePrefixBytes` (31), on every dialect

`sqlstore.New` refuses a prefix longer than `const MaxTablePrefixBytes = 31`. The detail reads:

> `sqlstore: a table prefix may be at most 31 bytes, so that every table and index name fits PostgreSQL's 63-byte identifiers`

The number is PostgreSQL's 63-byte identifier limit minus the longest name the store owns (`ntfy_notifications_recipient_idx`, 32 bytes). MySQL's limit is 64, so 31 fits there too. SQLite has none.

The guard is `TestTablePrefixBoundFitsEveryOwnedName`. It renders `Schema()` and `EmailSchema()` for all three dialects with a prefix of exactly `MaxTablePrefixBytes`, extracts every quoted identifier, and asserts two things:
- every identifier is at most 63 bytes;
- the longest is exactly 63.

So an index name added later that is longer than 32 bytes fails the build. So would a constant set lower than it needs to be.

| Option | Verdict |
| --- | --- |
| Keep the documented 29 | Arbitrary: 30 and 31 work, as observed. Rule 4 says offer flexibility up to the line. |
| **One bound, 31, on every dialect** | **Chosen.** A schema and its prefix can move between dialects, and the docs publish one number. SQLite hosts lose nothing they need: a 31-byte prefix is ample. |
| A bound per dialect (PostgreSQL 31, MySQL 32, SQLite none) | Three numbers to document for one byte of gain. A prefix that works on SQLite in tests would fail on PostgreSQL in production. |
| Shorten or hash long names | Changes the documented names (`ntfy-modules`), and the verifier would need the same mapping. Rejected. |

**Default:** no prefix. **Override:** `WithTablePrefix`, at most 31 bytes of `[A-Za-z0-9_]`. The limit has no override, because above it PostgreSQL truncates names and indexes are silently skipped.

### D2. A base path begins with `/` and contains no `{` or `}`; `/` is the root

The new exported function `ntfy.CleanBasePath(path string) (string, error)` holds the rule. `ntfy.NewHandler` and `websocket.NewHandler` both call it. It is exported on the same precedent as `ntfy.ParseQuery`: *a transport calls it*, so the WebSocket module cannot drift from the HTTP contract.

- It trims trailing `/`.
- `""` → `ConfigurationError`: `a base path must not be empty; omit WithBasePath to keep /v1, or pass "/" to serve at the root`.
- It must begin with `/`. Otherwise → `ConfigurationError`: `the base path "api" must begin with "/"; a path without one is read as a host by http.ServeMux`.
- It must not contain `{` or `}`. Otherwise → `ConfigurationError`: `the base path "/a{" must be a literal path; wildcards belong to the routes, which already use {id}`.
- `"/"` (or `"//"`) → `""`, so the routes are `/notifications…`.

`WithBasePath` records the raw value and a `set` flag. The constructor validates it only when the option was given, so an absent option keeps `DefaultBasePath` untouched.

*Why refuse all braces, not only malformed ones?* A well-formed wildcard such as `/{tenant}` does register. But `Routes()` documents `{id}` as the only path value, and a base named `{id}` would panic as a duplicate wildcard. A host that routes by tenant strips the prefix in its own router (`http.StripPrefix`, a Gin group). So the library's base stays literal.

*Why an error for `""`, not the default?* An explicit empty value is meaningless configuration (rule 6). The Redis `WithChannel("")` and NATS `WithSubject("")` already refuse it, and an absent option is how to keep a default. **Breaking before the first tag:** `WithBasePath("")` used to keep `/v1`, and `WithBasePath("/")` used to keep `/v1` too.

**Default:** `/v1`. **Override:** `WithBasePath` on either handler, including `/` for the root.

### D3. Origin patterns must be able to match a host, and cannot be combined with any-origin

`websocket.NewHandler` checks every pattern given to `WithOriginPatterns`:

- `""` → `ConfigurationError` (`an origin pattern must not be empty`);
- it contains `/` → `ConfigurationError`: `the origin pattern "https://app.example.com" contains "/" and can never match a host; write the host, such as "app.example.com"`;
- `path.Match(pattern, "")` returns `path.ErrBadPattern` → `ConfigurationError`: `the origin pattern "[app.example.com" is malformed: syntax error in pattern`. `ConfigurationError` carries only a detail and matches `ErrConfiguration`, so the parse error is named in the text, not wrapped.

`WithAnyOrigin()` together with at least one pattern → `ConfigurationError`: `WithAnyOrigin permits every origin, so the origin patterns would have no effect; choose one`. This follows the precedent of `WithMaxStreamsPerInstance` together with `WithoutMaxStreamsPerInstance`.

`checkOrigin` keeps discarding `path.Match`'s error, because a pattern that reaches it has already been parsed. A comment says so.

**Default:** same host only. **Override:** `WithOriginPatterns(hosts...)`, or `WithAnyOrigin()`, but not both.

### D4. `MaxReconnectDelay` is half of `time.Duration`'s range

`const MaxReconnectDelay = time.Duration(math.MaxInt64 / 2)`, about 146 years. `NewHub` refuses a base above it:

> `a hub reconnect delay must be at most MaxReconnectDelay, so that the delay and its jitter stay below twice it`

With `base ≤ MaxInt64/2`, `base + rand.Int64N(base) ≤ 2·base − 1 ≤ MaxInt64 − 2`, so `ReconnectDelay` cannot wrap and is left unchanged.

| Option | Verdict |
| --- | --- |
| **Bound at construction, at the overflow line** | **Chosen.** Rule 6, and the line is exactly where the promise "between the delay and twice it" stops being representable. |
| Saturating arithmetic in `ReconnectDelay` | Hides the mistake. Every stream would carry `MaxInt64` rather than a jittered value, breaking the spread the requirement promises. |
| A practical ceiling (for example one hour) | Arbitrary. The library does not know a host's reconnect policy (rule 4). |

**Default:** 1s. **Override:** `WithReconnectDelay`, from 1ms up to `MaxReconnectDelay`.

### D5. An option given nil is a configuration error, in every module

Decision: **refuse**, rather than making the email options also ignore nil.

- Rule 6 calls meaningless configuration a construction error. `WithClock(nil)` is meaningless: it is almost always an unset variable, and today it silently runs on the system clock.
- Five options already refuse nil (`WithEmailErrorHandler`, `WithEmailFilter`, `WithEmailFailureDetail`, and both `WithSubscriptionAuthorizer`). Seven ignore it. The newer convention, and the one the specs already state for policies, is refusal.
- The default is kept by omitting the option, which is already true of every option.

Implementation, per option type:
- **`ntfy.Option` (`func(*Service)`).** The option assigns unconditionally. `New` checks the four fields after applying options: `clock`, `ids`, `broadcaster`, `onSignalError`. The detail names the option, for example `WithClock was given nil; omit it to read the system clock`. It checks before `svc.limits.validate()`, so a nil option is reported first.
- **`PruneOption`.** `WithPruneErrorHandler` assigns unconditionally over the no-op default. `pruneConfig.validate` refuses `onError == nil`.
- **`redis` / `nats` `Option`.** `config.onError` becomes `*func(ctx context.Context, err error)`, as `emailConfig.onError` already is. `NewBroadcaster` refuses a non-nil pointer to a nil func.
- **A nil `Option` value** (`opts = append(opts, nil)`) is still skipped, in every constructor, so a host can build its list conditionally. This is documented.

**Typed nils** pass, and the godoc of each option says so. **Breaking before the first tag:** a host passing nil to one of the seven options now fails at construction. `docs/notifications.md:57` ("A nil option is ignored and keeps the default") is rewritten.

**Default:** unchanged for every option. **Override:** unchanged. Only the nil path changes, from "silently the default" to an error.

## Risks / Trade-offs

- **[A host already relies on `WithBasePath("")`, `WithBasePath("/")` or a nil option]** → The library has no tag (`git tag` is empty). The change is recorded here as a compatibility decision (rule 7), and each error detail says what to do instead.
- **[A future index name longer than 32 bytes]** → `TestTablePrefixBoundFitsEveryOwnedName` fails at once and names the identifier. The fix is to shorten the name, or to lower the constant and record that as a compatibility decision.
- **[An existing deployment runs with a prefix of 32 bytes or more]** → On PostgreSQL such a schema is already broken: `VerifySchema` reports the missing indexes. On MySQL, `Migrate` refuses any prefix over 32 bytes (names over 64). So the deployments this newly refuses are these two, and the error names the limit and why:
  - a MySQL deployment with a prefix of exactly 32 bytes;
  - a SQLite deployment with a prefix of 32 bytes or more.

  Such a host renames its tables and indexes to a shorter prefix with its database's own `ALTER ... RENAME` statements. `docs/schema.md` says so in one sentence, but gives no script: the library has never published one for prefixes. That is the price of one portable bound, and it is recorded here.
- **[`CleanBasePath` becomes public API]** → It is small, pure, and documented as the rule both transports share. The alternative, a duplicated private copy in `websocket`, is the drift this change exists to prevent.
- **[Refusing braces stops a host routing by tenant through the base path]** → The host strips the prefix in its own router. The godoc names that alternative.

## Migration Plan

No data or schema migration. A host that fails at construction after upgrading reads the detail, which names the fix:
- remove a nil option, or `WithBasePath("")`;
- use `"/"` deliberately for the root;
- shorten the table prefix to 31 bytes;
- write origin patterns as hosts;
- choose either `WithAnyOrigin` or patterns.

Rollback is reverting the commit, because nothing persistent changes.
