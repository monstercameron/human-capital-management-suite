# Capability Registry, Manifest, and Lifecycle Contract

The Capability Registry owns semantic operation identity, manifests, versions,
implementation/transport bindings, publication, activation, adoption, quarantine,
deprecation, and retirement. A route, Go function, workflow node, or agent tool is
not a capability until an active manifest binds it.

## State and Manifest

```text
CapabilityIdentity
CapabilityManifest
CapabilityVersion
ImplementationBinding
TransportBinding
CapabilityOwnership
CapabilityPublication
CapabilityActivationReceipt
CapabilityDeprecation
CapabilityQuarantine
```

```text
DRAFT -> VALIDATED -> REVIEWED -> PUBLISHED -> ACTIVE
       -> DEPRECATED -> RETIRED
Any published/active version may become QUARANTINED.
```

A manifest has a required core and optional extensions. The core is what the
gateway needs to authorize and invoke safely:

```text
core (required from the first endpoint)
  capability id + version
  owner domain
  request / response / error schema refs
  side-effect profile
  read and write data domains and field sets
  risk class
  idempotency policy
  agent eligibility (default: not eligible)

extensions (added when a consumer needs them)
  consistency/freshness, obligations, execution modes, bulk/population/cost
  limits, SLO class, implementation build/provenance, transport bindings,
  dependencies, retention/evidence policy, conformance results
```

An extension is added to the manifest schema only when a real consumer reads
it. Nothing may require an extension before that consumer exists.

## Bootstrap Profile

The registry must never be the reason the first endpoint cannot ship. Two
profiles exist:

```text
BOOTSTRAP (P1A and P1B)
  the registry is a compiled-in Go table generated from Protobuf service
  annotations at build time; the build is the publication; the binary's
  digest is the snapshot fingerprint; no separate publish/activate flow,
  no signed control bundle, no distribution

MANAGED (Gate C and later)
  the full propose / validate / review / publish / activate lifecycle,
  signed Control Bundle distribution, quarantine SLA, and adoption tracking
```

Under `BOOTSTRAP`, the workflow compiler, gateway, UI action binder, and agent
gateway resolve the same compiled table, so the single-source rule still holds.
The lifecycle states below describe the `MANAGED` profile.

## APIs and Runtime Resolution

```text
capabilities.propose|validate|review|publish|activate
capabilities.quarantine|deprecate|retire
capabilities.read|search|resolve|explain|adoption
capabilities.snapshots.status
```

Capability ID/version is globally unique within platform namespace; customer
extensions use tenant-owned namespaces. Duplicate/conflicting identities fail
publication. Schema, domain owner, implementation build, transport, policy
requirements, and tests must resolve before validation. Manifest and dependency
digest are signed and distributed in the Control Bundle.

Workflow compiler, grpcbridge edge, agent tool gateway, UI action binder, SDK
generator, billing meter, and runtime resolve the same signed locally applied
snapshot. Unknown, inactive, quarantined, retired, incompatible, or unadopted
versions fail closed. Deprecation does not stop existing pinned workflows until
their declared support window; retirement requires dependency/adoption proof or
an approved migration.

## Authority Classes and Connector Bindings

A capability is a semantic contract; who performs it is resolved per tenant.
The product is an overlay on incumbent systems in its first stages, so many
operations a workflow needs are computed by a payroll provider, carrier,
screening vendor or government service, not by this platform. One contract
serves both cases, which is how authority can later move from an incumbent to
a native domain without changing any workflow:

```text
NATIVE        the owning Go domain computes and commits
              (hire commit, assignment revision, absence commit,
               adverse-impact analysis, retro period resolution)

DELEGATED     an incumbent or vendor computes; the platform dispatches,
              correlates the callback, observes and reconciles
              (final pay and tax, benefit continuation administration,
               carrier enrollment feeds, background screening,
               employment eligibility verification)

RULE-PACK     timing and thresholds come from a signed legal rule pack,
PARAMETERIZED never from code (mass-layoff notice, release consideration
              and revocation periods, leave notices, final-pay deadlines)
```

- The authority class is a manifest extension. Its first consumer is the
  workflow compiler, which needs it to decide whether a node commits or
  dispatches and observes.
- The `ImplementationBinding` resolves through the tenant's `SourceAuthority`
  for the field, population and jurisdiction.
- A `DELEGATED` binding names a connector-binding registry entry. That entry
  declares the dispatch operation, callback correlation, polling fallback, SLA
  and quarantine policy (`WF-EXT-022`).
- A delegated capability never fabricates the result it is waiting for.
  Until the external system answers, the run reports the obligation as open.

Registration covers every package that exposes a callable operation, not only
`internal/domains`. The messaging and document planes (`internal/messaging`,
`internal/documents`) publish `messaging.*` and `documents.*` capabilities so
the notify and document fragments can bind to them (`WF-EXT-015`). The
capability bundles still missing are tracked as `WF-CAP-001`–`WF-CAP-019`.

## Security, Failure, and Evidence

Domain owner proposes semantic/effect metadata; schema/security/privacy/operations
review their owned fields; publisher and runtime activation executor are separate.
No implementation may self-assert lower risk, narrower writes, safer side effects,
or agent eligibility. Quarantine/kill propagation has a measured SLA, prevents new
invocations, and handles in-flight work by declared safe-point policy.

Evidence records manifest/digest/signature, ownership, validations/reviews,
schemas/dependencies/build/transports, publication/activation receipts, runtime
snapshot, consumers/adoption, invocations by version, quarantine, deprecation,
migration and retirement. P1A and P1B run the `BOOTSTRAP` profile with the
core manifest fields; the `MANAGED` profile, signed bundles, and quarantine
tests are Gate C.
