# Human Work, Forms, and Business Rules

This specification defines three business-interaction models that sit beside the [Workflow Execution Kernel](workflow-runtime.md). They are three models in one Go package, not three services: workflow structure, human work, data collection, and business decision logic evolve independently, but nothing in Phase 1 needs them deployed or versioned separately.

## Phase 1 Packaging

```text
internal/work/
  items      WorkItem, claim, complete            P1B
  approvals  ApprovalDecision bound to proposal   P1B
  reason     a typed reason field on approval     P1B  (no form engine)
  thresholds one decision table from tenant config P1A (no expression language)
```

P1B needs one approval WorkItem with a reason field and one threshold table
resolved at compile time. The queue, delegation, SLA, and escalation models,
the questionnaire engine, and the expression language are contracts only until
a second workflow family needs them. Their lifecycle in Phase 1 is
`DRAFT -> PUBLISHED -> RETIRED`; the full lifecycle below applies when the
`MANAGED` registry profile exists.

```text
                    WORKFLOW KERNEL
                          |
       +------------------+------------------+
       v                  v                  v
  HUMAN WORK            FORMS           BUSINESS RULES
 queues/tasks/SLA   questions/answers   expressions/tables
       |                  |                  |
       +------------------+------------------+
                          v
            typed decisions / artifacts / signals
```

The workflow coordinates. Human Work owns responsibility and completion. Forms own structured interaction and answers. Business Rules own deterministic customer logic. None owns canonical worker facts merely because it references them.

## Human Work Management

### WorkItem

```text
WorkItem

work_item_id
tenant_id
organization_scope

work_type
subject_refs[]

queue_expression
assignee_expression
resolved_queue_id?
resolved_candidates[]
assigned_principal_id?

status
priority
risk_class

input_artifact_refs[]
required_output_schema_ref
form_definition_ref?

due_at?
sla_policy_ref?
escalation_policy_ref?
delegation_policy_ref?

workflow_instance_id?
node_execution_id?
case_id?
correlation_id

created_at
claimed_at?
completed_at?
```

```text
CREATED -> ROUTED -> AVAILABLE -> CLAIMED -> IN_PROGRESS
                          |                        |
                          |                        +-> WAITING_INPUT
                          +-> ASSIGNED             +-> COMPLETED
                          +-> ESCALATED             +-> RETURNED
                          +-> EXPIRED               +-> CANCELLED
```

An approval is a specialized WorkItem whose output is an `ApprovalDecision` bound to an immutable proposal. A form does not become a task until a WorkItem assigns responsibility for completing it.

### Queues and Assignment

```text
WorkQueue

queue_id
scope
eligible_principal_expression
work_type_filter
claim_policy
assignment_strategy
capacity_policy
visibility_policy
business_calendar_ref
sla_policy_ref
```

Assignment strategies may be explicit, relationship/role resolved, round-robin, least-loaded, skill-qualified, geographic/timezone aware, or capability-resolved. They remain deterministic and explainable. Automated assignment cannot expand the candidate authority set.

```text
WorkItem created
      |
resolve queue + eligible candidates
      |
AuthZ / relationship / availability / capacity
      |
assign or expose for claim
      |
current-authority check at every material action
```

### Claim, Reassignment, Delegation, and Escalation

- Claim uses an atomic item version and optional lease to prevent double ownership.
- Reassignment preserves previous ownership and reason.
- Delegation uses the Platform delegation/proxy authority contract and may narrow but not expand scope.
- Escalation changes attention, priority, queue, or approver resolution according to workflow policy; it does not silently approve work.
- Separation-of-duties constraints apply to assignment and completion.
- Unavailable, departed, suspended, or unauthorized assignees trigger re-resolution rather than stranded tasks.

The [Messaging Plane](messaging-and-notification-plane.md) notifies assignees and returns delivery observations. It does not own assignment or SLA escalation.

### Completion

```text
WorkCompletion

work_item_id
item_version
completed_by
delegation_context?

output_artifact_ref
form_submission_ref?
decision_ref?
evidence_refs[]

authority_decision_ref
completed_at
```

Completion validates current authority, expected item version, required output schema, form version, evidence, separation of duties, deadline policy, and workflow correlation before emitting a typed workflow signal.

## Forms and Questionnaire Engine

### FormDefinition

