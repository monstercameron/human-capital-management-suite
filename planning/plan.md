# Human Capital Management Suite High-Level Plan

This document is the durable product strategy and architecture constitution. It defines what Human Capital Management Suite is, the invariants that must remain true, the authority-expansion path, and the people-and-work foundation for a small-business operations workspace. It is intentionally broader than the executable delivery plan.

Use the planning set according to this hierarchy:

```text
planning/plan.md
  product thesis + architecture constitution + long-term direction
            |
            +--> planning/execution-plan.md
            |      delivery gates and their acceptance
            |
            +--> planning/next-steps.md
            |      exact P1A / P1B release contents; wins over any spec's
            |      phase table where they disagree
            |
            +--> planning/specs/
                   focused contracts owned and evolved independently
            |
             +--> reference-workflows/
                    executable conformance scenarios as they are extracted
            |
            +--> planning/specs/legacy-implementation-baseline.md
                   retained behavior, fixture and cutover evidence
```

When this document describes a complete subsystem, that is not automatically a commitment to implement it in Phase 1. Section 13 and the execution plan classify each capability as **implemented now**, **contracted minimally**, or **deferred**.

Two standing rules keep this document from growing in place of the product:
the adversarial audits are frozen inputs and no further audit pass runs until
P1A executes; and no lifecycle dimension, kernel family, workflow primitive,
or coordination layer is added without a scope exchange recorded in the
execution plan.

## 1. Executive Summary

Human Capital Management Suite is an HR and workflow platform for small businesses, with a planned expansion into connected business operations. It helps people request, assign, approve, execute, observe, reconcile, and explain work across employees, customers, suppliers, jobs, resources, and commercial records. Domain-specific rules stay with their semantic owners; the shared platform supplies identity, authorization, workflow, records, integrations, and one coherent experience.

The existing **Human Capital Management Suite ChangeOps** release remains the bounded workforce transaction proof: promotions, compensation adjustments, manager changes, and organizational moves. The owner-authorized small-business expansion recorded on 2026-10-22 is a delivery backlog, not merely a destination sketch; its feature contracts, reuse decisions, and acceptance criteria are in [the small-business expansion plan](small-business-platform-ideas.md). Neither this planning decision nor a GitHub issue claims shipped functionality or grants production data authority.

The product does not begin by replacing Workday, UKG, Oracle HCM, SAP SuccessFactors, Dayforce, or customer-built systems. It begins by controlling the fragmented transaction process around them.

```text
Existing HCM systems
        +
Human Capital Management Suite ChangeOps
        =
safer, faster, explainable employee changes
```

The long-term opportunity is larger than becoming a system of record for a few HR domains. Human Capital Management Suite can become a **Workforce Operating System**: a unified system spanning people, workforce operations, talent, rewards, employee experience, and workforce access on one shared Person/Worker Graph.

The 2026-10-22 product decision extends that destination to optional small-business operations. ChangeOps remains a retained proof and integration path, while customers may enter through people, work, or a supported business workflow. Production authority is earned per domain through transaction safety, operational reliability, customer approval, and served acceptance evidence. Existing Gate A/B contents remain bounded; expansion work is sequenced explicitly in the execution plan.

## 2. Strategic Thesis

Small businesses need dependable coordination across people, customer work, resources, and commercial administration without building their own integrations. The common foundation is governed work with clear ownership and observable outcomes. HR remains a first-class domain and the Person/Worker Graph remains authoritative for workforce facts. Customer and supplier relationships extend the workspace without turning every contact into an employee. The enterprise ChangeOps use case below remains a demanding proof of the same mechanisms.

A single promotion may involve:

- A manager and employee
- HR and compensation teams
- Finance and payroll approval
- Effective-dated HCM records
- Position and organization structures
- Identity and access changes
- Payroll and finance integrations
- Policy and compliance review
- Audit and reconciliation

Most enterprises manage this process through a mixture of suite workflows, tickets, spreadsheets, email, chat, manual entry, and custom integrations. Each tool sees part of the process, but no system reliably governs the whole transaction.

Human Capital Management Suite creates a shared transaction model above those systems. Its value comes from reusable workflow mechanics combined with owned business semantics, including:

- HCM-specific transaction semantics
- Effective-dated employee changes
- Immutable proposals and approvals
- Cross-workflow conflict management
- Permission-aware experiences
- Transparent simulation
- Controlled cross-system execution
- Reconciliation and repair
- Historically explainable decisions
- Governed AI assistance

## 3. Product Vision

### 3.1 Vision Statement

> Give a small business one dependable workspace for its people and work, with connected operations that remain understandable, governed, and repairable.

### 3.2 Category

Primary category:

> HR and Business Workflow Platform

Buyer-facing category:

> Small-Business Operations Workspace

Enterprise HR ChangeOps remains a supported positioning for the existing bounded transaction pilot.

### 3.3 Day-One Positioning

> Organize your people and work, then connect the operations your business needs.

For an existing enterprise HCM estate, the retained ChangeOps proposition is to coordinate workforce changes across the systems the customer already runs.

### 3.4 Long-Term Positioning

> One workspace for running a small business, built around its people and the work they do.

The workforce model connects HR, work, pay, talent, experience, and access. Optional customer, job, scheduling, commercial, inventory, purchasing, equipment, portal, accounting, device, and setup capabilities connect through existing platform contracts. They retain separate data authority and lifecycle rules.

`Workforce Operating System`, AI, workflow, APIs, webhooks, analytics, and
orchestration are not treated as unique positioning by themselves. Incumbent
suites already market overlapping categories and language. The durable thesis is:

> Turn every material workforce action into a governed, inspectable,
> authority-aware, reconcilable, and repairable transaction.

### 3.5 Product Boundary

The bounded ChangeOps P1A/P1B release does not initially implement:

- A complete HCM suite
- A payroll calculation engine
- A benefits administration platform
- An applicant tracking system
- A generic ticketing product
- A generic workflow builder
- An autonomous AI decision maker
- A universal integration platform

Within that bounded ChangeOps release, Human Capital Management Suite provides:

- The governed entry point for selected employee changes
- The place where proposed changes are validated and approved
- The source of truth for transaction intent and history
- The coordinator of controlled writes to existing systems
- The place where intended and observed outcomes are reconciled
- The audit surface for decisions, execution, failure, and repair

### 3.6 Workforce product family within the operations workspace

Sections 3.6 through 3.11 retain the workforce-family destination sketch. The separately adopted small-business contracts in section 3.13 and the expansion plan have an explicit implementation backlog. The retained workforce sketch exists so
that the kernel does not paint the product into a corner; it is not a
backlog, and nothing in the kernel, catalog, planes, registries, or data
models may be sized to that sketch alone. Stages 3 to 5 of the retained workforce path are options
with their own evidence gates.

HCM is the umbrella category rather than one application beside recruiting, payroll, time, and talent. Major HCM suites already group core HR, talent acquisition, talent management, learning, compensation, benefits, payroll, time, absence, workforce planning, and analytics into connected portfolios. Rippling extends the boundary further by connecting HR with workforce identity, application access, devices, and selected finance operations around common workforce data.

The retained workforce-family and enterprise ChangeOps track has two related identities:

```text
Near-term category:
    HCM Transaction Control Plane

Long-term product:
    Workforce Operating System
```

The complete system should not become a loose collection of HR modules sharing a brand. Workforce features use the same worker identity and employment facts. All product families reuse platform identity, authorization, workflows, ledger mechanics, documents, policies and event contracts while retaining domain-owned facts. Customer/supplier contacts are linked explicitly; they neither require fictitious employment nor gain access to HR records.

### 3.7 Architectural Center: Person/Worker Graph

The Person/Worker Graph remains the workforce data center. The wider workspace connects it to separately owned business parties, service locations, work orders, resources and commercial records through typed references and current authorization. An organization/customer/supplier record need not represent a person, employee or HR organization unit.

```text
                              PERSON
                                │
        ┌───────────────────────┼────────────────────────┐
        │                       │                        │
 CandidateRelationship   Employment/Engagement    Identity Relationships
 application/history      current and historical   login/account/external IDs
        │                       │                        │
        │        ┌──────────────┼──────────────┐         │
        │        │              │              │         │
        │     Job/Org        Talent/Time    Rewards       │
        │     Position       Skills/Leave   Pay/Benefits  │
        │        │              │              │         │
        └────────┴──────────────┴──────────────┴─────────┘
                                 │
                    facts + events + workflows + policy
```

The graph contains stable shared concepts:

- Person and identity anchors
- Candidate, worker, contractor, and alumnus roles or relationships that may overlap
- Employment and engagement relationships
- Job, position, organization, manager, location, and legal entity
- Compensation, benefits, payroll, time, and schedule relationships
- Accounts, roles, groups, applications, devices, and entitlements
- Skills, goals, performance, learning, and certifications
- Cases, documents, communications, and employee interactions
- Effective-dated facts and complete worker history

`Candidate`, `Worker`, and `Former Worker` are not mutually exclusive Person states. A current worker may be an internal candidate; a contractor may also apply for employment; a former employment may coexist with a new candidate relationship. “Former worker” is derived from historical employment relationships rather than stored as an exclusive human identity.

The Person/Worker Graph is what makes the future suite unified. A change to a worker's position can affect compensation eligibility, approval authority, schedule, learning requirements, application access, equipment, reporting, and workforce plans without requiring each product to invent a separate employee identity.

### 3.8 Six Product Pillars

The eventual product portfolio should be presented through six buyer-friendly pillars rather than eighteen disconnected top-level applications.

| Pillar         | Product families                                                    | Unifying question                                    |
| -------------- | ------------------------------------------------------------------- | ---------------------------------------------------- |
| **People**     | Core HR, organization, documents, onboarding, offboarding           | Who is this worker and how are they employed?        |
| **Workforce**  | Time, attendance, scheduling, availability, absence, leave          | When, where, and under what conditions do they work? |
| **Talent**     | Recruiting, performance, goals, skills, learning, succession        | How are they hired, developed, and moved?            |
| **Rewards**    | Compensation, benefits, payroll, bonuses, equity                    | What do they cost and what are they entitled to?     |
| **Experience** | Communications, surveys, recognition, HR service delivery, cases    | How do they interact with the organization and HR?   |
| **Access**     | Workforce identity, accounts, roles, applications, devices, reviews | What resources may they use because of their work?   |

Three horizontal layers support every pillar:

```text
People  Workforce  Talent  Rewards  Experience  Access
   └────────┴────────┴────────┴──────────┴────────┘
                           │
          ┌────────────────┼────────────────┐
          ▼                ▼                ▼
       Workflow       Intelligence       Platform
       and Ledger     Analytics + AI     Security
                                         Integration
                                         Documents
                                         Search
                                         Notifications
```

### 3.9 Product Families

The six pillars decompose into the product families an incumbent suite
markets: core HR, workforce management, payroll, compensation, benefits,
recruiting, onboarding and offboarding, workforce identity, performance and
talent, learning, employee experience, HR service delivery, workforce
planning, people intelligence, employee relations, contingent workforce,
global mobility, and workplace safety. The list is the competitor's catalog,
recorded here so that the disposition register in §3.12 has something to
dispose of. It is not a roadmap, and no family is planned for beyond the
compensation adjacency that ChangeOps already touches.

### 3.10 Pillar Concepts

#### People

People is the suite foundation. It owns the coherent view of person, worker, employment, job, position, organization, manager, legal entity, location, cost center, status, worker type, effective dates, documents, and history.

Onboarding and offboarding belong here commercially but operate across every pillar:

```text
Hire or termination
      │
      ├── People records and documents
      ├── Access accounts, roles, and devices
      ├── Workforce schedule and availability
      ├── Rewards payroll and benefits
      ├── Talent learning assignments
      └── Experience communications and cases
```

#### Workforce

Workforce covers the operational reality of when and where work occurs: time, attendance, scheduling, availability, shifts, overtime, absence, PTO, leave, labor allocation, exceptions, and forecasting.

It fits the transaction model naturally. A PTO request, for example, connects eligibility, balance, coverage, approval, schedule changes, payroll effects, and reconciliation as one governed transaction.

#### Talent

Talent spans the lifecycle before and after employment:

```text
Position
  -> Requisition
  -> Candidate
  -> Application and interview
  -> Offer
  -> Employment proposal
  -> Worker
  -> Goals, performance, skills, and learning
  -> Internal mobility and succession
```

Candidate history and employee talent history should attach to the same person identity where law, consent, retention, and access policy permit. Skills become a cross-platform primitive connecting job requirements, worker capability, learning, eligibility, mobility, and workforce planning.

#### Rewards

Rewards provides the complete view of what a worker costs and what the worker is entitled to receive:

- Base and variable compensation
- Bonuses, commission, and equity
- Benefits and deductions
- Reimbursements
- Payroll outcomes

Compensation is a natural early expansion because it is already central to ChangeOps. Payroll is the deepest standalone domain in the map and must have an independent entry decision; a complete-suite vision must not force premature payroll construction.

#### Experience

Experience combines employee communications with service delivery:

- Employee home, directory, and organization views
- Targeted announcements and resources
- Surveys, recognition, communities, and employee voice
- HR help, knowledge, policy questions, and case management
- Sensitive employee-relations and compliance cases

An employee question can move from knowledge or AI assistance to a governed case and, when necessary, to an HCM transaction. Sensitive cases use stronger record-level and purpose-based access than ordinary worker data.

#### Access

Access extends the worker model into digital and physical work:

```text
Person
  -> Employment
  -> Position, job, organization, and location
  -> Access policy
  -> Digital identity
  -> Applications, roles, groups, devices, and facilities
```

A promotion, transfer, leave, or termination can therefore drive entitlement review and provisioning from the same authoritative worker event. Access is a major differentiator because it connects who a person is, what work they perform, and what resources they may use without creating a second lifecycle system.

### 3.11 Planning and Intelligence

Workforce planning converts desired future state into governed work:

```text
Approved workforce plan
  -> create or close positions
  -> transfer workers
  -> open requisitions
  -> adjust budgets
  -> generate governed transactions
```

People Intelligence remains horizontal across all pillars. It combines reporting, dashboards, metrics, workforce analytics, anomaly detection, forecasting, governed natural-language queries, AI analysis, and data export on a shared semantic model.

The same authority, permission, privacy, and reproducibility rules apply to analytics and AI. A unified graph does not imply unrestricted data visibility.

### 3.12 Portfolio Discipline

The Workforce Operating System is a destination architecture, not a mandate to build the whole catalog at once.

Every proposed product family must pass four tests before investment:

1. **Shared-model advantage** — does the Person/Worker Graph materially improve the product?
2. **Workflow advantage** — do governed transactions, reconciliation, and audit create differentiation?
3. **Customer pull** — are existing customers asking and willing to pay for it?
4. **Operating readiness** — can Human Capital Management Suite safely own the required domain authority and regulatory burden?

This prevents the broader vision from weakening the ChangeOps wedge or turning the roadmap into a checklist of incumbent-suite modules.

