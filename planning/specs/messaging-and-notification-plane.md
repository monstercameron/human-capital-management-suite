# Messaging and Notification Plane

This specification defines Human Capital Management Suite's human Communications Plane and its relationship to workflow signals and system-to-system event delivery. The master delivery scope remains governed by [the Phase 1 execution plan](../execution-plan.md) and the P1A/P1B contents in [next-steps.md](../next-steps.md).

## Phase 1 Boundary

P1A sends nothing. P1B sends exactly one kind of message: an approval or task
notification by email, and only if the design partner's operating process
needs it. The whole of Phase 1 messaging is:

```text
MessageIntent (purpose APPROVAL_REQUIRED | TASK_ASSIGNED)
      -> audience = the resolved approver
      -> one published template, one locale
      -> one email adapter through the Integration Platform
      -> DeliveryAttempt states: QUEUED | SUBMITTED | ACCEPTED_BY_PROVIDER | FAILED
      -> workflow signal: delivery.satisfied | delivery.failed
```

The inbox, threads, replies, preferences, quiet hours, bulk plans, provider
routing, SMS/push/chat, and legal-notice evidence described below are the
destination contract. None is a Phase 1 dependency, and the secure inbox is
`MINIMAL CONTRACT`: an inbox message record readable from the Promotion
workspace, with no separate channel machinery.

Notification as a Service is used here as an architectural pattern: product domains express one semantic notification intent while shared infrastructure owns multi-channel routing, templates, preferences, and delivery observability. Human Capital Management Suite extends that pattern with secure inbox, inbound replies, durable conversations, legal evidence, workflow signals, and HR-specific data controls.

## Implemented recipient-feed storage contract (NAAS-003)

`internal/data/inbox.Store.ListPage` is the reusable Go read API. It accepts
an authenticated tenant/recipient supplied by its application caller, a bounded
page size (default 50, maximum 200), read state, archive selection, optional pin
state and a half-open creation-date range. `WorkflowNoticesPage` additionally
filters APPROVAL/TASK purpose and workflow instance. SQL filters before limiting;
the response has records and an optional continuation position, never a total
population count. Both methods use the immutable `(created_at, inbox_record_id)`
descending key, not OFFSET or mutable read/pin timestamps. An error returns no
partial page. A caller-owned transaction supplies tenant RLS; explicit tenant and
recipient predicates apply on every query. Notification ownership does not grant
authority to inspect or decide its workflow: application readers still recheck
current assignment and workflow visibility.

The position is an internal storage key, **not** an authenticated public cursor.
A public paginated transport must sign it, bind it to tenant, principal and the
canonical filters, enforce expiry and reject tampering; it must not accept a
caller-provided recipient override. It must reauthorize every page and permit an
empty authorized page with a continuation. Pages are a live feed, not a snapshot:
new inserts appear on refresh, while read/archive/pin changes can change filter
membership between requests. Existing CAS mutation and atomic publication
semantics are unchanged.

Migration `00321_inbox_feed_indexes.sql` adds recipient creation-time, read-state
and pinned-feed B-trees plus a workflow-message lookup index. These indexes add
write/WAL/storage costs; the migration builds them non-concurrently and requires
a planned maintenance window on populated cells. Rehearse on a production-sized
copy before rollout. This change does not partition tables, purge history, change
retention/legal-hold rules, or add an in-memory cache of authorization decisions.

An isolated 100,001-row PostgreSQL fixture measured a 50-row first-page p95 of
82.2 ms without the feed indexes and 0.76 ms with them; a deep page was 0.71 ms,
and unread/pinned pages approximately 0.55 ms. The deep unread plan returned 51
rows using the expected index and five cached blocks, without a sort/sequence
scan. These are warm, single-recipient storage measurements, not a production
SLA, multi-tenant throughput claim or end-to-end API latency. The joined workflow
fixture contains homogeneous approval metadata; sparse workflow/purpose filters
and million-row concurrent traffic need separate qualification.

The current browser still receives its bounded feed through `ListJourneys`;
standalone paginated gRPC/HTTP transport, client continuation/filter controls and
inbox mutations are not delivered by NAAS-003. They must use this store rather
than enumerate all journeys or materialize a recipient's entire history.

## Architectural Boundary

