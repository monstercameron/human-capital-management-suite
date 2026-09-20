# Human Capital Management Suite Phase 1 Execution Plan

This document converts the architecture constitution in [plan.md](plan.md) into a bounded Phase 1 delivery plan. When the two documents differ on near-term scope, this execution plan controls implementation sequencing; `plan.md` continues to control architectural invariants and long-term direction.

The dependency-ordered implementation bridge, current blockers, first ten working
days and separate P1A/P1B release contracts are maintained in
[next-steps.md](next-steps.md). Where any specification's phase table disagrees
with the P1A/P1B contents in that document, the P1A/P1B contents win; this
plan defines the gates and their acceptance, not the release inventory.

## Phase 1 Outcome

Prove that Human Capital Management Suite improves one Promotion + Compensation Change workflow for paid design partners without becoming the employee system of record or building the eventual Workforce OS prematurely.

```text
manager/HR intent
      -> immutable proposal
      -> preflight and simulation
      -> exact approval binding
      -> execution-time revalidation
      -> one governed HCM write path
      -> external observation
      -> reconciliation or RepairPlan
      -> complete evidence
```

The competitive proof is not that Human Capital Management Suite has workflow, APIs, webhooks, AI, or
an HCM feature catalog. Those are table stakes in current enterprise suites. The
pilot must show that cross-system proposal integrity, source authority, conflict
control, execution-time revalidation, observation, reconciliation, and repair
remove more risk and operating complexity than the additional control-plane
dependency introduces. See the [competitive positioning and authority expansion
contract](specs/competitive-positioning-and-authority-expansion.md).

## Scope Rules

Phase 1 has four implementation-depth labels:

- **IMPLEMENT** — production behavior exists, is operated, and gates the pilot.
- **MINIMAL CONTRACT** — stable boundary and identifiers exist, with only the pilot behavior implemented.
- **DESIGN / CONFORMANCE ONLY** — scenarios prevent architectural dead ends; no production subsystem is staffed.
- **OUT OF PHASE** — no work unless an explicit dependency decision removes comparable scope.

These are the only normative delivery-depth values. Other documents and generated
sources map to them as follows:

| Alternate wording                                                     | Normative delivery depth                                                                                                  |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `Phase 1 slice`, `GATE_A_IMPLEMENT`, `GATE_B_IMPLEMENT`               | `IMPLEMENT` only after the applicable gate evidence exists; before then `MINIMAL CONTRACT` or `DESIGN / CONFORMANCE ONLY` |
| `Phase 1 gate`, `CONTRACT ONLY`, `Minimal contract`                   | `MINIMAL CONTRACT`                                                                                                        |
| `CONFORMANCE ONLY`, `DESIGN_CONFORMANCE_ONLY`, long-term architecture | `DESIGN / CONFORMANCE ONLY`                                                                                               |
| `Deferred`, `OUT`                                                     | `OUT OF PHASE`                                                                                                            |

Artifact maturity is independent: `DRAFT_CONTRACT`, `CONTRACTED`, `PUBLISHED`,
`DEPRECATED`, and `RETIRED` describe contract lifecycle, not implementation depth.
Coverage states (`DEFINED`, `PARTIAL`, `IMPLIED`, `MISSING`, `DEFERRED`) describe
responsibility coverage, not delivery. No alternate word may silently promote an
artifact across these axes.