Each proposal also receives one scoped disposition: `OBSERVE`, `ORCHESTRATE`,
`OWN_SELECTED_SCOPE`, `REPLACE_SELECTED_PRODUCT`, `PARTNER_LONG_TERM`, `DEFER`,
or `REJECT`. A feature appearing in UKG, Workday, or another suite is not a reason
to build it. Native payroll, WFM, benefits, or other suite-depth investment must
pass the authority-absorption gate in the [competitive positioning and authority
expansion contract](specs/competitive-positioning-and-authority-expansion.md).

### 3.13 Small-business operations expansion (2026-10-22)

The owner has authorized detailed planning and implementation backlog creation for all twelve gap subsystems described in [the expansion plan](small-business-platform-ideas.md): business relationships; service jobs; scheduling/dispatch; quotes/orders; billing/payment tracking; stock; equipment/assets; purchasing; external portals; accounting connectivity; devices; and guided setup. Additional operational steps discovered during design belong to those owners and receive explicit backlog coverage.

Reuse or enhance existing subsystems before introducing code. In particular, service execution extends workorder/workorderservice/workorderstore, equipment extends domains/asset and assetstore, job pricing extends workorderbilling, and scheduling extends appointment/scheduling/crew primitives. Recruiting CRM, employment service-credit, and the suite's own subscription billing must retain their present meanings. New customer-party or receivable semantics need narrowly bounded ownership, not a second workflow, ledger, integration runtime, identity system, or frontend.

The common user outcome is request -> scope -> assign -> approve if required -> execute -> confirm -> reconcile. Each domain preserves its specific invariants. Roles, module availability, and object scope determine discovery and UI actions; workers, customers, suppliers, guests, and machine identities are related explicitly without inheriting one another's permissions.

Feature registration, storage, APIs, events, schemas, and recovery are additive. No new kernel family or general orchestration layer is authorized. New semantic definitions enter the existing catalog through its normal publication lifecycle; suggested operation names are not pre-existing published definitions. `BI.WORK`, `BI.COMMERCIAL`, `BI.INTEGRATION`, `BI.EXPERIENCE`, and other accepted domain sets can describe implementation scope without asserting direct intent availability.

The first connected reference workflows cover a service team delivering a customer job and a team receiving/issuing supplies. Full capability coverage remains in the backlog; these workflows determine implementation order and acceptance depth, not removal of the other approved features. Runtime enablement remains per tenant and per capability after real endpoint, database, browser, authorization, recovery, and migration evidence.

## 4. Target Market

### 4.1 Small-business customer profile and retained ChangeOps pilot

The primary expansion audience is a small business coordinating employees or contractors, customer work, suppliers, and physical or informational resources. Candidate segments are professional services, trades/field services, recurring service teams, repair/retail, and property services. The product supports an owner operating alone as well as multiple teams and locations; no market size, willingness to pay, preferred segment, or production scale is established by this plan.

Customer discovery must identify the daily workflow, existing tools to retain, import/cutover needs, responsible operator, deployment constraints, and the outcome worth paying for. Simple setup and clear recovery are required even when one person holds several roles. Templates may simplify role assignments but cannot waive mandatory separation or authorization rules.

The retained enterprise ChangeOps pilot targets multi-system HR estates with frequent employee changes and independently owned downstream systems. Its former 2,000–25,000 employee range is a pilot hypothesis, not an eligibility requirement for the small-business workspace. Incumbent non-duplication and authority-by-field proof continue to apply to that pilot; they do not exclude a small business using native people or work capabilities.

### 4.2 Likely Stakeholders

For the small-business track, discovery and acceptance include the owner/operator, office administrator, dispatcher, field worker, bookkeeper, customer and supplier participant. One person may perform several authorized roles. Each needs simple setup, a clear next action, recovery from interruption and scope-appropriate information. The retained enterprise ChangeOps stakeholders below remain relevant to that proof track.

| Stakeholder                  | Primary concern                                      |
| ---------------------------- | ---------------------------------------------------- |
| Head of HRIS                 | Workflow reliability, maintainability, integrations  |
| People operations leadership | Cycle time, employee experience, operational control |
| Payroll leadership           | Cutoffs, correctness, retroactivity, reconciliation  |
| Compensation leadership      | Policy compliance, approvals, sensitive data         |
| Finance                      | Budget authority and cost attribution                |
| IT and integration teams     | Security, identity, credentials, system reliability  |
| Security and privacy         | Access, retention, assurance, regional obligations   |
| Internal audit               | Evidence, accountability, reproducibility            |
| Managers and employees       | Clear actions, status, and timely outcomes           |

The initial champion, economic buyer, implementation owner, and security approver may be different people. Design-partner work must validate these roles rather than assume the Head of HRIS performs all of them.

### 4.3 Customer problems

The small-business expansion addresses fragmented requests, responsibilities, schedules, records, resources, and payment status. Each reference workflow must prove less duplicate entry and a clear next action across these boundaries.

The retained ChangeOps proof addresses:

> High-risk employee changes take too long, cross too many systems, lack consistent controls, and are difficult to explain when something goes wrong.

## 5. Product Principles

### 5.1 Govern Transactions, Not Just Tasks

A workflow task is only one part of an employee change. The product must govern the proposal, approvals, context, execution, downstream effects, reconciliation, and correction as one connected transaction.

### 5.2 Separate Different Kinds of Truth

Human Capital Management Suite must distinguish:

```text
Transaction truth:
    what was proposed, approved, attempted, and decided

Domain truth:
    the authoritative employee state for a field and period

Execution truth:
    what Human Capital Management Suite sent or changed

Observed external truth:
    what a destination system reports after execution
```

Human Capital Management Suite owns transaction truth from the beginning. It owns employee-domain truth only when a customer explicitly promotes a defined domain or field scope to Human Capital Management Suite authority.

### 5.3 Approval Must Bind to an Immutable Proposal

An approval is not approval of a mutable request ID. It is approval of a specific proposal under a known decision context.

Conceptually:

```text
Approval
  = proposal version
  + policy version
  + relevant context
  + approver authority
  + decision timestamp
```

Material changes to the proposal or context must invalidate or reevaluate the approval according to policy.

### 5.4 Concurrent Changes Are a Business Problem

Workflow-instance locking is insufficient when several requests affect the same employee, field, or effective period.

The platform must identify whether concurrent changes can:

- Merge safely
- Execute in a defined order
- Supersede one another
- Require human conflict resolution

No material transaction should execute solely because its individual workflow version is current.

### 5.5 Effective Time and Recorded Time Are Different

The product must preserve both:

- When a business fact is effective
- When the system learned or recorded that fact

This distinction is essential for future-dated changes, retroactive corrections, audits, payroll coordination, and historical explanations.

### 5.6 External Outcomes Must Be Observed

A successful API response does not prove that every intended business outcome occurred. Execution must be followed by observation and reconciliation.

### 5.7 Failure and Repair Are Product Experiences

Failures, partial writes, conflicts, retries, compensation, and manual repair are expected enterprise outcomes. They must be visible, governed, and understandable rather than hidden in operational logs.

### 5.8 AI Assists; Humans and Policy Remain Accountable

AI may review, explain, summarize, identify missing information, and suggest next actions. It must not independently approve or execute material employment transactions in the initial product.

### 5.9 Every Product Action Is a Governed Capability

> If the platform can do it, there is an API for it. If there is an API for it, authorization describes exactly who or what may invoke it, against which records, fields, relationships, purposes, and conditions.

There is no UI-only, agent-only, or hidden administrative business operation. User interfaces, agents, command-line tools, partners, and customer applications all use the same governed semantic capability layer. Ordinary operations do not bypass policy through direct database manipulation.

### 5.10 Organization Scope Is a Security Primitive

Tenant identity, business hierarchy, legal employment scope, human identity, and authenticated principal are related but separate concepts.

> Tenant defines the hard customer boundary. Organization defines business scope. Capability controls what may be done. Resource and field policies control what may be touched. Relationship, purpose, and context determine when it is permitted.

The same authorization model must remain simple for a single-company customer and scale to multinational parents with subsidiaries, isolated payroll domains, shared corporate resources, regional agents, and local policy.

### 5.11 Globalization Is a Business Dimension

Locale is not synonymous with translation. Language, country, currency, timezone, calendar, formatting, legal jurisdiction, and worker preference influence different parts of the product and must resolve independently.

> Store canonical business values; preserve the locale and jurisdiction context that gives them meaning; localize only at boundaries where humans consume them.

The shared HCM model remains global without assuming that every country, language, currency, calendar, naming system, address, or employment regime behaves the same way.

### 5.12 Law Is a Versioned Constraint System

Legal requirements must not be buried in workflow code, UI warnings, or developer comments. They are versioned, effective-dated rules that evaluate a proposed action in its legal context and produce typed obligations, restrictions, prohibitions, evidence requirements, and deadlines.

> AuthZ answers whether this actor may act. Legal policy answers whether the organization may act or process data in these circumstances and what obligations attach. Workflow executes those obligations. The ledger proves what happened.

Human Capital Management Suite supplies governed infrastructure, baseline legal packs, provenance, and change-management tools. Customer counsel controls final interpretations and ambiguous configuration; the platform does not represent itself as a substitute for legal advice.

### 5.13 Preserve Epistemic Integrity

Human Capital Management Suite must know what the workforce looked like, how it changed, why decisions were made, and what humans or systems believed when they acted. It must never confuse those categories.

```text
FACT
what happened or is authoritatively asserted

OBSERVATION
what a person or system reported or believed

DECISION
what an actor chose and why

INTERACTION
what an actor did inside Human Capital Management Suite

INFERENCE
what an algorithm believes may be true
```

> Semantic data may enrich truth, but it never silently becomes canonical truth.

All five categories can be timestamped, attributable, governed, and analyzed together while retaining their distinct provenance, confidence, purpose, classification, and retention.

### 5.14 Agents Interpret; Deterministic Services Execute

Humans increasingly express intent. Agents discover governed data and capabilities, correlate domains, construct analyses, create hypotheses, compose workflows, explain tradeoffs, and propose new behavior. Deterministic platform services remain responsible for authorization, validation, transaction execution, reconciliation, and authoritative state.

> Agents may discover, correlate, infer, recommend, plan, compose, and predict within their authorized information space. They never obtain ungoverned authority over canonical HCM state.

Agents are an overlay, not the product's primary interface. The governed request, approval, and repair surfaces are the product; an agent, where later enabled, is another entry point to the same capabilities. "Intent as the primary interface" is a Stage 5 hypothesis. Deterministic lookup, calculation, policy evaluation, authorization, ledger state, payroll, tax, and transaction execution remain deterministic capabilities; the agent security boundary (tool gateway, taint, typed output, kill switch) binds every plane now, while the agent product plane has no phase.

### 5.15 One Canonical History, Many Rebuildable Read Planes

Human Capital Management Suite will not treat the data layer as one database. It will maintain one authoritative chronology of business facts, plus authoritative content-addressed artifacts where a ledger event references large verbatim content. Operational projections, caches, search indexes, embeddings, analytical tables, and semantic indexes are derived serving planes.

> Normalize and protect truth. Denormalize reads aggressively. Make every derived representation disposable, reproducible, and continuously reconcilable.

Historical mutation is prohibited through ordinary product and administrative interfaces. Corrections append new facts. Integrity controls make unauthorized alteration detectable; they do not support the false promise that corruption is literally impossible.

### 5.16 Bill at Semantic Boundaries; Meter Technical Work Precisely

Entitlements determine what a customer has purchased. Authorization determines which principal may use it. Metering records usage. Rating applies an effective contract and price. Billing produces charges, credits, statements, and invoices. Cost accounting records what Human Capital Management Suite paid to deliver the service.

> Bill customers at stable, understandable product boundaries while preserving fine-grained technical usage and cost lineage underneath.

Internal refactoring must not unexpectedly change a customer bill. One packaged promotion must not cost more merely because Human Capital Management Suite changed it from seven internal calls to eleven. Variable API, AI, analytics, integration, and storage consumption may be priced explicitly, but only through published, versioned meters and contracts.

### 5.17 Resolve Jurisdictions; Compose Domain-Specific Regulatory Rules

Global regulatory behavior cannot be reduced to one country field or one generic legal rules engine. Human Capital Management Suite resolves the applicable supranational, national, state or provincial, local, contractual, collective, plan, and company authorities for a specific action and effective time. Specialized deterministic engines then calculate amounts, generate filings, or produce typed obligations under their own composition semantics.

> The Legal Plane governs whether and why the organization may act. The Regulatory Platform deterministically computes which jurisdictional rules, calculations, filings, deadlines, and evidence apply.

Rules remain versioned, effective-dated, source-attributed, explainable, simulated before publication, and subject to customer-counsel control where interpretation is required.

Phase 1 legal context is one versioned rule pack of customer-configured
thresholds and notice obligations. The jurisdiction graph, deterministic
regulatory engines, country packs, and filing gateways are a separate product
line with their own gate; they are not kernel vocabulary, and the kernel's
three families do not include a filing family.

### 5.18 Go Core, Contract-First, and Open-Source-First

Go is the authored language for the Human Capital Management Suite product core: backend services, deterministic engines, workflow execution, control-plane capabilities, integrations, command-line tools, and operational workers. Protobuf and gRPC define service contracts. GWC/GoWebComponents, `grpcbridge`, and SchemaFlux are the preferred choices for UI, transport edge, and definition generation, each with a named qualification fixture and a named fallback (Go server-rendered HTML, grpc-gateway or connect-go, and protoc with Go code generation).

> Human Capital Management Suite ships one Go platform. Its house libraries earn their place by passing a fixture, not by decree; TypeScript, React, and Node are not release-image or runtime dependencies, and Node-based developer tooling is allowed.

Open-source-first does not mean dependency-first or self-host-everything. A dependency must reduce total complexity after security response, upgrades, operations, testing, licensing, portability, and exit cost are included. Managed services remain acceptable when they materially reduce regulated operational risk and preserve export, replay, and migration paths.

The current TypeScript/React implementation is historical evidence and a comparison baseline. It is not extended with new behavior. It may run beside the Go slice through P1A so that customer evidence does not wait on the rewrite; its exclusion from the release is a P1B gate.

See [the Go Technology Constitution](specs/go-only-technology-constitution.md).

### 5.19 Platform Correctness Is Business Correctness

HCM correctness is not preserved if the infrastructure can mix tenants, starve payroll, amplify an outage through retries, admit an unsigned build, let a compromised workload impersonate another, allow hostile content to steer an agent tool call, destroy every recovery copy, or assign false time to audit evidence.

Therefore production behavior is part of the domain contract:

```text
BUSINESS CORRECTNESS                    PLATFORM CORRECTNESS

valid proposal                         correct tenant cell
authorized execution                   authenticated workload
legal obligation                       controlled data egress
ordered side effect                    priority + backpressure
immutable event                        signed artifact + trusted time
repair plan                            isolated verified recovery
agent recommendation                   taint-aware governed tool use

                 BOTH MUST HOLD
                       |
                       v
               TRUSTWORTHY HCM STATE
```

The architecture must make overload, isolation, recovery, cryptographic trust, and AI containment explicit and testable. These controls should begin as shared Go libraries, policy contracts, and a small number of control-plane modules. They become independent services only when blast radius, scaling, privilege separation, or availability evidence requires it.

## 6. Core Product Model

### 6.1 Business Intent and Transaction Kernel

The platform kernel begins with `BusinessIntent`: a governed request to answer, calculate, or change something. It prevents the eventual Workforce OS from forcing every domain operation through an employee-mutation abstraction.