```text
BUSINESS EVENT / WORKFLOW NODE / HUMAN OR AGENT INTENT
                         |
                         v
                    MessageIntent
                         |
       +-----------------+-----------------+
       v                 v                 v
 Audience Resolver   Content Resolver   Delivery Policy
       |                 |                 |
       +-----------------+-----------------+
                         v
                MESSAGE ORCHESTRATOR
                         |
      +----------+-------+-------+----------+
      v          v       v       v          v
    Email       SMS     Push   Secure    Slack/Teams
                               Inbox
      +----------+-------+-------+----------+
                         v
              DELIVERY OBSERVATIONS
                         |
       delivered | failed | read | acknowledged | replied
                         |
              +----------+----------+
              v                     v
        Business Evidence      Workflow Signal
```

The central rule is:

> Workflows create communication intent. The Communications Plane determines safe delivery. Providers transport it. Observed delivery and replies return as durable workflow signals.

A workflow never calls `sendEmail(address, body)`. It invokes a governed semantic capability:

```text
communications.notify
  audience = ManagerOf(worker)
  purpose = APPROVAL_REQUIRED
  template = promotion.approval
  urgency = HIGH
  requirement = DELIVERED
```

The workflow kernel owns business escalation, waits, and deadlines. The Communications Plane owns recipient/channel resolution, rendering, delivery, provider fallback, receipt ingestion, and communication reconciliation.

## Human and System Messaging Remain Distinct

```text
HUMAN COMMUNICATIONS                   ASYNC INTEGRATION DELIVERY

person / principal / role             system subscriber
email / SMS / push / inbox / chat      webhook / queue / SFTP / callback
preferences and quiet hours            subscription filters and schemas
read / acknowledge / reply             accept / reject / retry / dead-letter
privacy and legal notice evidence       delivery and consumer checkpoint
conversation threads                    event identity and replay
```

They may share queueing, scheduling, retry libraries, provider health, rate limits, encryption, telemetry, and egress controls. They do not share recipient semantics, preference policy, content templates, acknowledgement meaning, or business evidence.

The [Integration Platform](integration-platform.md) owns external-system connections, webhook/event adapters, schemas, credentials, rate-aware scheduling, operation journals, and redrive. This specification owns human communication and defines the semantic subscription contract consumed by that delivery runtime.

## MessageIntent

```text
MessageIntent

intent_id
tenant_id
organization_scope

purpose
  APPROVAL_REQUIRED
  TASK_ASSIGNED
  REMINDER
  WORKFLOW_UPDATE
  EMPLOYEE_MESSAGE
  NOTICE
  LEGAL_NOTICE
  INCIDENT

audience_expression
audience_resolution_policy

content_ref?
template_ref?
parameters_ref

classification
urgency
delivery_requirement
reply_mode

workflow_instance_id?
human_task_id?
case_id?
business_transaction_id?

correlation_id
available_at?
expires_at?
```

The intent contains business meaning, not provider-specific addresses or rendered transport payloads. It is immutable after acceptance. Material corrections create a replacement/superseding intent rather than editing evidence of what was requested.

## Audience Resolution

Audience expressions reuse the relationship, role, organization-scope, and approval-resolution vocabulary:

```text
Worker(worker_id)
ManagerOf(worker_id)
HRBPFor(worker.organization)
Role(PayrollAdmin, scope = pay_group_17)
Group(IncidentResponders)
Principals([...])

ANY_OF(...)
ALL_OF(...)
```

```text
MessageIntent
      |
      v
resolve relationship + org scope + purpose
      |
      v
apply current identity and communication authority
      |
      v
AudienceResolution
  included principals
  excluded principals + reasons
  relationship/policy versions
  resolution timestamp
```

Resolution timing is explicit:

```text
CREATE_TIME
DELIVERY_TIME
EACH_ATTEMPT
PINNED
```

A reminder for `ManagerOf(worker)` normally resolves current authority at delivery time. A legally identified recipient may be pinned to the approved transaction context. Bulk audiences always produce a previewable population snapshot and exclusion report before execution.

Audience resolution grants no right to disclose fields. Content rendering and delivery perform current field, purpose, legal, classification, and endpoint checks independently.

## Business Identity and Delivery Endpoints

```text
DeliveryEndpoint

endpoint_id
principal_id
person_id?

channel
address_or_provider_ref

verification_status
business_or_personal

valid_from
valid_to?

allowed_purposes[]
classification_limit
jurisdiction_constraints[]
preferred_locale?

source
source_authority
last_observed_at
```

