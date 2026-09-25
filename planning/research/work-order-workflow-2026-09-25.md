# Work order workflow research (2026-09-25)

## Implementation checkpoint

The backend foundation now has the work order aggregate, version-pinned published templates, tenant-isolated journal/outbox storage, typed request and decision commands, work entries, progress and spending, report definitions, billing draft calculations, project task links, and authenticated RPC transport. A Riverside pilot template is available as a validated fixture. The API is composed only when the separate work order database is configured.

The workflow registration is a candidate and admits no live work order intent. Phase movement requires a completed runtime node bound by trusted evidence to the exact work order revision; the production binder, signed execution authority, and capability adapters are still pending. Customer pricing sources, invoice issuance, and a live Ironridge pilot seed are also pending. Until those sources are connected, their commands fail closed.

## Product boundary

A work order is a governed record of a defined piece of field work within an
Ironridge project or jobsite. It can span several project tasks. The work order
owns scope, planned and actual quantities, crew assignments, labor entries,
material and subcontract spend, evidence, acceptance, and revisions. A board
task is a coordination view or linked action; moving its column does not certify
work, approve spend, or close the order.

The workflow engine coordinates decisions and obligations for that record. It
does not store the work order's financial ledger, become the chat transcript, or
own deployed documents. Its business subject is a stable `WORK_ORDER` reference
in the project/field-operations authority domain.

## Current code findings

| Area                  | What exists                                                                                                                                                                                              | Gap for work orders                                                                                                                                                                                                                          |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Workflow kernel       | `internal/workflow/runtime.Instance` has generic business subject refs and durable state. The kernel specification defines typed capability, human task, approval, wait, signal, and compensation nodes. | No published work order plan or work order capability bindings.                                                                                                                                                                              |
| Execution composition | `internal/platform/execution.WorkflowRegistration` selects published plans and capability handlers.                                                                                                      | `StepRunFunc` takes a `promotionsteps.Runner`; registration and several authorization/projection paths still assume promotion or employment subjects. This must become a domain-neutral handler port before binding field-work capabilities. |
| Intent                | `intent.SubjectReference` supports kind, ID, and authority domain.                                                                                                                                       | Register a typed work order intent/subject and policy; resolve project and work order membership instead of treating a work order ID as a worker identity.                                                                                   |
| Projects              | A project task has configurable status, assignee, fields, comments, activity, and typed chat/document/Human Work links.                                                                                  | No work order aggregate, quantity acceptance, time entry, budget, or cost ledger. Project-task completion must remain separate from work order acceptance.                                                                                   |
| Labor                 | `internal/domains/labor` has exact decimal costing dimensions, rate/rule references, and worked-time allocation validation.                                                                              | It explicitly does not persist time or calculate authoritative payroll. A work order needs a source time entry, approval, and cost snapshot; payroll integration is a separate decision.                                                     |
| Collaboration         | Project links resolve protected chat posts and deployed documents under current target authorization.                                                                                                    | A work order needs typed outbound and backlink references with the same read-time checks and neutral restricted state. It should not copy protected chat or document bodies into a broad project record.                                     |

The project link contract currently admits only declared target kinds. Adding
`WORK_ORDER` as a board link requires a versioned allowlist change, a target
resolver, and tenant/project/work-order authorization. A work order can have
multiple crew members even though an ordinary project task has one assignee.
The served `ExecutionAuthority` currently admits only the promotion intent
under its signed P1B authority. A work order execution path needs an explicit
authority and intent-definition extension, not just another registry entry.

## Proposed record model

`WorkOrder` has tenant, project, jobsite/location, title, scope, priority,
requested-by, accountable supervisor, due window, lifecycle, revision, and a
published workflow-definition version. It links zero or more project tasks.
`WorkOrderLine` records planned work as a measured quantity and unit, with an
optional baseline budget. `Assignment` identifies the HCM worker (or separately
admitted contractor), role, and effective window. The HCM directory is the
identity source for employee assignments; membership and current eligibility
are checked at command time.

