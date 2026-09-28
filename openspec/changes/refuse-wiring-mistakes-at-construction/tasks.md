## 1. Bound the reconnect delay (core, `hub.go`)

- [ ] 1.1 Red: add two cases to `TestHubReconnectDelay` in `hub_test.go`.
  - "a delay past half the range is a configuration error": the base `time.Duration(math.MaxInt64/4*3)` is expected to be refused.
  - "the largest representable base never draws below itself": with the base `time.Duration(math.MaxInt64/2)`, 1000 draws are all `>= base`.

  Verify with `GOTOOLCHAIN=go1.26.8 go test -run 'TestHubReconnectDelay' -count=1 .`. The first case must fail with `Expected error with "ntfy: invalid configuration" in chain but got nil.` The second passes already, and is the guard for task 1.2.
- [ ] 1.2 Green: add `const MaxReconnectDelay = time.Duration(math.MaxInt64 / 2)` to `hub.go`. `NewHub` refuses `*cfg.reconnect > MaxReconnectDelay`. Update the godoc of `WithReconnectDelay` and `NewHub` to name the ceiling. Add a case pinning `ntfy.MaxReconnectDelay == time.Duration(math.MaxInt64/2)`, and refusing `MaxReconnectDelay+1`. Verify that `TestHubReconnectDelay` passes, and that `TestNewHub` still passes.

## 2. Validate the HTTP base path (core, `http.go`)

- [ ] 2.1 Refactor: extract today's trimming into `func CleanBasePath(path string) (string, error)`, which returns `strings.TrimRight(path, "/"), nil`, and use it in `WithBasePath`. Verify with `go test -run 'TestNewHandler' -count=1 .` that it passes unchanged.
- [ ] 2.2 Red: add `TestCleanBasePath`, a table, to `http_test.go`.
  - `/v1` gives `/v1`.
  - `/api/` gives `/api`.
  - `/` and `//` give `""`.
  - `""`, `api`, `/a{`, `/{tenant}` and `/a}` are refused with `ErrConfiguration`.

  Also add cases to `TestNewHandler`:
  - `WithBasePath("api")` is refused;
  - `WithBasePath("/a{")` is refused;
  - `WithBasePath("")` is refused;
  - `WithBasePath("/")` gives `GET /notifications` as the first route.

  Also add `TestHandlerServesUnderItsBasePath`, a table:
  - alice's `GET /api/notifications` answers 200 under `/api/`;
  - her `GET /notifications` answers 200 under `/`;
  - her `GET /v1/notifications` answers 404 under `/`.

  Verify in three runs, because the `/a{` case panics and ends the test binary:
  1. `go test -run 'TestCleanBasePath' -count=1 .`. The five refusals must fail with `Expected error with "ntfy: invalid configuration" in chain but got nil.` `/`, `//`, `/v1` and `/api/` pass already.
  2. `go test -run 'TestNewHandler/a_base_path_the_router_cannot_parse_is_refused' -count=1 .`. It must panic with `bad wildcard segment`.
  3. `go test -run 'TestNewHandler|TestHandlerServesUnderItsBasePath' -skip 'TestNewHandler/a_base_path_the_router_cannot_parse_is_refused' -count=1 .`.
     - The other two refusals must fail with `Expected error with "ntfy: invalid configuration" in chain but got nil.`
     - The root case must show actual `"GET /v1/notifications"`.
     - The two root rows of `TestHandlerServesUnderItsBasePath` must fail: `/notifications` answers 404, and `/v1/notifications` answers 200.
- [ ] 2.3 Green: implement the rule in `CleanBasePath` (`design.md` D2). `handlerConfig` records `basePath` and `basePathSet`, and `NewHandler` calls `CleanBasePath` only when the option was given. Update the godoc of `WithBasePath`, `CleanBasePath` and `NewHandler`. Verify with `go test -run 'TestCleanBasePath|TestNewHandler|TestHandlerServesUnderItsBasePath' -count=1 .`.

## 3. WebSocket base path and origin patterns (`ntfy/websocket`)

- [ ] 3.1 Red: add cases to `TestNewHandler` in `websocket/handler_test.go`.
  - These must be configuration errors:
    - base path `api`, `""` and `/a{`;
    - origin pattern `[app.example.com`, `https://app.example.com` and `""`;
    - `WithAnyOrigin()` with `WithOriginPatterns("app.example.com")`, in both orders.
  - Base path `/` must give `GET /notifications/socket`.
  - `WithOriginPatterns()` with no patterns, and `WithOriginPatterns("*.example.com", "app.example.com:8443")`, must be accepted.

  Verify with `GOTOOLCHAIN=go1.26.8 go test -C websocket -run 'TestNewHandler' -count=1 .`. Every refusal must fail with `Expected error with "ntfy: invalid configuration" in chain but got nil.`, and the root case must show `"GET /v1/notifications/socket"`.
- [ ] 3.2 Green: in `websocket/handler.go`:
  - `WithBasePath` records the raw value and a set flag, and `NewHandler` validates it through `ntfy.CleanBasePath`;
  - add `validateOriginPattern(pattern string) error` (empty, contains `/`, `path.Match` syntax), called on every pattern;
  - add the any-origin conflict case.

  Update the godoc of `WithBasePath`, `WithOriginPatterns`, `WithAnyOrigin` and `NewHandler`, and put a comment on `checkOrigin` saying why its error is still discarded. Verify with `go test -C websocket -count=1 .`. `refusal_test.go` must still pass.

## 4. Bound the table prefix (`ntfy/sqlstore`)