One person may have work/personal email, phone, push devices, Slack/Teams identities, and an Human Capital Management Suite inbox. Existence does not imply eligibility. Compensation details may be prohibited over SMS; a personal address may be permitted for post-employment tax documents but prohibited for internal investigations.

Endpoint verification proves control or authoritative provisioning according to channel policy. It does not prove that the current person actually read a message.

## Delivery Policy and Plan

Effective channel selection composes:

```text
message requirement
      + company policy
      + legal/privacy/DLP restrictions
      + endpoint eligibility
      + recipient preferences
      + quiet hours/business calendar
      + provider health/residency/cost
      = DeliveryPlan
```

```text
DeliveryPlan

plan_id
intent_id
audience_resolution_ref

recipient_plans[]
  principal_id
  locale
  ordered_channel_steps[]
  fallback_conditions[]
  deadline

policy_versions[]
legal_obligations[]
preference_versions[]
provider_eligibility_snapshot

estimated_cost
plan_hash
```

Example:

```text
Medical accommodation document

content delivery      secure HCM inbox
attention signal      work email: "You have a secure HR message"
prohibited            email attachment, SMS body, chat body
required              VERIFIED_RECIPIENT + ACKNOWLEDGED
fallback              human task to update secure access
```

Preferences may govern optional or ordinary operational communication. They cannot disable mandatory legal, security, payroll, or workflow delivery. Quiet-hour overrides are declared by purpose/urgency policy and remain auditable.

## Delivery Requirements and Evidence

```text
BEST_EFFORT
SUBMITTED
DELIVERED
VERIFIED_RECIPIENT
READ
ACKNOWLEDGED
RESPONDED
SIGNED
LEGAL_EVIDENCE
```

`SIGNED` is fulfilled by the Document Execution/e-signature contract, not inferred from a reply or acknowledgement. `LEGAL_EVIDENCE` is a compound jurisdiction-specific obligation, never shorthand for provider acceptance.

Transport and recipient states remain separate:

```text
DeliveryAttempt                         RecipientState

QUEUED                                 UNSEEN
SUBMITTED                              SEEN
ACCEPTED_BY_PROVIDER                   READ
DELIVERED                              ACKNOWLEDGED
BOUNCED                                RESPONDED
REJECTED
EXPIRED
FAILED
```

```text
provider accepted email  != delivered
delivered                != read
read                     != acknowledged
acknowledged             != signed
```

## Secure Inbox

The Human Capital Management Suite inbox is the canonical sensitive human-delivery channel:

```text
InboxMessage

message_id
recipient_principal_id
thread_id?

subject_ref
body_artifact_ref
classification

created_at
available_at
expires_at?

seen_at?
read_at?
acknowledged_at?

workflow_instance_id?
human_task_id?
action_capabilities[]

content_hash
access_policy_snapshot_ref
```

External channels may carry a low-sensitivity attention signal while the protected content remains behind current authentication, authorization, legal-purpose, and field controls. Inbox availability does not freeze authorization forever; access is rechecked when content is opened or an action is invoked.

Inbox retention, legal hold, export, accessibility, localization, and post-employment access follow the Records, Legal, and Globalization planes.

## Templates and Deterministic Rendering

```text
MessageTemplate

template_id
version
purpose

channel_variants
locale_variants

required_parameter_schema
allowed_data_domains[]
classification
legal_requirements[]
fallback_policy

status
effective_from
effective_to?
```

Rendering is deterministic for the pinned template/locale/parameter set:

```text
template version + locale + parameter snapshot
                        |
                        v
                 render + validate
                        |
        +---------------+---------------+
        v                               v
rendered content hash           redaction/disclosure result
```

Evidence records template/version, locale, renderer version, parameter snapshot hash, rendered-content hash, attachments/artifacts, redaction decision, and policy/legal versions. Agent-drafted prose becomes an input artifact and passes deterministic template, schema, classification, and disclosure validation before delivery.

## Conversations and Inbound Replies

```text
ConversationThread

thread_id
tenant_id
subject_type
subject_id
participants[]
purpose
classification

workflow_instance_id?
case_id?

opened_at
closed_at?
retention_policy_ref
```

