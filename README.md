# Human Capital Management Suite

> **Every workforce action, one governed operating model.**

Human Capital Management Suite is a Go-based architecture for expressing, governing, executing,
observing, and repairing Human Capital Management business intent.

It is not architecturally centered on integrations, a collection of modules, or
an AI assistant. Its behavioral center is `BusinessIntent`; its data center is
the governed Person/Worker Graph.

```text
                          BEHAVIOR

                       BusinessIntent
                            |
                            v
Person / Worker Graph <-----+-----> Capability Graph
                            |
                            v
                         Workflow
                            |
                            v
                          Outcome
```

The initial product, **Human Capital Management Suite ChangeOps**, applies this architecture to
cross-system employee changes while incumbent systems remain authoritative. That
is the entry wedge—not the boundary of the architecture.

## Project status

Human Capital Management Suite is currently an architecture and early implementation project. It is
not a production HCM suite, payroll engine, or UKG/Workday replacement.

Artifact maturity must be interpreted precisely:

```text
planning/specs       mixed contracts, active registers and reference documents;
                     consult the ownership registry for each artifact's status
schema/proto         canonical contract sources; generation pinned: run `buf generate`
schema/schemaflux    draft structured sources; no HCM compiler pipeline yet
src/blocks/go        existing Go conformance and execution fixtures
legacy TypeScript    historical behavior evidence; not the target architecture
```

Do not present a planned subsystem, catalog entry, diagram, or Protobuf source as
implemented product behavior. The Phase 1 execution gates define what is actually
being built.

## The core thesis

HCM is a coherent domain of business intent, facts, capabilities, decisions,
workflows, effects, and outcomes.

Examples of business intent include:

```text
Promote Jane                 Hire Sarah
Run payroll                  Request leave
File a government report    Investigate a complaint
Change benefits              Find successors
Correct a paycheck           Provision access
Analyze turnover             Send a legal notice
```

All enter the same architectural spine:

```text
                         BUSINESS INTENT
                                |
                                v
                         INTENT KERNEL
                    classify / normalize / scope
                                |
                                v
                           GOVERNANCE
          Identity | AuthZ | Legal | Privacy | Entitlement
              Risk | Purpose | Jurisdiction | Policy
                                |
                                v
                            WORKFLOW
          capabilities | rules | approvals | tasks | timers
          signals | agents | documents | calculations | waits
                                |
                                v
                       DOMAIN CAPABILITIES
       People | Workforce | Rewards | Talent | Experience | Access
       Payroll | Benefits | Recruiting | Regulatory | Intelligence
                                |
                                v
                    DETERMINISTIC EFFECT PLAN
                                |
              +-----------------+-----------------+
              v                 v                 v
               HCM Suite state   External systems Human interaction
              |                 |                 |
              +-----------------+-----------------+
                                |
                                v
                         OBSERVE OUTCOME
                                |
                                v
                           RECONCILE
                                |
                                v
                    COMPLETE | DIAGNOSE | REPAIR
```

The product's code-level names are `hcmnext`, `HCMNEXT_*` environment
variables, and the `HCM_NEXT` database.

The responsibilities remain distinct:

| Concept             | Question answered                                                                         |
| ------------------- | ----------------------------------------------------------------------------------------- |
| Person/Worker Graph | What people, employments, assignments, positions, organizations, and relationships exist? |
| BusinessIntent      | What is an authorized actor trying to accomplish or answer?                               |
| Governance          | May it happen, for this purpose, subject, scope, time, and risk?                          |
| Capability          | What typed semantic operation can perform part of it?                                     |
| Workflow            | In what sequence, under which obligations and failure rules, should it happen?            |
| Domain              | Which component owns the business meaning, invariants, and authoritative writes?          |
| Ledger              | What was proposed, decided, attempted, committed, observed, corrected, or superseded?     |
| Reconciliation      | Did observed reality reach the intended outcome?                                          |
| Repair              | How is a partial or incorrect outcome corrected without rewriting history?                |

## BusinessIntent is the behavioral root

`BusinessIntent` means something a human, agent, service, schedule, rule, or
external event wants Human Capital Management Suite to accomplish or answer.

The stable kernel has three families, distinguished by one question: may this
intent cause a material mutation or effect?

```text
BusinessIntent
  |
  +-- ChangeRequest         may mutate or cause effects, directly or via
  |                         explicitly bound child intents
  +-- CalculationRequest    deterministic, pure computation
  `-- AnalyticalRequest     governed query, explanation, or inference
