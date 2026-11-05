# Explicit Platform Responsibility Boundaries

This register converts every formerly partial, implied, or missing capability in
the coverage matrix into an explicit system boundary. It defines architecture,
not an instruction to implement all systems in Phase 1.

Every entry uses this fixed contract:

```text
OWNER     canonical authority
STATE     objects and lifecycle owned by that authority
API       semantic capability boundary
DEPENDS   permitted upstream authorities and downstream effect gateways
FAILURE   behavior when validation or a dependency fails
EVIDENCE  durable proof required
SECURITY  identity, authorization, classification, egress, and retention rules
PHASE     implementation depth
```

The detailed entries below define owner, state, API, failure, evidence, and
phase. Dependencies and security are consolidated here so they remain explicit
and comparable rather than being inferred from prose:

| Responsibility           | Permitted dependencies                                                                                               | Explicit security/privacy boundary                                                                                                                         |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Human Work               | Identity, Governance, Organization/Relationship, Forms, Workflow, Messaging, Obligation Tracker                      | Tenant/org/case compartment, current assignee authority, field masks, delegation limits, separation of duties, task-output classification and retention    |
| Master/Reference Data    | Schema Registry, Stewardship Human Work, Dependency Graph, Integration Crosswalks                                    | Steward capabilities separate propose/publish/merge; scope and classification per reference domain; no customer can mutate global data                     |
| Schema Registry          | Protobuf source, admitted Go compiler, Dependency Graph, Config Publication, Conformance                             | Signed publication, owner approval, tenant-safe visibility, no schema may weaken field classification or AuthZ without explicit migration review           |
| Knowledge/Content        | Content Ingress, Human Work, Translation, Artifact Plane, DLP, Search/RAG                                            | Author/reviewer separation, classification and audience filters before retrieval, source/tenant isolation, no unreviewed content in authoritative answers  |
| SLA/Obligation           | Source authority, Calendar/Time, Relationship resolver, Human Work, Messaging                                        | Due items reveal only permitted subject fields; waiver/satisfaction are separate capabilities; legal obligations retain applicable evidence                |
| Batch/Job                | Admission/priority, Workload Identity, Schema, Domain capabilities, Artifact/Checkpoint stores                       | Tenant/cell quotas, scoped service identity, partition field masks, bounded exports, no direct domain-table writes                                         |
| Scheduler/Trigger        | Trusted Time, tzdb/calendars, Config Publication, Admission, Workflow/Batch targets                                  | Scoped trigger publication, anti-replay dispatch keys, no arbitrary target or payload, sensitive schedule metadata classified                              |
| Managed File Transfer    | Application/Partner Identity, PKI, Content Ingress, Artifact Plane, Integration                                      | Per-partner mailbox/key isolation, encrypted transport/storage, DLP before send, quarantine before receive, filename/path traversal prevention             |
| Import/Migration         | Content Ingress, Schema, Mapping/Crosswalk, Identity Resolution, Reference Data, Domain capabilities, Reconciliation | Separate stage/approve/commit authority, row/field AuthZ, DLP and residency, encrypted artifacts, no bypass of domain validation                           |
| Document Processing      | Content Ingress, Artifact, Classification/DLP, Search/RAG, Records                                                   | Isolated parser identity, no network/domain writes, object/derivative ACLs, redaction policy, retention/hold propagated to every derivative                |
| E-Signature              | Identity Proofing, Documents, Messaging, Provider Connector, Trusted Time, Legal/Records                             | Signer assurance and consent, document hash binding, endpoint/DLP policy, provider-minimized payload, compartmented evidence package                       |
| Service Catalog          | Master/Reference, Forms, Entitlements, Governance, Workflow, Human Work                                              | Published audience/eligibility, requester/delegate AuthZ, form-field classification, no catalog item grants its own authority                              |
| Case Management          | Identity, Governance, Human Work, Documents, Messaging, Records/Legal Hold                                           | Strong case compartments, participant role checks per access, search prefiltering, conflict-of-interest controls, protected notes/evidence exports         |
| Application Identity     | Identity/PKI, Governance, Tenant, Edge, Secrets                                                                      | No shared clients across tenants without explicit multi-tenant registration; least scopes, sender constraint, rotation/revocation, owner lifecycle         |
| Tenant Lifecycle         | Cell Placement, Identity, KMS, Entitlements/Billing, Config, all plane provisioners                                  | Dual-control activation/closure, tenant-isolated resources and keys, bootstrap admin step-up, no cross-tenant repair authority                             |
| Tenant Exit              | Legal/Records, DLP/Export, Data/Artifacts, Connectivity, Identity/Keys, Billing                                      | Verified requester and dual approval, encrypted export, recipient verification, holds enforced, support/connector/key revocation, signed destruction proof |
| Sandbox/Test Data        | Environment, Synthetic/Masking, Config Packages, Feature Control, Side-Effect Fence                                  | Separate tenant/cell/credentials, production egress deny, copied-data access and expiry, visible non-production marking                                    |
| Masking/Synthetic Data   | Classification/DLP, Data Quality, Artifact, Sandbox                                                                  | Re-identification testing, secret/free-text handling, no release on privacy failure, source-to-output access does not expand                               |
| Feature Control          | Config Publication, Identity/Governance, Health/SLO, Dependency Graph                                                | Server-side signed evaluation, scoped publication, critical kill authority separated, no client override of security/entitlement                           |
| Support Access           | Identity/step-up, Customer consent, Governance, DLP, Session recording, Incident                                     | JIT tenant-bound least privilege, view-as labeling, no approval impersonation, field redaction, immutable activity, immediate revocation                   |
| Identity Proofing        | Identity, approved providers, Content Ingress, Fraud/Risk, Records                                                   | Evidence minimization/compartment, provider eligibility and egress, anti-replay/liveness where required, strict retention/deletion                         |
| BYOK                     | External/platform KMS, PKI, Data/Artifact encryption, Backup/Recovery, Tenant Lifecycle                              | Key handles not raw keys, grants bound to workloads/tenant, dual-control destructive revocation, access audit, crypto-erasure verification                 |
| Edge/API Gateway         | Identity/Application Identity, grpcbridge, Schema, Admission/Quota, Governance, DLP                                  | Default-deny registered routes, request normalization/limits, WAF/DDoS/abuse controls, tenant binding, safe logs, TLS/mTLS                                 |
| Resource Reservation     | Identity/Governance, Domain resource authority, Workflow/Transaction, Trusted Time                                   | Acquire/renew/commit are distinct capabilities, fenced leases, tenant/org/resource scope, no reservation implies execution authority                       |
| Government Filing        | Regulatory Reporting, PKI, Provider/Agency Connector, DLP, Trusted Time, Records                                     | Authorized filer and dual approval, certified endpoint/schema, minimized encrypted payload, signing-key custody, immutable filing evidence                 |
| Regulatory Content Ops   | Primary sources, Human Work, Rule Registry, Conformance, Translation, Config Publication                             | Dual expert review, signed packs, customer/country scope, protected interpretations, no agent publication authority                                        |
| Billing Tax              | Contract/Billing, Customer/Location reference, Tax content/provider, Ledger                                          | Commercial-finance roles, customer tax data compartment, immutable rate evidence, adjustments separated from original charges                              |
| Translation              | Knowledge/Content, Human Work, Locale datasets, Config Publication                                                   | Tenant/content scope, translator/reviewer roles, restricted legal content, no machine draft presented as certified                                         |
| Geospatial Resolution    | Address/domain facts, approved providers, Boundary/tzdb datasets, Jurisdiction Resolver                              | Address minimization and provider eligibility, residence/work data classification, confidence visible, no automatic legal conclusion                       |
| Fraud/Insider Detection  | Activity/Decision ledgers, Risk policy, Data Quality, Case Management                                                | Monitoring-purpose limitation, protected models/signals, need-to-know access, no autonomous adverse employment action, appeal and retention                |
| Config Package Manager   | Registries, admitted Go compiler, Dependency Graph, Config Publication, Conformance                                  | Signed packages, publisher/install authority separation, tenant/environment scope, secret references only, full install evidence                           |
| Config Dependency Graph  | All registries/publishers, Provenance, Control Publication                                                           | Publisher-authenticated edges, tenant/source ACLs, sensitive dependency redaction, completeness visible, graph cannot itself authorize effects             |
| Conformance Harness      | Build provenance, fixtures, sandbox, all plane test APIs, Evidence                                                   | Synthetic/masked data only, non-production effects, signed result bundles, waiver authority and expiry, test secrets isolated                              |
| Chaos Harness            | Reliability identity, sandbox/cell controls, SLO/Incident, Kill path                                                 | Explicit scope/approval, independent abort, production deny by default, tenant fencing, evidence and cleanup verification                                  |
| Audit Evidence Packaging | Ledger/registries/provenance, DLP/Redaction, PKI, Artifact/Delivery                                                  | Purpose and recipient authorization, minimum necessary scope, redaction review, encrypted delivery, signed manifest, expiry/revocation                     |

