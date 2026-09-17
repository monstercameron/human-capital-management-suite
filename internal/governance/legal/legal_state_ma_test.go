package legal

// LEGAL-ST-MA-001 verification tests.
//
// The Massachusetts draft is agent-verified, not counsel-reviewed: the
// pack stays UNREVIEWED and unusable under any nonzero tenant review
// floor. These tests pin the verification half of the review — every
// GREEN parameter the research supports, including the two distinct
// pay-transparency duties under M.G.L. c. 149 § 105F that a single
// trigger field cannot represent, the matrix row matched — and the
// guardrail that keeps the pack out of evaluation until counsel approves
// it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func massachusettsPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "MA").Candidate()
	if err != nil {
		t.Fatalf("Candidate(MA): %v", err)
	}
	return candidate.Pack()
}

// checkMassachusettsPack runs the PRIMARY assertions over a draft
// definition. The committed test loads the checked-in file;
// pre-registration staging runs this same function over the staged bytes,
// so both verify the identical contract.
func checkMassachusettsPack(t *testing.T, def PackDefinition) RulePack {
	t.Helper()
	if def.PackID != "us-ma-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Massachusetts draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "MA" {
		t.Fatalf("subdivision=%q, want MA", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	candidate, err := def.Candidate()
	if err != nil {
		t.Fatalf("Candidate(MA): %v", err)
	}
	pack := candidate.Pack()
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: state minimum with no local overlay identified.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "15.00 USD" {
		t.Fatalf("floor=%q, want the $15.00/hr state floor", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if !strings.Contains(floor.Citation.Note, "no local overlay") {
		t.Fatalf("note=%q, want the no-local-overlay finding carried", floor.Citation.Note)
	}
	if !strings.Contains(floor.Citation.Section, "151") {
		t.Fatalf("section=%q, want M.G.L. c. 151 § 1", floor.Citation.Section)
	}
	// PAY_TRANSPARENCY: two duties from one statute (M.G.L. c. 149
	// § 105F), each with its own effective date. A single trigger field
	// cannot represent both, so the pack carries both.
	if len(pack.PayTransparencyDuties) != 2 {
		t.Fatalf("pay transparency duties=%d, want the posting and reporting duties", len(pack.PayTransparencyDuties))
	}
	posting := pack.PayTransparencyDuties[0]
	if posting.Trigger != "internal_promotion" {
		t.Fatalf("posting trigger=%q, want internal_promotion", posting.Trigger)
	}
	if !strings.Contains(posting.RequiredDisclosure, "25+") {
		t.Fatalf("posting=%q, want the 25-employer threshold carried", posting.RequiredDisclosure)
	}
	if !strings.Contains(posting.Citation.Note, "2025-10-29") {
		t.Fatalf("posting=%q, want the 2025-10-29 posting duty date carried", posting.Citation.Note)
	}
	if !strings.Contains(posting.Citation.Section, "105F") {
		t.Fatalf("posting section=%q, want M.G.L. c. 149 § 105F", posting.Citation.Section)
	}
	reporting := pack.PayTransparencyDuties[1]
	if !strings.Contains(reporting.RequiredDisclosure, "100+") {
		t.Fatalf("reporting=%q, want the 100-employer threshold carried", reporting.RequiredDisclosure)
	}
	if !strings.Contains(reporting.Citation.Note, "2025-02-01") {
		t.Fatalf("reporting=%q, want the 2025-02-01 reporting duty date carried", reporting.Citation.Note)
	}
	if !strings.Contains(reporting.Citation.Section, "105F") {
		t.Fatalf("reporting section=%q, want M.G.L. c. 149 § 105F", reporting.Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban (M.G.L. c. 149 § 105A).
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "105A") {
		t.Fatalf("section=%q, want M.G.L. c. 149 § 105A", pack.FieldRestrictions[0].Citation.Section)
	}
	if pack.FieldRestrictions[0].Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("salary-history ban must read CONFIRMED: the research states the ban plainly")
	}
	// LEAVE_INTERACTION: 40-hour paid sick time for 11+ employers
	// (accrual after 90 days, M.G.L. c. 149 § 148C) plus PFML
	// job-protected leave with the 0.88% payroll contribution shifting
	// fully to the employer 2027-01-01 (c. 175M). The PFMLA duration
	// tension keeps the marker at VERIFY.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.InteractionRule, "40 hours") {
		t.Fatalf("leave=%q, want the 40-hour sick entitlement carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "90") {
		t.Fatalf("leave=%q, want the 90-day accrual gate carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "0.88%") {
		t.Fatalf("leave=%q, want the 0.88%% contribution carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "2027-01-01") {
		t.Fatalf("leave=%q, want the 2027-01-01 contribution shift carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "175M") {
		t.Fatalf("leave=%q, want the c. 175M program carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.Citation.Section, "148C") {
		t.Fatalf("section=%q, want M.G.L. c. 149 § 148C", leave.Citation.Section)
	}
	if leave.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("leave must stay VERIFY: the research hedges PFMLA durations")
	}
	// FINAL_PAY_DEADLINE: same-day on discharge, next payday on
	// resignation, treble-damages strict liability, 3-year statute of
	// limitations (Wage Act, M.G.L. c. 149 §§ 148, 150).
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "same day") {
		t.Fatalf("deadline=%q, want the same-day discharge rule", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "treble") {
		t.Fatalf("deadline=%q, want treble damages carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "3-year") {
		t.Fatalf("deadline=%q, want the 3-year limitations period carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "150") {
		t.Fatalf("section=%q, want M.G.L. c. 149 § 150", finalPay.Citation.Section)
	}
	// NON_COMPETE: mandatory garden leave at 50% of salary with
	// reasonable duration, geography and scope (M.G.L. c. 149 § 24L).
	// The "likely unenforceable" hedge keeps the marker at VERIFY.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Rule, "50%") {
		t.Fatalf("rule=%q, want the 50%% garden leave carried", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "24L") {
		t.Fatalf("section=%q, want M.G.L. c. 149 § 24L", nc.Citation.Section)
	}
	if nc.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("non-compete must stay VERIFY: the research hedges enforceability")
	}
	// PERSONNEL_FILE: 10-day negative-information notice,
	// 5-business-day access, 3-year post-termination retention for 20+
	// employers (c. 149 § 52C).
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	pf := pack.PersonnelFileRules[0]
	if pf.ResponseDays != 5 || pf.DayBasis != "BUSINESS" {
		t.Fatalf("personnel file=%+v, want the 5-business-day access rule", pf)
	}
	if pf.FrequencyCapPerYear != 2 {
		t.Fatalf("personnel file=%+v, want the 2-inspection yearly cap carried", pf)
	}
	if !strings.Contains(pf.Citation.Note, "10 days") {
		t.Fatalf("note=%q, want the 10-day negative-information notice carried", pf.Citation.Note)
	}
	if !strings.Contains(pf.Citation.Section, "52C") {
		t.Fatalf("section=%q, want M.G.L. c. 149 § 52C", pf.Citation.Section)
	}
	// RETENTION: 3-year post-termination personnel retention
	// (M.G.L. c. 149 §§ 52C, 49).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year rule", pack.RetentionRules)
	}
	if pack.RetentionRules[0].DurationBasis != "employment_plus_years" {
		t.Fatalf("basis=%q, want employment_plus_years", pack.RetentionRules[0].DurationBasis)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "52C") {
		t.Fatalf("section=%q, want M.G.L. c. 149 § 52C", pack.RetentionRules[0].Citation.Section)
	}
	// PAY_FREQUENCY (? with evidence): weekly floor (§ 148),
	// carried at VERIFY.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "WEEKLY" {
		t.Fatalf("pay frequency=%+v, want the weekly floor", pack.PayFrequencyConstraints)
	}
	if pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// MINI_WARN (? with evidence): no state mini-WARN act, carried at
	// VERIFY rather than dropped.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want the ?-cell emission", len(pack.MiniWARNTriggers))
	}
	if !strings.Contains(pack.MiniWARNTriggers[0].Note, "No state mini-WARN") {
		t.Fatalf("mini-warn=%q, want the no-state-act finding carried", pack.MiniWARNTriggers[0].Note)
	}
	if pack.MiniWARNTriggers[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("mini-warn must stay VERIFY: the matrix marks MINI_WARN ?")
	}
	// PAY_EQUITY_REVIEW: different-gender comparable work (§ 105A).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	pe := pack.PayEquityReviews[0]
	if len(pe.ProtectedBases) != 1 || pe.ProtectedBases[0] != "gender" {
		t.Fatalf("bases=%+v, want exactly the gender basis § 105A states", pe.ProtectedBases)
	}
	if pe.ComparatorStandard != "comparable work" {
		t.Fatalf("comparator=%q, want comparable work", pe.ComparatorStandard)
	}
	if !strings.Contains(pe.Citation.Section, "105A") {
		t.Fatalf("section=%q, want M.G.L. c. 149 § 105A", pe.Citation.Section)
	}
	// PAY_STATEMENT (? with evidence): no detailed state stub mandate
	// (LEGAL-TOOL-005 records NOT_MANDATED), carried at VERIFY.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want the ?-cell emission", len(pack.PayStatements))
	}
	if pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay statement must stay VERIFY: the matrix marks PAY_STMT ?")
	}
	// ANTI_RETALIATION (? with evidence): PFML-interaction flag,
	// carried at VERIFY.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want the ?-cell emission", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !hasString(ar.ProtectedActivities, "protected_leave_request") {
		t.Fatalf("activities=%+v, want the leave-request protection carried", ar.ProtectedActivities)
	}
	if ar.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("anti-retaliation must stay VERIFY: the matrix marks ANTI_RETALIATION ?")
	}
	// DRUG_TESTING (? with evidence): no state statute, federal bases,
	// carried at VERIFY.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the ?-cell emission", len(pack.DrugTestingRules))
	}
	if pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: no state statute is stated")
	}
	// F cells stay absent: notices, classifications, e-verify, separation
	// filings (? NONE_IDENTIFIED), job security, automated decisions,
	// monitoring consents. BREACH is ? with no Implications evidence, so
	// no duty is typed.
	if len(pack.Notices) != 0 || len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 ||
		len(pack.MonitoringConsents) != 0 || len(pack.BreachNotifications) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
	return pack
}

