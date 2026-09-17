package legal

// LEGAL-ST-NC-001 verification tests.
//
// The North Carolina draft is agent-verified, not counsel-reviewed: the
// pack stays UNREVIEWED and unusable under any nonzero tenant review
// floor. These tests pin the verification half of the review — every
// GREEN parameter the research supports, the corrected state-agency-only
// salary-history rule, the matrix row matched — and the guardrail that
// keeps the pack out of evaluation until counsel approves it. They do
// not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func northCarolinaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "NC").Candidate()
	if err != nil {
		t.Fatalf("Candidate(NC): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_NC_001 verifies the North Carolina draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_NC_001(t *testing.T) {
	def := loadStateDraft(t, "NC")
	if def.PackID != "us-nc-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the North Carolina draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "NC" {
		t.Fatalf("subdivision=%q, want NC", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := northCarolinaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// Federal-only floor: no state amount, hourly basis.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, North Carolina states no minimum", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	// Hire notice plus the 24-hour pre-decrease rule; the exact timing of
	// the 2021 amendment is still under legal review, so the marker stays
	// VERIFY.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	noticeCite := draftObligation(t, "NC", "us-nc-notice").Citation
	if !strings.Contains(noticeCite.Section, "95-25.13") {
		t.Fatalf("notice citation=%+v, want section 95-25.13", noticeCite)
	}
	if noticeCite.ConfidenceMarker != "VERIFY" {
		t.Fatalf("notice marker=%q, the amendment timing is still under review", noticeCite.ConfidenceMarker)
	}
	if !strings.Contains(noticeCite.Note, "under legal review") {
		t.Fatalf("notice note=%q, want the completed timing caveat", noticeCite.Note)
	}
	// Both leave interactions the GREEN clause names: school-involvement
	// and domestic-violence protective-order leave.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if leave.LeaveType != "school involvement leave" {
		t.Fatalf("leave type=%q, want the school-involvement rule", leave.LeaveType)
	}
	if !strings.Contains(leave.InteractionRule, "50B-5.5") || !strings.Contains(leave.InteractionRule, "denial of promotion") {
		t.Fatalf("leave rule=%q, want the domestic-violence protection", leave.InteractionRule)
	}
	// REDA plus the sickle-cell/genetic and lawful-product protections:
	// three protected activities, blocking disposition.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	retaliation := pack.AntiRetaliationRules[0]
	for _, activity := range []string{"workers_compensation_claim", "osha_complaint", "wage_and_hour_complaint"} {
		if !hasString(retaliation.ProtectedActivities, activity) {
			t.Fatalf("activities=%v, want %q", retaliation.ProtectedActivities, activity)
		}
	}
	if retaliation.Disposition != "BLOCK" {
		t.Fatalf("disposition=%q, want BLOCK", retaliation.Disposition)
	}
	// Drug-testing procedures with the statutory reliability standards.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	if len(pack.DrugTestingRules[0].PermittedBases) != 4 {
		t.Fatalf("permitted bases=%v, want the four statutory bases", pack.DrugTestingRules[0].PermittedBases)
	}
	// E-Verify at the 25-employee threshold, verified within 3 business days.
	if len(pack.EVerifyChecks) != 1 || !pack.EVerifyChecks[0].RequiredOnNewHireOnly {
		t.Fatalf("e-verify=%+v, want the 25+ new-hire check", pack.EVerifyChecks)
	}
	if got := draftObligation(t, "NC", "us-nc-e-verify"); got.Body.EmployeeThreshold != 25 {
		t.Fatalf("e-verify threshold=%d, want the 25-employee floor", got.Body.EmployeeThreshold)
	}
	// Breach notice names the Attorney General referral.
	if got := draftObligation(t, "NC", "us-nc-breach-notification").Citation.Note; !strings.Contains(got, "Attorney General") {
		t.Fatalf("breach note=%q, want the AG referral", got)
	}
	// Non-competes live in matrix F: North Carolina employment
	// restraints stand on common law plus the section 75-4 validation of
	// written contracts, with no employment-specific statutory threshold
	// for the pack to carry — so the kind stays absent.
	if len(pack.NonCompeteThresholds) != 0 {
		t.Fatalf("non-competes=%+v, matrix F carries none", pack.NonCompeteThresholds)
	}
	// No truncated extraction notes survive the verification pass.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nc.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	for _, stub := range []string{"verify exact...\",", "employer-set); support labor...\",", "shortly before...\",", "Examination Regulation):\",", "Protection Act):\","} {
		if strings.Contains(body, stub) {
			t.Errorf("pack still carries truncated note %q", stub)
		}
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_NC_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_NC_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nc.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "north-carolina.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var digest string
	for _, line := range strings.Split(string(want), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		digest = line
	}
	if got != digest {
		t.Fatalf("draft bytes moved:\n got=%q\nwant=%q", got, digest)
	}
}

