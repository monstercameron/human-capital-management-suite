// Package scopeceiling implements PHASE-001: the signed Phase 1 scope-
// ceiling manifest that freezes the maximum candidate intents, capabilities,
// workflows, user flows, endpoints, models and effects the ChangeOps
// Promotion + Compensation Change pilot may ever draw build work from,
// together with four explicitly empty selection slots (provider,
// jurisdiction, topology, SLO) that only NEXT-002's dependency chain
// (SELECT-001, SELECT-002, CUSTOMER-001, TOPOLOGY-001, COMMERCIAL-001) may
// fill.
//
// # Independence from the P1A selection
//
// [ScopeCeilingManifest] is deliberately built from the same independent
// catalogs NEXT-002's selection-bound P1A manifest was built from -
// definitions/planning/capability-coverage.yaml (the fourteen DRAFT_CONTRACT
// business intents and ten bootstrap capabilities),
// definitions/planning/product-slices.yaml (the one live "promotion"
// vertical slice), definitions/api/endpoint-manifest.json (the fourteen
// generated endpoint definitions), planning/specs/business-intent-catalog.md,
// planning/specs/http-grpc-endpoint-contract.md, planning/user-flows/catalog.md,
// planning/execution-plan.md's Phase 1 Implementation-Depth Matrix and Plane
// Dependency Rule, and planning/next-steps.md's P1A/P1B contract and
// "Explicitly deferred" list - never from
// definitions/planning/gates/p1a-manifest.yaml itself. ceiling_test.go's
// TestPhaseOneManifestContainsOnlyAuthorizedSourceBoundExecutableScope and
// conformance_test.go's TestTodo_PHASE_001_Conformance cross-check the
// ceiling against the P1A manifest only as a falsifiable subset check
// (every P1A/P1B item must appear here, never the reverse), exactly as
// PHASE-001's REFACTOR clause requires: "selection cannot logically depend
// on a manifest that already requires the selection."
//
// # Selection slots
//
// [SelectionSlot] models a slot that this package can only ever leave
// empty: [SelectionSlot.Filled] is false and [SelectionSlot.Value] is ""
// for every slot in the checked-in manifest, and [ScopeCeilingManifest.Validate]
// fails loudly if a future edit ever fills one - filling a slot here would
// mean this manifest has silently become the NEXT-002 selection it is
// supposed to bound.
//
// # Signing
//
// [CanonicalDigest] hashes every field except the signature itself, exactly
// as tools/planning/gateevidence.P1AManifest.CanonicalDigest does for its
// manifest. Signing and verification reuse
// tools/planning/gateevidence.SignDigest and
// tools/planning/gateevidence.VerifyDigestSignature directly rather than
// reimplementing Ed25519 handling; the checked-in signature uses the same
// repository-local test-only development key fixture at
// tools/planning/gateevidence/testdata/dev-signing-key.yaml that
// tools/planning/gateevidence's own tests use, loaded through
// tools/policy/provenance.LoadSigningKeyFixture.
package scopeceiling
