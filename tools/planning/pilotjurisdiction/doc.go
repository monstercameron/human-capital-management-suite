// Package pilotjurisdiction implements SELECT-001: the signed pilot
// jurisdiction profile that replaces abstract legal composition with one
// reviewable jurisdiction, effective window and explicit non-coverage
// boundary for the ChangeOps Promotion + Compensation Change pilot.
//
// # What this package is not
//
// It is not a second jurisdiction-resolution engine. [internal/governance/legal.Resolve]
// already derives an explicit jurisdiction from transaction facts and fails
// closed with [legal.ErrLegalContextUnknown] on anything ambiguous; this
// package never reimplements that. [JurisdictionProfile.Authorize] calls
// Resolve directly and adds exactly one further check: whether the
// jurisdiction Resolve found is the one this profile was reviewed against.
// A resolution Resolve cannot make is reported exactly as Resolve reports
// it (HUMAN_REVIEW_REQUIRED); a resolution Resolve makes confidently but to
// some other jurisdiction this profile has not reviewed is a different
// failure this package adds (UNKNOWN, [ErrOutsideReviewedScope]) rather than
// silently evaluating the wrong state's law.
//
// # The pilot selection
//
// The pilot jurisdiction is California, chosen because LEGAL-001/LEGAL-010/
// LEGAL-011 already produced a mechanically-extracted draft rule pack for it
// (definitions/legal/packs/states/us-ca.json, eighteen populated obligation
// kinds, review_status UNREVIEWED, pack id ending "-draft") from
// planning/research/state-employment-law/california.md, and California's
// obligation density exercises the rule-pack machinery hardest of the fifty
// state drafts under definitions/legal/packs/states.
//
// # Scope completeness, not just non-emptiness
//
// [JurisdictionProfile.Validate] does not accept an [Exclusion] list that is
// merely non-empty. Every one of [legal.AllObligationTypes] must appear
// exactly once, either as an [ObligationMapping] (this profile reviewed the
// kind and maps it to the Phase 1 intent and evidence path that carries it)
// or as an OBLIGATION_KIND [Exclusion] (this profile declares the kind out
// of the reviewed scope with a reason) — never both, never neither. That is
// what makes "declares exact exclusions" a falsifiable property instead of a
// sentence in a comment: dropping either an obligation mapping or its
// complementary exclusion fails Validate by naming the exact kind missing.
//
// # The reviewer field is honestly empty
//
// RED requires a qualified legal owner/reviewer. No named human has reviewed
// this profile, and this package will not fabricate one into a signed
// governance artifact. [Reviewer.Name] is therefore a required field —
// [JurisdictionProfile.Validate] reports a violation whenever it is empty —
// so the checked-in profile is structurally incapable of claiming a review
// that has not happened, the same way definitions/legal/packs/states/us-ca.json
// itself carries review_status UNREVIEWED rather than a fabricated
// COUNSEL_APPROVED. The checked-in
// definitions/planning/gates/select-001-jurisdiction-profile.yaml therefore
// carries exactly one Validate violation (the empty reviewer) by design;
// profile_test.go's PRIMARY test asserts it is exactly that one and no
// other.
//
// # Signing
//
// [JurisdictionProfile.CanonicalDigest] hashes every field except the
// signature itself, following the same JSON-projection approach
// tools/planning/scopeceiling.ScopeCeilingManifest.CanonicalDigest uses.
// Signing and verification reuse tools/planning/gateevidence.SignDigest and
// tools/planning/gateevidence.VerifyDigestSignature directly rather than
// reimplementing Ed25519 handling, using the same repository-local
// development key fixture at
// tools/planning/gateevidence/testdata/dev-signing-key.yaml.
package pilotjurisdiction