// TestTodo_LEGAL_ST_NC_001_Conformance checks the North Carolina matrix
// row against the pack and proves every registered state draft still
// loads and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_NC_001_Conformance(t *testing.T) {
	pack := northCarolinaPack(t)
	// Table A Y cells: NOTICE, LEAVE. Table B Y cells: FINAL_PAY,
	// E_VERIFY, ANTI_RETAL, BREACH_NOTIFICATION. FIELD_RESTRICTION,
	// RETENTION, PAY_FREQUENCY, PAY_STATEMENT and DRUG_TESTING are `?`
	// with evidence, kept at VERIFY. The federal-only WAGE_FLOOR is the
	// review's explicit addition for the F cell.
	counts := pack.KindCounts()
	for _, kind := range []ObligationType{ObligationTypeNotice, ObligationTypeLeaveInteraction, ObligationTypeFinalPayDeadline, ObligationTypeEVerify, ObligationTypeAntiRetaliation, ObligationTypeBreachNotification, ObligationTypeFieldRestriction, ObligationTypeRetention, ObligationTypePayFrequency, ObligationTypePayStatement, ObligationTypeDrugTesting, ObligationTypeWageFloor} {
		if counts[kind] == 0 {
			t.Errorf("matrix Y/`?`-with-evidence cell %s has no pack obligation", kind)
		}
	}
	// Table A/B F cells the flow consumes must stay absent: pay
	// transparency, non-competes, classifications, pay equity, personnel
	// files, mini-WARN, job security. LOCAL is `?` with no evidence, so
	// no locality overlay and no preemption assertion either way.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.NonCompeteThresholds) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 ||
		len(pack.PersonnelFileRules) != 0 || len(pack.MiniWARNTriggers) != 0 ||
		len(pack.JobSecurityRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	if len(pack.PreemptionAssertions) != 0 {
		t.Fatalf("preemptions=%+v, want none", pack.PreemptionAssertions)
	}
	// Every registered state draft loads and validates.
	for _, code := range allStatePackCodes() {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_NC_001_Mutation seeds mutants into the North Carolina
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_NC_001_Mutation(t *testing.T) {
	mutants := []struct {
		name   string
		mutate func(*PackDefinition)
	}{
		{"schema version drift", func(d *PackDefinition) { d.SchemaVersion = 99 }},
		{"missing pack id", func(d *PackDefinition) { d.PackID = "" }},
		{"unknown jurisdiction level", func(d *PackDefinition) { d.Jurisdiction.Level = "GALAXY" }},
		{"unknown obligation kind", func(d *PackDefinition) { d.Obligations[0].Kind = "VIBES" }},
		{"citation without a section", func(d *PackDefinition) { d.Obligations[0].Citation.Section = "" }},
		{"inverted effective window", func(d *PackDefinition) { d.Window.End = "2025-01-01" }},
		// The REDA review is the retaliation guard: without an id it
		// must not load.
		{"anti-retaliation without an id", func(d *PackDefinition) {
			findObligationByKind(d, "ANTI_RETALIATION").ID = ""
		}},
		// A final-pay deadline must name its trigger.
		{"final pay without a trigger", func(d *PackDefinition) {
			findObligationByKind(d, "FINAL_PAY_DEADLINE").Body.Trigger = ""
		}},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			rejectMutant(t, "NC", m.mutate)
		})
	}
}