The detailed matrix is authoritative in [Phase 1 of the master plan](plan.md#phase-1-changeops-overlay).

Coverage status is not staffing authority. Every Gate A/B deliverable must appear
in a named delivery manifest with artifact, owner, estimate, dependency,
acceptance evidence, operations owner, and displaced scope for late additions.
The scope-budget, stop conditions, contract-toolchain gate, manual-continuity
requirements, and negative conformance cases are defined in the
[2026-08-13 adversarial gap-closure contract](specs/adversarial-gap-closure-2026-08-13.md)
and the maintained
[2026-08-14 thirty-two-reviewer audit](specs/adversarial-audit-32-reviewers-2026-08-14.md).

## Legacy Baseline Rule

The repository already documents working legal-name, headcount-approval,
organization-transfer, workflow-admin, schema-driven UI, and AI-generated-page
behavior. Treat that behavior as regression input and a comparison baseline
for the Go reimplementation. Do not extend the Node/TypeScript and React paths
with new behavior; they may keep running beside the Go slice through P1A.

```text
legacy route / fixture / UI contract
              |
        capture current evidence
              |
       define Protobuf capability
              |
       implement the Go vertical slice
              |
  P1A: run beside legacy, compare outcomes
              |
      verify Go-native conformance
              |
  P1B: exclude legacy runtime from release
```

Historical markdown statements that tests passed do not establish present
status. The cutover gate uses fresh Go-native CI, ledger, projection, AuthZ, workflow,
outbox, accessibility, and recovery evidence. See the retained
[legacy implementation baseline](specs/legacy-implementation-baseline.md).

The language and core-library boundary is governed by [the Go Technology
Constitution](specs/go-only-technology-constitution.md), including the
qualification fixtures and fallbacks for GWC, grpcbridge, and SchemaFlux.

## Capability Coverage Rule

Every newly discovered backend responsibility is added to the [Platform Capability Coverage Matrix](specs/platform-capability-coverage-matrix.md) as `DEFINED`, `PARTIAL`, `IMPLIED`, `MISSING`, or `DEFERRED`, with an owner, evidence link, phase depth, and next closure action. `IMPLIED` or `MISSING` responsibilities required by the pilot must be resolved before implementation; they cannot be hidden inside workflow, connector, or UI code.

For Phase 1, “resolved” means the owner, canonical state, typed API, dependency boundary, fail-open/closed/stale/queued behavior, evidence, security controls, and implementation depth are written and reviewed. The [explicit responsibility boundaries](specs/platform-responsibility-boundaries.md) close the formerly implied shared systems; the [foundation gap-closure contracts](specs/platform-foundation-gap-closure.md) define runtime configuration/bootstrap, identity/session/recovery, hostile-content quarantine, governed global datasets, dependency discovery, idempotency retention, certificate lifecycle, deletion, incident communication, and accessibility assurance.

## HRIS DataOps Operator-Surface Rule

Phase 1 will already need import staging, system comparison, temporal/provenance diagnosis, AuthZ explanation, connector testing/redrive, and configuration diff/promotion to operate the pilot safely. Build these as semantic capabilities with customer-grade authorization and evidence, but productize only the slices a design partner uses independently.

```text
pilot operational need
        -> governed internal capability
        -> safe HRIS-admin workspace
        -> repeated independent customer use?
             | yes                 | no
             v                     v
       supported DataOps       remain operator-only
          capability
```

See [the HRIS Admin Toolkit and DataOps specification](specs/hris-admin-dataops.md).

## Integration Platform Slice

Phase 1 builds one reusable external-system path, not a collection of vendor services:

```text
ConnectorDefinition
      + ConnectorConnection
      + MappingProfile
      + ExternalReference crosswalk
               |
               v
       ConnectorOperation journal
               |
      rate-aware queue + adapter
               |
               v
       external observation
               |
        reconcile / redrive
```

The design-partner system determines the first named connector. That connector must use the shared definition, connection, mapping, permission-diagnostic, capacity, operation, observation, redrive, and reconciliation contracts. Phase 1 does not promise other named connectors, bidirectional coverage for every vendor object, or marketplace certification.

See [the Integration Platform specification](specs/integration-platform.md).

## Communications Plane Slice

Phase 1 communication is intentionally narrow. P1A sends nothing. P1B sends
one kind of message, and only if the selected customer path needs it:

```text
Approval / WorkItem
        |
   MessageIntent (APPROVAL_REQUIRED | TASK_ASSIGNED)
        |
resolved approver + one template + one locale
        |
   one email adapter
        |
delivery attempt states -> workflow signal
```

The secure inbox is a minimal contract (an inbox record readable from the
workspace). No omnichannel, preference, thread, bulk, or legal-notice work is
in Phase 1.

See [the Messaging and Notification Plane specification](specs/messaging-and-notification-plane.md).

## Experience and Branding Slice

Phase 1 implements one Promotion workspace over the same Protobuf capabilities
used by internal Go callers, on GoWebComponents if it passes the UI
qualification fixture before P1B and on Go server-rendered HTML otherwise.
The qualified transport edge (grpcbridge or grpc-gateway/connect-go) supplies
the web adaptation; the qualified generator (SchemaFlux or protoc) compiles
the selected structured catalogs without becoming a competing source of schema
truth.

```text
authorized workflow state
          +
provenance-bearing field bindings
          +
available semantic actions
          v
typed PageDefinition -> registered widgets -> GoWebComponents
```

The slice must retain semantic brand tokens, field filtering before render,
keyboard and assistive-technology support, and workflow-native actions. It does
not need pixel parity with the historical React console or a general visual
builder. No React or TypeScript runtime is in the P1B release image.
See [the experience, dynamic UI, and branding contract](specs/experience-ui-and-branding.md).

## Plane Dependency Rule

Phase 1 remains a vertical slice through the canonical plane model:

```text
GWC -> grpcbridge -> Identity/Governance -> Control resolution
                                           |
                                           v
                                        Workflow
                                           |
                                           v
                            People + Rewards capabilities
                                           |
                                           v
                         ledger + projection + outbox commit
                                           |
                              connector / message effects
```

Workflow code may coordinate `people.promote` and
`rewards.compensation.change`; it may not update their tables or construct their
canonical events. Domain packages own validation, invariants, transaction
behavior, events, and critical projections. Analytics, search, semantic indexes,
and reporting remain outside the synchronous pilot command path.

See [the Canonical Platform Plane Model](specs/platform-plane-model.md).

The bounded business semantics are defined by the [People, Employment, and
Assignment Domain](specs/people-employment-assignment-domain.md), [Organization
and Relationship Domain](specs/organization-and-relationship-domain.md),
[Compensation Domain](specs/compensation-domain.md), and [Position and Headcount
Domain](specs/position-and-headcount-domain.md). [Source Authority](specs/source-authority-and-external-mastering.md), [Identity Resolution](specs/identity-resolution-and-entity-linkage.md), and [Workforce Budget Authority](specs/workforce-budget-authority.md) are shared governed inputs. Approval, idempotency, event,
configuration, and evidence hashes use the [Canonical Envelope and Digest
Contract](specs/canonical-envelope-and-digest.md).

The [Business Intent Kernel](specs/business-intent-and-change-request.md) owns the
multidimensional request lifecycle; the [Conflict Registry](specs/cross-workflow-conflict-and-write-intent.md) closes races at commit; [Classification and DLP](specs/data-classification-and-dlp.md) owns labels and propagation; and [Records Management](specs/records-management-and-disposition.md) owns record declaration through disposition.

The [Business Intent Catalog](specs/business-intent-catalog.md) is the fourteen
drafted Promotion/Compensation, explanation, drift, and repair definitions. The
intake name list is non-normative vocabulary; no Phase 1 work item depends on
ingesting, partitioning, or attesting it.

The [Transaction Plan and Commit Coordinator](specs/transaction-plan-and-commit-coordinator.md) owns the immutable approval-bound executable plan and atomic local commit. [Secrets and Credential Leases](specs/secrets-key-custody-and-credential-leases.md) owns material access, [Quality and Invariant Evaluation](specs/data-quality-and-invariant-evaluation.md) owns assessment protocol, and [Provenance](specs/provenance-graph-and-lineage.md) owns authorization-aware lineage completeness.

The [Capability Registry](specs/capability-registry-and-lifecycle.md) is the only
source of active semantic operations. The [Governance Coordinator](specs/governance-decision-and-obligation-composition.md) composes authoritative policy results. [Incident Management](specs/incident-management.md) owns operational incident truth, while [Reliability Management](specs/slo-sli-error-budget.md) owns measurable SLI/SLO and error-budget consequences.

## Delivery Gates, Staffing Envelope, and Critical Path

Phase 1 is three authority gates, not one architecture-completion gate. Passing an
earlier gate does not imply permission to perform the next gate's effects.

### Gate A — Paid Design-Partner Observation

Objective: prove customer value without Human Capital Management Suite owning or executing the employee
change.

```text
partner problem and baseline
          -> incumbent edition/topology and native-capability assessment
          -> incumbent read/observe connector
          -> independently owned downstream handoff/observation boundary
          -> normalized facts + provenance
          -> promotion proposal and deterministic simulation
          -> approval binding demonstration
          -> intended/observed diff and repair recommendation
          -> GWC workspace used by paid design partner
          -> measured outcome and proceed/stop decision
```

Required workstreams and effort estimate, in person-weeks so the schedule
scales with whoever is available:

| Workstream                 | Person-weeks | Exit artifact                                                                            |
| -------------------------- | -----------: | ---------------------------------------------------------------------------------------- |
| Design-partner lead        |            4 | Paid agreement, exact incumbent system/fields, baseline, target, price and stop criteria |
| Intent kernel + simulation |            8 | Protobuf read/query/simulation/proposal contracts, digest, and deterministic fixtures    |
| Read connector + diff      |            8 | One read/observe connector, mappings, provenance, comparison and reconciliation view     |
| Workspace                  |            6 | Promotion analysis/proposal workspace on GWC or the Go SSR fallback                      |
| Read-only safety           |            4 | Tenant isolation, AuthN, field AuthZ, no secrets in logs, backups exist                  |

Total: roughly 30 person-weeks after partner access and sample data exist, so
about 8 weeks for a team of four or about 30 weeks for one builder working
alone. The current team is one builder; the partner contract must state which
of those two it is buying. Critical path is partner data access `->`
schema/mapping `->` read connector `->` simulation `->` workspace `->`
observed paid use.

Gate A qualification is not satisfied by a single-system demo or approval artifact.
The design-partner manifest must identify the licensed incumbent edition and native
workflow coverage, at least one independently owned downstream system/effect, a
real consumable handoff plus acknowledgement/observation, eligible transaction
volume, adoption denominator, bypass sources, customer labor by role, cost-to-serve,
and numeric proceed/reselect/stop thresholds. If those facts do not demonstrate a
non-duplicative cross-system failure class, the gate stops or the product claim is
narrowed explicitly.

Gate A does **not** require write-capable agents, transactional email, generalized
Human Work, customer config packaging, certificate-compromise drills, automated
subject deletion across restored backups, production chaos, or automated customer
incident communications unless the selected customer path directly depends on
one. It requires explicit boundaries and safe read-only behavior, not full
implementations.

### Gate B — Limited Write Authority

Objective: permit one design partner's bounded Promotion/Compensation write path.
Incremental work begins only after Gate A demonstrates repeated value.

```text
Gate A evidence
    -> immutable proposal + exact approval binding
    -> durable workflow slice + one approval task
    -> multi-stream transaction + outbox
    -> one governed external write
    -> observe + reconcile
    -> RepairPlan / redrive
    -> limited authority decision
```

Incremental effort estimate: roughly 40 person-weeks (approval binding and
WorkItem 6, conflict and revalidation 6, transaction coordinator and outbox 8,
durable runtime slice 10, one connector write with observation and redrive 6,
write-path safety 4), refined after Gate A. Gate owner is the product lead
jointly with the security and domain-authority approvers. Critical path is
proposal/approval `->` conflict and revalidation `->` durable execution `->`
external write/idempotency `->` observation/repair.

### Gate C — General Production Authority

Objective: operate broader production authority with the full trust, recovery,
support, communication, upgrade, deletion, incident, and conformance controls
appropriate to the contracted domains and scale. Gate C has no date or staffing
commitment in Phase 1; it is planned only from Gate B operating evidence.

### Scope-Exchange Rule

Any requirement promoted into Gate A or B must identify its gate owner, staff and
schedule impact, dependency, acceptance evidence, and the equivalent scope removed
or deferred. No new subsystem becomes mandatory merely because its long-term
contract is `DEFINED`. If no equivalent scope can be removed, the gate must be
re-estimated and explicitly reapproved.

### Recorded placement: workflow engine extensibility (2026-09-19)

Scope: the plan to let many workflows, and later tenant variants, be assembled
from building blocks. The engine side is `WF-EXT-001`–`WF-EXT-026` and the
missing domain capabilities are `WF-CAP-001`–`WF-CAP-019`, both in
[the backlog](todos.md#77-workflow-engine-extensibility-and-building-blocks-2026-09-19).
The contract is in
[the workflow kernel spec](specs/workflow-runtime.md#extensibility-closed-kernel-open-registries).

- **Inside Gate B, no exchange needed:** `WF-EXT-001` and `WF-EXT-002`. They
  fix the budget-hold compensation defect and replace the two-plan Promotion
  switch with workflow registrations. That work is already forced by the
  open `PROMO-EXEC-007` and `HIPERF-002`/`HIPERF-004` items, and adds no new
  subsystem.
- **Gate C:** `WF-EXT-003`–`WF-EXT-026` and every `WF-CAP-*` item. None is a
  Gate A or Gate B acceptance requirement. Pulling any of them into Gate B
  needs the Scope-Exchange Rule fields above, which have not been filled in.
  The first candidates are `WF-EXT-004`–`WF-EXT-008`, which let a second
  workflow family execute without new engine code.
- **The Phase 1 non-goals below still hold.** At Gate C, forms, bulk
  communication, e-signature and population work come back as registry
  entries, fragments and spawn sources on the existing kernel. They do not
  return as separate platforms.
- **No new regulatory engine.** Regulated calculation stays delegated to the
  incumbent or vendor through the capability registry's `DELEGATED` class.
  This keeps the non-goal "general regulatory calculation or government
  filing engines".

## Explicit Non-Goals

- Full payroll, tax, benefits, timekeeping, recruiting, talent, or workforce-access products
- Reimplementation of every legacy route or screen before the Promotion slice can ship
- Any Node, TypeScript, React, or Vite dependency in the P1B release image or runtime (development tooling is not in scope; legacy may run beside the Go slice in P1A)
- General regulatory calculation or government filing engines
- Kafka, ClickHouse, OpenSearch, vector infrastructure, or a distributed cache as mandatory dependencies
- General case management, e-signature platform, omnichannel/inbound conversations, or bulk communications
- Multi-provider model routing, autonomous write-capable agents, or agent-created production workflows
- Full usage-rating, invoicing, tax, or payment collection
- Multiple physical tenant cells or live tenant relocation
- A complete master-data, migration, ontology, or knowledge-management platform
- General forms/questionnaire, case/service-catalog, arbitrary customer-rule, batch-job, or managed-file-transfer platforms
- The complete 27-tool HRIS DataOps catalog; only pilot-required operator slices are in scope
- A broad named-connector catalog, connector marketplace, or certification program

## Gate A Acceptance — Paid Observation

Gate A passes only when:

- A paid design partner repeatedly uses the GWC workspace on its own incumbent
  data and the agreed time/error/visibility metric improves against baseline.
- Human Capital Management Suite reads and observes only the approved fields through one connector;
  source authority and every transformation remain visible.
- A dated incumbent-edition/topology assessment proves that the selected failure
  class is not already governed adequately by licensed native functionality.
- The topology includes one HCM source and at least one independently owned
  downstream handoff/observation boundary; otherwise the approved claim is
  explicitly single-system governance rather than cross-system orchestration.
- Promotion proposal, deterministic compensation/position simulation, exact
  approval binding demonstration, and intended-versus-observed comparison work
  without executing employee mutations.
- Read-only safety matches read-only risk: tenant isolation is tested, every
  request has a server-derived principal, field-level authorization masks
  before render, no secret or worker payload appears in logs, and a database
  backup exists and has been restored once in a non-production environment.
  Signed builds, SBOMs, telemetry privacy gateways, and restore drills are
  Gate B and Gate C controls, not Gate A.
- The partner and Human Capital Management Suite jointly record proceed, change-wedge, or stop evidence.
- The handoff is consumable by the incumbent/downstream operating process and
  produces an acknowledgement or observation. Approval binding alone is not proof
  of cycle-time, quality, audit-effort or adoption improvement.
- The delivery manifest records customer labor, eligible-volume denominator,
  bypass taxonomy, cost-to-serve and numeric economic/adoption stop thresholds.

## Gate B Acceptance — Limited Write Authority

Gate B grants one partner write authority over one field family (job/level and
base pay) for one workflow. The acceptance list is sized to that, and is
grouped so that each item names the risk it retires.

**Transaction correctness**

- Reference scenarios cover stale approval, concurrent change, future effective date, retry, partial external failure, and correction.
- The published Promotion graph compiles before execution; invalid types, capabilities, and effect/idempotency declarations fail publication.
- Approvals bind the exact material proposal digest; approver authority is rechecked at decision and at execution; a control-snapshot change revalidates without invalidating approvals whose material result is unchanged.
- The Multi-Stream Transaction Contract commits all local authoritative streams and outbox records atomically; process death before and after commit resumes from durable state without duplicating effects.
- Every pilot effect has a durable idempotency record whose retention exceeds its retry window, a per-resource ordering key, pre-send authority revalidation, and an observed reconciliation result.
- Intended and observed external state reconcile, or the system exposes `ConsistencyState=DEGRADED` and a governed RepairPlan; redrive cannot repeat the parent transaction.
- Timezone, currency, and effective-range semantics are version-pinned for the pilot's one locale and currency.

**Access and isolation**

- UI, HTTP, and gRPC paths share validation, AuthZ, idempotency, ledger, and error semantics.
- Tenant/org/record/field/capability restrictions pass abuse tests.
- Session revocation and step-up authentication for the approve and execute actions have exercised evidence.
- Uploaded content, if the pilot accepts any, is quarantined before use.

**Operability**

- A restore reproduces ledger, outbox, and critical projections, and the reference workflow passes afterward; the measured time is recorded as the pilot's RTO.
- A named on-call owner, support hours, a customer advisory route, a redrive/rollback procedure, and an exercised pilot exit/export runbook exist.
- A signed build with SBOM is deployed through the approved path, and the release image contains no legacy runtime.
- The workspace passes the UI qualification fixture (keyboard, screen reader, contrast, reflow, provenance and available-action rendering) on GWC or the Go SSR fallback.
- If workflow notification is required by the selected customer path, it is the one-email slice defined in the messaging plane; otherwise messaging is outside Gate B.

**Customer evidence**

- Paid users repeatedly complete the workflow and demonstrate measurable improvement against the Gate A baseline.
- The partner can see, for every promotion, the worker notice and the visible inputs the decision used, and can request a correction.
- Relevant legal-name, headcount approval, and organization-scope fixture behavior is preserved or explicitly superseded.
- No pilot dependency remains `IMPLIED` or `MISSING` in the capability coverage matrix.

## Gate C Acceptance — General Production Authority

Gate C adds, according to contracted domain and risk:

- Controls moved here from earlier drafts of Gate B: signed-configuration
  fingerprints with offline continuation, tenant/cell placement fencing,
  workload identity on east-west paths, rolling upgrade with rollback before
  the irreversible boundary, IdP-outage behavior, DestinationTrust with
  SSRF/rebinding/redirect suites for customer-configured destinations, WCAG
  2.2 AA evidence for the complete process including accessible documents and
  a governed non-digital route, timer behavior under reference-dataset
  change, time-bounded JIT diagnostic access, and multi-locale/currency
  pinning.
- Full support/JIT, certificate compromise, isolated recovery, restore-game-day,
  customer incident communication, subject/tenant deletion and backup re-delete,
  chaos, workload/cell relocation, and broad configuration-package exercises.
- Messaging, agent, billing, regulatory, analytics, and additional domain gates
  only when those capabilities are sold or placed in the production path.
- Quantitative SLO, RPO/RTO, blast-radius, capacity, projection/search lag,
  reconciliation, and obligation commitments with staffed operational ownership.

Failure to meet the gate produces a narrow remediation plan or a product decision. It does not automatically justify implementing more platform planes.