```text
InboundMessage

inbound_message_id
provider_message_id
channel

sender_endpoint
resolved_principal_id?
identity_confidence

thread_id?
in_reply_to?
received_at

content_artifact_ref
attachment_refs[]
classification

correlation_id?
processing_status
```

```text
reply received
      |
signature/provider validation + malware/DLP scan
      |
sender identity + thread/correlation resolution
      |
authorized reply schema / human review if ambiguous
      |
InboundMessageRecorded
      |
HumanTask response or Workflow Signal
```

Unresolved identities, ambiguous correlation, malicious attachments, prompt-injection content, or content outside the expected reply schema are quarantined. An inbound message never becomes executable workflow instruction merely because it belongs to a known thread.

## Provider and Channel Routing

```text
Channel
  Email ------ Provider A / Provider B / customer provider
  SMS -------- Provider A / Provider B
  Push ------- APNs / FCM / web push
  Chat ------- Slack / Teams
  Inbox ------ Human Capital Management Suite
```

```text
DeliveryProfile

channel
classification
required_region
required_delivery_semantics
latency_class
cost_priority
customer_provider_preference
provider_capability_requirements[]
```

The router intersects profile requirements with customer approval, DPA/retention/residency, provider health, credential validity, throughput/rate limits, evidence capabilities, cost budget, and fallback policy. Cost never makes an ineligible provider eligible.

Provider adapters are implemented through the [Integration Platform](integration-platform.md), which supplies credentials, health, rate-aware scheduling, normalized errors, operation journals, and redrive. Communications retains semantic message/delivery evidence above those transport operations.

## Asynchronous Delivery and Backpressure

```text
workflow/business transaction
          |
   MessageIntentCreated
          |
      ACID commit
          |
      outbox relay
          |
 messaging admission + tenant/purpose priority
          |
    channel/provider queues
          |
      delivery workers
          |
    provider observations
```

Workflow execution does not synchronously depend on SMTP, SMS, push, or chat provider latency. The workflow may wait durably for a required communication state through a signal subscription.

Queues apply cell, tenant, purpose, urgency, provider, channel, and bulk fairness. Critical security/termination messages can reserve capacity; optional campaigns, reminders, and analytics notifications shed first. Retries consume shared budgets and honor provider retry-after and do-not-retry classifications.

## Bulk Messaging

Single-recipient and bulk delivery are distinct capabilities.

```text
BulkCommunicationPlan

audience_query_ref
resolved_population_snapshot
exclusions_by_reason

template_ref
channel_policy
localization_distribution

classification
legal_and_preference_result

estimated_attempts
estimated_provider_cost
rate_profile
batch_size
schedule

pause_conditions[]
kill_conditions[]
approval_requirements[]
```

```text
plan -> resolve -> preview -> cost/policy simulate -> approve
     -> resumable batches -> observe -> reconcile -> completion report
```

Bulk messaging requires population/field/export authority, cost budget, audience snapshot evidence, deduplication, rate controls, pause/kill switches, and small-campaign/canary support. A retry or restart cannot resend already satisfied recipient requirements unless an explicit redelivery decision is approved.

## Preferences, Scheduling, and Escalation

```text
NotificationPreference

principal_id
purpose
channel
enabled
priority
quiet_hours
timezone
locale
urgency_override_policy
version
```

Scheduling uses trusted time plus the Globalization calendar contracts:

```text
deadline
  - reminder offset in business hours
  - local timezone / quiet hours / holiday calendar
  + urgency override
  = scheduled delivery time + calculation trace
```

Messaging reports delivery state. The workflow engine owns business escalation:

```text
APPROVAL task created
      |
communications.notify
      |
WAIT 24 business hours
      |
no decision? -> workflow reminder/escalation policy
      |
new MessageIntent / reassignment / alternate approver
```

This prevents provider/template configuration from silently altering approval authority or workflow state.

## Communication Reconciliation

```text
Required communication state
        |
        v
DeliveryPlan and attempts
        |
        v
Observed provider + recipient state
        |
   +----+----+
   v         v
SATISFIED   UNSATISFIED / AMBIGUOUS
              |
     fallback / HumanTask / RepairPlan / escalation
```

Mandatory communication completion is a business obligation dimension. Provider outages, bounces, invalid endpoints, expired inbox access, unverified recipients, missing acknowledgements, and reply-correlation failures remain visible rather than being flattened to `sent = true`.

## Business Evidence, Operational State, and Telemetry

Material evidence classes include:

