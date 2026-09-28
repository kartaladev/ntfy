## Why

`.claude/rules/library-design.md` rule 6 says a wiring mistake fails at construction, before traffic. The 2026-09-28 audit found four places where a constructor accepts a configuration that cannot work. Each one then fails later, at first use or never visibly. All four were re-proved against `main` at `20967b3` on 2026-09-28, with throwaway tests that have since been deleted. Tasks 1.1, 2.1, 3.1 and 4.1 land them as the red steps.

1. **A table prefix longer than PostgreSQL can hold.** `sqlstore.New` checks the prefix's characters but not its length. PostgreSQL truncates identifiers at 63 bytes without saying so, and the longest name the store owns, `ntfy_notifications_recipient_idx`, is 32 bytes. So with a longer prefix, index names collide with each other or with the primary key. `CREATE INDEX IF NOT EXISTS` then skips them, and `Migrate` still reports success. `docs/schema.md` gets the numbers wrong too: it gives 34 bytes for that name, and 29 for the prefix limit.
2. **A base path without a leading slash.** `ntfy.WithBasePath("api")` and `websocket.WithBasePath("api")` are both accepted. The route pattern becomes `GET api/notifications`, which the standard library's mux reads as a *host* named `api`. So `GET /api/notifications` answers 404. A base path containing `{` makes `ntfy.NewHandler` **panic** instead of returning an error.
3. **Origin patterns that can never match.** `websocket.WithOriginPatterns` accepts a pattern `path.Match` cannot parse (`[app.example.com`), because the parse error is discarded when an origin is checked. It also accepts a full origin (`https://app.example.com`), which a host never equals, and an empty pattern. `WithAnyOrigin()` combined with patterns is accepted too, leaving the patterns silently dead. All of this fails closed: the browsers the host meant to permit are refused.
4. **A reconnect delay that overflows.** `ntfy.NewHub` accepts any base of at least a millisecond. `Hub.ReconnectDelay` returns the base plus a jitter below the base, and that sum wraps negative once the base passes half of `time.Duration`'s range.

**Proved by:**

- `TestAuditPostgresPrefix`, adapted from `TestAuditPostgres/a_table_prefix_New_accepts_yields_a_working_schema` (PostgreSQL 16 in a container). It checks prefixes of 44, 32 and 31 bytes:

  ```
  --- FAIL: TestAuditPostgresPrefix/44
      VerifySchema after a successful Migrate: sqlkit: the postgres schema does not match what is expected:
        ppp…p_ntfy_notifications: index ppp…p_ntfy_notifications_source_key is missing; … (six indexes)
      Error: sqlkit: pq: there is no unique or exclusion constraint matching the ON CONFLICT specification (42P10)
      Messages: a store New accepted and Migrate reported ready cannot publish
      Error: sqlkit: apply schema on postgres: sqlkit: pq: relation "ppp…p_ntfy_email_deliveri" already exists (42P07)
      Messages: MigrateEmail
  --- PASS: TestAuditPostgresPrefix/32
      VerifySchema after a successful Migrate: … index ppp…p_ntfy_notifications_recipient_idx is missing
  --- PASS: TestAuditPostgresPrefix/31    (VerifySchema and VerifyEmailSchema: <nil>)
  ```

  At 32 bytes the store publishes, but it runs without its listing index, and only `VerifySchema` notices. At 31 bytes everything holds.
- `TestAuditHTTPBasePathWithoutLeadingSlash` and `TestAuditHTTPBasePathInvalidPattern` (package `ntfy_test`):

  ```
  NewHandler accepted base path "api"; GET /api/notifications answered 404; route pattern "api/notifications"
  NewHandler panicked on base path "/a{": parsing "GET /a{/notifications": at offset 5: bad wildcard segment (must start with '{')
  ```
- `TestAuditWSBasePathWithoutLeadingSlash`, `TestAuditOriginPatternsAreValidatedAtConstruction` and `TestAuditAnyOriginWithPatterns` (package `websocket_test`):

  ```
  Error: An error is expected but got nil.   Messages: accepted; Pattern()="GET api/notifications/socket"
  --- FAIL: TestAuditOriginPatternsAreValidatedAtConstruction/[app.example.com
  --- FAIL: TestAuditOriginPatternsAreValidatedAtConstruction/https://app.example.com
  --- FAIL: TestAuditOriginPatternsAreValidatedAtConstruction/#00      (the empty pattern)
      Error: An error is expected but got nil.  expected: *ntfy.ConfigurationError
  --- FAIL: TestAuditAnyOriginWithPatterns
      Messages: WithAnyOrigin + WithOriginPatterns accepted; the patterns are dead
  ```
