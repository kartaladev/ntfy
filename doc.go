// Package ntfy is a durable, per-recipient notification inbox that knows
// nothing about the domain publishing into it.
//
// A publisher addresses each [Notification] to exactly one recipient, about a
// subject at a version. A notification is ACTIVE until its recipient reads it or
// its subject moves on and a publisher closes it. Publishing is idempotent per
// source and recipient, and a close remembers the version it closed at, so a
// source retried after a newer close can never reopen the subject.
//
// Everything a host touches goes through a [Service]. It stores through a
// [Store] (in memory by default; ntfy/sqlstore for PostgreSQL, MySQL and
// SQLite), signals changes through a [Broadcaster] (in-process by default),
// bounds storage with a [Pruner] the host drives, and serves the HTTP contract
// and its server-sent event stream through a [Handler] the host mounts.
//
// Kinds, titles, links and data belong to the publisher: the library stores and
// returns them unchanged and interprets none of them.
//
// The package imports only the standard library. Durable storage, WebSocket
// transport and cross-instance broadcasting live in the separate modules
// ntfy/sqlstore, ntfy/websocket, ntfy/redis and ntfy/nats, so a host pulls in
// only the dependencies it uses.
package ntfy
