# Customer Project Management and Adaptive Boards

**Status:** DRAFT_CONTRACT  
**Owner:** Project Collaboration Product owner, with Workflow, AuthZ, AI/Agent, Data, Experience, and Reliability review  
**Started:** 2026-09-21  
**Review trigger:** Before roadmap promotion, project persistence/API implementation, AI configuration publication, cross-company projects, or migration of a live board

## Decision and product boundary

HCM Next may offer a small-business project layer that turns conversation and
documentation into assigned, visible work. Customers can configure project
workflows and board views manually or ask AI to propose and refine them for
their business. A board is a saved view of project tasks; a method such as
Scrum or Kanban adds behavior beyond column names. The product should be
usable by any team, not only software engineering.

This is a proposed product workstream, not a current served capability or an
automatic expansion of Phase 1 or the chat/document release gates. The
[company chat plan](company-chat-and-collaboration.md) currently puts task
boards under later product evidence. The [execution plan](../execution-plan.md)
still governs near-term commitments. Promotion requires a scope exchange,
budget, owner, and acceptance gate.

The first customer wedge is a non-engineering team in an organization with
fewer than 1,000 employees coordinating recurring operational work. The proof
case is one team publishing a board, assigning work, resolving overdue items,
and linking the governing chat or document. The first useful release includes
tasks, owners, due dates, comments, configurable statuses, typed custom fields,
a board and list view, search, and chat/document links. It does not promise a
general workflow builder, arbitrary code, cycles, WIP enforcement, portfolio
planning, resource scheduling, time billing, a software development suite,
cross-company projects, or AI-controlled project changes. It must be usable
with AI disabled. AI setup is a separate pilot slice after manual boards work.

### Why this belongs in the suite

- Chat provides discussion; documents preserve guidance and decisions; a
  project makes deliverables and ownership visible. Linking them reduces the
  need to copy context into separate tools.
- The [Human Work](human-work-forms-and-rules.md) model already owns governed
  HCM obligations and approvals. The project layer supplies ordinary team
  execution tasks and can display a reference to an HCM WorkItem without
  gaining authority to complete it.
- A small company can start with a simple task board and add process only
  when it needs it. Templates offer defaults, not immutable methodology.

The first release makes those links usable in both directions. An authorized
participant may start a project task from a chat post or deployed document
passage, retaining only a typed source reference and an explicitly entered
task description. After the task is created, an authorized participant may
share its stable link into a chat conversation or document candidate through
that surface's own write permission and review path. Neither action copies
protected source text into a broader project task by default, grants access
to the source or task, or mutates an official deployed document. Restricted
targets render a neutral unavailable state. These are in-app routes that
preserve browser Back/Forward and the persistent shell without a full-page
reload.

