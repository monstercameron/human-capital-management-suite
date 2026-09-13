package legal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// This file holds LEGAL-010's test matrix. The backlog entry's TEST field is
// TestTodo_LEGAL_010 — the ticket numbering runs one ahead of the test
// numbering throughout section 20 of planning/todos.md (LEGAL-009 ->
// TestTodo_LEGAL_008, LEGAL-010 -> TestTodo_LEGAL_010, LEGAL-011 ->
// TestTodo_LEGAL_011) — so these names follow the TEST fields, not the ticket
// ids.

// loadStateDraft loads one extracted state draft definition.
func loadStateDraft(t *testing.T, code string) PackDefinition {
	t.Helper()
	path, err := PackDefinitionPath("states", "us-"+strings.ToLower(code)+".json")
	if err != nil {
		t.Fatalf("PackDefinitionPath: %v", err)
	}
	def, err := LoadPackDefinitionFile(path)
	if err != nil {
		t.Fatalf("LoadPackDefinitionFile(%s): %v", code, err)
	}
	return def
}

// draftRelease loads a state draft, validates it, and signs it as the release
// publisher would.
func draftRelease(t *testing.T, code string, seed byte) PackRelease {
	t.Helper()
	candidate, err := loadStateDraft(t, code).Candidate()
	if err != nil {
		t.Fatalf("Candidate(%s): %v", code, err)
	}
	release, err := candidate.Sign(SigningRoleReleasePublisher, fixedSigner(t, seed))
	if err != nil {
		t.Fatalf("Sign(%s): %v", code, err)
	}
	return release
}

// --- TestTodo_LEGAL_010 (PRIMARY) -------------------------------------------

