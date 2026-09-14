// Package pilotcommercial implements COMMERCIAL-001's signed pilot
// commercial package. See freeze.go's package comment for the schema; this
// file records why the checked-in freeze promises exactly what it promises
// and nothing more.
//
// # What landed today, and why it bounds this freeze
//
// COMMERCIAL-001 depends on PHASE-001 and SELECT-002 (also WEDGE-007 and
// WEDGE-009, both already closed). All three of the artifacts this package
// binds to are real and checked in:
//
//   - definitions/planning/gates/phase1-scope-ceiling.yaml (PHASE-001) names
//     four selection slots - provider, jurisdiction, topology, slo - and
//     every one carries filled: false. Its own security test proves a
//     quietly-filled slot invalidates the signature. Notably, the slo slot's
//     filling_todo_id is "SELECT-002, COMMERCIAL-001": this todo is named as
//     one of the slot's joint fillers, and PHASE-001's REFACTOR clause
//     ("selection cannot depend on a supposedly final manifest that already
//     contains the selection") means COMMERCIAL-001 does not fill that slot
//     by editing the ceiling - it fills it, honestly, by promising nothing
//     against it.
//   - definitions/planning/gates/select-001-jurisdiction-profile.yaml
//     (SELECT-001) selects California but reports exactly one violation,
//     reviewer.name: missing, because no qualified legal reviewer exists.
//     Its review_status is UNREVIEWED.
//   - definitions/planning/gates/select-002-provider-topology.yaml
//     (SELECT-002) is an enforced placeholder: vendor_id carries the
//     mandatory "-PLACEHOLDER-UNVERIFIED" suffix, and
//     SatisfiesRealProviderSelectionGate is deliberately independent of
//     Validate so flipping a status flag alone cannot make it pass.
//
// Given that, the only honest package promises the California jurisdiction
// subject to its unreviewed status, promises no provider-dependent
// entitlement beyond what a placeholder can back, and promises no SLO at
// all. Every one of those three promises is expressed in this schema as a
// PromiseStatus field (JurisdictionPromise.Status,
// ProviderPromise.Status, and the closed-to-NONE
// EvidenceAndSLO.SLOStatus) and is cross-checked against the live artifacts
// above by derive.go's ConformsToLiveRegistries, not asserted once and left
// to drift - see conformance_test.go's TestTodo_COMMERCIAL_001_Conformance,
// the load-bearing test.
//
// # Pricing is a declared hypothesis, never a rate card
//
// COMMERCIAL-001's own title calls out "the pricing hypothesis", and
// plan.md#125-commercial-architecture-hypotheses frames the whole
// commercial architecture as hypotheses to be tested, not settled facts.
// PricingHypothesis.IsHypothesis is a required-true field precisely so RED's
// "hides ... hypothesis" clause has a concrete, Validate-enforced signal
// rather than relying on a reader's inference from a price range. No real
// customer, invoice or signed rate card is asserted anywhere in this
// package or the checked-in YAML.
//
// # Consuming internal/commercial rather than restating it
//
// internal/commercial (PERSIST-COMMERCIAL-001's runtime package) already
// carries a machine-readable P1A commercial ceiling -
// DefaultPilotCommercialPackage's entitlements, pricing hypothesis,
// authority boundary and exit terms. REFACTOR requires "commercial views
// consume entitlement, usage, cost and release registries; contract prose
// cannot independently enable capability", so this freeze's Intent,
// Pricing, Authority and Exit sections are declared as data but verified as
// a live derivation: derive.go's diffEntitlements/diffPricing/
// diffAuthority/diffExit compare the checked-in YAML's values against a
// fresh call to commercial.DefaultPilotCommercialPackage() on every test
// run. If a future edit to the YAML promises an entitlement, price,
// authority claim or exit term the registry does not also grant, the
// CONFORMANCE test fails naming exactly which field diverged - contract
// prose cannot silently drift ahead of the registry that is supposed to
// back it.
//
// A known, out-of-scope inconsistency is recorded here rather than hidden:
// internal/commercial.P1AManifestDigest is a hardcoded constant that no
// longer matches gateevidence.LoadP1AManifest's live CanonicalDigest for
// definitions/planning/gates/p1a-manifest.yaml as of this writing. Fixing
// that digest is PERSIST-COMMERCIAL-001/internal/commercial's problem, not
// this todo's - internal/commercial is out of this lane's edit scope - so
// this freeze binds its own "Phase 1 digest" (GREEN: "maps entitlements and
// exclusions to the Phase 1 digest") to PHASE-001's scope-ceiling digest
// (IntentPromise.ScopeCeilingDigest, cross-checked live against
// phase1-scope-ceiling.yaml), which is the artifact COMMERCIAL-001's own
// Refs and this todo's dependency graph actually name as "Phase 1", and
// which this lane can verify directly rather than trust a possibly-stale
// constant in a package it cannot edit.
//
// # Replay and repair are never billed
//
// RED specifically names "bills replay or repair duplicates" as a failure
// condition. BillingPolicy declares the policy (ReplayBillable and
// RepairBillable are both closed to false); billing.go's
// ComputeBillableTotal enforces it against real events, not merely a
// checked box: a REPLAY or REPAIR event is skipped outright regardless of
// its idempotency key, and a duplicate NEW submission for an
// already-charged transaction is not billed twice either.
// TestReplayAndRepairEventsAreNeverBilled constructs a fixture that
// genuinely contains one of each and proves the total charged reflects only
// the two distinct new transactions.
//
// # Overlay authority vs. system-of-record ownership
//
// RED's "confuses overlay authority with system-of-record ownership" is
// enforced structurally in AuthorityBoundary, mirroring
// internal/commercial.AuthorityBoundary's own EXTERNAL_OBSERVATION topology
// with WriteAuthority: false and an empty OwnedDomains: Validate rejects any
// domain claimed as both owned and observed, rejects any owned domain when
// write_authority is false, and rejects EXTERNAL_OBSERVATION topology
// carrying write_authority or owned domains at all. The checked-in freeze
// carries zero owned domains and seven observed domains, exactly mirroring
// the live commercial registry's own claim that the pilot never becomes
// system of record for anything it touches.
package pilotcommercial
