## 1. Import from hmntsk (D1, D2, D5)

- [x] 1.1 Record hmntsk's committed `main` HEAD, and confirm `git -C ../hmntsk diff --quiet <commit> -- notify sqlkit` is clean. Verify: the hash is noted for the import commit and `pkg/sqlkit/SOURCE`.
- [x] 1.2 Extract `notify/` with `git archive <commit>` into the repository root (`notify/*` → `./`, `notify/notifytest` → `ntfytest/`). Verify: the extracted file list matches `git -C ../hmntsk ls-tree -r --name-only <commit> notify` after the path mapping.
- [x] 1.3 Extract `sqlkit/` with `git archive <commit>` into `pkg/sqlkit/`, apply only the `github.com/kartaladev/hmntsk/sqlkit` → `github.com/kartaladev/sqlkit` rewrite, and add `pkg/sqlkit/README.md` and `pkg/sqlkit/SOURCE`. Verify: `grep -r 'kartaladev/hmntsk' pkg/sqlkit` finds nothing.
- [x] 1.4 Rewrite ntfy import paths and module lines (`github.com/kartaladev/hmntsk/notify` → `github.com/kartaladev/ntfy`, `.../notifytest` → `.../ntfytest`, `hmntsk/sqlkit` → `kartaladev/sqlkit`), and update the `go.mod` comments that mention hmntsk's go.work. Verify: `grep -rn 'kartaladev/hmntsk' --include='*.go' --include=go.mod .` finds nothing.
- [x] 1.5 Create `go.work` listing the six ntfy modules and the five `pkg/sqlkit` modules. Verify: `go build ./...` succeeds in every module, and `go test -count=1 ./...` passes in the root module while the package is still named `notify`.
- [x] 1.6 Commit the import as one commit citing the hmntsk hash, with no other change mixed in. Verify: `git show --stat HEAD` contains only imported files, `go.work` and the rewrites.

## 2. Rename the package (D3), test-first

- [x] 2.1 Rename `package notify`/`notify_test` to `ntfy`/`ntfy_test` and `notifytest` to `ntfytest` with gopls, together with every qualifier in the adapters and tests. Verify: `go vet ./...` passes in every module, and `go test -count=1 ./...` passes in the root, `ntfytest`, `websocket`, `redis`, `nats` and `sqlstore` (unit only).
- [x] 2.2 Update doc links (`[notify.X]` → `[ntfy.X]`) and prose or error details that name the package (for example "pass notify.AllowAll", "notify/sqlstore"). Verify: gopls reports no broken doc links, and `grep -rnw 'notify' --include='*.go' .` shows only the English verb.

## 3. Rename storage and wire names (D4, spec `ntfy-modules`), test-first

- [x] 3.1 Red: change the existing tests that assert error text, table and index names, the subprotocol, and the Redis and NATS defaults so they expect the `ntfy` names. Add a scenario test for `WithTablePrefix("app_")` if none covers it. Verify: `go test -run '<those tests>' -count=1 ./...` fails on the old names, not on compilation.
- [x] 3.2 Green: rename the `ntfy:` error prefixes, `websocket.Subprotocol`, `redis.DefaultChannel`, `nats.DefaultSubject`, the `sqlstore` table and index constants, and every DDL file under `sqlstore/ddl/`. Verify: the tests from 3.1 pass, and `grep -rn 'notify_\|notify\.v1\|notify\.signals\|"notify:' .` outside `openspec/changes/archive` finds nothing.
- [x] 3.3 Run the SQLite store conformance entries (`TestStoreOnStdSQLSQLite`, `TestStoreOnGormSQLite`) against the renamed DDL. Verify: both pass.

## 4. Dependency boundary (D7, spec `ntfy-modules`), test-first

- [x] 4.1 Red: add a root-module test asserting that production imports are standard library or the root module only, then temporarily add a third-party import and watch it fail. Verify: it fails with the offending import named, then passes once the import is removed.
- [x] 4.2 Add `make split-check`, which fails on any `github.com/kartaladev/...` import outside ntfy and sqlkit, test graph included. Verify: it passes on the tree and fails when a scratch test in `sqlstore` imports a `github.com/kartaladev/hmntsk` package (scratch file removed afterwards).

## 5. Tooling (D6, D8)

- [x] 5.1 Write the `Makefile` with `NTFY_MODULES`, `SQLKIT_COPY_MODULES` and the targets build, lint, fmt, test, test-race, test-integration, tidy, vuln, generate, split-check, sqlkit-copy-check and store-matrix. Verify: `make build test split-check` succeeds.
- [x] 5.2 Implement `sqlkit-copy-check`: fetch hmntsk at `SOURCE`, apply the path rewrite, diff against `pkg/sqlkit` excluding `go.sum`. Verify: it passes, and fails after a one-character edit to a copy file (edit reverted).
- [x] 5.3 Port `.golangci.yml` with the ntfy/sqlkit depguard rule and `pkg/sqlkit/**` excluded. Verify: `make lint` passes with no new `//nolint`.
- [ ] 5.4 Add `.github/workflows/ci.yml` with jobs `lint` (lint, split-check, sqlkit-copy-check), `unit` and the seven-entry `store-matrix` on Go 1.26. Verify: the workflow runs green on a pushed branch.
- [x] 5.5 Run the full store conformance matrix locally or in CI (stdsql PostgreSQL, MySQL, SQLite; pgx PostgreSQL; GORM PostgreSQL, MySQL, SQLite). Verify: all seven entries pass.

## 6. Documentation (D9)

- [x] 6.1 Move `notify/docs/*.md` to `docs/`, rewrite the task-engine references (`notifications.md` lines 5, 65, 89, 211; `realtime-operations.md` line 92), and apply the D4 names in the defaults tables. Verify: the root docs test passes, and `grep -rniE 'task|hmntsk' docs/` finds only generic uses.
- [x] 6.2 Add `docs/releasing.md` covering module tag order, and the rule that no tag is made while `pkg/sqlkit/` exists, plus the swap procedure. Verify: the document names every module in `NTFY_MODULES`.
- [x] 6.3 Expand `README.md` with an introduction, the module list with opt-in modules, install, and links to `docs/`. Verify: every import path in the README resolves in the workspace (`go list`).

## 7. OpenSpec history (D10)

- [x] 7.1 Copy hmntsk's `openspec/changes/archive/2026-09-14-notify-{core,email,listen-readiness,realtime-adapters}` unchanged, and add `openspec/changes/archive/README.md` noting they come from hmntsk and keep its names. Verify: `openspec list --json` still lists only this change as active.

## 8. Final verification

- [x] 8.1 Confirm no trace of the old identity remains. Verify: `grep -rnw notify . --exclude-dir=.git --exclude-dir=archive` shows only the English verb, and `grep -rn 'kartaladev/hmntsk' . --exclude-dir=.git --exclude-dir=archive` shows only `pkg/sqlkit/SOURCE`, the README provenance lines and this change's artifacts.
- [x] 8.2 Run `make lint split-check sqlkit-copy-check test test-race`. Verify: all targets pass, and `openspec validate spin-out-from-hmntsk --strict` passes.
