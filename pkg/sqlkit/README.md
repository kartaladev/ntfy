# pkg/sqlkit: temporary copy

This directory is a copy of sqlkit, the domain-free SQL toolkit `sqlstore` is
built on. It exists only so ntfy can build and test while sqlkit moves to its
own repository, `github.com/kartaladev/sqlkit`, and has no tag yet.

- **Source:** `sqlkit/` in `github.com/kartaladev/hmntsk` at the commit named in
  [`SOURCE`](SOURCE).
- **The rewrite:** every `github.com/kartaladev/hmntsk/sqlkit` path is
  rewritten to `github.com/kartaladev/sqlkit`, so ntfy already imports sqlkit's
  final paths.
- **The patches:** changes ntfy needed before sqlkit could make them are listed
  in [`PATCHES.md`](PATCHES.md), each with its reason, its proof and a patch
  file in [`patches/`](patches/) to send to sqlkit's own repository.
- **Wiring:** the repository's `go.work` uses these five modules. No ntfy
  `go.mod` names a sqlkit version while this copy exists.
- **Edit it only through a recorded patch.** `make sqlkit-copy-check` rebuilds
  the copy from its source, the rewrite and the patches in `patches/`, and fails
  on any other difference. To change sqlkit here, make the change, add or
  regenerate its patch, and describe it in `PATCHES.md`. Prefer fixing sqlkit in
  its own repository when that is possible.
- **No ntfy tag while it exists.** Consumers never see `go.work`, so a tag would
  not resolve. Once sqlkit is tagged with the patches in it, delete this
  directory, drop it from `go.work` and require the real sqlkit versions.
