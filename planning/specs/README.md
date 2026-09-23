# Human Capital Management Suite Architecture Specifications

The master [plan](../plan.md) defines product strategy, governing principles, phases, and long-term architecture. This directory is the destination for focused, independently reviewable contracts extracted from that plan.

Specifications should be created only when a delivery workstream needs them. Do not pre-create dozens of empty documents.

Current specification index:

### Architecture, control, and trust foundations

- [Go-only technology constitution](go-only-technology-constitution.md)
- [Platform plane model](platform-plane-model.md)
- [Platform foundation gap closure](platform-foundation-gap-closure.md)
- [Platform responsibility boundaries](platform-responsibility-boundaries.md)
- [HTTP and gRPC endpoint contract](http-grpc-endpoint-contract.md)
- [Canonical envelope and digest](canonical-envelope-and-digest.md)
- [Capability registry and lifecycle](capability-registry-and-lifecycle.md)
- [Source authority and external mastering](source-authority-and-external-mastering.md)
- [Data classification and DLP](data-classification-and-dlp.md)
- [Secrets, key custody, and credential leases](secrets-key-custody-and-credential-leases.md)
- [SLO, SLI, and error budgets](slo-sli-error-budget.md)

### Intent, workflow, transaction, and domain kernels

- [Business Intent and Change Request](business-intent-and-change-request.md)
- [Business Intent catalog](business-intent-catalog.md)
- [Workflow runtime](workflow-runtime.md)
- [Transaction plan and commit coordinator](transaction-plan-and-commit-coordinator.md)
- [Transaction ledger, reconciliation, and repair](transaction-ledger-reconciliation-and-repair.md)
- [Cross-workflow conflict and write intent](cross-workflow-conflict-and-write-intent.md)
- [People, employment, and assignment domain](people-employment-assignment-domain.md)
- [Organization and relationship domain](organization-and-relationship-domain.md)
- [Organization scope and AuthZ](organization-scope-and-authz.md)
- [Position and headcount domain](position-and-headcount-domain.md)
- [Compensation domain](compensation-domain.md)
- [Workforce budget authority](workforce-budget-authority.md)
- [Legal rule packs and state configuration](legal-rule-packs-and-state-configuration.md)
- [Identity resolution and entity linkage](identity-resolution-and-entity-linkage.md)

### Shared product and connectivity platforms

- [Human work, forms, and rules](human-work-forms-and-rules.md)
- [Integration platform](integration-platform.md)
- [Messaging and notification plane](messaging-and-notification-plane.md)
- [Company chat and collaboration mode](company-chat-and-collaboration.md)
- [Customer agents: creation, business context, and chat](customer-agent-creation-business-context-and-chat.md)
- [Channel and team documentation hub](channel-documentation-hub.md)
- [Core-routed chat with separate message databases](chat-core-routing-and-isolation.md)
- [Customer project management and adaptive boards](customer-project-management-and-adaptive-boards.md)
- [HRIS Admin DataOps](hris-admin-dataops.md)
- [Experience UI and branding](experience-ui-and-branding.md)
- [User-flow design program](../user-flows/README.md)

### Assurance, evidence, and operations

- [Data quality and invariant evaluation](data-quality-and-invariant-evaluation.md)
- [Provenance graph and lineage](provenance-graph-and-lineage.md)
- [Records management and disposition](records-management-and-disposition.md)
- [Governance decision and obligation composition](governance-decision-and-obligation-composition.md)
- [Incident management](incident-management.md)
- [Structured logging and OpenTelemetry](structured-logging-and-opentelemetry.md)

### Strategy, inventories, and audit registers

- [Competitive positioning and authority expansion](competitive-positioning-and-authority-expansion.md)
- [Adversarial gap closure, 2026-08-13](adversarial-gap-closure-2026-08-13.md)
- [Thirty-two-reviewer adversarial audit, 2026-08-14](adversarial-audit-32-reviewers-2026-08-14.md)
- [Adversarial edge-case and tooling audit, 2026-08-14](adversarial-edge-and-tooling-audit-2026-08-14.md)
- [Specification ownership registry](specification-ownership-registry.md)
- [Platform capability coverage matrix](platform-capability-coverage-matrix.md)
- [Risk register](risk-register.md)
- [Architecture charter backlog](architecture-charter-backlog.md)
- [Platform architecture catalog](platform-architecture-catalog.md)
- [Legacy implementation baseline](legacy-implementation-baseline.md)

Retained pre-restructure behavior and cutover evidence are tracked in the
[legacy implementation baseline](legacy-implementation-baseline.md).

Each specification should contain, or have a current entry in the
[Specification Ownership and Review Registry](specification-ownership-registry.md):

- Scope and explicit non-goals
- Vocabulary and authority boundaries
- Typed contracts and lifecycle/state model
- Invariants and failure semantics
- Security, privacy, and operational obligations
- Compatibility and migration rules
- Reference scenarios and acceptance tests
- Phase implementation depth
- Owner, status, and review history

Executable schemas, policies, migrations, and tests remain authoritative over prose.
