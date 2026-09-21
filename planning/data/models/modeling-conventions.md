# Modeling Conventions and Shared Value Objects

## Universal entity envelope

Every aggregate, revision, relationship, control artifact and material evidence
record explicitly carries the applicable subset of:

```text
EntityEnvelope
  entity_id
  entity_kind
  tenant_id
  owning_organization_scope_ref
  cell_id
  placement_epoch
  lifecycle_state
  revision
  effective_interval
  recorded_at
  known_at
  source_authority_ref
  authority_epoch
  schema_ref
  classification_ref
  purpose_constraints[]
  retention_policy_ref
  legal_hold_keys[]
  provenance_ref
  correction_of_ref?
  supersedes_ref?
  created_by_principal_ref
  created_by_transaction_ref
```

Not every value object repeats this envelope. The owning aggregate/revision binds
its values. Cross-tenant identity is never inferred from an ID alone.

## Temporal primitives

```text
EffectiveInterval
  start_inclusive
  end_exclusive?
  semantic_kind = INSTANT | LOCAL_DATE | PAY_PERIOD | REPORTING_PERIOD
  timezone_id?
  tzdb_version?
  disambiguation = REJECT_GAP | EARLIER | LATER | EXPLICIT_OFFSET

TemporalPoint
  effective_at
  known_at
  recorded_at
  observed_at?
  received_at?
  policy_as_of

Deadline
  trigger_ref
  clock_start_basis
  due_expression
  authoritative_timezone
  tzdb_version
  business_calendar_ref/version
  cutoff_time?
  extension/tolling/grace refs[]
  resolved_due_at
  satisfaction_mode
```

Half-open intervals are canonical. Runtime timestamps never substitute for
business-effective or legally applicable time.

## Identity and references

```text
EntityRef
  tenant_id
  entity_kind
  entity_id
  revision_or_as_of?

ExternalObjectRef
  tenant_id
  connection_id
  external_account_id
  connector_version
  object_type
  external_id
  effective_interval

NaturalKeyClaim
  namespace
  normalized_value_hash
  issuer/source
  assurance
  effective_interval
```

External identifiers are scoped and may be reused. They never become canonical
IDs without an effective-dated linkage decision.

## Money, quantity and rate

```text
Money
  amount_decimal
  currency_code
  rounding_policy_ref

Rate
  amount_decimal
  currency_code?
  unit
  frequency
  basis
  denominator?

Quantity
  value_decimal
  unit_code

Percentage
  value_decimal
  scale
  rounding_policy_ref

ExchangeRateObservation
  source
  pair
  rate_decimal
  rate_type
  effective_at
  observed_at
  source_version
```

Material financial values never use binary floating point.

## Address, location and jurisdiction

```text
PostalAddress
  lines[]
  locality
  administrative_area
  postal_code
  country_code
  script
  normalized_address_ref?
  validation_status

GeoPoint
  latitude_decimal
  longitude_decimal
  accuracy_meters?
  source
  observed_at

LocationRef
  location_id
  address_revision_ref
  timezone_id
  jurisdiction_boundary_refs[]

JurisdictionAssertion
  role
  domain
  jurisdiction_ref
  activity_segment_ref?
  effective_interval
  source_authority_ref
  evidence_refs[]
  confidence
  status
```

Locale and jurisdiction remain distinct.

## Names and communication values

```text
LocalizedText
  language_tag
  script
  direction
  text
  translation_source_ref?
  approved_equivalence_ref?

StructuredName
  given_names[]
  middle_names[]
  family_names[]
  prefixes[]
  suffixes[]
  local_full_name
  latin_full_name?
  script
  ordering_rule

ContactEndpointValue
  channel
  normalized_address_or_provider_ref
  verification_state
  business_or_personal
  allowed_purposes[]
  classification_limit
```

## Classification, authority and provenance

```text
DataClassification
  class = PUBLIC | INTERNAL | CONFIDENTIAL | RESTRICTED | HIGHLY_RESTRICTED
  categories[]
  compartment_refs[]
  residency_constraints[]

AuthorityBinding
  resource/field scope
  effective_interval
  mastering_mode = LOCAL_MASTER | EXTERNAL_MASTER | SHARED_FIELD
  domain_fact_owner
  permitted_writer
  policy_fingerprint
  activation_epoch
  promotion_or_merge_rule

ProvenanceLink
  source_ref
  relation = ASSERTED_BY | DERIVED_FROM | OBSERVED_FROM | CORRECTS |
             SUPERSEDES | DECIDED_BY | PRODUCED_BY | SENT_TO
  field_paths[]
  transformation_ref?
  confidence?
```