Append-only `WorkLog` entries record worker, local work date and timezone,
duration, task/line, source, submitter, approver, and correction link. They are
not payroll timesheets by default. `CostEntry` records category (labor,
material, subcontract, equipment, other), amount as exact decimal and currency,
quantity/unit when relevant, commitment versus actual, source document,
approver, and correction/reversal. Labor cost uses an approved time entry and a
versioned rate snapshot; later pay-rate changes do not silently rewrite prior
cost. Cost readers can see authorized totals without receiving individual pay
rates. `ProgressEntry` records completed quantity, evidence, verifier, and
acceptance state. A change order revises scope, quantities, price, and schedule
with its own approval trail; it never mutates the original baseline in place.

Report planned, committed, incurred, approved, and remaining cost separately.
Report submitted, verified, and accepted progress separately. A percentage is
derived from accepted quantity against the current approved scope, with an
explicit "not measurable" state for work lacking a quantity basis. Workflow
node completion is not a progress percentage.

## Initiator requests and work order journal

The initiator can be a project member with `create_work_order` authority; the
accountable supervisor may be a different person. Creation pins a published
work order template and opens a draft. The initiator can submit requests from
the template's catalog whenever its phase policy allows. A request is a durable
child record with a stable ID, work order ID/revision, kind, form/schema version,
requester, business subject, rationale, attachments, expected outcome,
idempotency key, timestamps, and independent status. It may create a Human Work
item or a child workflow, but it is never just a chat message or a mutable
form blob.

| Request kind                 | Required content                                                                           | Authorized outcome                                                                                                                         |
| ---------------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------ |
| Budget or budget change      | Amount, currency, cost category, funding source, baseline/revision, reason and evidence    | Approve, reject, or return for revision; approval raises a versioned spending limit or reservation and never records an expense by itself. |
| Crew, material, or equipment | Quantity/role, location, dates, estimated cost and source                                  | Assign or procure through the owning capability; an accepted request does not imply delivery or worked time.                               |
| Approval or exception        | Subject, exact proposed action, policy, approver class, deadline and evidence              | A scoped decision bound to that proposal revision. The requester cannot approve their own controlled action.                               |
| Inspection or document       | Inspection scope or document type, due window and evidence policy                          | A Human Work obligation and protected artifact/reference; inspection result records its verifier.                                          |
| Change order                 | Scope delta, quantity/price/schedule delta, original baseline, reason and affected records | A new approved baseline revision, retaining the original and all prior costs.                                                              |
| Billing review               | Billing period, contract/pricing version and source cutoff                                 | An approved billing draft that can be issued or exported through a billing capability.                                                     |

The work order journal is append-only and records phase changes, requests,
decisions, work logs, progress, costs, corrections, and document/chat links as
typed events. Participants may add a note with author, time, visibility,
classification, optional line/phase anchor, and protected attachments. A note
can mention or link to chat, but cannot by itself approve money, accept work, or
advance a phase. Editing a material note adds a revision; removal follows
records policy. The activity view merges journal entries and permitted linked
previews without copying protected bodies or revealing restricted targets.

## Modular published configuration

Use a `WorkOrderTemplate` bundle for each job type or customer variant. It
contains stable phase IDs, a phase-to-compiled-node projection, request catalog
and typed forms, approval policy references, capability/connector bindings,
required evidence, spending thresholds, report definitions, billing policy,
role grants, and time/calendar rules. Each component carries a version and
digest. A customer may configure declared slots such as approver classes,
thresholds, optional inspection, field labels, report layout, and permitted
pricing mode. They cannot insert arbitrary executable code or remove mandatory
authorization, safety, reconciliation, or closure gates.

The existing workflow kernel's typed nodes and `extensionregistry` manifest
contract can support these modules. Authoring expands templates and reviewed
fragments into a compiled plan before publication. Validation checks schema
bindings, referenced capabilities, reachable phase paths, separation of duties,
required evidence, idempotency, effect class, and billing source rules. Publish
produces an immutable version; a running work order keeps its pinned plan.
Configuration changes show a diff and affected-run preview. Migration of an
active order needs an explicit safe-point decision and audit record. Current
permissions and business validity are still rechecked at action time.

Visible phases are a work order projection over the pinned process, with
stable IDs such as `DRAFT`, `AUTHORIZATION`, `READY`, `EXECUTION`,
`INSPECTION`, `ACCEPTED`, and `CLOSED`. Each phase declares allowed exits,
required requests/decisions/evidence, authorized actors, and deadline behavior.
The work order service changes its business phase only after the workflow's
guarded outcome commits. A project board may map those phases into columns;
moving a card does not execute a phase transition. Optional or repeated phases
(for example rework or budget revision) are compiled from reviewed template
slots, with bounded cycles and a visible audit trail.
Billing runs as an optional governed branch after an eligible source cutoff,
with its own Draft, Approved, Issued, and Reconciled status. Operational work
can be accepted while invoicing remains pending; payment does not retroactively
change whether field work passed inspection.