```

Process, filing, batch, and case semantics are attributes of a `ChangeRequest`
definition, not extra families.

Semantic intent names such as `ChangeManager`, `RunPayroll`, or
`AnalyzeTurnover` do not create new runtime classes. They are immutable,
versioned `IntentDefinition`s with typed Protobuf input/output, ownership,
governance, side effects, idempotency, conflict, evidence, reliability, and
outcome contracts.

The catalog is the fourteen definitions that exist in repository source. A
larger intake list of candidate names is kept only as naming vocabulary; it has
no maturity state and no work depends on it. Only definitions that advance
through the maturity lifecycle may be invoked:

```text
DRAFT_CONTRACT -> CONTRACTED -> COMPILED -> PUBLISHED
                                          |
                             DEPRECATED -> RETIRED
```

Each intent carries five lifecycle dimensions and no universal status:
`RequestState`, `ExecutionState`, `BusinessState`, `ConsistencyState`, and
`ObligationState`.

See the [Business Intent Catalog](planning/specs/business-intent-catalog.md) and
the canonical [Protobuf contract](schema/proto/hcmnext/intents/v1/business_intent.proto).

## Native and external execution share one business model

A connector is an execution adapter, not the product model.

Early Manager Change:

```text
ChangeManager
      |
      v
governed workflow
      |
      v
people.manager.change
      |
      v
Workday / UKG connector
```

Later, for a scope where Human Capital Management Suite owns People:

```text
ChangeManager
      |
      v
same governed workflow
      |
      v
people.manager.change
      |
      v
Human Capital Management Suite People domain
```

The semantic capability is stable. Authority resolution changes:

```text
SourceAuthority
  authority: WORKDAY

becomes, after an explicit field/population/domain cutover:

SourceAuthority
  authority: HCM_NEXT
```

Authority is resolved by field, domain, population, organization, jurisdiction,
effective time, and customer—not by a global `system_of_record` flag.

## Composition is recursive, but bounded

One intent can create explicitly related child intents:

```text
HireWorker
  |
  +-- ResolvePersonIdentity
  +-- CreateEmployment
  +-- SetCompensation
  +-- EnrollWorkerInPayroll
  +-- ProvisionWorkerAccess
  +-- AssignOnboardingLearning
  `-- StartOnboarding
```

A child operation may be a separately governed intent when it needs independent
authorization, lifecycle, idempotency, evidence, repair, or outcome tracking. It
may remain a capability call when the parent fully owns those semantics.

This recursion is not arbitrary runtime recursion. The compiled plan requires:

- explicit parent/child relationship and version;
- bounded depth and fan-out;
- typed inputs and results;
- deterministic idempotency and ordering;
- governance at every authority boundary;
- declared cancellation, compensation, and completion propagation;
- cycle rejection unless a bounded loop primitive is explicitly supported.

Parent completion cannot erase or collapse child truth.

## Workflow coordinates; domains own meaning

The workflow kernel understands ten core primitives and three gated structural
ones:

```text
core        CAPABILITY  DECISION  TRANSFORM  OBSERVE  END
            APPROVAL    TASK      WAIT       SIGNAL   COMPENSATE

structural  PARALLEL    JOIN      SUBWORKFLOW        (after P1B evidence)
```

Safe points are a node attribute the compiler places; rules, agents, and
documents are capabilities, not node types.

It does not own salary calculation, tax, worker storage, legal interpretation,
email delivery, vendor API behavior, or AI authority. It invokes typed capabilities
owned by the relevant domain or platform plane.

```text
Customer-specific Promotion workflow
        |
        +-- people.job.change
        +-- positions.reserve
        +-- rewards.compensation.simulate
        +-- budget.compensation.reserve
        +-- regulation.obligations.resolve
        +-- approvals.resolve
        +-- documents.generate
        +-- payroll.worker.sync
        +-- access.entitlements.recalculate
        +-- learning.assign
        +-- communications.send
        `-- reconciliation.verify
```

Capabilities form the vocabulary. Compiled workflows are the grammar customers
use to express how their organization operates.

## Atomic truth and honest partial completion

One parent intent does not imply distributed ACID across SaaS providers.

```text
parent BusinessIntent
        |
        v
authoritative local transaction core
        |
        v
ONE ACID COMMIT
ledger events + critical projections + intent state + outbox
        |
        +--------------+---------------+---------------+
        v              v               v               v
     payroll          access         learning       messaging
        |              |               |               |
        +--------------+---------------+---------------+
                               |
                               v
                     observe + reconcile + repair
```

Completion is multidimensional:

```text
ExecutionState     COMMITTED
BusinessState      COMPLETED
ConsistencyState   DEGRADED       (RepairPlan linked)
ObligationState    SATISFIED
```

If Jane is promoted but one downstream access grant fails, Jane remains promoted.
Human Capital Management Suite records the drift, creates a bounded `RepairPlan`, redrives or corrects
the access effect, observes the result, and closes reconciliation. It does not
rewrite the promotion or claim that everything succeeded.

## Agents interpret and compose; deterministic services execute

AI assistance is not an authority bypass and generic “agentic orchestration” is
not the product moat.

```text
user intent or authorized data
              |
              v