## Shared Interaction and Decision Services

### Human Work Management

- **Owner:** Human Work Service; Workflow creates requirements but does not own
  assignment or queue state.
- **State:** `WorkItem`, `Queue`, `Assignment`, `Claim`, `Delegation`,
  `Escalation`, `Completion`; lifecycle is `CREATED -> READY -> CLAIMED ->
IN_PROGRESS -> COMPLETED`, with `BLOCKED`, `EXPIRED`, `CANCELLED`, and
  `REASSIGNED` branches.
- **API:** `work.items.create|claim|release|reassign|delegate|complete|cancel`,
  `work.queues.query`, `work.sla.status`.
- **Failure:** an unresolved assignee enters an exception queue; it never becomes
  an unowned silent task. Completion validates output schema, current authority,
  deadline, proposal binding, and separation of duties.
- **Evidence:** resolver inputs/result, candidate set, assignments, claims,
  decisions, delegation chain, deadlines, escalations, and completion hash.
- **Phase:** implement approval and exception queues for Promotion.

### Master and Reference Data

- **Owner:** Master Data Service; each reference domain also names a steward.
- **State:** `ReferenceConcept`, `ReferenceVersion`, `Alias`, `HierarchyEdge`,
  `ExternalCrosswalk`, `StewardshipDecision`; supports draft, active, retired,
  merged, split, and superseded effective intervals.