The initiator or current phase owner advances through an explicit
`RequestPhaseTransition(work_order_id, expected_revision, target_phase_id,
reason, evidence_refs, idempotency_key)` command. The command returns the
fulfilled transition, outstanding requirements, or a typed refusal. A pending
approval keeps the current phase in place and creates a visible Human Work
item. Completion of that item rechecks the work order revision and current
authority before advancing. Notes and chat activity never trigger an implicit
phase move.

## Reports and billing

Reports are governed projections of work order records, with definitions pinned
to a version, source cutoffs/watermarks, currency and timezone, completeness
state, and current reader authorization. Start with a daily field report,
planned-versus-accepted quantity, budget/commitment/incurred-cost variance,
open requests and approvals, and a closeout evidence bundle. A report can be
regenerated from retained source revisions and identifies any missing or stale
input. Existing versioned reporting contracts provide a starting point; work
order sources and access rules still need implementation.

Billing has its own authority and record model. A `BillingDraft` references a
customer contract and pricing-policy version, billing period, approved scope
revision, line-level work/cost evidence, calculation trace, currency, tax and
retainage treatment, and source cutoff. The policy selects billable sources by
contract mode: accepted quantities for unit price, approved time/material for
time-and-material, or accepted milestones for fixed price. Incurred internal
cost alone never makes a line billable. A reviewer can adjust or reject a draft
with a reason; approval seals immutable line snapshots. Issuance creates an
invoice document and/or dispatches to an accounting connector with an
idempotency key. Reconciliation records the external invoice ID and status;
credit, void, and rebill use linked corrective records, never edits to an
issued invoice. Payment collection and the accounts-receivable ledger remain
with the finance system unless separately brought into scope.

The current `internal/commercial.InvoiceEvidence` is for a platform contract's
external/manual invoice receipt and explicitly does not generate invoices. It
must not be reused as a project billing engine. The first work order slice can
generate an auditable billing draft and export; invoice issuance follows once
the customer contract, pricing, tax, and accounting ownership are defined.

## Initial workflow

1. **Draft and scope:** an authorized initiator creates the order, work lines,
   budget requests, location, and initial evidence; validation checks tenant,
   project state, units, and rate/currency precision.
2. **Review and authorize:** accountable approvers accept scope, spending
   authority, safety prerequisites, and crew eligibility. A rejected order
   returns to draft with a reason and revision; an approved revision is pinned.
3. **Release and execute:** assign crew and dates. Laborers submit daily logs,
   quantity progress, photos/documents, and issues. A work order discussion
   points to a protected chat thread; workflow signals wake on accepted records,
   blockers, due times, and change requests. State notices use the messaging
   outbox and current audience policy; they cannot reveal cost in a broad
   channel. Field clients retain drafts through network loss and submit with
   stable idempotency keys when connected.
4. **Control changes:** material scope or budget changes use a change-order
   approval. Recorded labor and purchases remain visible against the prior and
   revised baseline; overrun alerts do not silently authorize spend.
5. **Inspect and accept:** supervisor verifies work and costs; inspector or
   customer accepts deliverables where required.
6. **Close and bill:** operational closure checks unresolved blockers,
   unapproved spend, missing evidence, and required documents. For billable
   orders, a separate branch produces a source-backed billing draft and obtains
   billing approval before issue/export. The work order shows both dispositions
   independently. Reopen creates an audited successor/revision rather than
   erasing closure.

Suggested visible states are Draft, Awaiting approval, Ready, In progress,
Blocked, Awaiting inspection, Accepted, Closed, and Cancelled. These are work
order business states, not a copy of the runtime instance status or Kanban
columns. A published nonbillable template omits the billing branch entirely.

## Ironridge pilot case