agent discovers visible typed capabilities
              |
              v
typed BusinessIntent / workflow draft / analysis plan
              |
              v
schema + AuthZ + legal + privacy + risk + cost validation
              |
              v
simulation + human decision where required
              |
              v
deterministic capability execution
              |
              v
observation + reconciliation + outcome evaluation
```

Agent inputs may be hostile. Prompt injection controls, semantic taint,
least-privilege data retrieval, tool-use authorization, typed output validation,
budgets, kill switches, and incident handling surround every agent path.

## Product path

Human Capital Management Suite approaches the HCM market through earned authority:

```text
Stage 1  ChangeOps overlay
         observe, simulate, approve, reconcile
              |
Stage 2  workflow operating layer
         controlled cross-system execution and repair
              |
Stage 3  system of transaction
         Human Capital Management Suite owns transaction truth and selected writes
              |
Stage 4  selected system of record
         explicit field/domain/population authority
              |
Stage 5  Workforce Operating System
         native and external domains share one intent architecture
```

Stages 1 and 2 are the product plan. Stages 3 to 5 are options that each
require their own evidence gate, and nothing in the kernel, catalog, planes, or
data models is sized to them.

The initial competitive proposition is not “replace UKG” or “replace Workday.”
It is:

> Safely coordinate workforce change across everything you already run.

The long-term architectural proposition is:

> One semantic runtime for HCM business intent.

Native payroll, deep workforce management, benefits, recruiting, and other mature
suite domains remain separate authority-expansion investments. A box in a diagram
or an intent in the catalog is not a feature-parity claim.

See [Competitive Positioning and Authority Expansion](planning/specs/competitive-positioning-and-authority-expansion.md).

## Phase 1: Promotion and Compensation Change

Phase 1 proves one bounded vertical slice for paid design partners:

```text
manager / HR intent
      -> immutable proposal
      -> source-authority-aware reads
      -> deterministic preflight and simulation
      -> exact approval binding
      -> effective-time conflict control
      -> execution-time revalidation
      -> one governed write path
      -> external observation
      -> reconciliation or RepairPlan
      -> complete evidence
```

Phase 1 does not implement the complete intent catalog, a global payroll engine,
deep WFM, a general integration marketplace, or autonomous write-capable agents.
The authoritative delivery scope is the [Phase 1 Execution Plan](planning/execution-plan.md).

Reference conformance workflows include:

- [Manager Change](planning/reference-workflows/manager-change.md), the smallest useful single-domain mutation;
- [Promote Into Management](planning/reference-workflows/promote-into-management.md), the cross-domain composition fixture;
- the broader [Reference Workflow Suite](planning/reference-workflows/reference-suite.md).

The [HR Workflow Exploration Index](planning/workflows/README.md) extracts the
concrete legacy workflows and models their dependencies, features, steps, data,
failure paths and candidate domain properties. Exploratory workflow documents are
discovery artifacts, not implementation or contractual maturity claims.

## Architecture planes

```text
1. Experience       GWC UI | APIs | SDK | CLI | agent interface
2. Identity         humans | agents | services | federation | sessions
3. Governance       AuthZ | legal | privacy | risk | entitlement | DLP
4. Control          tenants | config | schemas | capabilities | versions
5. Workflow         durable orchestration | human work | timers | repair
6. Domain           People | Workforce | Rewards | Talent | Experience | Access
7. Connectivity     integrations | messaging | files | events | government
8. Data             ledger | artifacts | projections | outbox | serving planes
9. Intelligence     reports | analytics | semantic access | outcomes

Cross-cutting:
   Operations / Assurance
   Billing / Metering
```

The synchronous dependency spine is:

```text
Experience
    -> Identity
    -> Governance
    -> Control resolution
    -> Workflow
    -> Domain capability
    -> authoritative transaction and data truth
```

Connectivity, Intelligence, agents, Operations, and Billing attach to this spine
without becoming universal synchronous dependencies.

## Technology constitution

The product core is Go: services, workflow runtime, domains, data, connectors,
and operations. Three house libraries are the preferred, not mandatory, choice
for their roles, and each must pass a named qualification fixture before it is
a release dependency:

```text
                         Go product core
                              |
          +-------------------+-------------------+
          v                   v                   v
   GWC / GoWebComponents   grpcbridge          SchemaFlux
   preferred UI            preferred edge      preferred generator
   fallback: Go SSR HTML   fallback: grpc-gateway / connect-go
                                               fallback: protoc + Go codegen
          +-------------------+-------------------+
                              |
                              v
                Protobuf/gRPC + PostgreSQL + OpenTelemetry
