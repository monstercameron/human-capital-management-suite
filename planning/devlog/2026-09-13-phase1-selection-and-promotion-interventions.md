# 2026-09-13 — Phase 1 selection chain, promotion interventions, and the UI audit series

## Session goal

The active goal, set by Cam, is these twelve todos:

`PROMOUX-015`, `WF-DISC-012`, `SLICE-016`, `DATA-022`, `CLOSE-002`,
`PHASE-001`, `SELECT-001`, `SELECT-002`, `THREAT-001`, `CUSTOMER-001`,
`COMMERCIAL-001`, `NEXT-002`.

Dependency order matters here and was checked rather than assumed. `PHASE-001`
is the root: it alone unblocks `SELECT-001`, `SELECT-002`, `THREAT-001`,
`CUSTOMER-001`, `COMMERCIAL-001` and `NEXT-002`. `CLOSE-002` waits on the
SELECT/THREAT/CUSTOMER/COMMERCIAL set plus `SLICE-014`. `SLICE-016` waits on
`SOURCE-001`, `SLICE-013` and `SLICE-015`, and `DATA-022` waits on
`SLICE-016`. `PROMOUX-015` waits on `PROMOUX-012` and `PROMOUX-013`, and
`PROMOUX-012` in turn waits on `UXAUDIT-017`.

### Closed this session, against that goal

- `PHASE-001` — Phase 1 scope ceiling and its four empty selection slots.
- `SELECT-001` — pilot jurisdiction, California.
- `SELECT-002` — pilot provider topology, as an enforced placeholder.
- `WF-DISC-012` — workflow maturity gate.
- `PROMOUX-013` — governed edit, withdraw and cancel paths.

### Closed this session, outside that goal

`UXAUDIT-008`, `UXAUDIT-024`, `PROMOUX-008`, `PROMOUX-009`, `PROMOUX-010`,
`PROMOUX-014`, `UIPOLISH-004`. `UXAUDIT-017` landed partially: five of its
seven items are committed, the remaining two need journey-summary fields the
wire does not carry.

### Still open in the goal

`THREAT-001`, `CUSTOMER-001`, `COMMERCIAL-001` are now unblocked and
unstarted. `NEXT-002` additionally needs `TOPOLOGY-001`. `PROMOUX-012` needs
`UXAUDIT-017`. `SLICE-016`, `DATA-022` and `CLOSE-002` remain blocked behind
todos outside this goal.

## The ordering hazard PHASE-001 exposed

`definitions/planning/gates/p1a-manifest.yaml` already existed — 912 lines,
signed 2026-09-10, carrying `todo_id: NEXT-002`, with a working compiler and
passing tests. The selection-bound release manifest had been published before
the scope ceiling it was supposed to have been selected _from_ was ever built.
That is precisely the circularity `PHASE-001`'s REFACTOR warns about:
"selection cannot depend on a supposedly final manifest that already contains
the selection."

Deriving the ceiling from that manifest would have been guaranteed to contain
the selection and would have proved nothing. The ceiling was therefore built
independently from `capability-coverage.yaml`, `product-slices.yaml`,
`endpoint-manifest.json`, the business-intent catalog, the endpoint contract
spec, the user-flow catalog and `execution-plan.md`'s depth matrix, with the
P1A manifest read only as a falsifiable check.

**That check passed.** Every P1A intent and capability, every P1B contract and
every generated endpoint appears in the independently built ceiling, so no
unauthorized scope entered the release. It is mutation-verified: deleting
`hcmnext.people.promote_worker/v1` makes the conformance test name it as
ABSENT.

## Two artifacts that are deliberately incomplete

Both record something nobody has actually decided, and both are structurally
incapable of pretending otherwise.

`SELECT-001`'s jurisdiction profile requires a qualified legal reviewer. No
such person is recorded anywhere in the repository, so rather than inventing a
name into a signed governance artifact the reviewer is a required field that
`Validate` rejects when empty. The checked-in file reports exactly one
violation, `reviewer.name: missing`. **A named reviewer is required before
that profile can be considered complete.**

