// Package pilotblueprint implements CUSTOMER-001: a versioned, reusable
// design-partner implementation blueprint and RACI/responsibility matrix
// covering the pilot lifecycle discovery through hypercare. See blueprint.go
// for the schema; this file records why the checked-in blueprint is a
// template with no confirmed tenant facts, and why that is correct rather
// than incomplete.
//
// # There is no design partner
//
// WEDGE-001 selected a problem (a Promotion/Compensation-change failure
// class), never a partner (see tools/planning/pilotprovider's doc.go, which
// documents the identical gap for SELECT-002's provider topology). No named
// customer, customer contact, or customer capability evidence exists
// anywhere in this repository prior to this package, and this package does
// not invent one. Fabricating a design-partner name, executive sponsor, or
// "discovery complete" signal into a signed governance artifact would be
// exactly the defect RED names: "an assumed customer capability reports
// ready".
//
// # Two things this package deliberately keeps separate
//
// [Blueprint] is the reusable TEMPLATE: an ordered set of ten
// [Workstream] entries (discovery through hypercare, [AllWorkstreamKinds]),
// each carrying role TITLES (not named humans), relative day OFFSETS (not
// real dates), and described procedures (acceptance oracle, data-processing
// boundary, escalation, fallback). Every field [Blueprint.Validate] checks
// is satisfiable by a generic template with zero tenant facts, exactly the
// way tools/planning/pilotprovider's checked-in topology is a structurally
// complete PLACEHOLDER_UNVERIFIED topology rather than an invalid one.
//
// [Instantiate] is the per-tenant READINESS ASSESSMENT (REFACTOR:
// "instantiate the reusable blueprint per tenant"). It takes a Blueprint
// together with one tenant's [CustomerFacts], one
// tools/planning/pilotprovider.ProviderTopology and one
// tools/planning/pilotjurisdiction.JurisdictionProfile, and returns a
// [ReadinessReport] naming, per workstream, whether that workstream is
// [ReadinessReady], [ReadinessUnknown] (stale evidence) or [ReadinessBlocked]
// (missing evidence). It never mutates the Blueprint it is given -
// refactor_test.go proves this by re-hashing the same Blueprint value's
// CanonicalDigest after several Instantiate calls with different tenant
// facts - so customer-specific facts can never rewrite a workstream's Kind,
// Owners, Prerequisites, artifacts, AcceptanceOracle, DataProcessingBoundary,
// Escalation or Fallback: the platform's semantic contract for what each
// workstream requires. A tenant instantiation binds real values into the
// template's already-declared slots; it can never add, remove or redefine a
// slot.
//
// # Structurally incapable of reporting ready
//
// ReadinessReport carries no field a caller could set to fake completion:
// [WorkstreamReadiness] has exactly Kind, Status and Reasons, and Status is
// entirely computed by [Instantiate] from the CustomerFacts, ProviderTopology
// and JurisdictionProfile it is given - there is no boolean flag anywhere in
// this schema whose value alone flips a workstream to READY, mirroring the
// way tools/planning/pilotprovider.ProviderTopology.SatisfiesRealProviderSelectionGate
// is deliberately independent of the SelectionStatus flag. Instantiating the
// checked-in Blueprint against today's real repository state - the zero
// value of CustomerFacts (no design partner confirmed anywhere), the real
// checked-in select-002-provider-topology.yaml (a structurally-marked
// placeholder that fails SatisfiesRealProviderSelectionGate by value) and
// the real checked-in select-001-jurisdiction-profile.yaml (an honestly
// empty reviewer.name) - reports BLOCKED for every workstream, naming the
// exact missing customer and provider inputs. instantiate_test.go's
// TestInstantiateAgainstRealRepositoryStateReportsBlockedNamingMissingCustomerAndProviderInputs
// proves this against the live files, not a fixture.
//
// # Binding to what already exists
//
// [Blueprint.ProviderTopologyRef] and [Blueprint.JurisdictionProfileRef] pin
// definitions/planning/gates/select-002-provider-topology.yaml and
// definitions/planning/gates/select-001-jurisdiction-profile.yaml by path;
// [Instantiate]'s provider-side and legal-review checks call
// tools/planning/pilotprovider.ProviderTopology.SatisfiesRealProviderSelectionGate
// and read tools/planning/pilotjurisdiction.JurisdictionProfile.Reviewer
// directly rather than re-deriving a second opinion about provider or
// jurisdiction readiness. This package never treats SELECT-002's placeholder
// vendor as confirmed: a workstream's provider dependency is BLOCKED for as
// long as SatisfiesRealProviderSelectionGate reports false, by value, not by
// reading SelectionStatus alone.
//
// # Signing
//
// [Blueprint.CanonicalDigest] hashes every field except the signature
// itself, following the same JSON-projection approach
// tools/planning/pilotprovider.ProviderTopology.CanonicalDigest and
// tools/planning/pilotjurisdiction.JurisdictionProfile.CanonicalDigest use.
// Signing and verification reuse tools/planning/gateevidence.SignDigest and
// tools/planning/gateevidence.VerifyDigestSignature directly, using the same
// repository-local development key fixture at
// tools/planning/gateevidence/testdata/dev-signing-key.yaml.
//
// # What REFACTOR's contract-isolation proof cannot do yet
//
// refactor_test.go's digest-invariance proof and its reflection-based check
// that [ReadinessReport] and [WorkstreamReadiness] carry no semantic-contract
// field (only identity, status and free-text reasons) are the concrete,
// falsifiable form of REFACTOR available today. What it cannot do is what
// tools/planning/pilotprovider's ScanForIdentityLeak does for provider
// identity: scan a real internal/ consumer package for a hard-coded branch
// on a specific tenant's CustomerFacts.DesignPartnerName. No such consumer
// exists yet - nothing in internal/ reads a Blueprint or a ReadinessReport,
// because no onboarding orchestrator has been built (that is CUSTOMER-002's
// and CUSTOMER-003's territory, both already evidenced elsewhere in
// planning/todos.md). Once one exists, it should get the same
// textual-reference scan pilotprovider uses, over that consumer's package,
// for the literal tenant name a real CustomerFacts.DesignPartnerName would
// carry.
package pilotblueprint
