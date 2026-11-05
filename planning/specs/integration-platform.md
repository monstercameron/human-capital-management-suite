# Integration Platform

This specification defines Human Capital Management Suite's horizontal framework for external APIs, files, webhooks, events, and named vendor connectors. It is consumed by ChangeOps, HRIS DataOps, reference workflows, and future HCM domains.

## Architectural Role

External APIs must not leak vendor-specific semantics throughout domain and workflow code.

```text
External System
      |
      v
Connector Definition + Connection
      |
      +-- READ
      +-- WRITE
      +-- SUBSCRIBE
      +-- OBSERVE
      +-- RECONCILE
      |
      v
Canonical Human Capital Management Suite Capabilities
      |
      v
BusinessIntent / Workflow / Ledger / Repair
```

The five verbs are deliberately distinct:

| Verb        | Meaning                                                                                     |
| ----------- | ------------------------------------------------------------------------------------------- |
| `READ`      | Retrieve external data for a governed purpose                                               |
| `WRITE`     | Request a vendor-side mutation through a semantic operation                                 |
| `SUBSCRIBE` | Register or consume a vendor event/change mechanism                                         |
| `OBSERVE`   | Determine external state and record an `EXTERNAL_OBSERVATION`                               |
| `RECONCILE` | Compare intended/canonical state with observed external state under source-authority policy |

A connector provides transport and semantic adaptation. It does not automatically make Human Capital Management Suite or the vendor authoritative for a field. Source authority remains an independent, effective-dated tenant policy.

## Connector Families

| Family                | Representative ecosystems                      | Human Capital Management Suite use                             |
| --------------------- | ---------------------------------------------- | -------------------------------------------------------------- |
| Core HCM              | Workday, UKG, Oracle, SAP, Dayforce, ADP       | People, employment, jobs, positions, org, compensation         |
| Payroll               | ADP, UKG, Dayforce, regional payroll providers | Pay inputs/results, status, correction, reconciliation         |
| Workforce access      | Microsoft Entra/Graph, Okta                    | Users, groups, accounts, lifecycle, access observation         |
| Recruiting            | Greenhouse, Lever, Workday Recruiting, ADP     | Requisitions, candidates, applications, offers                 |
| Communications        | Slack, Teams, email providers                  | Notifications, approval links, workflow communication          |
| E-signature           | DocuSign, Adobe Acrobat Sign                   | Offers, agreements, notices, acknowledgements, evidence        |
| IT service management | ServiceNow, Jira Service Management            | HR/IT cases, access and equipment tasks                        |
| Finance/ERP           | SAP, Oracle, NetSuite                          | Cost centers, budgets, GL dimensions, workforce cost           |
| Benefits              | Carriers, brokers, benefit platforms           | Eligibility, enrollment, deductions, status                    |
| Screening             | Background-check providers                     | Candidate screening requests, statuses, adverse-action inputs  |
| Device management     | Microsoft Intune, Jamf                         | Worker/device assignment and lifecycle                         |
| Physical access       | Badge and access-control providers             | Hire, transfer, leave, and termination access                  |
| Learning              | LMS providers                                  | Courses, assignments, completions, certifications              |
| Expense/travel        | Concur-class providers                         | Worker, manager, organization, and cost-center synchronization |

Names are ecosystem hypotheses, not roadmap commitments. The first named connector is selected from paid design-partner systems and the initial Promotion workflow.

## Core Contracts

### ConnectorDefinition

```text
ConnectorDefinition

connector_id
vendor
product
connector_version
supported_vendor_versions[]

objects[]
semantic_capabilities[]

auth_schemes[]
read_modes[]
write_modes[]
event_modes[]

pagination_model
delta_model
rate_limit_model
batch_model

mapping_schema_refs[]
vendor_schema_refs[]

idempotency_profile
observation_profile
reconciliation_profile

health_checks[]
sandbox_profile
residency_profile
data_classifications[]

maturity_level
status
signature
```

Definitions are immutable, signed, dependency-addressable packages. They describe supported behavior rather than claiming that every vendor endpoint is safe or semantically normalized.

### ConnectorConnection

```text
ConnectorConnection

connection_id
tenant_id
organization_scope

connector_definition_ref
environment              SANDBOX | TEST | PRODUCTION
endpoint_profile
credentials_ref

mapping_profile_refs[]
crosswalk_profile_refs[]
sync_policy
rate_policy
egress_policy
residency_policy

external_tenant/account identifiers
granted_scope_snapshot

status
status_reason
last_verified_at
```