- **API:** `reference.read|search|resolve|diff|impact`,
  `reference.versions.propose|validate|publish|retire|merge`.
- **Failure:** unknown/ambiguous codes are quarantined for resolution; callers may
  not invent canonical values. Retirement is blocked while hard dependencies
  lack a migration.
- **Evidence:** sources, steward, version, effective interval, aliases, mapping
  confidence, consumers, publication decision, and migration results.
- **Phase:** implement jobs, levels, org units, locations, currencies, and
  connector crosswalks required by Promotion. Positions are owned by the
  [Position and Headcount Domain](position-and-headcount-domain.md), not this
  reference-data service.

### Schema and Data Contract Registry

- **Owner:** Schema Registry; source definitions are Protobuf plus explicitly
  registered file/semantic schemas. The admitted Go compiler validates but is not
  a parallel authority.
- **State:** `Schema`, `SchemaVersion`, `CompatibilityPolicy`, `ConsumerBinding`,
  `Migration`, `Deprecation`, `AdoptionReceipt`.
- **API:** `schemas.read|diff|validate|dependencies`,
  `schemas.versions.propose|publish|deprecate`, `schemas.adoption.report`.
- **Failure:** incompatible publication is rejected unless every affected
  consumer has an approved migration and rollout. Unknown schema/version input
  is quarantined, not best-effort decoded.
- **Evidence:** canonical descriptor hash, compatibility result, consumers,
  generated artifacts, migration tests, rollout/adoption, and retirement.
- **Phase:** implement capability, event, workflow, connector, import, and
  projection contracts used by the pilot.

### Knowledge and Content Management

- **Owner:** Knowledge Service; Agent/RAG indexes are disposable consumers.
- **State:** `ContentItem`, `ContentVersion`, `Ownership`, `Review`, `Approval`,
  `Publication`, `EffectiveInterval`, `Retirement`, `RAGPublication`.
- **API:** `knowledge.content.read|search`, `knowledge.versions.propose|review|
publish|retire`, `knowledge.rag.publish|revoke`.
- **Failure:** expired, unapproved, quarantined, or superseded content is excluded
  from authoritative retrieval. Missing locale falls back only by declared
  policy and is labeled.
- **Evidence:** source, owner, reviewers, citations, content hash,
  classification, locale, effective period, approved audiences, RAG chunk set,
  embedding version, and revocation watermark.
- **Phase:** deferred except a small approved Promotion-policy corpus.

### SLA and Obligation Tracking

- **Owner:** Obligation Tracker; origin systems remain authoritative for why an
  obligation exists.
- **State:** `TrackedObligation`, `Deadline`, `ResponsibleParty`, `Satisfaction`,
  `Waiver`, `Escalation`; origin types include workflow, legal, contract, task,
  connector, filing, incident, and SLO.
- **API:** `obligations.track|query|satisfy|waive|escalate|explain`.
- **Failure:** unknown owner or calendar produces `UNRESOLVED`, not a guessed due
  date. A source correction supersedes the obligation without rewriting history.
- **Evidence:** source/version, trigger, calendar/version, due-date calculation,
  owner resolution, satisfaction evidence, waiver authority, and escalations.
- **Phase:** minimal common object for Promotion tasks and connector recovery.

## Batch, Data Movement, Documents, and Cases

### Batch and Job Execution

- **Owner:** Batch Runtime; Workflow remains owner of durable business-process
  graphs.
- **State:** `JobDefinition`, `JobRun`, `Partition`, `Checkpoint`, `Lease`,
  `RunResult`; states include queued, admitted, running, paused, degraded,
  completed, failed, cancelled, and repair-required.
- **API:** `jobs.plan|run|pause|resume|cancel|status|redrive`.
- **Failure:** bounded retries, durable checkpoints, fenced leases, priority and
  tenant quotas, partial-result manifest, and explicit repair; no restart from
  zero unless the job declares it safe.
- **Evidence:** input snapshot, partitions, checkpoints, attempt history,
  produced artifacts/effects, rejected rows, and completion counts.
- **Phase:** implement only import, projection, reconciliation, and extract jobs
  needed by the pilot.

### Generic Scheduling and Trigger Service

- **Owner:** Trigger Service; it starts work but does not own the resulting job or
  workflow.
- **State:** `TriggerDefinition`, `TriggerVersion`, `Occurrence`, `Dispatch`,
  `MisfireDecision`; supports cron, instant, business-calendar, event, and
  recurring-obligation triggers.
- **API:** `triggers.validate|publish|pause|resume|fire|occurrences.query`.
- **Failure:** duplicate occurrence keys dedupe; missed occurrences follow
  explicit `SKIP`, `CATCH_UP_ONCE`, `CATCH_UP_ALL`, or `REQUIRE_REVIEW`; clock
  skew blocks sensitive dispatch.
- **Evidence:** expression, timezone/tzdb/calendar versions, scheduled and actual
  times, misfire policy, target, dispatch key, and result reference.
- **Phase:** deferred; workflow timers cover the pilot.

### Managed File Transfer and EDI Gateway