```text
BusinessIntent
      │
      ├── ChangeRequest          may mutate or cause effects, directly or
      │                          through explicitly bound child intents
      ├── CalculationRequest     deterministic, pure computation
      └── AnalyticalRequest      governed question or analysis
             │
             v
      validated BusinessTransaction or governed read result
```

Three families, distinguished by whether the intent may cause a material
mutation or effect. Process, filing, batch, and case semantics are attributes
of a `ChangeRequest` definition rather than families; a fourth family is added
only when a funded domain proves an attribute cannot express the distinction.

All families share identity, tenant and organization scope, declared purpose, actor/delegation context, effective and recorded time, capability lineage, authorization and legal decisions, correlation, lifecycle dimensions, and evidence. Only intents that mutate or cause material external effects become `BusinessTransaction`s.

`HCMChangeRequest` remains the central ChangeOps business object and the first implemented subtype. It is not the universal root object for time punches, applications, payroll runs, benefit enrollments, cases, learning completions, filings, or analytical queries.

The authoritative lifecycle, subtype, cancellation, supersession, correction,
security, and evidence rules are in the [Business Intent and Change Request Kernel
Contract](specs/business-intent-and-change-request.md).
The [Business Intent Catalog and Runtime Model](specs/business-intent-catalog.md)
governs the fourteen drafted definitions, domain-qualified identifiers,
maturity gates, and the typed instance envelope. The intake name list is
non-normative vocabulary.

### 6.2 HCM Change Request

An HCM Change Request represents a proposed mutation to employee-related data that must be evaluated, approved, executed, reconciled, and audited.

It includes:

- Request identity and tenant
- Change type and business reason
- Requester and target worker
- Effective date or date range
- Current and proposed state
- Immutable proposal revision
- Affected fields and systems
- Baseline state and context
- Validation and policy results
- Required approvals and their validity
- Simulation and confidence results
- Transaction plan
- Execution and reconciliation status
- Conflicts, supersession, and correction lineage
- Failure and repair status
- Audit history

### 6.3 Change Request Lifecycle

Simulation precedes approval so approvers review the exact material proposal.
Execution-time revalidation may confirm that proposal; any material change creates
a new revision and invalidates affected approvals.

```text
Draft -> Preflight -> Simulate -> Submit exact CanonicalDigest -> Approve
      -> Schedule -> Revalidate -> Execute -> Observe/Reconcile/Repair -> Close
```

Lifecycle is five dimensions rather than one misleading linear status, and
never more than five:

```text
RequestState       where is the request itself?
ExecutionState     what has the runtime done?
BusinessState      did the business outcome happen?
ConsistencyState   does observed external state agree with intent?
ObligationState    are attached obligations discharged?
```

Needs-data, reject, cancel, approval invalidation, conflict, blocked execution,
repair, supersession, correction, and reopen are typed transitions in the
appropriate dimension. Proposal revisions, approval bindings, closure records,
incidents, and outcome tracking are linked records, not further dimensions.
Historical dimensions are preserved rather than collapsed into a final
`Resolved` flag.

### 6.3a Lifecycle Budget

The design multiplies state machines easily and each one costs evidence,
authority, projection, and UI work. The platform carries these lifecycles and
no others without a scope exchange:

```text
IntentInstance (five dimensions)   WorkItem              ConnectorOperation
ProposalRevision / ApprovalBinding TransactionPlan       ConnectorConnection
WorkflowInstance / NodeExecution   RepairPlan            AuthorityPolicy
Domain aggregates (Employment, Assignment, Position, Compensation, Organization)
```

Everything else that has a `state` field (capability manifests, forms, rules,
templates, classification labels, provenance edges, message intents, budget
reservations) uses the shared three-step `DRAFT -> PUBLISHED -> RETIRED` or
`REQUESTED -> HELD -> RELEASED` shape until a consumer needs more.

### 6.4 Initial HCM Primitives

The first product should model only the objects required for the initial workflow family:

- Person
- Worker or employee
- Employment
- Job
- Position
- Organization unit
- Location
- Manager relationship
- Compensation record
- Change request
- Approval task
- Transaction plan
- Repair plan
- Workflow migration plan
- Ledger event
- Connector operation and external observation
- Principal and authentication context
- Organization authorization scope
- Versioned authorization policy and decision
- Money, calendar, and business-time values (one currency, one locale in Phase 1)
- One versioned rule pack of thresholds and notice obligations

Locale resolution beyond one locale, legal and processing context, retention
and data-subject machinery, semantic schemas, analytical envelopes, and every
agent object are later-phase primitives and are not modeled until a release
needs them.

The model should use stable HCM primitives plus governed metadata extensions. Fully schema-less HCM data would undermine interoperability, policy consistency, and auditability.

## 7. Initial Product Scope

### 7.1 First Workflow Family

The first product release family is related employee changes:

1. Job or title change
2. Compensation change
3. Manager or organization change
4. Combined promotion change

Of these, P1B executes only the combined promotion (job/level) and its base-pay
change. Manager and organization change are conformance fixtures until a
second release; the family is named here so that the kernel is tested against
all four, not so that all four ship.

This family is narrow enough to ship while proving the platform's most important claims:

- Effective dating
- Relationship-aware permissions
- Sensitive compensation access
- Multi-party approval
- Payroll and finance impact
- Concurrent change detection
- Cross-system execution
- Reconciliation and audit

### 7.2 First User Experiences

The initial product should offer four primary experiences:

1. A manager or HR request experience
2. A role-specific approval experience
3. A transaction simulation and conflict view
4. An audit, reconciliation, and repair timeline

A simple schema-driven administration experience is sufficient initially. A full visual workflow studio is not required to validate the category.

### 7.3 First Integration Scope

Integration expands in deliberate layers, and the first layer is the one the
design partner's estate dictates:

1. One read/observe connector to the partner's HCM, selected with the partner
2. One read/observe path to the partner's independently owned downstream
   system (payroll or identity), so the cross-system claim is testable
3. The same connector's single governed write (P1B)
4. Transactional email for approvals, only if the partner's process needs it
5. Further connectors, payroll reconciliation, and identity synchronization
   after the first workflow family is stable

CSV import is an operator tool for seeding pilot data, not an integration layer.
The product delivers value in observe, validate, approve, simulate, and
reconcile modes before requiring direct write authority.

### 7.4 Explicitly Deferred Scope

The following remain outside the initial plan:

- Full HCM replacement
- Payroll calculation
- Benefits enrollment
- Time, attendance, and scheduling
- Leave and accommodation workflows
- ATS conversion
- Talent, performance, and learning suites
- General-purpose analytics
- Arbitrary customer-authored workflows
- Open extension marketplace
- Autonomous AI execution

### 7.5 Reference Workflow Integration Suite

The five lifecycle workflows and payroll-correction stress test remain architecture conformance tests, not a Phase 1 feature roadmap. Phase 1 implements only Promotion + Compensation Change; the other scenarios test whether its contracts create future dead ends.

Detailed actors, flows, gaps, and the shared simulation contract are maintained in [the reference workflow suite](reference-workflows/reference-suite.md).
The suite also contains a [single-domain Manager Change fixture](reference-workflows/manager-change.md)
and a [cross-domain Promote Into Management fixture](reference-workflows/promote-into-management.md)
to test the boundary between one authoritative domain mutation and one parent
intent coordinating independently owned effects.

### 7.6 Adjacent Product: HRIS DataOps

The operational capabilities required to run ChangeOps create a valuable adjacent surface for the initial HRIS administrator persona:

```text
internal platform operations
  diff | inspect | map | validate | simulate | redrive | reconcile | promote
                              |
                   governed capability review
                              |
                              v
                       HRIS DataOps
  Import | Compare | Diagnose | Repair | Configure | Export
```

The first candidates are bounded data staging/import, cross-system diff, effective-date/provenance debugging, AuthZ simulation, connector testing/redrive, and configuration diff/promotion. Phase 1 implements only the slices required to operate the Promotion workflow and first connector. A slice becomes a supported customer product when design partners use it independently, its safety and support boundaries are proven, and productization requires materially less effort than creating a separate product line.

See [the HRIS Admin Toolkit and DataOps specification](specs/hris-admin-dataops.md).

### 7.7 Horizontal Subsystem: Integration Platform

External APIs, files, webhooks, and named vendor connectors must enter Human Capital Management Suite through one governed integration runtime rather than bespoke domain code:

```text
External System
      |
      v
Connector Definition + Tenant Connection
      |
      +-- read ---------> External Observation
      +-- write --------> External Effect
      +-- subscribe ----> Webhook Receipt
      +-- sync ---------> Canonical Delta
      +-- reconcile ----> Match / Drift / Repair
      |
      v
Mapping + Crosswalk + Capacity-Aware Scheduler
      |
      v
Canonical Human Capital Management Suite Capability / Workflow / Ledger
```

The framework owns connection testing, external-permission diagnosis, schema discovery and impact, mappings and reference-data crosswalks, synchronization, webhooks, rate-aware queues, operation journals, health, redrive, and reconciliation. Domain code invokes semantic capabilities such as `worker.read` or `identity.deprovision`; it does not import vendor-specific clients.

Connector maturity is explicit: transport adapter, typed connector, semantic connector, governed connector, then certified connector. A vendor name in a backlog or configured adapter is not a claim of certified support. Phase 1 implements the shared contract and one design-partner connector only; a broad connector catalog and marketplace remain deferred.

See [the Integration Platform specification](specs/integration-platform.md).

### 7.8 Horizontal Subsystem: Messaging and Notification Plane

Business domains and workflows express semantic communication intent rather than channel/provider calls:

```text
MessageIntent
  purpose + audience + content/template + requirement
                         |
       +-----------------+-----------------+
       v                 v                 v
 audience/identity   render/locale    delivery policy
       +-----------------+-----------------+
                         v
                 message orchestrator
                         |
        email | SMS | push | inbox | Slack/Teams
                         |
                         v
        delivered / read / acknowledged / replied
                         |
                  workflow signals
```

The plane owns audience and endpoint resolution, templates/localization, secure inbox, preferences, channel/provider policy, asynchronous delivery, replies/conversations, bulk controls, and communication reconciliation. The workflow engine owns business waits and escalation. Provider adapters and system event delivery reuse the Integration Platform.

Human messaging and system subscriptions share delivery infrastructure but retain different identity, preference, schema, evidence, and retry semantics. Provider acceptance, delivery, read, acknowledgement, response, signature, and legal evidence are never collapsed into `sent = true`.

P1A sends nothing. P1B implements one approval/task email with one template and delivery signals, only if the partner's process needs it; the secure inbox is a minimal contract. Inbound conversations, SMS/push/chat routing, bulk communication, legal-notice evidence, and customer-configurable subscriptions are deferred.

See [the Messaging and Notification Plane specification](specs/messaging-and-notification-plane.md).

### 7.9 Capability Coverage and Shared Business Services

The architecture does not claim an exhaustive gap list. It maintains a reviewed coverage matrix with independent coverage and delivery states:

```text
responsibility discovered
          |
          v
DEFINED | PARTIAL | IMPLIED | MISSING | DEFERRED
          |
          +-- owner and evidence link
          +-- implementation depth
          +-- dependency and next closure action
          +-- reference/conformance trigger
```

This prevents a diagram mention from being mistaken for a subsystem and prevents deliberate deferral from being mistaken for neglect. Every responsibility must explicitly name authority, state, capabilities, dependencies, failure behavior, evidence, security, and phase depth; undocumented defaults are prohibited. See [the Platform Capability Coverage Matrix](specs/platform-capability-coverage-matrix.md), [the explicit responsibility boundaries](specs/platform-responsibility-boundaries.md), and [the foundation gap-closure contracts](specs/platform-foundation-gap-closure.md).

Three cross-domain responsibilities are promoted into reusable shared business services now:

```text
                    Workflow Kernel
                          |
       +------------------+------------------+
       v                  v                  v
  Human Work            Forms           Business Rules
 task/queue/owner    questions/answers   expressions/tables
 claim/SLA/escalate validation/version   pure typed decisions
       +------------------+------------------+
                          v
              typed artifacts and signals
```

Human Work owns responsibility, queues, claims, delegation, deadlines, escalation, and completion. Forms own versioned conditional data collection, validation, attachments, accessibility, and submission evidence. Business Rules own deterministic expressions and decision tables separate from workflow topology, Legal rules, AuthZ policy, regulated computations, and agent inference.

The remaining promoted foundations are Master/Reference Data, Schema/Data Contracts, Crosswalks, Data Import/Migration, Managed File Transfer, Tenant Lifecycle, Support Access, DLP/Egress, and Conformance Infrastructure. The completeness review also promotes control-bundle distribution/bootstrap, identity/session/recovery lifecycle, hostile-content quarantine, global reference-dataset lifecycle, service discovery/dependency inventory, idempotency retention, certificate/trust-bundle lifecycle, verified deletion/sanitization, customer incident communication, and accessibility assurance. Promotion means explicit ownership and contracts—not full Phase 1 implementation.

See [the Human Work, Forms, and Business Rules specification](specs/human-work-forms-and-rules.md).

### 7.10 Explicit Runtime Foundation Contracts

Runtime behavior may not rely on ambient configuration, operating-system locale/time data, network location, an in-memory idempotency cache, or an unspecified file-safety assumption.

```text
signed control snapshot + verified bootstrap
                    |
identity/session assurance + workload trust
                    |
registered endpoint + dependency/failure contract
                    |
typed request + durable idempotency record
                    |
safe content/reference-data versions
                    |
domain transaction + evidence
                    |
verified deletion / incident / accessibility lifecycle
```

The focused contract is [Platform Foundation Gap Closure](specs/platform-foundation-gap-closure.md). The explicit boundary register for deferred and shared systems is [Platform Responsibility Boundaries](specs/platform-responsibility-boundaries.md). Position capacity and occupancy are explicitly separated from reference data in the [Position and Headcount Domain Contract](specs/position-and-headcount-domain.md). The first business mutation is owned by the [People, Employment, and Assignment Domain](specs/people-employment-assignment-domain.md), [Organization and Relationship Domain](specs/organization-and-relationship-domain.md), and [Compensation Domain](specs/compensation-domain.md), not Workflow. [Source Authority](specs/source-authority-and-external-mastering.md), [Identity Resolution](specs/identity-resolution-and-entity-linkage.md), and [Workforce Budget Authority](specs/workforce-budget-authority.md) are separate shared authorities. Exact proposal, idempotency, ledger, configuration, and evidence hashes use the [Canonical Envelope and Digest Contract](specs/canonical-envelope-and-digest.md).

### 7.11 Legacy Extraction and Go-Only Cutover

The existing repository is not a blank slate. Its older documents describe a
Node/TypeScript control plane, deterministic Go blocks, PostgreSQL ledger and
projections, a React/Vite schema-driven console, and several useful vertical
fixtures. These assets are regression evidence and migration inputs; they do
not override the Go-only target and will not remain production dependencies.