Connection lifecycle:

```text
DRAFT -> VALIDATING -> READY -> ACTIVE
                   \-> INVALID

ACTIVE -> DEGRADED -> ACTIVE
ACTIVE -> SUSPENDED -> ACTIVE
ACTIVE -> REVOKED
ACTIVE -> QUARANTINED
```

`REVOKED` means credentials or consent no longer authorize use. `QUARANTINED` means Human Capital Management Suite blocked the connection because of security, schema, behavioral, or data-integrity risk.

### ConnectorOperation

```text
ConnectorOperation

operation_id
tenant_id
connection_id
connector_version

business_transaction_id?
workflow_instance_id?
repair_plan_id?

semantic_operation
direction
criticality
external_resource_key
ordering_class
causal_predecessor_operation_id?
resource_sequence?
expected_external_version_or_watermark?

source_authority_decision_ref
authority_policy_fingerprint
writer_fence_epoch
authority_cutover_watermark?

canonical_input_ref/hash
mapping_profile_version
mapped_payload_ref/hash

idempotency_key
external_idempotency_key?
external_object_ref?

attempts[]
response_class
observed_result_ref
reconciliation_status

created_at
deadline
completed_at?
```

Raw sensitive payloads are stored only when required, encrypted under appropriate retention and access policy. The journal always preserves enough hashes, classifications, mapping versions, request metadata, response class, and correlation to explain what occurred.

`ordering_class` is one of `STRICT`, `COMMUTATIVE`, `INDEPENDENT`, or
`PROVIDER_CONDITIONAL`. Operations for the same external resource cannot be
reordered merely because they use different semantic-operation queues. `STRICT`
operations serialize by resource sequence. `PROVIDER_CONDITIONAL` requires an
`If-Match`-like provider precondition. A connector without ordering or conditional
write support must serialize-and-observe or remain observe-only/manual-repair.

Every leased operation revalidates source authority, writer fence, cutover
watermark, mapping and destination immediately before send. An authority handoff
must inventory pending operations and drain, cancel, quarantine or re-plan them;
pre-cutover work cannot execute under a post-cutover authority regime.

Operation lifecycle:

```text
PLANNED -> QUEUED -> LEASED -> SENT -> ACKNOWLEDGED
                              |             |
                              |             v
                              |         OBSERVING
                              |             |
                              v             v
                           FAILED      RECONCILED
                              |
                    RETRYABLE | REPAIR_REQUIRED | FINAL
```

Business completion and integration completion remain separate. An acknowledged request is not proof that the intended external state exists.

## Semantic Boundary

Domain and workflow code calls Human Capital Management Suite capabilities:

```text
worker.read
worker.compensation.change
identity.account.deprovision
communication.message.deliver
document.signature.request
```

The Integration Platform resolves a capable connection and connector implementation:

```text
semantic operation
       |
 capability + tenant/org + destination policy
       |
       v
connection resolver
       |
       v
canonical request -> vendor mapping -> transport request
       |
       v
vendor response -> normalized result/observation/error
```

Workflow definitions do not import `workday_service.go` or construct Microsoft Graph URLs. Vendor extensions are confined to connector packages, mapping profiles, and typed vendor evidence.

## Authentication and External Permission Diagnostics

Supported credential patterns may include OAuth 2.0 client credentials and delegated authorization, API keys, HTTP basic authentication where unavoidable, mutual TLS, signed webhook secrets, SFTP keys, and vendor-specific certificate flows.

Secrets remain references. A diagnostic operation reports capability, not credential material:

```text
Connection: Microsoft Entra production

network reachable                 PASS
token acquisition                 PASS
directory identity                PASS
required read privilege           PASS
required group-write privilege    FAIL
webhook validation                PASS

Impact:
  worker/account reads can operate
  group membership writes will fail

Affected:
  Onboarding v9
  Transfer v12
  Termination v7
```

External permission and consent are not substitutes for Human Capital Management Suite AuthZ. Both must allow the operation:

```text
Human Capital Management Suite authority
        INTERSECT
connection organization scope
        INTERSECT
external application/delegated privilege
        INTERSECT
current legal/egress policy
        =
eligible external operation
```

## Mapping and Crosswalk Engine

Mappings use a canonical intermediate model:

```text
SOURCE                         HCM NEXT                      DESTINATION

Workday Worker_ID       -> worker.external_id       -> ADP associate identifier
Legal_First_Name        -> person.name.given        -> Okta profile.firstName
Manager_ID              -> relationship.manager    -> Graph manager reference
Annual_Compensation     -> compensation.base       -> payroll earning input
```

```text
MappingProfile

mapping_id
version
source_schema_ref
destination_schema_ref
field_rules[]
transforms[]
defaults[]
lookup_refs[]
null_and_delete_semantics
lossiness_declaration
effective_interval
test_fixtures[]
approval/status
```

The initial transformation language is bounded and deterministic: field selection, rename, type conversion, enum/reference lookup, date/time normalization, fixed-precision money handling, string composition, conditional mapping, and explicit omission/delete semantics. Arbitrary customer code is deferred.

Reference crosswalks are first-class:

```text
                    Canonical Department
                        Engineering
                             |
       +----------+----------+----------+-----------+
       v          v          v          v           v
 Workday ENG   ADP 0017   Finance    Okta text   Payroll DEP_ENG_US
                         CC-4200
```

Every mapping records external system, connection scope, object type, external identifier, canonical resource, effective interval, mapping version, source, confidence/review state, and correction lineage.

## Schema Discovery and Change Impact

Where the vendor exposes metadata, descriptors, discovery documents, or stable example contracts:

```text
vendor schema/API version
          |
          v
discover/import -> normalize schema snapshot -> registry
          |
       compare prior
          |
    +-----+------+--------+
    v            v        v
  added       changed   removed
    +------------+--------+
                 v
        dependency impact graph
 mappings | workflows | reports | agents | tests | connections
                 |
          compatibility decision
```

Discovery output is untrusted external metadata until reviewed and registered. Automatic discovery never silently republishes mappings or changes production behavior.

For vendors without discovery, Human Capital Management Suite imports a maintained vendor schema package and detects behavioral drift through contract fixtures and observed-response classification.

## Generic Synchronization Engine

```text
SyncJob

sync_job_id
connection_id
object_type
direction         INBOUND | OUTBOUND | BIDIRECTIONAL
mode              FULL | INCREMENTAL | DELTA | TARGETED
cursor/checkpoint
source_watermark
mapping_version
authority_snapshot

read_count
mapped_count
unchanged_count
changed_count
created_count
missing_count
ignored_count
error_count

status
started_at
completed_at?
```

Snapshot and delta processing avoids blind rewrites:

```text
External snapshot/delta
        |
canonical normalize
        |
stable semantic fingerprint
        |
        +-- unchanged -> checkpoint only
        +-- changed   -> governed intent/observation
        +-- new       -> identity/reference resolution
        +-- missing   -> absence policy; never assume deletion
```

Inbound sync records observations or proposes governed changes according to source authority. Bidirectional sync is allowed only with explicit field ownership, loop prevention, correction rules, and convergence tests.

## Vendor Capacity and Scheduling

External systems are dependencies with independent quotas and failure modes.

```text
10,000 workflows
       |
 integration intents
       |
       v
queue by tenant + connection + external resource ordering key + criticality
       |
       v
Vendor Capacity Manager
 quota | concurrency | remaining | reset | latency | errors
       |
rate-aware fair scheduler
       |
       v
External API
```

```text
ExternalCapacity

connection_id
limit_window
request_limit
remaining
reset_at
concurrency_limit
observed_429_rate
observed_latency
pending_by_criticality
predicted_drain_at
confidence
```

Rate-limit headers and vendor errors update capacity state. Unknown capacity starts conservatively. Workflows receive queue/progress information instead of independently retrying `429` or timeout failures. The platform's retry-budget and backpressure contracts extend through the connector queue.

P0 termination access revocation can receive reserved capacity, but it cannot violate vendor quotas or fabricate success. Capacity exhaustion becomes visible degraded external consistency with escalation.

## Webhook and Event Runtime

Human email, SMS, push, inbox, and conversation semantics belong to the [Messaging and Notification Plane](messaging-and-notification-plane.md). This runtime supplies channel/provider and system-subscription delivery adapters without treating a human acknowledgement as an ordinary webhook receipt.