`SELECT-002`'s provider topology requires a named provider with edition,
region, entitlements, quotas and data-processing terms. `WEDGE-001` selected a
problem — a Promotion/Compensation failure class — not a partner. Cam chose to
proceed with a named placeholder after being told that asserting those facts
about a real vendor inside a signed manifest would be fabricated evidence. It
landed as `vendor_id: legacyhcm-incumbent-PLACEHOLDER-UNVERIFIED` with the
suffix enforced by validation while the status is placeholder, and a display
name that says "not a real vendor" inline. `SatisfiesRealProviderSelectionGate`
is independent of `Validate`, so flipping `selection_status` alone still fails
— only a fully consistent swap of status, real vendor id and complete
confirmation passes. **A real vendor must replace it before pilot use.**

Note the deliberate asymmetry: SELECT-001's profile carries one violation
because its reviewer is genuinely missing; SELECT-002's carries none, because
it is a structurally honest _complete_ placeholder whose incompleteness is
expressed through a separate gate.

## Defects found by running code rather than reading it

`PROMOUX-013` surfaced three, each observed failing before the fix:

1. `lifecycle_endpoints.go`'s `SupersedeIntent` minted and durably appended
   the successor intent _before_ validating and applying the original's own
   transition, so a refused or raced supersede left an orphaned successor — a
   genuine partial write. Reordered so the original's compare-and-swap lands
   first.
2. `journey_decide.go`'s `Decide` and `Execute` checked no terminal
   `RequestState`, so a journey cancelled mid-flight could still be decided or
   executed.
3. `internal/data/promotionguard.Release` was dead code by its own doc
   comment, so a cancelled-then-reproposed journey for the same worker and
   effective date would have refused itself as a duplicate.

It also established that `SupersedeIntent` is structurally unusable for
journeys: `promote_worker` is simulate-only, its `SUPERSEDED` edges exist only
from `SIMULATED`/`SUBMITTED`/`APPROVED`, and a journey's intent never durably
leaves `DRAFT`. `EditProposal` composes `CancelIntent` + `Propose` instead,
which makes "edits invalidate material approvals" literally true.

`WF-DISC-012` found a different class of problem: `planning/workflows/catalog.md`
hand-asserts maturity across 268 `**Status**:` lines, and the new gate found
**30 disagreements** with generated evidence. Every one of the 14 accepted
BusinessIntent definitions carries a real unresolved-reference blocker, so each
`EXISTING` claim naming them is unsupported past `CATALOGUED` today.

## Infrastructure landmine: buf.gen.yaml clean:true

`buf.gen.yaml` declares `clean: true` over the whole `gen/go` tree, which also
holds `gen/go/hcmnext/model/model_generated.go` — a non-buf output produced by
`tools/gen/modelgen`. **Every `buf generate` silently deletes it.**

Reproduced directly: ran `buf generate`, watched the file disappear, restored
it byte-identical with `go run ./tools/gen/modelgen/cmd/modelgen`. This is the
true cause of a phantom `missing gen/go/hcmnext/model` build failure that was
initially misattributed to a concurrency race between lanes.

Needs a permanent fix — a combined regeneration script, or a separate output
root for the buf-owned subtree. Not patched here.

## Operational notes

Disk reached **0.56 GB free** mid-session and killed a full
`internal/application` run with "no space left on device". Clearing
`.artifacts/gocache` (13.3 GB) and roughly 400 orphaned `go-build*` temp dirs
recovered 30.8 GB. This is the second time in one day that low disk presented
as unrelated test failures rather than as a disk error; it is worth sweeping
periodically rather than waiting for it to bite.

Running several lanes concurrently while one regenerates `gen/` makes the
other lanes' gate results unreliable and the tree intermittently unbuildable.
Two lanes reported failures that were entirely collateral. Lanes no longer
regenerate `architecture.md` or `todo-registry.json`; those are regenerated
once at commit time instead.

## 2026-09-14 — Workflow engine completion, industry packs and engine telemetry

### Goal

Cam's goal for this stretch: complete every remaining workflow todo, then go
over the whole workflow engine and make sure OTEL spans and structured logging
cover everywhere viable to track status and issues.

### Commits

- `9f523218`, `aa241b86`, `def9ea18`, `8ddf83c7` — the observe seam, governed
  Pause/Resume/Cancel/RetryNode controls, the ALIGN/DATAOPS/ONBOARD
  prerequisites, and live JIT-backed Pause/Resume with the engine coverage test.