The seeded Riverside project gives a concrete first scenario. `RIV-14` records
the RFI-014 stair-stringer embed conflict, `RIV-27` records the CO-03 redesign
cost and approval, and `RIV-10` coordinates material release. A Riverside stair
stringer work order could link all three tasks, the RFI log document, and the
leadership discussion. It would remain blocked until the revised dimensions
and CO-03 approval are current. The crew would then record hours, material
receipts, installed quantity, and inspection evidence against that one work
order. Completion would require accepted installation and reconciled spend;
the three board cards' Done status alone would not satisfy those conditions.

This case also tests revision handling: the architect-directed redesign changes
the approved scope and budget while preserving the original takeoff and every
cost logged against it. A change order is therefore a governed successor to a
baseline, not an edit to a single budget field.

## Engine extension boundaries

The kernel already has durable tasks, approvals, waits, signals, and typed
capabilities. The highest-risk change is in composition: replace the
promotion-specific `StepRunFunc` runner parameter with a capability-dispatch
port whose implementation belongs to each registered workflow. Keep promotion
handlers behind an adapter and prove their existing behavior unchanged before
adding a field-work registration. Approval routing should resolve the current
work order supervisor, finance approver, and optional inspector from the work
order's project and jobsite authority, rechecking them when the decision is
made. It must not reuse the promotion path's employment-manager fallback.
The existing `PromotionExecution` composition and `PromotionPlan` selector can
remain as a compatibility adapter while a general execution composition serves
multiple domain registrations. The new route also needs an authorized work
order fact resolver for simulation/revalidation and a work-order-specific
terminal outcome, rather than calling a promotion terminal writer.

One long-lived workflow instance may coordinate an order, with child instances
or separate governed intents for material change orders and rework. Daily work
logs and cost entries remain work-order commands; they emit deduplicated events
that the workflow can observe. They do not each create a new workflow run.
Signals should correlate by the stable work order subject ID and expected
record revision. A replayed step or late event must not duplicate a time entry,
charge, approval, notification, or chat post.

## Architecture and delivery sequence

1. Define work order invariants, commands, events, append-only correction
   records, permissions, and typed API. Build a manual end-to-end pilot with
   work logs, spend entries, progress, and project/chat/document links.
2. Generalize the execution composition behind a domain-neutral capability
   handler interface, keeping the existing promotion registration behavior and
   conformance tests intact. Register `WORK_ORDER` intent and project-scoped
   authorization with a pinned workflow version.
3. Add workflow steps for release, assignment, evidence request, spend/change
   approval, inspection, and close. Use idempotent commands, durable signals,
   outbox delivery, and revision checks; replay must not duplicate cost entries
   or chat posts.
4. Add board and My Work projections that distinguish project tasks, workflow
   Human Work items, and work orders. Each source remains authoritative for its
   own state. Show cost only to finance/supervisor roles with current authority.
5. Add pinned work order report definitions and source-backed billing drafts;
   connect invoice issuance only after contract and accounting ownership are
   agreed. Prove every billed line traces to a billable source and pricing
   version.
6. Qualify cross-tenant denial, revoked worker/project/document access,
   concurrent edits, duplicate signals, correction/reversal accounting,
   workflow restart/replay, and mixed project/workflow load before rollout.

A configuration pilot should publish two distinct work order templates using
the same engine (for example construction change work and a service call),
exercise a budget request and approval in both, and show an optional inspection
phase plus different billing policies. A new publication must leave running
orders on their pinned versions. The pilot fails if an unauthorized note or
board move advances a phase, an unapproved cost becomes billable, a repeated
signal duplicates an invoice line, or a report cannot identify its source
revisions and completeness.

## Decisions to settle before implementation

- Is a labor entry for operational costing only, or an authoritative payroll
  timesheet? The recommended first slice is costing only; payroll export needs
  explicit pay-period, overtime, union, and correction semantics.
- Who can enter and approve material/subcontract costs, and which system owns
  purchase orders and actual payment? The recommended first slice records
  commitments and incurred cost without claiming to post to accounting.
- Which customer contract and pricing modes apply to each job, and which
  system issues invoices and owns accounts receivable? Billing draft generation
  can begin with explicit contract versions; issuing an invoice requires an
  agreed finance boundary and tax/retainage policy.
- Which jobsite roles can accept completed work, and when is customer signoff
  required? This determines the closing approval path and evidence policy.
- Can external subcontractors participate in an Ironridge work order? Tenant
  guest identity, bilateral access, and cross-company data policy would need a
  separate scope decision.