```

- **Protobuf** is canonical for service and typed payload contracts.
- **PostgreSQL**, OpenTelemetry, object storage, and reviewed open-source
  infrastructure support the core; they do not replace it.
- Node-based developer tooling (browser test runners, formatters) is allowed
  in the development toolchain. It is excluded from the release image and the
  runtime.
- Legacy TypeScript and React code is not extended. It may run beside the Go
  slice during P1A for comparison; its exclusion from the release is a P1B gate.

See the [Go Technology Constitution](planning/specs/go-only-technology-constitution.md).

## Library strategy

Human Capital Management Suite owns intent, capability, governance, workflow, transaction, ledger,
engine, and domain semantics. Go plus GWC, grpcbridge, and SchemaFlux are the
declared core choices for UI, transport edge, and definition generation;
qualification determines which choice is admitted. Libraries supply
replaceable mechanics behind those owned contracts. The inventory below is generated from
the current Go module and architecture manifests; the surrounding guidance in
this README remains human-authored.

<!-- BEGIN GENERATED LIBRARY STRATEGY -->
<!-- This region is generated by tools/gen/librarystrategy. DO NOT EDIT. -->

### Preferred house libraries

| Candidate             | Current status                                                | Named fallback                                       |
| --------------------- | ------------------------------------------------------------- | ---------------------------------------------------- |
| Go                    | PROJECT CORE; product language/toolchain                      | Go standard library                                  |
| GWC / GoWebComponents | QUALIFIED; selected renderer (UX-QUAL-001)                    | Go server-rendered HTML with progressive enhancement |
| grpcbridge            | PREFERRED; not admitted; current edge uses connect-go v1.20.0 | connect-go (v1.20.0)                                 |
| SchemaFlux            | DISQUALIFIED; deterministic fallback selected                 | protoc-style deterministic Go compiler               |

### Package and dependency shape

```text
Go product core (github.com/monstercameron/human-capital-management-suite)
├── package roots
│   ├── internal/application [application; P1A; owner=platform-foundation]
│   ├── internal/authn [trust; P1A; owner=governance-and-trust]
│   ├── internal/kernel [kernel; P1A; owner=platform-foundation]
│   ├── internal/intent [intent; P1A; owner=intent-and-capability]
│   ├── internal/capability [capability; P1A; owner=intent-and-capability]
│   ├── internal/governance [governance; P1A; owner=governance-and-trust]
│   ├── internal/workflow [workflow; P1B; owner=workflow-runtime]
│   ├── internal/engines [engines; P1A; owner=shared-engines]
│   ├── internal/domains [domains; P1A; owner=domain-teams]
│   ├── internal/transaction [transaction; P1B; owner=transaction-and-conflict]
│   ├── internal/ledger [data; P1A; owner=data-and-ledger]
│   ├── internal/data [data; P1A; owner=data-and-ledger]
│   ├── internal/humanwork [transport; P1A; owner=experience-and-transport]
│   ├── internal/connectivity [connectivity; P1A; owner=connectivity]
│   ├── internal/trust [trust; P1A; owner=governance-and-trust]
│   ├── internal/operations [operations; P1A; owner=operations-and-assurance]
│   ├── internal/platform [platform; P1A; owner=platform-foundation]
│   ├── internal/transport [transport; P1A; owner=experience-and-transport]
│   ├── internal/a11y [transport; P1A; owner=experience-and-transport]
│   ├── internal/agentsecurity [trust; P1A; owner=governance-and-trust]
│   ├── internal/commercial [domains; P1A; owner=intent-and-capability]
│   ├── internal/conformance [engines; P1A; owner=shared-engines]
│   ├── internal/contractarchive [data; P1A; owner=data-and-ledger]
│   ├── internal/cryptoagility [trust; P1A; owner=governance-and-trust]
│   ├── internal/customobject [domains; P1A; owner=intent-and-capability]
│   ├── internal/documentextract [engines; P1A; owner=shared-engines]
│   ├── internal/documentredact [trust; P1A; owner=governance-and-trust]
│   ├── internal/documents [data; P1A; owner=data-and-ledger]
│   ├── internal/documentsecurity [trust; P1A; owner=governance-and-trust]
│   ├── internal/effectgraph [platform; P1A; owner=platform-foundation]
│   ├── internal/experience [transport; P1A; owner=experience-and-transport]
│   ├── internal/flow [platform; P1A; owner=platform-foundation]
│   ├── internal/forms [transport; P1A; owner=experience-and-transport]
│   ├── internal/generated [platform; P1A; owner=platform-foundation]
│   ├── internal/i18n [transport; P1A; owner=experience-and-transport]
│   ├── internal/messaging [connectivity; P1A; owner=connectivity]
│   ├── internal/performance [platform; P1A; owner=platform-foundation]
│   ├── internal/replan [platform; P1A; owner=platform-foundation]
│   ├── internal/resource [data; P1A; owner=data-and-ledger]
│   ├── internal/store [platform; P1A; owner=platform-foundation]
│   ├── internal/configuration [platform; deferred; owner=platform-foundation]
│   └── internal/evidence [data; deferred; owner=data-and-ledger]
└── replaceable mechanics (versions and roles below)
```

| Module                                                              | Version                                | Role                      | Direct   | Semantic owner           |
| ------------------------------------------------------------------- | -------------------------------------- | ------------------------- | -------- | ------------------------ |
| `connectrpc.com/connect`                                            | `v1.20.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | experience-and-transport |
| `github.com/BurntSushi/toml`                                        | `v1.4.1-0.20240526193622-a339e1f7089c` | `INFRASTRUCTURE_MECHANIC` | indirect | data-and-ledger          |
| `github.com/cenkalti/backoff/v5`                                    | `v5.0.3`                               | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `github.com/cespare/xxhash/v2`                                      | `v2.3.0`                               | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `github.com/cockroachdb/apd/v3`                                     | `v3.2.3`                               | `INFRASTRUCTURE_MECHANIC` | direct   | kernel                   |
| `github.com/fergusstrange/embedded-postgres`                        | `v1.34.0`                              | `DEV_TEST_ONLY`           | direct   | platform-foundation      |
| `github.com/fxamacker/cbor/v2`                                      | `v2.9.0`                               | `INFRASTRUCTURE_MECHANIC` | indirect | experience-and-transport |
| `github.com/go-logr/logr`                                           | `v1.4.4`                               | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `github.com/go-logr/stdr`                                           | `v1.2.2`                               | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `github.com/google/uuid`                                            | `v1.6.0`                               | `INFRASTRUCTURE_MECHANIC` | direct   | kernel                   |
| `github.com/gorilla/websocket`                                      | `v1.5.3`                               | `INFRASTRUCTURE_MECHANIC` | indirect | experience-and-transport |
| `github.com/grpc-ecosystem/grpc-gateway/v2`                         | `v2.30.0`                              | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `github.com/jackc/pgpassfile`                                       | `v1.0.0`                               | `INFRASTRUCTURE_MECHANIC` | indirect | data-and-ledger          |
| `github.com/jackc/pgservicefile`                                    | `v0.0.0-20240606120523-5a60cdf6a761`   | `INFRASTRUCTURE_MECHANIC` | indirect | data-and-ledger          |
| `github.com/jackc/pgx/v5`                                           | `v5.10.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | data-and-ledger          |
| `github.com/jackc/puddle/v2`                                        | `v2.2.2`                               | `INFRASTRUCTURE_MECHANIC` | indirect | data-and-ledger          |
| `github.com/joho/godotenv`                                          | `v1.5.1`                               | `DEV_TEST_ONLY`           | indirect | platform-foundation      |
| `github.com/lib/pq`                                                 | `v1.10.9`                              | `INFRASTRUCTURE_MECHANIC` | indirect | data-and-ledger          |
| `github.com/mfridman/interpolate`                                   | `v0.0.2`                               | `INFRASTRUCTURE_MECHANIC` | indirect | data-and-ledger          |
| `github.com/monstercameron/GoGRPCBridge`                            | `v1.1.2`                               | `INFRASTRUCTURE_MECHANIC` | direct   | experience-and-transport |
| `github.com/monstercameron/GoWebComponents/v5`                      | `v5.0.1`                               | `INFRASTRUCTURE_MECHANIC` | direct   | experience-and-transport |
| `github.com/monstercameron/schemaflux`                              | `v1.2.0`                               | `DEV_TEST_ONLY`           | direct   | platform-foundation      |
| `github.com/pressly/goose/v3`                                       | `v3.28.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | data-and-ledger          |
| `github.com/rogpeppe/go-internal`                                   | `v1.16.0`                              | `DEV_TEST_ONLY`           | indirect | platform-foundation      |
| `github.com/sashabaranov/go-openai`                                 | `v1.20.4`                              | `DEV_TEST_ONLY`           | indirect | platform-foundation      |
| `github.com/sethvargo/go-retry`                                     | `v0.4.0`                               | `INFRASTRUCTURE_MECHANIC` | indirect | operations-and-assurance |
| `github.com/x448/float16`                                           | `v0.8.4`                               | `INFRASTRUCTURE_MECHANIC` | indirect | experience-and-transport |
| `github.com/xi2/xz`                                                 | `v0.0.0-20171230120015-48954b6210f8`   | `DEV_TEST_ONLY`           | indirect | platform-foundation      |
| `github.com/yuin/goldmark`                                          | `v1.7.13`                              | `INFRASTRUCTURE_MECHANIC` | indirect | experience-and-transport |
| `go.opentelemetry.io/auto/sdk`                                      | `v1.2.1`                               | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `go.opentelemetry.io/otel`                                          | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace`                 | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`   | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `go.opentelemetry.io/otel/exporters/stdout/stdouttrace`             | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `go.opentelemetry.io/otel/metric`                                   | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `go.opentelemetry.io/otel/sdk`                                      | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `go.opentelemetry.io/otel/sdk/metric`                               | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `go.opentelemetry.io/otel/trace`                                    | `v1.46.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `go.opentelemetry.io/proto/otlp`                                    | `v1.11.0`                              | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `go.uber.org/multierr`                                              | `v1.11.0`                              | `INFRASTRUCTURE_MECHANIC` | indirect | operations-and-assurance |
| `golang.org/x/exp/typeparams`                                       | `v0.0.0-20231108232855-2478ac86f678`   | `DEV_TEST_ONLY`           | indirect | platform-foundation      |
| `golang.org/x/mod`                                                  | `v0.38.0`                              | `DEV_TEST_ONLY`           | direct   | platform-foundation      |
| `golang.org/x/net`                                                  | `v0.58.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | experience-and-transport |
| `golang.org/x/sync`                                                 | `v0.22.0`                              | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `golang.org/x/sys`                                                  | `v0.47.0`                              | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `golang.org/x/text`                                                 | `v0.41.0`                              | `INFRASTRUCTURE_MECHANIC` | direct   | experience-and-transport |
| `golang.org/x/tools`                                                | `v0.48.0`                              | `DEV_TEST_ONLY`           | indirect | platform-foundation      |
| `google.golang.org/genproto/googleapis/api`                         | `v0.0.0-20260819154853-08b0e4226688`   | `INFRASTRUCTURE_MECHANIC` | indirect | platform-foundation      |
| `google.golang.org/genproto/googleapis/rpc`                         | `v0.0.0-20260831171406-18b4a7587f8a`   | `INFRASTRUCTURE_MECHANIC` | indirect | experience-and-transport |
| `google.golang.org/grpc`                                            | `v1.83.2`                              | `INFRASTRUCTURE_MECHANIC` | direct   | experience-and-transport |
| `google.golang.org/grpc/cmd/protoc-gen-go-grpc`                     | `v1.6.2`                               | `DEV_TEST_ONLY`           | indirect | experience-and-transport |
| `google.golang.org/protobuf`                                        | `v1.36.12`                             | `INFRASTRUCTURE_MECHANIC` | direct   | experience-and-transport |
| `gopkg.in/yaml.v3`                                                  | `v3.0.1`                               | `INFRASTRUCTURE_MECHANIC` | direct   | platform-foundation      |
| `honnef.co/go/tools`                                                | `v0.8.1`                               | `DEV_TEST_ONLY`           | indirect | platform-foundation      |