- **Owner:** Managed Transfer Service; Integration Plane owns semantic mapping.
- **State:** `Partner`, `Mailbox`, `TransferProfile`, `FileManifest`, `Transfer`,
  `Acknowledgement`, `Quarantine`, `Redelivery`.
- **API:** `mft.connections.test`, `mft.files.send|receive|acknowledge|redrive`,
  `mft.manifests.verify`.
- **Failure:** incomplete, duplicate, bad-signature, undecryptable, unknown-
  schema, or unsafe files remain quarantined; partner retry cannot duplicate the
  semantic import/export.
- **Evidence:** partner identity, PGP/mTLS key version, filenames, hashes, byte and
  record counts, manifest, receipt, acknowledgements, mapping/import reference,
  and retention.
- **Phase:** deferred unless the design partner requires SFTP; then implement one
  governed profile with no generic EDI promise.

### Data Import, Staging, and Migration

- **Owner:** Data Onboarding Service; domain services own committed facts.
- **State:** `ImportSource`, `StagingDataset`, `Profile`, `Mapping`, `Resolution`,
  `ValidationResult`, `Simulation`, `CommitBatch`, `Cutover`, `Reconciliation`.
- **API:** `imports.stage|profile|map|validate|resolve|simulate|commit|resume|
reconcile|rollback_plan`.
- **Failure:** invalid rows remain staged with stable row IDs and reasons;
  commits are partitioned, idempotent, resumable, and domain-authorized. A cutover
  cannot complete until counts, totals, identities, reference mappings, and
  authority handoff reconcile.
- **Evidence:** original file hash, source IDs, row lineage, mappings/versions,
  transformations, validation, approvals, transaction IDs, rejects, and
  cutover verification.
- **Phase:** implement CSV staging and governed bulk Promotion input; full
  historical tenant migration is deferred.

### Document Processing Pipeline

- **Owner:** Document Processing Service; Artifact Plane owns original and derived
  bytes; Content Ingress owns quarantine safety.
- **State:** `Document`, `DocumentVersion`, `Inspection`, `Extraction`,
  `Classification`, `Redaction`, `Transformation`, `IndexPublication`.
- **API:** `documents.ingest|inspect|extract|classify|redact|transform|publish|
revoke`.
- **Failure:** no unsafe or unclassified content enters search/RAG/workflow use;
  parser failure produces a bounded exception and preserves the original.
- **Evidence:** source and hashes, tools/versions, inspection results, extracted
  text hash, redactions, classification, derivative lineage, and publications.
- **Phase:** implement evidence-document ingestion only.

### E-Signature and Evidence Execution

- **Owner:** Document Execution Service; external providers transport ceremonies.
- **State:** `SignatureRequest`, `SignerRequirement`, `Ceremony`, `Signature`,
  `Decline`, `Expiry`, `Countersignature`, `EvidencePackage`.
- **API:** `signatures.prepare|send|observe|cancel|resend|verify|evidence.read`.
- **Failure:** provider acceptance is not signature completion. Identity proof,
  consent, signing order, document hash, required signatures, and authoritative
  acknowledgement must all reconcile; ambiguous results enter repair.
- **Evidence:** document/template version, signer resolution, assurance level,
  consent, timestamps/trusted-time evidence, provider journal, signed bytes hash,
  certificate, and completion package.
- **Phase:** deferred.

### Service Catalog and Request Types

- **Owner:** Service Catalog; Workflow owns fulfillment execution.
- **State:** `ServiceOffering`, `RequestTypeVersion`, `Eligibility`, `InputForm`,
  `FulfillmentBinding`, `ServiceRequest`, `Disposition`.
- **API:** `services.catalog.query`, `services.requests.validate|submit|cancel|
status`.
- **Failure:** unpublished/ineligible requests are rejected with explanation;
  a retired type remains readable for historical requests; workflow binding is
  version-pinned.
- **Evidence:** offering/version, audience, eligibility decision, submitted form,
  requester/delegation, workflow instance, SLA, result, and disposition.
- **Phase:** deferred.

### Case Management Kernel

- **Owner:** Case Service; Human Work owns tasks and queues; Workflow owns process.
- **State:** `Case`, `CaseType`, `Participant`, `ConfidentialityCompartment`,
  `Note`, `EvidenceLink`, `RelatedMatter`, `Disposition`.
- **API:** `cases.create|read|assign|link|note|close|reopen`,
  `cases.evidence.attach`.
- **Failure:** compartment access is fail-closed and search-pre-filtered; deletion,
  retention, conflict-of-interest, preservation, and disclosure policies apply
  per evidence item, not merely per case.
- **Evidence:** participants/roles, access decisions, notes and revisions,
  evidence hashes, tasks/workflows, decisions, holds, disposition, and exports.
- **Phase:** deferred.

## Tenant, Application, Support, and Environment Trust

### Developer and Application Identity

- **Owner:** Application Identity Service; separate from workforce Access domain.
- **State:** `DeveloperOrganization`, `Application`, `OAuthClient`, `ServiceAccount`,
  `Credential`, `ScopeGrant`, `Consent`, `Rotation`, `Revocation`.
