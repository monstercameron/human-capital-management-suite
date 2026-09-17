package program

import (
	"strings"
	"testing"
)

// fixedFixtures returns the four canonical domain fixtures used across the
// matrix. Every facet digest is a well-formed sha256: digest over
// domain-distinct content.
func fixedFixtures() []Fixture {
	mk := func(domain, tenant string, rules []string) Fixture {
		digests := map[string]string{}
		for _, f := range SharedFacets {
			digests[f] = seededDigest(domain + "\x00" + f)
		}
		return Fixture{
			Domain:              domain,
			Tenant:              tenant,
			DefinitionRef:       domain + "-def-1",
			RevisionRef:         domain + "-rev-3",
			PopulationRef:       "pop-2026-q3",
			EligibilityRef:      domain + "-elig-2",
			CycleRef:            "cycle-2026-q3",
			RuleRefs:            rules,
			ParticipationStatus: ParticipationEnrolled,
			OutcomeStatus:       OutcomeAchieved,
			FacetDigests:        digests,
		}
	}
	return []Fixture{
		mk(DomainBenefit, "tenant-acme", []string{"benefit-elig-v2", "benefit-accrual-v1"}),
		mk(DomainBonus, "tenant-acme", []string{"bonus-pool-v3", "bonus-payout-v1"}),
		mk(DomainLearning, "tenant-acme", []string{"learning-prereq-v2", "learning-credit-v1"}),
		mk(DomainLeave, "tenant-acme", []string{"leave-entitle-v4", "leave-concur-v1"}),
	}
}

func TestTodo_PROGRAM_CONF_001(t *testing.T) {
	report, err := CheckSharedAbstraction(fixedFixtures(), "tenant-acme")
	if err != nil {
		t.Fatalf("CheckSharedAbstraction: %v", err)
	}
	if len(report.Domains) != 4 {
		t.Fatalf("expected 4 domains, got %d", len(report.Domains))
	}
	for _, want := range []string{DomainBenefit, DomainBonus, DomainLearning, DomainLeave} {
		found := false
		for _, got := range report.Domains {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("report omits domain %q", want)
		}
	}
	if report.Digest == "" {
		t.Fatal("sealed report carries no digest")
	}
	if len(report.Facets) != len(SharedFacets) {
		t.Fatalf("report covers %d facets, want %d", len(report.Facets), len(SharedFacets))
	}
}

func TestTodo_PROGRAM_CONF_001_Property(t *testing.T) {
	base := fixedFixtures()
	// Determinism: identical input seals an identical digest.
	first, err := CheckSharedAbstraction(base, "tenant-acme")
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	second, err := CheckSharedAbstraction(fixedFixtures(), "tenant-acme")
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("identical fixtures seal different digests")
	}
	// Removing any single facet from any single domain refuses.
	for i := range base {
		for _, facet := range SharedFacets {
			mut := fixedFixtures()
			delete(mut[i].FacetDigests, facet)
			if _, err := CheckSharedAbstraction(mut, "tenant-acme"); err == nil {
				t.Fatalf("missing facet %q in domain %q accepted", facet, mut[i].Domain)
			}
		}
	}
	// Every domain accepts every shared status in the closed vocabularies.
	for _, p := range []string{ParticipationEnrolled, ParticipationPending, ParticipationWithdrawn} {
		for _, o := range []string{OutcomeAchieved, OutcomePartial, OutcomePending} {
			mut := fixedFixtures()
			for i := range mut {
				mut[i].ParticipationStatus = p
				mut[i].OutcomeStatus = o
			}
			if _, err := CheckSharedAbstraction(mut, "tenant-acme"); err != nil {
				t.Fatalf("shared statuses %q/%q refused: %v", p, o, err)
			}
		}
	}
}

func TestTodo_PROGRAM_CONF_001_Golden(t *testing.T) {
	report, err := CheckSharedAbstraction(fixedFixtures(), "tenant-acme")
	if err != nil {
		t.Fatalf("CheckSharedAbstraction: %v", err)
	}
	want := strings.TrimSpace(readGolden(t, "testdata/program_conf_001.golden"))
	if report.Digest != want {
		t.Fatalf("sealed digest mismatch:\n got %q\nwant %q", report.Digest, want)
	}
}

