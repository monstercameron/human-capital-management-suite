package legal

// LEGAL-ST-NE-001 verification tests.
//
// The Nebraska draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the corrected common-law non-compete
// encoding, the state mini-WARN layered on (not restating) the federal
// rule — and the guardrail that keeps the pack out of evaluation until
// counsel approves it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nebraskaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "NE").Candidate()
	if err != nil {
		t.Fatalf("Candidate(NE): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_NE_001 verifies the Nebraska draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_NE_001(t *testing.T) {
	def := loadStateDraft(t, "NE")
	if def.PackID != "us-ne-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Nebraska draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "NE" {
		t.Fatalf("subdivision=%q, want NE", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := nebraskaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// $15.00 from 2026-01-01 with the scheduled 1.75% annual increases
	// from 2027. The floor binds every worker: the extractor's tipped
	// subclass described the $2.13 cash wage, not the floor, so the
	// verified body carries no worker class.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "15.00 USD" {
		t.Fatalf("floor=%q, want the $15.00 2026-01-01 rate", got)
	}
	if floor.WorkerClass != "" {
		t.Fatalf("worker class=%q, the floor is general", floor.WorkerClass)
	}
	if floor.Indexation != "SCHEDULE" {
		t.Fatalf("indexation=%q, want the scheduled 1.75%% adjustments", floor.Indexation)
	}
	if got := floor.NextAdjustmentDate.String(); got != "2027-01-01" {
		t.Fatalf("next adjustment=%q, want 2027-01-01", got)
	}
	// Thirty days' written notice before payday changes.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDirection != "BEFORE" || notice.TimingDays != 30 {
		t.Fatalf("notice=%+v, want 30 days BEFORE the change", notice)
	}
	// Paid sick leave preserved across the promotion: 1hr/30hrs,
	// 11+-employee threshold, eff. 2025-10-01.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if leave.LeaveType != "paid sick leave" {
		t.Fatalf("leave type=%q, want paid sick leave", leave.LeaveType)
	}
	if !strings.Contains(leave.InteractionRule, "1 hour per 30 hours") {
		t.Fatalf("leave rule=%q, want the accrual rate", leave.InteractionRule)
	}
	leaveCite := draftObligation(t, "NE", "us-ne-leave-interaction").Citation
	if !strings.Contains(leaveCite.Section, "48-3801") {
		t.Fatalf("leave citation=%+v, want the Healthy Families and Workplaces Act", leaveCite)
	}
	// Final pay counts accrued PTO as wages.
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	if got := pack.FinalPayDeadlines[0].DeadlineDescription; !strings.Contains(got, "PTO counts as wages") {
		t.Fatalf("deadline=%q, want PTO counted as wages", got)
	}
	// The state mini-WARN layers on the federal rule: 90 days at 25+
	// same-day separations, not a restatement of 60-day/100-employee WARN.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-WARN triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.EmployeeThreshold != 25 || warn.NoticeDays != 90 {
		t.Fatalf("mini-WARN=%+v, want 25 employees and 90 days", warn)
	}
	// Sex-based equal-pay review with documentation.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	review := pack.PayEquityReviews[0]
	if !review.DocumentationRequired || !hasString(review.ProtectedBases, "sex") {
		t.Fatalf("review=%+v, want a documented sex-based review", review)
	}
	// No frequency mandate: the extractor's semi-monthly floor overstated
	// the research, so the verified body carries no minimum frequency.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	if got := pack.PayFrequencyConstraints[0].MinimumFrequency; got != "" {
		t.Fatalf("minimum frequency=%q, Nebraska mandates none", got)
	}
	// Wage-disclosure and equal-pay retaliation with exact sections.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	retaliation := pack.AntiRetaliationRules[0]
	for _, activity := range []string{"wage_disclosure", "equal_pay_complaint"} {
		if !hasString(retaliation.ProtectedActivities, activity) {
			t.Fatalf("activities=%v, want %q", retaliation.ProtectedActivities, activity)
		}
	}
	retaliationCite := draftObligation(t, "NE", "us-ne-anti-retaliation").Citation
	if !strings.Contains(retaliationCite.Section, "48-1114") || !strings.Contains(retaliationCite.Section, "48-1221") {
		t.Fatalf("anti-retaliation citation=%+v, want both retaliation sections", retaliationCite)
	}
	// Non-competes are common law only after the LEGAL-018 correction
	// removed the section 87-404 franchise-law citation: matrix F carries
	// no statutory obligation.
	if len(pack.NonCompeteThresholds) != 0 {
		t.Fatalf("non-competes=%+v, matrix F carries none", pack.NonCompeteThresholds)
	}
	// E-Verify for public contractors is matrix L: a subdivision pack
	// carries no such obligation.
	if len(pack.EVerifyChecks) != 0 {
		t.Fatalf("e-verify=%+v, matrix L stays out of the subdivision pack", pack.EVerifyChecks)
	}
	// No truncated extraction notes survive the verification pass.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ne.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	for _, stub := range []string{"(Neb.\",", "\"Neb.\",", "published by...\",", "federal...\",", "Wage Statements: Neb.\",", "Equal Pay Law: Neb.\","} {
		if strings.Contains(body, stub) {
			t.Errorf("pack still carries truncated note %q", stub)
		}
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_NE_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_NE_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ne.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "nebraska.golden.txt"))
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