An observation may be authoritative evidence of what a source reported without
being authoritative for the underlying business fact.

## State and correction rules

- Mutable business meaning is represented by immutable revisions/facts plus an
  aggregate lifecycle, not silent in-place history edits.
- Corrections append new assertions and preserve the original statement/event.
- Merge and separation never erase identity lineage.
- Deletion/disposition may remove payloads while retaining minimized evidence or
  tombstones under a separate retention rule.
- Projections, search, analytics, vectors and caches are reconstructable and do
  not acquire authority through replication.

## Tenant data models at scale

Status: Gate C contract (todos `WF-DATA-001`–`WF-DATA-035`). Tenants define their own
record types and build small systems from them. The same metamodel defines
product features, so a gap that blocks a client also blocks the product.

### One metamodel, generic primitives

A tenant model is data, never code or DDL. It is built from a closed set of
declarative primitives. Each primitive is generic: it is defined once and
applies to any type or relationship, never to one use case.

```text
types + relationships     objects, fields, typed links, self-links (trees)
derived fields            bounded expression over a record and its links
path resolution           value resolved along a relationship path under a
                          policy: nearest wins | min | max | accumulate | locked
rollups                   registered reducers over related records
constraints               conditional required, scoped uniqueness, cross-record
lifecycles                declared states and guarded transitions
access rules              owner, scope and relationship based record policy
automation                record and schedule events start workflows
presentation              generated list, detail and form pages, governed reports
packages                  versioned bundles of all of the above
```

Org cascades, location defaults, headcount rollups, custody trackers and
expiry-driven renewals are compositions of these, not features.

### Definition compile

Publishing a definition, or a package of definitions, compiles a dependency
graph across derived fields, resolutions, rollups, constraints and lifecycles.
Compilation rejects:

- cycles between definitions;
- expressions or paths above their cost bounds;
- hierarchies without a depth limit;
- a rollup or resolution whose worst-case fan-out exceeds the tenant's limits
  without being declared asynchronous.

The compiled definition is pinned by version and digest exactly as a workflow
plan is, and workflows reference it by that pin.

### Storage without per-tenant DDL

```text
custom_record_revision      append-only truth, hash-partitioned by tenant
custom_record_current       REBUILDABLE current-state projection
custom_field_index          REBUILDABLE typed index rows
                            (field key, text | number | date | reference value,
                            record, effective interval) under generic indexes
hierarchy_closure           REBUILDABLE effective-dated ancestor rows
                            per relationship type
resolved_value, rollup      REBUILDABLE projections with watermarks
```

- The platform never creates a column, table or expression index for one
  tenant.
- A tenant's searchable fields become rows in the generic typed index, and
  queries may filter or sort only on those.
- A very large tenant may be placed on its own partition or cell. Its schema
  does not change.

### Propagation, freshness and stability

- A change that fans out, such as a value set at the root of a 20,000-unit
  tree or a reorg that re-parents a division, is never recomputed inside the
  writing transaction. The write commits one fact. An asynchronous,
  resumable, throttled propagation job updates the projections from the
  ledger and advances their watermarks. It reports progress and can be
  paused.
- Readers declare the freshness they need: a maximum age or a required
  watermark, the same fields workflow context requirements already carry.
  A DECISION reads a pinned snapshot, so propagation in progress cannot flip
  a route mid-run.
- A new derived field, rollup or index is backfilled before use. It stays
  unusable by workflows and screens until its backfill reports `READY`.
- Record events feed workflow triggers through coalescing and rate limits. A
  change whose preview would start more runs than the tenant threshold needs
  an explicit confirmation, and a circuit breaker stops runaway triggering.
- Each tenant has budgets for expression evaluation, propagation throughput
  and trigger rate. A definition that repeatedly breaches them is
  quarantined, and existing pinned consumers keep their prior version.
- Projection lag, propagation backlog, resolution latency and trigger rate
  are observable per tenant and per definition, with SLOs.

### Proof

A large-company fixture (at least 500,000 workers, 20,000 organization units,
five hierarchies and 5,000,000 custom records) is a release benchmark.

Three product features ship as metadata packages with no feature-specific Go
code: organization cascading attributes, asset custody, and training
requirements with expiry and renewal. They are the acceptance test that the
primitives are sufficient.
