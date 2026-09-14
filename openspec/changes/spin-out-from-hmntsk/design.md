## Context

See `proposal.md` for why. The source is hmntsk (`/Users/zakyalvan/Documents/RND/hmntsk`, `git@github.com:kartaladev/hmntsk.git`), where:

- notify is six modules in one `go.work`. Their `go.mod` files leave intra-repository requires to the workspace, with no `replace` directives. Four hmntsk commits touch `notify/`, the latest being `e2be1b0`.
- sqlkit is five modules (`sqlkit`, `sqlkittest`, `stdsql`, `pgx`, `gorm`) from a single commit, `95f9278`. `notify/sqlstore` imports all five; `pgx` and `gorm` only from test code.
- `make split-check` and a depguard rule enforce that notify imports only notify and sqlkit. The same Makefile runs a seven-combination store conformance matrix (stdsql on PostgreSQL, MySQL and SQLite; pgx on PostgreSQL; GORM on all three).
- hmntsk's working tree has uncommitted work outside `notify/` and `sqlkit/`.

A separate session is moving sqlkit to `github.com/kartaladev/sqlkit` in parallel. Go treats `hmntsk/sqlkit` and `kartaladev/sqlkit` as unrelated types.

## Goals / Non-Goals

**Goals:**
- ntfy builds, lints and passes the full test suite, conformance matrix included, from this repository alone, with no checkout of hmntsk or sqlkit.
- Every import in ntfy code is already its final path. Replacing the sqlkit copy with real tags changes no import.

**Non-Goals:**
- No behaviour change beyond the renames in `specs/ntfy-modules`.
- No sqlkit development here, and no hmntsk edits (see Migration Plan for the later hmntsk work).
- No project-wide agent or OpenSpec configuration changes (see Open Questions).

## Decisions

### D1. Plain copy from a committed hmntsk tree, no history

Take the files with `git -C ../hmntsk archive <commit> notify sqlkit`, not from the working tree, so uncommitted hmntsk work cannot leak in. Use hmntsk's `main` HEAD at import time. The import commit message records that hash, and `pkg/sqlkit/SOURCE` records it for sqlkit.

*Alternatives:* `git filter-repo` keeps four notify commits and one sqlkit commit. It isn't worth mixing hmntsk's history into a repository that already has its own. Copying the working tree is rejected because of the WIP.

### D2. Layout: the library at the root, sqlkit under `pkg/`

```
ntfy/
  go.mod  (github.com/kartaladev/ntfy, package ntfy)     <- notify/
  ntfytest/   sqlstore/   websocket/   redis/   nats/    <- notify/*
  docs/                                                  <- notify/docs
  pkg/sqlkit/ (+ sqlkittest, stdsql, pgx, gorm)          <- sqlkit/, temporary
  go.work  Makefile  .golangci.yml  .github/workflows/
```

`pkg/` holds only the borrowed copy. ntfy's own packages never go under it, so the folder disappears with the copy.

*Alternatives:* keeping a `notify/` subdirectory would give an import path of `ntfy/notify`, which says the name twice. `third_party/` was considered, and the user chose `pkg/`.

### D3. Rename the package and its identifiers

`package notify` becomes `ntfy`, and `notify_test` becomes `ntfy_test`. `notifytest` becomes `ntfytest`, in both the directory and the package. Doc links (`[notify.X]`), prose and error details that name the package (for example `pass notify.AllowAll`) follow. Import paths are rewritten mechanically (`github.com/kartaladev/hmntsk/notify` → `github.com/kartaladev/ntfy`, and `.../notifytest` → `.../ntfytest`). Package clauses and qualifiers are renamed with gopls, so references are resolved semantically rather than by text.

**Default / override:** none. An import path and package name are the library's identity. A consumer who wants another local name uses an import alias, which Go already allows.

### D4. Storage and wire names follow the rename (user decision)

