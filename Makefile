# NTFY_MODULES are the modules this repository publishes.
NTFY_MODULES := . ntfytest sqlstore websocket redis nats

# SQLKIT_COPY_MODULES are the temporary, unedited copy of
# github.com/kartaladev/sqlkit (see pkg/sqlkit/README.md). They are built and
# tested with everything else, but never linted, formatted, tidied or
# regenerated here: their code is fixed in sqlkit's own repository, and
# sqlkit-copy-check fails if it changes.
SQLKIT_COPY_MODULES := pkg/sqlkit pkg/sqlkit/sqlkittest pkg/sqlkit/stdsql pkg/sqlkit/pgx pkg/sqlkit/gorm

ALL_MODULES := $(NTFY_MODULES) $(SQLKIT_COPY_MODULES)

GO ?= go
GOLANGCI_LINT ?= golangci-lint

# The project builds, tests and lints on Go 1.26, the version every go.mod
# declares. Pinning GOTOOLCHAIN for every target keeps a developer whose default
# toolchain has moved ahead on exactly what CI runs.
#
# `?=` leaves CI alone: actions/setup-go installs the pinned version and sets
# GOTOOLCHAIN=local itself, and an environment value wins here.
GOTOOLCHAIN ?= go1.26.8
export GOTOOLCHAIN

# SQLKIT_SOURCE_REPO is where sqlkit-copy-check fetches the copy's source commit
# from. Point it at a local clone to check without network access.
SQLKIT_SOURCE_REPO ?= https://github.com/kartaladev/hmntsk.git

.PHONY: all build lint split-check sqlkit-copy-check fmt test test-race test-integration \
        tidy vuln generate store-matrix clean

all: lint split-check test

## build: compile every module in the workspace.
build:
	@set -e; for m in $(ALL_MODULES); do \
		echo "==> build $$m"; \
		(cd $$m && $(GO) build ./...); \
	done

## lint: run golangci-lint over every ntfy module.
lint:
	@set -e; for m in $(NTFY_MODULES); do \
		echo "==> lint $$m"; \
		(cd $$m && $(GOLANGCI_LINT) run ./...); \
	done

## split-check: fail when an ntfy module imports a github.com/kartaladev module
## other than ntfy or sqlkit.
##
## Test dependencies count too, so a sqlstore test importing a task-engine
## package fails here. This is the authoritative check; depguard in
## .golangci.yml is the earlier warning in an editor.
split-check:
	@set -e; fail=0; \
	for m in $(NTFY_MODULES); do \
		echo "==> split-check $$m"; \
		out=$$(cd $$m && $(GO) list -e -deps -test -f '{{.ImportPath}}|{{join .Imports " "}}' ./... | \
			awk -F'|' -v module="$$m" '{ \
				n = split($$2, imports, " "); \
				for (i = 1; i <= n; i++) { \
					p = imports[i]; \
					if (p ~ /^github\.com\/kartaladev\// && p !~ /^github\.com\/kartaladev\/(ntfy|sqlkit)(_test)?(\/|$$)/) \
						print "    " module ": package " $$1 " imports " p; \
				} \
			}' | sort -u); \
		if [ -n "$$out" ]; then echo "$$out"; fail=1; fi; \
	done; \
	if [ $$fail -ne 0 ]; then \
		echo "split-check: ntfy may import only ntfy and sqlkit among github.com/kartaladev modules"; \
		exit 1; \
	fi