```text
FormDefinition

form_id
version
name
purpose

subject_types[]
input_context_schema_ref
answer_schema_ref

sections[]
questions[]
repeat_groups[]

visibility_rules[]
required_rules[]
validation_rules[]
calculated_fields[]

attachment_policy
acknowledgement_policy?
signature_requirement?

locale_variants
classification
retention_policy_ref

status
effective_from
effective_to?
```

Supported question primitives should remain bounded:

```text
text | rich text display | number | money | date | date-time
boolean | single choice | multiple choice | lookup/reference
address | person | organization | file attachment | acknowledgement
```

Repeated groups, conditional visibility, requiredness, and validation compile into deterministic rules. Arbitrary browser/server code is prohibited.

### Form Registry (Gate C)

The Promotion-specific form todos `FORM-001`–`FORM-003` were retired on
2026-09-02 because Phase 1 needs only a typed reason field. The 2026-09-19
review of twenty HR workflows found that nine of them (hire, compensation
change, transfer, personal data change, life-event enrollment, timesheets,
requisitions, open enrollment, leave intake) cannot collect their input
without a form. The form engine returns at Gate C as a registry, not as a
separate platform:

- A `FormDefinition` is a registry entry of kind `FORM` stored in the tenant
  definition store, published through the shared lifecycle below and pinned by
  digest.
- A workflow `TASK` binds it through `form_ref`; the compiler checks that the
  answer schema is assignable to the node's declared output (`WF-EXT-013`).
- The same `TASK` spec carries a subject set, for worksheet and calibration
  forms over many subjects, and an assurance level, for step-up
  re-authentication on bank or identity changes.
- Rendering stays schema-driven in GoWebComponents; a new form never needs new
  page code.

### Render and Submission

```text
Form task
   |
resolve exact definition + locale + subject context
   |
field AuthZ + Legal + purpose + classification mask
   |
FormRenderPlan
   |
human answers + protected attachments
   |
server-side validation using same compiled rules
   |
FormSubmission (immutable)
   |
typed artifact / HumanTask completion / workflow signal
```

```text
FormSubmission

submission_id
form_id
form_version

subject_refs[]
respondent_principal_id
delegation_context?

render_context_hash
visible_question_ids[]
answer_artifact_ref
attachment_refs[]

validation_result_ref
acknowledgement_ref?
signature_evidence_ref?

submitted_at
supersedes_submission_id?
```

The system preserves what the respondent was shown, which questions were hidden, the exact locale/content/rules, answer revisions, and validation outcome. Form submissions are observations/claims until a separately authorized domain capability accepts them as canonical facts.

### Security and Accessibility

- Field visibility and editability are evaluated per respondent, subject, purpose, and current context.
- Hidden fields are not serialized to the client.
- Server validation is authoritative; client validation is usability only.
- Attachments pass the document ingestion/security pipeline.
- Sensitive repeat groups and conditional answers inherit explicit classification.
- Labels, errors, instructions, focus order, keyboard behavior, and assistive metadata are locale/version governed.
- A signature requirement invokes the document/e-signature subsystem; a checkbox is not a legal signature.

## Generic Business Rules and Decision Tables

Rules answer a typed question. They do not perform side effects or encode workflow topology.

```text
RuleDefinition

rule_id
version
name
purpose

input_schema_ref
output_schema_ref

rule_type
  EXPRESSION
  DECISION_TABLE
  FORMULA
  VALIDATION

body_ref
dependencies[]

effective_from
effective_to?
scope
owner
status
```

### Expression Subset

The language supports typed boolean/arithmetic/string/date operations, null-safe comparisons, bounded collection predicates, reference-data lookups, and named pure functions. It excludes network/database access, filesystem access, time/randomness without explicit inputs, unbounded loops, recursion, dynamic code loading, and agent/model calls.

Named pure functions come from a versioned, costed function library that is
itself a registry (`WF-EXT-016`). The first additions the workflow review
requires are list membership, date-interval overlap and business-day
arithmetic against a pinned calendar. Executing Manager Change through the
generic engine (`WF-EXT-008`) is the "second workflow family" that the phase
table below names as the trigger for this language.

```text
inputs + reference snapshots + rule version
                    |
                    v
              deterministic evaluator
                    |
                    v
typed result + trace + dependencies + warnings
```

### Decision Tables

```text
RaiseApprovalTable

inputs:
  raise_percent
  compa_ratio
  country

rows:
  country=US AND raise>10%       -> FINANCE_REQUIRED
  country=DE AND raise>8%        -> FINANCE_AND_HR_REQUIRED
  otherwise                      -> MANAGER_ONLY

hit policy:
  FIRST | UNIQUE | COLLECT
```