### Prohibited semantic frameworks

These families are generated from `definitions/architecture/prohibited-frameworks.yaml`; infrastructure mechanics may not become semantic authority.

| Rule       | Category              | Disposition       | Import prefixes                                                                                        |
| ---------- | --------------------- | ----------------- | ------------------------------------------------------------------------------------------------------ |
| `orm`      | ORM                   | PROHIBITED        | `gorm.io/gorm`, `entgo.io/ent`                                                                         |
| `workflow` | WORKFLOW_ENGINE       | PROHIBITED        | `go.temporal.io/sdk`, `github.com/camunda/camunda-platform`                                            |
| `rules`    | CUSTOMER_RULE_RUNTIME | PROHIBITED        | `go.starlark.net`, `github.com/yuin/gopher-lua`                                                        |
| `broker`   | PHASE1_BROKER         | PHASE1_PROHIBITED | `github.com/IBM/sarama`, `github.com/confluentinc/confluent-kafka-go`, `github.com/segmentio/kafka-go` |
| `provider` | PROVIDER_SDK          | ADAPTER_ONLY      | `cloud.google.com/go`, `github.com/Azure/azure-sdk-for-go`, `github.com/aws/aws-sdk-go-v2`             |

<!-- END GENERATED LIBRARY STRATEGY -->

