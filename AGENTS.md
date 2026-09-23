# human-capital-management-suite Agent Instructions

## Scope

These instructions apply to the whole repository. `CLAUDE.md` at the root is a thin pointer to this file so the rules live in one place. The coding discipline is informed by the vendored Karpathy guidelines under `.claude/skills/karpathy-guidelines/`; where the two disagree, this file wins.

## Who decides what

| Question                                           | Authority                                                               |
| -------------------------------------------------- | ----------------------------------------------------------------------- |
| What gets built, in which phase, behind which gate | `planning/plan.md`, `planning/execution-plan.md`, `planning/specs/*`    |
| The unit of work and its proof                     | `planning/todos.md` (RED, GREEN, TEST and TEST MATRIX are the contract) |
| How the work is done                               | this file                                                               |
| What already exists                                | source, tests, migrations, `definitions/`                               |

Where they disagree about implemented behaviour, the source wins. Where they disagree about intent, the plan wins. Do not invent packages, tables or capabilities because the plan anticipates them.

## Tracked agent configuration

| Path                                  | What it is                                                                                                                                      |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `.claude/settings.json`               | Permissions. Gates and read-only git are pre-allowed; bypassing the hook, stashing, hard resets, amends, rebases and pushes to main are denied. |
| `.claude/agents/gate-runner.md`       | Runs the gates and returns a triaged failure list instead of raw logs.                                                                          |
| `.claude/skills/karpathy-guidelines/` | Vendored verbatim from `multica-ai/andrej-karpathy-skills` at `2c60614`, MIT by its own frontmatter.                                            |

## Quality gates

Four gates hold the project on track. All of them run in the pre-commit hook (`.husky/pre-commit`) and in CI (`.github/workflows/tests.yml`); a commit that fails one does not land.

| Gate           | Command                                                        | Rule                                                                                |
| -------------- | -------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Format         | `npm run format:check`, `npm run check:go`                     | Every tracked file is formatted. `gofmt -w` only files you created or edited.       |
| Lint           | `npm run lint`, `npm run check:code-style`, `go vet`           | No lint findings, no vet findings, house code-style rules hold.                     |
| Unit tests     | `npm run check:coverage:staged`, `npm run test`                | Every package holding a staged Go file passes `go test`; the vitest suites pass.    |
| Coverage floor | `npm run check:coverage:staged` (CI: `npm run check:coverage`) | Every gated package covers at least 70% of statements; no package is without tests. |

The coverage policy is `definitions/toolchain/coverage-gate.yaml`. A package below the floor either gets tests or an exception naming its exact path, kind, owner, reason and expiry; prefixes and wildcards are refused, and an expired exception waives nothing. The gate judges by `go test` result lines, so the Windows "unlinkat ... Access is denied" exit is not a failure.

The hook also runs typecheck and the drift, API, substrate-coverage, engine-coverage, race-policy and decomposition gates, the nested-module tests and the build. Never bypass it: no `--no-verify`, ever. If the hook is red because of another session's half-written file, wait for that file to compile; do not edit it.

Root `go test ./...` is not a gate on the development machine: every data package starts an embedded PostgreSQL and the full run takes hours. Test one package at a time with `go test -count=1 ./<pkg>/`; CI runs the whole module with the race detector.

## Task lifecycle

1. Read this file, then the todo entries you are closing in `planning/todos.md`, then every package their Refs name.
2. Inspect the source, tests, migrations and git state actually in scope. Another session may be working in the same checkout; `git status` is not all yours.
3. Run the narrowest relevant test before changing anything.
4. Implement the smallest sufficient change. No speculative abstraction, no adjacent cleanup.
5. Rerun the targeted package, then `gofmt -l`, `go vet ./<pkg>/` and `npm run check:coverage:staged`.
6. Inspect the final diff and `git status`. Nothing outside the change, no scratch directories, no credentials.
7. Report what you verified and what remains partial. A finished change under an unticked todo looks like work nobody started; a ticked todo without passing evidence is a lie. Neither is acceptable.

## Go engineering rules