Compilation rejects ambiguous `UNIQUE` tables, uncovered required outputs, incompatible types, invalid effective intervals, cycles, unavailable reference versions, and formulas that violate bounded-computation limits.

### Rule Boundaries

```text
Workflow graph     HOW and WHEN work proceeds
Business rule      deterministic customer/business decision
Legal rule         law/policy obligation with legal provenance
AuthZ policy       whether a principal may act/access
Payroll/tax kernel specialized regulated numerical computation
Agent inference    derived hypothesis, never deterministic rule truth
```

A generic business rule cannot override mandatory Legal/AuthZ denies or reproduce regulated tax/payroll calculations merely because the expression language is capable of arithmetic.

## Shared Lifecycle and Promotion

Forms, rules, queues, and assignment policies use:

```text
DRAFT -> VALIDATED -> REVIEWED -> PUBLISHED -> ACTIVE
                                    |
                                    +-> QUARANTINED
                                    +-> DEPRECATED -> RETIRED
```

Publication includes schema compatibility, dependency impact, effective date, test fixtures, security/classification review, locale completeness where relevant, approval, immutable version, and rollback/forward plan. Running tasks/forms/rule evaluations remain pinned unless an explicit migration/re-resolution policy says otherwise.

## Capability Surface

```text
work.items.create
work.items.read
work.items.claim
work.items.assign
work.items.reassign
work.items.delegate
work.items.complete
work.items.cancel

work.queues.read
work.queues.publish
work.sla.evaluate

forms.definitions.validate
forms.definitions.publish
forms.render
forms.submissions.validate
forms.submissions.submit
forms.submissions.supersede

rules.validate
rules.simulate
rules.evaluate
rules.publish
rules.dependencies.explain
```

## Go-Only Implementation Shape

```text
internal/work/
  items/ queues/ assignment/ delegation/ sla/ projections/

internal/forms/
  definitions/ compiler/ renderer/ submissions/ validation/

internal/rules/
  definitions/ compiler/ expressions/ tables/ evaluator/ traces/

api/proto/work/v1/
api/proto/forms/v1/
api/proto/rules/v1/
```

Use Protobuf for contracts, PostgreSQL for durable state/versioning, SchemaFlux where useful for definition IR/generation, grpcbridge for web delivery, and GoWebComponents for accessible schema-driven task/form experiences. Prefer a small audited deterministic expression implementation over embedding a general scripting runtime.

## Phase Classification

| Capability                                   | P1A                       | P1B                                           |
| -------------------------------------------- | ------------------------- | --------------------------------------------- |
| Approval WorkItem: create, claim, complete   | **OUT**                   | **IMPLEMENT** (one item type)                 |
| Typed reason field on approval               | **OUT**                   | **IMPLEMENT**                                 |
| Threshold decision table from tenant config  | **IMPLEMENT** (read-only) | **IMPLEMENT**                                 |
| Queue, assignment strategy, reassignment     | **OUT**                   | **MINIMAL CONTRACT** (direct assignment only) |
| Delegation, SLA, escalation                  | **OUT**                   | **DESIGN / CONFORMANCE ONLY**                 |
| Form engine (sections, repeat groups, rules) | **OUT**                   | **DESIGN / CONFORMANCE ONLY**                 |
| Expression language and formula rules        | **OUT**                   | **OUT** until a second workflow family        |
| Customer-authored arbitrary formulas         | **OUT**                   | **OUT**                                       |
| Case/service-catalog specialization          | **OUT**                   | **OUT**                                       |
| Form registry bound to `TASK.form_ref`       | **OUT**                   | **OUT** (Gate C, `WF-EXT-013`)                |
| Expression function library registry         | **OUT**                   | **OUT** (Gate C, `WF-EXT-016`)                |

## Phase 1 Acceptance Contract

- Approval responsibility survives worker-process failure and recipient relationship changes.
- Two principals cannot claim an exclusive WorkItem concurrently.
- Completion rechecks authority, proposal/item version, reason presence, evidence, and separation of duties.
- The approval reason and decision are preserved with the exact proposal digest they were made against.
- Hidden/unauthorized fields never reach the browser or message template.
- The threshold table is deterministic, bounded, versioned with the tenant configuration, and explainable.
- A business rule cannot bypass AuthZ, Legal obligations, approval binding, or execution revalidation.
- WorkItem, FormSubmission, RuleEvaluation, workflow node, communication intent, and ledger evidence are traversable through correlation identifiers.