```text
External webhook/event
          |
          v
edge: size/rate/IP policy
          |
signature + timestamp + replay-window validation
          |
immutable receipt + protected payload reference
          |
schema classify -> connection resolve -> map subject
          |
          v
idempotent consumer -> observation/event -> reconcile
```

```text
WebhookReceipt

receipt_id
connection_id
vendor_event_id?
event_type
received_at
signature_status
replay_key
schema_version
payload_hash/ref
mapped_subjects[]
processing_attempts[]
correlation_id?
status
```

Admin operations include inspect, revalidate, redeliver internally, reprocess under an approved mapping version, and compare mapping results. The original receipt is never changed. Reprocessing does not imply repeating downstream side effects; consumers remain idempotent and effectful replay requires separate authority.

## Error Taxonomy and Redrive

Connectors normalize vendor failures without discarding vendor evidence:

| Class                  | Example response                                               | Default action                                     |
| ---------------------- | -------------------------------------------------------------- | -------------------------------------------------- |
| Authentication         | Expired/revoked credential                                     | Suspend writes, notify owner, require revalidation |
| Authorization/scope    | External privilege missing                                     | Final until consent/config changes                 |
| Validation             | Vendor rejected field/value                                    | Repair mapping/data; no blind retry                |
| Conflict               | External version/state changed                                 | Observe, revalidate, conflict workflow             |
| Rate limited           | Quota exhausted                                                | Queue until reset under retry budget               |
| Transient dependency   | Timeout or temporary server failure                            | Bounded retry with jitter                          |
| Ambiguous outcome      | Timeout after request may have applied                         | Observe before retry                               |
| Schema incompatibility | Field/shape/enum no longer supported                           | Quarantine affected operation/version              |
| Final business reject  | Vendor accepted request semantics but refused business outcome | Surface governed failure/repair                    |

Redrive is scoped to one failed `ConnectorOperation` or a selected compatible set:

```text
failed operations
       |
cluster by cause
       |
mapping/credential/vendor correction
       |
simulate using original canonical input
       |
compare original vs current connector/mapping/policy
       |
approval if material semantics changed
       |
new attempt under same operation lineage
       |
observe -> reconcile
```

It never reruns the parent employee workflow. If the intended business state is no longer valid, execution revalidation blocks the redrive and requires a new RepairPlan or BusinessIntent.

## Operations and Health Surface

```text
Integration Health

Workday       HEALTHY
  p95 242 ms | queue 0 | last observation 8 sec

ADP           DEGRADED
  p95 3.8 sec | queue 1,291 | 429 rate 12% | drain 22 min

Okta          INCIDENT
  authentication revoked | 812 affected workflow/effects
```

Health derives from connection tests, credential validity, external permissions, queue age, capacity, latency/errors, webhook freshness, sync cursor progress, schema compatibility, observation freshness, and reconciliation results.

```text
connection problem
       |
       v
affected operation + dependency graph
       |
 workflows | tenants/orgs | fields | deadlines | P0/P1 effects
       |
       v
incident -> degradation policy -> owner/escalation -> repair -> verify
```

## Connector Maturity Model

| Level | Name                | Contract                                                                                  |
| ----- | ------------------- | ----------------------------------------------------------------------------------------- |
| L0    | Transport Adapter   | Transport/auth/file/event mechanics; no vendor object semantics                           |
| L1    | Typed Connector     | Versioned vendor objects, operations, errors, pagination, and fixtures                    |
| L2    | Semantic Connector  | Maps supported vendor behavior to canonical Human Capital Management Suite capabilities   |
| L3    | Governed Connector  | Simulation, observation, reconciliation, idempotency, capacity, repair, schema monitoring |
| L4    | Certified Connector | Supported version matrix, reference workflows, scale/security/failure tests, owned SLO    |

Only L4 is marketed as a production-ready certified connector. L0/L1 adapters may be useful, but their UI and documentation must not imply semantic or reconciliation guarantees they do not provide.

Promotion requires evidence:

```text
L0 -> L1  schema + operation + error fixtures
L1 -> L2  canonical mapping + authority semantics + domain tests
L2 -> L3  idempotency + observe/reconcile + repair + capacity tests
L3 -> L4  supported versions + reference workflows + security + scale + SLO owner
```

## Generic Adapters

Build generic adapters only when a real connector needs them:

```text
HTTP: REST | SOAP/XML | GraphQL
Events: inbound webhook | outbound webhook
Files: SFTP | CSV | JSON | XML
Identity: SCIM
Auth: OAuth2 | API key | basic where unavoidable | mTLS
```

