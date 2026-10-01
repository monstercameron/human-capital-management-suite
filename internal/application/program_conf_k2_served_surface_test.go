package application_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	programconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/program"
)

func servedProgramFixtures() []programconformance.Fixture {
	digests := map[string]string{}
	for _, facet := range programconformance.SharedFacets {
		digests[facet] = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}
	fixture := func(domain string, rules []string) programconformance.Fixture {
		copyDigests := make(map[string]string, len(digests))
		for key, value := range digests {
			copyDigests[key] = value
		}
		return programconformance.Fixture{
			Domain: domain, Tenant: "tenant-acme", DefinitionRef: domain + "-definition", RevisionRef: domain + "-revision",
			PopulationRef: "population-2026", EligibilityRef: domain + "-eligibility", CycleRef: "cycle-2026",
			RuleRefs: rules, ParticipationStatus: programconformance.ParticipationEnrolled,
			OutcomeStatus: programconformance.OutcomeAchieved, FacetDigests: copyDigests,
		}
	}
	return []programconformance.Fixture{
		fixture(programconformance.DomainBenefit, []string{"benefit-rule"}),
		fixture(programconformance.DomainBonus, []string{"bonus-rule"}),
		fixture(programconformance.DomainLearning, []string{"learning-rule"}),
		fixture(programconformance.DomainLeave, []string{"leave-rule"}),
	}
}

func TestTodo_PROGRAM_CONF_001_Served(t *testing.T) {
	surface := application.NewServedProgramConformanceSurface()
	report, err := surface.CheckSharedAbstraction(servedProgramFixtures(), "tenant-acme")
	if err != nil || len(report.Domains) != 4 || report.Digest == "" {
		t.Fatalf("served Program conformance=%+v err=%v", report, err)
	}
	fixtures := servedProgramFixtures()
	fixtures[2].Tenant = "tenant-rival"
	if _, err := surface.CheckSharedAbstraction(fixtures, "tenant-acme"); err == nil {
		t.Fatal("served Program conformance accepted a cross-tenant fixture")
	}
}

func TestServedProgramConformanceAppSurface(t *testing.T) {
	var app application.App
	if got := app.ProgramConformance(); got.ValidateFixture == nil || got.CheckSharedAbstraction == nil {
		t.Fatal("composed application omitted Program conformance surface")
	}
	var nilApp *application.App
	if got := nilApp.ProgramConformance(); got.CheckSharedAbstraction != nil {
		t.Fatal("nil application exposed Program conformance capabilities")
	}
}
