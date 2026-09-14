# ntfy

ntfy is a durable, per-recipient notification inbox for Go applications. The
host decides who is told what; ntfy stores each notification, keeps it in step
with its subject so a late publish never reopens something already closed,
bounds how much is kept, emails what goes unread, and tells each recipient's
connected clients that something changed.

It is a library to embed, not a service: sensible defaults with no
configuration, and every default replaceable through an option or a port.

## Modules

Take only what you use. The core module imports nothing but the standard
library.

| Module | What it adds |
| --- | --- |
| `github.com/kartaladev/ntfy` | The service, the in-memory store, the in-process broadcaster, retention, email dispatch, and the HTTP and server-sent event handlers. |
| `github.com/kartaladev/ntfy/sqlstore` | Durable storage on PostgreSQL, MySQL or SQLite, through database/sql, pgx or GORM. |
| `github.com/kartaladev/ntfy/websocket` | Change signals and mark-read over WebSocket. |
| `github.com/kartaladev/ntfy/redis` | Change signals across instances over Redis pub/sub. |
| `github.com/kartaladev/ntfy/nats` | Change signals across instances over NATS. |
| `github.com/kartaladev/ntfy/ntfytest` | Conformance suites for your own `Store` or `Broadcaster`. |

## Install

ntfy has no tagged release yet. Until it does, work inside this repository's Go
workspace (`go.work`).

```sh
go get github.com/kartaladev/ntfy
```

```go
svc, err := ntfy.New(ntfy.NewMemoryStore())
```

## Documentation

- [Notifications](docs/notifications.md): publishing, closing, reading, retention
  and the HTTP contract.
- [Email](docs/email.md): emailing unread notifications.
- [Realtime operations](docs/realtime-operations.md): broadcasters, transports
  and running several instances.
- [Schema](docs/schema.md): the SQL tables and migrations.
- [Releasing](docs/releasing.md): tags, release order and the temporary sqlkit
  copy.

## Development

```sh
make all            # lint, split-check and test
make store-matrix   # store conformance on every driver and dialect (needs Docker)
```

ntfy was built inside [hmntsk](https://github.com/kartaladev/hmntsk) as `notify/`
and moved here before its first release.
