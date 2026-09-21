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
(which the local hook does not run) first flagged seven findings in
ticked todos' test files, all pre-existing HEAD content my branch did
not touch -- an S1016 struct literal in `data/outbox`, four dead
helpers (`appt003Request`, `readGolden`, `streamIDs`, `selectionCite`,
each defined once and never called), and two same-expression `!=`
determinism assertions (`align063`, `secarch010`). Fixed on the branch
without weakening any assertion and the seven suites pass -- but the
re-run surfaced fourteen more pre-existing U1000/S1011/SA4006 findings
in production files (my first log grep had filtered them out by
matching only `_test.go` lines): dead `valid()` methods on
`ReservationState` and `OfferStatus`, three unused schema-version
consts, an unused `version` const in `clock/offline.go`, an S1011 loop
in `custom/versioning.go`, dead route/address setters in `productui`
shell and `productclient` (orphaned by the UXLIVE-007 profile
refactor), a dead `requireAnyPageView` helper, and an unread derived
context in `workflowcontrol` Simulate. All removed or mechanically
fixed (append form, blank context) with zero callers each; every
affected suite passes and `go run ./tools/quality` is clean locally,
which replicates the CI gate exactly.

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

## Follow-up, 2026-09-19 (journey notes, provider integrations, logging)

Committed on the local branch `operations/promo-exec-provider-integrations`
in area groups. Nothing was pushed. No todo was ticked in this round; the
work below has no registry rows of its own yet.

What landed:

- Journey notes: the engine side of free-standing, append-only notes on a
  promotion journey (`internal/intent/app/journey_notes.go`; the table and
  transport spine landed in the earlier checkpoint), idempotent by key, with
  note focus handling in the WASM client. Verified live in the served UI.
- Two third-party provider simulators run as plain processes
  (`tools/integrationsim/cmd/payrollsim`, API-key auth; `.../iamsim`, OAuth2
  client-credentials token then bearer API), with signed result callbacks,
  reversal, key rotation and scenario control. They started under `cmd/`; the
  decomposition gate counts every `cmd/` directory as a production process
  needing a reviewed decision, so they moved under `tools/`, which is what
  they are, and the two layout waivers were dropped.
- Provider delivery pieces: stdlib OAuth2 client-credentials client
  (`oauthcc`), full-jitter exponential backoff with Retry-After as a floor
  and a circuit breaker (`retrypolicy`), payroll/access delivery
  (`providerdelivery`), receipt verification (`providerreceipt`) and
  provider-call telemetry with header propagation (`providertelemetry`).
  The receipt store (`internal/data/providerreceipts`,
  `migrations/00313_integration_provider_receipt.sql`) and the outbox
  consumer's `FailAfter`/`Defer`/`Abandon` retry outcomes land with them.
- Promotion workflow 1.1.0 adds the payroll and access confirmation waits
  after the commit; 1.0 stays servable for pinned in-flight runs.
- Logging: one business event per journey operation (`journey.proposed`,
  `journey.workflow_started`, `journey.decision_recorded`, and so on), whose
  top-level `correlation_id` is the intent's; request ids now reach every log
  line even when no OTel exporter is configured.

Defects found by the live UI log run and fixed:

- The request-path execution engine built its log recorder only when an OTel
  provider was configured, so on local dev no engine line was written for
  starting a workflow or completing an approval. It now builds the recorder
  whenever a logger exists; spans still need the provider.
