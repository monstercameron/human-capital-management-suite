# Business intent definitions

This directory is the SchemaFlux source for governed semantic intent metadata.
Protobuf remains authoritative for wire request/result types. These YAML files
are intended to relate those types to capabilities, governance, evidence,
reliability, and execution semantics. The deterministic HCM SchemaFlux tooling
lives in `tools/gen/schemaflux`: `LoadDefinitions` loads these files, the
generator emits the Go registry (`RegistrySource`), catalog Markdown
(`CatalogMarkdown`) and fixtures JSON (`FixturesJSON`), and
`CrossCheckCompiled` checks the compiled output against
`internal/intent/definitions`.

Rules:

- one immutable `(intent_type_id, version)` definition;
- one stable catalog number, never reused;
- no JavaScript/TypeScript generator or runtime;
- no published definition with unresolved references;
- no defaulting of security, side effects, cancellation, or failure behavior;
- one file per catalog partition as it becomes contracted;
- catalogued-only entries are not invocable.

The source-to-Protobuf adapter must implement and test this mapping rather than
relying on coincidental YAML field names:

| YAML source                  | `IntentDefinition` target                        |
| ---------------------------- | ------------------------------------------------ |
| `intent_type_id` + `version` | `reference.intent_type_id` + `reference.version` |
| `input_schema_ref`           | parsed `input_schema` descriptor reference       |
| `result_schema_ref`          | parsed `result_schema` descriptor reference      |
| `required_capabilities`      | `required_capability_refs`                       |
| `governance_requirements`    | `governance_requirement_refs`                    |
| `preconditions`              | `precondition_refs`                              |
| `invariants`                 | `invariant_refs`                                 |
| `idempotency_scope`          | resolved `idempotency_policy_ref`                |
| `conflict_footprint_rule`    | `conflict_footprint_rule_ref`                    |
| `proposal_binding_rule`      | `proposal_binding_rule_ref`                      |
| `revalidation_rule`          | `revalidation_rule_ref`                          |
| `cancellation_rule`          | `cancellation_rule_ref`                          |
| `compensation_rule`          | `compensation_rule_ref`                          |
| `evidence_rule`              | `evidence_rule_ref`                              |
| `outcome_contract`           | `outcome_contract_ref`                           |
| `availability_policy`        | `availability_policy_ref`                        |

Every symbolic reference resolves through a typed registry. Unknown source fields,
lossy mappings and unresolved descriptors/capabilities/rules are compilation
errors and keep the definition below `CONTRACTED`.

`promotion.yaml` is the first draft-contract candidate partition. All current
definitions remain `DRAFT_CONTRACT` until their field mapping, Protobuf request
and result descriptors, capabilities, rules, tests and generated artifacts resolve.
The remaining catalog partitions stay planning backlog until their owning domain
is funded.