## sqlkit-copy-check: fail when pkg/sqlkit differs from the source commit named
## in pkg/sqlkit/SOURCE, after the one path rewrite the copy is allowed.
##
## go.sum is excluded because the workspace may re-tidy it; README.md and
## SOURCE are this repository's own notes about the copy.
sqlkit-copy-check:
	@set -e; \
	commit=$$(awk '$$1 == "commit:" { print $$2 }' pkg/sqlkit/SOURCE); \
	test -n "$$commit" || { echo "sqlkit-copy-check: pkg/sqlkit/SOURCE names no commit"; exit 1; }; \
	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	echo "==> sqlkit-copy-check against $$commit"; \
	git init -q "$$tmp/repo"; \
	git -C "$$tmp/repo" fetch -q --depth 1 "$(SQLKIT_SOURCE_REPO)" "$$commit"; \
	mkdir -p "$$tmp/want"; \
	git -C "$$tmp/repo" archive "$$commit" sqlkit | tar -x -C "$$tmp/want"; \
	grep -rl 'github.com/kartaladev/hmntsk/sqlkit' "$$tmp/want/sqlkit" | \
		xargs perl -pi -e 's#github\.com/kartaladev/hmntsk/sqlkit#github.com/kartaladev/sqlkit#g'; \
	cp pkg/sqlkit/README.md pkg/sqlkit/SOURCE "$$tmp/want/sqlkit/"; \
	if ! diff -r -x go.sum "$$tmp/want/sqlkit" pkg/sqlkit; then \
		echo "sqlkit-copy-check: pkg/sqlkit was edited; fix sqlkit in its own repository instead"; \
		exit 1; \
	fi

## fmt: apply the configured formatters to every ntfy module in place.
fmt:
	@set -e; for m in $(NTFY_MODULES); do \
		(cd $$m && $(GOLANGCI_LINT) fmt ./...); \
	done

## test: run the tests in every module.
test:
	@set -e; for m in $(ALL_MODULES); do \
		echo "==> test $$m"; \
		(cd $$m && $(GO) test ./...); \
	done

## test-race: run the tests with the race detector.
test-race:
	@set -e; for m in $(ALL_MODULES); do \
		echo "==> test -race $$m"; \
		(cd $$m && $(GO) test -race ./...); \
	done

## test-integration: the same tests, with the time and freshness a container run
## needs. There is deliberately no build tag: tests that provision a real
## database or broker are part of `go test ./...`.
test-integration:
	@set -e; for m in $(ALL_MODULES); do \
		echo "==> integration $$m"; \
		(cd $$m && $(GO) test -count=1 -timeout 30m ./...); \
	done

## tidy: tidy every ntfy module and re-sync the workspace.
tidy:
	@set -e; for m in $(NTFY_MODULES); do \
		(cd $$m && $(GO) mod tidy); \
	done
	$(GO) work sync

## vuln: scan every module for known vulnerabilities.
vuln:
	@set -e; for m in $(ALL_MODULES); do \
		echo "==> govulncheck $$m"; \
		(cd $$m && govulncheck ./...); \
	done

## generate: regenerate mocks and other generated code in every ntfy module.
generate:
	@set -e; for m in $(NTFY_MODULES); do \
		(cd $$m && $(GO) generate ./...); \
	done

# STORE_MATRIX is the seven valid driver-by-dialect combinations of the
# notification store conformance suite. It is sparse because pgx is
# PostgreSQL-only. The GORM entry points live in their own test package: GORM's
# SQLite driver registers under the same name as the one database/sql uses, and
# one binary cannot hold both.
STORE_MATRIX := sqlstore:TestStoreOnStdSQLPostgres sqlstore:TestStoreOnStdSQLMySQL \
                sqlstore:TestStoreOnStdSQLSQLite \
                sqlstore:TestStoreOnPgxPostgres \
                sqlstore/internal/gormtest:TestStoreOnGormPostgres \
                sqlstore/internal/gormtest:TestStoreOnGormMySQL \
                sqlstore/internal/gormtest:TestStoreOnGormSQLite

## store-matrix: run the notification store conformance suite over all seven
## combinations.
store-matrix:
	@set -e; for entry in $(STORE_MATRIX); do \
		m=$${entry%%:*}; run=$${entry##*:}; \
		echo "==> $$m $$run"; \
		(cd $$m && $(GO) test -count=1 -timeout 30m -run "^$$run$$" ./...); \
	done

clean:
	$(GO) clean -cache -testcache