- **API:** `apps.register|approve|suspend|delete`, `apps.credentials.issue|rotate|
revoke`, `apps.grants.read|approve|revoke`.
- **Failure:** client authentication, tenant binding, audience, scope, redirect,
  owner, and status are checked before Governance. Orphaned owners, expired
  credentials, replayed token families, and revoked grants fail closed.
- **Evidence:** owner, software identity/version, redirect/endpoints, scopes,
  consent/approval, credential references, issuance/rotation/revocation, calls,
  and incidents.
- **Phase:** implement internal services and the pilot connector/customer app.

### Tenant Provisioning and Lifecycle

- **Owner:** Tenant Lifecycle Orchestrator; individual planes own their resources.
- **State:** `TenantRequest`, `Placement`, `BootstrapPlan`, `ProvisioningRun`,
  `Activation`, `Suspension`, `Closure`, `ExitPlan`.
- **API:** `tenants.plan|provision|verify|activate|suspend|resume|close|status`.
- **Failure:** provisioning is resumable and reconciles every plane. Activation is
  blocked until identity, admin, keys, placement, products, policies, schemas,
  recovery contacts, audit, and health checks pass. Suspension behavior is
  capability-specific and preserves payroll/legal access where contractually
  required.
- **Evidence:** requester/approvals, placement, resources, config fingerprints,
  keys, admins, entitlements, verification results, failures/repair, and state.
- **Phase:** implement one repeatable pilot tenant bootstrap.

### Tenant Exit and Portability

- **Owner:** Tenant Exit Service coordinating Data, Connectivity, Identity,
  Billing, Legal, and Support.
- **State:** `ExitRequest`, `HoldAssessment`, `ExportManifest`, `ShutdownPlan`,
  `DeletionPlan`, `ExitCertificate`.
- **API:** `tenant_exit.plan|export|verify|shutdown|destroy|certify|status`.
- **Failure:** holds and contract disputes block only affected destruction; export
  is checksum-verifiable and schema-described; credentials/connectors/support
  sessions are revoked before closure; incomplete copy inventory blocks
  certification.
- **Evidence:** scope, schema/version inventory, artifacts/hashes, legal holds,
  delivered receipts, revoked integrations/identities/keys, per-store deletion,
  backup expiry, and signed certificate.
- **Phase:** explicit contract and pilot runbook; automated exit is deferred.

### Sandbox and Test Data Platform

- **Owner:** Environment Service.
- **State:** `Sandbox`, `DataSource`, `MaskingPolicy`, `FixtureSet`, `SideEffectFence`,
  `Reset`, `Expiry`, `Destruction`.
- **API:** `sandboxes.create|seed|reset|freeze|destroy|status`.
- **Failure:** production destinations, credentials, messaging endpoints, payment,
  filing, and irreversible capabilities are denylisted by an independent fence;
  copied data expires and cannot be promoted back to production.
- **Evidence:** source snapshot, masking/synthesis versions, isolated resources,
  outbound attempts, config bundle, resets, access, and destruction verification.
- **Phase:** deterministic synthetic Promotion fixture; masked copies deferred.

### Data Masking and Synthetic Data

- **Owner:** Test Data Service.
- **State:** `GenerationPolicy`, `SourceProfile`, `MaskedDataset`,
  `SyntheticDataset`, `RelationshipMap`, `QualityReport`, `Expiry`.
- **API:** `testdata.profile|mask|synthesize|validate|destroy`.
- **Failure:** direct identifiers, rare combinations, free text, files, secrets,
  and cross-dataset linkability are risk-checked; failed privacy validation
  prevents release.
- **Evidence:** source authorization, policy/version, deterministic seed where
  allowed, transformations, privacy tests, retained relationships, synthetic
  provenance, consumers, and destruction.
- **Phase:** synthetic fixtures only.

### Feature Flags and Progressive Rollout

- **Owner:** Feature Control Service.
- **State:** `Feature`, `FlagVersion`, `TargetRule`, `Evaluation`, `Rollout`,
  `KillAction`, `Expiry`.
- **API:** `features.evaluate|explain`, `features.rollouts.plan|start|pause|
rollback|complete`, `features.kill`.
- **Failure:** missing/expired rules use an explicit safe default; security and
  authority features cannot be enabled by a client-side flag. Evaluation is
  deterministic from a signed local snapshot and bounded attributes.
- **Evidence:** rule/version, target, evaluation reason, rollout cohorts, health
  gates, overrides, kill action, and cleanup/expiry.
- **Phase:** pilot tenant/cell canary and kill switches.

### Customer Support Access

- **Owner:** Support Access Service; customer and platform security policies both
  constrain sessions.
- **State:** `SupportRequest`, `CustomerConsent`, `Elevation`, `ViewAsSession`,
  `Recording`, `Revocation`, `AfterActionReview`.
- **API:** `support_access.request|approve|start|inspect|revoke|close`.
- **Failure:** no standing broad access; tenant binding, purpose, fields,
  duration, ticket, consent, step-up, and separation of duties are mandatory.
  Redacted view-as is distinct from impersonation; support cannot create customer
  approvals or conceal its identity.
- **Evidence:** requester, customer approver, reason, grants, queries/actions,
  redactions, recording reference, expiry/revocation, and review.
- **Phase:** manual JIT support for the pilot with automatic expiry and audit.

