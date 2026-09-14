// Package redis carries notification change signals between application
// instances over Redis publish/subscribe, as a
// [github.com/kartaladev/ntfy.Broadcaster].
//
// Every instance that shares a channel receives every signal broadcast on it, so
// a notification stored through one instance reaches a client connected to
// another. A message carries only recipients, changes and their times, in the
// ntfy signal format; never a notification's title, links, data, kind or
// subject.
//
// Listening reports ready only once the broker has confirmed the subscription,
// so a hub running over this broadcaster accepts streams only when a signal
// broadcast from then on will reach them.
//
// Broadcasting is best effort. An unreachable broker never fails the
// notification write that produced a signal, signals broadcast while an
// instance is disconnected are not replayed to it, and the notification store
// remains the source of truth.
//
// Signals are ephemeral pub/sub messages, not durable events. A host that needs
// events it can replay should publish them to a stream of its own; the
// broadcaster's default channel is namespaced so that it does not collide with
// one.
package redis
