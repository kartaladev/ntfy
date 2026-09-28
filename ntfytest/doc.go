// Package ntfytest is the conformance suite every [ntfy.Store] must pass.
//
// One suite, run against the in-memory store and against ntfy/sqlstore on
// every supported driver and dialect, is what makes "identical on every store"
// a fact rather than an intention. A host that writes its own store runs the
// same suite against it.
//
// Beyond behaviour, the suite asserts memory isolation: a store copies what it
// retains and returns nothing it shares with its caller, with its own state, or
// with another notification from the same call.
//
// It also asserts identity: a store compares recipients, sources, subjects and
// kinds byte for byte, so identifiers differing only in case, a trailing space,
// an ignorable code point or their Unicode normalisation are different.
package ntfytest