- Module path is `github.com/monstercameron/human-capital-management-suite`. The typo `monstercamarin` recurs; check it.
- Every hand-written `.go` file has a test in its package that exercises it. Generated code (`gen/go/...`), `testdata/` fixtures and thin `cmd` wrappers whose every call is covered by their library are the only exclusions, and each is named in `planning/test_coverage_root.md`.
- A test must be able to fail: assert on outputs, errors (`errors.Is` against sentinels) and state after the call. A test that only calls a function, repeats its fixture or aliases another test is not a test.
- Test names come from the todo's TEST and TEST MATRIX fields (`TestTodo_<ID>`, `TestTodo_<ID>_<Kind>`). A matrix label must prove what it says: a Race test runs goroutines, an Integration test reaches a real store, a Golden test pins bytes.
- Kernel packages are pure: no database, no clock, no network, no new dependencies. Data packages use `internal/data/pgtest` (`pgtest.New(t)`, `TestMain` with `pgtest.RunMain`); if pgtest reports an archive or startup lock, wait sixty seconds and retry.
- Every tenant-scoped table has `tenant_isolation` RLS and every append-only table a `forbid_mutation` trigger; every new table gets a `definitions/storage/storage-disposition.yaml` row. plpgsql blocks in migrations need `-- +goose StatementBegin/End`.
- Transport is a thin generated boundary (ARCH-GO-023): handlers call ports, never business logic. Commands reach the capability registry through `internal/application`, never by importing `internal/capability`.
- Package-level mutable registries are refused by the composition-root rule; state lives on the value that owns it.
- Change `go.mod` or `go.sum` only when the assigned work requires a dependency change; include the reason and verification in that work's commit.

## Delivery loop

Any agent may take a todo from requirements through implementation, tests, documentation and a gated commit on `main`. Assign work by task scope and file conflicts, not by model or permanent role. Before committing, review the diff independently for authorization after a side effect, tenant-scoping gaps, aliased or assertion-free tests, forged or replayable inputs, and business logic in transport (`.claude/agents/integration-reviewer.md`).

**Treat every todo as atomic.** Each one goes through the whole loop before the next one is called done. A red gate at any stage returns to the earliest
stage that owns the defect; the defect is fixed at the source, never bypassed, never filed for later.

1. **Requirements.** The contract is `planning/plan.md`, `planning/execution-plan.md`, `planning/specs/*` and the todo's `planning/todos.md` entry. The TEST
   and TEST MATRIX fields are the acceptance criteria. A todo with no testable criteria is not ready; clarify it before implementation.
2. **Context.** Read every package the todo's Refs name, plus the source, tests, migrations and git state actually in scope. Run the narrowest relevant test
   before changing anything.
3. **Plan.** Write the smallest sufficient change down: the files owned, the tests to add or amend, the definitions rows and migration numbers needed. No
   speculative abstraction, no adjacent cleanup.
4. **Adversarial plan review.** Challenge the plan: wrong package, missing tenant scope, untestable criterion, raced assumption.
   Findings are fixed in the plan, not filed. A rejected plan returns to step 3.
5. **Build.** Implement the plan with accessibility and i18n built in, not bolted on: names, keyboard semantics, focus, reduced motion, form-error
   association, en-US / de-DE / RTL ar wherever the surface renders text or controls.
6. **Review loop.** Review the diff against the todo's contract and the rules in this file (`.claude/agents/integration-reviewer.md`):
   authorization before side effects, tenant scoping, assertion-free or aliased tests, forged or replayable inputs, business logic in transport. Findings are
   fixed, then refined: remove speculative abstraction, duplicated helpers and dead code; match the surrounding style. Repeat until clean.
7. **Test ladder.** Three rungs, in order, no skipping: unit (every hand-written file exercised in its package, matrix labels prove what they say, the package
   clears the 70% floor); component (the vitest suites, the `tools/uxqual` harnesses); end-to-end (the real runtime: `test/workflow`, `test/bootstrap`, the
   endpoint parity harness, the embedded-PostgreSQL integration test). A failure at any rung returns to step 5.
