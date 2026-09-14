# Releasing

Six modules live in this repository, each tagged and versioned on its own:

| Module | Tag |
| --- | --- |
| `github.com/kartaladev/ntfy` | `v1.2.3` |
| `github.com/kartaladev/ntfy/ntfytest` | `ntfytest/v1.2.3` |
| `github.com/kartaladev/ntfy/sqlstore` | `sqlstore/v1.2.3` |
| `github.com/kartaladev/ntfy/websocket` | `websocket/v1.2.3` |
| `github.com/kartaladev/ntfy/redis` | `redis/v1.2.3` |
| `github.com/kartaladev/ntfy/nats` | `nats/v1.2.3` |

Go's module resolution decides the scheme: a module in a subdirectory is tagged
with its path as the prefix. A patch to the NATS broadcaster does not move the
core's version, and a host pinning the core is not dragged forward by it.

## No tag while `pkg/sqlkit/` exists

`pkg/sqlkit/` is a temporary copy of `github.com/kartaladev/sqlkit`, supplied to
`sqlstore` by `go.work` (see [`pkg/sqlkit/README.md`](../pkg/sqlkit/README.md)).
Consumers never see `go.work`, so a tag made while the copy exists would name a
`sqlstore` that does not resolve. **Nothing is tagged until the copy is gone.**

Replacing the copy with sqlkit's tags:

1. Delete `pkg/sqlkit/`.
2. Remove its five entries from `go.work`, `SQLKIT_COPY_MODULES` and the
   `sqlkit-copy-check` target from the `Makefile`, and the
   `make sqlkit-copy-check` step and `pkg/sqlkit` from the CI workflow.
3. In `sqlstore/go.mod`, require `github.com/kartaladev/sqlkit`,
   `.../sqlkittest`, `.../stdsql`, `.../pgx` and `.../gorm` at their released
   versions, then run `make tidy`.
4. Fix anything sqlkit's API changed since the copy's source commit, and prove it
   with `make test` and `make store-matrix`.

## Release order

A module can only be released after everything it depends on, because its
`go.mod` has to name a version that already exists. sqlkit is released first, in
its own repository.

```
1. .           the library: standard library only    (+ ntfytest, for tests)
2. ntfytest    depends on ntfy
3. sqlstore    depends on ntfy, ntfytest, sqlkit      (+ sqlkittest, stdsql, pgx, gorm, for tests)
4. websocket   depends on ntfy
5. redis       depends on ntfy                        (+ ntfytest, for tests)
6. nats        depends on ntfy                        (+ ntfytest, websocket, for tests)
```

`sqlstore` requires `ntfytest` outright, not only for tests: its internal test
harness is an ordinary package. The core's tests use `ntfytest`, which itself
requires the core. Go accepts that cycle between modules, so tag the core first
and then `ntfytest`, and raise the core's `ntfytest` requirement when the core is
next released.

For each module after the first, write the real `require` lines its `go.mod`
currently leaves to `go.work`, run `make tidy`, and tag. `make all` passes before
every tag.

## During development

`go.work` resolves the dependencies between this repository's modules, and the
satellite modules' `go.mod` files therefore do not name the core module. That is
deliberate for an untagged tree: a `replace` directive would be ignored by
consumers, and a placeholder version would not resolve.