- [ ] 4.1 Red: land the audit proof as `TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema` in a new `sqlstore/prefix_test.go`. It runs over one `sqlkittest.RunTestPostgres` container, with PostgreSQL cases for prefixes of 44, 32 and 31 bytes. Each case is either refused by `New` with `ErrConfiguration`, or:
  - `Migrate` succeeds;
  - `VerifySchema` is nil;
  - an `Insert` succeeds;
  - `MigrateEmail` succeeds;
  - `VerifyEmailSchema` is nil.

  Verify with `GOTOOLCHAIN=go1.26.8 go test -C sqlstore -run 'TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema' -count=1 .`.
  - The 44 case must fail on `42P10` (Insert), on six missing indexes (VerifySchema), and on `42P07` (MigrateEmail).
  - The 32 case must fail on `ntfy_notifications_recipient_idx is missing`.
  - The 31 case passes.
- [ ] 4.2 Red: add cases to `TestNew` in `sqlstore/store_test.go`: a 32-byte prefix and a 44-byte prefix, each refused with `ErrConfiguration`. Verify with `go test -C sqlstore -run 'TestNew$' -count=1 .` that both fail with `Expected error with "ntfy: invalid configuration" in chain but got nil.`
- [ ] 4.3 Green:
  - add `const MaxTablePrefixBytes = 31` and the length check to `sqlstore.New`, and update the godoc of `WithTablePrefix` and `New`;
  - add a `TestNew` case accepting exactly `MaxTablePrefixBytes` bytes;
  - add `TestTablePrefixBoundFitsEveryOwnedName` to `prefix_test.go`. For PostgreSQL, MySQL and SQLite it renders `Schema()` and `EmailSchema()` at the maximum prefix, and asserts that every quoted identifier is at most 63 bytes and that the longest is exactly 63;
  - add a MySQL row at `MaxTablePrefixBytes` to `TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema`, over `sqlkittest.RunTestMySQL`.

  Verify with `go test -C sqlstore -run 'TestNew$|TestTablePrefixBoundFitsEveryOwnedName|TestEveryTablePrefixNewAcceptsYieldsAWorkingSchema' -count=1 .`.
- [ ] 4.4 Correct `docs/schema.md`'s "Table prefix" section:
  - the longest index name is 32 bytes, and the prefix limit is 31 (`sqlstore.MaxTablePrefixBytes`);
  - a longer prefix is a configuration error from `sqlstore.New`;
  - a host with a longer prefix renames its tables and indexes with its database's own `ALTER ... RENAME`.

  Verify with `go test -C sqlstore -run 'TestTheDocumented' -count=1 .`, which reads its SQL from this document.

## 5. Refuse options given nil (core, `redis`, `nats`)

- [ ] 5.1 Red: in `service_test.go`, replace the `TestNew` case "nil options leave the defaults in place" with these cases:
  - "a nil option value is skipped" (`[]ntfy.Option{nil}`);
  - four refusals, one each for `WithClock(nil)`, `WithIDGenerator(nil)`, `WithBroadcaster(nil)` and `WithSignalErrorHandler(nil)`.

  In `pruner_test.go`, add "a nil error handler is refused" to `TestNewPruner`. Verify with `go test -run 'TestNew$|TestNewPruner' -count=1 .`. The five refusals must fail with `Expected error with "ntfy: invalid configuration" in chain but got nil.`
- [ ] 5.2 Green: the four `service.go` options and `WithPruneErrorHandler` assign unconditionally. `New` refuses each nil field before validating limits, and `pruneConfig.validate` refuses a nil `onError`. Rewrite each option's godoc from "A nil … is ignored" to "Nil is a [ConfigurationError]; omit the option to keep the default". Verify with `go test -count=1 .` (the whole core module).
- [ ] 5.3 Red, then green, in `ntfy/redis`.
  - Red: in `TestNewBroadcaster`, replace the filler `redis.WithDecodeErrorHandler(nil)` in "a channel and a timeout replace the defaults" with a real no-op handler, and add "a nil decode error handler" as `configurationError`. Watch it fail with `go test -C redis -run 'TestNewBroadcaster' -count=1 .`.
  - Green: make `config.onError` a `*func(ctx context.Context, err error)`, refuse a nil one in `NewBroadcaster`, and update the godoc. Verify that the same command passes.
- [ ] 5.4 The same as 5.3 in `ntfy/nats` (`TestNewBroadcaster`, "a subject replaces the default"). Verify with `go test -C nats -run 'TestNewBroadcaster' -count=1 .`.

## 6. Documentation

- [ ] 6.1 Update `docs/notifications.md`:
  - rewrite line 57 ("A nil option is ignored…") to state that a nil option value is skipped, and that an option given nil is a configuration error;
  - the `WithBasePath` bullet names `/` for the root, and the leading-slash and brace rules.

  Update `docs/realtime-operations.md`:
  - the origins row says patterns are hosts, validated at construction, and not combinable with `WithAnyOrigin`;
  - the reconnect-delay row names `ntfy.MaxReconnectDelay`.

  Verify with `go test -run 'TestTheDocumentMatchesTheImplementation' -count=1 .`.

## 7. Verify and hand off

- [ ] 7.1 Run `make all` and `make store-matrix`, and verify that both pass. Also run `openspec validate refuse-wiring-mistakes-at-construction --strict`.
- [ ] 7.2 Run `/code-review` on the branch diff, fix what it finds under `.claude/rules/prove-errors-with-tests.md`, and re-run 7.1.