```text
MessageIntentCreated
AudienceResolved
DeliveryPlanApproved
ContentRendered
DeliveryRequested
ProviderAccepted
DeliveryConfirmed
DeliveryFailed
MessageRead
MessageAcknowledged
ReplyReceived
FallbackTriggered
CommunicationRequirementSatisfied
```

The business ledger contains material communication facts and hashes/references. The communications operational store contains provider attempts, queue state, endpoint health, and high-volume diagnostics. Telemetry contains worker scheduling, latency, network errors, and provider-adapter traces.

```text
business evidence       operations                telemetry
-----------------       ----------                ---------
notice acknowledged     attempt 3 / bounce code   provider call 842ms
reply accepted           next fallback 14:00       queue depth
requirement satisfied    endpoint health           stack/error trace
```

All three correlate through intent, delivery-plan, attempt, workflow, task, connector-operation, transaction, correlation, and trace identifiers.

## System Event Subscriptions

```text
EventSubscription

subscription_id
subscriber
event_filter
organization_scope

delivery_mode
connection_ref
destination_ref

schema_version
purpose
classification_limit

retry_policy
dead_letter_policy
rate_limit
ordering_key_expression
signature_policy

status
```

```text
Business Event
      |
subscription filter + AuthZ/purpose/egress
      |
versioned delivery envelope
      |
webhook / queue / callback / SFTP
      |
receipt / retry / dead-letter
```

System replay redelivers an existing immutable event identity and schema representation. It never invents a new business event. Endpoint test messages are explicit synthetic events that cannot be mistaken for production facts.

Webhook envelopes include event ID, occurred/recorded time, schema/version, tenant/scope, ordering key, idempotency identity, signature/key version, and delivery attempt. The Integration Platform owns endpoint credentials, health, retries, dead-letter operations, replay authorization, and operation evidence.

## Workflow Integration

```text
DOCUMENT legal notice
      |
CAPABILITY communications.notify
      |
SIGNAL wait for ACKNOWLEDGED
      | timeout 5 days
      +----------------------+
      v                      v
continue                 TASK manual contact
                             |
                        Repair / evidence
```

The workflow kernel receives typed signals such as:

```text
communications.delivery.satisfied
communications.delivery.failed
communications.message.acknowledged
communications.reply.accepted
communications.reply.requires_review
```

Signals carry only the identifiers and typed outputs needed by the waiting node. Message content is fetched through separately authorized capabilities.

## Agent Boundary

Agents may resolve an authorized audience, draft content, select a published template, propose a bulk plan, and explain delivery state. They do not receive provider credentials or direct channel APIs.

```text
agent request
    |
AudiencePlan + MessagePlan
    |
AuthZ / purpose / field / DLP / cost simulation
    |
human approval where required
    |
deterministic render + Communications capability
```

Imported messages, replies, attachments, and external templates are untrusted/tainted content. They cannot modify system instructions or select tools without the Agent Tool Security Gateway and typed authorization.

## Capability Surface

```text
communications.intents.create
communications.intents.simulate

communications.audiences.resolve

communications.messages.read
communications.messages.acknowledge
communications.messages.reply

communications.threads.open
communications.threads.read
communications.threads.close

communications.preferences.read
communications.preferences.configure

communications.delivery.status
communications.delivery.retry
communications.delivery.reconcile

communications.templates.validate
communications.templates.publish

communications.bulk.plan
communications.bulk.simulate
communications.bulk.send
communications.bulk.pause

subscriptions.create
subscriptions.pause
subscriptions.resume

webhooks.test
webhooks.replay
webhooks.health
```

Every capability declares tenant/org/population scope, fields and classifications, purpose, destination/channel, risk, bulk limits, cost, side effects, simulation, approval, idempotency, retention, and evidence obligations.

## Runtime Architecture

```text
                   COMMUNICATIONS PLANE

+-------------------------------------------------------+
| Message Intent Service                                |
| Audience Resolver                                     |
| Template + Localization Renderer                      |
| Delivery Policy + Preference Service                  |
| Secure Inbox + Conversation Service                   |
| Channel Router + Provider Eligibility                 |
| Delivery Scheduler + Retry/Fallback                   |
| Receipt/Reply Ingestion                               |
| Bulk Messaging                                        |
| Communication Reconciliation                          |
+--------------------------+----------------------------+
                           |
             +-------------+-------------+
             v             v             v
        HCM Inbox      Channel Adapters   Integration Delivery
                       email/SMS/push/    webhook/queue/SFTP
                       Slack/Teams
             +-------------+-------------+
                           v
                  Delivery Observations
                           |
             +-------------+-------------+
             v                           v
       Business Evidence            Workflow Signals
```

