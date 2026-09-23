# Customer Agents: Creation, Business Context, and Chat

**Status:** DRAFT_CONTRACT  
**Owner:** Agent Product and Agent Runtime owners, with Collaboration, BusinessIntent, AuthZ, Privacy, Security, Knowledge, Integration, Reliability, and Experience review  
**Started:** 2026-09-21  
**Review trigger:** Before customer agent creation, model-provider activation, schedule publication, workflow invocation, autonomous channel participation, cross-company installation, or agent-assisted HCM execution

## Decision and scope

HCM Next should let a business create named agents that understand its approved
context, participate visibly in native chat, run on schedules or authorized
events, and use governed capabilities.
The agent is a versioned business resource with an owner, a stated job,
bounded knowledge sources, tool grants, model policy, autonomy ceiling,
budget, evaluation suite, and stop controls. Publishing the definition and
installing it in a conversation are separate decisions. A channel invitation
does not grant HCM, document, or project authority.

The first agent release must cover the choices already made for chat: all
employees can encounter admitted agents; agents may participate autonomously
in permitted channels; and an agent may carry an **approved** HCM action
through the normal BusinessIntent/workflow path. This is a proposed release
contract, not a claim that a deployed model service or customer agent builder
exists today. It requires a scope exchange against the
[execution plan](../execution-plan.md) and a release gate. The
[agent architecture](platform-architecture-catalog.md) currently treats the
agent product plane as design-only. The
[chat contract](company-chat-and-collaboration.md) already requires visible
identity, scoped installation, bounded triggers, and governed HCM actions.

The initial product is a no-code Agent Studio with a typed advanced manifest
and integration API for qualified builders. A creator can start from an
approved template or a blank draft. AI may suggest a configuration, but the
same deterministic validator, evaluation, review, and publication path applies
to manual and AI-authored drafts. Arbitrary customer code, direct SQL,
unreviewed HTTP calls, and provider credentials in prompts are outside the
first release.

