# Human Capital Management Suite Convergence Plan

This is the dependency-ordered bridge from the current planning repository to a
paid, production-shaped ChangeOps pilot. It narrows sequencing; it does not
replace [plan.md](plan.md) or the [Phase 1 execution plan](execution-plan.md).

## Current planning authority (2026-10-22)

The source tree now includes a root Go module, production commands, schema migrations, and serving composition. Their presence does not establish production readiness. The owner has authorized the [small-business operations plan](small-business-platform-ideas.md) and its implementation backlog; [the execution-plan placement](execution-plan.md#recorded-placement-small-business-operations-2026-10-22) controls that track. This document continues to define the bounded ChangeOps P1A/P1B release; it does not veto the separately recorded expansion.

## Historical baseline at the original convergence-plan drafting

The following bullets are retained historical context, not a current inventory of the implementation or current scope authorization. Verify present capabilities against source, running paths, and acceptance evidence.

- The architectural backlog is broad, but the production implementation is not
  bootstrapped. There is no root Go module, generated RPC service, authoritative
  PostgreSQL migration tree, production command set, container image or deployable
  cell.
- The only Go implementation is the isolated `src/blocks/go` module. It is useful
  test evidence, not the product runtime.
- The BusinessIntent catalog is the fourteen `DRAFT_CONTRACT` definitions.
  The intake name list is non-normative vocabulary; catalog identity is closed
  and no source-recovery or reconciliation work exists.
- No paid design partner, provider edition, first jurisdiction, authority-by-field
  matrix, deployment target or customer responsibility matrix has been selected.
- Therefore the project is design-rich but is not product-ready or authorized to
  implement broad HCM scope.

## Two releases, never one blended Phase 1

### P1A — paid observation, preflight and simulation

P1A is the first executable product. It has eight executable intent contracts:

1. `hcmnext.people.explain_worker_state/v1`
2. `hcmnext.people.promote_worker/v1` in `DRAFT`, `PREFLIGHT` and `SIMULATE` modes only
3. `hcmnext.rewards.simulate_compensation/v1`
4. `hcmnext.rewards.evaluate_pay_band_position/v1`
5. `hcmnext.intelligence.explain_transaction/v1`
6. `hcmnext.operations.detect_drift/v1`
7. `hcmnext.operations.create_repair_plan/v1` as a recommendation only
8. `hcmnext.operations.simulate_repair/v1`

Its single compiled workflow is
`promotion.preflight-simulate-observe/v1`, using thin `CAPABILITY`, `TRANSFORM`,
`DECISION` (with a `rule_ref` to the tenant threshold table), `OBSERVE` and
`END` steps. Intents carry the five lifecycle dimensions; `ExecutionState`
never leaves `NOT_PLANNED` in P1A.

P1A may persist intent, snapshot, simulation, proposal, handoff,
observation, reconciliation, evidence and non-executable RepairPlan records. It
must persist zero worker, employment, assignment, organization, position,
compensation or budget mutations; zero reservations, WorkItems and timers; and
zero committed external effects, provider writes and MessageIntents.

### P1B — one bounded authority amendment

P1B exists only after a signed Gate A `PROCEED`. It adds six contracts:

1. `promote_worker` in `EXECUTE` mode
2. `change_base_pay`
3. `reserve_compensation_budget`
4. `release_compensation_budget`
5. `approve_proposal`
6. `reject_proposal`

It uses `promotion.execute/v1`, adding `APPROVAL`, `TASK`, `WAIT`, `SIGNAL`,
and `COMPENSATE`, with compiler-placed safe points. Outbound effects remain
sequential `OBSERVE` steps in this release; `PARALLEL`, `JOIN`, `SUBWORKFLOW`,
live migration and shadow execution are not P1B prerequisites. The build-or-adopt
decision for the durable runtime is recorded before P1B code starts.

Before implementation, the partner must select exactly one authority topology:

- **External authority:** Human Capital Management Suite records the approval-bound transaction and
  intended effect, the incumbent receives one governed mutation, and observed
  incumbent state remains `EXTERNAL_OBSERVATION`. Human Capital Management Suite must not emit false
  local domain facts for externally mastered fields.
- **Transferred authority:** the partner explicitly transfers the selected field
  authority, permitting Human Capital Management Suite to commit the corresponding domain facts.

Do not build both topologies speculatively.

## First ten working days

| Day | Outcome                                                | Required evidence                                                                                                                                           |
| --- | ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Freeze the P1A scope ceiling and exclusions.           | Candidate manifest with the eight intents, one workflow, exact effect ceiling and stop rules.                                                               |
| 2   | Start the partner evidence lane.                       | Named prospect/partner owner, paid-use hypothesis, incumbent failure and independent downstream-observation requirement.                                    |
| 3   | Freeze one repository and command manifest.            | Root package tree and initial commands: `hcmnext`, `worker`, `projector`, `migrate`; scheduler/admin explicitly sequenced later.                            |
| 4   | Bootstrap the root Go module and generation toolchain. | Pinned Go, Protobuf and grpc-go; edge and generator qualification fixtures run, with grpcbridge/SchemaFlux or their named fallbacks selected on the result. |
| 5   | Define the first typed wire contract.                  | `IntentService.Create`, `Get` and `Simulate` descriptors, generated Go bindings and descriptor digest.                                                      |
| 6   | Create the authoritative migration root.               | Tenant, intent, proposal, ledger event, stream head, projection and outbox migrations with checksums.                                                       |
| 7   | Prove one ACID chronology.                             | Intent/proposal plus ledger, critical projection and outbox append atomically; stale-head CAS fails exactly.                                                |
| 8   | Expose direct gRPC and grpcbridge parity.              | Same Protobuf vector, digest, authorization result and typed error through both paths.                                                                      |
| 9   | Run the no-effect Promotion fixture.                   | Snapshot status, before/after simulation, provenance, uncertainty and zero-effect receipt.                                                                  |
| 10  | Review evidence and choose.                            | `CONTINUE_DISCOVERY`, `NARROW`, `RESELECT` or `STOP`; no write authority can be granted.                                                                    |

If a paid partner and representative access are unavailable, days 2–10 continue
only as a technical feasibility spike. They cannot be reported as Gate A product
evidence.

## Dependency-ordered milestones

### M0 — commercial truth

Run `WEDGE-001` through `WEDGE-013`. There is no source-attestation, parser,
verifier, or reconciliation work: the catalog is the fourteen drafted
definitions and the intake list is vocabulary. M0 is a partner conversation,
not a tooling milestone.

Exit: paid non-duplicative problem evidence exists, or the program stops/reselects.

### M1 — concrete selections and bound P1A manifest

Select the first provider product/edition/API entitlement, independently observed
downstream boundary, hypothetical-to-reviewed legal scope (one jurisdiction set, one tenant review floor, and the obligation kinds from the fifty-state matrix that the partner's promotion flow consumes), customer RACI and
deployment target. Then sign the concrete P1A manifest and digest. The current
`PHASE-001` must act as a scope ceiling first; selection cannot logically depend on
a manifest that already requires the selection.

Exit: every P1A artifact is explicitly included, deferred or rejected; every
selected external fact has an owner, expiry and stop condition.

### M2 — production-shaped physical spine

Create one root Go module with semantic package boundaries, generated contracts,
four initial commands, explicit SQL via pgx, an authoritative Goose migration
tree, Testcontainers integration tests and Go-only authoritative CI. Split the
bootstrap `cmd/migrate` from mature expand/backfill/cutover rehearsals. Split a
runnable local/dev cell from a production-qualified HA/PITR cell.

Exit: `migrate -> seed -> serve -> create/get/simulate intent -> append ledger and
projection/outbox -> worker acknowledge -> restart/reconcile` passes in an
ephemeral environment.

### M3 — P1A Promotion vertical slice

Implement trusted ingress, action discovery, intent create/preflight, one selected
connector read path, mixed-source snapshot, no-effect simulation, immutable
proposal, consumable handoff, downstream observation, reconciliation and
non-executable repair recommendation. The provider adapter owns transport
mechanics only; mapping, authority, idempotency, truth classification and repair
remain Human Capital Management Suite semantics.

Exit: repeated paid use beats the baseline, all facts expose authority/provenance/
freshness, and the zero-workforce-effect invariant passes under retry, crash,
staleness and unauthorized-field tests.

### M4 — Gate A decision

The Gate A compiler consumes the exact signed P1A manifest, not numeric todo
ranges. It rejects missing, stale, waived or out-of-manifest evidence and returns
`PROCEED`, `RESELECT`, `NARROW` or `STOP`. `PROCEED` grants no write authority.

### M5 — P1B bounded write path

Only after Gate A: sign an authority amendment; add exact approval binding, one
WorkItem, one future-date timer, execution-time revalidation, partial replanning,
conditional scarce-resource reservations, immutable TransactionPlan,
prepare/fence/commit, one local outbox operation, one provider write,
observation, reconciliation and repair. Repair must never replay the parent
Promotion.

Exit: stale approvals/snapshots/reservations, concurrent heads, crash-before/after
commit, timeout-after-send, duplicate delivery, provider ambiguity, observation
lag, repair and restore all resolve to their exact allowed states.

### M6 — Pilot and authority decision

Run cutover, rollback/fail-forward, manual continuity, restore, upgrade, security,
accessibility, operational ownership and customer-exit rehearsals. Authority is
tenant/domain/field/capability/effective-time bounded and expires.

Exit: the pilot has repeated paid completion, owned on-call and exit paths, no
`IMPLIED` or `MISSING` selected dependency, and a signed `GO`, `CONDITIONAL_GO`,
`NO_GO` or `RESELECT` decision.

## Required backlog repairs

1. Split the Phase 1 scope ceiling from the final selection-bound P1A release and
   the later P1B authority amendment.
2. Replace the broad dependency ranges on `WEDGE-014` and `WEDGE-015` with a
   manifest-derived evidence closure. Current ranges pull agents, Gate C trust
   work, Phase 2 workflow migration and other excluded systems into Phase 1.
3. Remove source-package tooling and source-attestation items from the backlog;
   they have no consumer.
4. Split P0 jurisdiction-source/reviewer selection from later legal RulePack
   implementation; split provider selection from mature vendor-continuity drills.
5. Split topology selection and runnable sandbox deployment from production HA,
   PITR and disaster-recovery qualification.
6. Split the bootstrap migration command from mature migration/cutover rehearsal.
7. Move the no-effect Promotion endpoint/simulation façade to P1A or use the
   generic typed Intent service; do not label it Gate B.
8. Keep Medical Leave/Return-to-Work outside Gate A/B. Split its contract/vector
   conformance from Phase 2 implementations so `CONF-004` does not require JOIN,
   SUBWORKFLOW, provider writes, durable notices or production legal authority.
9. Resolve assurance inversions before claiming a gate: a Gate A todo may not
   depend on Gate B/C implementation, and independent assurance cannot depend on
   the Gate B evidence package it is meant to inform.

## Medical Leave conformance track

After shared contracts exist, run one fixed synthetic corpus: continuous medical
leave, PTO drift from 72 to 56 hours, unavailable benefits observation, and a
restricted return. Use hypothetical pinned rules and fake ports. The conformance
receipt must prove zero authoritative writes and provider requests, eligibility
independent of approval, partial replanning, `UNKNOWN` never becoming
`INELIGIBLE` or `READY`, medical-data noninterference, and external unavailability
never rolling back valid leave.

Stop after two unchanged clean gap passes. A real jurisdiction, provider write or
production leave endpoint requires a separately staffed and legally reviewed
product gate.

## Mandatory release evidence

- Signed scope/release manifest and digest; exact intent, capability, workflow,
  endpoint, model, property and storage manifests.
- Protobuf descriptor set, generated-Go provenance and dependency/SBOM/build
  attestations.
- Provider topology, authority-by-field matrix, mapping/crosswalk versions and
  adversarial provider fixtures.
- Promotion golden fixtures, exact T0..Tn logical persistence trace and final
  execution/evidence receipt.
- Threat register, structured-log/telemetry redaction and cardinality evidence,
  SLOs, privacy/residency disposition and operational ownership.
- Migration, rollback, restore, projection rebuild and release-upgrade evidence.
- Customer RACI, cutover/manual-continuity/exit pack and signed Gate A/B decision
  packages.

## Non-negotiable stop rules

- No paid, repeated, non-duplicative cross-system problem: `STOP` or `RESELECT`.
- No named provider edition/API/field authority/observation path: no adapter or
  write implementation.
- No further adversarial audit pass, planning expansion, or new specification
  until P1A executes against partner data. Findings close by test or explicit
  deferral only.
- No new lifecycle dimension, kernel family, workflow primitive, or coordination
  layer without a recorded scope exchange.
- Any P1A workforce mutation or external effect: fail the release.
- Any P1B critical or stale evidence, unresolved authority ambiguity, unsafe
  replay/restore, or unowned incident/repair/exit path: `NO_GO`.
- Any added scope requires an explicit scope exchange; roadmap ambition is not a
  dependency on the first release.

## Explicitly deferred

The remaining catalog, Medical Leave implementation, native payroll/benefits/
time/ATS/IAM, general cases/forms/DataOps, agents, broad analytics/search/reports,
bulk/batch/event automation, advanced workflow steps, multiple connectors,
webhooks/sync/MFT, omnichannel messaging, Kafka/cache/search/vector/ClickHouse,
multi-cell relocation and legacy Node/TypeScript production paths do not block
P1A or P1B unless a new signed scope decision exchanges comparable work.