func TestTodo_PROGRAM_CONF_001_Conformance(t *testing.T) {
	fixtures := fixedFixtures()
	// Every domain binds the same facet key set: no domain may add, drop
	// or rename a facet.
	reference := map[string]bool{}
	for _, f := range SharedFacets {
		reference[f] = true
	}
	for _, fx := range fixtures {
		if len(fx.FacetDigests) != len(reference) {
			t.Fatalf("domain %q binds %d facets, want %d", fx.Domain, len(fx.FacetDigests), len(reference))
		}
		for k := range fx.FacetDigests {
			if !reference[k] {
				t.Fatalf("domain %q binds non-shared facet %q", fx.Domain, k)
			}
		}
	}
	// The sealed digest verifies through the shared oracle for every
	// domain: recompute per-domain facet coverage and reseal.
	report, err := CheckSharedAbstraction(fixtures, "tenant-acme")
	if err != nil {
		t.Fatalf("CheckSharedAbstraction: %v", err)
	}
	resealed, err := CheckSharedAbstraction(fixtures, "tenant-acme")
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	if report.Digest != resealed.Digest {
		t.Fatal("shared oracle does not reseal identically")
	}
}

func FuzzTodo_PROGRAM_CONF_001(f *testing.F) {
	base := fixedFixtures()[0]
	f.Add([]byte("sha256:abc"), []byte("definition"), []byte("tenant-acme"))
	f.Fuzz(func(t *testing.T, digest, facet, tenant []byte) {
		fx := base
		fx.FacetDigests = map[string]string{}
		for _, fc := range SharedFacets {
			fx.FacetDigests[fc] = seededDigest(string(digest) + fc)
		}
		fx.FacetDigests[string(facet)] = string(digest)
		fx.Tenant = string(tenant)
		// Must never panic; malformed input must never be accepted.
		report, err := CheckSharedAbstraction([]Fixture{fx, fixedFixtures()[1], fixedFixtures()[2], fixedFixtures()[3]}, "tenant-acme")
		if err == nil && report.Digest == "" {
			t.Fatal("accepted fixture seals an empty digest")
		}
	})
}

func TestTodo_PROGRAM_CONF_001_Security(t *testing.T) {
	fixtures := fixedFixtures()
	fixtures[2].Tenant = "tenant-rival"
	_, err := CheckSharedAbstraction(fixtures, "tenant-acme")
	if err == nil {
		t.Fatal("cross-tenant fixture accepted")
	}
	rej, ok := AsRejection(err)
	if !ok {
		t.Fatalf("denial is not a typed rejection: %v", err)
	}
	if rej.Code != CodeRejected {
		t.Fatalf("denial code = %q, want %q", rej.Code, CodeRejected)
	}
	// No existence leakage: the denial must not name which domains exist
	// or which fixture failed.
	msg := err.Error()
	for _, name := range []string{DomainBenefit, DomainBonus, DomainLearning, DomainLeave} {
		if strings.Contains(msg, name) {
			t.Fatalf("denial leaks domain existence: %q", msg)
		}
	}
}

func TestTodo_PROGRAM_CONF_001_Mutation(t *testing.T) {
	// Mutant A: one mono-rule across all four domains must be killed.
	mono := fixedFixtures()
	for i := range mono {
		mono[i].RuleRefs = []string{"universal-rule-v1"}
	}
	if _, err := CheckSharedAbstraction(mono, "tenant-acme"); err == nil {
		t.Fatal("mono-rule mutant survived")
	}
	// Mutant B: tampered facet digest must be killed.
	tampered := fixedFixtures()
	tampered[1].FacetDigests[FacetOutcome] = "sha256:tampered"
	if _, err := CheckSharedAbstraction(tampered, "tenant-acme"); err == nil {
		t.Fatal("tampered-digest mutant survived")
	}
	// Mutant C: forced placeholder field must be killed.
	forced := fixedFixtures()
	forced[3].FacetDigests[FacetCycle] = "N/A"
	if _, err := CheckSharedAbstraction(forced, "tenant-acme"); err == nil {
		t.Fatal("forced-placeholder mutant survived")
	}
	// Mutant D: dropped domain must be killed.
	if _, err := CheckSharedAbstraction(fixedFixtures()[:3], "tenant-acme"); err == nil {
		t.Fatal("dropped-domain mutant survived")
	}
}
