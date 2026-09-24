# Operations, Recovery, Reliability and Production Assurance Entities

This catalog makes the correctness of the platform running HCM business state
explicit. Operational records never replace business ledger facts.

## Reconciliation, drift and repair

```text
ReconciliationRun
  run_id, policy/version, subject/effect/resource scope
  expected/observed snapshot refs, authority/freshness/watermark
  started/completed/status, difference refs[]

ReconciliationDifference
  difference_id, run/resource/field path
  expected/observed value digests and presence states
  class/severity/confidence/authority/causal candidates/disposition

DriftFinding
  finding_id, reconciliation/difference refs
  mismatch type/suspected causes/confidence/impact/risk
  affected intents/workflows/effects, owner/status

RepairPlanRevision
  revision_id, repair plan/discrepancy/expected-state refs
  precondition/CAS snapshot, expected post-state
  action DAG/capabilities/idempotency/reversibility
  simulation/approval/SoD/expiry/digest

RepairExecution
  execution_id, plan revision, lease/fence
  action execution/attempt/provider receipt refs[]
  ambiguity/cancellation/compensation/result/status

RepairVerification
  verification_id, execution/post-state observations
  authority/watermark/residual differences
  PASS | FAIL | PARTIAL | UNKNOWN, repair/incident refs
```

## Incidents, quarantine and availability

```text
IncidentSignal
  signal_id, incident/source/type/severity/confidence
  affected target/time/evidence/dedupe/correlation

IncidentImpactRevision
  revision_id, incident/tenant/cell/service/capability/workflow/data scope
  severity/priority/business impact, effective/recorded times
  source evidence/supersession

IncidentTimelineEntry
  entry_id, incident/action/decision/communication ref
  event type/actor/authority/occurred/recorded times/evidence

IncidentRoleAssignment
  assignment_id, incident/principal/role
  scope/authority/effective interval/handoff/status

IncidentAction
  action_id, incident, contain/mitigate/recover/verify type
  target/owner/authority/start/end/expected-observed result/status

IncidentResolution
  resolution_id, incident/triggering-condition evidence
  containment/recovery/verification refs
  residual risk/known issues, authority/resolved-at

PostIncidentReview
  review_id, incident/timeline/evidence refs
  root/contributing causes/lessons/control gaps
  corrective action refs, owner/approval/closure

QuarantineRecord
  quarantine_id, target kind/id/version
  tenant/org/region/global scope, reason/evidence
  fail behavior/fallback/degradation, affected work
  activated/expires/released times, owner/approver/status

CapabilityAvailability
  availability_id, capability/version/scope
  AVAILABLE | DEGRADED | READ_ONLY | QUEUED | QUARANTINED | DISABLED
  reason/dependencies/fallback/valid interval/health evidence
```

## Recovery and backup

```text
RecoveryContract
  contract_id, plane/store/service/authority
  RPO class and numeric target / RTO, RESTORE | REBUILD | REPLAY mode
  ordering/key/config/schema dependencies
  replay watermark/validation/degraded-state requirements

RecoveryScenario
  scenario_id, loss/compromise/failure assumptions
  affected planes/tenants/cells/dependencies, expected objectives

RecoveryRun
  run_id, scenario/plan/isolated environment
  checkpoint/dependency/cutover/failback refs
  started/completed, actual RPO/RTO/status

RecoveryCheckpoint
  checkpoint_id, run/plane/step
  source/target watermarks, artifact/key/config/schema versions
  validation/approval/fence/status

RecoveryValidation
  validation_id, run/plane/tenant scope
  cryptographic/semantic/invariant/workflow/reconciliation checks[]
  PASS | FAIL | PARTIAL | UNKNOWN, evidence/status

RecoveryCutover
  cutover_id, run/source/target placement
  freeze/catch-up/fence/authority/DNS-routing refs
  approval/start/end/rollback/verification status

BackupPolicy
  policy_id, plane/data class/tenant scope
  full/incremental schedule, isolation/immutability/encryption
  retention/RPO/verification/restore-test rules

BackupSet
  set_id, policy/source placement/watermark
  base/incremental chain refs, manifest/digest/key version
  isolated location/retention lock/expiry/status

BackupVerification
  verification_id, backup set
  completeness/hash/signature/decrypt/semantic-restore checks
  verified-at/tool/version/result/evidence
```

## Overload, capacity and cell isolation

