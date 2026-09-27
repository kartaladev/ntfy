# Rule: How OpenSpec and superpowers divide the work

Two plugins cover this project's workflow, and they overlap in five places. **OpenSpec owns
what is being built and the durable record of it; superpowers owns how the work is carried
out.** Where both offer a step, the table below says which one runs.

## Phase ownership

| Phase | Use | Not |
| --- | --- | --- |
| Shaping an idea that will become a change | `/opsx:explore` | `superpowers:brainstorming`, unless the idea is not yet a change |
| Requirements and scope | `/opsx:propose`, `/opsx:update` | — |
| Implementation detail | `superpowers:writing-plans` → `plans.md` | — |
| Workspace isolation | `superpowers:using-git-worktrees` | working several changes in one tree |
| Progress and state | `/opsx:apply` | the plan's own checkboxes |
| Context hygiene on a long plan | `superpowers:subagent-driven-development` | one session grinding through 70 steps |
| The red-green loop | `.claude/rules/golang-tdd.md`, the `table-test` skill | `superpowers:test-driven-development` for mechanics |
| A test that fails unexpectedly | `superpowers:systematic-debugging` | guessing, then patching |
| Done-ness | `superpowers:verification-before-completion`, `make all` | "it looks right" |
| Capability specs | `/opsx:archive` | editing `openspec/specs/` by hand |

## Precedence where they overlap

1. **Plan location.** Beside `tasks.md`, never `docs/superpowers/plans/`. See
   `.claude/rules/plans-beside-tasks.md`.
2. **Progress.** `tasks.md` checkboxes are authoritative. A plan's boxes are working notes,
   and plans regroup tasks deliberately — each carries a mapping back to `tasks.md`.
3. **Testing mechanics.** This project's rules and skills win. The superpowers TDD skill
   supplies the discipline, not the assertion style.
4. **Ideation.** `/opsx:explore` when the output belongs in `openspec/`; brainstorming when
   there is nothing to write down yet.
5. **Review.** `/code-review` reads the actual diff and is the one that runs;
   `superpowers:receiving-code-review` governs how feedback is answered — verify before
   agreeing, and never perform agreement.

## The loop for one change

```
1.  worktree for the change                    superpowers:using-git-worktrees
2.  /opsx:apply <change>                       drives tasks.md, reads plans.md
3.  per task: red -> green -> commit           golang-tdd + prove-errors-with-tests
      a surprising failure ------------------> superpowers:systematic-debugging
4.  make all   (+ make store-matrix if a store changed)
5.  /code-review, then fix what it finds
6.  /opsx:archive <change>                     specs sync into openspec/specs/
7.  PR, merge, remove the worktree             superpowers:finishing-a-development-branch
```

**Step 6 runs before the pull request**, deliberately: one PR then carries the code, the
tests and the updated capability specs together, so `openspec/specs/` never lags `main`.

## Working several changes at once

One worktree per change, one agent per worktree
(`superpowers:dispatching-parallel-agents`). Independent changes then never interleave edits
or share a dirty tree.

Before starting a parallel batch, list the files more than one change touches and say who
edits them first — shared registration points such as `ntfytest`'s suite registration, the
docs tests that assert literal strings, and any DDL document are the usual ones. A textual
conflict is cheap to resolve when it is expected and expensive when it is discovered at
merge.

## Why this is written down

Both plugins are good, and both are opinionated about steps the other also covers. Without a
stated division, each session picks a different combination, `tasks.md` and `plans.md` drift
apart, specs are edited by hand, and changes land without their spec deltas. The cost is not
a broken build — it is a record that stops matching the code, which is the one thing this
workflow exists to prevent.
