# Rule: Every `tasks.md` has a `plans.md` beside it

**Whenever an OpenSpec change's `tasks.md` is created or modified, invoke
`/superpowers:writing-plans` and write the resulting plan to `plans.md` in the same
directory.** The plan is part of the same piece of work as the task list, not a follow-up.

This applies however the `tasks.md` came to be:

- `/opsx:propose` or `openspec-propose`, which generate it as one of the change's artifacts;
- `/opsx:update` or `openspec-update-change`, which revise it;
- `/opsx:apply`, or any other skill or command that rewrites it mid-implementation;
- a hand edit, including a one-line change to a single task.

## Where the plan goes

```
openspec/changes/<change-name>/
    proposal.md
    design.md
    specs/<capability-path>/spec.md
    tasks.md
    plans.md      <- here, beside the tasks it plans
```

This location **overrides** the writing-plans skill's own default of
`docs/superpowers/plans/YYYY-MM-DD-<feature>.md`. A plan belongs with the change it
implements, travels with it into `openspec/changes/archive/`, and is read alongside the
proposal, the design and the spec delta.

## What the plan must contain

Everything the skill requires, with nothing softened:

- its required header, including the agentic-workers line, and a **Spec:** field pointing at
  the change directory — the plan argues from the proposal, the design and the spec delta;
- a **Global Constraints** section carrying this project's rules with exact values: the Go
  toolchain pin, the module import limits, `make all` (and `make store-matrix` where a store
  changes), the `table-test`, `use-mockgen` and `use-testcontainers` skills, and the rules in
  this directory;
- a **File Structure** section naming every file created or modified and its responsibility;
- an **Interfaces** block per task with exact signatures, because an executor sees only their
  own task and learns neighbouring names from nowhere else;
- single-action steps under `- [ ]`, in red → green → commit order;
- **no placeholders** — real code, real commands, real expected failure output.

## Keeping the two in step

`tasks.md` says *what*; `plans.md` says *how*, in enough detail to execute without the
surrounding conversation. They are written and revised together:

- Every task in `tasks.md` is covered by the plan. A task with no plan step is a gap, and the
  skill's Self-Review is where it gets caught.
- Changing `tasks.md` later means updating `plans.md` in the same turn. **A stale plan is
  worse than no plan**: it reads as authoritative while describing work that is no longer
  wanted.
- Where the two disagree, `tasks.md` and the spec win, and the plan is corrected to match.

## How it meets the other rules

- `.claude/rules/prove-errors-with-tests.md`: a plan that fixes a defect opens with the test
  that proves it, and the step that runs it and watches it fail.
- Where a plan rests on an unproven claim — a performance problem, an assumption about the
  code's shape — it carries an explicit **STOP** step: if the first measurement or check does
  not reproduce the problem, the work does not proceed.
- `.claude/rules/golang-tdd.md`: the plan's step order *is* red → green → refactor, written
  out concretely for someone who has never seen this codebase.
- `.claude/rules/library-design.md`: a plan introducing or changing behaviour shows the
  default, the option that overrides it, and a test for each.

## Why

A task list is a reminder for whoever wrote it. A plan is instructions for someone who was
not in the room: which files, which signatures, which command, what failure to expect before
the fix. Work gets handed to a fresh session, a subagent or another person far more often
than the author expects, and the difference between the two documents is whether that handoff
costs minutes or an afternoon of rediscovery.
