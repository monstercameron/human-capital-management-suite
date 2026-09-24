# Specification Ownership and Review Registry

This registry is the canonical metadata source when a specification does not
carry equivalent front matter. Role ownership is sufficient during architecture
design. A named accountable person or staffed rotation is mandatory before the
owned capability is published or activated.

Status meanings:

```text
DEFINED          normative contract exists; implementation depth is phase-specific
ACTIVE_REGISTER  maintained inventory/governance register
REFERENCE        non-normative inventory, baseline or extracted design atlas
DRAFT_CONTRACT   material references/evidence remain unresolved
```

Rows present at the time received a corpus-level adversarial review on
2026-08-13. A second, 32-reviewer cross-plane adversarial audit completed on
2026-08-14 and is tracked in
`adversarial-audit-32-reviewers-2026-08-14.md`. Newer specifications, including
the draft company chat design, have not received those historical reviews.
These reviews test coherence,
negative cases and omitted responsibilities; they do not substitute for executable
conformance evidence or specialist legal, payroll, security, privacy,
accessibility, reliability, commercial, implementation, or domain certification.

| Specification                                          | Accountable owner role                                     | Status            | Next mandatory review trigger                                                                                                                                          |
| ------------------------------------------------------ | ---------------------------------------------------------- | ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `README.md`                                            | Architecture Council                                       | `ACTIVE_REGISTER` | On specification add/remove, index restructuring or status-model change                                                                                                |
| `adversarial-gap-closure-2026-08-13.md`                | Architecture Council                                       | `DEFINED`         | Before Gate A staffing and after material architecture/incident findings                                                                                               |
| `adversarial-audit-32-reviewers-2026-08-14.md`         | Architecture Council                                       | `ACTIVE_REGISTER` | Every phase gate, authority expansion, material incident or product-domain implementation claim                                                                        |
| `adversarial-edge-and-tooling-audit-2026-08-14.md`     | Platform Engineering and Security owners                   | `ACTIVE_REGISTER` | Before dependency/tool adoption, database concurrency-policy change or new export/storage boundary                                                                     |
| `architecture-charter-backlog.md`                      | Architecture Council                                       | `REFERENCE`       | When charters are added, closed, split or superseded                                                                                                                   |
| `business-intent-and-change-request.md`                | Business Transaction Service owner                         | `DEFINED`         | Before intent-instance implementation or lifecycle migration                                                                                                           |
| `business-intent-catalog.md`                           | Business Transaction and Capability Registry owners        | `DRAFT_CONTRACT`  | On each catalog partition ingestion/publication                                                                                                                        |
| `canonical-envelope-and-digest.md`                     | Canonicalization Registry and Cryptographic Custody owners | `DEFINED`         | Before hashing/signing implementation or algorithm/profile change                                                                                                      |
| `capability-registry-and-lifecycle.md`                 | Capability Registry owner                                  | `DEFINED`         | Before first capability publication or lifecycle change                                                                                                                |
| `compensation-domain.md`                               | Rewards/Compensation domain owner                          | `DEFINED`         | Before Gate B compensation write or calculation-policy change                                                                                                          |
| `company-chat-and-collaboration.md`                    | Collaboration Product and Messaging Platform owners        | `DRAFT_CONTRACT`  | Before release-scope decision, cross-company pilot, agent execution, external app installation or live-call implementation                                             |
| `chat-core-routing-and-isolation.md`                   | Core Routing, Collaboration, Data and Reliability owners   | `DRAFT_CONTRACT`  | Before chat persistence implementation, message-store sharding, cross-company pilot or all-employee activation                                                         |
| `channel-documentation-hub.md`                         | Knowledge/Content and Collaboration Product owners         | `DRAFT_CONTRACT`  | Before official publication, document database deployment, cross-company sharing, semantic indexing or agent retrieval                                                 |
| `customer-project-management-and-adaptive-boards.md`   | Project Collaboration Product owner                        | `DRAFT_CONTRACT`  | Before roadmap promotion, project persistence/API implementation, AI configuration publication, cross-company projects or live-board migration                         |
| `customer-agent-creation-business-context-and-chat.md` | Agent Product and Agent Runtime owners                     | `DRAFT_CONTRACT`  | Before customer agent creation, provider activation, schedule publication, workflow invocation, autonomous participation, cross-company installation, or HCM execution |
| `competitive-positioning-and-authority-expansion.md`   | Product Strategy owner                                     | `DEFINED`         | Quarterly and before positioning/roadmap/authority changes                                                                                                             |
| `cross-workflow-conflict-and-write-intent.md`          | Conflict Registry owner                                    | `DEFINED`         | Before Gate B concurrent writes or conflict-rule publication                                                                                                           |
| `data-classification-and-dlp.md`                       | Privacy and Security owners                                | `DEFINED`         | Before new classification, destination, declassification or egress path                                                                                                |
| `data-quality-and-invariant-evaluation.md`             | Data Quality Platform owner                                | `DEFINED`         | Before rule publication, override policy or commit-blocking use                                                                                                        |
| `experience-ui-and-branding.md`                        | Experience Platform and Accessibility owners               | `DEFINED`         | Before GWC workspace publication or widget/brand trust change                                                                                                          |
| `go-only-technology-constitution.md`                   | Platform Engineering owner                                 | `DEFINED`         | Before dependency/toolchain/runtime-language exception                                                                                                                 |
| `governance-decision-and-obligation-composition.md`    | Governance Coordinator owner                               | `DEFINED`         | Before a new governance plane or composition rule is activated                                                                                                         |
| `hris-admin-dataops.md`                                | HRIS DataOps product owner                                 | `DEFINED`         | Before customer productization or a new bulk/repair surface                                                                                                            |
| `http-grpc-endpoint-contract.md`                       | API/Transport and Capability Registry owners               | `DRAFT_CONTRACT`  | Before first public method, HTTP binding or endpoint compatibility-policy change                                                                                       |
| `human-work-forms-and-rules.md`                        | Human Work Platform owner                                  | `DEFINED`         | Before task/form/rule publication beyond Promotion slice                                                                                                               |
| `identity-resolution-and-entity-linkage.md`            | Identity Resolution owner                                  | `DEFINED`         | Before probabilistic matching, merge or separation activation                                                                                                          |
| `legal-rule-packs-and-state-configuration.md`          | Legal and Regulatory Content owner                         | `DRAFT_CONTRACT`  | Before any rule-pack release above `UNREVIEWED`, on any obligation-kind vocabulary change, and on each state research rework                                           |
| `incident-management.md`                               | Reliability/Incident Management owner                      | `DEFINED`         | After each major incident or incident-state model change                                                                                                               |
| `integration-platform.md`                              | Connectivity/Integration Platform owner                    | `DEFINED`         | Before connector certification, new adapter class or effect guarantee                                                                                                  |
| `legacy-implementation-baseline.md`                    | Go Migration/Cutover owner                                 | `REFERENCE`       | When a fixture is replaced, verified, removed or contradicted                                                                                                          |
| `messaging-and-notification-plane.md`                  | Messaging Platform owner                                   | `DEFINED`         | Before new channel/provider or legal-delivery semantics                                                                                                                |
| `organization-and-relationship-domain.md`              | Organization Domain owner                                  | `DEFINED`         | Before manager/org authoritative mutation or relationship type addition                                                                                                |
| `organization-scope-and-authz.md`                      | Authorization Platform owner                               | `DEFINED`         | Before scope/policy/enforcement or graph-freshness change                                                                                                              |
| `people-employment-assignment-domain.md`               | People Domain owner                                        | `DEFINED`         | Before authoritative People write or entity invariant change                                                                                                           |
| `platform-architecture-catalog.md`                     | Architecture Council                                       | `REFERENCE`       | After a normative contract changes an indexed architecture view                                                                                                        |
| `platform-capability-coverage-matrix.md`               | Architecture Council                                       | `ACTIVE_REGISTER` | On every discovered responsibility, phase gate or ownership change                                                                                                     |
| `platform-foundation-gap-closure.md`                   | Platform Engineering and Security owners                   | `DEFINED`         | Before affected foundation activation or recovery/security incident                                                                                                    |
| `platform-plane-model.md`                              | Architecture Council                                       | `DEFINED`         | Before ownership/dependency/plane-boundary change                                                                                                                      |
| `platform-responsibility-boundaries.md`                | Architecture Council                                       | `ACTIVE_REGISTER` | On responsibility split/merge, owner change or phase dependency                                                                                                        |
| `position-and-headcount-domain.md`                     | Workforce/Position Domain owner                            | `DEFINED`         | Before reservation/occupancy/headcount authoritative mutation                                                                                                          |
| `provenance-graph-and-lineage.md`                      | Provenance/Data Governance owner                           | `DEFINED`         | Before publisher/edge/completeness/retention rule changes                                                                                                              |
| `records-management-and-disposition.md`                | Records Management and Privacy owners                      | `DEFINED`         | Before new record series, hold, archive or deletion method                                                                                                             |
| `risk-register.md`                                     | Enterprise Risk and Architecture owners                    | `ACTIVE_REGISTER` | Each phase gate, material design change and post-incident review                                                                                                       |
| `research/security-control-matrix-2026.md`             | Architecture Council                                       | `ACTIVE_REGISTER` | On framework edition or source change, cited todo status/evidence change, or each security assurance review                                                            |
| `secrets-key-custody-and-credential-leases.md`         | Security/Cryptographic Custody owner                       | `DEFINED`         | Before key/secret provider, algorithm, rotation or recovery change                                                                                                     |
| `slo-sli-error-budget.md`                              | Reliability Management owner                               | `DEFINED`         | Before SLI/SLO semantics, exclusions or contractual target changes                                                                                                     |
| `source-authority-and-external-mastering.md`           | Data Governance/Source Authority owner                     | `DEFINED`         | Before authority assignment, handoff, precedence or dispute change                                                                                                     |
| `structured-logging-and-opentelemetry.md`              | Observability Platform, Security and Privacy owners        | `DEFINED`         | Before telemetry schema, propagation, sampling, exporter, backend or diagnostic-access change                                                                          |
| `specification-ownership-registry.md`                  | Architecture Council                                       | `ACTIVE_REGISTER` | On every specification add/remove, owner/status change or review-policy change                                                                                         |
| `transaction-ledger-reconciliation-and-repair.md`      | Transaction/Data Platform owner                            | `DEFINED`         | Before ledger assertion, repair, reconciliation or projection change                                                                                                   |
| `transaction-plan-and-commit-coordinator.md`           | Transaction Coordinator owner                              | `DEFINED`         | Before Gate B commit, new domain participant or ambiguity rule                                                                                                         |
| `workflow-runtime.md`                                  | Workflow Platform owner                                    | `DEFINED`         | Before runtime/compiler publication, primitive or durable-state change                                                                                                 |
| `workforce-budget-authority.md`                        | Workforce Planning/Finance Integration owner               | `DEFINED`         | Before budget reservation/consumption or authority-model change                                                                                                        |

## Activation gate

Before activation, each applicable row gains an operational assignment record:

```text
specification
capability/workflow/control scope
named accountable person
operational owner and on-call rotation
security/privacy/domain reviewers
support and customer-communication route
SLO/error-budget owner
deprecation/sunset owner
assigned_at
review_due_at
```

Missing, expired, departed or unreachable ownership blocks publication/activation.
Emergency reassignment is evidenced and time-bounded; it does not silently inherit
all prior authority.
