# Human Capital Management Suite Repository Architecture

Generated from the checked-in architecture manifests and the current Go package tree.

- Module: `github.com/monstercameron/human-capital-management-suite`
- Source graph: f17174653e531eea9ec7bb0bab5571788d9f483ce81f4c76fe14d85cea62a8f6
- Package count: 883
- Within-module edge count: 2316
- Source manifests: `definitions/architecture/repository-layout.yaml`, `definitions/architecture/package-dependency-policy.yaml`, `definitions/architecture/dependency-roles.yaml`

## Declared layers and roots

| Layer | Declared root | Owner | Phase | Purpose |
| --- | --- | --- | --- | --- |
| application | `internal/application` | platform-foundation | P1A | Single explicit application composition root (ARCH-GO-020). Builds registries, governance, workflows, engines, domains, ports/adapters and worker roles for one process role from a validated Config value, and exposes the Start/Stop lifecycle cmd/* invokes. |
| trust | `internal/authn` | governance-and-trust | P1A | Authentication adapters: the tenant federation issuer registry (AUTHN-001) and the federation registry that binds issuers to the internal/trust/federation port. |
| kernel | `internal/kernel` | platform-foundation | P1A | Identity/reference/revision, temporal, decimal/money, digest and evidence primitives. |
| intent | `internal/intent` | intent-and-capability | P1A | BusinessIntent definitions, instances, families, lifecycle, proposals/change requests. |
| capability | `internal/capability` | intent-and-capability | P1A | Capability descriptors, modes, side-effect declarations, handlers/gateway/discovery. |
| governance | `internal/governance` | governance-and-trust | P1A | AuthZ/legal/privacy/entitlement/risk/DLP composition over independent policy subsystems. |
| workflow | `internal/workflow` | workflow-runtime | P1B | Workflow definition, compiler, durable runtime and thin step adapters. |
| engines | `internal/engines` | shared-engines | P1A | Reusable transform/rules/population/eligibility/etc. engines independent of domains. |
| domains | `internal/domains` | domain-teams | P1A | HCM domain packages (people, organization, position, compensation, ...). |
| transaction | `internal/transaction` | transaction-and-conflict | P1B | Transaction plan/participants/prepare/commit/receipt and conflict analysis (port/adapter). |
| data | `internal/ledger` | data-and-ledger | P1A | Append-only ledger event truth (port/adapter). |
| data | `internal/data` | data-and-ledger | P1A | Repositories, projections, outbox and other serving/distribution stores (port/adapter). |
| transport | `internal/humanwork` | experience-and-transport | P1A | Production Go/WASM user interface (workspace shell, productui pages and components, uicomponents); the served surface in the README "Review the production frontend" section. |
| connectivity | `internal/connectivity` | connectivity | P1A | System integration connectors, APIs, webhooks, files, government gateways. |
| trust | `internal/trust` | governance-and-trust | P1A | Identity/session/federation/workload-identity primitives. |
| operations | `internal/operations` | operations-and-assurance | P1A | Telemetry, SLOs, reconciliation, incidents, integrity, DR overlays. |
| platform | `internal/platform` | platform-foundation | P1A | Process bootstrap, build identity, config, telemetry and reliability plumbing shared by commands. |
| transport | `internal/transport` | experience-and-transport | P1A | Experience/API transport adaptation (grpcbridge or fallback edge, protocol exposure). |
| transport | `internal/a11y` | experience-and-transport | P1A | Versioned assistive-technology, browser, locale and input-mode compatibility matrix (A11Y-001) the product UI qualification consumes. |
| trust | `internal/agentsecurity` | governance-and-trust | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| domains | `internal/commercial` | intent-and-capability | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| engines | `internal/conformance` | shared-engines | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| data | `internal/contractarchive` | data-and-ledger | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| trust | `internal/cryptoagility` | governance-and-trust | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| domains | `internal/customobject` | intent-and-capability | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| engines | `internal/documentextract` | shared-engines | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| trust | `internal/documentredact` | governance-and-trust | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| data | `internal/documents` | data-and-ledger | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| trust | `internal/documentsecurity` | governance-and-trust | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| platform | `internal/effectgraph` | platform-foundation | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| transport | `internal/experience` | experience-and-transport | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| platform | `internal/flow` | platform-foundation | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| transport | `internal/forms` | experience-and-transport | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| platform | `internal/generated` | platform-foundation | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| transport | `internal/i18n` | experience-and-transport | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| connectivity | `internal/messaging` | connectivity | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| platform | `internal/performance` | platform-foundation | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| platform | `internal/replan` | platform-foundation | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| data | `internal/resource` | data-and-ledger | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| platform | `internal/store` | platform-foundation | P1A | Declared on 2026-09-06 when the repository-layout policy joined the pre-commit gates; the package existed and was in use before its root was written down here. |
| platform | `internal/configuration` | platform-foundation | deferred | Configuration-semantics contracts (ARCH-GO-024); declared on 2026-09-10 when CI first ran the layout gate past the quality gate and reported the missing root. Deferred beyond P1A scope; no domain, workflow or store dependencies. |
| data | `internal/evidence` | data-and-ledger | deferred | Closed-intent execution receipts (EVIDENCE-001); declared on 2026-09-10 when CI first ran the layout gate past the quality gate and reported the missing root. Deferred beyond P1A scope; digests point at owner records, never a second ledger. |

## Allowed dependency edges

Ranked layers may depend on the same layer or a lower-ranked layer. Port packages are allowed dependencies for business layers; concrete adapters remain behind their ports.

| Importing layer | Allowed ranked layers | Port roots |
| --- | --- | --- |
| kernel | kernel | none |
| engines | kernel, engines | none |
| domains | kernel, engines, domains | `internal/transaction`, `internal/ledger`, `internal/data` |
| capabilities | kernel, engines, domains, capabilities | `internal/transaction`, `internal/ledger`, `internal/data` |
| workflow | kernel, engines, domains, capabilities, workflow | `internal/transaction`, `internal/ledger`, `internal/data` |
| transport | kernel, engines, domains, capabilities, workflow, transport | none |

Forbidden edge rules are evaluated by `tools/policy/depedge`: `kernel-must-not-import-upward`, `engine-must-not-import-domain-implementation`, `workflow-must-not-import-domain-persistence`, `transport-must-not-import-store`, `business-must-not-import-concrete-adapter`.

## Library firewall roots

Third-party modules are admitted only at the owning roots declared by `dependency-roles.yaml`. Empty root lists mean the module is not expected to be imported directly.

| Library selector | Role | Allowed import roots |
| --- | --- | --- |
| `golang.org/x/` | INFRASTRUCTURE_MECHANIC | none |
| `google.golang.org/` | INFRASTRUCTURE_MECHANIC | none |
| `go.opentelemetry.io/` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel`, `internal/transport/otelmw`, `internal/connectivity/providertelemetry` |
| `github.com/cockroachdb/apd/v3` | INFRASTRUCTURE_MECHANIC | `internal/kernel` |
| `github.com/fergusstrange/embedded-postgres` | DEV_TEST_ONLY | `test`, `tools`, `internal/data/pgtest` |
| `github.com/google/uuid` | INFRASTRUCTURE_MECHANIC | `internal/kernel`, `internal/intent`, `internal/ledger`, `internal/data`, `internal/connectivity`, `internal/transaction`, `internal/humanwork`, `internal/workflow`, `internal/operations/explorer`, `internal/resource`, `internal/operations/reconcile`, `internal/engines/wire/digest`, `internal/application`, `internal/domains/leave`, `internal/domains/promotion`, `internal/platform/devclock`, `internal/platform/execution`, `internal/transport`, `tools/uxqual/journeyclient`, `cmd`, `test` |
| `github.com/jackc/pgx/v5` | INFRASTRUCTURE_MECHANIC | `internal/data`, `internal/ledger`, `migrations`, `internal/platform/bootstrap`, `cmd/hcmnext`, `cmd/migrate` |
| `github.com/lib/pq` | INFRASTRUCTURE_MECHANIC | `internal/data`, `internal/ledger`, `migrations` |
| `github.com/pressly/goose/v3` | INFRASTRUCTURE_MECHANIC | `migrations`, `cmd`, `internal/data/pgtest`, `internal/data/schema` |
| `github.com/sethvargo/go-retry` | INFRASTRUCTURE_MECHANIC | `internal/operations`, `internal/connectivity`, `internal/data` |
| `github.com/xi2/xz` | DEV_TEST_ONLY | `test`, `tools` |
| `golang.org/x/exp/typeparams` | DEV_TEST_ONLY | `tools` |
| `golang.org/x/mod` | DEV_TEST_ONLY | `tools`, `internal/workflow/version` |
| `golang.org/x/net` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `tools/uxqual/forms`, `tools/uxqual/qual`, `tools/uxqual/hydration`, `tools/uxqual/wcag` |
| `golang.org/x/sync` | INFRASTRUCTURE_MECHANIC | `internal/platform`, `internal/operations`, `internal/workflow` |
| `golang.org/x/text` | INFRASTRUCTURE_MECHANIC | `internal/engines/wire/canonical`, `internal/kernel/values`, `internal/intent`, `internal/domains/people`, `internal/experience/i18n`, `internal/humanwork/productui`, `internal/i18n`, `tools/uxqual/forms` |
| `golang.org/x/tools` | DEV_TEST_ONLY | `tools` |
| `google.golang.org/genproto/googleapis/rpc` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `gen` |
| `connectrpc.com/connect` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `cmd` |
| `github.com/monstercameron/GoGRPCBridge` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `cmd`, `tools/uxqual/journeyclient`, `tools/uxqual/cmd/journeywasm`, `tools/uxqual/productclient` |
| `github.com/monstercameron/GoWebComponents/v5` | INFRASTRUCTURE_MECHANIC | `tools/uxqual`, `internal/humanwork/productui`, `internal/humanwork/uicomponents`, `internal/humanwork/workspace` |
| `github.com/monstercameron/schemaflux` | DEV_TEST_ONLY | `tools/gen` |
| `go.opentelemetry.io/otel/sdk/metric` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel`, `internal/transport/otelmw` |
| `go.opentelemetry.io/otel` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel`, `internal/transport/otelmw`, `internal/connectivity/providertelemetry` |
| `go.opentelemetry.io/otel/trace` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel`, `internal/transport/otelmw`, `internal/connectivity/providertelemetry`, `internal/intent/app` |
| `go.opentelemetry.io/otel/metric` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel` |
| `go.opentelemetry.io/otel/sdk` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel` |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel` |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel` |
| `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` | INFRASTRUCTURE_MECHANIC | `internal/platform/telemetry/otel` |
| `google.golang.org/grpc` | INFRASTRUCTURE_MECHANIC | `internal/transport`, `gen`, `tools/gen`, `cmd`, `tools/uxqual/journeyclient`, `tools/uxqual/cmd/journeywasm`, `tools/uxqual/productclient` |
| `google.golang.org/grpc/cmd/protoc-gen-go-grpc` | DEV_TEST_ONLY | `tools` |
| `google.golang.org/protobuf` | INFRASTRUCTURE_MECHANIC | `gen`, `internal/transport`, `internal/intent/protomap`, `internal/engines/wire`, `tools/gen`, `tools/uxqual/journeyclient`, `tools/uxqual/cmd/journeywasm`, `tools/uxqual/productclient`, `tools/quality/bufprotovalidatekit` |
| `gopkg.in/yaml.v3` | INFRASTRUCTURE_MECHANIC | `tools`, `internal/data/tenancy/storagedisposition`, `internal/platform/telemetry`, `internal/transport/eastwest` |
| `honnef.co/go/tools` | DEV_TEST_ONLY | `tools` |

## Package inventory by declared root

### `cmd`

Composition-root command binaries. Every second-level directory name must be an approved command from approved_commands.initial.

- `github.com/monstercameron/human-capital-management-suite/cmd/frontenddev`
- `github.com/monstercameron/human-capital-management-suite/cmd/hcmctl`
- `github.com/monstercameron/human-capital-management-suite/cmd/hcmnext`
- `github.com/monstercameron/human-capital-management-suite/cmd/migrate`
- `github.com/monstercameron/human-capital-management-suite/cmd/projector`
- `github.com/monstercameron/human-capital-management-suite/cmd/scheduler`
- `github.com/monstercameron/human-capital-management-suite/cmd/worker`

### `api`

Reserved for API/service composition surfaces. May be empty until a phase requires a distinct package here; internal/transport is the current home for transport adaptation.

_No Go packages currently scanned._

### `definitions`

Machine-readable architecture, planning and policy manifests. Never executable; consumed by tools/policy and tools/planning.

_No Go packages currently scanned._

### `internal`

Semantic package roots. Every second-level directory name must be a declared root in internal_package_roots.

- `github.com/monstercameron/human-capital-management-suite/internal/a11y`
- `github.com/monstercameron/human-capital-management-suite/internal/agentsecurity`
- `github.com/monstercameron/human-capital-management-suite/internal/application`
- `github.com/monstercameron/human-capital-management-suite/internal/authn`
- `github.com/monstercameron/human-capital-management-suite/internal/authn/enterprisegate`
- `github.com/monstercameron/human-capital-management-suite/internal/authn/federation`
- `github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry`
- `github.com/monstercameron/human-capital-management-suite/internal/authn/oidc`
- `github.com/monstercameron/human-capital-management-suite/internal/authn/outage`
- `github.com/monstercameron/human-capital-management-suite/internal/authn/subjectlink`
- `github.com/monstercameron/human-capital-management-suite/internal/capability`
- `github.com/monstercameron/human-capital-management-suite/internal/capability/binding`
- `github.com/monstercameron/human-capital-management-suite/internal/commercial`
- `github.com/monstercameron/human-capital-management-suite/internal/configuration`
- `github.com/monstercameron/human-capital-management-suite/internal/conformance/selectedjurisdiction`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/adapter`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/adapter/connrt001`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/application`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/artifactstore`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/diagnostics`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/edge`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/health`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/iac`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/iamsim`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/mapping`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/mapping/execute`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/mappingprofile`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/marketdata`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/mft`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/oauthcc`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe/adapters/postgres`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding/readiness`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/payrollsim`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/providercontract`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerdelivery`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerdrift`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerexit`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry/providerwire`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/retrypolicy`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot/adapters/postgres`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot/diff`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi/spiconform`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/syncjob`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/transport`
- `github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook`
- `github.com/monstercameron/human-capital-management-suite/internal/contractarchive`
- `github.com/monstercameron/human-capital-management-suite/internal/cryptoagility`
- `github.com/monstercameron/human-capital-management-suite/internal/customobject`
- `github.com/monstercameron/human-capital-management-suite/internal/data`
- `github.com/monstercameron/human-capital-management-suite/internal/data/accessstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/admissionstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/aggregates`
- `github.com/monstercameron/human-capital-management-suite/internal/data/analytics`
- `github.com/monstercameron/human-capital-management-suite/internal/data/artifacts`
- `github.com/monstercameron/human-capital-management-suite/internal/data/assetstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/assurancemeta`
- `github.com/monstercameron/human-capital-management-suite/internal/data/attestationstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/balancestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/bandfacts`
- `github.com/monstercameron/human-capital-management-suite/internal/data/benefitsstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal`
- `github.com/monstercameron/human-capital-management-suite/internal/data/budgetfacts`
- `github.com/monstercameron/human-capital-management-suite/internal/data/budgetstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/careerstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/cbastore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/commercialstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/committedfacts`
- `github.com/monstercameron/human-capital-management-suite/internal/data/compfacts`
- `github.com/monstercameron/human-capital-management-suite/internal/data/configregistry`
- `github.com/monstercameron/human-capital-management-suite/internal/data/conflictstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/connectivityopstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/contactstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/contentregistrystore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/crmstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/customstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/dbport`
- `github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce`
- `github.com/monstercameron/human-capital-management-suite/internal/data/documentmeta`
- `github.com/monstercameron/human-capital-management-suite/internal/data/employeerelationsstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/equitystore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/evidencestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/fxstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/governance`
- `github.com/monstercameron/human-capital-management-suite/internal/data/health`
- `github.com/monstercameron/human-capital-management-suite/internal/data/hrcasestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/identityprivacystore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/inboundmsg`
- `github.com/monstercameron/human-capital-management-suite/internal/data/inbox`
- `github.com/monstercameron/human-capital-management-suite/internal/data/incentivestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/integration`
- `github.com/monstercameron/human-capital-management-suite/internal/data/integrationmeta`
- `github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol`
- `github.com/monstercameron/human-capital-management-suite/internal/data/jobarchstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/jobs`
- `github.com/monstercameron/human-capital-management-suite/internal/data/leavestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger/commit`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger/disposition`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger/partition`
- `github.com/monstercameron/human-capital-management-suite/internal/data/ledger/temporal`
- `github.com/monstercameron/human-capital-management-suite/internal/data/legalevidencestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/lineage`
- `github.com/monstercameron/human-capital-management-suite/internal/data/locationstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/meritstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/messagingmeta`
- `github.com/monstercameron/human-capital-management-suite/internal/data/mobilitystore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/operationstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/operatorjournal`
- `github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta`
- `github.com/monstercameron/human-capital-management-suite/internal/data/orgfacts`
- `github.com/monstercameron/human-capital-management-suite/internal/data/outbox`
- `github.com/monstercameron/human-capital-management-suite/internal/data/partition`
- `github.com/monstercameron/human-capital-management-suite/internal/data/payglstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/payinputstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/paymethodstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/payrollstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/performancestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/pgtest`
- `github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter`
- `github.com/monstercameron/human-capital-management-suite/internal/data/planningstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts`
- `github.com/monstercameron/human-capital-management-suite/internal/data/positionguard`
- `github.com/monstercameron/human-capital-management-suite/internal/data/positionstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/preferencestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/privacymeta`
- `github.com/monstercameron/human-capital-management-suite/internal/data/productdurability`
- `github.com/monstercameron/human-capital-management-suite/internal/data/projection`
- `github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical`
- `github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget`
- `github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit`
- `github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard`
- `github.com/monstercameron/human-capital-management-suite/internal/data/promotioninvalidation`
- `github.com/monstercameron/human-capital-management-suite/internal/data/promotionladder`
- `github.com/monstercameron/human-capital-management-suite/internal/data/provenance`
- `github.com/monstercameron/human-capital-management-suite/internal/data/providerreceipts`
- `github.com/monstercameron/human-capital-management-suite/internal/data/queryplans`
- `github.com/monstercameron/human-capital-management-suite/internal/data/rebuild`
- `github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta`
- `github.com/monstercameron/human-capital-management-suite/internal/data/refdata`
- `github.com/monstercameron/human-capital-management-suite/internal/data/repairrecord`
- `github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate`
- `github.com/monstercameron/human-capital-management-suite/internal/data/safetystore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/schedulingstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/schema`
- `github.com/monstercameron/human-capital-management-suite/internal/data/search`
- `github.com/monstercameron/human-capital-management-suite/internal/data/seed`
- `github.com/monstercameron/human-capital-management-suite/internal/data/shadow`
- `github.com/monstercameron/human-capital-management-suite/internal/data/signals`
- `github.com/monstercameron/human-capital-management-suite/internal/data/skillstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/store`
- `github.com/monstercameron/human-capital-management-suite/internal/data/subscriptionstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/successionstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/surveystore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/taxprofilestore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/tenancy`
- `github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition`
- `github.com/monstercameron/human-capital-management-suite/internal/data/tenantstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/truststore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/uow`
- `github.com/monstercameron/human-capital-management-suite/internal/data/wakeup`
- `github.com/monstercameron/human-capital-management-suite/internal/data/workeridstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/workflowdraftstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore`
- `github.com/monstercameron/human-capital-management-suite/internal/data/workforce`
- `github.com/monstercameron/human-capital-management-suite/internal/documentextract`
- `github.com/monstercameron/human-capital-management-suite/internal/documentredact`
- `github.com/monstercameron/human-capital-management-suite/internal/documents/evidence`
- `github.com/monstercameron/human-capital-management-suite/internal/documents/intake`
- `github.com/monstercameron/human-capital-management-suite/internal/documents/signing`
- `github.com/monstercameron/human-capital-management-suite/internal/documents/template`
- `github.com/monstercameron/human-capital-management-suite/internal/documentsecurity`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/access`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/appointment`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/asset`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/attendance`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/attestation`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/audience`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/availability`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/balance`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/benefits`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/budget`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/career`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/cba`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/clock`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/compensation`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/contact`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/crm`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/custom`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/dataops`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/demand`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/employeerelations`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/equity`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/evidence`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/fx`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/garnishment`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/headcount`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/hrcase`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/incentive`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/industrypack`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/jobarch`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/knowledge`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/labor`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/learning`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/leave`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/location`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/matching`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/merit`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/mobility`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/org`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/organization`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/paygl`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/payinput`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/achrisk`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/safeguards`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/payroll`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/auditpack`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/calcpolicy`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/correction`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/filing`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/paymentprofile`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/people`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/performance`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/position`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/privacy/dsr`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/program`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/eligibility`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/localcommit`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/positionpicker`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcomp`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/trace`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/proofing`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/pseudonym`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/qualification`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/recruiting`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/refdata`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/repair`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/rewards`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/safety`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/scenario`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/service`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/settlement`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/skill`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/subscription`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/succession`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/survey`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/taxprofile`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/tenant`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/tenant/govauth`
- `github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle`
- `github.com/monstercameron/human-capital-management-suite/internal/effectgraph`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/abuse`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/abuse/anomaly002`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/cycle`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/docextract`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/docredact`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/effectivedate`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/envelope`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/fielddiff`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/messagetemplate`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/payband`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/popscale`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/population`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/readiness`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/replan`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/rules`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/schedule`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/search`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/adapters`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/conformance`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/exec`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/lineage`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/propagation`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/runtime`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/taint`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/version`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/wire/canonical`
- `github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest`
- `github.com/monstercameron/human-capital-management-suite/internal/evidence`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/adoption`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/channelparity`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/continuity`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/disposition`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/draftflow`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/flowmigration`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/i18n`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/i18nparity`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/localize`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/outcome`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/participants`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/preferences`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/presentation`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/recovery`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/reporting`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/reportrender`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/reportschedule`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/status`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/userflow`
- `github.com/monstercameron/human-capital-management-suite/internal/experience/workerids`
- `github.com/monstercameron/human-capital-management-suite/internal/flow`
- `github.com/monstercameron/human-capital-management-suite/internal/forms/drafts`
- `github.com/monstercameron/human-capital-management-suite/internal/generated/schemaflux`
- `github.com/monstercameron/human-capital-management-suite/internal/governance`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/authority`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/decision`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/exit`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/gateb`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/attribution`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/carveouts`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/extract`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/indexation`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/payrules`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/pipeline`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/reciprocity`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/researchgaps`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legal/stateparams`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/legalhold`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/masking`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/pilot`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/privacy`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/assessment`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/dispatch`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/hipaa`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/transfer`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/records`
- `github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/formcontinuity`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/formdraft`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/profilephoto`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/reasontext`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/sla`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem`
- `github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace`
- `github.com/monstercameron/human-capital-management-suite/internal/i18n`
- `github.com/monstercameron/human-capital-management-suite/internal/intent`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/analysis`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/app`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/approval`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/definitions`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/draftstore`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/eventpolicy`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/evolution`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/model`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/model/deferred`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/modelbinding`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/operator`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/protomap`
- `github.com/monstercameron/human-capital-management-suite/internal/intent/surface`
- `github.com/monstercameron/human-capital-management-suite/internal/kernel/values`
- `github.com/monstercameron/human-capital-management-suite/internal/ledger`
- `github.com/monstercameron/human-capital-management-suite/internal/messaging`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/accessdrift`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/admin`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/connectorconfig`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/diagnosticsession`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/evidenceexport`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/incidentrepair`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/admission`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/adversarial`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/advisory`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/assurance`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/authorizedhealth`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/authzsim`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/drain`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/explorer`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/export`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/inspector`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/internal/viewdigest`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/onboardingruns`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/productcorrelation`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/recovery`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/recovery/opcmd`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/releaseevidence`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/reliability`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/repair`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/repairworkbench`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/residency`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/slo`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/subprocessor`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/synthetic`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/telemetryhealth`
- `github.com/monstercameron/human-capital-management-suite/internal/operations/wedge`
- `github.com/monstercameron/human-capital-management-suite/internal/performance`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/archive`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/cache`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/config`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/config/promotion`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/config/revalidation`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/database`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/devclock`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/diagnostics`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/execution`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/identitybinding`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/logging`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/observability`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/sandbox`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/schemaupgrade`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/backends`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/boundary`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/correlation`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/diagnostic`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/lifecycle`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel/testexport`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/queue`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/testexport`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/timeauth`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/topology`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/versionexplain`
- `github.com/monstercameron/human-capital-management-suite/internal/platform/workload`
- `github.com/monstercameron/human-capital-management-suite/internal/replan`
- `github.com/monstercameron/human-capital-management-suite/internal/resource/reservation`
- `github.com/monstercameron/human-capital-management-suite/internal/store/object`
- `github.com/monstercameron/human-capital-management-suite/internal/store/object/aws`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/cancel`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/commit`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/conformance`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/coordinator`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/correction`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/plan`
- `github.com/monstercameron/human-capital-management-suite/internal/transaction/recovery`
- `github.com/monstercameron/human-capital-management-suite/internal/transport`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/admin`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/admin/hcmctl`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/cell`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/clients`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/conformance`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/eastwest`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/edge`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/envelope`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/evidence`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/health`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/journey`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/list`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/manifest`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/operations`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/otelmw`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/productquery`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/queryenvelope`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/rpcpolicy`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/streaming`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest`
- `github.com/monstercameron/human-capital-management-suite/internal/transport/workflow`
- `github.com/monstercameron/human-capital-management-suite/internal/trust`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/accessreview`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/adversarial`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/attest`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/authz`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/bootstrap`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/breakglass`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/bundle`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/confidentialactor`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/consent`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/content`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/cryptoagile`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/custody`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/custody/kmsadapter`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/dataclass`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/dlp`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/envelope`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/federation`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/jit`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/lease`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/outage`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/outbound`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/pentest`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/pseudonym`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/secrets`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/session`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/session/pgstore`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/sod`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/stepup`
- `github.com/monstercameron/human-capital-management-suite/internal/trust/workload`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/benefits`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/bulkack`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/edges`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/hrcase`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/jurisdiction`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/learning`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/leave`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/leavereturn`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/managerchange`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/mobility`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/payroll`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/program`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/recruit`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/talent`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/termination`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/time`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/transfer`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/triage`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/execute`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/intervention`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/lease`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate/artifacts`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/migrationpreview`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/modeling`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/observe`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/parallel`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/progress`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionhiperf`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/quarantine`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/recover`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/replay`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/shadow`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/compensate`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/subworkflow`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/timer`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/version`
- `github.com/monstercameron/human-capital-management-suite/internal/workflow/workload`

### `migrations`

Authoritative Goose migration tree (owned outside tools/policy).

- `github.com/monstercameron/human-capital-management-suite/migrations`

### `test`

Cross-package conformance, integration and browser test suites that do not belong to a single internal package.

- `github.com/monstercameron/human-capital-management-suite/test/acceptance`
- `github.com/monstercameron/human-capital-management-suite/test/bootstrap`
- `github.com/monstercameron/human-capital-management-suite/test/edge`
- `github.com/monstercameron/human-capital-management-suite/test/serve`
- `github.com/monstercameron/human-capital-management-suite/test/tenant`
- `github.com/monstercameron/human-capital-management-suite/test/trustabuse`
- `github.com/monstercameron/human-capital-management-suite/test/tunnel`
- `github.com/monstercameron/human-capital-management-suite/test/workflow`
- `github.com/monstercameron/human-capital-management-suite/test/workspace`

### `tools`

Developer and CI tooling. Excluded from the release image; internal structure is not constrained by this manifest beyond being under tools/.

- `github.com/monstercameron/human-capital-management-suite/tools/conformance`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/checks`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/discover`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/intentdefinitions`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/internal/reporoot`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/model`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/parse`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/recruit`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/report`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/runner`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/transformationvectors`
- `github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab`
- `github.com/monstercameron/human-capital-management-suite/tools/gen`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/archdoc`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/archdoc/cmd/archdoc`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/architecturedoc`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/clients`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/clients/cmd/generateclients`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/compatibility`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/connectorsdk`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/contracts`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/dbdeferred`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/deferredschema`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/librarystrategy`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/librarystrategy/cmd/generatelibrarystrategy`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/modelgen`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/modelgen/cmd/modelgen`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/openapi`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/openapi/cmd/openapigen`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/bindingcheck`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/cmd/modelgen`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/drift`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/modelgen`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/schemafluxsql`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest`
- `github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest/cmd/storagemanifest`
- `github.com/monstercameron/human-capital-management-suite/tools/integrationsim/cmd/iamsim`
- `github.com/monstercameron/human-capital-management-suite/tools/integrationsim/cmd/payrollsim`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/atomicity`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/authoritygate`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/boundarytests`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/closurewitness`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/convergence`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/featurecoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/p1aselection`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/pilotblueprint`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/pilotcommercial`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/pilotjurisdiction`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/pilotprovider`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/plancheck`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/productslice`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/scopeceiling`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/threatregister`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/todogovernance`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/todoregistry`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/wedge`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/cmd/workflowmaturity`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/controlcrosswalk`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/controlcrosswalk/cmd/controlcrosswalk`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/convergence`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/corpus`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/corpus/cmd/corpus`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/coveragematrix`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/dbcoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/deferredimports`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/dependencygraph`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/depthvocab`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/designclosure`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/designownership`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/directcapability`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/docfix`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/docintegrity`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/evidence`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/federalbaseline`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence/selectionbind`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/iac`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage/cmd/intentcoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/legalmatrix`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/lineageconformance`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/links`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/manifest`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/obligations`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/obligations/cmd/obligations`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/operations`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/oraclespecificity`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/oraclestrength`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/performance`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/pilotblueprint`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/pilotcommercial`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/plancontradiction`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/productslice`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/progress`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/researchquestions`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/riskbinding`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/riskbinding/cmd/riskbinding`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/rolloutplan`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/scenariomatrix`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/scopeexchange`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/scopefidelity`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/securebydesign`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/tddcontract`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/terminology`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/threatmodel`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/threatmodel/cmd/threatmodel`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/todogovernance`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/traceability`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/userflowgaps`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/wedge`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowarchetypes`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdecisions`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign/cmd/workflowdesign`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesignjoin`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowexpansion`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowmaturity`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowpromotion`
- `github.com/monstercameron/human-capital-management-suite/tools/planning/workflowregistry`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/apigate`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/apigate/cmd/apigate`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/archrules`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/cleancheckout`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/cleancheckout/cmd/cleancheckout`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/crosscut`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/defaultactivation`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/defaultproduct`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/depadmission`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/depadmission/cmd/depadmission`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/depedge`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/depmanifest`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/dispositioncoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/dispositioncoverage/cmd/dispositioncoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/dispositionrebuild`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/docintegrity`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/docintegrity/cmd/docintegrity`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/driftgate`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/driftgate/cmd/driftgate`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/endpointmanifest`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/endpointmanifest/cmd/endpointmanifest`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/enginecoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/enginecoverage/cmd/enginecoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/engineownership`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/fkindex`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/garbagedrawer`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/gensources`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/iac`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/iacdrift`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/iacrecovery`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/iacstack`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/importgraph`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/invocationpath`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/layout`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/libqualification`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/migrationci`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/migrationci/cmd/migrationci`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/mutationpolicy`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/mutationpolicy/cmd/mutationpolicy`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/oraclestrength`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/oraclestrength/cmd/oraclestrength`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/phaseone`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/phaseonegate`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/placementbindings`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/processroles`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/prohibitedframework`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/provenance`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/provenance/cmd/provgen`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/querytransport`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/racepolicy`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/racepolicy/cmd/racepolicy`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/regexhoist`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/release`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/release/cmd/release`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/releaseadmission`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/rlsparity`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/rowbatch`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/runtimedecision`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/sbom`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/sbom/cmd/sbomgen`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/storageproduct`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/storeboundaries`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/storeprivacy`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/substratecoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/substratecoverage/cmd/substratecoverage`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/tableinventory`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/tableownership`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/testhygiene`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/testlayout`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/vulnimpact`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/webdelivery`
- `github.com/monstercameron/human-capital-management-suite/tools/policy/workspace`
- `github.com/monstercameron/human-capital-management-suite/tools/quality`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/bufprotovalidatekit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/buildverify`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/celqual`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/cicd`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/compositionroot`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/configboundaries`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/cosignkit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/covergate`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/covergate/cmd/covergate`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/decomposition`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/definitionscontract`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/ephemeralenv`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/fuzzkit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/integrationboundaries`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/oidckit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/ownershipboundaries`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/phaseonepackages`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/provenance`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/racecheck`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/rapidkit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/releaseboundary`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/sbom`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/sqlckit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/storagearch`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/synctestkit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/testcontainerskit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/thintransport`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/toolinventory`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/toxiproxykit`
- `github.com/monstercameron/human-capital-management-suite/tools/quality/xtextkit`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/cmd/genfixtures`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/cmd/journeywasm`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/cmd/uxqualwasm`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/contract`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/floorplan`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/hydration`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/i18n`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/invalidation`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/presentation`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/page`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/workspace`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/ssrshell`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/vpat`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcagtest`
- `github.com/monstercameron/human-capital-management-suite/tools/uxqual/widgetreg`

### `gen`

Generated Protobuf/Go artifacts. Content is owned by the generator (TOOL-002/TOOL-004), never hand-edited; internal structure is not constrained by this manifest.

- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/capabilities/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/model`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1`
- `github.com/monstercameron/human-capital-management-suite/gen/wire`

## Document digest

`889bbfb0799305a7c378a0fba99fbabe022b084cdbf1355e0ade7065db55f9ef`
