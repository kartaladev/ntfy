## Why

The generic notification library lives in hmntsk as `notify/`. Its own `go.mod` promises it "moves to its own repository before its first tag", and until it does nobody can depend on it without also taking the hmntsk import path, which is about to change. `github.com/kartaladev/ntfy` is that home. The sqlkit spin-out (to `github.com/kartaladev/sqlkit`) is running at the same time, and waiting for its tags would serialise two independent efforts.

## What Changes

- Import the six notify modules from hmntsk (`notify`, `notify/notifytest`, `notify/sqlstore`, `notify/websocket`, `notify/redis`, `notify/nats`) into this repository as a **plain copy with no hmntsk history**. The import commit cites the source hmntsk commit.
- **BREAKING** (for anyone reading hmntsk's untagged code; nothing is tagged): new module paths under `github.com/kartaladev/ntfy`, with the library module at the repository root. The package `notify` is renamed to `ntfy`, and `notifytest` to `ntfytest`. `sqlstore`, `websocket`, `redis` and `nats` keep their names.
- **BREAKING** (same caveat): names outside Go code follow the rename. Error text is prefixed `ntfy:`, the WebSocket subprotocol is `ntfy.v1`, the default Redis channel and NATS subject are `ntfy.signals`, and the SQL tables and indexes are `ntfy_notifications`, `ntfy_watermarks` and `ntfy_email_deliveries`. The existing options that replace the channel, subject and table prefix are unchanged.
- Put a temporary, unedited copy of hmntsk's five sqlkit modules under `pkg/sqlkit/`. It uses the final `github.com/kartaladev/sqlkit` module paths and is wired in through `go.work`, so ntfy code imports sqlkit's final paths from its first commit. A check fails the build if the copy drifts from its recorded source. The copy is deleted, and replaced by real `require`s, once sqlkit is tagged.
- Bring the notify documentation (`docs/`) over, with task-engine wording removed so it describes a standalone library.
- Bring the build tooling over: Makefile targets (build, lint, test, the notification store conformance matrix), GitHub Actions CI, a `split-check` that allows only ntfy and sqlkit among kartaladev modules, and the golangci-lint configuration with a matching depguard rule.
- Bring the notification specs over as this repository's own capabilities, and the four `notify-*` archived changes from hmntsk as read-only design history.
- No release: ntfy is not tagged while `pkg/sqlkit/` exists.

## Non-goals

- No change to hmntsk. `notify/` and `sqlkit/` stay there, and `tasknotify` and `examples` keep importing them. The hmntsk cut-over is planned in `design.md` but done by a later change in hmntsk.
- No behaviour change to the library beyond the rename. Everything else is carried over as is.
- `tasknotify`, the `task-notifications` spec and hmntsk's runnable `examples/` stay in hmntsk.
- sqlkit itself is not developed here. Its home, spec (`sql-toolkit`) and tags belong to the sqlkit spin-out.

## Capabilities

### New Capabilities

- `ntfy-modules`: the module layout consumers depend on. It covers import paths, package names, which modules a consumer must take and which are opt-in, and the rule that ntfy depends on no hmntsk module.
- `notification-inbox`: durable per-recipient notifications: idempotent publish, active/read/closed states, closing by subject, reading. Carried over from hmntsk unchanged.
- `notification-email`: host-driven email of unread notifications with at-most-once delivery by default. Carried over from hmntsk unchanged.
- `notification-realtime`: change signals over SSE and WebSocket, and Redis/NATS broadcasting across instances. Carried over from hmntsk unchanged.
- `notification-retention`: count and age bounds and host-driven pruning. Carried over from hmntsk unchanged.
- `notification-http-api`: the notification HTTP contract. Carried over from hmntsk, with the error-body requirement restated without reference to hmntsk's task HTTP contract.

### Modified Capabilities

None. This repository has no specs yet.

## Impact

- **This repository**: gains every Go module, `go.work`, `Makefile`, `.golangci.yml`, `.github/workflows/`, `docs/` and `openspec/specs/`.
- **Dependencies**: a temporary workspace dependency on the `pkg/sqlkit/` copy. Later, a real dependency on `github.com/kartaladev/sqlkit` tags. The third-party dependencies are the same as in hmntsk: `coder/websocket`, `go-redis`, `nats.go`, the SQL drivers, GORM, testcontainers, testify, goleak, gomock.
- **hmntsk**: untouched by this change. The follow-up cut-over there must switch to `kartaladev/sqlkit` before, or together with, switching to `kartaladev/ntfy`, because the old and new sqlkit types are incompatible.
- **sqlkit spin-out**: ntfy builds against the sqlkit API as of hmntsk commit `95f9278`. API changes made during that spin-out need to be reflected in `sqlstore` when the copy is replaced.