### Identity Proofing and Verification

- **Owner:** Identity Proofing Service; providers supply observations, not final
  assurance decisions.
- **State:** `ProofingRequest`, `EvidenceItem`, `Verification`, `FraudSignal`,
  `ManualReview`, `AssuranceDecision`, `RetentionAction`.
- **API:** `proofing.start|submit|verify|review|complete|explain`.
- **Failure:** confidence below the requested assurance routes to retry/manual
  review/deny; provider outage cannot silently lower assurance; evidence access
  is compartmented and minimized.
- **Evidence:** requested level, methods/providers/versions, consent/notices,
  evidence hashes, checks, fraud signals, reviewer, decision, expiry, deletion.
- **Phase:** deferred except step-up identity confirmation for support and
  critical approvals.

### Customer-Controlled Encryption and BYOK

- **Owner:** Key Custody Service; tenant controls grant/revoke where contracted.
- **State:** `TenantKeyPolicy`, `KeyReference`, `Grant`, `Rotation`, `Revocation`,
  `RewrapCampaign`, `CryptoErase`, `RecoveryBinding`.
- **API:** `tenant_keys.register|verify|grant|rotate|revoke|rewrap|status`.
- **Failure:** unavailable/revoked keys make affected domains explicitly
  unavailable; services never fall back to platform keys. Backup, restore,
  search, analytics, and derived artifacts declare key dependencies.
- **Evidence:** ownership, KMS identity, algorithms, scopes, grants, access,
  rotations, rewrap coverage, outages, recovery proof, and erase verification.
- **Phase:** architecture contract only; platform-managed envelope encryption in
  Phase 1.

### Edge and API Security Gateway

- **Owner:** Edge Gateway; Governance remains authoritative for business access.
- **State:** `RoutePolicy`, `ClientTrust`, `RequestEnvelope`, `AbuseDecision`,
  `RateDecision`, `EdgeIncident`.
- **API:** grpcbridge exposes only registered Protobuf capabilities; management
  APIs cover routes, client trust, limits, blocks, and evidence queries.
- **Failure:** unknown routes/methods/schemas, invalid authentication, oversized
  requests, invalid encodings, replay, disallowed content, and exhausted quotas
  fail before capability execution. Load shedding respects P0-P4 criticality.
- **Evidence:** normalized request ID, route/capability/schema, client identity,
  tenant, controls evaluated, rate/abuse decision, response class, and trace; no
  secrets or unrestricted HCM payloads in access logs.
- **Phase:** implement pilot HTTP/gRPC edge, request limits, authentication,
  quotas, abuse controls, and DDoS-provider integration.

## Regulatory and Commercial Boundaries

### Resource Reservation

- **Owner:** Reservation Service; resource domain validates availability and owns
  final consumption.
- **State:** `ReservationType`, `Reservation`, `Hold`, `Commit`, `Release`,
  `Expiry`, `Conflict`.
- **API:** `reservations.check|acquire|renew|commit|release|query`.
- **Failure:** acquisition is atomic over normalized resource/field/time scopes;
  leases are fenced; expiry never commits; execution revalidates both reservation
  and domain version.
- **Evidence:** requester, purpose, resource/write set, quantity, effective range,
  baseline, expiry, renewals, conflicts, commit/release.
- **Phase:** positions, compensation budget, and affected worker fields only.

### Government Filing Gateway

- **Owner:** Filing Gateway; Regulatory Reporting owns filing contents.
- **State:** `AuthorityConnection`, `FilingSubmission`, `TransmissionAttempt`,
  `Acknowledgement`, `Rejection`, `Amendment`, `Reconciliation`.
- **API:** `filings.validate|submit|status|acknowledge|amend|reconcile`.
- **Failure:** transport receipt is not acceptance; duplicate submission IDs are
  reconciled; ambiguous outcomes block resubmission until authority state is
  observed; corrections append an amendment.
- **Evidence:** filing package/version/hash, signer/certificate, endpoint/schema,
  attempts, authority IDs/responses, acceptance/rejection, amendments.
- **Phase:** deferred until a filing jurisdiction is selected.

### Legal and Regulatory Content Operations

- **Owner:** Regulatory Content Operations with named legal/domain approvers.
- **State:** `SourceNotice`, `Interpretation`, `RulePackChange`, `GoldenCaseSet`,
  `Certification`, `Publication`, `Expiry`, `RegulatoryIncident`.
- **API:** `regulatory_sources.ingest`, `rulepacks.propose|test|review|certify|
publish|quarantine`, `regulatory_changes.impact`.
- **Failure:** unverified sources and failed golden cases cannot publish; urgent
  ambiguity declares a known exclusion or human obligation, never invented law;
  bad packs can be quarantined without erasing historical reproducibility.
- **Evidence:** primary sources/citations, interpretation owner, reviewers,
  tests/results, effective dates, coverage/exclusions, certification, rollout.
- **Phase:** conformance-only until a country pack is selected.

### Billing Tax Engine

- **Owner:** Commercial Tax Service; separate from employee payroll tax.
- **State:** `TaxRegistration`, `TaxabilityRule`, `CustomerTaxProfile`,
  `TaxCalculation`, `Exemption`, `Adjustment`.