## Repository map

Current and target material coexist during migration:

```text
planning/
  plan.md                 architecture constitution and long-term direction
  execution-plan.md       bounded Phase 1 delivery gates
  specs/                  focused contracts
  reference-workflows/    conformance scenarios
  workflows/              exploratory per-domain workflow and data inventories

schema/
  proto/                  canonical Protobuf contracts
  schemaflux/             governed structured definitions

src/blocks/go/            current Go fixtures and execution code

src/, package.json
                          legacy implementation evidence unless a planning
                          contract explicitly classifies an artifact otherwise
```

The target Go repository shape is described in the technology constitution. Do
not infer the target architecture from the legacy Node workspace layout.

## Source-of-truth hierarchy

```text
planning/plan.md
  strategy + architecture constitution
        |
        +-- planning/execution-plan.md
        |     delivery gates and acceptance
        |
        +-- planning/next-steps.md
        |     exact P1A / P1B release contents; wins over any spec's
        |     phase table where they disagree
        |
        +-- planning/specs/*.md
        |     owned subsystem contracts
        |
        +-- planning/reference-workflows/*.md
              integration/conformance behavior

schema/proto + schema/schemaflux + migrations + executable tests
  authoritative over prose where implementation exists
```

The adversarial audits under `planning/specs/adversarial-*` are frozen inputs.
No further audit pass is run until P1A executes; findings close by test or by
explicit deferral, not by more contract prose.