- Engine lines written during an approval carried the approval request's id
  as the top-level `correlation_id` and the run's id only in `attrs`. The
  run's id now wins; `request_id` still names the request. Unit-tested and
  seen live on a fresh run (start and finance approval lines join the
  proposal's correlation id).
- `TestTodo_WF_RUN_016_Integration` still expected the 1.0 graph (payroll
  failure ending straight in `REPAIR_REQUIRED`); it now confirms the payroll
  wait first, like its sibling test.

Verified: targeted suites for every touched package, including
`internal/application` (428s), `internal/platform/execution`,
`internal/intent/app`, `internal/platform/sandbox` and `test/workflow`;
the full hook on each commit. A live run for Caleb went from proposal
through both approvals, the effective-date timer and the commit to the
payroll confirmation wait with every step logged under one correlation id.

Left partial:

- The worker role that delivers the outbox rows to the simulators, and the
  receipt intake endpoints that record confirmations and resume the waits,
  are not wired. A live 1.1.0 run therefore parks on the payroll wait until
  its timeout.
- `tools/quality` has three failures outside this change (the Windows
  unlinkat exit in `TOOL_013`, the `oidckit` rootless-directory check, and
  `LIB_020`'s x/text ownership list naming productui and i18n packages).

## Follow-up, 2026-09-19 (UI/UX backlog)

Scope: every open UI/UX item, meaning `UXLIVE-027`..`UXLIVE-033` and the review-gap items against the UX sections (`REV-090-01/02`, `REV-091-01..03`, `REV-092-01`, `REV-093-01`, `REV-095-01..05`). Seven parallel lanes implemented them. Each change was then checked on a served build of the current tree with headless Chromium at 1280 and 390 px, in light and dark and in en-US, de-DE and ar, with interaction scripts for typing, scroll restoration, dialog validation and focus return (`.artifacts/uxcheck/`).

Defects the unit tests did not catch and the live pass did:

- The 9-versus-0 disagreement between Journeys and Insights came from the saved My Work tab being applied to every page's journey list, not from the summary maths.
- The WASM client started importing `internal/humanwork/workspace`. Its package-level stylesheet hashes then ran in the browser and wrote a runtime `<style>` through GWC's DOM sink, which CSP blocked about 12,500 times per load. The shared predicate moved to the leaf package `reasontext`, and a test now pins the WASM dependency boundary.
- The confirm dialog's slide-in keyframes end at `transform:none`, which cancelled its centring. The fields and actions sat below the fold at 1280×800.
- Unkeyed page-frame and title-block children re-mounted the heading when a page resolved and on sort. That dropped focus to `<body>` and reset scroll.
- A lane's `\2192` went through a shell heredoc and became a 0x11 control byte, which rendered as a box on phones.
- An in-place `buf generate` (another session) deleted `gen/go/hcmnext/model`. It was regenerated byte-identical with `modelgen`.

Not this work, and left to the owning sessions:

- `test/bootstrap/epwork001_test.go` no longer compiles against the new `WritePorts` constructor parameter.
- RBAC-RT's fail-closed `requireFeatureAction` makes two `journeyclient` integration tests (`PROMOUX_007_Integration`, `REV_091_01_Integration`) run without a role-access store and get denied.

## Follow-up, 2026-09-19 (workflow designer Promotion parity)

`WF-UI-005` is complete. The registry-backed palette is tenant-filtered before projection and presents authorized blocks, fragments and templates in searchable domain groups with effect and reversal metadata. Fragment insertion persists one collapsible group, and optimistic revisions prevent lost edits.

The Promotion authoring template now uses the exact canonical `promotionexec.Definition()` rather than a hand-maintained facsimile. Its durable document bytes, start node, 25 nodes, 72 routed outcomes, semantic version successor and payroll/access signal sources are covered by tests. The transport projects both the draft definition digest and the admitted template digest; the client validates those values and only then labels the draft an exact executable-template match.

The focused `WF-UI-005` suites reported `ok` across the palette, edit service, application composition, workflow and edge transports, product UI, product client and Go/WASM controller. The Windows toolchain subsequently refused to unlink some completed test executables, which is the repository's documented cleanup-only condition. In the Codex browser, the real Go/WASM editor showed the 25-step/72-route Promotion graph and equivalent outline; searching for `Promotion` reduced the palette to the governed template and review fragment, and expanding the fragment exposed its grouped insertion action plus `Pure`/`No Effect` badges.

## Follow-up, 2026-09-19 (immutable successor parity and typed refinement)

The live immutable-version test found that `Create newer version` established the correct SemVer and base digest but initialized an empty draft. The fix does not trust a workflow ID or name: server-only palette metadata binds the canonical Promotion source template to the exact compiled-plan digest it produces, and successor creation hydrates that template only when the selected publication carries the same digest. The transport regression test compares the stored successor bytes with `promotionexec.Definition()` byte-for-byte, recompiles those stored bytes, and proves the resulting plan digest is the publication's digest.

The real Go/WASM product was restarted against a fresh migrated PostgreSQL database and tested through the Codex browser. From the read-only published 1.1.0 view, creating version 1.1.2 opened a revision-1 draft with 25 steps, 72 routed outcomes, both provider-confirmation waits and `Exact match to executable template`. The earlier empty-draft behavior no longer reproduced.

`WF-UI-006` is also complete. Typed signal edits, invalid-value rejection, optimistic revision changes, reason-gated omit/replace overlays, mandatory-phase locks and author-before-mutation checks are covered in the edit, transport, product, client and WASM suites. Live testing changed the payroll acknowledgement label and timeout, observed the exact-match badge become changed, restored the canonical values and exact status, verified that a manager-approval control stayed locked even after entering a reason, and confirmed URL-backed node selection across reload. Desktop, 390px and 320px visual passes kept the inspector usable without clipped controls.

Verification: the scoped `WF-UI-005`/`WF-UI-006` package matrix reported `ok` for `designeredit`, `designerpalette`, `application`, workflow transport, product UI, product client and the Go/WASM controller. `go vet` for those packages, `buf lint` and `buf build` all passed. One combined test command returned nonzero only after every package reported `ok`, when Windows refused to unlink a completed test executable; the focused successor regression then passed cleanly on its own.

## Follow-up, 2026-09-19 (compiler-owned outcome and data links)

`WF-UI-007` is complete. A node's outcome ports and input bindings are now distinct authoring surfaces backed by distinct draft commands. Outcome routes come from the definition and update under the same optimistic fence as other edits. Binding choices come from a workflow-core authoring query that reuses graph analysis, strict dominators and value-type assignability, so the browser cannot offer a source the compiler would reject and the edit kernel independently enforces the same rule.

The contract crosses the full production path: protobuf and generated bindings, role-gated Connect handlers, edge procedure registration, product client validation, serialized Go/WASM mutations and accessible native forms. The reusable labeled-control component now exposes its class hook; the inspector stacks labels and controls cleanly and uses only governed radius and warning tokens. Focused core, edit, transport, product, client and controller tests, `go vet`, `buf lint` and `buf build` all pass. The broader package sweep surfaced only existing failures outside this change: the `WF-UI-004` transport golden, fail-closed role-visibility matrices and two journey-client denial expectations.

The live product check used the canonical Promotion successor (version 1.1.2). It changed `Raise Threshold / ABOVE_THRESHOLD` from Finance to Manager and back, then changed `Band Position` from `Evaluate Band.band_position` to the compatible simulation output and back. The final durable draft is revision 5, again reports 25 steps, 72 routed outcomes and `Exact match to executable template`. A keyboard-only no-op submit preserved that revision. Desktop, 390px and 320px inspection plus temporary light-mode and restored system/dark-mode checks found no clipped controls after the responsive refinement.

## Follow-up, 2026-09-19 (accessible outline authoring parity)

`WF-UI-009` is complete. Both the desktop graph and semantic outline now emit the same configure and move commands against one server-owned draft. Moving changes presentation order only; execution edges remain authoritative. The new command is revision-fenced and role-gated across protobuf, Connect transport, cell/edge registration, product client and the serialized Go/WASM authoring controller. Exact named primary, browser and property tests cover stale and invalid commands, boundary no-ops, round-trip digest/order restoration, native keyboard controls, localized copy, theme tokens and the 320px layout.

The live Codex-browser pass found and fixed two defects that an in-memory test had hidden. PostgreSQL jsonb normalizes the stored document, so byte comparison made the first otherwise-identical edit invent a revision; the edit kernel now compares the canonical definition identity and the regression compacts the fixture before submitting it. An identical reload also left the imperatively written `Saving workflow changes…` text mounted because the virtual tree did not change; successful workflow-loader completion now clears the live region and authoring fence. Parameter, outcome and binding no-op submissions all remain at revision 6, clear `aria-busy`, and preserve the canonical Promotion's 25 steps, 72 routes and exact-template badge.

Keyboard-only testing created a separate 0.1.0 draft, inserted `Task`, advanced it from revision 1 to 2 and selected the same inserted node in the graph, outline and inspector. Desktop inspection kept the existing product pattern; at 320×700 the graph yields to the semantic outline, move controls stack below labels, long names remain intact, and logical properties preserve RTL behavior. The visible single-node summary was also corrected from `1 steps` to localized singular copy. The focused eight-package matrix reported `ok` throughout, `go vet` passed, and both `buf lint` and `buf build` were clean; the aggregate Go command ended nonzero only when Windows refused to unlink an already-passed executable, the documented cleanup-only condition.

## Follow-up, 2026-09-19/20 (UI/UX backlog, front-end performance, company data and the dev sign-in page)

Three rounds ran in this session, coordinated from one integrator session with parallel Opus lanes. Nothing from any of the three is committed: HEAD is still 36043e8e, the seven prepared commit groups are blocked, and the reason is recorded at the end of this entry.

### Round 1 - the open UI/UX backlog (19 todos)

`UXLIVE-027`..`UXLIVE-033`, `REV-090-01/02`, `REV-091-01..03`, `REV-092-01`, `REV-093-01` and `REV-095-01..05` are implemented, verified live and ticked with evidence in `planning/todos.md`. Four lanes built them; every change was then checked on a served build at 1280 and 390 px, light and dark, in en-US, de-DE and ar, with interaction scripts for typing, scroll restoration, dialog validation and focus return.

What the live pass caught that the unit tests had not:

- The Journeys-versus-Insights disagreement (nine requests against zero) came from the saved My Work tab narrowing the journey list on every page, not from the summary arithmetic.
- The WASM client began importing `internal/humanwork/workspace`; its package-level stylesheet hashes then ran in the browser and wrote a runtime style element that CSP blocked about 12,500 times per page load. The shared predicate moved to the leaf package `internal/humanwork/reasontext`, and `TestWasmClientDoesNotLinkServerPackages` now pins the boundary.
- The edit-proposal confirm dialog rendered off screen at 1280x800 because its slide-in keyframes end at `transform: none`, which cancelled the centring; its fields and both buttons were unreachable.
- Unkeyed page-frame and title-block children re-mounted the heading on resolve and on sort, dropping focus to body and resetting scroll.
- A CSS escape written through a shell heredoc became a 0x11 control byte and rendered as a box glyph on phones.

### Round 2 - front-end performance, measured before and after

Baseline on the served build (median of three runs, headless Chromium): cold load to settled content 4.8-6.0 s, WASM 8.7 MB compressed and 43.5 MB decoded, page switches 0.5-1.7 s, worst search keystroke 456 ms, People sort 1392 ms, cold-load total blocking time 1353 ms.

CPU profiling (Chrome DevTools Protocol, self and inclusive time by Go function) found one dominant cause: on every header render the action launcher built an entry for all 64 directory rows, and for each entry `actionLauncherItemPolicy` rebuilt that entire list again to find the matching row. The work grew with the square of the population, and while typing in global search it accounted for 2.5 s of 3.4 s. `authorizedActionLauncherItems` now builds the canonical list once per call and indexes it by ID.

After that plus three smaller fixes (memoized per-locale catalog coverage, a fast path in `canonicalLocale`, fast paths in `statefulHref` and `authorizedFavoritePages`): worst search keystroke 140 ms, People sort 391 ms, cold-load blocking time 406 ms, dialog open 231 ms. `personActionLauncherItems` went from 2580 ms to 78 ms in the search profile.

Also fixed: the local rebuild script omitted the `-ldflags=-s -w` the official builder uses (`tools/uxqual/cmd/journeywasm/main_native.go:95`), so every asset rebuilt during the session carried debug information (43.8 MB raw versus 42.1 MB stripped).

Still open on performance: the 42 MB WASM bundle itself, which is what makes a cold load slow. Two data segments account for about 10 MB; `twiggy top` on the unstripped binary is the tool that shows them, and the investigation stopped there. The catalog-coverage memo first used a package-level `sync.Map`, which the composition-root gate correctly rejects as shared mutable state; it is now an immutable map of per-locale `sync.OnceValue` closures.

### Round 3 - company-wide data and the dev sign-in page

Four lanes plus two follow-up rounds. What the demo tenant now carries, verified by querying a database seeded from scratch:

| Data                        | Before                                  | After                                                                       |
| --------------------------- | --------------------------------------- | --------------------------------------------------------------------------- |
| Employment facts per person | seven fields hard-coded empty in the UI | all seven stored and rendered                                               |
| Pay bands in the database   | none (188 specs in a Go map)            | 220 rows, and the served catalog reads them                                 |
| Promotion targets           | one per job, computed in Go             | 143 edges in `promotion_path_edge`, every non-executive job has two or more |
| Job architecture            | never written                           | 19 families, 44 levels, 44 grades, 55 profiles                              |
| Performance ratings         | none                                    | 10 cycles, 360 reviews, 30 calibration sessions, 120 final ratings          |
| Payroll                     | none                                    | 12 runs across three periods with frozen populations                        |
| Per-worker role assignments | none                                    | 60 role sets, 99 assignments                                                |
| Job family and FLSA         | empty and EXEMPT for all 47 jobs        | 16 families, FLSA drawn on duties                                           |
| Open vacancies              | 162, many in unrelated units            | 44, one per published target, each in its job's home unit                   |

`REV-096-01` is ticked: a `performancestore` adapter resolves a subject's most recently finalized closed cycle into a validated `FinalCalibratedRating`, bound in the serve composition, so the high-performer plan variant is reachable in a served cell for the first time.

The dev sign-in page now renders the whole organization: all 60 employees are signable with a role bundle derived from their job, with an employee search that also matches role labels, a role filter, and a server-rendered collapsible org tree. The page has no JavaScript (its CSP is `default-src 'none'` with one style hash), so the tree uses native `details` disclosure and plain GET forms.

Defects found by driving the product rather than reading lane reports:

- Every promotion priced against a database band was refused: the new band reader published bounds at the column's scale-4 precision and the evaluator refuses a scale-2 amount in a scale-4 band. All 54 demo paths failed; corpus paths masked it by falling back to the in-memory catalog.
- Promotion targets were implausible - a Senior Product Designer was offered Director of Clinical Operations and Director of Quality and Safety. Cross-function edges were ranked by pay gap alone; they now require same family, same unit, a declared adjacent discipline in the same division, or a division vice-presidency from M4 upward.
- Vacancies opened in the unit of every job that pointed at them, so Director of Product seats existed inside Engineering Platform; 44 of 103 scopes were wrong. A seat now follows its job's home unit.
- The seed cycled a unit's role list, so any unit with more people than roles re-issued the leader's title to a later hire who then reported to the original holder; nine units were affected.
- A fresh local-dev database cannot run any workflow until `hcmnext workflow-version bootstrap-dev` is run by hand: the engine refuses with `VERSION_NOT_ACTIVE` and the UI shows only "Service temporarily unavailable", which retrying can never fix.

### Where this stops, and what is left

Two lanes were stopped mid-task on request. Neither left broken code: `go vet` is clean across `internal/application`, `internal/intent/app`, `internal/data/...`, `internal/humanwork/...` and `tools/uxqual/...`, and no probe files remain.

- **Workforce read performance and the phantom corpus.** `ListWorkers` resolves each worker's placement with a per-worker `committedfacts.CurrentPlacement` call and validates each vacancy in its own transaction; `TestTodo_PROMOUX_015_Performance` measures p95 2.45 s against a 750 ms budget. The lane had begun batching both and had reached the assignee-name resolver in `journey_work_summary.go`. The same task carries the second defect: `ListWorkers` unconditionally appends the release's fixed four-worker corpus to the tenant's own population, so the seeded demo shows 64 people - the 60 real employees plus four who exist in no table. The corpus should be a fallback for cells with no durable population, not an addition.
- **Workflow activation on a fresh database** (make the local-dev serve perform the same approve-and-activate bootstrap the subcommand performs, idempotently, gated exactly like the workforce seed), **an honest refusal for `VERSION_NOT_ACTIVE`**, and the **`worker_access_role_set` collision**: the seeder writes a version-1 role set for every planned worker, and `TestTodo_WF_RUN_034_Security` then calls `SaveAssignment` with no expected version and fails with `roleaccess: stale version`.

Smaller open items: a Senior Registered Nurse's only steps are two directorships (there is no charge-nurse rung); `LEG-CMP3 -> PPL-HRBP4` ranks ahead of `PPL-DIR` because it is one grade step rather than four; and `REV-027-01` is only partly satisfied - the demo seed is a real call site for `payrollstore` and `performancestore`, but about 37 other stores remain importer-less.

### Not this work, and blocking the commits

The tree cannot pass the pre-commit hook while three other sessions' in-flight changes are red:

- The RBAC fail-closed change (`requireFeatureAction` denies when permission data is missing) breaks roughly 170 existing tests across `internal/humanwork/productui`, `internal/transport/journey` and `internal/humanwork/workspace`. It also breaks self-service in the running product: `Myself` fetches the signed-in person's own record through the directory RPC, which is gated on People access, so three of the four quick-pick personas cannot see their own profile and a finance approver's work queue is empty. The durable bundle and the credential roles are identical for those personas, so the seeded role data is not the cause.
- The workflow-designer session's edits to `internal/humanwork/productui` and `internal/transport/workflow` leave those packages intermittently uncompilable, and `buf generate` (which has `clean: true` over `gen/go`) twice deleted `gen/go/hcmnext/model/model_generated.go`; it was regenerated byte-identically with `modelgen` both times.
- EP-WORK added a `WritePorts` parameter to the cell constructors without updating `test/bootstrap`, which fails the whole-tree `go vet` the hook runs first.

Seven commit groups are prepared (`data`, `connectivity`, `workflow`, `api`, `domain`, `ui`, `docs`) with messages written, excluding 104 files belonging to other sessions. `planning/todos.md` carries 20 ticks with evidence, the registry is regenerated, `definitions/storage/storage-disposition.yaml` has the `promotion_path_edge` row and its source range corrected to 00317, the generated storage manifest and archdoc are regenerated, and `planning/test_coverage_root.md` has rows for every new file.

### Environment notes for whoever picks this up

- Verification ran against a scratch database `hcm_next_ux` on the shared dev PostgreSQL, created and dropped with `.artifacts/uxcheck/mkdb.exe`, so the seed could be rebuilt from scratch without disturbing the other sessions' `hcm_next`. `%LOCALAPPDATA%/hcm-next/uxcheck-serve.ps1` points at it with `-migrate=true`; the preview entry is `hcm-uxcheck` on port 8096.
- The seeders verify existing rows rather than overwrite them, and `journey_worker` is append-only, so any change to the plan requires recreating the database rather than re-running the seed.
- `.artifacts/uxcheck/` holds the verification tooling (`rebuild.sh` with a build lock and the stripped-binary flags, `shots.mjs`, `probe.mjs`, `propose.mjs`, `runjourney.mjs`, `loginshot.mjs`, `q.exe`, `mkdb.exe`). It was deleted once mid-session by an over-broad temp sweep and recreated; sweeps should stay inside `.artifacts/tmp`.
- Two resource traps recurred: orphaned embedded PostgreSQL instances (44 at one point, which is what makes the latency gates fail) and the Go build cache reaching 96 GB. Trimming cache entries untouched for 12 hours freed 57 GB.

## Follow-up, 2026-09-20 (PR #55 root-gate reconciliation)

The provider-integration topic branch was brought back through the current root gates rather than merged on the strength of its earlier partial checks. Six implementation commits now separate static-analysis cleanup, concurrent error-path hardening, planning and architecture reconciliation, controlled data-plane deletion, portable CI verification and deferred-schema isolation.

The failures fixed during the final CI pass were real and independent:

- deferred leave preview tables reused the live `leave_request` and `leave_record` names, so the generator's preview and disposition checks collided with the migrated schema; the preview relations now carry `_preview` names and a newly pinned digest;
- the external `NEXT-004` process smoke inherited the production execution-authority defaults and supplied a fictional Position-domain identifier to a corpus-only cell; it now opts out of execution and scheduling explicitly, sends the certified position-less corpus vector, deterministically marshals its payload, and passes Create, retry, Get and repeated Simulate;
- clean-checkout's fake `git` and `go` tools were Windows-only `.cmd` scripts, so Linux CI invoked the real tools from an empty fixture repository; the test helpers now emit native batch or POSIX executables;
- UX latency assertions were running inside the Go race detector, where instrumentation invalidated their wall-clock budgets; the default coverage sweep still measures those budgets while the race sweep retains the functional, recovery, security and fault tests;
- the race job started hundreds of PostgreSQL-backed packages at default package fan-out, converting resource contention into ten-minute package timeouts; the job now uses two-package concurrency and a 30-minute per-package ceiling inside its unchanged 120-minute outer bound.

Focused suites for deferred schema/model, clean checkout, invalidation, race policy and the external serve process pass. Both resulting commits also completed the repository's full pre-commit chain: formatting, TypeScript checks, ESLint, root and legacy Go formatting/vet, coverage, race policy, decomposition, drift/API/substrate/engine gates, all Vitest suites, legacy Go tests and workspace builds.

The final merge pass exposed a CI topology problem rather than another product defect: every topic-branch update ran the full workflow once for `push` and again for `pull_request`, and each root job serialized the roughly 384-package race sweep before the full coverage sweep. The workflow now limits push verification to `main`, runs topic branches once through the pull-request event, and executes quality/policy, race and coverage as independent lanes with isolated PostgreSQL containers. A small `Go tests (root module)` fan-in retains the existing branch-protection check name and fails unless all three lanes succeed, so the wall-clock optimization does not weaken merge admission. Workflow structure tests pin the trigger, lane wiring, race and coverage commands, and required fan-in name.

The first parallel run then reached CICD-001 and exposed a stale command matrix: it required `cmd/admin`, which the architecture manifest does not approve, while omitting the existing approved `cmd/hcmctl` and `cmd/frontenddev` roots. The gate now mirrors the seven real `approved_commands.initial` entries and its golden/property tests pin that exact set.

The subsequent coverage lane exposed the same kind of policy/implementation mismatch. Covergate claimed to exclude generated code, but only filtered a top-level `gen/` directory and still scheduled `internal/generated/schemaflux`. The generated-only package has no hand-written tests and Go reports its zero coverage without an `ok` or `?` prefix, which the gate surfaced as a missing package result after an otherwise complete sweep. Directory filtering now excludes any `generated` path segment, with regression coverage in both staged-file and all-package discovery tests.
