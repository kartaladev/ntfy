# pkg/sqlkit: temporary copy, do not edit

This directory is an unedited copy of sqlkit, the domain-free SQL toolkit
`sqlstore` is built on. It exists only so ntfy can build and test while sqlkit
moves to its own repository, `github.com/kartaladev/sqlkit`, and has no tag yet.

- **Source:** `sqlkit/` in `github.com/kartaladev/hmntsk` at the commit named in
  [`SOURCE`](SOURCE).
- **The only change:** every `github.com/kartaladev/hmntsk/sqlkit` path is
  rewritten to `github.com/kartaladev/sqlkit`, so ntfy already imports sqlkit's
  final paths.
- **Wiring:** the repository's `go.work` uses these five modules. No ntfy
  `go.mod` names a sqlkit version while this copy exists.
- **Never edit it here.** `make sqlkit-copy-check` fails when the copy differs
  from its source. Fix sqlkit in its own repository.
- **No ntfy tag while it exists.** Consumers never see `go.work`, so a tag would
  not resolve. Once sqlkit is tagged, delete this directory, drop it from
  `go.work` and require the real sqlkit versions.