| Name | Default after the rename | Consumer override |
| --- | --- | --- |
| Error text prefix | `ntfy:` | None. Errors are matched with `errors.Is` or `errors.As` against the exported sentinels and types, and the text is not a contract. |
| WebSocket subprotocol | `ntfy.v1` | None. It is a versioned client contract, and a client that needs another version needs a new handler version, not a string. |
| Redis channel | `ntfy.signals` | `redis.WithChannel` (unchanged). |
| NATS subject | `ntfy.signals` | `nats.WithSubject` (unchanged). |
| SQL tables and indexes | `ntfy_notifications`, `ntfy_watermarks`, `ntfy_email_deliveries`, and `ntfy_*_idx` | `sqlstore.WithTablePrefix` adds a prefix. The base names stay fixed so the schema check and the DDL files remain one contract. |

The signal JSON format (`{"v":1,...}`) contains no name and is unchanged.

*Alternatives:* renaming only the Go names, or only the package, would keep hmntsk and ntfy sharing tables and channels during cut-over. It would also leave the library's storage and wire names permanently disagreeing with its name. Nothing is tagged and nothing is known to run against these names, so a rename now is free. Per `.claude/rules/library-design.md` this is still a default change, and this table is its record.

### D5. The sqlkit copy lives under `pkg/sqlkit/`, never edited, wired in by `go.work`

- Copy hmntsk's `sqlkit/` at the D1 commit. Replace every occurrence of `github.com/kartaladev/hmntsk/sqlkit` with `github.com/kartaladev/sqlkit`, in `.go`, `go.mod` and docs. Change nothing else.
- As in hmntsk, the copy's `go.mod` files leave their links to each other to the workspace. ntfy's `go.work` uses all five copy modules, and ntfy's own `go.mod` files name no sqlkit version.
- `pkg/sqlkit/README.md` states what the copy is, where it came from, that it must not be edited, and that it is removed once sqlkit is tagged. `pkg/sqlkit/SOURCE` records the repository and commit.

*Alternatives:*
- Pseudo-versions of `hmntsk/sqlkit` don't resolve (their `go.mod` files rely on go.work) and would import a dying path.
- A go.work pointing at a sibling `../sqlkit` checkout would force CI to check out two repositories.
- Waiting for sqlkit tags would serialise the two efforts.

### D6. A drift check keeps the copy honest

`make sqlkit-copy-check` does four things:
1. Fetch hmntsk at the commit in `pkg/sqlkit/SOURCE` (a shallow fetch of the public repository).
2. Apply the D5 path rewrite to its `sqlkit/`.
3. Compare the result with `pkg/sqlkit/`, excluding `go.sum`, which the workspace may re-tidy.
4. Fail on any difference.

CI runs it in the lint job. It is deleted together with the copy.

### D7. The dependency boundary is checked on the module graph

`make split-check` lists every package of every ntfy module (the copy excluded) with `go list -deps -test`. It fails when any `github.com/kartaladev/...` import is neither `github.com/kartaladev/ntfy...` nor `github.com/kartaladev/sqlkit...`, naming the module and the import (spec: `ntfy-modules`).

A depguard rule mirrors it as an early warning in the editor. Unlike in hmntsk, the two allowed prefixes no longer overlap a forbidden one, so depguard is exact. `split-check` stays authoritative because it also sees the test-only dependency graph.

A root-module test asserts that production imports are standard library only. It follows the pattern of hmntsk's `depdirection_test.go`.

### D8. Tooling carried over and narrowed

- **Makefile:** `NTFY_MODULES` (root, `ntfytest`, `sqlstore`, `websocket`, `redis`, `nats`) drives build, lint, fmt, test, test-race, test-integration, tidy, vuln and generate. `SQLKIT_COPY_MODULES` is built and unit-tested but not linted, since its code is not ntfy's to fix. `store-matrix` runs the seven sqlstore combinations. The `GROUP`, hmntsk and examples machinery is dropped.
- **CI:** jobs `lint` (lint, split-check, sqlkit-copy-check), `unit` and `store-matrix` (seven entries), on Go 1.26 as in hmntsk. The sqlkit executor matrix belongs to the sqlkit repository and is not carried.
- **`.golangci.yml`:** hmntsk's linter set and settings, with the D7 depguard rule and `pkg/sqlkit/**` excluded.