```text
DOCUMENTED CURRENT BEHAVIOR

React/Vite -> Node/TypeScript -> Go block executor -> PostgreSQL
     |             |                    |               |
 UI schemas    workflow API       pure planning      ledger/outbox
     +-------------+--------------------+---------------+
                           |
              extract parity fixtures
                           |
                           v
TARGET VERTICAL SLICE

GoWebComponents -> grpcbridge -> Go capabilities/runtime -> PostgreSQL
                          |
               Protobuf + SchemaFlux proof
```

The legal-name workflow remains the smallest regression slice. The HarborCare
headcount fixture contributes sequential, quorum, and veto approval cases. The
HarborCare organization-transfer fixture contributes source/target manager,
team, location, and cost-center scope. The documented termination path and
compensation-provider fakes contribute high-risk effect and connector-recovery
cases. Phase 1 does not productize those older flows; it uses their durable
behavior to prevent regressions in shared contracts.

The presentation contract preserves schema-driven pages, semantic brand packs,
registered widgets, provenance-bearing bindings, available-action rendering,
accessibility, and generated-page safety in a clean GWC implementation. The
cutover seeks semantic and safety parity, not source-code or pixel parity. No
React renderer or Node service is carried into the target platform.

See [the implementation baseline](specs/legacy-implementation-baseline.md), [the
experience and branding contract](specs/experience-ui-and-branding.md), and
[the organization-scope contract](specs/organization-scope-and-authz.md).

## 8. Platform Plane Model and Capabilities

Human Capital Management Suite uses nine logical planes with two cross-cutting overlays. These are
responsibility boundaries, not a mandate for nine separately deployed services.

```text
Experience/API
      |
      v
Identity/Trust
      |
      v
Governance
      |
      v
Control resolution
      |
      v
Workflow orchestration
      |
      v
Domain + Regulatory capabilities
      |
      v
Data: truth + serving + distribution
      |
      +----------> Connectivity: integrations + messaging
      |                         |
      +<----- observations -----+
      |
      v
Intelligence: reports + analytics + semantics + outcomes

OPERATIONS / ASSURANCE ---------------- wraps every plane
BILLING / METERING --------------------- observes semantic boundaries
```

The essential ownership boundary is:

```text
Workflow coordinates
        |
        v
Domain capability validates and executes HCM meaning
        |
        v
Domain-owned transaction appends ledger + projection + outbox
```

The workflow engine never writes employee, compensation, payroll, benefit,
identity, or other domain tables directly. It invokes typed domain capabilities.
Integration and Messaging deliver external system and human side effects.
Search, analytics, semantic indexing, process mining, and dashboards consume
committed facts without becoming mandatory synchronous dependencies for
unrelated business transactions.

The nine planes are Experience/API, Identity/Trust, Governance, Control,
Workflow, Domain plus Regulatory, Connectivity, Data, and Intelligence. Agents
attach to Control discovery, Intelligence access, Workflow composition, and
Domain capabilities through Governance. Operations/Assurance and
Billing/Metering observe and protect the whole system.

See [the Canonical Platform Plane Model](specs/platform-plane-model.md).

The Phase 1 transaction spine is deliberately small:

```text
BusinessIntent / HCMChangeRequest
  -> immutable proposal + approval binding
  -> simulation + multi-stream transaction plan
  -> ACID ledger/projection/outbox commit
  -> external observation
  -> reconciliation
  -> business correction or derived-state RepairPlan
```

Ledger events classify their assertion authority as transaction fact, domain fact, external observation, claim, or correction. Historical chronology remains immutable while current domain state may be corrected. Local authoritative stream appends commit atomically; external side effects remain saga activities with explicit completion and reconciliation.

The full ledger envelope, stream ordering, telemetry separation, invariants, incidents, repair semantics, projection evolution, and operations model are maintained in [the transaction, ledger, reconciliation, and repair specification](specs/transaction-ledger-reconciliation-and-repair.md).

## 9. Governing Platform Contracts

### 9.1 Authority Contract

Authority is configured per tenant, domain, field, system, and effective period. The authority model defines:

- The authoritative system
- Permitted initiators and writers
- Precedence and merge policy
- Required freshness
- Reconciliation tolerance
- Conflict ownership
- Whether automatic repair is allowed

Authority is promoted deliberately. Orchestrating a change does not automatically make Human Capital Management Suite authoritative for the underlying employee domain.

### 9.2 Conflict Contract

Each proposal declares the employee fields and effective periods it may change. Overlap is evaluated against pending, approved, future-dated, and executed requests.

Conflict outcomes are explicit:

- Compatible merge
- Ordered dependency
- Supersession
- Hard conflict requiring a decision

High-risk approvals may create visible, scoped, expiring reservations. Execution always repeats conflict detection against current authoritative state.

### 9.3 Reproducibility Contract

Every material decision references the versions of the environment that produced it, including:

- Workflow and block definitions
- Proposal and schemas
- Policy and permissions
- Metadata and reference data
- Integration mappings
- AI prompt, review policy, provider, and model configuration
- Relevant decision inputs

Historical explanation reconstructs why a decision was made then. Current reevaluation asks what the system would decide now. These are separate operations and must be clearly labeled.

### 9.4 Identity and Permission Contract

The identity plane must support enterprise federation, user lifecycle, human and machine identities, delegation, service-on-behalf-of context, session assurance, and step-up authentication.

Two identity domains must remain terminologically distinct:

```text
PLATFORM IAM
  authenticates and authorizes Human Capital Management Suite users, services, agents,
  operators, workloads, sessions, and production administration

WORKFORCE ACCESS PRODUCT
  governs customer-worker accounts, applications, devices,
  entitlements, provisioning, deprovisioning, and access reviews
```

A workforce access event may trigger platform workflows, but a customer's application account is never confused with a platform workload or operator identity.

Authorization combines:

```text
role
+ relationship
+ business attributes
+ field sensitivity
+ action
+ workflow state
+ tenant policy
+ AI policy
+ authentication assurance
```

Every material action remains attributable to a canonical actor. Shared service accounts must not erase human accountability.

### 9.5 Privacy Contract

Ledger immutability does not mean retaining every sensitive payload forever. The platform should preserve minimal audit facts and use encrypted references for sensitive content where possible.

The privacy model must cover:

- Field classification and minimization
- Regional storage and tenant isolation
- Retention by data class
- Legal holds
- Redacted audit access
- Prompt and model-input retention
- Connector payload retention
- Cryptographic erasure of deletable content
- Tamper evidence without permanent plaintext retention

### 9.6 AI Governance Contract

AI safety has two independent dimensions:

1. Was the model permitted to see the information?
2. Was the model's output accurate and appropriate?

Production AI capabilities require versioned evaluation suites, structured-output validation, grounding, regression testing, quality thresholds, controlled model changes, failover testing, monitoring, tenant disablement, and a non-AI fallback.

AI output is retained as a recommendation observed at a point in time. It is not regenerated during historical replay.

### 9.7 Administrative Governance Contract

Customers need explicit ownership for:

- HCM policy
- Payroll and finance rules
- Security and permission policy
- Workflow configuration
- Integration credentials and mappings
- AI review policy
- Production publication and emergency repair

Configuration changes require named owners, permitted editors, reviewers, effective dates, segregation of duties, audit, and rollback. Publishing a high-risk workflow or policy is itself a governed change.

### 9.8 Detailed Platform Architecture Catalog

The long-term platform planes are specified separately so their completeness is not confused with Phase 1 implementation scope:

- API/capability, organization AuthZ, corporate scope, globalization, legal/compliance, analytics, agents, data, billing, regulatory computation, and production infrastructure
- Identity resolution, source authority, schemas, classification, secrets, configuration, connectors, data quality, provenance, and remaining gap portfolios
- The ASCII Architecture Atlas and production-correctness flows

See [the platform architecture catalog](specs/platform-architecture-catalog.md). The [Phase 1 execution plan](execution-plan.md) determines which slices are implemented, contracted minimally, deferred for conformance, or out of phase.

## 10. Workflow Execution Kernel

The workflow engine is a small domain-neutral execution kernel. Payroll, GDPR, IAM, compensation, billing, and other domain semantics stay in typed governed capabilities outside it.

```text
facts + obligations + policy + decisions + time/events
                         |
                         v
              DECLARATIVE WORKFLOW GRAPH
                         |
                         v
                     COMPILER
  types | capabilities | effects | idempotency | safe points
                         |
                         v
               COMPILED IMMUTABLE PLAN
                         |
                         v
          durable instances / nodes / tasks
          timers / signals / leases / checkpoints
                         |
                         v
                  CAPABILITY GATEWAY
                         |
                         v
                  deterministic effects
```

Its primitive vocabulary is ten core primitives (`CAPABILITY`, `DECISION`, `TRANSFORM`, `OBSERVE`, `END`, `APPROVAL`, `TASK`, `WAIT`, `SIGNAL`, `COMPENSATE`) and three structural ones gated behind P1B evidence (`PARALLEL`, `JOIN`, `SUBWORKFLOW`). Safe points are a node attribute the compiler places; rules, agents, and documents are capabilities. Published definitions compile to immutable typed plans before execution.

The runtime provides durable instance and node state, proposal-bound approval resolution, timers, signals, leases and fencing, bounded retries, safe pause/cancel/quarantine semantics, the five-dimension completion model, simulation and execution modes, and first-class inspection. Runtime state, business ledger evidence, operational projections, and telemetry remain logically separate.

P1A uses five primitives in simulate mode; P1B uses nine with a PostgreSQL-backed scheduler and Go workers. Before P1B, an embedded Go durable-execution library is evaluated against four non-negotiables and adopted if it passes; the runtime specification records the test. Arbitrary loops, customer-authored compensation, subworkflows, replay, shadow mode, live migration, and general-purpose orchestration are deferred.

See [the workflow runtime specification](specs/workflow-runtime.md).

## 11. Enterprise Operating Model

### 11.1 Environment and Change Governance

The platform needs separate development, sandbox, staging, and production environments with governed promotion between them.

High-risk artifacts must support:

- Versioned drafts
- Validation and simulation
- Human review and approval
- Diff and impact views
- Staged rollout
- Rollback
- Complete publication history

### 11.2 Operational Readiness

The operating model must include:

- Structured logging, metrics, and traces
- Transaction and correlation identifiers
- Workflow, block, queue, and integration health
- Stuck-workflow and repair backlogs
- Retry-storm and circuit-breaker visibility
- AI quality, latency, cost, and override monitoring
- Backup, restore, and disaster-recovery testing
- Tenant-level recovery procedures

Operational readiness also requires measurable event-distribution lag, projection lag, reconciliation freshness, invariant coverage, incident aggregation, repair throughput, and successful independent rebuilds. These are product health measures because they establish whether Human Capital Management Suite's derived answers still agree with authoritative business truth.

### 11.3 Quantitative Service and Capacity Contract

Every production capability declares a measurable envelope:

```text
CapabilitySLO

availability_target
latency_target          p50 / p95 / p99
freshness_target        maximum accepted projection/observation age
reconciliation_target
rpo_class
rto_class
criticality
maximum_blast_radius
maximum_request_cost
tenant_concurrency
fanout_limit
payload_limit
queue_age_limit
degradation_policy
measurement_source
exclusions
```

Initial internal planning objectives—not contractual customer promises—are:

| Work class                                      | Initial planning objective                                                      |
| ----------------------------------------------- | ------------------------------------------------------------------------------- |
| Interactive projection read                     | p95 under 500 ms inside the Human Capital Management Suite boundary             |
| Change preflight without slow external provider | p95 under 2 seconds                                                             |
| Local authoritative command commit              | p99 under 1 second                                                              |
| Critical projection freshness                   | p95 lag under 5 seconds; never used silently beyond its declared age            |
| Search freshness                                | p95 lag under 60 seconds                                                        |
| Analytical freshness                            | under 15 minutes unless a report declares batch semantics                       |
| P0 IAM termination reconciliation               | target under 60 seconds when the provider is available                          |
| Workflow wake-up                                | p95 within 5 seconds of a due timer for P0/P1                                   |
| Agent execution                                 | bounded by agent-specific time, tool-call, token/credit, and side-effect limits |

Every design partner establishes workload and capacity assumptions: workers, concurrent users, changes/day, workflow fan-out, integration latency, document volume, agent runs, peak multiplier, and permitted degradation. Load and noisy-neighbor tests must prove those bounds before authority expands.

### 11.4 Recovery Matrix

Recovery objectives attach to authority class rather than technology name:

```text
RPO-A  no acknowledged committed business transaction may be lost
RPO-B  bounded data loss measured in minutes and explicitly accepted
RPO-C  rebuildable from an RPO-A/B source; source watermark defines recovery

RTO-1  critical operating path
RTO-2  same business day
RTO-3  background experience
```

| Plane                                      | Authority status                       | RPO class | Recovery action                                       |
| ------------------------------------------ | -------------------------------------- | --------- | ----------------------------------------------------- |
| Business ledger                            | Authoritative chronology               | RPO-A     | Restore, verify chains/epochs, then reopen writes     |
| Referenced artifacts                       | Authoritative verbatim content         | RPO-A/B   | Restore objects and verify hashes/manifests           |
| Workflow/config/policy/schema definitions  | Authoritative versioned configuration  | RPO-A     | Restore signed versions and active pointers           |
| Key material or externally held key access | Authoritative security dependency      | RPO-A     | Recover through separately controlled custody process |
| Critical transactional projections         | Rebuildable but on critical read paths | RPO-C     | Restore for speed or replay and reconcile             |
| Search/vector indexes                      | Rebuildable                            | RPO-C     | Reindex from retained sources                         |
| Analytics                                  | Rebuildable                            | RPO-C     | Replay from event/object sources                      |
| Cache                                      | Disposable                             | None      | Repopulate                                            |
| Telemetry                                  | Operational evidence with scoped value | RPO-B/C   | Restore only where incident/control policy requires   |

Each deployment profile assigns actual RTO/RPO durations and dependencies. Restore validation must prove the complete application path, not merely data availability, and must include tenant placement, platform IAM, policy/configuration, key access, ledger verification, required projections, and at least one reference workflow.

### 11.5 Trust and Assurance

Production write authority should be conditional on demonstrated readiness across:

- Security and identity
- Privacy and retention
- Transaction correctness
- Reconciliation accuracy
- Failure recovery
- Administrative governance
- AI governance
- Business continuity

## 12. Go-to-Market Plan

### 12.1 Small-business adoption and retained ChangeOps offer

The small-business offer must prove a complete daily workflow, imports from existing tools, understandable role/module setup, and safe exit. Customer interviews and pilot evidence will determine the initial segment, pricing unit and support model. No price, market validation, partner or staffing is established by this expansion decision. Pricing and support discovery for this track are independent from the enterprise pilot hypotheses below.

The retained enterprise ChangeOps commercial hypothesis is a paid, narrowly scoped pilot:

- One workflow family
- One primary HCM source
- A limited set of downstream systems
- One measurable operational or risk outcome
- Observe or export mode before direct writeback where appropriate
- A defined expansion decision at the end

Initial commercial hypotheses, priced to the authority each release carries:

- One to three paid design partners for P1A, five by the end of P1B
- P1A (read-only observation, simulation, diff): a $15,000-$40,000 pilot over
  60-90 days, procurable without a full security review because nothing is
  written to the partner's systems
- P1B (one governed write): a $50,000-$150,000 authority amendment, which is
  where the security review and the 90-120 day implementation belong
