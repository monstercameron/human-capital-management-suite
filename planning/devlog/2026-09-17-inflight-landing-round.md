# In-flight landing round, 2026-09-17

Three more commits preserve lane work for still-open todos without ticking
any of them: `30f70cc4` (SEARCH-002, BEN-003, CLOCK-003, GARN-001, METRIC-001,
PERSIST-JOBARCH-002, KNOW-003/004, SETTLE-004), `db515efa` (ABUSE-006,
ANON-006/008) and `67424d83` (UIPOLISH-002/003/005/006, UXSCAN-008). Each went
through the full hook; no todo was marked complete here.

## Verification

- Every landed package passes `go test -count=1` in-tree, including the
  first-ever run of `internal/domains/pseudonym`. No files were touched after
  21:02 while the round ran, so no active lane writer was disturbed.
- The first attempt at the UI commit was refused by the hook on a real signal:
  `TestTodo_PROMOUX_011_ProductRefreshPublishesOnlyNewestSequence` failed once
  in `tools/uxqual/productclient`, then passed in isolation and in two
  consecutive full-package runs, and the recommit passed the hook clean. That
  is a flake (order- or load-dependent), not a regression from this round —
  nothing in these commits touches that test's code. It needs a quarantine
  record with owner and expiry per GOV-020, owned by the UX lane, not silent
  retries.

## Still held (hook refuses or discipline forbids)

- The red legal area: `internal/governance/legal`, `extract`, the 19 state
  packs and `intentmanifests/channel_parity` fail their own tests, so no hook
  will take them without `--no-verify`, which is never used. Landing them
  means fixing first: the pack/matrix/golden reconciliation plus the
  hidden-children mutant gap.
- `.claude/settings.json`: still modified, still unexplained (it drops the
  `git push` denial), still unstaged. Needs its owner's eyes.
