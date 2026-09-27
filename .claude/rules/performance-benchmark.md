# Rule: Prove performance with a benchmark and a failing threshold

**A claim that code is too slow, allocates too much or does not scale must be proved by a
benchmark and a threshold that fails against the current code.** Until that threshold has been
seen to fail, the claim is a hypothesis, not a finding. It may not justify a change, and it is
labelled **unverified**, exactly as `.claude/rules/prove-errors-with-tests.md` requires for any
other defect.

This applies wherever a performance problem is asserted: an audit, a review, a proposal or
design, a task, a commit message, a pull request, or a claim made in conversation.

## What counts as proof

1. **A benchmark that measures the operation in question.** A `Benchmark` function sized to
   show the effect, run with `-benchmem` when allocation is part of the claim.
2. **A threshold that fails.** A benchmark only reports numbers and cannot fail on its own.
   Pair it with one of these, and watch it fail against the code as it stands:
   - a Go test that times the operation and asserts the threshold;
   - a recorded comparison of benchmark output against a threshold stated beforehand.
3. **A relative threshold where one exists.** Prefer ratios and invariants over absolute
   timings, which vary between machines:
   - "the cost at 200k is within 2× of the cost at 20k";
   - "the pass costs the same after rows are added outside the window".

   An absolute number such as "under 100 ms" is an anchor for one stated machine and seed,
   never the only threshold.
4. **The threshold is stated before it is measured.** A threshold lowered, or a fixture
   resized, until the run happens to fail proves nothing. The same holds in the other
   direction after a fix.

## When the measurement does not reproduce

If the first measurement does not show the claimed cost, **stop**. Record the numbers under a
heading "Does not reproduce", commit the evidence and the benchmark, and park the change. Do
not implement the optimisation. `.claude/rules/plans-beside-tasks.md` requires every plan
resting on a performance claim to carry this step explicitly.

## Record the evidence

Each performance change keeps an evidence file in its change directory, for example
`openspec/changes/<change>/evidence.md` or `measurements.md`. It records:

- the command, run verbatim;
- the machine: CPU, RAM, OS and Go version, plus Docker version for a store that needs it;
- the fixture or seed shape;
- the numbers before the change, and after it;
- the date.

Absolute figures are labelled as one machine's. The relative threshold is the property under
test.

## Keep the benchmark

- **The benchmark and its threshold test stay in the repository** as a regression guard,
  beside the code they measure.
- **Re-run them when that code changes**, and update the evidence file when the numbers move.
- **Keep them out of the default test run's way.** Where a threshold test is slow or sensitive
  to instrumentation, gate it:
  - with a build constraint, such as `//go:build !race`, since the race detector distorts
    timings;
  - or with an opt-in environment variable for measurements that need a large seed or a
    container.

  The gate's condition is documented at the top of the file.

## Where this does not apply

- **Hot paths do not need benchmarks by default.** `.claude/rules/golang-tdd.md` requires hot
  paths to be covered by *test cases*, meaning correctness. Benchmarks are required only where
  performance is the reason for a change, or the subject of a claim. Benchmarking everything
  adds noisy upkeep and proves nothing in particular.
- **A performance fact about a third party** is not a defect in this repository. Examples: a
  database planner's behaviour, a library's allocation profile. It carries its own evidence: a
  link to the documentation, or the label **unverified**.

## How it meets the other rules

- **`.claude/rules/prove-errors-with-tests.md`:** a performance defect is a defect. This rule
  says what its failing test looks like.
- **`.claude/rules/golang-tdd.md`:** the failing threshold is the red step. The fix is green
  when the same threshold passes, and it stays in place as the guard.
- **`.claude/rules/plans-beside-tasks.md`:** the plan's STOP step is the "does not reproduce"
  branch above.

## Why

Performance claims drift further than any other kind. "This is slow" survives from an audit on
one machine to a proposal on another, and an optimisation built on it adds complexity that may
buy nothing here. A benchmark without a threshold cannot fail, so it cannot prove a defect or
guard a fix. A threshold without a benchmark has no evidence behind it. Together they turn a
suspicion into something reproducible. They also leave behind a guard that notices when the
cost comes back.
