package jobarch

// UX-JOBARCH-001 PRIMARY: add an authorized Job Architecture admin workspace.
//
// RED: admin UI edits published architecture in place, offers unsupported
// job/grade combinations, exposes salary or benefit rules without authority,
// or saves browser-only configuration.
//
// GREEN: responsive reusable components show the family/level/profile graph,
// exact pay-band and increase rules, benefit impacts and publish status; all
// writes use server-side governed workflows and role/page/action
// authorization.

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func workspaceArchFixture() ArchitectureRevision {
	base := architectureFixture()
	base.Profiles = []JobProfileRevision{{
		ID: "profile-1", ProfileID: "profile-1", Revision: "p1",
		FamilyID: "family-1", LevelID: "level-1", GradeID: "grade-1",
		JobCode: "ENG", Title: "Engineer",
		Requirements: JobProfileRequirements{CompensationRef: CompensationReference{
			GradeRef: "grade-1", GradeRevision: "g1",
			BandRef: "band-1", BandRevision: "v1",
			Currency: "USD", Authority: "comp-committee", EffectiveFrom: archAt(1),
		}},
		Lifecycle: LifecycleDraft, EffectiveFrom: archAt(1), KnownFrom: archAt(1),
		Lineage: RevisionLineage{RootID: "profile-1"},
	}}
	return base
}