A generic database reader is later and read-only by default because it substantially expands network, credential, schema, and extraction risk. Direct external database writes are not a normal connector strategy.

Generic adapters handle transport. Named connectors add tested schemas, mappings, vendor-specific pagination/delta behavior, error classification, capacity interpretation, sandbox fixtures, and reconciliation probes.

## Initial Named Connector Hypotheses

The order is determined by design-partner systems and the first workflow, not market-logo collection:

1. One major HCM used by the first design partners, likely Workday or an equivalent incumbent.
2. Microsoft Entra/Microsoft Graph when manager, group, user, device, or account lifecycle provides immediate workflow value.
3. Okta for equivalent workforce-access lifecycle and group/application operations.
4. Slack or Teams for low-risk notification and approval-delivery use.
5. DocuSign when document execution becomes part of a selected workflow.
6. ADP when compensation/payroll observation and reconciliation justify the operational depth.

Each named connector has a deliberately limited support matrix. “Workday connector” is not a claim to support every Workday service, customer configuration, or API version.

## Capability Surface

```text
integrations.definitions.read
integrations.definitions.versions.list

integrations.connections.create
integrations.connections.test
integrations.connections.activate
integrations.connections.suspend
integrations.connections.health
integrations.connections.permissions.explain

integrations.schemas.discover
integrations.schemas.diff
integrations.schemas.impact

integrations.mappings.validate
integrations.mappings.simulate
integrations.mappings.publish

integrations.crosswalks.read
integrations.crosswalks.publish

integrations.sync.preview
integrations.sync.run
integrations.sync.pause
integrations.sync.resume
integrations.sync.status

integrations.operations.search
integrations.operations.read
integrations.operations.simulate_redrive
integrations.operations.redrive

integrations.webhooks.inspect
integrations.webhooks.reprocess
integrations.webhooks.simulate

integrations.reconcile
integrations.dependencies.explain
integrations.health.query
```

Every capability declares connection/environment scope, data domains and fields, external destinations, purpose, risk, bulk limits, side effects, simulation support, approval, egress, idempotency, and evidence obligations.

## Security and Isolation