Important starting points:

- [High-Level Plan](planning/plan.md)
- [Phase 1 Execution Plan](planning/execution-plan.md)
- [Canonical Platform Plane Model](planning/specs/platform-plane-model.md)
- [Business Intent Kernel](planning/specs/business-intent-and-change-request.md)
- [Business Intent Catalog](planning/specs/business-intent-catalog.md)
- [Workflow Execution Kernel](planning/specs/workflow-runtime.md)
- [Transaction Plan and Commit Coordinator](planning/specs/transaction-plan-and-commit-coordinator.md)
- [Integration Platform](planning/specs/integration-platform.md)
- [Capability Coverage Matrix](planning/specs/platform-capability-coverage-matrix.md)
- [Risk Register](planning/specs/risk-register.md)

## Working in the repository

### Review the production frontend

The development frontend contains no sample-data provider or alternate UI
server. It is a same-origin gateway to a running Human Capital Management Suite cell, including the
authenticated HTML shell, Go/WASM client, and gRPC-over-WebSocket tunnel.
For the normal local loop, start the cell and gateway in two terminals:

```powershell
npm run dev:server
npm run dev:frontend
```

Both commands use the explicit `local-dev` profile. It defaults the cell to
the loopback PostgreSQL URL and HarborCare tenant, skips automatic migrations,
enables the executable promotion plan and development browser admission, and
shortens graceful shutdown to one second. The gateway mints and injects a
short-lived matching credential, eliminating the token-copy/login loop. The
profile is rejected if the cell, database, gateway, or upstream is not on
loopback. It still runs the real credential verifier, authorization policies,
tenant isolation, PostgreSQL stores, gRPC tunnel, and production Go/WASM UI.
Run migrations and seed explicitly when schema or fixtures change, as shown in
the Run the prototype locally section below.

Explicit flags and `HCMNEXT_DATABASE_URL` / `HCMNEXT_DEV_HMAC_KEY` override
the profile defaults. To exercise the manual credential path instead, start
the standard profile, mint a token, and run:

```powershell
$env:HCMNEXT_DEV_BEARER = go run ./cmd/hcmnext token -tenant=harborcare-demo -subject=local-developer -roles=intent_author,comp_admin,promotion_operator -org-scope=org:harborcare-demo:people-ops
go run ./cmd/frontenddev -upstream http://127.0.0.1:8080
```

Open <http://127.0.0.1:8768/workspace/app/home>. The gateway injects the
development bearer only at its upstream boundary and never logs it. The page
loads the production GoWebComponents WASM bundle, which calls
`JourneyService.ListJourneys` and `JourneyService.ListWorkers` through
`/workspace/grpc`. Pages for capabilities the cell has not published show an
explicit unavailable state; they do not substitute fixture records or pretend
to save changes.

### Run the prototype locally

The root prototype is Go-only and requires a reachable PostgreSQL server. It
is non-production software: `hcmnext token` mints development HMAC credentials,
so use a local-only key of at least 32 bytes and never use it for real data.

Set the database URL and development signing key in PowerShell:

```powershell
$env:HCMNEXT_DATABASE_URL = "postgres://postgres:postgres@127.0.0.1:5432/hcm_next?sslmode=disable"
$env:HCMNEXT_DEV_HMAC_KEY = "a-local-development-key-at-least-32-bytes"
```