// TestTodo_LEGAL_ST_MA_001 verifies the Massachusetts draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_MA_001(t *testing.T) {
	checkMassachusettsPack(t, loadStateDraft(t, "MA"))
}

// TestTodo_LEGAL_ST_MA_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_MA_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ma.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "massachusetts.golden.txt"))
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

// TestTodo_LEGAL_ST_MA_001_Conformance checks the Massachusetts matrix
// row against the pack and proves every registered state draft still
// loads and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_MA_001_Conformance(t *testing.T) {
	pack := massachusettsPack(t)
	// Table A Y cells: PAY_TRANSPARENCY, FIELD_RESTRICTION, WAGE_FLOOR,
	// LEAVE, NON_COMPETE, PAY_EQUITY, RETENTION, PERSONNEL_FILE. Table B
	// Y cells: FINAL_PAY, ANTI_RETALIATION.
	if len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 || len(pack.WageFloors) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.AntiRetaliationRules) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: notices, classifications. Table B F
	// cells stay absent: e-verify, job security, automated decisions.
	// SEP_FILING is ? with no identified form (LEGAL-TOOL-008
	// NONE_IDENTIFIED); BREACH is ? with no Implications evidence.
	if len(pack.Notices) != 0 || len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 ||
		len(pack.MonitoringConsents) != 0 || len(pack.BreachNotifications) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_MA_001_Mutation seeds mutants into the Massachusetts
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_MA_001_Mutation(t *testing.T) {
	checkPackMutation(t, loadStateDraft(t, "MA"))
}
