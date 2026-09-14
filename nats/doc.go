// Package nats carries notification change signals between application
// instances over NATS core subjects, as a
// [github.com/kartaladev/ntfy.Broadcaster].
//
// Every instance subscribed to the subject receives every signal published on
// it: the subscription deliberately uses no queue group, which would hand each
// signal to only one instance. A message carries only recipients, changes and
// their times, in the ntfy signal format; never a notification's title,
// links, data, kind or subject.
//
// Listening reports ready only once the server has confirmed the subscription,
// within a subscribe timeout, so a signal broadcast from then on reaches it.
//
// Broadcasting is best effort. An unreachable server never fails the
// notification write that produced a signal, signals published while an
// instance is disconnected are not replayed to it, and the notification store
// remains the source of truth.
//
// Signals are ephemeral core-NATS messages, not durable events. A host that
// needs events it can replay should publish them through JetStream on subjects
// of its own; the broadcaster's default subject is namespaced so that it does
// not collide with them.
package nats