### D9. Documentation

`notify/docs/*.md` becomes `docs/`. The root package's docs test reads `docs/notifications.md` relative to its own directory, so the path still holds with the library at the root.

Task-engine references are rewritten as library-neutral text:
- `notifications.md` lines 5, 65, 89 and 211 (the `tasknotify` adapter, "a task adapter", "the task HTTP contract");
- `realtime-operations.md` line 92 (the comparison with hmntsk's `delivery/*` modules).

Defaults tables pick up the D4 names. `README.md` grows a short introduction: module list, install and a link to `docs/`.

### D10. Specs and archived changes

The notification capabilities arrive as this change's ADDED specs (see `specs/`). They become `openspec/specs/` when the change is archived.

hmntsk's `2026-09-14-notify-{core,email,listen-readiness,realtime-adapters}` archives are copied unchanged to `openspec/changes/archive/` as design history. They keep their hmntsk paths and names, and a line in `openspec/changes/archive/README.md` says so.

`task-notifications` and the `2026-09-14-tasknotify` archive stay in hmntsk.

## Risks / Trade-offs

- **[sqlkit's spin-out changes its API]** → `sqlstore` breaks when the copy is swapped for tags. Mitigation: the copy is pinned to `95f9278`, and the sqlkit session knows ntfy builds against it. The swap is its own change, with the conformance matrix as the gate.
- **[hmntsk's `notify/` changes after the copy]** → fixes land in the wrong place or get lost. Mitigation: import from the latest committed hmntsk `main` just before this change is applied. After import, notify changes go to ntfy only, and any later hmntsk notify commit is ported by hand, citing its hash. A freeze check in hmntsk is the hmntsk session's decision, not this change's.
- **[a "notify" name survives the rename]** → an inconsistent error, doc link or DDL name. Mitigation: a verification task searches the tree for `notify` as a word and allows only the English verb and archived history.
- **[ntfy is tagged while `pkg/sqlkit/` exists]** → the tag does not resolve for consumers, because go.work is invisible to them. Mitigation: `docs/releasing.md` states the gate, and CI's `sqlkit-copy-check` makes the copy's presence explicit. No tag is part of this change.
- **[someone already runs hmntsk's notify tables or channel]** → at hmntsk cut-over they need `ALTER TABLE ... RENAME` and a client subprotocol update. Mitigation: nothing is tagged. The hmntsk cut-over change documents the rename statements for each dialect.

## Migration Plan

1. **This change (ntfy):** import, rename, tooling, specs. It only adds to this repository. Rollback is reverting its commits.
2. **Swap sqlkit (ntfy, later change, after sqlkit is tagged):** delete `pkg/sqlkit/`, drop its `go.work` entries and `sqlkit-copy-check`, add real `require github.com/kartaladev/sqlkit...` lines, and fix any API drift. Then tag ntfy `v0.1.0`, with tag order: root, `ntfytest`, then `sqlstore`, `websocket`, `redis`, `nats`.
3. **hmntsk cut-over (hmntsk, later change):**
   1. Switch `store/*`, `storetest` and `tasknotify` to `kartaladev/sqlkit`.
   2. Switch `tasknotify` and `examples` to `kartaladev/ntfy`. This happens in the same change as step 1 or after it, never before, because the old and new sqlkit types are incompatible.
   3. Delete `notify/` and `sqlkit/`.
   4. Update `go.work`, the Makefile, CI, `.golangci.yml` and `docs/releasing.md`.

## Open Questions

- Should this repository get `openspec/config.yaml` project context and a `CLAUDE.md` like hmntsk's? It's a configuration change for the user to decide, and it doesn't affect the import.
- Should ntfy carry runnable examples beyond its `Example` tests (hmntsk's `notify-standalone`, `realtime-scaling`)? That can follow later without changing this change's tasks.