## Go-Only Implementation Shape

```text
internal/communications/
  intents/          audiences/        endpoints/
  policies/         preferences/      templates/
  rendering/        inbox/            threads/
  inbound/          delivery/         routing/
  scheduling/       bulk/             reconcile/
  projections/

api/proto/communications/v1/
  intent.proto      audience.proto     endpoint.proto
  template.proto    message.proto      thread.proto
  delivery.proto    preference.proto   bulk.proto

internal/integrations/
  adapters/email/   adapters/sms/      adapters/push/
  connectors/slack/ connectors/teams/  subscriptions/
```

Start in the modular Go platform with PostgreSQL for intent/inbox/runtime state, transactional outbox delivery, S3-compatible protected content artifacts, Protobuf/gRPC contracts, grpcbridge for web/inbox streams, and GoWebComponents for inbox/task/admin views. Prefer open protocols and low-cost/open-source components where total operating cost, security, deliverability, accessibility, and replacement paths are acceptable.

An evaluated open-source notification orchestrator may supply commodity routing or template components, but it cannot become the authority for HCM identity, legal delivery satisfaction, workflow state, business ledger evidence, tenant policy, or protected-content access.

## Phase Classification

| Capability                                                  | P1A     | P1B                                                  |
| ----------------------------------------------------------- | ------- | ---------------------------------------------------- |
| MessageIntent for approval/task, resolved approver audience | **OUT** | **IMPLEMENT** only if the customer path needs it     |
| One published template, one locale                          | **OUT** | **IMPLEMENT** with the above                         |
| One email adapter via Integration Platform                  | **OUT** | **IMPLEMENT** with the above                         |
| Delivery attempts and delivery signals                      | **OUT** | **IMPLEMENT** with the above                         |
| Secure inbox                                                | **OUT** | **MINIMAL CONTRACT** (inbox record in the workspace) |
| Read/acknowledged recipient states                          | **OUT** | **MINIMAL CONTRACT**                                 |
| Endpoints, preferences, quiet hours                         | **OUT** | **DESIGN / CONFORMANCE ONLY**                        |
| Conversations and inbound replies                           | **OUT** | **DESIGN / CONFORMANCE ONLY**                        |
| SMS, push, Slack/Teams provider routing                     | **OUT** | **OUT**                                              |
| Bulk messaging                                              | **OUT** | **OUT**                                              |
| Customer-configurable system subscriptions                  | **OUT** | **OUT**                                              |
| Legal-notice/e-signature evidence                           | **OUT** | **OUT**                                              |

This table agrees with the execution plan's Gate B rule: transactional
messaging is in P1B only if the selected customer path requires it, and it is
never a Gate A dependency.

## Phase 1 Acceptance Contract

- Workflow code expresses a semantic `MessageIntent`, never provider-specific delivery calls.
- `ManagerOf(worker)` and scoped-role audiences resolve with recorded current relationship and policy versions.
- Rendering cannot disclose a field unavailable under current authorization, legal purpose, classification, or endpoint policy.
- Email carries only an approved attention message; the protected content is read in the authenticated workspace.
- Message intent and workflow transaction commit durably before asynchronous provider work.
- Retries do not create duplicate logical messages or falsely satisfy delivery requirements.
- Provider acceptance, delivery, read, acknowledgement, and business satisfaction remain distinct.
- Provider outage produces queue/backpressure visibility and a workflow-observable unsatisfied state rather than blocking a request thread.
- Failed delivery can fall back or create a HumanTask without bypassing workflow escalation policy.
- Runtime/ledger/communications-operation/telemetry identifiers support end-to-end diagnosis without raw content in ordinary logs.
- Bulk and optional-channel functionality cannot become a dependency of the Promotion pilot.

## Reference

The Notification as a Service pattern—one API across channels with routing, templates, preferences, and delivery observability—is described in [SuprSend's NaaS overview](https://www.suprsend.com/post/notifications-as-a-service). Human Capital Management Suite treats this as a useful infrastructure pattern, not as a commitment to a specific provider.
