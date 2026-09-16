# Rule: Prove every claimed defect with a failing test

**Any claim that something is wrong must be proved by a test that fails against the current
code.** Until that test exists and has been seen to fail, the claim is a hypothesis, not a
finding, and must be labelled as one.

This applies to every place a defect is asserted:

- a code review, audit or security finding, whether written by a person or an agent;
- a bug report, an issue, a commit message or a pull request description;
- an `openspec` proposal, design or task that justifies work by "X is broken";
- a claim made in conversation, including one made in passing.

## What counts as proof

A test that:

1. **Fails against the code as it stands**, for the stated reason — not because it does not
   compile, a fixture is missing, or an unrelated assertion trips.
2. **Passes once the defect is fixed**, and would fail again if the fix were reverted.
3. **Lives in the repository** with the fix, so the defect cannot return unnoticed.

Show the failing run. A claim is reported with its evidence:

```sh
go test -run 'TestTheDefect' -count=1 ./...   # red, for the stated reason
```

For a defect whose test cannot be a plain `go test` — a schema, a generated document, a
Makefile target — the proof is the equivalent reproducible command and its failing output.

## When a failing test cannot be written

Say so explicitly, in the same breath as the claim, and say why. Use plain words:

> **Unverified:** the claim, and what stops it being tested (needs a provider account, a
> multi-instance deployment, a clock skew that cannot be faked, and so on).

An unverified claim may still be worth recording. It may **not** be stated as fact, counted
as a finding, or used as the justification for a change on its own. If it matters enough to
act on, the first task is making it testable.

Third-party facts — a provider's rate limit, an API's behaviour, a library's guarantee —
are not defects in this repository and are not covered by this rule. They carry their own
evidence: a link to the documentation, or the label **unverified**.

## Why

A defect that no test reproduces is a defect nobody has understood yet. Descriptions drift,
severity gets inflated or dismissed, and the fix has nothing to aim at. Worse, a fix accepted
without a red test may address something that was never broken, while the real fault survives.

The failing test is also what makes the fix reviewable: it states, executably, what the code
was doing wrong.

## Relationship to `golang-tdd.md`

`golang-tdd.md` governs the order in which *new behaviour* is written: red, green, refactor.
This rule governs *claims about existing behaviour*. They meet at the same place — a bug fix
starts with the test that proves the bug — and when both apply, that test is the red step.
