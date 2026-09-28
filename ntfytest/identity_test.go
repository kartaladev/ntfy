package ntfytest_test

// The conformance suite fails its own test when a store breaks it, so the only
// way to assert that it rejects a store is to run it in a child process and
// watch that process fail.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kartaladev/ntfy"
	"github.com/kartaladev/ntfy/ntfytest"
)

// identityChildEnv names the fold a child process applies and where, as
// "<fold>/<scope>", and marks a process as a child.
const identityChildEnv = "NTFYTEST_IDENTITY_CHILD"

// folds are the ways a store can merge identifiers that differ in their bytes.
var folds = map[string]func(string) string{
	"lower-casing":                strings.ToLower,
	"trimming trailing spaces":    func(s string) string { return strings.TrimRight(s, " ") },
	"stripping zero-width spaces": func(s string) string { return strings.ReplaceAll(s, "\u200b", "") },
	// The one canonical composition the suite's variants use, which is what a
	// collation equating NFC with NFD does to them.
	"normalising to NFC": func(s string) string { return strings.ReplaceAll(s, "e\u0301", "\u00e9") },
}

// The scopes a fold is applied in.
const (
	// everywhere folds every identifier the store is given.
	everywhere = "everywhere"
	// filtersOnly folds only what a store is asked to match against, and stores
	// what it is given as it is: the subject and kind filters of a listing, a
	// close's subject, kinds, exception and successor skips, and mark-all-read's
	// recipient.
	filtersOnly = "filters only"
)

// foldingStore is a memory store that folds identifiers, the way a database
// column with the wrong collation compares them.
type foldingStore struct {
	ntfy.Store
	fold        func(string) string
	filtersOnly bool
}

func (s foldingStore) all(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = s.fold(value)
	}

	return out
}

// stored folds an identifier the store keeps or looks up by key, which a
// filters-only store leaves alone.
func (s foldingStore) stored(value string) string {
	if s.filtersOnly {
		return value
	}

	return s.fold(value)
}

func (s foldingStore) Insert(ctx context.Context, subject string, in []ntfy.Insertion) (ntfy.InsertResult, error) {
	out := make([]ntfy.Insertion, len(in))
	for i, insertion := range in {
		n := &insertion.Notification
		n.Recipient, n.SourceID = s.stored(n.Recipient), s.stored(n.SourceID)
		n.Subject, n.Kind = s.stored(n.Subject), s.stored(n.Kind)
		out[i] = insertion
	}

	return s.Store.Insert(ctx, s.stored(subject), out)
}

func (s foldingStore) Close(ctx context.Context, req ntfy.CloseRequest, at time.Time, ids ntfy.IDGenerator) (ntfy.CloseResult, error) {
	req.Subject, req.Except = s.fold(req.Subject), s.fold(req.Except)
	req.Kinds, req.SuccessorSkip = s.all(req.Kinds), s.all(req.SuccessorSkip)

	return s.Store.Close(ctx, req, at, ids)
}

func (s foldingStore) Get(ctx context.Context, recipient, id string) (ntfy.Notification, error) {
	return s.Store.Get(ctx, s.stored(recipient), id)
}

func (s foldingStore) List(ctx context.Context, q ntfy.ListQuery) (ntfy.Page, error) {
	q.Recipient, q.Subject, q.Kinds = s.stored(q.Recipient), s.fold(q.Subject), s.all(q.Kinds)

	return s.Store.List(ctx, q)
}

func (s foldingStore) CountActive(ctx context.Context, recipient string) (int64, error) {
	return s.Store.CountActive(ctx, s.stored(recipient))
}

func (s foldingStore) MarkRead(ctx context.Context, recipient string, ids []string, at time.Time) (ntfy.MarkResult, error) {
	return s.Store.MarkRead(ctx, s.stored(recipient), ids, at)
}

func (s foldingStore) MarkAllRead(ctx context.Context, recipient string, through, at time.Time) (ntfy.MarkResult, error) {
	return s.Store.MarkAllRead(ctx, s.fold(recipient), through, at)
}

// TestIdentityChild runs the identity group against the store its environment
// names. It does nothing unless TestIdentityRejectsFoldingStores started it.
func TestIdentityChild(t *testing.T) {
	mode, ok := os.LookupEnv(identityChildEnv)
	if !ok {
		t.Skip("runs only as a child of TestIdentityRejectsFoldingStores")
	}

	name, scope, _ := strings.Cut(mode, "/")

	ntfytest.Run(t, func(*testing.T) ntfy.Store {
		if fold, folding := folds[name]; folding {
			return foldingStore{Store: ntfy.NewMemoryStore(), fold: fold, filtersOnly: scope == filtersOnly}
		}

		return ntfy.NewMemoryStore()
	})
}

// runIdentityChild runs the identity group in a child process folding by mode,
// and reports whether it passed and what it printed.
func runIdentityChild(t *testing.T, mode string) (passed bool, output string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run", "^TestIdentityChild$/^identity$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), identityChildEnv+"="+mode)
	out, err := cmd.CombinedOutput()

	return err == nil, string(out)
}

// TestIdentityRejectsFoldingStores proves the identity group fails a store that
// merges identifiers, wherever it merges them, and passes one that does not.
// The cases do not vary context, so the table has no ctx field.
func TestIdentityRejectsFoldingStores(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		mode   string
		assert func(t *testing.T, passed bool, output string)
	}

	// rejected requires an identity case itself to have failed, so that a child
	// failing for any other reason, such as a panic, is not taken for one.
	rejected := func(t *testing.T, passed bool, output string) {
		assert.Falsef(t, passed, "ntfytest.Run accepted a store that folds identifiers:\n%s", output)
		assert.Containsf(t, output, "--- FAIL: TestIdentityChild/identity/",
			"an identity case, not something else, rejected the store:\n%s", output)
	}

	cases := []testCase{
		{
			name: "a sound store passes",
			mode: "sound",
			assert: func(t *testing.T, passed bool, output string) {
				assert.Truef(t, passed, "ntfytest.Run failed the memory store:\n%s", output)
				assert.Contains(t, output, "--- PASS: TestIdentityChild/identity/alice_differing_in_case",
					"the child ran the identity group, so passing is not vacuous")
			},
		},
	}

	for name := range folds {
		for _, scope := range []string{everywhere, filtersOnly} {
			cases = append(cases, testCase{
				name: "a store " + name + " " + scope + " fails", mode: name + "/" + scope, assert: rejected,
			})
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			passed, output := runIdentityChild(t, tc.mode)
			tc.assert(t, passed, output)
		})
	}
}