- Connector definitions and packages are signed; third-party code never receives ambient platform credentials.
- Connections use scoped secret references and the minimum external privileges required by declared semantic capabilities.
- Connector workers receive short-lived workload identity and connection-scoped credential access.
- Every outbound connection uses the shared signed `DestinationTrust` and enforcing
  Egress Gateway defined in [Platform Foundation Gap Closure](platform-foundation-gap-closure.md#14-outbound-destination-trust-and-ssrf-boundary). Scheme, port, normalized host, all resolved IPv4/IPv6 addresses, private/link-local/metadata exclusions, DNS TTL/rebinding, redirects, proxy, TLS identity, residency, classification, and purpose are checked at send time; connector code cannot open arbitrary sockets.
- Tenant and connection queues, caches, payloads, cursors, rate state, and logs remain isolated.
- Raw payload inspection is purpose-bound, redacted, retained separately, and never the only business evidence.
- Webhooks enforce size/rate limits, signatures where supported, replay prevention, schema validation, and safe parsing.
- Mapping and schema inputs are untrusted data; they cannot inject executable instructions into agents or arbitrary code into the runtime.
- Production connection tests use read-only/non-mutating probes unless an explicitly approved synthetic-write fixture exists.
- Connector suspension and kill switches stop new external effects without disabling deterministic HCM or hiding already queued obligations.

## Go-Only Implementation Shape

```text
internal/integrations/
  registry/          connections/      resolver/
  operations/        scheduler/        capacity/
  mappings/          crosswalks/       schemas/
  sync/              webhooks/         reconcile/
  health/            redrive/          certification/

internal/integrations/adapters/
  http/              webhook/          files/
  oauth2/            mtls/             scim/

internal/integrations/connectors/
  <first-hcm>/       graph?/           okta?/

api/proto/integrations/v1/
  definition.proto   connection.proto  operation.proto
  mapping.proto      schema.proto      sync.proto
  webhook.proto      health.proto

cmd/
  hcm-worker integration scheduler and operation workers
```

Start inside the modular Go platform with separately scalable connector workers. A connector becomes a separate deployment only for isolation, vendor network topology, runtime dependency, scale, residency, or availability reasons.

Low-cost foundations include Go's HTTP and XML/JSON libraries, standard Protobuf contracts, `golang.org/x/oauth2`, an evaluated maintained SFTP implementation, PostgreSQL for definitions/operations/cursors, S3-compatible storage for protected payload artifacts, and OpenTelemetry for payload-free operational traces. Dependency selection follows the open-source dependency and supply-chain charters.

## Phase Classification

| Capability                                       | Phase 1 depth                                          |
| ------------------------------------------------ | ------------------------------------------------------ |
| ConnectorDefinition and ConnectorConnection      | **IMPLEMENT**                                          |
| ConnectorOperation journal and normalized errors | **IMPLEMENT**                                          |
| MappingProfile and pilot crosswalks              | **IMPLEMENT**                                          |
| Test bench, health, capacity, redrive, reconcile | **IMPLEMENT**                                          |
| One design-partner-selected named HCM connector  | **IMPLEMENT**                                          |
| Required REST/webhook/file adapter slices        | **IMPLEMENT**                                          |
| Schema snapshots and permission diagnostics      | **MINIMAL CONTRACT**                                   |
| Sync engine                                      | **MINIMAL CONTRACT**                                   |
| Microsoft Graph/Okta/Slack/DocuSign/ADP          | **DESIGN / CONFORMANCE ONLY** unless selected by pilot |
| General connector marketplace                    | **OUT OF PHASE**                                       |
| Arbitrary connector code/plugins                 | **OUT OF PHASE**                                       |
| External database write connector                | **OUT OF PHASE**                                       |

## Success Measures

- Time to configure and validate the first customer connection
- Percentage of required external privileges detected before workflow execution
- Mapping defects caught by fixture/simulation before production
- External operations with complete semantic, mapping, attempt, and reconciliation evidence
- Retry amplification and duplicate external effects
- Mean time to diagnose, repair, redrive, and reconcile failed operations
- Queue-age and predicted-drain accuracy under vendor throttling
- Webhook signature/replay/schema failures contained before consumption
- Vendor schema changes with complete dependency impact before production breakage
- Connector support incidents resolved without unsafe production access
- Time and evidence required to promote a connector from L1 through L4

## Official Ecosystem References

- [Workday REST API Directory](https://community.workday.com/sites/default/files/file-hosting/restapi/)
- [Microsoft Graph user resources and permissions](https://learn.microsoft.com/en-us/graph/api/resources/users?view=graph-rest-1.0)
- [Okta Core API](https://developer.okta.com/docs/reference/core-okta-api/)
- [Slack Web API](https://docs.slack.dev/apis/web-api/)
- [DocuSign eSignature embedded signing](https://developers.docusign.com/docs/esign-rest-api/esign101/concepts/embedding/embedded-signing/)
- [ADP Workforce Now Worker Management API](https://developers.adp.com/articles/preview/guide-worker-management-api-guide-for-adp-workforce-now-0?chapter=10)
- [ADP Workforce Now Job Applications API](https://developers.adp.com/articles/preview/guide-job-applications-v2-api-guide-for-adp-workforce-now-0?chapter=1)

## Small-business adapters and physical devices (2026-10-22)

The [small-business expansion](../small-business-platform-ideas.md) extends this same connector framework for accounting systems, customer/supplier exchanges, network-hosted appliances, and USB peripherals. Connector definitions, destination-bound leases, operation journal, idempotency, observation, retry, and reconciliation remain shared owners. Provider-specific mappings and hardware adapters add transport semantics without adding another dispatch runtime.

Keyboard-mode scans are input observations; committed business actions pass through the same domain capability as manual entry. Optional browser APIs have an explicit compatibility matrix. A local connector uses a separately admitted least-privilege workload role, tenant/site/device enrollment and outbound authenticated communication; it must not reuse the privileged server worker identity. The local durable queue is a transport spool linked to authoritative operation IDs, not a second business ledger. Disconnection, expiry, revocation, duplicate delivery and uncertain physical effects have durable states. Reprinting, payments, and other physical/external effects are never claimed exactly-once merely because a request was deduplicated.

Supported adapters, browser/OS/hardware combinations and provider editions require conformance evidence before UI availability. Connection health and permission errors appear through existing Admin integration/operations surfaces with safe retry, reconnect, cancel or reconcile actions. Secrets, local network topology, and private business records are not emitted in diagnostics or shared customer views.