- A $150,000-$300,000 initial annual contract after P1B

These figures are hypotheses to validate, not planning facts. The P1A price
exists so that the first partner conversation is not blocked on the procurement
process a write-path dependency requires.

### 12.2 Retained ChangeOps adoption objection

> Why should we introduce another mission-critical system into the write path of an already complex HR stack?

The product must answer with proof:

- Begin without demanding domain ownership
- Show value in visibility, validation, approval, simulation, and reconciliation
- Quantify reduced errors, manual work, and audit effort
- Earn write authority incrementally
- Provide credible bypass, recovery, rollback, and exit procedures
- Demonstrate that removed complexity exceeds the new dependency introduced

### 12.3 Pilot Success Measures

Each pilot should establish a baseline and target for:

- Change completion time
- Manual handoffs and duplicate entry
- Payroll-impacting errors
- Approval traceability
- Audit preparation time
- Workflow change lead time
- Reconciliation exceptions
- Mean time to repair
- User adoption and completion rate
- Implementation effort
- Customer confidence in expanding write authority
- Percentage of downstream effects observed and reconciled within the declared SLA
- Duration spent in degraded external consistency and unresolved repair
- Prevented stale approvals, write conflicts, duplicate effects, and unauthorized execution
- Complexity removed compared with the new dependency and operating cost introduced

### 12.4 Learning Agenda

Design partners must validate:

- The champion, buyer, and implementation-owner model
- Which workflow pain has an approved budget
- Security conditions for write-path adoption
- Acceptable implementation duration
- Required integration depth
- The value of simulation and reconciliation before writeback
- Willingness to grant authority by field or domain
- Pricing and procurement feasibility
- Expansion triggers and reasons not to expand
- Whether customers value cross-system transaction control beyond incumbent workflow/API features
- Whether HRIS DataOps or workforce-access lifecycle creates independently paid pull
- Which domains customers explicitly prefer Human Capital Management Suite to integrate with rather than own

### 12.5 Commercial Architecture Hypotheses

The initial paid pilot should remain commercially simple. It should not launch with granular consumption charges that obscure the primary value proposition.

The long-term hypothesis is a hybrid model:

```text
base platform subscription
  + licensed HCM modules or populations
  + included variable-use allowances
  + transparent overage for selected AI, API, analytics, storage, and premium integration usage
```

The company must validate:

- Which worker population definitions customers already understand and can audit
- Whether PEPM, employees paid, seats, committed spend, or another unit best fits each product family
- Which usage categories customers expect to be included
- Whether AI credits and API compute units are understandable and predictable
- Acceptable budget, warning, hard-limit, and overage behavior
- Demand for parent billing, subsidiary attribution, chargeback, and showback
- Procurement tolerance for variable usage alongside annual commitments
- Whether invoice explanations are sufficient for finance and customer-success teams
- Gross margin by product, tenant, agent, workflow, integration, and provider

Commercial packaging must not dictate internal architecture, but the metering model must preserve enough evidence to test and evolve these hypotheses without rerating history.

## 13. Phased Roadmap

Roadmap progression is based on evidence, not feature accumulation. Authority may advance independently for each customer and HCM domain.

### Cross-Phase Foundation Track

Every subsystem receives one implementation-depth label per phase:

| Label                         | Meaning                                                                                     |
| ----------------------------- | ------------------------------------------------------------------------------------------- |
| **IMPLEMENT**                 | Production behavior exists, is operated, and passes the phase gate                          |
| **MINIMAL CONTRACT**          | Stable types, IDs, boundaries, metadata, and extension path exist; implementation is narrow |
| **DESIGN / CONFORMANCE ONLY** | Scenarios and compatibility constraints prevent dead ends; no production subsystem is built |
| **OUT OF PHASE**              | No design or implementation work unless a discovered dependency forces reconsideration      |

Architecture prose never overrides this classification. “The platform supports” means the end-state architecture; “Phase 1 implements” means staffed delivery scope.

The dependency spine is:

```text
PRODUCT WEDGE + REFERENCE SCENARIOS
                |
                v
BUSINESS INTENT / CAPABILITY / SCHEMA CONTRACTS
                |
       +--------+---------+
       v                  v
IDENTITY + AUTHZ      AUTHORITY + CLASSIFICATION
       +--------+---------+
                v
PROPOSAL + WRITE SET + MULTI-STREAM COMMIT
                |
        CONFLICT + REVALIDATION
                |
                v
WORKFLOW + OUTBOX + ONE CONNECTOR
                |
                v
OBSERVATION + RECONCILIATION + REPAIR
                |
                v
PROVEN PILOT VALUE AND OPERABILITY
                |
      +---------+----------+
      v                    v
more workflows       broader platform planes
```

Work begins as vertical slices through this spine. A shared subsystem is promoted only after the first workflow needs it and a second domain proves reuse. Regulatory engines, polyglot analytics, full billing, generalized human work, and multi-cell deployment cannot become prerequisites for discovering whether ChangeOps solves the initial customer problem.

### Phase 1: ChangeOps Overlay

Goal:

> Prove that Human Capital Management Suite improves one high-risk employee-change workflow without becoming the employee system of record.

Primary scope:

- Promotion + Compensation Change is the only fully executable reference workflow.
- The supported mutation family is limited to the job, manager, organization, and compensation fields required by design partners.
- One primary HCM connector is observed first and gains narrowly scoped writeback only after the authority gate.
- The Phase 1 product core is Go: capability, workflow, data, connector and
  operations code. Agent code is not included.
- One workspace proves the UI path on GWC or the Go SSR fallback; the transport
  edge and definition generator are whichever of grpcbridge/SchemaFlux or their
  named fallbacks passed the M2 qualification fixtures.
- The P1B release image contains no Node, TypeScript, React, or Vite runtime.
  Development tooling is out of scope of that check, and legacy may run beside
  the Go slice in P1A.

#### Phase 1 Implementation-Depth Matrix

`IMPLEMENT` is gate-specific. A Gate B implementation is not a prerequisite for
Gate A product evidence.

| Subsystem                                | Gate A — paid observation                                                                      | Gate B — limited write                                                                                         | Gate C / later                                           |
| ---------------------------------------- | ---------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| BusinessIntent / ChangeRequest           | **IMPLEMENT:** proposal/simulation identity and immutable revisions                            | Extend lifecycle and effect idempotency                                                                        | General BusinessTransaction subtypes                     |
| Capability/schema slice                  | **IMPLEMENT:** read/query/simulate Protobuf, compatibility and Go clients                      | Add write/workflow/event contracts                                                                             | Broad lifecycle/deprecation program                      |
| People/Organization/Compensation domains | **IMPLEMENT:** authorized as-of reads and deterministic promotion simulation                   | Bounded assignment, manager/org relationship and configured compensation mutation                              | Broader employment, reorganization and rewards authority |
| Ledger/projections                       | **MINIMAL:** observation/proposal/decision evidence and read models                            | **IMPLEMENT:** ACID multi-stream transaction/outbox and corrections                                            | Integrity epochs and wider domains                       |
| Approval/conflict/reservation            | Demonstrate exact proposal binding and detect incumbent conflicts                              | **IMPLEMENT:** current authority, normalized write set, position/budget reservation                            | General scarce-resource engine                           |
| Revalidation                             | Simulation reports what would be revalidated                                                   | **IMPLEMENT:** AuthZ, authority, source state, position/budget, policy and conflicts immediately before effect | Cross-domain policy evolution                            |
| Workflow kernel                          | **MINIMAL:** compiled simulation/proposal graph; no employee effect                            | **IMPLEMENT:** durable Promotion nodes, one approval task, timer/lease, inspect/replay/recovery                | General human-work and migration depth                   |
| Integration/reconciliation               | **IMPLEMENT:** one read/observe connector, mapping, provenance, diff                           | Add one governed write, journal, idempotency, redrive and RepairPlan                                           | Connector catalog/certification                          |
| Identity/AuthZ/data safety               | **IMPLEMENT** for read-only tenant/org/field/purpose scope                                     | Add step-up, write risk, session revocation and workload authority                                             | Broad support/recovery/emergency programs                |
| Data quality/invariants                  | Pilot read/simulation validation and plausibility                                              | Add transaction and reconciliation invariants                                                                  | Cross-domain campaigns                                   |
| Human work/messaging                     | **CONTRACT ONLY:** no generalized service required                                             | One approval task; messaging only if selected customer path requires it                                        | General queues, inbox and omnichannel                    |
| Experience/legacy extraction             | **IMPLEMENT:** accessible GWC analysis/proposal workspace and relevant Go parity               | Add governed execute/repair states                                                                             | Dynamic workspaces and broader migration                 |
| Forms/business rules                     | Typed simulation inputs and bounded deterministic thresholds                                   | One approval/reason form if required                                                                           | General customer-authored catalog                        |
| Secrets/workload access                  | Connector and service secret references, least privilege                                       | Short-lived write credentials and operator procedure                                                           | JIT/support and compromise exercises                     |
| Recovery/supply chain                    | Signed Go build/SBOM and restore appropriate to read-only state                                | Restore ledger/workflow/outbox/projections; rolling upgrade/rollback                                           | Isolated cyber-recovery/game days                        |
| Locale/money/time/legal/classification   | Actual pilot types and versioned input metadata                                                | Same scope enforced on writes and timers                                                                       | Country/regulatory computation depth                     |
| Source authority/reference/provenance    | **IMPLEMENT** pilot mappings, exact identity linkage and authoritative-observation distinction | Writer authority, ambiguity, budget authority and future revalidation enforced                                 | Mastering/handoff and identity merge expansion           |
| Position/headcount                       | Read/simulate capacity and compatibility                                                       | Reservation/conflict/revalidation slice                                                                        | Full position/workforce-plan authority                   |
| HRIS DataOps                             | Connector test, diff, temporal/provenance and AuthZ explanation                                | Redrive/repair and config diff as required                                                                     | Productized toolkit                                      |
| Agent                                    | **OUT unless scope-exchanged**                                                                 | **OUT unless scope-exchanged**                                                                                 | Read/draft then higher autonomy by separate gate         |
| Entitlement/metering                     | Pilot entitlement and fixed-price external invoice                                             | Idempotent semantic usage only if commercially required                                                        | Rating/billing engine                                    |
| Tenant placement                         | Logical tenant/cell identity and per-tenant limits                                             | Same                                                                                                           | Multiple cells/live relocation                           |
| Consent/notice                           | Pilot notice evidence and optional processing authority if used                                | Withdrawal/propagation only for processing actually present                                                    | General privacy-preference service                       |
| Remaining workflows/regulatory engines   | **CONFORMANCE ONLY**                                                                           | **CONFORMANCE ONLY**                                                                                           | Separate domain investments                              |
| Search/OLAP/vector/Kafka/full billing    | **OUT**                                                                                        | **OUT**                                                                                                        | Adopt only after measured need                           |
| Cases/e-signature/omnichannel            | **OUT**                                                                                        | **OUT unless customer path requires bounded provider integration**                                             | Separate product gates                                   |

Anything not required to pass the executable Promotion workflow or protect its production use defaults to `DESIGN / CONFORMANCE ONLY` or `OUT OF PHASE`.

Promotion evidence:

- Paid partners use the workflow repeatedly
- Cycle time, error rate, manual work, or audit effort materially improves
- Intended and observed states reconcile reliably
- Identity, privacy, approval, and repair controls pass customer review
- The implementation is repeatable enough to support another customer
- The WorkflowSimulationContract exposes exact proposal, reads, writes, stream heads, conflicts, approvals, source authority, obligations, side effects, completion, revalidation, and repair
- Promotion scenarios pass stale approval, concurrent transfer, future-effective change, external partial failure, corrective history, retry, and replay
- UI, HTTP, and gRPC share authorization, validation, ledger, error,
  idempotency, and entitlement semantics; an agent is included only through the
  explicit scope-exchange rule and then uses the same governed path
- The pilot restores its authoritative data and required projections within its declared RPO/RTO and passes the reference workflow afterward
- No deferred subsystem is pulled into Phase 1 without removing comparable scope or passing an explicit dependency decision

### Phase 2: HCM Workflow Operating Layer

Goal:

> Prove that common HCM transaction primitives support multiple workflow families without bespoke rebuilding.

Potential expansion:

- Productize proven HRIS DataOps operator capabilities as a governed admin toolkit
- Promote the first connector through governed/certified evidence and prove a second connector family reuses the same mapping, scheduling, journal, redrive, and reconciliation runtime
- Onboarding and offboarding
- Reorganizations
- Document workflows
- Selected leave or employee-relations processes after privacy review

Promotion evidence:

- Shared platform primitives work across several workflows
- At least two HRIS DataOps capabilities are used independently of the workflow that originally required them
- Concurrent, future-dated, stale-approval, and retroactive cases are reliable
- Long-running workflow recovery and repair meet defined service levels
- Connector packages and customer onboarding become repeatable
- A second connector uses shared runtime contracts without introducing a parallel vendor-specific integration architecture
- Administrative governance works across HR, payroll, finance, IT, and security
- Capability discovery and workflow composition reuse governed primitives across workflow families
- Agent planning and execution authorization remain separate and auditable
- Quarantine, pause, safe-point intervention, repair, and compatible migration are proven against live-instance failure scenarios
- Corporate scope resolution and inheritance remain explainable across parent, company, and legal-entity policies
- Customer policy simulation proves inherited grants, mandatory denies, obligations, and organization moves cannot create silent privilege expansion
- Multi-country workflows resolve language, currency, timezone, calendar, and jurisdiction independently with explainable provenance
- Localized document variants and calendar calculations remain reproducible by version
- Legal pack simulation identifies affected workflows, capabilities, documents, agents, retention, and pending transactions
- Data-subject requests, retention, legal holds, and processing inventory use reusable platform workflows
- Governed semantic schemas and certified metrics work across multiple workflow families without changing canonical worker facts
- Capability-generated activity and process events replace ad hoc frontend instrumentation
- Agent-authored workflow drafts pass deterministic compilation, simulation, generated tests, and human publication
- Specialized agents delegate without expanding human, organization, purpose, or capability authority
- Event consumers are independently replay-safe and publish scoped provenance, watermarks, lag, schema version, and rebuild status
- Secure search constrains tenant, organization, classification, and resource scope during candidate generation and verifies current AuthZ before disclosure
- Shadow rebuilds prove that new projection and index versions can be compared and promoted without rewriting authority-bearing history
- Capability manifests declare entitlement, meter, quota, and cost-estimation behavior at customer-visible semantic boundaries
- Agent-authored workflows expose expected usage and cost ranges before publication
- Parent and subsidiary usage attribution works independently from payer and invoice scope
- Recruit → Hire → Onboard and Termination → Offboarding validate creation and closure of the worker lifecycle without workflow-specific platform exceptions
- Reference scenarios enforce Person, Worker, Employment, Position, identity, and completion-state distinctions consistently
- Rule Pack validation, impact simulation, future-effective publication, quarantine, and current-law revalidation work across more than one legal domain
- Obligation and statutory-calendar APIs create explainable workflow steps, deadlines, responsible parties, satisfaction evidence, and overdue states
- Identity resolution, transaction reservations, side-effect dependencies, compensation, schema compatibility, and configuration promotion are reused across at least two workflow families
- GoWebComponents meets accessibility, performance, hydration, large-list, and intermittent-connectivity budgets for shipped experiences
- grpcbridge passes transport security, streaming, cancellation, backpressure, metadata, error, load, and failure-injection conformance