The seven root commands are `hcmnext` (the API cell), `migrate` (schema and
fixture seed), `projector` (projection reconciliation), `worker` (outbox
consumption), `scheduler` (workflow-frontier scheduling), `hcmctl` (operator
CLI), and `frontenddev` (local frontend development). The `hcm_next` database
must exist before `migrate up` (create it with any PostgreSQL client, e.g.
from a Go one-off using pgx, or an external server; the embedded PostgreSQL
binaries this repo's tests cache ship no `psql` or `createdb`). Start a fresh
database with:

```powershell
go run ./cmd/migrate up
go run ./cmd/migrate seed -tenant=harborcare-demo
```

Run the API cell after migration and seeding:

```powershell
go run ./cmd/hcmnext serve -tenant=harborcare-demo -migrate=false -dev-browser-login=true
```

Without `-execution-authority` the cell is P1A and refuses ExecuteIntent. To enable P1B execution:

```powershell
go run ./cmd/hcmnext serve -tenant=harborcare-demo -migrate=false -dev-browser-login=true -execution-authority=true -execution-authority-digest=sha256:dev-local-demo-authority
```

The digest is carried as evidence and is not verified by the process (cmd/hcmnext/main.go documents this).

Serve publishes the shipped promotion workflow versions as DRAFT and never approves or activates them itself (WF-COMP-006); until a version is activated, ExecuteIntent is refused with a precondition failure naming the release commands. On a local development database, activate them once with the explicit bootstrap, which runs each version's conformance fixtures in-process, approves under the distinct `cmd/hcmnext:dev-release-approver` and activates:

```powershell
go run ./cmd/hcmnext workflow-version bootstrap-dev
```

Outside local development a release is three governed steps: `workflow-version fixtures -digest <d> -out report.json` runs the fixtures and writes the sealed report, `workflow-version approve -digest <d> -approved-by <principal> -authority <ref> -reason <text> -fixture-report report.json` re-runs every declared fixture and records the approval (refused on a failed, missing, digest-mismatched or unreproduced report, and for the publisher itself), and `workflow-version activate -digest <d>` activates on that approval (add `-supersede` to replace a version that is already active). `workflow-version list -workflow <id>` shows the digests and statuses. All of them take `-database-url` or `HCMNEXT_DATABASE_URL`.

It listens on gRPC `127.0.0.1:8443` and HTTP `127.0.0.1:8080` by default. The
Promotion workspace is at <http://127.0.0.1:8080/workspace/promotion>; the
explicit development-login flag enables its local pasted-token form and must
remain off outside local development. Mint a development credential for the
workspace or API with:

```powershell
$token = go run ./cmd/hcmnext token -tenant=harborcare-demo -subject=local-developer -roles=intent_author,comp_admin,promotion_operator -org-scope=org:harborcare-demo:people-ops
```

The three roles are: `intent_author` to author intents in the workspace, `comp_admin` for administrative capability access, and `promotion_operator` for ExecuteIntent when the cell has `-execution-authority=true`. `-org-scope` is required to create intents: the kernel refuses an intent whose initiator carries no organization scope.

The projector and worker are separate long-running processes against the same
database:

```powershell
go run ./cmd/projector
go run ./cmd/worker
go run ./cmd/scheduler
```

Focused smoke checks are:

```powershell
go build ./...
go test -count=1 ./cmd/migrate ./test/bootstrap ./test/serve ./test/workspace
```

The existing Go fixture suite can be run with:

```powershell
go -C .\src\blocks\go test ./...
```

Generating contracts is pinned: run `buf generate` from the repository root.
The `protoc-gen-go` and `protoc-gen-go-grpc` plugins resolve through `go.mod`
tool directives, output lands in `gen/go`, and generated files are checked in.
Do not check in handwritten Go duplicates of Protobuf messages. SchemaFlux YAML
(`schema/schemaflux`) is compiled by `tools/gen/schemaflux`, not by `buf
generate`.

When changing architecture or implementation:

1. Identify the owning plane, domain, capability, and authority boundary.
2. Update the focused contract rather than hiding a responsibility in workflow code.
3. Preserve intent, domain, execution, observation, and outcome truth separately.
4. Add or update a reference/conformance scenario.
5. Keep Phase 1 implementation depth explicit.
6. Use Go and canonical Protobuf contracts; use GWC, grpcbridge, and SchemaFlux
   where they have passed their qualification fixtures, and their named
   fallbacks otherwise.
7. Do not extend the legacy TypeScript/Node runtime.
8. Do not add a lifecycle dimension, kernel family, workflow primitive, or
   coordination layer without a scope exchange recorded in the execution plan.

## License

This repository is MIT licensed. See [LICENSE](LICENSE) (Copyright (c) 2026
Earl Cameron).