```text
WorkloadCriticalityPolicy
  policy_id, workload/capability/workflow classes
  priority/order/fail behavior/degradation/admission rules/version

RetryBudget
  budget_id, tenant/service/dependency/operation scope
  period/allowed-consumed-refunded tokens
  retryable classes/backoff/owner/expiry/status

BackpressureSignal
  signal_id, source resource/dependency
  load/queue/capacity/recommended rate
  propagation targets/issued/expires times/status

LoadSheddingDecision
  decision_id, workload/tenant/criticality/resource snapshot
  admit/queue/defer/reject/degrade result
  policy/retry-after/evidence/expiry

TenantResourceQuota
  quota_id, tenant/tier/cell/resource type
  limit/burst/reservation/fair-share/window
  consumed/pending/degraded values, policy/version

CapacityForecast
  forecast_id, cell/resource/horizon
  tenant/tier demand/scenarios/confidence
  exhaustion risk/recommendations/model/version

NoisyNeighborFinding
  finding_id, tenant/resource/window
  expected/observed share/impact/confidence
  throttle/relocation/incident/remediation refs

WorkloadDrainPlan
  plan_id, target node/shard/cell/workload
  safe points/in-flight inventory/fences/dependencies
  deadline/capacity/rollback/verification policy

TenantCell
  cell_id, region/residency/isolation tier
  API/workflow/database/ledger/queue/cache/search/agent/integration/
  artifact/key/config resource refs, capacity/health/lifecycle

TenantPlacement
  placement_id, tenant/cell/epoch
  allocated resources/isolation/tier/residency/SLA
  decision/activation/fence/effective interval/status

TenantRelocationPlan
  plan_id, tenant/source-target cells/placement epochs
  per-plane freeze/snapshot/copy/catch-up/cutover/rollback steps
  workflows/timers/signals/connectors/journals/key/cert migration
  checkpoints/fences/reconciliation/approval/status
```

## SLO and telemetry governance

```text
SLITarget
  target_id, SLO/tenant-tier/cell/capability/workflow/criticality
  indicator/query/window/threshold/authority/freshness dimensions

ErrorBudget
  budget_id, SLO/window, eligible/excluded denominator policy
  allowed/consumed/remaining values, burn alerts/status

SLOBreach
  breach_id, SLO/target/window
  observation/evidence/error-budget impact
  incident/admission/load-shedding/remediation refs/status

TelemetryPolicy
  policy_id, signal/data class/tenant/purpose
  attribute allowlist/redaction/sampling/cardinality/export/retention rules

TraceContext
  trace_id/span/parent, tenant/cell
  workflow/node/effect/incident correlation refs only
  sampling/privacy/export decisions

SamplingDecision
  decision_id, trace/log/metric/security-event ref
  policy/risk/outcome, keep/drop/rate/reason/version

CardinalityBudget
  budget_id, metric/tenant/exporter/window
  allowed/observed series and label values
  overflow aggregation/drop/quarantine policy

TelemetryExport
  export_id, signal batch/destination
  redaction/DLP/sampling/tenant-isolation decisions
  bytes/attempt/receipt/retention/status

TelemetryPipelineHealth
  observation_id, collector/exporter/pipeline/as-of
  queue/drop/error/latency/backpressure/capacity values
  SLO/incident/status
```

## Software supply chain and continuous controls

```text
SoftwareComponent
  component_id, name/version/supplier/license/source digest
  dependency/build/artifact/vulnerability refs

SBOM
  sbom_id, artifact/release/format/version
  component/dependency refs, generated-at/tool/signature/digest

BuildProvenance
  provenance_id, source revision/dependency lock/builder/environment
  isolated build inputs/steps/output artifact digest
  SLSA-like level/signature/attestation

ArtifactSignature
  signature_id, artifact digest/key/algorithm
  signer/build provenance/trusted time/verification/status

Deployment
  deployment_id, artifact/environment/cell/service/version
  config/schema/migration refs, rollout/canary/rollback/status

DeploymentVerification
  verification_id, deployment/artifact/SBOM/provenance/signature refs
  policy/security/conformance/runtime checks
  PASS | FAIL | UNKNOWN, admission/quarantine evidence

VulnerabilityFinding
  finding_id, advisory/component/artifact/deployment refs
  affected service/cell/tenant/job/agent graph
  exploitability/severity/mitigation/remediation/deadline/status

ControlDefinition
  control_id, framework/control/version/owner
  requirement/test/evidence/frequency/scope

ControlEvidence
  evidence_id, control/implementation/scope/window
  source/artifact/digest/collector
  freshness/completeness/result/retention

ControlDriftFinding
  finding_id, control/approved/current state refs
  difference/risk/detected-at/owner/remediation/incident/status
```

## Operations invariants

```text
Repair never overwrites newer authority and always verifies separately.
A healthy backup has cryptographic and semantic restore evidence.
Critical work consumes priority before background work under overload.
Retries share bounded budgets and propagate backpressure.
Placement epochs fence stale writers across relocation.
Quarantine release requires explicit health and reconciliation evidence.
Telemetry never becomes a second business ledger or a sensitive-data shadow.
Production rejects artifacts without verified digest, SBOM, provenance and signature.
```
