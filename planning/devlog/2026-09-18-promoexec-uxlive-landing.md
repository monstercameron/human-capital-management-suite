# Landing round, 2026-09-18 (promotion execute, live UI findings, market-rate)

Committed the working tree in six area groups (engine acknowledgement and
signal groundwork, domain execute defaults, data fact packages, rewards
market-rate port, product UI findings, docs) on a local campaign branch.
Nothing was pushed. The 29 appended todos that were already ticked with
evidence (`PROMO-EXEC-001`, `PROMO-EXEC-006`, `UXLIVE-001`..`UXLIVE-026`,
`HIPERF-001`) kept their ticks after re-verification; the 9 appended todos
with no named tests in the tree (`PROMO-EXEC-002/003/004/005/007`,
`HIPERF-002/003/004/005`) stay open and their groundwork landed without
ticks.

## What landed

- Six commits, each through the full pre-commit hook with no `--no-verify`:
  engine (`AcknowledgeJourney` RPC, ack stage and next step, regenerated
  bindings, durable signal subscription with expiry and resume, scheduler
  and signal workloads, compensate hold, WF-COMP-007 fingerprint bindings,
  `test/workflow` and `test/tunnel` coverage), domain (`PROMO-EXEC-001`
  execute-by-default composition with docs, `PROMO-EXEC-006` vacancy
  auto-selection on every propose path), data (`positionfacts` directory,
  `evidencestore` transaction recording, new `budgetfacts`/`compfacts`
  packages), rewards (`HIPERF-001` port, stub table, HTTP skeleton),
  UI (all 26 `UXLIVE` findings across `productui`, `workspace` and the
  `uxqual` journey clients plus the rebuilt WASM bundle), docs
  (`planning/todos.md`, regenerated registry, CHANGELOG, this file).
- Verification before staging: `go build ./...` clean, then targeted
  `go test -count=1` per affected package on embedded PostgreSQL --
  journeyclient, productclient, render/journey, workspace (12.8s),
  transport/journey, otelmw, tunnel (247.2s), intent/app (112.2s), data
  signals (197.8s), positionfacts (153.4s), budgetfacts (128.0s),
  compfacts (112.2s), evidencestore (73.2s), rewards/fixtures/marketdata,
  execution (104.4s), promotionsteps, scheduler (119.3s), workflow,
  observe, runtime (307.9s), execute (401.5s), promotionexec, the
  `PROMO-EXEC-001`/SERVE application selections (36.3s), the
  `PROMO-009`/`WF-RUN-037`/promotion-full workflow selections (41.1s),
  productui (149.0s) and todoregistry -- plus `go vet ./...`, `gofmt`,
  the drift, API, decomposition, substrate, engine and race-policy gates
  and both code-style scripts, all green.

## Defects found and decisions taken

- The acknowledgement change was structurally complete on the server but
  had never reached the client vocabularies, failing four maintained
  totality contracts: `TestTodo_PROMOUX_012_Regression`,
  `TestJourneyStatusDimensionTable`, `TestTodo_WEB_034_Golden`
  (journeyclient) and
  `TestRecordedJourneyLeavesOpenWorkAndAllKnownStagesHaveLabels`
  (productclient). Completed the mapping instead of weakening the tests:
  `StagePresentation` names the ack stage ("Awaiting acknowledgement",
  warning tone, paralleling "Awaiting approval"), a new
  `NextStepAwaitAcknowledgement` code ("Await acknowledgement") crosses
  into the `nextStepCodes` table, the WEB-034 golden gains the
  `AcknowledgeJourney` descriptor line, `journeyStageKey` gains
  `journey.stage_awaiting_acknowledgement`, and that key plus
  `work.next_step.await_acknowledgement` were added to the product
  catalog in en-US, de-DE and ar. The pinned test-side tables (dimension
  table, golden vector) were extended as their own comments require when
  the enum grows. The `KnownWorkNextStep` mirror in `page_work.go` needed
  the new code too, which the `UXAUDIT-017` regression test caught.