**Ownership boundary:** HCM Next owns agent definitions, instructions,
identities, context assembly, schedules, trigger admission, tool execution,
workflow participation, memory, runs, approvals, costs, audit, and chat
delivery. OpenAI, Anthropic, and approved private model endpoints supply
inference behind adapters. Provider-side agent/session objects, hosted tools,
conversation stores, schedules, or workflow features are never the canonical
HCM Next agent state or an HCM authority path. A provider can propose a tool
call; HCM Next validates and executes any allowed operation through its own
capability gateway. This matches the application-executed tool round trip in
[OpenAI's function-calling documentation](https://developers.openai.com/api/docs/guides/function-calling)
and [Anthropic's client-tool documentation](https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview).

## Product experience: how a company creates an agent

The creator journey has nine steps, each independently saveable as a draft:

1. **Choose its job.** Name the business outcome, owner, intended users,
   supported languages, answer boundaries, and escalation destination.
2. **Choose its context.** Select approved deployed documents, permitted
   chat spaces, governed data capabilities, organization scope, and freshness
   needs. A source picker previews exactly which audience and classification
   each source allows.
3. **Choose tools.** Select published typed capabilities such as document
   search, safe employee lookup, project task creation, or BusinessIntent
   drafting. The UI shows read/write risk, fields, purpose, and required
   approval for each tool.
4. **Choose where and how it runs.** Set DM, `@` mention, slash command,
   selected channel events, schedules, governed domain events, UI/API
   invocation, or a workflow capability call. Choose eligible channels,
   recipients, and whether replies are public, private, or a typed workflow
   result.
5. **Set autonomy and budgets.** Select maximum autonomy level, posting
   frequency, trigger cooldown, model/provider policy, cost, latency,
   concurrent-run, and escalation limits. Channel participation is bounded
   even when it is autonomous.
6. **Preview effective authority.** Show the intersection of definition,
   installation, channel, trigger/invoker, tenant/org policy, data purpose,
   and capability rules. List denied operations as clearly as allowed ones.
7. **Test in a sandbox.** Replay representative questions, hostile channel
   messages, inaccessible documents, stale HCM facts, approval requests,
   provider failure, and budget exhaustion. Show answer, citations, tool
   trace, disclosure audience, cost, and why a proposed action did or did
   not advance.
8. **Review and publish.** A permitted reviewer accepts the exact immutable
   version digest and evaluation result. High-risk or scope-expanding changes
   require a different reviewer from the author.
9. **Install and monitor.** A conversation manager installs the published
   version into selected conversations within its approved ceiling. Users
   see the agent identity, owner, capabilities, trigger mode, and pause/report
   controls. The owner sees runs, failures, costs, and evaluations.

Templates should begin with a **Policy Guide** (answers from deployed
documents with citations), **Onboarding Coordinator** (answers and safe task
coordination), **Project Assistant** (ordinary project work), and **HCM
Action Assistant** (drafts typed HCM actions and follows approved outcomes).
Templates are editable starting points; their names confer no capability.
The first release can pilot two templates while retaining the same creation
model. Specialist payroll, benefits, legal, hiring, and compensation agents
require domain-specific evaluation and authority gates before publication.

| Business role        | Allowed operation                                                                                         | Boundary                                                                                                  |
| -------------------- | --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Employee             | Discover admitted agents, invoke installed agents, inspect identity/capabilities, report or mute an agent | No definition, installation, source, or tool grant.                                                       |
| Agent creator        | Draft a tenant agent and run sandbox evaluations                                                          | Drafting grants no production access or chat posting.                                                     |
| Agent owner          | Maintain charter, request publication, monitor runs and spend, pause owned installations                  | Cannot self-approve material scope expansion or override domain policy.                                   |
| Agent reviewer       | Approve an exact version and source/tool/autonomy ceiling                                                 | Must have the relevant domain and risk authority; author/reviewer separation applies to material changes. |
| Conversation manager | Request or remove an installation and select narrower triggers                                            | Cannot exceed the published version, channel policy, or bilateral cross-company approval.                 |
| Security/operator    | Quarantine, revoke, investigate, and fence in-flight work                                                 | Does not become an HCM action approver merely by operating the agent.                                     |

This approach follows a familiar app pattern: users discover agents, start
DMs or add them to channels, and admins review their scopes. HCM Next adds
the business-specific approval, HCM authority, and audience rules described
below. [Slack agent interaction](https://slack.com/help/articles/33076000248851-Work-with-AI-agents-in-Slack),
[Slack app permissions](https://api.slack.com/help/articles/115003461503-Understand-app-permissions-).

## Canonical objects and lifecycles

| Object              | Owner and invariant                                                                                                                                                                                                                                     |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `AgentTemplate`     | Platform-authored versioned starting manifest and evaluation pack. Installing a template creates a tenant-owned draft; later template changes never silently alter published customer agents.                                                           |
| `AgentDefinition`   | Tenant-owned stable agent ID, display identity, accountable owner, purpose, eligible audience, organization scope, allowed source classes, tool grants, invocation modes, autonomy ceiling, model policy, memory policy, budget, and escalation policy. |
| `AgentVersion`      | Immutable validated manifest, instructions, schema/tool/model pins, evaluation report, approver, publication digest, and compatibility metadata. A run pins one version.                                                                                |
| `AgentInstallation` | Grant binding a published version to one named conversation, host/home-company policy, trigger modes, posting audience, and narrower effective tool/source scopes.                                                                                      |
| `AgentRollout`      | Optional desired-placement plan for many conversations, with selector, preview, approver, batch limit, rollout stage, and reconciliation cursor. It produces individually revocable installations; a selector never becomes a blanket grant.            |
| `AgentRun`          | One invocation or accepted autonomous trigger with principal chain, cause, context snapshot IDs, version pins, tool attempts, decisions, output, cost, and terminal state.                                                                              |
| `AgentRunRequest`   | Durable, idempotent request with trigger kind, source occurrence/event/workflow reference, requested agent/version, purpose, audience, deadline, budget and principal chain. Admission may refuse it before model work.                                 |
| `AgentSchedule`     | Tenant-owned versioned binding from a published schedule trigger to a pinned agent version, purpose, output destination, misfire/overlap policy, owner, and budget. A schedule is separately pausable and revocable.                                    |
| `AgentContextGrant` | Reviewed right to use a document scope, chat history window, business capability, event class, or project context for a stated purpose and retention period.                                                                                            |
| `AgentMemoryItem`   | Derived item with source, audience, purpose, classification, TTL, approval state, and revocation inputs. It never becomes authoritative HCM data.                                                                                                       |

Definition lifecycle:

```text
DRAFT -> VALIDATED -> REVIEW_REQUIRED -> PUBLISHED -> SUPERSEDED -> RETIRED
                        |                  |
                        v                  v
                    REJECTED           QUARANTINED
```

An unchanged low-risk draft may publish without separate review only when
tenant policy permits it. Write-capable tools, wider audience, broader source
scope, higher autonomy, model/provider change, or cross-company eligibility
always require review. Revisions are additive immutable versions. A rollback
publishes a new pointer to a previously approved version after current
eligibility and evaluation checks; it does not erase intervening history.

Installation lifecycle:

```text
REQUESTED -> APPROVED -> ACTIVE -> SUSPENDED -> ACTIVE
                           |           |
                           +----------> REMOVED
```

Revocation or agent quarantine immediately prevents new runs, stops in-flight
write-capable tool leases, ends future chat delivery, and invalidates derived
context/memory where access was lost. Removal retains records under policy.
Changing the definition's scopes never upgrades an installation silently;
the installer must review the new effective grant. A departing owner must
transfer ownership through an approved successor or the agent suspends.
Each installation has its own stable ID and revision even when a rollout
places the same agent in hundreds of conversations. A new channel matching a
rollout selector remains pending until current membership, classification,
and approval policy admit it. A deleted or reclassified channel revokes or
suspends its installation independently.

Run lifecycle (tool calls and action proposals are recorded substeps; the
BusinessIntent owner tracks any later business action):

```text
ACCEPTED -> ADMITTED -> PLANNING -> RESPONSE_READY -> POSTED -> COMPLETED
                         |               |
                         +-> REFUSED     +-> COMPLETED (private response)
Any nonterminal state -> FAILED | CANCELLED | EXPIRED
```

Every transition is durable and idempotent. A chat post is a delivery event,
not proof that a downstream HCM action executed. The runtime records whether
an action is drafted, awaiting approval, accepted for execution, observed,
reconciled, or failed, using the owning BusinessIntent status.

## Business context model

An agent answers or acts only inside a resolved `EffectiveAgentContext`:

```text
tenant + company/organization scope + agent version + installation
+ trigger and initiator/delegation + business purpose + subject/resource
+ conversation and output audience + source grants and versions
+ capability grants + current AuthZ/LegalContext + classification
+ freshness, time zone, locale + autonomy/risk/budget + correlation
```

The context builder obtains these facts from their owners. It does not treat
the conversation topic, a message's claimed role, a prompt, an uploaded
document, or a tool response as a trusted tenant, purpose, policy, or grant.
It distinguishes **canonical HCM facts**, external observations, document
policy, user claims, and model inference in both traces and answers. It
labels source freshness and temporal mode when the answer depends on
effective-dated employee data. Ambiguous or stale high-risk facts cause a
clarifying request or specialist handoff, not an invented answer.

Effective authority is the intersection of:

```text
agent definition grant
∩ published version and installation grant
∩ current tenant/org and field authorization
∩ source/document/chat membership and classification
∩ purpose, legal, risk, and capability eligibility
∩ trigger or human delegation (when a human invoked it)
∩ autonomy and resource budget
```

For an autonomous channel trigger there is no human invoker whose broad
rights can be borrowed. The run uses the agent's own tenant service identity,
named business sponsor, explicit event subscription, and installation grant.
It may retrieve only what that identity and current conversation context
allow. For a human-requested private answer, the invoker's current rights
further narrow retrieval. Delegation to another specialist agent can only
narrow these rights and requires a typed purpose, depth cap, and trace.

Business context is configured through a **Business Charter** in the agent
definition: what the agent is for, whom it serves, allowed organization and
jurisdiction, approved knowledge collections, data categories, permissible
decisions, escalation owner, and success measures. The charter is displayed
to reviewers and channel managers, compiled to policy, and versioned with
the agent. Business users can write it in plain language, but executable
grants are selected from typed capabilities and current directory objects.

### Configuration across different businesses

The charter is a reusable business-level definition, not a copy of a single
company's org chart. Configuration resolves in this order: mandatory platform
and legal controls; tenant and legal-entity policy; an optional versioned
industry or function pack; the published agent version; the narrower
installation; and the current request context. A lower layer may narrow a
grant or supply content and presentation defaults, but cannot override a
higher-layer denial. The resolved configuration and source version of each
setting are visible in validation, evaluation, and run traces.

The configuration model must represent subsidiaries, branches, franchises,
contractors, unions, multiple languages and jurisdictions, seasonal
headcount, shift workers, and cross-company channels without a new agent
runtime for each business type. Organization and legal-entity selectors are
stable IDs with effective dates, not name matching. Localization covers
instructions, output, templates, escalation routes, and accessibility;
policy and source eligibility are still decided by typed rules. A small
business can use defaults and one owner; a multi-entity business can delegate
creation, review, budgets, and installations by scope without giving a local
manager tenant-wide access.

Templates and packs declare compatible agent-manifest, capability, document,
and policy schema versions. A pack update creates an upgrade proposal and
evaluation run; it cannot rewrite a published agent. Tenant-specific
extensions are configuration records or published capabilities, not a forked
schema, tenant-specific code path, or unrestricted prompt override. Exporting
and importing a definition preserves the portable manifest but strips
secrets, tenant IDs, grants, installations, memory, and run history; the
destination must remap sources and capabilities and review the new version.

### Context sources and memory

- Deployed documents from the
  [documentation hub](channel-documentation-hub.md) provide cited knowledge;
  draft/private documents need explicit current grants. A document link does
  not grant content access.
- Chat context comes from the installed conversation and a bounded history
  window. The agent does not read unrelated channels or a user's DMs because
  someone mentioned them. Quoted posts, files, and interactive embeds are
  untrusted source material.
- HCM and project data arrive through typed read capabilities with field
  masks, purpose, subject and as-of rules. The agent never queries another
  module's tables or vector index directly.
- Conversation text is not automatically persistent agent memory. A proposed
  shared memory item requires source provenance, classification, audience,
  human review where policy requires, TTL, and records treatment. Revoking a
  source removes or invalidates its memory, cached answers, search material,
  and future prompts.

## Chat participation contract

An agent appears as a distinct nonhuman principal with name, home company,
owner, version, availability, allowed commands, and a visible agent marker.
It may participate via DM, `@agent`, `/tool` command, or a bounded autonomous
channel trigger after installation. Chat owns post ordering and membership;
the agent runtime owns planning and tool execution. The model never inserts
directly into the message database.

Chat commits an eligible event and emits its outbox record. The agent
dispatcher checks installation revision, current channel access, trigger
policy, dedupe key, quotas and kill switch before accepting a run. It may
read an authorized context window through chat RPC. It posts through the
normal `SendPost` path using agent identity, idempotency key, source/run ID,
and content classification. A failed model or tool call leaves the original
human post intact and produces a bounded user-visible failure or silent
operator record according to trigger policy; it does not block `SendPost`.

| Mode               | First-release rule                                                                                                                                                                                          |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| DM                 | User-initiated, private to admitted participants; agent may ask clarifying questions and cite sources.                                                                                                      |
| Mention            | One invocation from an authorized `@agent`; a quoted or forwarded mention is inert unless a new authorized user explicitly invokes it.                                                                      |
| Slash command      | Typed command arguments and invoker identity; discoverable only where installed and authorized.                                                                                                             |
| Autonomous channel | Subscribed event classes and filters, explicit opt-in, cooldown, per-channel/day budget, max concurrent runs and replies, cause-chain depth, and a responsible owner. No blanket response to every message. |
| Thread reply       | Respond in originating thread by default; top-level posts or broad mentions require extra grant.                                                                                                            |

An agent-to-agent message or its own post cannot recursively trigger an
unbounded run. Cause IDs, origin markers, dedupe keys, loop depth, and circuit
breakers prevent reciprocal mentions and retry storms. Moderators can pause
an installation and report an agent post without acquiring its private
working context. Users can distinguish citations, agent inference, and
action proposal cards from authoritative HCM state.

### Audience-safe answers

A public reply is safe for **every current recipient** of the channel,
including external participants. The runtime computes a disclosure ceiling
from the conversation's audience and classification policy, then validates
citations, fields, attachments, cards, and previews against it immediately
before posting. A private invoker's broader rights never widen a public
answer. If a useful answer depends on private data, the agent offers a
private handoff or a neutral restricted result; it does not hint at a hidden
person, document, channel, or count. An agent can cite a document only when
all recipients can open that exact deployment or the citation is explicitly
marked private and delivered outside the channel.

In a cross-company channel, the host tenant owns the canonical timeline;
agent installation needs host approval and home-company approval for each
participating company whose data the agent may process. The effective grant
is their intersection, with declared processing region, retention, egress,
incident contact, and exit behavior. A host-owned agent does not inherit a
guest company's HCM rights. A home-company agent cannot join merely because
its employee is a member. Until bilateral policy is proven for a specific
agent version and installation, it stays unavailable in that shared channel.

## Invocation, schedules, and durable runs

All entry points create the same `AgentRunRequest` and pass through one
admission path. The request names a source and stable dedupe key, exact agent
version policy, tenant/legal entity, initiating principal or autonomous
sponsor, purpose, audience, deadline, and budget. Admission resolves current
authority, installation or workflow grant, provider eligibility, source
freshness, and stop controls before making a model request. A model response
can ask for more tool calls, but cannot create a new invocation mode or
schedule on its own.

| Invocation                      | Authority and delivery rule                                                                                                                            |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Chat DM, mention, slash command | The current user and installed agent are both checked; the result returns to the invoking conversation and current audience.                           |
| Autonomous channel event        | An installed, versioned subscription and nonhuman sponsor authorize the run; cooldown, dedupe, and audience validation apply.                          |
| Agent Studio or internal UI/API | The caller needs `agents.invoke` for that version and purpose; an API token cannot impersonate a chat member or supply arbitrary HCM scope.            |
| Published schedule              | A due occurrence creates one request under the schedule's own sponsor, pinned version, output destination, and budget; no human authority is borrowed. |
| Governed domain event           | A typed subscribed event is validated and deduplicated; the agent receives only an authorized event projection, not an unrestricted event bus.         |
| Workflow `CAPABILITY`           | The workflow supplies a typed context and correlation ID; the agent returns a validated typed result or failure to the owning workflow.                |

An `AgentSchedule` is created from a draft with an owner, business purpose,
agent version binding, target organization, recurrence or business-calendar
rule, timezone, tzdb/calendar references, start/end dates, output audience,
allowed capabilities, cost ceiling, overlap policy, misfire/catch-up policy,
maximum firings, and escalation route. The Studio previews the next
occurrences and recipient disclosure before publication. Publication and
scope expansion require review by an agent reviewer; posting to a channel
also needs its conversation manager's installation/destination approval.
Owners can pause, resume, skip one
occurrence, run an authorized dry-run, or retire a schedule with an audit
record. A schedule's output destination can be a private inbox, an installed
channel, a document draft, a project task draft, or a typed workflow result;
each destination has its own current grant and delivery check.

The existing [schedule engine](../../internal/engines/schedule/schedule.go)
already defines published cron, calendar, and event trigger shapes, overlap,
storm, occurrence, DST, and misfire behavior. Its current published target is
an intent reference, and a served agent-run target is not proven. Extend the
Scheduling plane's versioned target contract with `AGENT_RUN`, keeping
existing intent targets compatible. Its agent dispatch adapter writes to the
agent-owned run-request inbox; it does not create a BusinessIntent merely to
ask a model for a digest. Reuse occurrence identity and calendar rules
rather than adding a second cron calculator inside the agent worker. The
Scheduling owner commits a firing receipt and outbox event; the agent owner
accepts it through a deduplicating inbox and persists an `AgentRunRequest`
before inference. The two databases are not joined in a transaction, so
delivery is at least once and ambiguous handoffs are reconciled. A fire
key contains tenant, schedule ID/revision, occurrence key, and target agent
version. Replaying a firing returns the same run or refusal. Pausing a
schedule or revoking its owner/grant fences unstarted runs, public delivery,
and write leases.

Version selection is explicit: default to the exact approved agent version
pinned when the schedule was published. Moving to a newer version requires
evaluation and schedule review; a silent `latest` pointer is forbidden for
all schedules. Each occurrence resolves the
current roster, data grants, legal rules, destination membership, and
provider eligibility at fire time. A cohort may be reselected each run, but
its resolved subject IDs and as-of boundary are frozen in the run trace.
Holiday/quiet-hour rules, DST gap/fold choices, missed-fire catch-up,
overlap (`SKIP`, bounded `QUEUE`, or `REFUSE`), and event debounce are
declared, previewed, and tested. A schedule never fabricates success during
provider outage; it records a typed skipped, deferred, failed, or
review-required outcome.

Agent runs have durable checkpoints at admission, context resolution,
model request/response, each admitted tool request/result, output
validation, delivery, and workflow handoff. On restart, the worker resumes
from recorded checkpoints and rechecks grants; it does not replay a
side-effecting tool because a model response was lost. Provider calls may
be retried within the run budget, but any uncertain tool effect is
reconciled through the owning capability before continuation. A long-running
run reports progress and can be cancelled; a waiting approval or workflow
signal persists independently of a model process. Run expiration has a
defined user-visible or operator outcome, and a later approval cannot
resurrect an expired proposal without a new validation.

## Agents and workflows in both directions

The [workflow contract](workflow-runtime.md) expresses agent work as an
agent-eligible `CAPABILITY`; it does not add an `AGENT` node type. Publish
`agents.invoke` with typed input, output, risk, authority, timeout, and
budget. The capability starts an `AgentRun` and returns its ID. For a result
that outlives one worker lease, the workflow waits through its durable
`SIGNAL`/timeout contract keyed to that ID, then validates the result before
a deterministic `DECISION`, `TASK`, `APPROVAL`, or later capability step.
The workflow pins its definition, agent version, tool schemas, and context
references. A failed, refused, expired, or cancelled agent run takes an
explicit workflow branch; it never strands a workflow instance.

In the other direction, an agent may discover and **request** a published
workflow through a typed start capability. The agent supplies a validated
draft or exact intent reference; the workflow owner resolves current
authorization, simulation, approval, and admission. A chat instruction or
scheduled run cannot bypass these steps. The agent may watch the workflow
through an authorized status capability and explain observed progress, but
cannot turn its own text into a workflow-completion signal. If an agent run
starts a workflow that invokes an agent, cause-chain depth, tenant budget,
and explicit allowed-edge rules prevent recursive orchestration. Workflow
timers and queues remain owned by Workflow; agent schedule firings and model
workers have separate capacity and failure domains.

### Concrete business examples

These examples illustrate the common contracts; each is available only after
its trigger, capability, and domain release gates pass.

| Example                  | Trigger and agent work                                                                                                                              | Governed result                                                                                                                                                                 |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| New-hire coordinator     | A published onboarding event starts a run that reads the employee's permitted start-date facts and official team documents.                         | Drafts a welcome message and project checklist; posts only to the admitted onboarding channel after audience validation. A missing policy or uncertain start date routes to HR. |
| Weekly team digest       | Monday 09:00 in the team's timezone fires a pinned schedule. The agent reads project and channel activity within its installation grants.           | Posts a cited summary in the team channel. Quiet hours, holiday policy, duplicate fires, and a removed external member are rechecked at delivery.                               |
| Promotion assistant      | A manager invokes `@agent` in a private chat and asks to begin a promotion. The agent gathers eligible data and fills a typed BusinessIntent draft. | The normal workflow simulates and collects approvals; the agent can carry only the exact approved intent and report its observed outcome.                                       |
| Policy-change monitor    | An official document deployment emits a governed event. The agent compares the new version with the prior authorized deployment.                    | Drafts a change summary for the policy owner; publication or broad announcement requires the owner's review and destination permission.                                         |
| Workflow research step   | A published employee-transfer workflow invokes an agent capability to compare approved policies across two legal entities.                          | The run returns typed citations and uncertainty to a durable workflow signal; deterministic policy and human approval decide the transfer.                                      |
| Morning exception review | A business-calendar schedule asks an operations agent to read permitted failed integration and reconciliation summaries.                            | Creates a bounded human task for unresolved items. It cannot mark payroll or HCM effects repaired from a model guess.                                                           |

## HCM and project actions from chat

The agent's free-form text is never a business command. It discovers a
published capability, fills a typed draft, validates references/fields,
resolves policy and current data, simulates where required, and produces a
proposal card with exact intent/proposal ID, material digest, affected
subjects, consequences, sources, uncertainty, and approver route. The
[existing action compiler](../../internal/agentsecurity/action_compiler.go)
proves a bounded draft boundary against its current private intent-definition
type. It is not a served agent-run pipeline, and its definition resolution
must be connected to the actual served BusinessIntent catalog before an
agent-assisted HCM action can pass the release gate. Submission, approval,
execution, observation, and repair remain
owned by [BusinessIntent](business-intent-and-change-request.md) and its
workflow. Only an authorized approver in the normal action surface can
approve; a chat emoji, model claim, or agent message cannot.

After approval the agent may **carry** the action through execution only by
submitting the exact approved intent through the published capability. The
server rechecks current agent/installation grant, actor/delegation, policy,
proposal digest, source freshness, and approval at the side-effect boundary.
Idempotency prevents a retry or duplicate chat event from creating a second
effect. The agent reports `draft`, `awaiting approval`, `executing`,
`observed`, `needs repair`, or `failed` from durable owner state, never from
its own prediction. High-risk employment, pay, payroll, legal, and access
decisions keep their existing human and legal gates.

Ordinary project tasks use the
[project task capability](customer-project-management-and-adaptive-boards.md)
and its own revisions, permissions, and records rules. An agent may draft or
create a task only within its project grant; task completion never implies an
HCM WorkItem or employee record change. Agent-assisted board configuration
uses the project plan's preview and human publication path.

## Architecture and isolation

```text
Browser RPC / integration HTTP / chat or domain event
                / schedule occurrence / workflow capability
                                  |
                             Core edge/auth
                                  |
                   Agent run admission + owned store
                     |        |          |          |
                context   tool gateway   queues    model router
                 builder       |                      |
                    |      HCM Next                 provider adapters
          Chat/Knowledge/Project/BusinessIntent       |
          and Workflow owner RPCs               OpenAI/Anthropic/private
```

The first deployment may share the Go binary, but agent inference, event
dispatch, memory maintenance, evaluation, and provider calls use separate
bounded queues, workers and admission budgets. Agent definitions, versions,
installations, runs, traces, budgets and memory live in an agent-owned store
with its own credentials, pool, outbox, retention and recovery; large prompt
and trace artifacts use protected object storage. It never shares a message
table or workflow queue, and modules do not perform cross-database joins.
Core authenticates and routes; Chat stores posts; Knowledge stores documents;
BusinessIntent owns HCM effects. Agent timeouts or model outages cannot hold
chat send transactions or workflow locks. Saturation sheds optional agent
work first while deterministic HCM and human chat remain available.

The model router selects only approved providers/models for task profile,
data classification, jurisdiction, residency, retention, quality floor,
latency and cost. Provider keys are held by the secrets service, never in
agent manifests, chat, or model-visible context. Tool calls go through the
existing [tool-security gateway](../../internal/agentsecurity/toolgateway.go)
and typed capability path; retrieved text cannot add a tool or change a
scope. Model output passes structured validation before a tool result,
document citation, task update, HCM draft, or chat card is served. An agent
version pins prompt, model eligibility profile, tool schemas and output
schema; provider/model updates require evaluation before activation.

### Model-provider adapters and ownership

Define one HCM Next `ModelRequest`/`ModelResult` contract and separate
adapters for OpenAI, Anthropic, and approved private endpoints. The common
contract carries task profile, approved messages/context references, allowed
tool schemas, output schema, deadline, token/cost ceiling, trace ID, and
required processing policy. The result carries text or structured output,
proposed tool calls, usage, provider/model version, finish reason, and
provider request ID. The adapter maps provider-specific roles, streaming,
tool-call syntax, token accounting, errors, and cancellation into that
contract. Provider features absent from another adapter are optional
capabilities discovered during validation, not assumed universal behavior.

| Boundary             | HCM Next responsibility                                                                                                                                                                                                                                    |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Prompt and memory    | Assemble only authorized context; persist the canonical run transcript and memory under HCM Next retention. A provider response ID is a trace reference, not the agent's conversation identity.                                                            |
| Tool execution       | Expose only eligible HCM Next tools; reject unrecognized names, malformed arguments, excessive parallel calls, and scope changes. Provider-hosted tools with external network or persistent state require a separate reviewed capability and egress grant. |
| Data processing      | Check per-provider and per-model region, retention, logging, training-use terms, encryption, and contract before dispatch. Minimize fields and redact where task semantics permit.                                                                         |
| Output               | Validate against HCM Next schemas and citations even when a provider offers structured-output mode; only validated output may enter chat, workflow, documents, or BusinessIntent.                                                                          |
| Failure and fallback | Retry within a bounded same-provider policy first; route to another provider only if it meets the same data and task constraints and has passed that agent's evaluations. Never silently switch after an uncertain tool effect.                            |
| Private model        | Use the same adapter contract and evaluations with an approved network endpoint, service identity, health check, and declared capability profile; hosting location alone grants no HCM authority.                                                          |

Streaming tokens shown privately during an interactive run are provisional;
they cannot be committed as a channel post, workflow result, or action until
the complete output is validated. If an adapter loses contact after a model
request, it may resume only from HCM Next's last durable checkpoint; it must
not assume a provider-side conversation or tool execution is authoritative.
OpenAI and Anthropic both describe model-generated tool calls that an
application can execute and return; their exact APIs and structured-output
limits differ, so adapter conformance tests must cover both rather than
exposing provider syntax to business capabilities.
[OpenAI function calling](https://developers.openai.com/api/docs/guides/function-calling),
[Anthropic tool use](https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview),
[Anthropic structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs).

### Extensibility contract

New business skills enter through the existing capability registry or a
reviewed Integration Platform connector. A capability manifest declares
stable ID and version, typed inputs and outputs, classification, read/write
effects, subject and organization scope, current AuthZ/purpose requirements,
idempotency, timeout, cost, simulation, approval, and repair behavior. The
runtime discovers only capabilities eligible for the current task and
effective grant. A capability can be added, deprecated, or replaced without
changing the agent runner; an incompatible schema requires a new version and
re-evaluation of affected agents. A connector cannot smuggle an arbitrary
HTTP destination through a tool argument.

Source adapters for documents, project records, and future CRM/accounting or
industry systems implement one retrieval envelope: source ID and version,
owner, tenant/legal entity, classification, audience, purpose, freshness,
deletion/hold status, citation target, and revocation token. Search indexes
are projections of those owners, not independent grants. Per-source and
per-capability failures are typed so an agent can give a partial answer with
clear limits or escalate; it cannot treat missing data as a negative fact.

Agent manifests, tools, retrieval envelopes, events, cards, and run records
have independently versioned schemas with compatibility rules. Extension
fields are namespaced and length-limited; unknown security-relevant fields
fail validation rather than being silently ignored. A published version
pins compatible tool and output schemas. Canary evaluation and staged
installation upgrades precede fleet promotion, and a retired capability
keeps a migration window or causes affected agents to suspend visibly.

### Scale and placement contract

The agent runtime is a logical plane, not one global queue or database.
Start with an agent-owned database and isolated worker pools; partition run
and event processing by tenant/cell and installation, and give each tenant
fair admission and a bounded share of model, retrieval, tool, and posting
capacity. Separate interactive requests, autonomous triggers, evaluation,
memory maintenance, and bulk rollouts so a trigger burst cannot starve a
person's DM or a workflow action. Use queue age, admitted concurrency,
provider quota, token and tool budget, and downstream health for admission;
CPU alone is insufficient for model-bound work. Retries are bounded and
jittered, dead letters are explainable and replayable, and backlogs have
retention and expiry rules.

The tenant/cell route is explicit in every run and event. A tenant may move
from pooled capacity to a dedicated cell or region without changing agent
IDs, definitions, or public API semantics. Migration fences the old writer,
reconciles outbox and idempotency state, revalidates installations and grants,
then switches a route epoch. Cross-company installations still use the host
conversation's route for delivery and each participating company's policy
for data processing. No global semantic cache, memory index, or trace store
may co-mingle tenant context without tenant-scoped keys and access checks.
Dedicated placement is an operational option for residency, contractual,
or load needs, not a separate product fork.

Budgeting is hierarchical: platform safety limits, tenant entitlement,
legal entity or department allocation, agent/version, installation,
trigger, and run. Every run estimates and reserves cost before model work,
accounts for actual tokens/tools/egress, and releases unused reservation.
An autonomous trigger is shed or deferred before it consumes another
tenant's allowance. Operators see cost and saturation by tenant, agent,
installation, provider, and source, without exposing another tenant's data.
This pool-or-dedicated-cell option follows the tradeoff described in
[Azure's multitenant deployment guidance](https://learn.microsoft.com/en-us/azure/architecture/guide/multitenant/approaches/overview)
and [AWS's SaaS isolation guidance](https://docs.aws.amazon.com/wellarchitected/latest/saas-lens/pool-isolation.html).

The browser Agent Studio and chat use canonical Protobuf/gRPC through the
existing RPC edge. A versioned integration HTTP API projects the same
definition, installation, run, evaluation, and operator operations through
the [endpoint contract](http-grpc-endpoint-contract.md). HTTP callers gain
no generic raw-prompt or unrestricted tool endpoint. Every public method
declares AuthZ, purpose, idempotency, expected revision, rate budget, audit,
stream/cursor behavior, and compatibility. External builders can submit a
manifest draft and request installation, subject to the same review gates.

The first method set is `CreateAgentDraft`, `UpdateAgentDraft`,
`ValidateAgentDraft`, `EvaluateAgentDraft`, `RequestAgentPublication`,
`PublishAgentVersion`, `RequestAgentInstallation`,
`ApproveAgentInstallation`, `PauseAgentInstallation`, `InvokeAgent`,
`ListAgentRuns`, and `GetAgentRun`. Chat's per-conversation integration API
exposes installed agent IDs, revision, trigger settings, posting policy, and
installation requests; it forwards lifecycle decisions to the agent owner
service. It cannot edit a definition or grant a capability. Mutations use
expected revisions and idempotency keys; run streams are authorized and
cursor-based. These are candidate contracts, not claims of served endpoints.
Add `CreateAgentScheduleDraft`, `PreviewAgentSchedule`,
`PublishAgentSchedule`, `PauseAgentSchedule`, `ResumeAgentSchedule`,
`SkipAgentOccurrence`, `ListAgentSchedules`, `CancelAgentRun`, and
`GetAgentRunEvents` to the candidate method set. Schedule publication and
run-now controls have distinct grants; a run-now request cannot bypass a
paused schedule, budget, or publication review. Workflow invokes the same
agent service through its registered typed capability, not the public HTTP
endpoint.

## Operations, evidence, and safety

The runtime keeps an agent trace separate from the HCM business ledger.
Trace records input reference IDs and classifications, version pins, model
route, tool admission/denial, cost, output validation, citations, and failure
codes. Protected raw prompt/output retention is minimized, tenant-configured,
encrypted, and never required for a user to understand an action. Material
HCM decisions, approvals, execution and reconciliation live with their
owning BusinessIntent/ledger records; correlation IDs join the two.

Every installed agent has an owner dashboard: version, scope, channel list,
schedule list and next/last firing, run cause, trigger rate, spend, latency,
queue/schedule lag, grounding/citation quality, tool denials,
refusals, user corrections, escalation rate, incidents, and last successful
evaluation. Owners can pause one installation; security can quarantine an
agent version, tool, model/provider, tenant fleet, or every write-capable
agent path. The stop control fences in-flight write leases and leaves
non-agent deterministic operations available. Owner exit, source revocation,
model change, policy change, or incident opens a re-review.

Evaluation packs include golden business answers, source freshness,
field-level access, output audience, prompt injection, tool misuse,
impersonation, approval spoofing, duplicate trigger, loop storms, provider
failure, budget exhaustion, cross-company participation, and current grant
revocation. A new version cannot publish on a failed required fixture.
There is a manual fallback and specialist handoff for every consequential
workflow. NIST's AI RMF emphasizes risk management across the AI lifecycle,
and OWASP's agentic guidance names goal hijack, tool misuse, and identity
abuse as distinct risks; the concrete gates here address those failure
classes. [NIST AI RMF](https://www.nist.gov/itl/ai-risk-management-framework),
[OWASP Agentic Top 10](https://genai.owasp.org/2025/12/09/owasp-top-10-for-agentic-applications-the-benchmark-for-agentic-security-in-the-age-of-autonomous-ai/).

## Release sequence and acceptance

| Gate                             | Delivery                                                                                                             | Evidence required                                                                                                                                       |
| -------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Scope exchange                   | Product owner, first cohort, domains, models, expected cost, displaced roadmap work, support and incident owners     | Signed decision; no Phase 1 or chat-release claim inferred from this draft.                                                                             |
| Definition and Studio            | Typed draft/version/charter, templates, manual editor, validator, permission preview, review/publish, owner transfer | Two-tenant scope tests, version/digest races, owner exit, upgrade without silent scope expansion, accessible and localized UI.                          |
| Read-only interaction            | DM, mention, slash invocation, cited authorized answers, provider router, trace and budget                           | Real served chat runs; hostile context, stale documents, revoked member and model outage preserve access and chat latency.                              |
| Autonomous channel participation | Installed event subscriptions, cooldown, bounded context, audience-safe replies, loop and kill controls              | Mixed human/agent channel simulation, duplicate/replay/loop storm, cross-company bilateral admission, pause and revocation tests.                       |
| Approved HCM action              | Typed draft, simulation, approval card/handoff, exact approved execution, observed outcome and repair state          | Forged approval, changed digest, stale facts, retry and concurrent revocation tests prove zero unauthorized effect and one authorized effect.           |
| Provider adapter conformance     | One served provider and the same contract for each later offered OpenAI, Anthropic, or private endpoint              | Every offered adapter passes tool-call, structured-output, cancellation, usage, policy-routing, outage, and eligible-fallback fixtures.                 |
| Scheduled agent runs             | Publish, preview, pause, resume, skip, fire, and reconcile agent schedules through the Scheduling plane              | DST, holiday, overlap, misfire, restart, replay, revoked destination, changed agent version, and budget tests create one admitted run or typed refusal. |
| Workflow participation           | Agent invocation as a typed capability, durable result signal, and agent-requested governed workflow start           | Crash/retry, timeout, approval, stale result, recursion, and cancellation fixtures leave no stranded workflow or unauthorized effect.                   |
| Design-partner pilot             | Named under-1,000-employee business, support/rollback, model spend and adoption measurements                         | Employees resolve real questions and one approved action, operators can explain every tool call, and workflow/chat SLOs remain within signed budgets.   |

The chat rollout may stage the gates internally, but a first-agent-release
claim requires the previously chosen autonomous channel and approved-HCM
action behavior, plus at least one real scheduled run and one workflow
capability invocation, to be served and proven. Broad event-driven
operational monitoring, specialist agents, multi-agent delegation, broad persistent memory,
customer-developed code, arbitrary outbound HTTP tools, and autonomous
high-risk HCM decisions remain later capabilities with separate scope
decisions.

For a capacity baseline, qualify a tenant with 1,000 employees, 100 active
channels, 20 installed agents, and 10 concurrent runs, then measure chat
send latency, agent queue age, model/tool latency, spend, workflow timer
lateness, approval completion latency and pool wait in paired runs. Product
and Reliability sign actual thresholds and hardware before the pilot. Any
workflow SLO miss or agent-induced chat send regression blocks rollout;
optional triggers shed load before HCM admission or message durability does.

That baseline is one pilot workload, not an architectural maximum. Release
qualification also exercises a tiny tenant with intermittent use; one
tenant at its peak; many tenants on a pooled cell; one noisy tenant beside
quiet tenants; hundreds of installations of one version; bulk version
rollout and rollback; a provider quota cut; a region/cell move; and a
cross-company channel with conflicting retention and residency rules. For
each profile, record interactive and autonomous run latency percentiles,
queue age, fair-share admission, failed/expired runs, cost, and workflow/chat
impact. The signed capacity envelope states tenants, employees, agents,
installations, triggers per second, concurrent runs, context size, regions,
provider quotas, and projected growth, with scale-out and tenant-move
thresholds. Capacity beyond the proven envelope is not claimed.

| Company shape                                  | Required proof                                                                                                                                                                                |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Owner-operated company with 30 employees       | A default template can be created, reviewed, installed, paused, and explained without a specialist administrator or idle infrastructure cost dominating use.                                  |
| Multi-site service company with shift workers  | Local managers receive delegated installation rights only for their sites; multilingual and after-hours escalation work; bursty shift-change posts stay within budgets.                       |
| Multi-entity employer in several jurisdictions | Entity and effective-date scope, residency, policy, and approver rules are resolved per run; a shared agent version cannot cross a legal-entity boundary by inherited name or cached context. |
| Regulated or unionized employer                | Mandatory legal/contract controls and records rules override template settings; provider routing and retention respect data-class restrictions; an operator can explain an action trace.      |
| Franchise or cross-company partner channel     | Host and participant grants are evaluated independently, local owners manage their installations, and external membership changes suspend an unsafe agent before its next reply.              |
| Seasonal organization with 1,000 employees     | A peak trigger burst preserves human chat and workflow SLOs; quiet periods scale down; bulk channel placement and employee turnover do not create orphaned grants.                            |

## Failure scenarios to prove

1. A channel post says it is an admin instruction or embeds a hostile link:
   it remains untrusted content and cannot change the agent charter or tools.
2. A manager asks a private pay question in a mixed channel: the agent does
   not reveal pay, existence, title, citation or a count to the channel.
3. Two autonomous agents mention each other: cause-chain and budget controls
   terminate the loop without blocking human chat.
4. A document or channel grant is revoked during retrieval, generation, or
   delivery: the final answer is rechecked and no stale content is posted.
5. A model proposes a compensation change, then the proposal or policy
   changes before approval: exact digest and current authority reject the
   stale action.
6. The same event is replayed after restart: it produces at most one agent
   post and one draft/action result under idempotency, or a visible refusal.
7. An external company joins a shared channel: installed agents stop until
   bilateral scope and egress review is complete.
8. Model/provider outage or quota exhaustion: chat post/send and HCM
   workflows continue; the agent exposes typed failure and owner alert.
9. Agent owner departs or an installation is removed: new runs stop and
   memory, traces, pending tool leases and notification derivatives follow
   current access and records policy.
10. A scheduled Monday run falls into a DST fold, holiday, or outage catch-up:
    the published policy selects and records the exact occurrence once, or
    requires review; it does not silently double-post.
11. A provider returns an unknown tool name, schema-invalid arguments, or
    partial streamed output: no tool effect or public/workflow result is
    committed.
12. A workflow waits on an agent run when the worker crashes or the model
    times out: durable signal or timeout resolution takes the declared
    fallback branch, without leaving a live lease or inventing success.
13. An agent starts a workflow that invokes the same agent: cause depth,
    allowed-edge policy, and budget stop recursion before another side
    effect.
14. A scheduled agent is upgraded or moved to another provider after its
    occurrence is due: the run retains its pinned version and eligibility
    evidence, or is refused pending re-evaluation; it does not silently
    change tools or data-processing region.

## Open decisions before implementation

- Select the first two templates and their named customer owners. The
  examples above are proposals, not a promise to ship all four.
- Define the exact trigger filters, cooldowns, per-channel posting limits,
  model spend ceiling, and human handoff latency for the design partner.
- Name the HCM capabilities eligible for agent drafting and approved
  execution. A capability without a complete approval and reconciliation
  path stays unavailable to agents.
- Decide how much of agent creation, installation, run history and budget
  management external builders may operate through the HTTP API.
- Set whether any customer may publish an agent with no human review; the
  default above requires review for every material scope or write change.
- Set the first signed tenant/cell capacity envelope, pooled-to-dedicated
  placement criteria, migration SLO, and which residency regions are offered.
- Decide which industry/function packs and external source adapters are
  supported in the first pilot; every later adapter still uses the same
  retrieval and capability contracts.
- Select the first served model provider, approved regions and retention
  profile, then define adapter qualification for Anthropic, OpenAI, and any
  private endpoint actually offered. Decide whether customer-managed
  endpoints are in the first release. Provider failover remains disabled for
  a task until an equivalent eligible model passes that agent's evaluations.
- Set initial schedule recurrence types, minimum interval, owner and
  destination grants, quiet-hour defaults, DST/misfire/overlap policy,
  catch-up ceiling, and whether any external cross-company posting may be
  scheduled in the pilot.
- Publish the `AGENT_RUN` Scheduling target migration and the
  `agents.invoke` workflow capability/signaling schema; neither is served
  by the current security and scheduling libraries alone.

## Adversarial review and resolved changes

| Finding                                                | Risk in the initial draft                                                                                                  | Resolution in this revision                                                                                                           |
| ------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| One charter looked like one org chart                  | Subsidiaries, franchises, regulated branches, and multilingual teams would require forks or accidental tenant-wide grants. | Added layered policy resolution, scoped delegation, portable manifests, effective-dated organization IDs, and business-shape proofs.  |
| Manual channel installation would not scale            | Hundreds of channels would make upgrades and revocation inconsistent.                                                      | Added previewed `AgentRollout` plans that reconcile to independent per-conversation installations with no silent grants.              |
| A single queue/store was implied                       | A busy tenant or autonomous trigger storm could delay other tenants, DMs, chat, or workflows.                              | Added tenant/cell partitioning, fair admission, workload lanes, hierarchical budgets, cell migration, and a multi-tenant load matrix. |
| Tool and source growth lacked a compatibility boundary | Each new business system could require agent-runtime code or leak data through generic HTTP tools.                         | Added versioned capability, retrieval-envelope, manifest, card, event, and run contracts with fail-closed upgrades.                   |
| Existing action compiler sounded production-ready      | Its private intent-definition registry could disagree with the served BusinessIntent catalog.                              | Made catalog integration and exact approved-action execution explicit release evidence.                                               |
| The 1,000-employee baseline looked like a ceiling      | The plan did not demonstrate either tiny-tenant economics or pooled SaaS growth.                                           | Added workload profiles, signed capacity envelope, noisy-neighbor tests, and pooled/dedicated placement criteria.                     |
| Provider ownership was implicit                        | A hosted agent/session or provider tool could become an unseen source of authority or durable state.                       | Made HCM Next the owner of runs, memory, tools, schedules, and audit; adapters supply inference only.                                 |
| Invocation lacked one durable contract                 | Chat, schedules, API, events, and workflow calls could diverge on grants, retries, and budgets.                            | Added one `AgentRunRequest` admission path with source identity, dedupe, version, principal chain, audience, and budget.              |
| Scheduling had no agent target                         | Existing trigger publication names intents; a cron expression alone cannot safely launch an agent.                         | Added a versioned `AGENT_RUN` target, outbox/inbox handoff, pinned version, misfire and overlap policy, and replay proof.             |
| Workflow handoff was underspecified                    | A slow model call could hold a workflow lease or leave an instance waiting forever.                                        | Added `agents.invoke` as a typed capability with durable signal, timeout, fallback, and reverse governed workflow start.              |

Residual concerns require real benchmarks, domain review, and provider/legal
contracts before activation. This review is a design challenge, not evidence
that the agent runtime or these scale properties are implemented.

## Review history

| Date       | Review                                                   | Outcome                                                                                                                                                   |
| ---------- | -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-09-21 | Initial design draft                                     | Awaiting product scope exchange and specialist review.                                                                                                    |
| 2026-09-21 | Adversarial scalability and extensibility review         | Resolved configuration, rollout, tenancy, compatibility, capacity, and action-catalog gaps in the draft; implementation proof remains required.           |
| 2026-09-21 | Owned-agent, provider, schedule, and workflow refinement | Added common run requests, Scheduling `AGENT_RUN` target, workflow capability and durable signal path, provider adapters, and concrete business examples. |