- **API:** `billing_tax.resolve|calculate|explain|adjust`.
- **Failure:** missing nexus/customer evidence prevents final invoice or routes
  governed manual review; historical invoices retain calculation/rule version;
  corrections append credit/debit adjustments.
- **Evidence:** seller/buyer locations, registrations, exemptions, product tax
  classification, rule/provider version, basis, rates, rounding, amounts.
- **Phase:** deferred; Phase 1 contract is fixed-price/manual invoicing.

### Translation Management

- **Owner:** Localization Service; legal content requires an additional certified
  reviewer designated by Regulatory Content Operations.
- **State:** `SourceString`, `TranslationUnit`, `Glossary`, `TranslationMemory`,
  `Review`, `Certification`, `Publication`, `Invalidation`.
- **API:** `translations.propose|review|certify|publish|coverage|invalidate`.
- **Failure:** missing required locale blocks publication when completeness is
  mandatory; machine translation remains labeled draft; source change
  invalidates dependent translations.
- **Evidence:** source/version/hash, locale, translator/tool, glossary/memory
  versions, reviewer/certifier, differences, publication and consumers.
- **Phase:** one English source locale; fallback contract tested.

### Geospatial and Work-Location Resolution

- **Owner:** Location Resolution Service; Jurisdiction Resolver makes legal
  conclusions.
- **State:** `AddressAssertion`, `NormalizedAddress`, `GeocodeObservation`,
  `BoundaryDataset`, `LocationResolution`, `Dispute`, `Correction`.
- **API:** `locations.normalize|geocode|resolve_timezone|resolve_boundaries|
explain|correct`.
- **Failure:** confidence and ambiguity are returned; no result is silently
  promoted to work location or jurisdiction. Provider errors and boundary
  versions remain visible.
- **Evidence:** submitted address, source/actor, provider/dataset/version,
  candidates/confidence, chosen result, evidence date, dispute/correction.
- **Phase:** validated configured work locations only; live geocoding deferred.

## Configuration, Assurance, and Abuse Boundaries

### Fraud and Insider-Abuse Detection

- **Owner:** Risk Detection Service; Case Management owns investigations.
- **State:** `DetectionRuleOrModel`, `Signal`, `RiskEvent`, `Alert`, `Disposition`,
  `Feedback`, `Suppression`.
- **API:** `risk.evaluate|alerts.query|explain|disposition|suppress`.
- **Failure:** a detection signal does not become an employment conclusion or
  autonomous adverse action. High-risk actions may be held by deterministic
  policy; otherwise signals route review. Protected monitoring data has separate
  access/retention.
- **Evidence:** inputs/provenance, rule/model/version, score/reasons, affected
  action, reviewers, disposition, false-positive feedback, and appeal.
- **Phase:** deterministic bulk-export, bank-change, privilege, break-glass, and
  fake-worker signals only.

### Customer Configuration Package Manager

- **Owner:** Configuration Package Service.
- **State:** `Package`, `PackageVersion`, `DependencyLock`, `Signature`,
  `InstallationPlan`, `Installation`, `Upgrade`, `Rollback`, `Uninstall`.
- **API:** `config_packages.build|verify|diff|impact|install|upgrade|rollback|
uninstall`.
- **Failure:** unresolved/incompatible dependencies, unsigned content, prohibited
  scope, failed simulation, or missing rollback block installation. Uninstall
  preserves historical executions and detects live references.
- **Evidence:** manifest/hash/signature, included objects, dependencies,
  environment/tenant, approvals, simulations, applied fingerprints, receipts.
- **Phase:** Promotion workflow/config bundle only.

### Configuration Dependency Graph

- **Owner:** Dependency Graph Service; source registries publish edges.
- **State:** `DependencyNode`, typed `DependencyEdge`, `GraphWatermark`,
  `ImpactQuery`, `CompletenessReport`.
- **API:** `dependencies.publish|remove|traverse|impact|consumers|completeness`.
- **Failure:** graph results state source watermarks and missing publishers; an
  incomplete graph cannot authorize destructive retirement. Critical publication
  declares mandatory edge types and blocks on missing edges.
- **Evidence:** publisher, source/version, edge type, observed/published time,
  query parameters/watermarks, affected set, and completeness.
- **Phase:** schemas, capabilities, workflow, connector mapping, reference data,
  feature, and configuration edges.

### Test Scenario and Conformance Harness

- **Owner:** Conformance Platform.
- **State:** `Scenario`, `FixtureVersion`, `Environment`, `Run`, `Assertion`,
  `EvidenceBundle`, `Waiver`.
- **API:** `conformance.plan|run|compare|evidence|waive`.
- **Failure:** required reference-workflow failures block release; flaky tests are
  quarantined as defects with an owner/expiry and cannot silently disappear;
  fixtures pin schemas/config/reference datasets.
- **Evidence:** source/build/config fingerprints, fixtures, simulated providers,
  expected/actual ledger and projections, traces, assertions, waiver.
- **Phase:** execute Promotion happy path, conflict, stale approval,
  revalidation, connector ambiguity, repair, access, accessibility, and recovery.

### Fault Injection and Chaos Harness