- `TestAuditReconnectDelayNeverOverflows` (package `ntfy_test`), with a base of three quarters of the range:

  ```
  Error: "-1486405h37m13.681891137s" is not greater than or equal to "1921535h50m27.641081853s"
  Messages: a delay is never below its base
  ```

The same audit also found an inconsistency, which is a matter of design rather than a defect. The 2026-09-28 probe `TestAuditNilOptionsAreIgnored` shows that each of these is accepted, and silently keeps the default:

- `ntfy.WithClock(nil)`, `WithIDGenerator(nil)`, `WithBroadcaster(nil)`, `WithSignalErrorHandler(nil)` and `WithPruneErrorHandler(nil)`;
- `redis.WithDecodeErrorHandler(nil)` and `nats.WithDecodeErrorHandler(nil)`.

On the other hand, `WithEmailErrorHandler(nil)`, `WithEmailFilter(nil)`, `WithEmailFailureDetail(nil)` and both `WithSubscriptionAuthorizer(nil)` are configuration errors. All of these are documented as they behave, so nothing here is claimed as broken. This change makes them consistent (`design.md` D5).

## What Changes

- `sqlstore.New` refuses a table prefix longer than a new constant, `sqlstore.MaxTablePrefixBytes` (31), with a `ConfigurationError`. A test holds the constant to the longest name in every published DDL document, so a longer index name added later fails the build. `docs/schema.md` corrects its numbers to 32 and 31.
- A base path must begin with `/`, and must not contain `{` or `}`. `ntfy.NewHandler` and `websocket.NewHandler` refuse any other with a `ConfigurationError`, and never panic. A new exported `ntfy.CleanBasePath` holds that rule, so both transports apply the same one. **BREAKING (pre-tag):**
  - `WithBasePath("/")` now serves the contract at the root. Until now it silently kept `/v1`.
  - `WithBasePath("")` is now a configuration error. Until now it kept the default.
- `websocket.NewHandler` refuses an origin pattern that is empty, that `path.Match` cannot parse, or that contains `/`, so cannot be a host. It also refuses `WithAnyOrigin()` combined with `WithOriginPatterns(...)`.
- `ntfy.NewHub` refuses a reconnect delay above a new constant, `ntfy.MaxReconnectDelay` (half of `time.Duration`'s range). That is the largest base whose jittered value, below twice the base, still fits.
- **BREAKING (pre-tag):** every option that takes a function or an interface refuses nil with a `ConfigurationError`. This covers the five core options and the two `WithDecodeErrorHandler` options listed above. A nil *option value* is still skipped. The library has no tag, so this is recorded as a compatibility decision (rule 7), not versioned.
- Godoc for every option touched names its default and its bound. `docs/notifications.md`, `docs/realtime-operations.md` and `docs/schema.md` state the new lines.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `notification-http-api`: the mounting requirement states what a base path may be, that `/` means the root, and that anything else is refused at construction.
- `notification-realtime`: three requirements change.
  - The reconnect delay has a documented ceiling.
  - Origin patterns must be able to match a host, and cannot be combined with the any-origin opt-out.
  - The WebSocket wiring list gains the base path and the origin rules.
- `ntfy-modules`: two changes.
  - The table prefix has a documented byte limit, enforced by the store's constructor.
  - A new requirement: an option given nothing (nil) is a configuration error in every module.

## Impact

- **Core module `ntfy`:**
  - `hub.go`: `MaxReconnectDelay`, and the check in `NewHub`.
  - `http.go`: `CleanBasePath`, and `NewHandler` validating through it.
  - `service.go`: the four options, and `New` checking what they set.
  - `pruner.go`: `WithPruneErrorHandler` and `pruneConfig.validate`.

  All of this stays standard-library only.
- **`ntfy/websocket`:** `handler.go`, covering the base path through `ntfy.CleanBasePath`, the origin pattern checks, and the any-origin conflict.
- **`ntfy/sqlstore`:** `store.go` gets `MaxTablePrefixBytes` and the length check. The tests are a unit guard plus a PostgreSQL and MySQL integration test.
- **`ntfy/redis`, `ntfy/nats`:** `broadcaster.go`, where a nil decode error handler is refused.
- **Docs:** `docs/schema.md`, `docs/notifications.md`, `docs/realtime-operations.md`.
- **Tests changed because behaviour changed:**
  - `service_test.go`, the case "nil options leave the defaults in place";
  - `redis/broadcaster_test.go` and `nats/broadcaster_test.go`, where `WithDecodeErrorHandler(nil)` is passed as filler in a "replace the defaults" case.
- **No schema, wire-format or dependency change.** No host running a working configuration sees a difference, except these two, which change deliberately: one that passed `WithBasePath("/")` or `WithBasePath("")`, and one that passed nil to an option.