### Phase 3: System of Transaction

The owner-authorized small-business track adds relationships, jobs, quotes, billing, setup, and selected domain extensions through the same capabilities and workflows. Detailed work is sequenced by dependency and feature acceptance in the expansion plan; workforce-only wording below describes the retained ChangeOps track.

Goal:

> Become the trusted place where selected HCM changes are initiated, approved, and governed while another platform remains the domain system of record.

Promotion evidence:

- Multiple customers grant scoped production write authority
- Material transaction volume is sustained safely
- Reconciliation, success, repair, and audit metrics meet published thresholds
- Bypass, outage, rollback, and authority-handoff procedures are tested
- Independent assurance matches enterprise buyer expectations
- Emergency bypass obligations, repair authority, and workflow migration controls pass customer audit review
- Cross-company transactions preserve legal-entity isolation while maintaining one causal transaction history
- Parent and subsidiary authorization supports aggregate sharing without unintended worker-level disclosure
- Financially material conversions, business dates, and jurisdiction changes remain historically explainable and revalidated before execution
- Customer counsel can approve and govern effective-dated legal interpretations independently by jurisdiction and corporate scope
- Data residency, processor, and cross-border transfer restrictions are enforced before disclosure
- Cross-domain analytical queries preserve organization, field, purpose, cohort, temporal, and legal restrictions
- Agent analytics use semantic query plans rather than warehouse credentials or unrestricted SQL
- Operational agents monitor bounded event subscriptions and stop safely when confidence, cost, error, or blast-radius limits are exceeded
- Model-provider changes use evaluation and shadow evidence without bypassing privacy, region, or employment-use eligibility
- Signed integrity epochs and external evidence anchors detect unauthorized historical alteration for the event classes that require them
- Tenant shard routing, time partitioning, backfills, and dedicated-tenant placement meet measured scale and residency requirements without changing semantic APIs
- Search, analytics, semantic indexes, and caches can be independently lost and rebuilt within stated recovery objectives
- Production usage, rating, charge, credit, invoice, and provider-cost records reconcile under late events, retries, adjustments, and contract changes
- Customers can forecast, budget, allocate, and explain variable AI, API, analytics, storage, and integration usage
- Model routing improves cost only inside customer, quality, privacy, residency, legal, and service-level constraints
- Cross-Org / Cross-Company Transfer demonstrates one causal transaction across legal entities, employments, currencies, payroll, IAM, residency, and scoped authority
- Payroll Correction proves bitemporal reconstruction, monetary correction, append-only repair, provider idempotency, and full reconciliation
- Regulatory coverage manifests state supported jurisdictions, domains, worker types, calculations, filings, exclusions, connectors, validation level, and counsel-required configuration
- Government filing packages preserve report definition, source watermarks, approvals, payload hashes, submissions, acknowledgements, rejections, and amendments
- Critical Go platform dependencies have pinned versions, license and security ownership, reproducible builds, upgrade tests, and credible replacement paths
- Tenant sandbox, bundle promotion, migration, progressive rollout, restore, and business-continuity procedures use only Go product paths

### Phase 4: Domain System of Record

Small-business stock, equipment, procurement, scheduling, portals, accounting adapters, and device operations gain authority only for their explicitly implemented and qualified scope. PHASE_3/PHASE_4 backlog labels are delivery horizons, not claims that previous phases are complete or approvals to replace incumbents wholesale.

Goal:

> Become authoritative for selected domains where Human Capital Management Suite can operate more safely and effectively than an integration-only approach.

Candidate domains:

- Organization and position management
- Employee action records
- Workflow and policy history
- Change ledger
- Selected policy-driven employee facts

Each domain requires its own:

- Authority contract
- Migration and backfill validation
- Effective-dated and retroactive correctness proof
- Downstream ownership agreements
- Reconciliation and recovery model
- Reversible cutover plan

Before Human Capital Management Suite becomes authoritative for leave, payroll-adjacent, employment, or identity domains, the applicable Leave → Return, Payroll Correction, Hire, Transfer, and Termination reference scenarios must pass under production-scale recovery and privacy controls.

Authoritative payroll, tax, statutory leave, or government-reporting capability additionally requires jurisdiction-specific specialist validation, golden calculations, filing certification or provider approval where applicable, regulatory update operations, correction procedures, and customer transition plans.

### Phase 5: Programmable HCM Core and Workforce OS Expansion

Goal:

> Replace selected legacy HCM modules and expand into a unified Workforce Operating System only after years of operational evidence and customer trust.

This phase is justified only when Human Capital Management Suite can demonstrate that operating an authoritative domain is safer, more adaptable, and economically better than integrating with the incumbent.

Expansion should follow shared worker journeys rather than recreating an incumbent catalog in arbitrary order. A likely strategic sequence is:

1. **People foundation** — authoritative worker, employment, organization, position, document, and lifecycle records.
2. **Access and lifecycle** — onboarding, offboarding, workforce identity, provisioning, and entitlement governance.
3. **Rewards adjacency** — deeper compensation and benefits, with payroll evaluated as a separate major commitment.
4. **Workforce operations** — time, attendance, scheduling, absence, and leave where customer and industry demand justify the depth.
5. **Talent and experience** — recruiting, performance, skills, learning, service delivery, and communications on the shared graph.
6. **Planning and intelligence** — cross-suite planning, analytics, and governed AI that can turn approved plans into transactions.

Product-family sequencing remains evidence-driven. Different customer segments may justify a different order. Every expansion must connect through shared identity, work, authorization, capability, and evidence contracts, while preserving independent domain authority; commercial parties and stock need not be modeled as workers.

The architectural breadth in this plan does not imply that deep domain kernels already exist. Until separately approved and specified:

| Domain                        | Current plan status                                                                              |
| ----------------------------- | ------------------------------------------------------------------------------------------------ |
| ChangeOps transaction control | Initial product and implementation target                                                        |
| People/employment/position    | Shared contracts plus progressively implemented pilot slices                                     |
| Payroll                       | Integration and correction stress test; authoritative gross-to-net remains a separate commitment |
| Benefits                      | Eligibility/enrollment architecture only; plan administration depth remains unspecified          |
| Time and workforce management | Temporal and workflow contracts only; clock, scheduling, and optimization kernels are deferred   |
| Recruiting/talent/learning    | Reference-workflow and graph compatibility; domain product depth is deferred                     |
| Government filing/regulatory  | Architecture and interfaces; production content/engine coverage requires a dedicated investment  |
| Workforce Access product      | Lifecycle integration first; authoritative customer IAM product scope is deferred                |

Each authoritative domain requires its own product thesis, subject-matter ownership, calculation/invariant model, jurisdiction coverage, conformance suite, operating SLO, correction model, and commercial gate.

## 14. Strategic Success Measures

### 14.1 Customer Value

- Employee-change cycle time
- Manual touchpoints per transaction
- Avoided payroll or compliance errors
- Approval and audit completeness
- Reconciliation exception rate
- Mean time to repair
- Time to change a workflow or policy

### 14.2 Product Trust

- Percentage of approvals bound to valid proposal versions
- Conflicts detected before execution
- Transactions reconciled within target time
- Stale approvals prevented
- Repair actions with complete causal history
- Security, privacy, or audit-critical incidents
- Customer willingness to expand authority

### 14.3 Platform Repeatability

- Time to onboard a workflow family
- Time to deploy a supported connector
- Percentage of configuration reused across customers
- Services effort per deployment
- Workflow and integration failure rates
- Long-running workflow recovery success
- Percentage of product actions represented by governed capability manifests
- Semantic parity across UI, HTTP, gRPC, workflow, and agent invocation paths
- Capability reuse across workflow families and customers
- Authorization decision latency, explanation completeness, and policy-test coverage
- Agent plans rejected or modified before execution because of policy, risk, or stale context
- Bulk operations executed through governed workflows rather than unrestricted request loops
- Percentage of interventions performed through typed repair or migration capabilities
- Bypass obligations completed within policy deadlines
- Time to quarantine a bad version and identify affected instances
- Migration previews classified as safe, transformable, repair-required, or impossible
- Scoped configuration resolutions with complete inheritance and override explanations
- Cross-company workflows reconciled without unauthorized field exposure
- Percentage of sensitive capabilities covered by organization, domain, field, and population policy tests
- Authorization decisions with complete policy, scope, field, and obligation explanations
- Customer policy changes simulated before publication
- Organization changes that correctly trigger authority and approval reevaluation
- Aggregate queries prevented from exposing cohorts below configured privacy thresholds
- LocaleContext resolutions with complete source, fallback, and configuration provenance
- Financial transactions using explicit currencies and fixed-precision arithmetic
- Material FX conversions with recorded rate source, purpose, date, and rounding
- Business-date and SLA calculations reproducible from versioned calendars
- Required localized content available without unsafe fallback
- Legal documents reproducible by template, locale, jurisdiction, inputs, and rendered hash
- Material transactions with complete LegalContext, applicable rules, obligations, and evidence
- Sensitive payloads stored outside immutable event bodies under retention and cryptographic controls
- Legal holds that suspend destruction without broadening access
- Data-subject requests completed within applicable policy deadlines with recipient reconciliation
- Processing registry coverage derived from capability, workflow, integration, agent, and execution metadata
- Legal-rule changes simulated before future-effective activation
- High-risk AI uses with current classification, human-oversight, evaluation, and assessment evidence
- Decisions with reproducible visible-input snapshots, recommendation provenance, reasons, and resulting-action links
- Analytical entities and metrics with complete source, transformation, classification, owner, and version lineage
- Reports that explicitly label effective versus known-at-the-time state
- Activity collection demonstrably limited to meaningful in-product actions
- Raw identifiable analytical events reduced, pseudonymized, aggregated, or deleted under policy
- Workflow optimization proposals reviewed and measured before activation
- Agent tasks routed to deterministic or least-complex eligible execution paths
- Production agents meeting task, grounding, policy, cost, latency, and override-rate thresholds
- Predictions evaluated against matured outcomes with calibration and drift reporting
- Agent-authored workflows with compile, simulation, and curated-regression pass rates
- Model-routing decisions with complete eligibility and cost provenance
- Cache reuse without cross-scope, stale-policy, or sensitive-data leakage
- Organizational memories promoted only through explicit review and later outcome validation
- A6 operations bounded by population, capability, volume, time, budget, error threshold, and kill switch
- Ledger stream-sequence and integrity verification success by tenant, shard, and epoch
- Projection, search, analytics, and semantic-index lag against published freshness objectives
- Percentage of derived stores with tested full and shadow rebuild procedures
- Reconciliation drift rate by serving plane and time to verified repair
- Artifact hash, encryption, retention, legal-hold, and reference-integrity coverage
- Event-consumer duplicate suppression, replay success, poison-event isolation, and backfill completion
- Backup restoration and tenant-shard migration completed with before-and-after integrity verification
- Secure-search tests covering result, count, facet, suggestion, snippet, timing, and cohort side channels
- Entitlement decisions consistent across UI, API, workflow, integration, and agent invocation
- Billable semantic executions with exactly one accepted UsageEvent or an explainable non-billable outcome
- Usage-to-charge-to-invoice reconciliation rate and time to resolve discrepancies
- Percentage of invoice lines explainable by product, meter, quantity, allowance, rate, discount, adjustment, and allocation
- Forecast accuracy for variable AI, API, analytics, storage, integration, and workflow consumption
- Budget and quota alerts delivered before exhaustion, plus safe handling of hard limits and emergency exceptions
- Gross margin and provider-cost attribution coverage by tenant, product, capability, workflow, agent, model, and integration
- Billing adjustments, disputes, refunds, and credits completed through append-only governed workflows
- Agent and workflow cost simulations compared with actual usage by version
- Reference workflows with complete conformance coverage across identity, authority, legal, temporal, integration, data, billing, and repair contracts
- Workflow simulations exposing all planned reads, writes, affected fields, effective ranges, reservations, and external effects before execution
- Simulation assumptions and predictions compared with observed execution and reconciliation outcomes
- Approved transactions blocked or returned safely when execution-time revalidation detects material change
- Business completion reported independently from external consistency, reconciliation, operational health, and outstanding obligations
- Person-match ambiguity, employment overlap, position capacity, and cross-workflow conflict cases resolved without implicit data mutation
- High-risk ordered side effects executed at declared boundaries with complete repair or escalation for partial failure
- Reference workflow regression suites passing after changes to the Worker Graph, runtime, AuthZ, Legal, billing, integration, data plane, or Agent Runtime
- Material transactions with a complete, evidence-backed JurisdictionContext and explicit unresolved-jurisdiction handling
- Active Rule Packs with current sources, effective dates, interpretation owners, review status, test coverage, and customer approval where required
- Regulatory calculations reproducible from input snapshots, engine versions, Rule Packs, composition traces, accumulators, currency, and rounding
- Tax and contribution results matching jurisdiction-specific golden cases and certified external benchmarks where available
- Obligations satisfied, waived, disputed, or escalated before their statutory or contractual deadlines
- Government reports reconciled to authority-bearing payroll facts before submission
- Filing acceptance, rejection, correction, and acknowledgement rates by authority and report-definition version
- Regulatory changes impact-analyzed, tested, reviewed, and published before their effective dates
- Country and regional packs with accurate coverage manifests and no unsupported feature implied by broad country labels
- Agent regulatory explanations grounded in deterministic traces, citations, coverage status, and explicit uncertainty
- Percentage of product capability ownership implemented in Go and exposed through registered Protobuf contracts: 100%
- Release images, build graph, and runtime dependency graph contain no Node, TypeScript, React, Vite, or npm requirement
- GoWebComponents startup, interaction, memory, hydration, accessibility, large-list, and offline-recovery performance against budgets
- SchemaFlux outputs reproducible from source with no unreviewed generated-code or documentation drift
- Identity matches, possible matches, merges, separations, and do-not-merge decisions with complete evidence and false-match review
- Concurrent write intents detected before approval and again before execution across resource, field, and effective-time boundaries
- Authority disputes resolved under an explicit field/domain policy rather than last-write behavior
- Registered contracts with owners, compatibility status, consumers, generated artifacts, test fixtures, and deprecation plans
- Classified sensitive fields, artifacts, events, search fragments, analytical columns, exports, and agent contexts with derived-data lineage
- Secrets, signing keys, certificates, and connector credentials rotated before expiry without entering logs, ledgers, prompts, or exports
- Configuration bundles promoted through sandbox with deterministic fixtures, signed manifests, staged rollout, verification, and rollback
- Certified connectors passing schema, security, idempotency, replay, rate, health, reconciliation, and failure tests
- Quality and invariant checks with measured coverage, false-positive rate, evaluation cost, repair outcome, and overdue violation state
- Material values and decisions with traversable source-to-transformation-to-effect-to-outcome provenance
- Open-source dependency cost, upgrade age, vulnerability remediation, operational effort, and replacement readiness

### 14.4 Commercial Validation

- Paid pilot conversion
- Pilot-to-annual conversion
- Expansion by workflow, system, or authority scope
- Implementation gross margin
- Sales-cycle duration
- Renewal and referenceability
- Proven economic value relative to added platform cost