- The same interface widening broke `go vet ./...` (and the `check:go`
  gate) on two `workspace.JourneyEngine` test fakes in `test/tunnel`
  (`stream_test.go`, `tool009_test.go`): added the `Acknowledge` stub in
  each fake's established delegate-to-`Inspect` style. The
  `var _ workspace.JourneyEngine` assertions are the durable check here;
  the tunnel suite passes unchanged otherwise.
- `PROMO-EXEC-006`'s TEST field named glob patterns (`` `TestPropose*` ``
  and friends) that match no real test, which failed
  `TestTodoRegistryMatchesMarkdown`. Rewrote it to the concrete served
  regression plus the intent-app suites, both verified green.
- The checked-in registry was 107 lines behind what the markdown
  generates, so the final registry state comes from
  `go run ./tools/planning/cmd/todoregistry` (additive only, verified by
  diff). The archdoc inventory was likewise regenerated for the new
  `marketdata`, `signals`, `budgetfacts` and `compfacts` packages, after
  which driftgate passes.
- One `gofmt` finding (`projector_test.go`) normalized with `gofmt -w`.
- The coverage gate's `internal/application` run failed the new
  `TestCompositionRootRejectsGlobalRegistrationAndHiddenDependencies` on
  committed code: `transport/conformance` built its localization catalog
  in `init()` into a package-level map. Refactored to a pure
  `buildCatalog()` constructor (`var catalog = buildCatalog()`, no
  `init`, never mutated afterwards) instead of allowlisting it; the
  conformance suite and the composition test pass unchanged otherwise.
- The coverage gate's `test/workflow` run failed
  `TestPromoUXRealServerPromotionContract`: the served inspect projection
  now renders the position the issued reference names (UXLIVE-003's
  settled `displayPositionID` behavior, green in `TestTodo_UXLIVE_003`),
  while the older contract asserted the raw reference token. Co-evolved
  the assertion to decode the seeded reference with the same
  `position.RevisionRef.Decode` the server uses and expect the named
  entity, keeping the test's real intent (no guessed codes). Verified
  with `go test -count=1 -run '^TestPromoUXRealServerPromotionContract$'
./test/workflow/` PASS (20.4s).

## CI red, then green

The PR's root-module suite failed only the quality gate: CI staticcheck
(which the local hook does not run) flagged seven findings in ticked
todos' test files, all pre-existing HEAD content my branch did not
touch -- an S1016 struct literal in `data/outbox`, four dead helpers
(`appt003Request`, `readGolden`, `streamIDs`, `selectionCite`, each
defined once and never called on any platform), and two
same-expression `!=` determinism assertions (`align063`,
`secarch010`). Fixed on the branch without weakening any assertion:
struct conversion, dead-helper removal (plus its orphaned imports),
and two-variable determinism comparisons. The seven suites pass.
`go run ./tools/quality` locally still lists fourteen further U1000s
that CI on Linux does not report; those functions are used under Linux
build constraints, so they were deliberately left alone.

## Left partial

- `PROMO-EXEC-002/003/004/005/007` and `HIPERF-002/003/004/005` have no
  `TestTodo_*` in the tree, so they stay unticked; the ack, resume,
  expiry, compensate-hold, evidence-atomicity, sim-parity, `budgetfacts`
  and `compfacts` groundwork toward them is committed as ordinary code
  with no todo claim. Whoever picks those todos up should name their
  tests after the existing `sim_parity`, `evidence_atomicity`,
  `signal_timeout_resume`, `wfrun_ack` and `signals_timeout` files where
  they fit.
- Two `productui` full-suite runs each failed a different gate
  (`TestEveryRenderedPageCarriesTheDarkModeContract` once,
  `TestInteractionLatencyGate` once) while both tests pass in isolation
  and a third full run passed clean in 149s. Both look like load flakes
  on this host (dev servers plus parallel embedded PostgreSQL), not
  tree defects, but they deserve a re-run if they ever fail twice in a
  row; the latency budget in particular is tight at p95 140-180ms
  against a 100ms budget under load.
- The dev PostgreSQL on 5432 had died with its temp-dir data directory
  earlier in the day, so the cell could not start. Rebuilt it fresh
  under `.artifacts/pg` (binaries in `dist/`, cluster in `data/`,
  same log file), applied all 311 migrations and seeded
  `harborcare-demo`. The old development rows are gone; this is a clean
  seed.