- **Owner:** Reliability Engineering.
- **State:** `FaultDefinition`, `ExperimentPlan`, `SafetyFence`, `Run`,
  `Abort`, `Cleanup`, `Finding`.
- **API:** `faults.catalog`, `chaos.plan|approve|run|abort|verify_cleanup`.
- **Failure:** experiments require environment/tenant/cell fences, stop
  conditions, maximum duration, recovery owner, and independent kill path;
  production experiments require explicit authorization.
- **Evidence:** hypothesis, scope, injected fault, timestamps, affected SLOs,
  stop actions, cleanup/invariant checks, findings/remediation.
- **Phase:** non-production connector timeout, duplicate event, lease expiry,
  projector lag, config rollback, and restore exercises.

### Audit Evidence Packaging

- **Owner:** Evidence Export Service; source ledgers/registries remain authority.
- **State:** `EvidenceRequest`, `Scope`, `Manifest`, `EvidenceItem`, `Redaction`,
  `Signature`, `Delivery`, `Revocation`.
- **API:** `audit_packages.plan|generate|verify|deliver|revoke`.
- **Failure:** missing watermarks, unverifiable hashes/signatures, unresolved
  redaction, or absent source versions produce an incomplete package explicitly;
  the service never fabricates completeness.
- **Evidence:** query/scope, sources and watermarks, included/excluded items,
  redactions/reasons, manifest hashes/signature, recipient, delivery/expiry.
- **Phase:** Promotion transaction package covering intent through reconciliation.

## Explicit Dependency Order

```text
Go-only contracts + Identity + Schema + Control distribution
                         |
          +--------------+---------------+
          v                              v
  Master/reference                Edge/workload trust
          |                              |
          +--------------+---------------+
                         v
       Workflow + Human Work + Domain capability
                         |
          +--------------+---------------+
          v                              v
  Import / Integration             Messaging / Documents
          |                              |
          +--------------+---------------+
                         v
       Reconciliation + Evidence + Operations
                         |
                         v
       deferred cases / service catalog / regulatory depth
```

No deferred subsystem may be implemented inside an earlier service merely to
avoid naming it. If a Phase 1 dependency requires more depth, update this
contract, the coverage matrix, execution scope, threat model, and conformance
suite before implementation.

## Go-Only Realization

Every implementation described here is Go. Protobuf defines APIs and events;
grpcbridge is the approved transport adapter; GoWebComponents provides UI;
The admitted deterministic Go compiler processes structured schemas, mappings,
rules, graphs, packages, and evidence manifests. SchemaFlux remains subject to
the qualification policy in the production frontend specification; its name
does not grant production-generator admission. There is no TypeScript, Node.js, npm, React, or Vite product
toolchain.

<!-- SMB-EXPANSION-20261022:START -->

## Small-business operational ownership extension

The owner-authorized PHASE_3 / PHASE_4 [expansion plan](../small-business-platform-ideas.md) reuses these shared responsibilities. Its [per-ID contracts](../../definitions/planning/smb-expansion.json) specify the exact implementation deltas and adversarial findings. Existing Phase 1 release limits continue to apply to that release; they do not prohibit the separately planned later-phase domain extensions.

- Organization/relationship owners add non-inheriting customer/supplier party roles; contact/address values and identity linkage are reused with purpose checks. Recruiting CRM and workforce hierarchy authority retain their existing meanings.
- Existing work orders, templates, access, appointment/scheduling, human work, project recurrence and reservations coordinate business jobs. A task/board transition does not directly post stock, issue an invoice, release equipment or charge a customer.
- Inventory owns quantity, movement, quarantine and consumable reservation semantics. Existing asset ownership remains the authority for physical custody, serviceability and retirement. A serialized item may link these records without merging their distinct lifecycles.
- Procurement owns requisitions, purchase commitments, receipt inspection and three-way match evidence. Billing owns supplier payables/credits and customer receivables/adjustments. Suite subscription entitlements in `internal/commercial` remain separate. Settlement and accounting connectors own their existing execution/observation boundaries.
- Approved financial facts use exact decimal/currency and pinned rules; issued records retain correction history. Tax and accounting mappings remain explicit, versioned and qualified; this expansion is not evidence of a universal tax engine or full general ledger.
- Existing configuration, capability and page registries compose optional modules. Existing DataOps owns staging/dry-run/commit receipts; each domain import adapter invokes its authoritative commands. Opening stock is a movement, not a direct balance overwrite.
- Existing connector operations, workload identities, destination leases and reconciliation carry device and accounting adapters. A local device connector is a scoped observation/transport boundary, not a business ledger or administrator tunnel. Ambiguous physical outcomes require reconciliation and do not justify automatic replay.
- External access, notifications, search, reporting and exports enforce current object/field/company authority. Shared UI primitives preserve the global shell; new hubs expose only enabled, authorized operations.

Every cross-domain handoff declares source identity/revision, purpose and authority, idempotent application, durable receipt, correction ownership and reconciliation. Dependencies order implementation; they do not force inventory/devices/accounting modules into a service-only company's runtime configuration. Failures prohibit authoritative business side effects while permitting sanitized, policy-approved security-denial telemetry.

<!-- SMB-EXPANSION-20261022:END -->