## 15. Major Risks and Responses

The active Phase 1 risks are scope expansion, unclear authority, stale or conflicting approval, workflow bypass of Domain ownership, derived-plane coupling in the critical path, connector partial failure, insufficient customer value, unsafe AI/tool use, tenant or credential leakage, unverified recovery, runtime overload, premature infrastructure decomposition, and losing proven behavior while extracting it from the retired TypeScript/React implementation.

Owners review these risks at each authority gate. The complete long-term register and responses are maintained in [the risk register](specs/risk-register.md).

## 16. Strategic Decisions

| Decision                           | Direction                                                                                                                                   |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Initial product                    | Human Capital Management Suite ChangeOps                                                                                                    |
| Adjacent administrator product     | HRIS DataOps, productized incrementally from proven governed operator capabilities                                                          |
| Integration architecture           | Horizontal connector definitions, tenant connections, mappings, operation journals, scheduling, observation, and repair                     |
| Communications architecture        | Semantic intent, audience/endpoint resolution, deterministic content, secure inbox, channel policy, delivery evidence, and workflow signals |
| Capability coverage governance     | Maintain DEFINED/PARTIAL/IMPLIED/MISSING/DEFERRED status independently from implementation depth and review it at authority gates           |
| Shared business services           | Human Work, Forms, and deterministic Business Rules remain separate from workflow topology and domain engines                               |
| Product category                   | HCM Transaction Control Plane                                                                                                               |
| Long-term category                 | Workforce Operating System                                                                                                                  |
| Initial workflow family            | Job, compensation, manager, organization, and combined promotion changes                                                                    |
| Kernel business abstraction        | `BusinessIntent`; material mutations and external effects become `BusinessTransaction`s                                                     |
| ChangeOps business abstraction     | `HCMChangeRequest` is central to Phase 1, not the universal Workforce OS root                                                               |
| Reference integration suite        | Hire, promotion, transfer, leave/return, and termination/offboarding lifecycle workflows                                                    |
| Repair stress test                 | Payroll correction and retroactive pay repair                                                                                               |
| Reference-suite role               | Architecture conformance and simulation first; phased product implementation later                                                          |
| Simulation contract                | Common pre-execution reads, writes, conflicts, obligations, risk, cost, effects, and repair model                                           |
| Completion model                   | Business, external consistency, reconciliation, operational, and obligation states remain separate                                          |
| Initial relationship to incumbents | Overlay and control layer, not replacement                                                                                                  |
| Core data model                    | Shared Person/Worker Graph with stable HCM primitives and governed metadata                                                                 |
| Source of truth                    | Human Capital Management Suite for transaction truth; configured authority for employee domains                                             |
| Ledger assertion authority         | Transaction fact, domain fact, external observation, claim, and correction remain explicit                                                  |
| Multi-stream transaction           | Validate expected heads and append local authoritative events, projections, and outbox atomically                                           |
| Approval model                     | Immutable proposal and context-bound approval                                                                                               |
| Conflict model                     | Effective-dated write sets with explicit resolution policy                                                                                  |
| Simulation promise                 | Provenance-aware analysis, not certainty about opaque systems                                                                               |
| Ledger role                        | Authoritative business-event source, never a substitute for telemetry                                                                       |
| Physical data model                | Polyglot serving planes behind one authoritative business chronology                                                                        |
| Artifact authority                 | Large verbatim content is content-addressed, hash-bound, and independently retained                                                         |
| History integrity                  | Mutation-prohibited, per-stream chained, externally anchored, and operationally verifiable                                                  |
| Scale boundary                     | Tenant-routed shards with time partitions and dedicated placement when justified                                                            |
| Event distribution                 | Transactional outbox followed by independent idempotent and replay-safe consumers                                                           |
| Read model                         | Purpose-built disposable projections, not raw-ledger operational queries                                                                    |
| Derived-data contract              | Every serving plane exposes provenance, watermark, version, drift, and tested rebuild behavior                                              |
| Search security                    | Scope-aware candidate generation followed by authoritative resource and field authorization                                                 |
| Embedding status                   | Versioned semantic projection, never source data or canonical worker truth                                                                  |
| Query architecture                 | Governed semantic router selects cache, projection, search, analytics, ledger, or object plane                                              |
| Repair model                       | Business correction or derived-state rebuild through a governed RepairPlan                                                                  |
| Operational model                  | Continuous reconciliation, invariants, incidents, and causal graphs                                                                         |
| Correctness vocabulary             | Validation, data quality, invariant enforcement, and reconciliation answer different questions                                              |
| Foundation priorities              | Identity, conflicts, authority, contracts, classification, secrets, promotion, connectors, correctness, provenance                          |
| Foundation promotion               | Thin reference-workflow slice first; reusable service only after a second domain proves it                                                  |
| API principle                      | Every product action is a semantic, governed, addressable capability                                                                        |
| Canonical plane model              | Experience, Identity, Governance, Control, Workflow, Domain/Regulatory, Connectivity, Data, and Intelligence                                |
| Domain ownership                   | Domain capabilities own HCM validation, invariants, transactions, events, and critical projections                                          |
| Workflow boundary                  | Workflow coordinates typed capabilities and never directly mutates domain or projection storage                                             |
| Cross-cutting overlays             | Operations/Assurance and Billing/Metering observe and constrain the stack without becoming universal synchronous hops                       |
| Application language               | Go only across control plane, domains, runtime, integrations, agents, operations, tooling, and UI                                           |
| UI framework                       | GWC/GoWebComponents with Go/WASM and SSR where appropriate; no React or authored TypeScript                                                 |
| Service contract                   | Protobuf and gRPC are canonical; generated clients and adapters preserve one semantic contract                                              |
| Web gateway                        | grpcbridge for required HTTP, gRPC-Web, WebSocket, and SSE adaptation                                                                       |
| Definition compiler                | SchemaFlux for governed structured compilation; it complements Protobuf and database migrations                                             |
| Legacy posture                     | Extract behavior and fixtures, implement clean Go equivalents, then exclude Node/TypeScript/React/npm from builds and releases              |
| Legacy implementation evidence     | Older documentation is a regression inventory; only fresh executable checks can establish current verification                              |
| Experience contract                | Typed pages, registered widgets, semantic brands, provenance, authorization, and accessibility remain renderer-independent                  |
| Platform shape                     | Modular Go monolith plus scalable workers before evidence-based service decomposition                                                       |
| Planning hierarchy                 | Master constitution plus Phase 1 execution plan and demand-created focused specifications                                                   |
| Phase implementation depth         | Implement, minimal contract, design/conformance only, or out of phase                                                                       |
| Production isolation               | Tenant-routed cells define blast radius; global services hold routing metadata rather than ordinary worker payloads                         |
| Placement                          | Versioned policy resolves cell, region, residency, isolation, capacity, and SLA; relocation preserves tenant identity                       |
| Resource governance                | Per-tenant cost budgets and propagated P0-P4 criticality drive admission, shedding, backpressure, and degradation                           |
| Overload retry rule                | Only the immediate caller retries; attempts consume propagated budgets and honor explicit do-not-retry responses                            |
| Workload trust                     | Short-lived workload identity, service AuthZ, default-deny east-west paths, and destination-scoped credentials                              |
| Production access                  | Approved, step-up, time-bounded, recorded JIT authority; no standing broad operator privilege                                               |
| Data egress                        | Every external destination passes AuthZ, legal, DLP, residency, and delivery-evidence checks                                                |
| Software supply chain              | Isolated builds emit SBOM and provenance; signed digest and policy verification gate deployment                                             |
| Agent security boundary            | Taint-aware inputs, external tool mediation, provider eligibility, typed output validation, and scoped containment                          |
| Recovery proof                     | Immutable isolated copies plus regular full-stack restore, replay, invariant, and reference-workflow verification                           |
| Temporal trust                     | Monitored trusted time and skew bounds augment sequences; wall clock never defines concurrency alone                                        |
| Cryptographic posture              | Versioned crypto profiles, usage inventory, dual verification, migration rehearsal, and compromised-key response                            |
| Telemetry governance               | Privacy filtering, bounded cardinality, risk-aware sampling, and monitored collector pipelines                                              |
| Dependency policy                  | Open-source and open-protocol first, judged by total cost, security, operations, and exit path                                              |
| Diagram policy                     | ASCII views explain boundaries and flows; executable contracts and tests remain authoritative                                               |
| Transport strategy                 | HTTP, gRPC, events, and SDKs adapt one Protobuf-defined domain capability contract                                                          |
| Authorization model                | Subject, action, resource, relationship, fields, purpose, context, and policy                                                               |
| Agent authority                    | Separate identity with delegated, intersected capability grants                                                                             |
| Agent execution                    | Compose and propose workflows; deterministic runtime performs material work                                                                 |
| Live-instance rule                 | Past execution is immutable; current and future execution are repairable                                                                    |
| Intervention model                 | Typed retry, resume, skip, satisfy, override, rewind, compensate, and migrate                                                               |
| Workflow versioning                | Instances remain pinned unless a governed compatible migration is approved                                                                  |
| Bad-version response               | Quarantine new starts and control existing instances at safe points                                                                         |
| Corporate hierarchy                | Parent, company, and legal-entity scopes live inside one tenant boundary                                                                    |
| Inheritance rule                   | Specific configuration may override; mandatory security restrictions persist                                                                |
| Organization security              | Organization scope is a first-class policy input, separate from tenant identity                                                             |
| Authentication boundary            | Authentication creates PrincipalContext; authorization determines authority                                                                 |
| Data authorization                 | Stable domains, fields, records, populations, queries, aggregation, and export                                                              |
| Customer policy                    | Versioned, simulated, explainable policy with inherited denies and obligations                                                              |
| Data-layer enforcement             | Sensitive repositories require an evaluated AuthorizationScope                                                                              |
| Globalization model                | Resolve language, currency, timezone, calendar, and jurisdiction independently                                                              |
| Canonical API values               | Structured locale-neutral values; formatting occurs at human boundaries                                                                     |
| Financial values                   | Explicit currency, fixed precision, versioned FX source, purpose, and rounding                                                              |
| Business time                      | Distinct Instant, LocalDate, LocalTime, ZonedDateTime, PayPeriod, and BusinessDay                                                           |
| Global documents                   | Versioned locale and jurisdiction variants with rendered-document evidence                                                                  |
| Legal policy                       | Versioned, effective-dated rule packs producing typed obligations and prohibitions                                                          |
| Regulatory architecture            | Shared jurisdiction and obligation framework with specialized deterministic domain engines                                                  |
| Jurisdiction model                 | Effective-dated graph resolved independently for employment, work, tax, payroll, privacy, and data                                          |
| Rule composition                   | Declared per family: additive, protective, restrictive, concurrent, offset, exclusive, or custom                                            |
| Regulatory content                 | Granular Country and Regional Packs with explicit coverage manifests and known exclusions                                                   |
| Tax calculation                    | Fixed-precision deterministic kernel with versioned inputs, accumulators, rules, and explanation                                            |
| Government reporting               | Versioned definitions and immutable FilingPackages through governed submission workflows                                                    |
| Obligation model                   | Typed, assigned, deadline-aware requirements with evidence and satisfaction conditions                                                      |
| Regulatory interpretation          | Vendor baseline plus specialist and customer-counsel-controlled production approval                                                         |
| Legal interpretation               | Human Capital Management Suite baselines with final tenant configuration controlled by customer counsel                                     |
| Privacy processing                 | Purpose, legal basis, classification, parties, regions, retention, and transfer                                                             |
| Sensitive ledger data              | Minimal immutable facts with encrypted, retainable, destructible payload references                                                         |
| Compliance operations              | DSAR, retention, holds, DPIA, breach, and legal-change workflows                                                                            |
| Intelligence categories            | Facts, observations, decisions, interactions, and inferences remain distinct                                                                |
| Temporal analytics                 | Current, effective-as-of, known-as-of, between, and change-history modes                                                                    |
| Decision intelligence              | Visible-input snapshots, recommendations, reasons, actions, and outcome links                                                               |
| Semantic extensibility             | Governed versioned schemas and observations that never overwrite canonical facts                                                            |
| Analytical access                  | Semantic query plans with AuthZ, legal-purpose, cohort, and export controls                                                                 |
| Billing architecture               | Separate entitlement, metering, rating, billing, and provider-cost responsibilities                                                         |
| Commercial metering                | Customer-visible semantic boundaries, never raw logs or internal service-call count                                                         |
| Billing history                    | Versioned contracts and append-only usage, charge, credit, refund, and invoice evidence                                                     |
| Billing hierarchy                  | Payer, usage, entitlement, invoice, and allocation scopes resolve independently                                                             |
| Pricing model                      | Predictable subscription/module pricing plus allowances and selected variable-use overage                                                   |
| AI pricing                         | Stable customer credits with provider tokens and actual COGS retained internally                                                            |
| Cost governance                    | Agent and workflow budgets, forecasts, limits, and semantic cost lineage                                                                    |
| Financial correction               | Idempotent metering and append-only adjustments rather than historical mutation                                                             |
| Agent-runtime boundary             | Agents interpret and propose; deterministic services execute authoritative changes                                                          |
| Agent configuration                | Versioned scoped AgentDefinitions with capabilities, purpose, memory, budget, and evaluation                                                |
| Model routing                      | Provider-neutral task routing constrained by quality, privacy, region, and cost                                                             |
| Agent autonomy                     | Explicit A0-A6 ladder with capability requirements and bounded A6 operation                                                                 |
| Agent learning                     | Predictions, evaluations, and observed patterns produce proposals, never silent production changes                                          |
| AI role                            | Review and explanation, not autonomous material decisions                                                                                   |
| First integrations                 | Import/export, REST/webhook, then one deep HCM connector                                                                                    |
| Workflow authoring                 | Schema-first administration before a visual canvas                                                                                          |
| Extension model                    | Controlled, signed, capability-scoped extensions later                                                                                      |
| Authority expansion                | Per tenant and domain, gated by evidence                                                                                                    |
| Full HCM destination               | Six unified pillars: People, Workforce, Talent, Rewards, Experience, Access                                                                 |
| Full HCM replacement               | Long-term option, not the initial sales motion                                                                                              |

## 17. Near-Term Planning Priorities

Planning is closed until P1A executes. The next planning cycle, when it opens, is limited to ten sequenced workstreams. Each must end in a decision, executable contract, fixture, or pilot artifact, and none may add a lifecycle dimension, kernel family, workflow primitive, or coordination layer without a scope exchange.