8. **Coverage gate.** `npm run check:coverage:staged`. A package below the floor gets tests, or an exception naming its exact path, kind, owner, reason and
   expiry; prefixes and wildcards are refused, and an expired exception waives nothing.
9. **Visual inspection when a surface exists.** Drive the observable surface in the browser pane (dev server, the affected page, the post-interaction state, a
   screenshot) before calling it done; do not ask the user to look. Desktop plus 390 px and 320 px widths, light and dark where themes exist, no overlap,
   truncation or console diagnostics. Record the widths, states and suites run.
10. **Commit gates.** Tick the todo with its evidence line, update `CHANGELOG.md` and the current `planning/devlog/` entry (the commits, the defects found,
    the decisions taken; what was verified and what was left partial, in plain prose), then give that todo its own focused commit through the full pre-commit hook: format,
    lint, typecheck, unit tests, coverage floor, drift, API, substrate and engine coverage, race policy, decomposition, nested-module tests and build.
11. **Shared-index check.** Only one agent stages or commits at a time. Before staging, inspect `git status` and the staged diff; stage explicit paths, verify the staged diff contains only this todo, and do not include another agent's edits. Coordinate if files or staged changes overlap.
12. **Report.** The commit lands on local `main` after review and the available gates pass. Record the exact local checks run and any CI-only checks that remain unverified; do not represent local verification as a green CI run. A PR is not required.

## One artifact root

`.artifacts/` is the only place in the checkout for disposable output, and it is ignored by git. Built binaries go to `.artifacts/bin/` (`scripts/build.sh`), task briefs, logs and reports to `.artifacts/lanes/` (`scripts/run-lane.sh`), coverage output to `.artifacts/coverage/`, Go temp directories and test binaries to `.artifacts/tmp/`, build caches to `.artifacts/gocache/`, and the embedded PostgreSQL binary cache to `.artifacts/pg/`. The pre-commit hook and task launcher export `GOTMPDIR`, `TMP`, `TEMP` and `HCMNEXT_TEST_PG_CACHE` to those paths; do the same for any command you run by hand that builds or tests. A scratch directory at the repository root is a bug: delete it and fix the command that made it. Never force-add anything under `.artifacts/`, and never point cleanup at anything broader than a child of it.

## Git discipline

- Work in the shared checkout on local `main`. Do not create feature branches or worktrees. Any agent may own and commit any part of its assigned work, including planning, definitions and migrations.
- Give each completed todo a focused commit whenever its code and tests can stand on their own. If two todos are inseparable, use one commit naming both IDs and explain the coupling in the devlog.
- Agents may edit concurrently, but must coordinate overlapping files, migration numbers and commits. Treat the Git index as shared: only one agent stages or commits at a time, stages explicit paths, and verifies the staged diff immediately before committing. Leave unrelated dirty or staged files untouched.
- A PR is not required. Publishing local `main` to a remote is a separate operation; do it only when the user asks and after checking the remote state. Never force-push or rewrite history.
- Never `git stash`, `git reset --hard`, `git commit --amend` or `git rebase`. Never `--no-verify`.
- Attribute commits to the actual contributor; do not add a fixed co-author trailer for an agent that did not contribute.
- Update `CHANGELOG.md` and the current `planning/devlog/` entry with the commits, the defects found and the decisions taken. Devlog updates say what was verified and what was left partial, in plain prose.

## Documents

Do not create a new Markdown file unless the user explicitly asked for that file. Existing planning documents may be edited when a todo or the user requires it. `planning/todos.md` ticks carry an evidence line naming the tests and the exact `go test` command in backticks.

## Environment notes

- Windows 11 on arm64, Go 1.26, no Docker, no race detector locally.
- Leaked scratch directories at the repo root (`.gotmp*`, `.codex-*`, `.tmp-*`, `tmp/`) trip `gofmt`; they are ignored by git and safe to delete.
- Stale embedded PostgreSQL servers and `go-build` temp directories accumulate under `%TEMP%`; sweep them when disk or CPU is short.