// TestTodo_LEGAL_ST_NE_001_Conformance checks the Nebraska matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_NE_001_Conformance(t *testing.T) {
	pack := nebraskaPack(t)
	// Table A Y cells: NOTICE, WAGE_FLOOR, LEAVE. Table B Y cells:
	// FINAL_PAY, MINI_WARN, ANTI_RETAL, BREACH_NOTIFICATION, plus
	// PAY_EQUITY from Table A. PAY_FREQ, PAY_STMT, SEP_FILING and
	// DRUG_TESTING are `?` with evidence, kept at VERIFY.
	counts := pack.KindCounts()
	for _, kind := range []ObligationType{ObligationTypeNotice, ObligationTypeWageFloor, ObligationTypeLeaveInteraction, ObligationTypeFinalPayDeadline, ObligationTypeMiniWARN, ObligationTypeAntiRetaliation, ObligationTypeBreachNotification, ObligationTypePayEquityReview, ObligationTypePayFrequency, ObligationTypePayStatement, ObligationTypeSeparationFiling, ObligationTypeDrugTesting} {
		if counts[kind] == 0 {
			t.Errorf("matrix Y/`?`-with-evidence cell %s has no pack obligation", kind)
		}
	}
	// Table A/B F cells the flow consumes must stay absent: field
	// restrictions, retention, pay transparency, non-competes,
	// classifications, personnel files, E-Verify, job security.
	if len(pack.FieldRestrictions) != 0 || len(pack.RetentionRules) != 0 ||
		len(pack.PayTransparencyDuties) != 0 || len(pack.NonCompeteThresholds) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 {
		t.Fatal("a matrix F/L cell gained a pack obligation")
	}
	// No preemption assertions: section 6.4 records none for Nebraska.
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

// TestTodo_LEGAL_ST_NE_001_Mutation seeds mutants into the Nebraska
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_NE_001_Mutation(t *testing.T) {
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
		// The floor moves on a schedule, not an unknown cadence.
		{"wage floor on an unknown indexation", func(d *PackDefinition) {
			findObligationByKind(d, "WAGE_FLOOR").Body.Indexation = "FORTUNE"
		}},
		// The payday-change notice fires BEFORE the change: an undeclared
		// timing direction must not validate.
		{"notice on an unknown timing direction", func(d *PackDefinition) {
			findObligationByKind(d, "NOTICE").Body.TimingDirection = "SOMETIME"
		}},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			rejectMutant(t, "NE", m.mutate)
		})
	}
}
