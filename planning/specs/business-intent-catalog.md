# Business Intent Catalog and Runtime Model

## Decision

The catalog is the set of `IntentDefinition`s that exist in repository source
with a resolvable owner. The fourteen definitions in the initial draft
slice below are the retained P1A/P1B baseline. The separately authorized
small-business track may add source definitions through the same lifecycle;
its proposed issue/operation names are not catalog entries until that work lands. A separately supplied list of roughly 530 candidate names is
retained as a non-normative vocabulary for naming consistency; it is not the
catalog, it has no maturity state, and no work item may depend on ingesting,
partitioning, counting, or attesting it. A name enters the catalog only when a
funded domain writes its definition.

```text
human | system | agent | schedule | rule | external event
                            |
                            v
                  semantic IntentDefinition
                            |
                 versioned typed input/output
                            |
                            v
                     IntentInstance
                            |
              governance + control resolution
                            |
                            v
          one stable kernel family and execution plan
                            |
              +-------------+-------------+
              v             v             v
          capability     workflow     analysis
            call        + domain       query
                        command
                            |
                            v
                  evidence and outcome
```

The three kernel families are:

| Family                | Meaning                                                                            | May create business mutation?                    |
| --------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------ |
| `CHANGE_REQUEST`      | Proposes governed mutation or external effect, directly or via bound child intents | Yes, after approval/revalidation when required   |
| `CALCULATION_REQUEST` | Requests deterministic, pure computation                                           | No, unless a separate change consumes the result |
| `ANALYTICAL_REQUEST`  | Requests governed read, explanation, comparison, or inference                      | No                                               |

Process, filing, batch, and case semantics are definition attributes, not
families; see the kernel contract's family table. A definition declares them
through `side_effect_profile`, `population_scope`, and child-intent bindings.

## Catalog identity

Display names are not identifiers. Every definition uses:

```text
intent_type_id = hcmnext.<domain>.<verb_noun>
version        = monotonically increasing positive integer
definition_ref = intent_type_id + "/v" + version
```

Examples:

```text
hcmnext.people.create_person/v1
hcmnext.people.promote_worker/v1
hcmnext.payroll.run_payroll/v1
hcmnext.regulatory.calculate_tax/v1
hcmnext.cases.open_investigation/v1
hcmnext.analytics.analyze_turnover/v1
```

Domain qualification resolves catalog collisions. In particular, the two
`GrantEntitlement` names are different definitions:

```text
hcmnext.access.grant_entitlement/v1
hcmnext.commercial.grant_entitlement/v1
```

Renaming a display label does not change identity. Changing material input,
output, authorization, side-effect, cancellation, evidence, or outcome semantics
requires a new definition version. Reusing a retired identifier for different
semantics is prohibited.

## Required definition fields

Each definition must pass from `DRAFT_CONTRACT` to `CONTRACTED` before it can
be published and invoked. No field below is inferred from its name. Fields
marked `*` are required at `DRAFT_CONTRACT`; the rest are required at
`CONTRACTED`:

```text
IntentDefinition
  intent_type_id *
  version *
  display_name *
  description *
  owner_domain *
  kernel_family *
  maturity *
  side_effect_profile *
  input_schema_ref *
  result_schema_ref *
  phase_depth *
  owner_plane
  allowed_initiators[]
  allowed_execution_modes[]
  population_scope?
  risk_class
  data_classification_floor
  subject_kinds[]
  required_capabilities[]
  governance_requirements[]
  preconditions[]
  invariants[]
  idempotency_scope
  conflict_footprint_rule
  proposal_binding_rule
  revalidation_rule
  cancellation_rule
  compensation_rule
  evidence_rule
  retention_class
  outcome_contract
  SLO_class
  availability_policy
```

Maturity is explicit:

```text
DRAFT_CONTRACT  required fields exist; some referenced contracts do not resolve
CONTRACTED      all required semantics and schemas are reviewable
COMPILED        generated registry and validation succeed
PUBLISHED       immutable version is available in an environment
DEPRECATED      no new callers; existing compatibility policy applies
RETIRED         invocation prohibited; historical resolution preserved
```

