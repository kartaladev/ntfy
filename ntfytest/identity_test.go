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

// identityChildEnv names the fold a child process applies, and marks a process
// as a child.
const identityChildEnv = "NTFYTEST_IDENTITY_CHILD"

// folds are the ways a store can merge identifiers that differ in their bytes.
var folds = map[string]func(string) string{
	"lower-casing":                strings.ToLower,
	"trimming trailing spaces":    func(s string) string { return strings.TrimRight(s, " ") },
	"stripping zero-width spaces": func(s string) string { return strings.ReplaceAll(s, "\u200b", "") },
}

// foldingStore is a memory store that folds every identifier it is given, the
// way a database column with the wrong collation compares them.
type foldingStore struct {
	ntfy.Store
	fold func(string) string
}

func (s foldingStore) all(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = s.fold(value)
	}

	return out
}

func (s foldingStore) Insert(ctx context.Context, subject string, in []ntfy.Insertion) (ntfy.InsertResult, error) {
	out := make([]ntfy.Insertion, len(in))
	for i, insertion := range in {
		n := &insertion.Notification
		n.Recipient, n.SourceID, n.Subject, n.Kind = s.fold(n.Recipient), s.fold(n.SourceID), s.fold(n.Subject), s.fold(n.Kind)
		out[i] = insertion
	}

	return s.Store.Insert(ctx, s.fold(subject), out)
}

func (s foldingStore) Close(ctx context.Context, req ntfy.CloseRequest, at time.Time, ids ntfy.IDGenerator) (ntfy.CloseResult, error) {
	req.Subject, req.Except = s.fold(req.Subject), s.fold(req.Except)
	req.Kinds, req.SuccessorSkip = s.all(req.Kinds), s.all(req.SuccessorSkip)

	return s.Store.Close(ctx, req, at, ids)
}

func (s foldingStore) Get(ctx context.Context, recipient, id string) (ntfy.Notification, error) {
	return s.Store.Get(ctx, s.fold(recipient), id)
}

func (s foldingStore) List(ctx context.Context, q ntfy.ListQuery) (ntfy.Page, error) {
	q.Recipient, q.Subject, q.Kinds = s.fold(q.Recipient), s.fold(q.Subject), s.all(q.Kinds)

	return s.Store.List(ctx, q)
}

func (s foldingStore) CountActive(ctx context.Context, recipient string) (int64, error) {
	return s.Store.CountActive(ctx, s.fold(recipient))
}

func (s foldingStore) MarkRead(ctx context.Context, recipient string, ids []string, at time.Time) (ntfy.MarkResult, error) {
	return s.Store.MarkRead(ctx, s.fold(recipient), ids, at)
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

	ntfytest.Run(t, func(*testing.T) ntfy.Store {
		if fold, folding := folds[mode]; folding {
			return foldingStore{Store: ntfy.NewMemoryStore(), fold: fold}
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
// merges identifiers, and passes one that does not. The cases do not vary
// context, so the table has no ctx field.
func TestIdentityRejectsFoldingStores(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		mode   string
		assert func(t *testing.T, passed bool, output string)
	}

	rejected := func(t *testing.T, passed bool, output string) {
		assert.Falsef(t, passed, "ntfytest.Run accepted a store that folds identifiers:\n%s", output)
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

	for mode := range folds {
		cases = append(cases, testCase{name: "a store " + mode + " fails", mode: mode, assert: rejected})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			passed, output := runIdentityChild(t, tc.mode)
			tc.assert(t, passed, output)
		})
	}
}
