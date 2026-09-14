// Package threatregister implements THREAT-001's signed trust-boundary
// threat register for every Phase 1 vertical slice
// (definitions/planning/gates/threat-001-register.yaml). See register.go
// for the schema; this file records the design decisions the todo's
// REFACTOR clause and "what matters most" guidance call out specifically.
//
// # What "every Phase 1 vertical slice" means today
//
// definitions/planning/product-slices.yaml (ALIGN-001, regenerated from the
// live Phase 1 package closure PHASE-001's
// definitions/planning/gates/phase1-scope-ceiling.yaml bounds) names
// exactly one admitted slice: "promotion". This register therefore carries
// exactly one Slice entry today. conformance_test.go cross-checks the
// register's slice_id set against the live product-slices.yaml registry
// rather than a hardcoded literal, so a future second slice would fail
// conformance until this register is extended to cover it - the register
// cannot silently fall behind the real slice catalog. It also cross-checks
// the promotion slice's three business intents
// (hcmnext.people.promote_worker/v1,
// hcmnext.rewards.simulate_compensation/v1,
// hcmnext.rewards.evaluate_pay_band_position/v1) against
// phase1-scope-ceiling.yaml's own INCLUDE dispositions, per the todo's
// instruction to enumerate against that ceiling rather than inventing a
// different slice set.
//
// # What a "slice graph edge" is
//
// GREEN requires the register "maps each slice graph edge to reviewed
// threats and controls". This package defines a slice graph edge as one
// concrete (actor, entry point, trust boundary, asset) tuple -
// [SliceEdge] - naming exactly one way one kind of actor can reach one
// asset, through one entry point, across one trust boundary. This is
// deliberately the finest edge grain that still names every RED element in
// one place: coarser (actor-to-asset only) would hide which entry
// point/boundary a threat actually crosses; finer (splitting entry point
// and boundary into separate edge types) would make [Validate]'s edge-
// coverage check ("every slice graph edge is not mapped to any reviewed
// threat") ambiguous about what "an edge" even is. [Register.Validate]
// requires every edge to be named by at least one Threat.ConsumingEdges
// entry - an edge nobody threat-modeled is exactly the gap GREEN's mapping
// requirement exists to catch.
//
// # Threat identity: stable and deduplicated by asset + boundary + attack
//
// REFACTOR requires "threat identities are stable and deduplicated by
// asset+boundary+attack". [ThreatIdentity] is that identity function: a
// threat's free-text ID field (e.g. "THR-07") is a human-readable label,
// never the identity Validate deduplicates by. Two Threat entries in the
// same slice that name the same (asset, trust_boundary, attack_class)
// triple - even under different IDs or different prose scenarios - are the
// same threat recorded twice, and [Register.Validate] reports it as a
// duplicate. mutation_test.go and refactor_test.go each construct a fixture
// with a genuine duplicate (same asset/boundary/attack, different ID and
// scenario text) to prove this fires; the real checked-in register has no
// duplicates, so this specific fixture could not be built from live data -
// see refactor_test.go's comment for why the synthetic fixture is the only
// way to exercise this branch honestly.
//
// # Shared mitigations retain every consuming edge
//
// REFACTOR also requires "shared mitigations retain every consuming slice
// edge". When more than one Threat names the same Mitigation.ID,
// [Register.Validate] computes the union of every one of those threats'
// ConsumingEdges and requires the Mitigation's own ConsumingEdges field to
// equal that union exactly - not a subset (that would silently lose an
// edge the mitigation is claimed to cover) and not a superset (that would
// claim coverage no threat actually attributes to it). The checked-in
// register exercises this for real: MIT-REVALIDATE-BEFORE-COMMIT is named
// by both THR-01 (STALE_AUTHORIZATION, edge EDGE-01) and THR-06 (REPLAY,
// edge EDGE-06), and its consuming_edges field lists both. refactor_test.go
// additionally builds a synthetic fixture where a shared mitigation's
// declared edge list is deliberately truncated, proving Validate catches
// the narrower "shared mitigation silently dropped a consuming edge on
// edit" regression even when the untruncated case never occurs in the real
// file.
//
// # Named attack classes are covered totally, driven off the taxonomy
//
// [AllAttackClasses] is the single source of truth for RED's nine named
// attack classes. [Register.Validate] and property_test.go's coverage
// assertions iterate this function - never a second hardcoded list of nine
// strings - so the taxonomy and its enforcement cannot drift apart the way
// a hand-copied list eventually does. The checked-in register's single
// "promotion" slice covers all nine classes exactly once each.
//
// # Honest incompleteness: one residual risk with no accountable owner
//
// RED requires every accepted residual risk to name accountable acceptance
// and an expiry. THR-07 (AMBIGUOUS_EFFECT on ExecuteJourney's timeout
// path) has no mitigation today: Phase 1 has not yet implemented an
// idempotency-key-bound observe-before-retry lookup for the domain
// capability that would let a caller resolve a mid-flight timeout instead
// of guessing. Inventing a mitigation that does not exist, or inventing an
// accepting owner nobody has actually designated, would both be fabricated
// evidence in a signed governance artifact - the same failure mode
// tools/planning/pilotjurisdiction's doc.go describes for its empty
// reviewer.name and tools/planning/pilotprovider's doc.go describes for its
// placeholder vendor identity. Instead, the checked-in register carries a
// [ResidualRisk] entry for THR-07 with a real justification and a real
// expiry, but an honestly empty AcceptedBy: nobody has been designated to
// accept this risk yet. [Register.Validate] therefore reports exactly one
// violation on the checked-in file
// (slices[0].residual_risks[0].accepted_by: missing ...), proved by
// register_test.go's PRIMARY test - every other RED/GREEN/REFACTOR element
// validates clean.
//
// # Release blocking is a separate, stronger gate than Validate
//
// [Register.ReleaseDecision] is deliberately independent of Validate,
// mirroring
// tools/planning/pilotprovider.ProviderTopology.SatisfiesRealProviderSelectionGate's
// relationship to its own Validate: Validate checks that the register
// honestly documents its own gaps (including the empty accepted_by above),
// while ReleaseDecision checks the substantive question a release gate
// actually cares about - can THR-07, a CRITICAL threat, actually ship?
// Because THR-07 has zero mitigations and its residual risk's accepted_by
// is empty, ReleaseDecision(now) reports the pilot blocked for exactly this
// threat, for any now. This is deliberate: this repository has not yet
// staffed a security officer able to accept a CRITICAL residual risk
// (Risk 127's "role names exist but nobody is operationally accountable"
// is exactly this gap), so a register that reported release as unblocked
// today would be fabricating readiness. fault_test.go proves both the
// blocking behavior and the companion invariant GREEN implies but does not
// spell out for the real file: an expired-but-otherwise-complete residual
// risk acceptance (accepted_by populated, expiry_date in the past) does not
// waive the block either - accepted_by alone is not enough once the
// acceptance itself has lapsed.
package threatregister