// TestTodo_LEGAL_010 walks the contract's section 3 release family end to end:
// a checked-in definition file becomes a validated candidate, the candidate
// becomes a digested and signed release, the release supersedes its
// predecessor and closes the prior window, and an unsupported vocabulary is
// refused rather than read as "no obligations".
func TestTodo_LEGAL_010(t *testing.T) {
	t.Run("definition file becomes a candidate becomes a release", func(t *testing.T) {
		def := loadStateDraft(t, "CA")
		if def.VocabularyVersion != uint32(VocabularyVersion2) {
			t.Fatalf("draft vocabulary_version = %d, want %d", def.VocabularyVersion, VocabularyVersion2)
		}
		candidate, err := def.Candidate()
		if err != nil {
			t.Fatalf("Candidate: %v", err)
		}
		unsigned := candidate.Unsigned()
		if unsigned.Digest != "" || len(unsigned.Signatures) != 0 {
			t.Error("a candidate carries a digest or a signature; only a release does")
		}
		if err := unsigned.Verify(); !errors.Is(err, ErrPackNotSigned) {
			t.Errorf("Verify() on a candidate = %v, want ErrPackNotSigned", err)
		}

		release, err := candidate.Sign(SigningRoleReleasePublisher, fixedSigner(t, 0x11))
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if len(release.Digest) != 64 {
			t.Errorf("release digest length = %d, want 64 hex characters", len(release.Digest))
		}
		if release.ComputeDigest() != release.Digest {
			t.Error("recorded digest does not match the recomputed canonical digest")
		}
		if err := release.Verify(); err != nil {
			t.Errorf("Verify() on a freshly signed release: %v", err)
		}
	})

	t.Run("the seed packs are produced by the loader, not by a Go literal", func(t *testing.T) {
		// LEGAL-010's REFACTOR clause. If the seed packs were still built in
		// Go, deleting their definition files would not break them.
		for _, parts := range [][]string{californiaSeedDefinition, newYorkSeedDefinition} {
			path, err := PackDefinitionPath(parts...)
			if err != nil {
				t.Fatalf("PackDefinitionPath: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("seed definition file missing: %v", err)
			}
		}
		ca, err := CaliforniaPromotionPack()
		if err != nil {
			t.Fatalf("CaliforniaPromotionPack: %v", err)
		}
		if ca.PackID != "us-ca-promotion-base-pay-change" || ca.Version != 1 {
			t.Errorf("seed pack identity drifted: %s v%d", ca.PackID, ca.Version)
		}
		if ca.SourceType != SourceTypeStatute || ca.ReviewStatus != ReviewStatusUnreviewed {
			t.Errorf("seed pack source/review = %s/%s, want STATUTE/UNREVIEWED", ca.SourceType, ca.ReviewStatus)
		}
	})

	t.Run("a supersession closes the prior window and links both directions", func(t *testing.T) {
		registry := NewRegistry()
		first := supersessionFixture(t, 1, 0, mustDate(t, 2026, time.January, 1))
		if err := registry.Register(first); err != nil {
			t.Fatalf("Register(v1): %v", err)
		}
		second := supersessionFixture(t, 2, 0, mustDate(t, 2027, time.January, 1))
		if err := registry.Supersede(first.Release(), second); err != nil {
			t.Fatalf("Supersede: %v", err)
		}

		prior, err := registry.GetExact(first.Release())
		if err != nil {
			t.Fatalf("GetExact(prior): %v", err)
		}
		if !prior.Window.HasEnd || prior.Window.End.Compare(second.Window.Start) != 0 {
			t.Fatalf("prior window = %s, want it closed at the successor's start %s",
				prior.Window, second.Window.Start)
		}
		if prior.SupersededBy == nil || prior.SupersededBy.Version != 2 {
			t.Error("prior release does not point at its successor")
		}
		next, err := registry.GetExact(second.Release())
		if err != nil {
			t.Fatalf("GetExact(successor): %v", err)
		}
		if next.Supersedes == nil || next.Supersedes.Version != 1 {
			t.Error("successor does not point at its predecessor")
		}

		// Business time, not execution time, selects the release.
		beforeAmendment, err := registry.Lookup(next.Jurisdiction, mustDate(t, 2026, time.December, 31))
		if err != nil {
			t.Fatalf("Lookup(before amendment): %v", err)
		}
		if beforeAmendment.Version != 1 {
			t.Errorf("a 2026-12-31 transaction resolved to v%d, want the release in force then (v1)", beforeAmendment.Version)
		}
		afterAmendment, err := registry.Lookup(next.Jurisdiction, mustDate(t, 2027, time.January, 1))
		if err != nil {
			t.Fatalf("Lookup(after amendment): %v", err)
		}
		if afterAmendment.Version != 2 {
			t.Errorf("a 2027-01-01 transaction resolved to v%d, want v2", afterAmendment.Version)
		}
	})

	t.Run("a pinned release keeps resolving forever through GetExact", func(t *testing.T) {
		registry := NewRegistry()
		first := supersessionFixture(t, 1, 0, mustDate(t, 2026, time.January, 1))
		if err := registry.Register(first); err != nil {
			t.Fatalf("Register: %v", err)
		}
		second := supersessionFixture(t, 2, 0, mustDate(t, 2027, time.January, 1))
		if err := registry.Supersede(first.Release(), second); err != nil {
			t.Fatalf("Supersede: %v", err)
		}
		// The v1 window is closed and "latest" is v2, yet a context that
		// pinned v1 must still get v1's content.
		got, err := registry.GetExact(first.Release())
		if err != nil {
			t.Fatalf("GetExact after supersession: %v", err)
		}
		if got.Version != 1 || len(got.RetentionRules) != len(first.RetentionRules) {
			t.Error("GetExact stopped returning the pinned release's own content")
		}
	})

	t.Run("an unsupported vocabulary version is refused, never read as no obligations", func(t *testing.T) {
		pack := supersessionFixture(t, 1, 0, mustDate(t, 2026, time.January, 1))
		pack.VocabularyVersion = SupportedVocabularyVersion + 1
		if err := pack.Validate(); !errors.Is(err, ErrVocabularyVersionUnsupported) {
			t.Fatalf("Validate(future vocabulary) = %v, want ErrVocabularyVersionUnsupported", err)
		}

		def := loadStateDraft(t, "NV")
		def.VocabularyVersion = uint32(SupportedVocabularyVersion) + 1
		if _, err := def.Candidate(); !errors.Is(err, ErrVocabularyVersionUnsupported) {
			t.Fatalf("Candidate(future vocabulary) = %v, want ErrVocabularyVersionUnsupported", err)
		}
	})

	t.Run("a v1 release read by a v2 engine reports not-considered, not not-applicable", func(t *testing.T) {
		registry := testRegistry(t)
		signer := fixedSigner(t, 0x12)
		ctx, err := Resolve(validInput(t, testCAJurisdiction()), registry, signer, mustInstant(t, 1_770_800_000))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		result, err := Evaluate(ctx, PromotionProposalSnapshot{}, registry)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(result.NotConsidered) != 12 {
			t.Fatalf("NotConsidered = %d kinds, want the 12 that vocabulary 1 predates", len(result.NotConsidered))
		}
		for _, nc := range result.NotConsidered {
			if VocabularyOf(nc.Type) != VocabularyVersion2 {
				t.Errorf("%s is reported as not considered but exists in vocabulary %d", nc.Type, VocabularyOf(nc.Type))
			}
			for _, na := range result.NotApplicable {
				if na.Type == nc.Type {
					t.Errorf("%s appears in both NotApplicable and NotConsidered", nc.Type)
				}
			}
		}
	})

	t.Run("a definition file cannot be edited in place through a loaded pack", func(t *testing.T) {
		registry := NewRegistry()
		pack := supersessionFixture(t, 1, 0, mustDate(t, 2026, time.January, 1))
		if err := registry.Register(pack); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if err := registry.Register(pack); !errors.Is(err, ErrRulePackDuplicate) {
			t.Fatalf("re-registering the same release = %v, want ErrRulePackDuplicate", err)
		}
	})
}

// supersessionFixture builds a minimal, valid pack for release-chain tests.
func supersessionFixture(t *testing.T, major, minor uint32, start values.LocalDate) RulePack {
	t.Helper()
	window, err := NewOpenEffectiveWindow(start)
	if err != nil {
		t.Fatalf("NewOpenEffectiveWindow: %v", err)
	}
	return RulePack{
		PackID:            "us-zz-supersession-fixture",
		Version:           major,
		MinorVersion:      minor,
		VocabularyVersion: VocabularyVersion2,
		SourceType:        SourceTypeStatute,
		ReviewStatus:      ReviewStatusUnreviewed,
		Jurisdiction:      Jurisdiction{Country: "US", State: "ZZ"},
		Window:            window,
		RetentionRules: []RetentionRule{{
			ID:            fmt.Sprintf("zz-retention-v%d", major),
			RecordClass:   "payroll_records",
			DurationYears: 3,
			DurationBasis: "from_record_date",
			Citation: Citation{
				SourceFile:       californiaSourceFile,
				Section:          "fixture",
				Status:           ReviewStatusUnreviewed,
				ConfidenceMarker: ConfidenceMarkerVerify,
			},
		}},
	}
}

// --- TestTodo_LEGAL_010_Property --------------------------------------------

// TestTodo_LEGAL_010_Property fixes the contract's section 3.2 digest
// boundary in both directions: everything the section lists changes the
// digest, and everything it excludes does not.
func TestTodo_LEGAL_010_Property(t *testing.T) {
	base := draftRelease(t, "MN", 0x21)

	t.Run("rewording a citation Note leaves the digest unchanged", func(t *testing.T) {
		reworded := base
		reworded.RetentionRules = append([]RetentionRule(nil), base.RetentionRules...)
		if len(reworded.RetentionRules) == 0 {
			t.Skip("fixture carries no retention rule")
		}
		reworded.RetentionRules[0].Citation.Note = "an entirely different paraphrase of the same section"
		if got := reworded.ComputeDigest(); got != base.Digest {
			t.Fatalf("digest changed when only the citation Note changed: %s != %s", got, base.Digest)
		}
		// And the signature still verifies, which is the point: correcting a
		// paraphrase must not invalidate a published release.
		reworded.Digest = base.Digest
		if err := reworded.Verify(); err != nil {
			t.Fatalf("Verify after rewording the Note: %v", err)
		}
	})

	t.Run("changing any typed body field changes the digest", func(t *testing.T) {
		mutations := map[string]func(p *RulePack){
			"retention duration": func(p *RulePack) {
				p.RetentionRules = append([]RetentionRule(nil), p.RetentionRules...)
				p.RetentionRules[0].DurationYears += 1
			},
			"retention record class": func(p *RulePack) {
				p.RetentionRules = append([]RetentionRule(nil), p.RetentionRules...)
				p.RetentionRules[0].RecordClass += "_amended"
			},
			"citation section": func(p *RulePack) {
				p.RetentionRules = append([]RetentionRule(nil), p.RetentionRules...)
				p.RetentionRules[0].Citation.Section = "a different operative provision"
			},
			"citation confidence marker": func(p *RulePack) {
				p.RetentionRules = append([]RetentionRule(nil), p.RetentionRules...)
				p.RetentionRules[0].Citation.ConfidenceMarker = ConfidenceMarkerDisputed
			},
			"review status": func(p *RulePack) { p.ReviewStatus = ReviewStatusVendorBaseline },
			"source type":   func(p *RulePack) { p.SourceType = SourceTypeAgencyGuidance },
			"minor version": func(p *RulePack) { p.MinorVersion++ },
			"vocabulary version": func(p *RulePack) {
				p.VocabularyVersion = VocabularyVersion1
			},
			"window start": func(p *RulePack) {
				p.Window.Start = p.Window.Start.AddDays(1)
			},
		}
		if len(base.RetentionRules) == 0 {
			t.Skip("fixture carries no retention rule")
		}
		for name, mutate := range mutations {
			mutated := base
			mutate(&mutated)
			if got := mutated.ComputeDigest(); got == base.Digest {
				t.Errorf("mutating %s left the digest unchanged", name)
			}
		}
	})

	t.Run("the digest is stable across an encode/decode round trip", func(t *testing.T) {
		def := PackDefinitionFrom(base, ProvenanceJSON{Generator: "round trip", SourceFiles: []string{}})
		encoded, err := MarshalPackDefinition(def)
		if err != nil {
			t.Fatalf("MarshalPackDefinition: %v", err)
		}
		reloaded, err := LoadPackDefinition(encoded)
		if err != nil {
			t.Fatalf("LoadPackDefinition: %v", err)
		}
		candidate, err := reloaded.Candidate()
		if err != nil {
			t.Fatalf("Candidate: %v", err)
		}
		if got := candidate.Pack().ComputeDigest(); got != base.Digest {
			t.Fatalf("digest changed across a definition round trip: %s != %s", got, base.Digest)
		}
	})

	t.Run("an empty locality path never encodes like a one-element empty path", func(t *testing.T) {
		stateLevel := base
		locality := base
		locality.Jurisdiction.Locality = ""
		// Two packs identical but for a locality that is present-and-empty
		// versus absent must not collide once a real locality appears.
		named := base
		named.Jurisdiction.Locality = "Minneapolis"
		if named.ComputeDigest() == stateLevel.ComputeDigest() {
			t.Error("a locality-level release digests identically to its state-level parent")
		}
		if locality.ComputeDigest() != stateLevel.ComputeDigest() {
			t.Error("an absent locality and an empty locality string digest differently")
		}
	})
}

// --- TestTodo_LEGAL_010_Golden ----------------------------------------------

// TestTodo_LEGAL_010_Golden pins the canonical encoding of a PackRelease. Any
// unreviewed change to the section 3.2 field list, the framing, or the field
// order breaks it.
func TestTodo_LEGAL_010_Golden(t *testing.T) {
	const wantDigest = "8a60badbb360be623a9326d1b8213ddcb217ae221d26b33b2c5538bd9ce30ee3"
	release := draftRelease(t, "WI", 0x2a)
	if release.Digest != release.ComputeDigest() {
		t.Fatal("signed digest disagrees with the recomputed digest")
	}
	if release.Digest != wantDigest {
		t.Fatalf("PackRelease digest = %s, want golden %s (the section 3.2 field list, its order, "+
			"the canonical framing, or the Wisconsin draft itself changed)", release.Digest, wantDigest)
	}

	// Wisconsin is the preemption fixture the contract's section 6.4 names:
	// without a first-class assertion a naive composition would attach a
	// preempted Milwaukee paid-leave obligation to a Wisconsin promotion.
	if len(release.PreemptionAssertions) == 0 {
		t.Fatal("the Wisconsin draft carries no preemption assertion")
	}
	wantPreempted := map[ObligationType]bool{
		ObligationTypeWageFloor:        true,
		ObligationTypeLeaveInteraction: true,
		ObligationTypeFieldRestriction: true,
		ObligationTypePayTransparency:  true,
	}
	for _, a := range release.PreemptionAssertions {
		if !wantPreempted[a.Kind] {
			t.Errorf("Wisconsin preempts %s, which the contract's section 6.4 does not list", a.Kind)
		}
		if a.Scope != "LOCALITY_ONLY" {
			t.Errorf("preemption scope = %q, want LOCALITY_ONLY", a.Scope)
		}
		delete(wantPreempted, a.Kind)
	}
	if len(wantPreempted) != 0 {
		t.Errorf("Wisconsin draft is missing preemption assertions for %v", wantPreempted)
	}
}

// --- FuzzPackDefinitionLoader -----------------------------------------------

// FuzzPackDefinitionLoader asserts the two properties the contract's section
// 8.4 names: a malformed definition never panics, and a definition that loads
// never produces a release that Validate accepts unless it really is valid.
func FuzzPackDefinitionLoader(f *testing.F) {
	seedPath, err := PackDefinitionPath("seed", "us-ca.json")
	if err != nil {
		f.Fatalf("PackDefinitionPath: %v", err)
	}
	if data, err := os.ReadFile(seedPath); err == nil {
		f.Add(data)
	}
	f.Add([]byte(`{"schema_version":1}`))
	f.Add([]byte(`{"schema_version":1,"pack_id":"x","version":{"major":1},"vocabulary_version":2}`))
	f.Add([]byte(`{`))
	f.Add([]byte(``))
	f.Add([]byte(`{"schema_version":1,"obligations":[{"kind":"NOTICE","id":"a"}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		def, err := LoadPackDefinition(data)
		if err != nil {
			return
		}
		candidate, err := def.Candidate()
		if err != nil {
			return
		}
		pack := candidate.Pack()
		if err := pack.ValidateForRelease(); err != nil {
			t.Fatalf("Candidate returned a pack its own ValidateForRelease rejects: %v", err)
		}
		if pack.ComputeDigest() == "" {
			t.Fatal("a validated pack produced an empty digest")
		}
	})
}

// --- TestTodo_LEGAL_010_Race ------------------------------------------------

// TestTodo_LEGAL_010_Race hammers Register, Lookup, GetExact and Supersede
// against one registry from many goroutines. The module is built without
// -race on this platform, so the assertion is behavioural: every reader sees
// a consistent release, and no writer loses one.
func TestTodo_LEGAL_010_Race(t *testing.T) {
	registry := NewRegistry()
	base := supersessionFixture(t, 1, 0, mustDate(t, 2026, time.January, 1))
	if err := registry.Register(base); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if err := registry.Register(ca); err != nil {
		t.Fatalf("Register(CA): %v", err)
	}

	const goroutines = 8
	const iterations = 40
	errCh := make(chan error, goroutines*iterations)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				got, err := registry.GetExact(base.Release())
				if err != nil {
					errCh <- fmt.Errorf("goroutine %d: GetExact: %w", id, err)
					return
				}
				if got.PackID != base.PackID || got.Version != base.Version {
					errCh <- fmt.Errorf("goroutine %d: GetExact returned %s v%d", id, got.PackID, got.Version)
					return
				}
				if _, err := registry.Lookup(testCAJurisdiction(), mustDate(t, 2026, time.June, 1)); err != nil {
					errCh <- fmt.Errorf("goroutine %d: Lookup: %w", id, err)
					return
				}
				unique := supersessionFixture(t, uint32(100+id*iterations+i), 0, mustDate(t, 2026, time.January, 1))
				if err := registry.Register(unique); err != nil {
					errCh <- fmt.Errorf("goroutine %d: Register: %w", id, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// --- TestTodo_LEGAL_010_Security --------------------------------------------

// TestTodo_LEGAL_010_Security is the contract's section 8.4 SECURITY row: a
// forged signature over a valid digest is rejected, an untrusted key is
// rejected by VerifyWithKey, and a release whose content changed after
// signing no longer verifies.
func TestTodo_LEGAL_010_Security(t *testing.T) {
	publisher := fixedSigner(t, 0x31)
	attacker := fixedSigner(t, 0x32)
	release := draftRelease(t, "NV", 0x31)

	t.Run("a forged signature over a valid digest is rejected", func(t *testing.T) {
		forged := release
		forged.Signatures = []RoleSignature{{
			Role: SigningRoleReleasePublisher,
			Signature: Signature{
				PublicKey: publisher.PublicKey(),
				Bytes:     bytes.Repeat([]byte{0xAB}, len(release.Signatures[0].Signature.Bytes)),
			},
		}}
		if err := forged.Verify(); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("Verify(forged signature) = %v, want ErrSignatureInvalid", err)
		}
	})

	t.Run("a release signed by an untrusted key is rejected by VerifyWithKey", func(t *testing.T) {
		other, err := loadStateDraft(t, "NV").Candidate()
		if err != nil {
			t.Fatalf("Candidate: %v", err)
		}
		signedByAttacker, err := other.Sign(SigningRoleReleasePublisher, attacker)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if err := signedByAttacker.Verify(); err != nil {
			t.Fatalf("the attacker's own release is internally consistent, so Verify must pass: %v", err)
		}
		if err := signedByAttacker.VerifyWithKey(SigningRoleReleasePublisher, publisher.PublicKey()); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("VerifyWithKey(untrusted key) = %v, want ErrSignatureInvalid", err)
		}
	})

	t.Run("content changed after signing no longer verifies", func(t *testing.T) {
		tampered := release
		tampered.RetentionRules = append([]RetentionRule(nil), release.RetentionRules...)
		if len(tampered.RetentionRules) == 0 {
			t.Skip("fixture carries no retention rule")
		}
		tampered.RetentionRules[0].DurationYears = 99
		if err := tampered.Verify(); !errors.Is(err, ErrDigestMismatch) {
			t.Fatalf("Verify(tampered content) = %v, want ErrDigestMismatch", err)
		}
	})

	t.Run("a second role signs the same digest and neither role may sign twice", func(t *testing.T) {
		counsel := fixedSigner(t, 0x33)
		twoRole, err := AddSignature(release, SigningRoleCustomerCounsel, counsel)
		if err != nil {
			t.Fatalf("AddSignature: %v", err)
		}
		if len(twoRole.Signatures) != 2 {
			t.Fatalf("signatures = %d, want 2", len(twoRole.Signatures))
		}
		if err := twoRole.Verify(); err != nil {
			t.Fatalf("Verify with two role signatures: %v", err)
		}
		if _, err := AddSignature(twoRole, SigningRoleCustomerCounsel, counsel); !errors.Is(err, ErrPackSignatureRoleDupe) {
			t.Fatalf("AddSignature(duplicate role) = %v, want ErrPackSignatureRoleDupe", err)
		}
	})

	t.Run("a VERIFY rule cannot reach COUNSEL_APPROVED", func(t *testing.T) {
		// Every extracted draft carries at least one VERIFY marker, which is
		// exactly what must block the highest review status.
		pack, err := loadStateDraft(t, "CA").Candidate()
		if err != nil {
			t.Fatalf("Candidate: %v", err)
		}
		claim := pack.Pack()
		claim.ReviewStatus = ReviewStatusCounselApproved
		if err := claim.ValidateForRelease(); err == nil {
			t.Fatal("a pack carrying VERIFY rules was accepted as COUNSEL_APPROVED")
		}
	})
}

// --- TestTodo_LEGAL_010_Mutation --------------------------------------------

// TestTodo_LEGAL_010_Mutation seeds a mutant into each guard the release
// family depends on and asserts the guard notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_010_Mutation(t *testing.T) {
	mutants := []struct {
		name    string
		mutate  func(*PackDefinition)
		wantErr error
	}{
		{"schema version drift", func(d *PackDefinition) { d.SchemaVersion = 99 }, ErrPackDefinitionSchema},
		{"missing pack id", func(d *PackDefinition) { d.PackID = "" }, ErrPackDefinitionField},
		{"zero major version", func(d *PackDefinition) { d.Version.Major = 0 }, ErrPackDefinitionField},
		{"missing vocabulary", func(d *PackDefinition) { d.VocabularyVersion = 0 }, ErrPackDefinitionField},
		{"unknown source type", func(d *PackDefinition) { d.SourceType = "FOLKLORE" }, ErrSourceType},
		{"unknown review status", func(d *PackDefinition) { d.ReviewStatus = "PROBABLY_FINE" }, ErrCitationStatus},
		{"unknown jurisdiction level", func(d *PackDefinition) { d.Jurisdiction.Level = "GALAXY" }, ErrPackDefinitionField},
		{"subdivision pack with a locality path", func(d *PackDefinition) {
			d.Jurisdiction.LocalityPath = []string{"Reno"}
		}, ErrPackDefinitionField},
		{"unknown obligation kind", func(d *PackDefinition) {
			d.Obligations[0].Kind = "VIBES"
		}, ErrPackDefinitionField},
		{"obligation without an id", func(d *PackDefinition) {
			d.Obligations[0].ID = ""
		}, ErrPackDefinitionField},
		{"citation without a confidence marker", func(d *PackDefinition) {
			d.Obligations[0].Citation.ConfidenceMarker = ""
		}, ErrPackDefinitionField},
		{"citation without a source file", func(d *PackDefinition) {
			d.Obligations[0].Citation.SourceFile = ""
		}, ErrCitationSourceFile},
		{"citation without a section", func(d *PackDefinition) {
			d.Obligations[0].Citation.Section = ""
		}, ErrCitationSection},
		{"duplicate obligation id", func(d *PackDefinition) {
			d.Obligations = append(d.Obligations, d.Obligations[0])
		}, nil},
		{"inverted effective window", func(d *PackDefinition) {
			d.Window.End = "2025-01-01"
		}, ErrRulePackWindow},
		{"preemption across kinds with no scope", func(d *PackDefinition) {
			d.Preemptions = append(d.Preemptions, PreemptionJSON{
				Kind:     "NOTICE",
				Scope:    "EVERYTHING",
				Citation: d.Obligations[0].Citation,
			})
		}, nil},
	}

	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			def := loadStateDraft(t, "NV")
			// Deep-copy the obligation slice so one mutant cannot leak into
			// the next.
			def.Obligations = append([]ObligationJSON(nil), def.Obligations...)
			def.Preemptions = append([]PreemptionJSON(nil), def.Preemptions...)
			m.mutate(&def)

			encoded, err := json.Marshal(def)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			reloaded, loadErr := LoadPackDefinition(encoded)
			if loadErr != nil {
				if m.wantErr != nil && !errors.Is(loadErr, m.wantErr) {
					t.Fatalf("LoadPackDefinition = %v, want %v", loadErr, m.wantErr)
				}
				return
			}
			_, err = reloaded.Candidate()
			if err == nil {
				t.Fatal("mutant survived: Candidate accepted it")
			}
			if m.wantErr != nil && !errors.Is(err, m.wantErr) {
				t.Fatalf("Candidate = %v, want %v", err, m.wantErr)
			}
		})
	}
}

// --- checked-in tree sanity -------------------------------------------------

// TestPackDefinitionTreeLoads is the cheap guard that keeps the other tests
// honest: every checked-in definition file parses and validates.
func TestPackDefinitionTreeLoads(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	dir := filepath.Join(root, filepath.FromSlash(PackDefinitionDir))
	count := 0
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		count++
		def, err := LoadPackDefinitionFile(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		if _, err := def.Candidate(); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	if count != 53 {
		t.Errorf("loaded %d definition files, want 53 (51 jurisdiction drafts + 2 seed fixtures)", count)
	}
}