Current products illustrate the distinction: Linear separates issues,
projects, cycles and saved views; Jira maps board columns to workflow status;
Asana permits natural-language instructions in workflow configuration. These
are reference patterns, not claims that HCM Next implements those products.
[Linear concepts](https://linear.app/docs/conceptual-model),
[Jira columns](https://support.atlassian.com/jira-software-cloud/docs/configure-columns/),
[Asana AI Studio](https://help.asana.com/s/article/ai-studio?language=en_US).

## Users and reference journeys

| User                         | Job to complete                                | Expected path                                                                                                                                                                                           |
| ---------------------------- | ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Owner or team lead           | Set up a customer onboarding or launch project | Choose a template or describe the work; inspect statuses, fields, permissions and sample cards; publish a configuration.                                                                                |
| Employee                     | Know what to do next                           | Open My Work or a project board, see assigned tasks, update status, comment, and link the relevant chat/document.                                                                                       |
| Project manager              | See blocked work and due dates                 | Use list/board filters, manual blocked and overdue indicators, and an activity history that distinguishes edits from AI suggestions; dependency indicators arrive with the later dependency capability. |
| HR operator                  | Coordinate an onboarding plan                  | Link governed HCM WorkItems; project cards show their safe status and open the owning HCM action when authorized.                                                                                       |
| External collaborator, later | Participate in one shared project              | Join only after host and home-company policy admit them; see an explicit project scope, not employee records by inheritance.                                                                            |

First reference scenarios: a general team task board, a product discovery
board, an operations/request board, and an employee onboarding coordination
board. Software engineering teams can use the same model with bug/feature
types, then add cycles and releases when those features exist.

## Vocabulary and authority

| Concept                  | Owner and meaning                                                                                                                                                                                                                    |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `Project`                | Project module owns a tenant-scoped outcome, owner, membership policy, `project_timezone`, lifecycle, and current configuration version.                                                                                             |
| `ProjectTask`            | Project module owns ordinary work with stable ID, title, description, status, assignee, due date, priority, type, typed fields, and revision. It does not authorize HCM mutations.                                                   |
| `ProjectWorkflowVersion` | Immutable published status/type/field/transition configuration. A draft may change until published; publication creates a new version.                                                                                               |
| `BoardView`              | Saved filter, optional swim-lane grouping, status-to-column mapping, ordering, card fields and audience (`PERSONAL` or `PROJECT`). Multiple views may show the same tasks; a view never widens task access.                          |
| `Cycle`                  | Optional, dated planning bucket for tasks. Required only when a template promises sprint/cycle behavior.                                                                                                                             |
| `ProjectLink`            | Typed reference to a chat conversation/post, deployed document, or safe HCM WorkItem projection in the first release; later target types require an explicit allowlist revision. It never conveys the target's read or write rights. |
| `HumanWork.WorkItem`     | Existing workflow-owned responsibility, approval, evidence request, or HCM task. Project boards may show a policy-filtered projection; only Human Work completes it.                                                                 |

Identifiers are stable across rename and view changes. Every mutation records
actor, origin (`HUMAN`, `APP`, `AGENT`), time, prior/new revision, and the
configuration version used. A board column ID is stable across label changes.
`ProjectTask` and `HumanWork.WorkItem` are separate identities; a link between
them is explicit, typed, and cardinality-bounded.

### Lifecycle, relationships, and My Work

`Project` supports `ACTIVE <-> SUSPENDED`, `ACTIVE -> ARCHIVED -> ACTIVE`,
and `SUSPENDED -> ARCHIVED`. An owner may archive/restore; a scoped operator
may suspend during an incident or records hold, recording reason and release
authority. Suspension blocks task/configuration writes while authorized reads,
search and mandatory records access remain available. New project activity
notices pause, but already committed outbox events still reconcile. Authorized
archive and records-authority operations remain available during suspension.
Deletion
is a records-governed disposition, not a UI hard delete. `ProjectTask` moves through configured statuses; archive
and restore are separate record-state actions that retain task history and
links. Archived projects and tasks are absent from ordinary search but remain
available to authorized records/admin queries.

In the first release each task has one owning project and zero or one assignee.
Comments and activity belong to the project task. Task checklists, parent/
subtask hierarchy, recurring schedules, and dependency graphs are later
capabilities with separate acceptance tests. `BLOCKED` is initially a manual
status/category, not a computed dependency claim. Later `BLOCKS` edges must
be typed, directed, cycle-checked and scoped to authorized projects; they
cannot complete or reopen a task automatically.

`ProjectTask` is canonical only for ordinary project execution state.
`HumanWork.WorkItem` remains canonical for assignment, claim, completion,
SLA, escalation, evidence, and governed HCM outcomes. A board may show a
read-only WorkItem reference with ID, observed version, safe status and
freshness. The WorkItem owner updates that projection through an authorized
event adapter with correlation and idempotency; it never accepts a project
card move as a WorkItem command. A linked project task can track additional
coordination work, but its completion does not imply HCM completion. The
project module owns project-task assignment/status notices; Human Work owns
WorkItem obligations and mandatory HCM notices. `My Work` extends the existing
surface as a combined read model with visibly different task kinds and
actions, not a third owner of completion state. A stale or revoked WorkItem projection becomes a neutral
unavailable reference until refreshed; it must not retain protected detail.

## Board families from one model

| Named board                        | Configuration required                                                                      | Delivery classification                                                                                       |
| ---------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| Task board                         | Simple status flow and assigned tasks                                                       | First release preset.                                                                                         |
| Kanban                             | Continuous flow, configurable columns, WIP limits and cycle-time reporting                  | First release offers a basic continuous-flow board; Kanban metrics and WIP policy are later capability gates. |
| Scrum                              | Backlog, cycle/sprint scope and dates, planning/close behavior, carryover and cycle reports | Later capability, then preset.                                                                                |
| Scrumban                           | Optional cycles plus Kanban WIP and flow                                                    | Later composition of proved cycle and WIP capabilities.                                                       |
| Feature, bug, discovery, milestone | Task type, fields, filters, status mapping and optional milestones                          | Later presets over the common model; a milestone view does not claim release/deployment authority.            |
| Release                            | Release object, scope, verification and deployment integration                              | Later capability; no first-release preset that implies release execution.                                     |
| Support/operations                 | Intake link, priority, queue owner, optional SLA and confidential route                     | Later integration with employee requests; SLA and incident semantics remain with their owning capability.     |
| Program/portfolio                  | Cross-project hierarchy, dependencies and rollups                                           | Later product workstream.                                                                                     |

One project may have several saved views. A board may initially show one
project; cross-project views require separate authorization and performance
qualification. Views never own task status or silently rewrite workflow rules.

### Columns and swim lanes

Columns are a view mapping from stable workflow status IDs, not independent
task states. A column may display several statuses; every visible status maps
to exactly one column in that view. Changing a column label or order does not
move tasks or change allowed transitions. A drag or keyboard move to a column
with several statuses must choose a valid target status explicitly.

The first board release offers an optional second grouping axis: assignee,
priority, task type, or a visible project `ENUM` field. Its lane IDs are
stable underlying value IDs, with an explicit Unassigned/Unset lane. Lanes
are derived only after task authorization and board filtering, including
their counts and empty states. Grouping is part of a versioned personal or
project view; it does not grant task, field, chat, or document access. The
board remains bounded by the same 100-card page limit across all lanes, not
100 cards per lane. Pagination preserves a declared total order and reports
when a lane may have more authorized cards.

Moving a card between lanes changes the underlying grouping field only when
the viewer may edit that field. A combined column-and-lane move validates
both the status transition and field edit against the current task and
configuration revisions in one project-owned transaction; rejection restores
the card and focus. A read-only lane or a derived future grouping cannot be a
drop target. Every drag action has a keyboard equivalent and announces the
result to assistive technology. Narrow screens may present lanes as a
switchable list, while retaining every move and task-detail action.

## Customer configuration contract

The first-release configuration vocabulary is: task types; status IDs and
names; status categories (`NOT_STARTED`, `ACTIVE`, `BLOCKED`, `DONE`,
`CANCELLED`); allowed transitions; required fields per type/transition; typed
custom fields (`TEXT`, `NUMBER`, `DATE`, `ENUM`, `PERSON`, `LINK`, `BOOLEAN`);
board filters; and column mappings. WIP rules, cycle cadence, and automation
are later versioned extensions, each with a separate capability gate. Each
custom field has a stable ID, type, validation, classification, default,
index/search policy, and retirement state. In the first release all custom
fields inherit task visibility; confidential field-level grants are deferred
and confidential HCM values are refused on ordinary project tasks. Limits on
field count, enum size, filter complexity, and tasks per query are tenant-plan
settings with published defaults and observable rejection errors. The pilot
proposal is at most 25 active custom fields and 20 saved views per project;
the load gate may revise these before publication.

Manual configuration and AI proposals compile to the same validated
`ProjectWorkflowVersion` schema. No arbitrary SQL, scripts, opaque executable
rules, or customer-supplied code runs in the task write path. Project-specific
fields cannot be treated as new canonical employee, payroll, legal, or other
HCM facts. Restricted HCM data is referenced through authorized capabilities,
not copied into a broad project task by default.

First-release due dates are local calendar dates in an explicit project
timezone. A task is overdue after that date passes in the project timezone
unless its status category is `DONE` or `CANCELLED`. A timezone change
previews the effect on open tasks and reminders before publication. Business
calendars, working hours, time-of-day deadlines, and SLA clocks are later
capabilities; a due date is not a legal or HCM deadline.

### Configuration lifecycle

```text
DRAFT -> VALIDATED -> REVIEW_REQUIRED (if policy requires) -> PUBLISHED
                                                       PUBLISHED -> SUPERSEDED -> RETIRED
```

Only a principal with `CONFIGURE_PROJECT` may submit/publish. Independent
review is required by tenant policy and for classification downgrades,
status/field removal, new required fields affecting
existing tasks, or a migration above the configured impact threshold. The
publisher cannot approve their own change when review is required. Publication
uses the exact reviewed digest, expected revision, and idempotency key.
Preview reports status/field/permission diffs, affected task IDs/counts,
invalid values, target mappings, notification effects, estimated duration,
and a rollback-forward action. A new required field must provide a valid
default or leave old tasks under a compatible version until corrected;
otherwise publication is rejected. A removed status needs an explicit mapping
for every active task, and changing a status category requires impact review.
Field type changes require a validated conversion or a new field ID. History
retains the version used for each task transition.

First-release migrations are bounded to an atomic project-owned transaction;
a preview above the published affected-task limit is rejected. The proposed
pilot limit is 1,000 affected tasks per publication. A later large-migration
capability may enter a visible `MIGRATING` state: affected writes are fenced
by migration epoch, batches are idempotent and resumable, and the new version
activates only when every task validates. Failure leaves the prior version
active with a repair/retry path; mixed versions are never silently interpreted
as one version. Rollback is a new publication, not deletion of history.
Archived projects may not publish until restored. Configuration drafts survive
failed validation without affecting current task writes.

Task moves call `MoveTask(task_id, target_status_id, expected_task_revision,
expected_config_revision, idempotency_key)` with an optional typed lane-field
edit. The server checks current task and config revisions, authorization,
transition rules, lane-field edit permission, and required fields in one
project-owned transaction. Once WIP or dependencies are enabled, their
published rules join this same decision. The accepted transition and outbox
event commit atomically.
The UI never treats drag-and-drop as success before that response. Rejected
moves return a reason and restore the card position.

## AI-assisted board design and refinement

AI is a proposal author with scoped read access, not a configuration
publisher. The same process is available through forms/manual editing.

1. The user describes the team, work types, desired flow, sensitive data,
   and decision rights. AI may ask a small number of clarifying questions
   and must state assumptions. If asked for cycles or WIP, it explains which
   capabilities are available before proposing a configuration.
2. AI emits a typed `BoardProposal`: workflow draft, fields, board views,
   example tasks, rationale, and unknowns within currently enabled capabilities. It may
   cite only documents and chat context the requesting user and installation
   currently may read. Retrieved text is untrusted input, not instructions.
3. A deterministic validator rejects unsupported fields, illegal transitions,
   unsafe sharing, duplicate IDs, unbounded rules, hidden required fields,
   and methodology claims unsupported by the proposed behavior.
4. The preview shows a plain-language summary and exact diff, sample cards,
   permission matrix, migration impact on current tasks, and anticipated
   notifications/automation. A human with current authority edits and
   publishes the validated version.
5. Subsequent AI refinement creates another draft against the current
   configuration revision. Stale drafts need rebase and renewed preview.
   Agent suggestions may be accepted one at a time; no silent publication.

Prompt, model version, cited input IDs, output schema version, validation
result, human decision, and published configuration digest are audit-linked.
Do not store raw confidential source text in a broadly accessible project
trace. The tenant can disable AI setup, control retrieval sources and agent
capabilities, and remove an agent's access without breaking manual boards.
The AI setup pilot uses a tenant-approved provider, region/residency, data
retention, logging, and cost policy. Prompt text, retrieved excerpts, traces,
and output are classified separately from project content; raw confidential
source text is not retained by default. The requester and agent installation
must both retain source access at generation and preview. Before publish,
the server rechecks cited source IDs/revisions, current access, draft digest,
and the publisher's authority. Revoked or newly restricted sources invalidate
the entire proposal and require regeneration from currently authorized
material. Provider outage, quota exhaustion, malformed output, or policy
rejection returns a typed non-material error and leaves the current board
unchanged. Per-tenant token, concurrency, and spend ceilings stop new AI runs
without stopping manual configuration. Example tasks in the preview are
visibly synthetic and cannot become live tasks without a separate explicit
selection. AI-generated task edits and summaries are outside the first
release. Material HCM actions always use the existing BusinessIntent and
workflow authority.

## Permissions, privacy, and cross-company scope

Project roles begin with `OWNER`, `MANAGER`, `CONTRIBUTOR`, and `VIEWER`,
combined with tenant policy and explicit action capabilities: read project,
read task, create/edit/move task, comment, manage members, manage views,
configure workflow, publish configuration, export, and archive. A role is a
starting grant, not a bypass of classification, tenant, purpose, or explicit
deny. Private projects do not appear in search, counts, notifications,
mentions, AI retrieval, or API listings for unauthorized users. A project
link, copied URL, board filter, or chat mention never grants membership.
New projects are private to the creator until explicit tenant-internal grants
are accepted. `OWNER` transfer requires a current owner or scoped recovery
authority, records both parties and leaves at least one active owner. Managers
may invite only within the project's classification ceiling. Admins do not
automatically read private task bodies; recovery access is purpose-bound,
time-limited, audited, and separately authorized. First-release tasks inherit
project readership; if a team needs a confidential subset, it uses a separate
restricted project or the owning HCM surface. Per-task and per-field sharing
are later features with their own leak tests.

The first release supports tenant-internal projects. A future cross-company
project requires bilateral admission, a host tenant, per-company delegated
membership policy, data classification/egress agreement, revocation and
retention/export behavior before activation. Sharing a chat channel with an
external person does not auto-share a project or its tasks. Sensitive HR,
employee case, pay, medical, and security work should live in separately
restricted HCM surfaces; the project layer may link a safe projection when
authorized, never copy protected evidence into an ordinary card.

All linked content is checked at read time. A viewer lacking document or
chat access sees a neutral restricted-link state without title, snippet,
participant names, or existence-revealing counts. Revocation invalidates
search, cached cards, notifications, API cursors, and agent retrieval.
Task comments and attachments follow task access and records policy; file
bytes use protected artifact storage with malware/DLP checks.
The first-release link allowlist is chat conversation/post, deployed document,
and a safe Human Work reference. Arbitrary URLs are plain sanitized task
content, never an authority-bearing `ProjectLink`. Cross-project task links,
private document drafts, and cross-company references require later policy.

Assignment, mention, due-soon, overdue, and relevant task-change notices use
the [Messaging and Notification Plane](messaging-and-notification-plane.md).
Project owns the triggering task event and per-user preference; Messaging
owns delivery, retry and channel policy. Mandatory HCM notices retain their
own policy even when shown in the same My Work feed. Revocation suppresses
future project notices and strips protected content from pending delivery.
Project mute and quiet hours may suppress or defer optional project notices;
they never override mandatory HCM notices. The more
restrictive current audience policy governs each delivery.
Project records series cover configuration drafts/publications, tasks,
comments, membership, activity, attachments, AI proposal evidence, exports,
holds and disposition under the existing
[Records Management](records-management-and-disposition.md) authority.
Archive hides ordinary views without erasing held
records. Exports obey the requester's current field and project access.

## Data, API, and performance boundaries

Project owns its tasks, configuration versions, board views, memberships,
activity history, and outbox. It does not write chat messages, document
deployments, Human Work items, or BusinessIntent state directly. The first
deployment runs the project module in the core Go binary with an isolated
project schema, database role, bounded connection pool, migrations and outbox
in the core PostgreSQL instance. No serving path uses cross-domain joins or
foreign keys; cross-domain references resolve through capabilities and
authorized events. This choice is conditional on the mixed-load pilot gate:
zero missed workflow deadlines and at most 5% regression in workflow p95/p99
latency against the same workload without project traffic. If the gate fails,
project persistence moves to its own logical database and pool before wider
rollout; if shared CPU/I/O remains the cause, it moves to separate database
capacity. Extraction preserves stable IDs, revision semantics, event cursors,
backup/restore and API behavior, and has a rehearsed copy/verify/cutover
plan. The project has no claim on workflow workers, locks, or outbox capacity.

Browser operations use typed Protobuf/gRPC over the existing browser RPC
transport. The integration HTTP API projects the same application operations
and policy checks via the [endpoint contract](http-grpc-endpoint-contract.md).
Each public method needs a generated endpoint manifest entry naming
capability, purpose, scope, classification, idempotency, expected revision,
pagination/cursor, budget, evidence, version and gRPC/HTTP parity tests.
The first-release surface is create/get/list/archive/restore project and
update project settings, including `project_timezone`, with revision/audit;
get/manage project membership; create/get/list/update/archive/restore task;
move/assign/comment task; list task activity; get/validate/preview/publish
configuration; create/update/list board views; create/remove allowlisted
links; and bounded task search. Attachment upload/download, import/export,
AI proposal, long-running migration status, cycles, dependencies and
automation are added only with their owning delivery slice. Every mutation
uses expected revision and idempotency; reads use bounded pages and stable
cursors. API clients cannot submit custom scripts or generic patches to HCM
facts. Webhooks/pull events use a project outbox with versioned task/project
event envelopes, source revision, event ID, tenant, classification, and
replay/deduplication policy. The project sequence orders events within one
project; delivery is at least once, so consumers deduplicate by event ID and
accept compatible schema versions. The retention window and replay cursor
expiry are published in the endpoint manifest. AI uses the same capabilities under its own
identity and installation scope.

Membership operations distinguish invite, accept, change role, remove, and
owner transfer; removal revokes active project streams/cursors before another
result is delivered. Only managers may edit project-wide views, while any
member may save a personal view. Saved filters are evaluated after task
authorization and reveal neither hidden tasks nor counts. Comments and
activity are append-oriented paginated subresources with stable cursors;
task detail includes only a bounded recent summary. A comment correction
creates a revision, and deletion creates a tombstone subject to records hold.
Title, description and comments use the approved rich-text sanitization,
mention resolution and classification rules. Task disposal is restricted to
the records authority, outside ordinary project CRUD.

Project reads should not synchronously fetch chat/doc/HCM bodies for every
card. Store minimal IDs and safe display projections from authorized events;
resolve details on demand under current access. Projection lag is visible and
does not invent task completion. Project indexing, AI inference, reminders,
and webhooks run in bounded project queues, separate from workflow timers and
chat fanout. Admission, per-tenant limits, pagination, and load tests cover
large boards, bulk imports, and reconnect storms. A project outage must not
delay a governed HCM approval or imply that its linked HCM task completed.

The pilot workload profile is 1,000 employees, 250 projects, 100,000 tasks,
a largest board of 10,000 tasks, 50 concurrent viewers, and 10 concurrent
writers. The server returns at most 100 task cards per page; arbitrary
unbounded board loads are rejected. Initial targets on agreed pilot hardware
are p95 first-page board load under 1.5 seconds, p95 task move under 500 ms,
and task-search freshness under 60 seconds. These are proposed gates, not
claims about current code or contracted SLOs; the Product and Reliability
owners must sign the hardware/profile and targets before pilot. A failed
search index falls back to bounded canonical filtering of exact status,
assignee, type, due-date and field predicates, returning at most 100
authorized tasks per page with a continuation cursor and explicit degraded
freshness label; free-text search returns a typed unavailable state until its
index recovers. It never widens access or blocks task writes.
Restore testing covers both project records and rebuildable indexes.

The mixed-load qualification uses a fixed seeded tenant and workflow scenario
in paired runs with and without project traffic, after warm-up, for a recorded
duration and at least three repetitions. It measures workflow timer lateness,
approval completion latency, ready-queue age, pool wait, CPU, I/O, and p95/p99
latency using the same percentile method in both runs. The gate fails on any
protected workflow SLO miss or a paired p95/p99 regression above 5%; run
artifacts preserve seeds, hardware, configuration and raw latency samples.

## Delivery slices and gates

| Slice              | Scope                                                                                                                                             | Exit evidence                                                                                                                                                        |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Scope decision     | Owner, customer cohort, pricing/entitlement hypothesis, support cost, exact displacement of other roadmap work                                    | Signed product scope exchange; no Phase 1 claim.                                                                                                                     |
| Foundation         | Project/task model, lifecycle, tenancy/auth, revisions, activity, retention, bounded API                                                          | Two-tenant denial tests, concurrent move/replay tests, records hold/restore, endpoint-manifest parity, and a workflow mixed-load baseline.                           |
| Useful pilot       | Manual setup, task and basic continuous-flow presets, board/list/search, typed fields, comments, task links to chat/docs, notices                 | Design partner completes a real non-HCM project; 100-card paging and measured workload targets pass; assignment, overdue and completion paths have browser evidence. |
| AI setup           | Typed proposal, source scoping, provider policy, deterministic validation, preview/diff, human publish                                            | Prompt-injection, source-revocation, quota/outage and stale-revision tests; malformed proposals leave the live board unchanged.                                      |
| Advanced flow      | WIP controls/metrics, long migrations, dependencies, checklists/hierarchy, cycles, Scrum/Scrumban, milestones, support intake/SLAs, import/export | Method-specific conformance, migration recovery, load, retention and accessibility evidence.                                                                         |
| Portfolio/external | Cross-project rollups and cross-company participation                                                                                             | Separate scope decision, bilateral security review and scale proof.                                                                                                  |

Initial success measures are activation (a team publishes and uses a board),
weekly active contributors, task assignment/completion, work age and overdue
rate, percentage of task links whose target remains accessible, AI proposal
accept/edit/reject rate, and support load. Do not count card creation or AI
output alone as successful project delivery. Set numeric targets with design
partners before pilot rather than inventing SLOs here.

Before pilot, Product records the design-partner job, previous tool/process,
switching cost, and the outcome that would make the partner continue using
the board. Experience gates include keyboard-only move and configuration,
focus recovery after a rejected move, labeled errors, screen-reader status
changes, reduced motion, mobile-width layout, and en-US, de-DE, and RTL Arabic
rendering. A board is usable without drag-and-drop. Support and Reliability
record incident ownership, capacity limits, restore objective, and rollback
steps before broad enablement.

## Failure and compatibility scenarios to prove

1. Two people drag the same card under different configuration revisions:
   exactly one compatible transition succeeds; the other gets a conflict.
2. A status/field is retired while tasks still use it: preview reports every
   affected task and requires safe mapping or historical compatibility.
3. AI reads a malicious instruction in a linked document or chat: it may
   quote/cite authorized content but cannot acquire publishing rights or
   silently change configuration.
4. A user loses project or linked-document access during a stream/search:
   subsequent results, previews, counts and notifications do not leak data.
5. Project event delivery or search falls behind: canonical task reads stay
   correct; derived views expose freshness, recover by replay, and dedupe.
6. A project links an HCM WorkItem: moving the project card cannot approve,
   complete, or cancel the HCM work; authorized deep link opens the owner.
   Reassignment, completion, cancellation or redaction of the WorkItem while
   the card is open refreshes or neutralizes the projection without implying
   that the project task changed.
7. A project is imported, exported, archived or restored: stable IDs,
   revisions, config versions, comments, links, holds and audit history are
   preserved under tenant records policy.
8. A project request or AI run floods the process: workflow admission and
   timer SLOs remain within their reserved budgets or project work sheds load.

## Open product decisions before implementation

- Choose the first design-partner industries and projects. Validate that
  general team execution is valuable enough beside established PM tools.
- Decide whether project guests or cross-company boards belong in the first
  commercial release; this draft defers them.
- Set customer limits and whether WIP limits are hard rejection or an
  override-with-reason. Define the reporting clock and timezone for cycle
  time before claiming Kanban analytics.
- Decide whether tasks can belong to multiple projects. This draft assumes
  one owning project with linked references elsewhere to avoid ambiguous
  permissions and status authority.
- Decide if personal tasks belong here or in My Work as projections; avoid
  two independent task inboxes without a clear owner.
- Confirm the proposed project-schema pilot and extraction threshold with
  measured mixed-load data; set recovery and isolation objectives.

## Review history

| Date       | Review                     | Outcome                                                                                                                                      |
| ---------- | -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-09-21 | Initial adversarial review | 7.5/10; narrowed the first release, specified Human Work projection, configuration migration, AI operation, API, and measurable pilot gates. |
| 2026-09-21 | Second adversarial review  | 8.3/10; threshold met. Follow-up tightened suspension, due dates, comment reads, membership, event delivery, and mixed-load measurement.     |
| 2026-09-21 | Final adversarial pass     | 8.4/10; no material contradiction. Clarified timezone authority, suspended archive, and paginated degraded filtering afterward.              |
