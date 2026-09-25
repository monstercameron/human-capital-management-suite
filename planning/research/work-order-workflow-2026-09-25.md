# Work order workflow research (2026-09-25)

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

## Initial workflow

1. **Draft and scope:** supervisor creates the order, work lines, budget,
   location, and initial evidence; validation checks tenant, project state,
   units, and rate/currency precision.
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
5. **Inspect and close:** supervisor verifies work and costs; inspector or
   customer accepts deliverables where required. Closure checks unresolved
   blockers, unapproved spend, missing evidence, and required documents.
   Reopen creates an audited successor/revision rather than erasing closure.

Suggested visible states are Draft, Awaiting approval, Ready, In progress,
Blocked, Awaiting inspection, Complete, and Cancelled. These are work order
business states, not a copy of the runtime instance status or Kanban columns.

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
5. Qualify cross-tenant denial, revoked worker/project/document access,
   concurrent edits, duplicate signals, correction/reversal accounting,
   workflow restart/replay, and mixed project/workflow load before rollout.

## Decisions to settle before implementation

- Is a labor entry for operational costing only, or an authoritative payroll
  timesheet? The recommended first slice is costing only; payroll export needs
  explicit pay-period, overtime, union, and correction semantics.
- Who can enter and approve material/subcontract costs, and which system owns
  purchase orders and actual payment? The recommended first slice records
  commitments and incurred cost without claiming to post to accounting.
- Which jobsite roles can accept completed work, and when is customer signoff
  required? This determines the closing approval path and evidence policy.
- Can external subcontractors participate in an Ironridge work order? Tenant
  guest identity, bilateral access, and cross-company data policy would need a
  separate scope decision.