| #   | Workstream                           | Depends on | Concrete outcome                                                                                          |
| --- | ------------------------------------ | ---------- | --------------------------------------------------------------------------------------------------------- |
| 1   | Design partner and wedge             | —          | Signed pilot thesis, bounded mutation fields, baseline metrics, commercial and exit criteria              |
| 2   | Promotion reference specification    | 1          | Canonical scenarios, BusinessIntent/ChangeRequest lifecycle, expected events and completion states        |
| 3   | Capability and schema kernel         | 2          | Protobuf contracts, compatibility policy, Go clients, grpcbridge proof, SchemaFlux catalog proof          |
| 4   | Authority, identity, and data safety | 2–3        | Source-authority rules, Platform IAM/AuthZ, classification, secret references, pilot reference data       |
| 5   | Transaction integrity kernel         | 2–4        | Proposal binding, write sets, multi-stream commit, conflicts, reservations, revalidation                  |
| 6   | Durable execution and human work     | 3–5        | Typed graph/compiler, instances/nodes, timers/leases, approvals/tasks, inspect/replay, retry/backpressure |
| 7   | Connector and reconciliation         | 3–6        | One certified connector, observation semantics, reconciliation, degraded completion, RepairPlan           |
| 8   | Production trust baseline            | 3–7        | Signed build/SBOM, JIT access, logical placement, tenant limits, telemetry policy, tested restore         |
| 9   | Bounded user and agent experience    | 3–8        | GoWebComponents workspace and one read/analyze/draft agent behind the tool-security gateway               |
| 10  | Pilot evidence and authority gate    | 1–9        | Load/reference tests, SLO/RPO/RTO results, customer outcomes, write-authority and Phase 2 decision        |

Workstreams 1–5 precede material write authority. Workstreams 6–9 may overlap after their dependencies stabilize. Workstream 10 decides whether to expand, repeat, narrow, or stop.

### Architecture Specification Backlog

The detailed charter inventory is maintained in [the architecture specification backlog](specs/architecture-charter-backlog.md). Items enter delivery only through the Phase 1 implementation-depth matrix or a later evidence-based phase gate.

## 18. Final Plan Statement

> Human Capital Management Suite is an HR and workflow foundation expanding into a small-business operations workspace. Existing enterprise ChangeOps remains a bounded transaction proof; the 2026-10-22 expansion plan governs the additional customer, job, resource, commercial, and connectivity domains. It will first make high-risk job, compensation, manager, and organization changes safer and easier to operate. It will distinguish transaction truth from employee-domain authority, bind approvals to immutable proposals, resolve concurrent and effective-dated changes explicitly, reconcile external outcomes, preserve explainable history, and use AI only within governed human accountability. Its ledger will act as the business black box recorder, while rebuildable projections, continuous integrity checks, causal incident views, and governed RepairPlans keep derived and external state aligned with that truth. Every product action will be exposed as a semantic capability governed by record, field, relationship, purpose, context, risk, and identity; agents will discover and compose those capabilities without receiving unrestricted credentials or bypassing deterministic workflows. Live processes will remain version-pinned and historically immutable, yet repairable through explicit intervention and migration plans. Corporate hierarchy, inheritance, and cross-company workflows will operate inside the tenant boundary with directional sharing and security-preserving scope resolution. Authentication will establish principal identity while customer-defined authorization composes organization scope, capability, resource, data domain, field, population, relationship, purpose, and current context into an explainable decision and enforceable workflow obligations. Globalization will preserve canonical business values while independently resolving language, currency, timezone, calendar, formatting, and jurisdiction for every transaction, workflow, document, integration, and human audience. The Legal and Compliance Plane will resolve whether the organization may act or process data, attach versioned obligations and prohibitions, and turn privacy rights, retention, legal holds, regulated AI, legal change, and breach response into auditable workflows controlled by customer-approved interpretation. Workforce Intelligence will connect historical facts, semantic observations, human and agent decisions, meaningful interactions, and explicit inferences through a governed bitemporal semantic model while preserving the authority, provenance, privacy, and uncertainty of each class. The Agent Runtime will make intent the primary interface, discover governed capabilities and semantic context, create reproducible analyses and workflows, and learn through evaluated proposals while deterministic services retain exclusive control of authoritative execution. The physical data plane will protect one canonical event chronology and content-addressed artifacts while serving product, search, analytics, and agents through specialized, provenance-bearing stores that can be independently rebuilt, reconciled, and replaced. The Billing Plane will separate purchased entitlement, principal authority, semantic usage, versioned rating, append-only customer billing, and provider cost so the product remains commercially predictable, operationally explainable, and economically governable as API and agent consumption grows.
>
> Five lifecycle reference workflows—Hire and Onboard, Promotion and Compensation, Cross-Company Transfer, Leave and Return, and Termination and Offboarding—will serve as architecture conformance tests, with Payroll Correction as the bitemporal repair stress test. Their shared Workflow Simulation Contract will make planned state, authority, legal obligations, conflicts, side effects, costs, completion dimensions, revalidation, and repair visible before material execution.
>
> The Regulatory Platform will resolve layered jurisdiction, contract, collective, plan, and company authority and then apply specialized deterministic tax, wage, leave, privacy, immigration, reporting, obligation, and calendar engines. Country and Regional Packs will state their coverage and exclusions precisely; specialists and customer counsel will control ambiguous production interpretations; immutable calculation and filing evidence will make every material result reproducible.
>
> Phase 1 will implement one narrow kernel: `BusinessIntent` with `HCMChangeRequest` as its first subtype, governed capabilities, source authority, immutable proposals, multi-stream transactional integrity, AuthZ, bounded workflow execution, one Integration Platform slice with one design-partner connector, external observation, reconciliation, and repair. Locale, legal, agent, entitlement, placement, reference-data, and provenance contracts will be implemented only to the depth required by the pilot. Broader connector catalogs, regulatory, analytical, billing, agent, and Workforce OS planes remain conformance designs or later investments. The platform core will be Go and Protobuf/gRPC contract-first, with GWC/GoWebComponents, grpcbridge, and SchemaFlux as the preferred UI, edge, and generator once each passes its qualification fixture, and Go server-rendered HTML, grpc-gateway or connect-go, and protoc as their fallbacks. Node, TypeScript, React, and Vite are excluded from the release image and runtime; development tooling is unconstrained.
>
> HRIS DataOps will expose selected operational capabilities—import, compare, temporal/provenance diagnosis, authorization explanation, connector test/redrive, and configuration promotion—to customer administrators when those capabilities are already required by ChangeOps and can be supported safely. It is an adjacent operator product and leverage mechanism, not permission to broaden Phase 1 into a general data platform.
>
> The Integration Platform will normalize third-party APIs, files, events, schemas, permissions, mappings, rate limits, and external effects behind canonical governed capabilities. Every external operation will preserve semantic intent, mapping and credential versions, attempts, observed outcomes, and reconciliation evidence; connector maturity and support claims will advance only through tested evidence.
>
> The Messaging and Notification Plane will turn workflow communication intent into policy-governed human delivery across secure inbox and approved external channels. Audience, language, content, endpoint, preferences, legal requirements, provider eligibility, delivery observations, acknowledgements, and replies will remain explicit; system subscriptions will reuse Integration Platform delivery without collapsing human and machine semantics.
>
> A maintained Platform Capability Coverage Matrix will distinguish defined contracts from partial, implied, missing, and deliberately deferred responsibilities without claiming exhaustiveness. Human Work, Forms, and deterministic Business Rules will become shared business services; Master/Reference Data, Schema Contracts, Crosswalks, Import/Migration, Managed File Transfer, Tenant Lifecycle, Support Access, DLP/Egress, and Conformance Infrastructure will retain explicit ownership so product domains cannot reinvent them invisibly.
>
> Production correctness begins in Phase 1 with logical placement, tenant limits, criticality-aware admission and bounded retries, workload identity on material paths, controlled egress, signed builds, JIT operator access, agent tool mediation, kill switches, restore verification, time-skew monitoring, and privacy-governed telemetry. Physical multi-cell operation, live relocation, advanced crypto migration, and regional recovery are promoted only when authority, scale, or customer commitments require them.
>
> The company will earn broader authority through measured customer value and operational evidence. It will expand from overlay, to workflow operating layer, to system of transaction, and only then to an authoritative system of record for selected domains. From that foundation, Human Capital Management Suite can become a Workforce Operating System spanning People, Workforce, Talent, Rewards, Experience, and Access on one governed Person/Worker Graph. Product scope, technical authority, and commercial investment will advance through explicit decision gates rather than vision alone.

The strategic punchline remains:

> Do not sell better HR software. Sell safe, fast employee changes across broken enterprise HR stacks.

## 19. Market, Legal, and Technical Context References

- [Workday: What is human capital management software?](https://www.workday.com/en-us/topics/hr/human-capital-management-software.html)
- [Workday Human Capital Management](https://www.workday.com/en-us/products/human-capital-management/overview.html)
- [Rippling product ecosystem](https://www.rippling.com/products)
- [UKG Pro Human Capital Management](https://www.ukg.com/products/ukg-pro)
- [Human Capital Management Suite transaction, ledger, reconciliation, and repair contract](specs/transaction-ledger-reconciliation-and-repair.md)
- [Human Capital Management Suite canonical platform plane model](specs/platform-plane-model.md)
- [Human Capital Management Suite business intent catalog](specs/business-intent-catalog.md)
- [Federal Trade Commission: Using Consumer Reports—What Employers Need to Know](https://www.ftc.gov/business-guidance/resources/using-consumer-reports-what-employers-need-know)
- [EUR-Lex: General Data Protection Regulation](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng/)
- [European Data Protection Board: Consent under GDPR](https://www.edpb.europa.eu/system/files/2026-04/edpb-summary-consent_en.pdf)
- [EEOC: Disability-Related Inquiries and Employee Medical Information](https://www.eeoc.gov/laws/guidance/enforcement-guidance-disability-related-inquiries-and-medical-examinations-employees)
- [U.S. Department of Labor: FLSA Recordkeeping Requirements](https://www.dol.gov/agencies/whd/fact-sheets/21-flsa-recordkeeping)
- [USCIS: Form I-9 Retention](https://www.uscis.gov/i-9-central/form-i-9-resources/handbook-for-employers-m-274/100-retaining-form-i-9)
- [EUR-Lex: European Union Artificial Intelligence Act](https://eur-lex.europa.eu/legal-content/EN/TXT/PDF/?uri=OJ%3AL_202401689)
- [European Commission: Navigating the AI Act](https://digital-strategy.ec.europa.eu/en/faqs/navigating-ai-act)
- [California Privacy Protection Agency: Employee Data](https://cppa.ca.gov/regulations/employee_data.html)
- [PostgreSQL: Table Partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html)
- [PostgreSQL: Logical Replication](https://www.postgresql.org/docs/current/logical-replication.html)
- [PostgreSQL: pgcrypto Cryptographic Functions](https://www.postgresql.org/docs/current/pgcrypto.html)
- [Amazon S3: Object Lock](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)
- [Redis: Cache-Aside Pattern](https://redis.io/docs/latest/develop/use-cases/cache-aside/)
- [OpenSearch: Search](https://docs.opensearch.org/platform/search/)
- [OpenSearch: Filtering Vector Search Results](https://docs.opensearch.org/latest/vector-search/filter-search-knn/index/)
- [OpenSearch: Hybrid Query](https://docs.opensearch.org/latest/query-dsl/compound/hybrid/)
- [ClickHouse: Real-Time Analytics and Materialized Views](https://learn.clickhouse.com/visitor_catalog_class/show/1914307/Real-time-Analytics-with-ClickHouse-Level-3)
- [Apache Kafka: Design and Delivery Semantics](https://kafka.apache.org/documentation/#design_deliverysemantics)
- [OpenTelemetry: Logging and Trace Correlation](https://opentelemetry.io/docs/specs/otel/logs/)
- [Internal Revenue Service: Form 941](https://www.irs.gov/forms-pubs/about-form-941)
- [Internal Revenue Service: Employee or Independent Contractor](https://www.irs.gov/businesses/small-businesses-self-employed/independent-contractor-self-employed-or-employee)
- [Internal Revenue Service: Employment Tax Forms](https://www.irs.gov/businesses/small-businesses-self-employed/employment-tax-forms)
- [U.S. Department of Labor: Minimum Wage Questions and Answers](https://www.dol.gov/agencies/whd/minimum-wage/faq)
- [U.S. Department of Labor: State Minimum Wage Laws](https://www.dol.gov/agencies/whd/minimum-wage/state)
- [OECD: Tax Administration 2025](https://www.oecd.org/content/dam/oecd/en/publications/reports/2025/11/tax-administration-2025_6360fad8/cc015ce8-en.pdf)
- [GoWebComponents](https://github.com/monstercameron/GoWebComponents)
- [gRPC for Go](https://grpc.io/docs/languages/go/)
- [gRPC-Go](https://github.com/grpc/grpc-go)
- [grpcbridge](https://github.com/renbou/grpcbridge)
- [Protocol Buffers for Go](https://github.com/protocolbuffers/protobuf-go)
- [SchemaFlux Documentation](https://schemaflux.dev/docs/)
- [AWS SaaS Lens: Tenant Isolation](https://docs.aws.amazon.com/wellarchitected/latest/saas-lens/tenant-isolation.html)
- [AWS SaaS Lens: Tenant Activity and Consumption](https://docs.aws.amazon.com/wellarchitected/latest/saas-lens/tenant-activity-and-consumption.html)
- [Google SRE: Handling Overload](https://sre.google/sre-book/handling-overload/)
- [Google SRE: Addressing Cascading Failures](https://sre.google/sre-book/addressing-cascading-failures/)
- [Kubernetes: Multi-Tenancy](https://kubernetes.io/docs/concepts/security/multi-tenancy/)
- [Kubernetes: Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [Kubernetes: Disruptions](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/)
- [NIST SP 800-207: Zero Trust Architecture](https://csrc.nist.gov/pubs/sp/800/207/final)
- [NIST SP 800-218: Secure Software Development Framework](https://csrc.nist.gov/pubs/sp/800/218/final)
- [NIST: Software Bill of Materials](https://www.nist.gov/itl/executive-order-14028-improving-nations-cybersecurity/software-supply-chain-security-guidance-20)
- [SLSA v1.2: Build Track](https://slsa.dev/spec/v1.2/build-track-basics)
- [OWASP Application Security Verification Standard](https://owasp.org/www-project-application-security-verification-standard/)
- [OWASP: Prompt Injection Prevention](https://cheatsheetseries.owasp.org/cheatsheets/LLM_Prompt_Injection_Prevention_Cheat_Sheet.html)
- [OWASP: AI Agent Security](https://cheatsheetseries.owasp.org/cheatsheets/AI_Agent_Security_Cheat_Sheet.html)
- [NIST AI Risk Management Framework](https://www.nist.gov/itl/ai-risk-management-framework)
- [Google Cloud: Testing Recovery from Data Loss](https://docs.cloud.google.com/architecture/framework/reliability/perform-testing-for-recovery-from-data-loss)
- [IETF RFC 8633: Network Time Protocol Best Current Practices](https://datatracker.ietf.org/doc/html/rfc8633)
- [IETF RFC 8915: Network Time Security for the Network Time Protocol](https://www.rfc-editor.org/rfc/rfc8915.html)
- [NIST: Crypto Agility](https://csrc.nist.gov/projects/crypto-agility)
- [OpenTelemetry Collector: Internal Telemetry](https://opentelemetry.io/docs/collector/internal-telemetry/)
- [SPIFFE](https://spiffe.io/)
- [SPIRE](https://spiffe.io/docs/latest/spire-about/)
- [Sigstore Cosign](https://docs.sigstore.dev/cosign/)
- [Syft](https://github.com/anchore/syft)
- [Grype](https://github.com/anchore/grype)
- [Trivy](https://trivy.dev/)