- `8a1088ae`, `717f6d1e` — PACK-003 experience binding.
- `1f3e5356` — Cancel under instance-scoped dual control and RetryNode with a
  rolled-back preflight simulation, both proven on the composed server.
- `ac9c002b` — PACK-004 publication gate, PACK-005 signed activation, PACK-006
  layered composition and PACK-007 Healthcare/Retail/Manufacturing conformance.
- `2464c45e` — the serve recorder threaded into gRPC controls and the journey's
  direct approval step; per-branch parallel spans; recorded evidence-write
  failures; a guard test against discarded context-bound failures.
- `65086a36` — execution-host instrumentation (step runs, threshold raises,
  work-item routing, timers, retry admission, terminal writes, authority
  resolution) and enforced coverage tests for the host and operator gateway.

### Defects found

- Governed controls arriving over gRPC produced no spans or logs: only the
  execution driver and the timer scheduler put a recorder in context, so every
  `observe.Begin` in the controller was a no-op in production. The journey's
  direct `stepsapproval.Complete` call had the same gap.
- A failed repair-evidence write was discarded with `_ =`, leaving no trace.
- Parallel branches ran without their own operation, so one failing branch was
  indistinguishable from its siblings.
- The gateway recorded a plan-resolution failure as `REPAIR_REQUIRED` before any
  effect ran; pre-commit failures now abort the pending journal entry.

### Decisions

- Dual control for Cancel is a JIT grant narrowed to the instance
  (`workflow_instance:<id>` field) whose approver differs from the requester. A
  broad grant carries no per-action second approval and is still denied.
- RetryNode's simulation evidence is a dry run of the exact retry in a
  transaction that always rolls back; an operator-held simulation is never
  replaced.
- Engine telemetry is enforced by test rather than by review: every exported
  context-taking function in `internal/workflow`, `internal/platform/execution`
  and `internal/intent/operator` opens an observe operation or carries a
  documented exemption, and stale exemptions fail.

### Verified and left partial

Verified: the workflowcontrol, operator, execute, parallel, observe, intent/app,
transport/workflow, platform/execution (with promotionsteps, promotionterminal,
scheduler) and industrypack suites, and `TestGovernedWorkflowControlsOnComposedServer`
(Cancel applies and leaves the instance `CANCELLED`; a live RetryNode clears the
simulation gate and is judged on node state). The committed registry showed 74
done, 4 retired and 0 open workflow/PACK todos.

Left partial: the operator journal is still in-memory, so control receipts do
not survive a restart. A live successful retry of a genuinely failed node on the
composed server is covered only by unit tests. PROGRAM-001 stays blocked behind
13 open balance, leave, eligibility and qualification todos. Another session
ticked the four retired `WF-STEP-*` items in its working copy; that tick is
committed with its planning changes, not re-litigated here.

### Grouped commit of concurrent session work

The working tree also held uncommitted work from concurrent sessions (Gate B
resilience and outage proofs, performance, governance, planning domains,
product theming). It was committed in feature groups from a clean worktree so
each group passed the full pre-commit gates on its own; planning ticks were
three-way merged against the ticks already on `main`.

### Second grouped commit: live-UI fixes, theming and role visibility

The product UI session's work had stopped compiling mid-edit during the first
grouped run; once it settled it was committed as three groups: the role
visibility evaluator, the product UI (theming and UXSCAN fixes together, since
`styles.go` and the theme helpers are shared and a split did not build), and
planning/docs. Before committing, the productui and uxqual suites were run:
`TestTodo_WEB_063_Golden` failed because UXSCAN-001 added accessible names to
the profile's Promotion and Internal transfer links. A render diff confirmed
those two `aria-label` attributes were the only change, and the golden was
re-pinned with that note. `TestTodo_PROMOUX_011_ProductRefreshPublishesOnlyNewestSequence`
failed once in a full-package run and passed alone and in four further package
runs; it is timing-sensitive and was not changed.

A process defect surfaced in this session and is recorded here: commits made
from `git worktree add` checkouts silently ran no pre-commit hooks, because
`core.hooksPath` is the gitignored `.husky/_`. Every session commit from
`9f523218` to `65086a36` bypassed the gates. The grouped commits since then copy
`.husky/_` into the worktree, verify hook output, and passed the full
`test:all` suite over a tree containing that earlier code; the changed-file
coverage gate did not re-run against those earlier files.