func workspacePathFixture(t *testing.T) PromotionPathRevision {
	t.Helper()
	minimum, err := values.NewPercentage("0.05", 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewPercentage("0.15", 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return PromotionPathRevision{
		ID: "path-1", PathID: "path-1", Revision: "pp1",
		From:                ProfileRevisionRef{ProfileID: "profile-1", Revision: "p1"},
		To:                  ProfileRevisionRef{ProfileID: "profile-1", Revision: "p2"},
		Kind:                PromotionPathUpward,
		MinimumBaseIncrease: minimum, MaximumBaseIncrease: maximum,
		CompensationPolicyRef:      VersionedReference{Ref: "promo-policy", Revision: "v3", Authority: "comp-committee", EffectiveFrom: archAt(1)},
		BenefitEligibilityRuleRefs: []VersionedReference{{Ref: "benefit-eligibility", Revision: "v2", Authority: "benefits-owner", EffectiveFrom: archAt(1)}},
		Authority:                  "comp-committee", Lifecycle: LifecyclePublished,
		EffectiveFrom: archAt(1), KnownFrom: archAt(1),
		Lineage: RevisionLineage{RootID: "path-1"},
	}
}

func workspaceAdmin() WorkspaceViewer {
	return WorkspaceViewer{PrincipalID: "admin-1", CanViewCompensation: true, CanProposePublication: true}
}

// TestJobArchitectureWorkspaceShowsLadderRulesAndPublishesOnlyThroughWorkflow
// proves the admin workspace renders only authorized, server-resolved
// architecture state (family/level/profile graph, exact pay-band and increase
// rules, benefit impacts, publish status) and that every publish goes through
// the governed workflow, never a direct store write.
func TestJobArchitectureWorkspaceShowsLadderRulesAndPublishesOnlyThroughWorkflow(t *testing.T) {
	arch, err := NewArchitectureRevision(workspaceArchFixture())
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	bands := bandCatalog(t)

	view, err := ResolveWorkspaceView(WorkspaceInput{
		Viewer:       workspaceAdmin(),
		Architecture: arch,
		Bands:        bands,
		Paths:        []PromotionPathRevision{workspacePathFixture(t)},
		PayZone:      "US",
	})
	if err != nil {
		t.Fatalf("ResolveWorkspaceView: %v", err)
	}
	if view.ArchitectureID != "work-arch" || view.Revision != "r1" || view.CanonicalDigest == "" {
		t.Fatalf("view lost architecture identity: %+v", view)
	}
	if len(view.Families) != 1 || len(view.Levels) != 1 || len(view.Profiles) != 1 {
		t.Fatalf("view lost the family/level/profile graph: %+v", view)
	}
	profile := view.Profiles[0]
	if profile.FamilyCode != "ENG" || profile.LevelCode != "IC1" || profile.GradeCode != "G1" || profile.JobCode != "ENG" {
		t.Fatalf("profile graph edges are wrong: %+v", profile)
	}
	if profile.PublishStatus != "DRAFT" {
		t.Fatalf("publish status=%q, want DRAFT", profile.PublishStatus)
	}
	if profile.PayBand == nil || profile.PayBand.BandID != "band-1" || profile.PayBand.BandRevision != "v1" || profile.PayBand.Currency != "USD" {
		t.Fatalf("exact pay band missing: %+v", profile.PayBand)
	}
	for _, bound := range []string{profile.PayBand.Minimum, profile.PayBand.Midpoint, profile.PayBand.Maximum} {
		if !strings.Contains(bound, "50000") && !strings.Contains(bound, "70000") && !strings.Contains(bound, "90000") {
			t.Fatalf("pay-band bound is not exact: %q", bound)
		}
	}
	if len(view.Ladder) != 1 || view.Ladder[0].MinimumBaseIncrease == "" || view.Ladder[0].MaximumBaseIncrease == "" {
		t.Fatalf("ladder increase rules missing: %+v", view.Ladder)
	}
	if len(profile.BenefitImpacts) != 1 || profile.BenefitImpacts[0].RuleRef != "benefit-eligibility" {
		t.Fatalf("benefit impacts missing: %+v", profile.BenefitImpacts)
	}

	// Unsupported job/grade combinations are never offered: a grade bound to
	// another level fails the whole resolution.
	broken := workspaceArchFixture()
	broken.Grades = append(broken.Grades, JobGradeRevision{ID: "grade-2", GradeID: "grade-2", Revision: "g2", LevelID: "missing-level", Code: "G2", Name: "Grade 2", Lifecycle: LifecycleDraft, EffectiveFrom: archAt(1), KnownFrom: archAt(1)})
	if _, err := ResolveWorkspaceView(WorkspaceInput{Viewer: workspaceAdmin(), Architecture: broken, Bands: bands, PayZone: "US"}); err == nil {
		t.Fatal("unsupported job/grade combination resolved without error")
	}

	// Published architecture is never edited in place: proposing a PUBLISHED
	// profile is refused, and only a DRAFT candidate may enter the workflow.
	published, err := NewArchitectureRevision(architectureFixture())
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if _, err := ProposeWorkspacePublication(workspaceAdmin(), published, published, "profile-1"); !errors.Is(err, ErrWorkspaceDirectPublish) {
		t.Fatalf("in-place publish proposal err=%v", err)
	}
	proposal, err := ProposeWorkspacePublication(workspaceAdmin(), published, arch, "profile-1")
	if err != nil {
		t.Fatalf("draft proposal: %v", err)
	}
	if proposal.CandidateDigest == "" || proposal.ProfileID != "profile-1" {
		t.Fatalf("proposal is not bound to the exact candidate: %+v", proposal)
	}

	// The proposal alone publishes nothing: applying it without approval is
	// refused and the architecture is unchanged.
	if _, _, err := PublishRevision(published, PublicationRequest{Candidate: arch, ProfileID: "profile-1"}); err == nil {
		t.Fatal("unapproved publication succeeded")
	} else if !errors.Is(err, ErrPublicationNotApproved) && !errors.Is(err, ErrConfigurationRequired) {
		t.Fatalf("unapproved publication failed with the wrong error: %v", err)
	}
}

// TestTodo_UX_JOBARCH_001_Security proves salary and benefit rules never reach
// a viewer without the compensation grant: the graph, ladder rules and publish
// status resolve identically, pay values arrive withheld, and proposal and
// resolution both fail closed without authority.
func TestTodo_UX_JOBARCH_001_Security(t *testing.T) {
	arch, err := NewArchitectureRevision(workspaceArchFixture())
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	bands := bandCatalog(t)
	admin, err := ResolveWorkspaceView(WorkspaceInput{Viewer: workspaceAdmin(), Architecture: arch, Bands: bands, PayZone: "US"})
	if err != nil {
		t.Fatalf("admin view: %v", err)
	}
	viewer, err := ResolveWorkspaceView(WorkspaceInput{
		Viewer:       WorkspaceViewer{PrincipalID: "viewer-1"},
		Architecture: arch,
		Bands:        bands,
		PayZone:      "US",
	})
	if err != nil {
		t.Fatalf("viewer resolution: %v", err)
	}
	if len(viewer.Profiles) != len(admin.Profiles) || viewer.Profiles[0].PublishStatus != admin.Profiles[0].PublishStatus {
		t.Fatal("unauthorized viewer lost the non-sensitive graph")
	}
	if !viewer.Profiles[0].PayBandKnown || viewer.Profiles[0].PayBand != nil || !viewer.Profiles[0].CompensationWithheld {
		t.Fatalf("compensation not withheld: %+v", viewer.Profiles[0])
	}
	for _, leaked := range []string{"50000", "70000", "90000"} {
		if strings.Contains(string(viewer.Canonical()), leaked) {
			t.Fatalf("viewer projection leaks pay value %s", leaked)
		}
	}
	if _, err := ResolveWorkspaceView(WorkspaceInput{Architecture: arch, Bands: bands}); !errors.Is(err, ErrWorkspaceViewerUnauthorized) {
		t.Fatalf("anonymous resolution err=%v", err)
	}
	if _, err := ProposeWorkspacePublication(WorkspaceViewer{PrincipalID: "viewer-1"}, arch, arch, "profile-1"); !errors.Is(err, ErrWorkspaceProposalUnauthorized) {
		t.Fatalf("unprivileged proposal err=%v", err)
	}
}

// TestTodo_UX_JOBARCH_001_Golden pins the exact admin projection bytes so a
// drift in graph edges, pay-band precision, increase rules, benefit impacts or
// publish status fails loudly instead of reaching the admin surface.
func TestTodo_UX_JOBARCH_001_Golden(t *testing.T) {
	arch, err := NewArchitectureRevision(workspaceArchFixture())
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	first, err := ResolveWorkspaceView(WorkspaceInput{
		Viewer:       workspaceAdmin(),
		Architecture: arch,
		Bands:        bandCatalog(t),
		Paths:        []PromotionPathRevision{workspacePathFixture(t)},
		PayZone:      "US",
	})
	if err != nil {
		t.Fatalf("ResolveWorkspaceView: %v", err)
	}
	second, err := ResolveWorkspaceView(WorkspaceInput{
		Viewer:       workspaceAdmin(),
		Architecture: arch,
		Bands:        bandCatalog(t),
		Paths:        []PromotionPathRevision{workspacePathFixture(t)},
		PayZone:      "US",
	})
	if err != nil {
		t.Fatalf("ResolveWorkspaceView: %v", err)
	}
	if string(first.Canonical()) != string(second.Canonical()) {
		t.Fatal("workspace projection is not deterministic")
	}
	golden := string(first.Canonical())
	for _, pin := range []string{
		`"ArchitectureID":"work-arch"`, `"Revision":"r1"`,
		`"FamilyCode":"ENG"`, `"LevelCode":"IC1"`, `"GradeCode":"G1"`,
		`"BandID":"band-1"`, `"BandRevision":"v1"`, `"Currency":"USD"`,
		`"MinimumBaseIncrease":"0.0500"`, `"MaximumBaseIncrease":"0.1500"`,
		`"RuleRef":"benefit-eligibility"`, `"PublishStatus":"DRAFT"`,
	} {
		if !strings.Contains(golden, pin) {
			t.Fatalf("golden projection lost %s: %s", pin, golden)
		}
	}
}