`CATALOGUED` is retired as a maturity value. A name that has not reached
`DRAFT_CONTRACT` is not in the catalog. Within the P1A/P1B release, only
the admitted Phase 1 promotion slice may advance. Under the separately recorded
[small-business placement](../execution-plan.md#recorded-placement-small-business-operations-2026-10-22),
SMB-CORE-001 and each domain contract add concrete owned source definitions,
schemas and capability bindings before advancing through CONTRACTED, COMPILED
and PUBLISHED. They retain the three kernel families and existing lifecycle
dimensions. Tenant enablement, actor/purpose scope, idempotency, conflict
footprints, immutable proposal/approval binding, action-time revalidation,
cancellation/correction and evidence are mandatory. Closed entity/role/domain
vocabularies receive additive compatibility tests; display labels and BI set
tags do not create authority or pretend a definition already exists.

## Instance envelope

An instance references a definition and never relies on a free-form name:

```text
IntentInstance
  intent_id                 UUIDv7
  definition_ref
  tenant_id
  organization_scope_id
  billing_account_id?
  initiator
  delegation_chain[]
  purpose
  subjects[]
  requested_effective_at?
  request_payload           typed Protobuf + schema reference
  idempotency_key
  correlation_id
  causation_id?
  trace_id
  classification
  retention_class
  control_snapshot_refs
  canonical_request_digest
  lifecycle_dimensions
  created_at
  execution_mode
  instance_version
  recorded_at
  last_transition_at
  origin_event_ref?
  source_authority_snapshot_digest
  risk_context_digest
  proposal_revisions[]
  approval_bindings[]
  execution_bindings[]
  cancellation_decisions[]
  supersession_references[]
  correction_references[]
  closure_records[]
```

The typed payload contains Protobuf wire bytes whose descriptor matches the
definition's `input_schema_ref`. Ordinary Protobuf serialization is not the
approval/idempotency canonical form: the versioned canonicalization profile
projects material fields and produces the bound `CanonicalDigestReference`.
Arbitrary JSON, `map[string]any`, and a model prompt are not valid authoritative
request payloads.

## Lifecycle dimensions

The catalog model uses the kernel's five dimensions and no others:

```text
RequestState
ExecutionState
BusinessState
ConsistencyState
ObligationState
```

It adds no universal `status`. A payroll process can be `COMMITTED` with
`ConsistencyState` still `PENDING_OBSERVATION`; an analytical request completes
with `ExecutionState` `NOT_PLANNED` and `ConsistencyState` `NOT_APPLICABLE`.

## Composition rules

One parent intent may create child intents, but the relationship is explicit:

```text
RunPayroll (CHANGE_REQUEST, child-bound process)
   +-- CalculatePayroll (CALCULATION_REQUEST)
   +-- ApprovePayroll (CHANGE_REQUEST: approval evidence)
   +-- ReleasePayroll (CHANGE_REQUEST)
   +-- ReconcilePayroll (CHANGE_REQUEST, child-bound process)
```

```text
SendBulkCommunication (CHANGE_REQUEST, population_scope declared)
   +-- immutable AudienceSnapshot
   +-- one SendMessage child per delivery subject/batch partition
   +-- aggregate result that never hides per-recipient failure
```

Parent completion cannot overwrite child truth. Child idempotency, authorization,
classification, evidence, and repair remain independently addressable.

## Intent, capability, workflow, and transaction boundaries

```text
Intent       what an authorized actor wants accomplished or answered
Capability   a governed semantic operation available to accomplish part of it
Workflow     durable coordination of capabilities, humans, time, and signals
Transaction  atomic authoritative mutation and evidence commit
Outcome      later evidence about whether the intent achieved its purpose
```

An intent may resolve directly to a pure capability without starting a workflow.
For example, `ExplainWorkerState` is analytical. `PromoteWorker` normally compiles
to a workflow and eventually one multi-stream domain transaction. A future
`SubmitGovernmentReport` would be a `CHANGE_REQUEST` with an irreversible
external side effect, invoking a filing gateway only after an immutable filing
package and approvals exist.

## Initiator and exposure policy

Allowed initiators are explicit per definition:

```text
HUMAN | AGENT | SERVICE | INTEGRATION | SCHEDULE | RULE | SYSTEM_EVENT
```

System-triggered definitions are not automatically public APIs. They default to
service, schedule, rule, or system-event initiation and require a separate
capability manifest before any human or partner invocation.
Agent initiation never increases authority: entitlement, AuthZ, legal, privacy,
risk, budget, DLP, and tool-gateway decisions still apply.

## Initial draft-contract slice

This table is the initial release slice, not a permanent ban on later source
definitions. P1A executes the eight marked `P1A`; P1B adds
the five marked `P1B`; `change_manager` is a conformance fixture only:

| Definition                                       | Family                | Side effect         | Release                   |
| ------------------------------------------------ | --------------------- | ------------------- | ------------------------- |
| `hcmnext.people.change_manager/v1`               | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | Design/conformance only   |
| `hcmnext.people.explain_worker_state/v1`         | `ANALYTICAL_REQUEST`  | `READ_ONLY`         | P1A                       |
| `hcmnext.people.promote_worker/v1`               | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | P1A simulate; P1B execute |
| `hcmnext.rewards.change_base_pay/v1`             | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | P1B                       |
| `hcmnext.rewards.simulate_compensation/v1`       | `CALCULATION_REQUEST` | `PURE`              | P1A                       |
| `hcmnext.rewards.evaluate_pay_band_position/v1`  | `CALCULATION_REQUEST` | `PURE`              | P1A                       |
| `hcmnext.rewards.reserve_compensation_budget/v1` | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | P1B                       |
| `hcmnext.rewards.release_compensation_budget/v1` | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | P1B                       |
| `hcmnext.work.approve_proposal/v1`               | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | P1B                       |
| `hcmnext.work.reject_proposal/v1`                | `CHANGE_REQUEST`      | `INTERNAL_MUTATION` | P1B                       |
| `hcmnext.intelligence.explain_transaction/v1`    | `ANALYTICAL_REQUEST`  | `READ_ONLY`         | P1A                       |
| `hcmnext.operations.detect_drift/v1`             | `ANALYTICAL_REQUEST`  | `READ_ONLY`         | P1A                       |
| `hcmnext.operations.create_repair_plan/v1`       | `ANALYTICAL_REQUEST`  | `READ_ONLY`         | P1A (recommendation only) |
| `hcmnext.operations.simulate_repair/v1`          | `CALCULATION_REQUEST` | `PURE`              | P1A                       |

Promotion is a composite `ChangeRequest`; it does not silently alias the base-pay
child request. The proposal binds the exact child set, versions, material inputs,
expected stream sequences, reservations, required approvals, and execution order.

## Vocabulary list (non-normative)

The candidate-name list supplied at project intake is retained only as a naming
reference so that a future `hcmnext.<domain>.<verb_noun>` identifier is chosen
consistently with earlier thinking. It carries no ownership assignments, no
numbering that must be preserved, no maturity state, and no coverage claim.
Partition files, ingestion tooling, source attestation, and count reconciliation
are not work items. When a funded domain contracts its first definition, that
domain starts its own definition source file.

CI must fail for duplicate `(intent_type_id, version)`, unqualified IDs,
unknown schemas/capabilities, invalid family/side-effect combinations, or a
published definition below `CONTRACTED` maturity.

## Go-only implementation boundary

Canonical request/result contracts are Protobuf. The admitted deterministic Go
compiler validates the structured sources and produces registries, documentation,
compatibility reports, fixtures, and dependency indexes. SchemaFlux remains
disqualified for production generation until the qualification gates in the
[frontend contract](production-frontend-and-page-composition.md) are satisfied. GWC
discovers permitted definitions through Go capability clients. grpcbridge adapts
external transports to the same gRPC services. No TypeScript catalog, UI model,
Node generator, or parallel JSON schema is permitted.

The initial canonical Protobuf contract is
[`schema/proto/hcmnext/intents/v1/business_intent.proto`](../../schema/proto/hcmnext/intents/v1/business_intent.proto).
Generated Go is present in
[`gen/go/hcmnext/intents/v1/business_intent.pb.go`](../../gen/go/hcmnext/intents/v1/business_intent.pb.go).
Further changes use the pinned repository toolchain and regeneration/drift gates;
handwritten duplicate message types are prohibited. Existing generated types do
not establish that a proposed business definition is registered or published.

## Acceptance tests

1. `(intent_type_id, version)` is unique and a retired identifier is never reused.
2. Domain-qualified IDs distinguish access and commercial entitlement grants.
3. Every published version resolves input/output Protobuf descriptors and capabilities.
4. Deterministic serialization produces the same canonical request digest in every Go process.
5. `ANALYTICAL_REQUEST` and `CALCULATION_REQUEST` definitions cannot declare mutation side effects.
6. A `CHANGE_REQUEST` with `IRREVERSIBLE_EXTERNAL_MUTATION` declares irreversibility and correction semantics.
7. A `CHANGE_REQUEST` with `population_scope` preserves per-item identities, results, failures, and retry safety.
8. An agent cannot invoke a definition absent a published capability and governance approval.
9. A renamed display label preserves definition identity; material contract change increments version.
10. Historical instances remain resolvable after definition deprecation or retirement.

## Status and ownership

- Owner: Business Transaction Service and Capability Registry owners
- Status: kernel contract defined; the initial fourteen draft definitions are the baseline; actual source and maturity determine additions. The intake name list remains non-normative vocabulary
- Phase 1: only the explicit slice above
- Expansion: the owner-authorized small-business backlog contracts source definitions through existing owners and publication gates; production release still requires domain-specific evidence
