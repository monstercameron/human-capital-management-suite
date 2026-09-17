package legal

// LEGAL-ST-ME-001 verification tests.
//
// The Maine draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports (the 2026-09-03 rework's actual
// numbers: $15.10 floor, $7.33 tip credit, 400%-FPL non-compete bar,
// EPL and PFMLA terms), the matrix row matched — and the guardrail that
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

func mainePack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "ME").Candidate()
	if err != nil {
		t.Fatalf("Candidate(ME): %v", err)
	}
	return candidate.Pack()
}

// checkMainePack runs the PRIMARY assertions over a draft definition.
// The committed test loads the checked-in file; pre-registration staging
// runs this same function over the staged bytes, so both verify the
// identical contract.
func checkMainePack(t *testing.T, def PackDefinition) RulePack {
	t.Helper()
	if def.PackID != "us-me-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Maine draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "ME" {
		t.Fatalf("subdivision=%q, want ME", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	candidate, err := def.Candidate()
	if err != nil {
		t.Fatalf("Candidate(ME): %v", err)
	}
	pack := candidate.Pack()
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $15.10/hr (2026) with the $7.33 tip credit,
	// 26 M.R.S. §§ 664, 665.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "15.10 USD" {
		t.Fatalf("floor=%q, want the $15.10/hr 2026 floor", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if !strings.Contains(floor.Citation.Note, "7.33") {
		t.Fatalf("note=%q, want the $7.33 tip credit carried", floor.Citation.Note)
	}
	if !strings.Contains(floor.Citation.Section, "664") {
		t.Fatalf("section=%q, want 26 M.R.S. § 664", floor.Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban, 26 M.R.S. § 628-A.
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "628-A") {
		t.Fatalf("section=%q, want 26 M.R.S. § 628-A", pack.FieldRestrictions[0].Citation.Section)
	}
	// NOTICE (? with evidence): at-hire written notice of rate, hours,
	// payday, pay method and leave policies, 26 M.R.S. § 621-A.
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written-notice ? emission", pack.Notices)
	}
	notice := pack.Notices[0]
	if !hasString(notice.ContentFields, "pay_rate") || !hasString(notice.ContentFields, "payday") {
		t.Fatalf("notice=%+v, want rate and payday content carried", notice)
	}
	if !strings.Contains(notice.Citation.Section, "621-A") {
		t.Fatalf("section=%q, want 26 M.R.S. § 621-A", notice.Citation.Section)
	}
	if notice.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("notice must stay VERIFY: the matrix marks NOTICE ?")
	}
	// PAY_FREQUENCY: weekly minimum (? cell carried at VERIFY),
	// 26 M.R.S. § 621-A.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "WEEKLY" {
		t.Fatalf("pay frequency=%+v, want the weekly floor", pack.PayFrequencyConstraints)
	}
	if pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// FINAL_PAY_DEADLINE: next regular payday, 26 M.R.S. §§ 621-A, 626.
	// The discharge-timing hedge keeps the marker at VERIFY.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-payday rule", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "626") {
		t.Fatalf("section=%q, want 26 M.R.S. § 626", finalPay.Citation.Section)
	}
	if finalPay.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("final pay must stay VERIFY: the research hedges discharge timing")
	}
	// LEAVE_INTERACTION: Earned Paid Leave at 1hr/40hrs with a 40-hour
	// minimum for 10+ employers and mandatory payout on termination
	// (26 M.R.S. § 637), plus PFMLA 12-week paid leave at 90%/66% wage
	// replacement with benefits from 2026-05-01 (26 M.R.S. § 850-A).
	// The PFMLA pending items keep the marker at VERIFY.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.InteractionRule, "40 hours") {
		t.Fatalf("leave=%q, want the 40-hour EPL minimum carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "850-A") {
		t.Fatalf("leave=%q, want the PFMLA citation carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.Citation.Section, "637") {
		t.Fatalf("section=%q, want 26 M.R.S. § 637", leave.Citation.Section)
	}
	if leave.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("leave must stay VERIFY: the research hedges PFMLA edges")
	}
	// NON_COMPETE: void at or below ~$62,920/yr (400% federal poverty
	// level), 3-business-day pre-signing notice, 1-2 year term cap,
	// 26 M.R.S. §§ 599-A, 599-B.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Rule, "62,920") {
		t.Fatalf("rule=%q, want the $62,920 threshold carried", nc.Rule)
	}
	if !strings.Contains(nc.Rule, "3 business days") {
		t.Fatalf("rule=%q, want the 3-day pre-signing notice carried", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "599-A") {
		t.Fatalf("section=%q, want 26 M.R.S. § 599-A", nc.Citation.Section)
	}
	// PERSONNEL_FILE: 6-year retention alongside, employee access with a
	// reasonable copy fee, 26 M.R.S. §§ 630-A, 631.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	pf := pack.PersonnelFileRules[0]
	if pf.ResponseDays != 10 {
		t.Fatalf("response=%d, want the 10-day access rule", pf.ResponseDays)
	}
	if !pf.CopyFeePermitted {
		t.Fatalf("personnel file=%+v, want the copy fee carried", pf)
	}
	if !strings.Contains(pf.Citation.Section, "631") {
		t.Fatalf("section=%q, want 26 M.R.S. § 631", pf.Citation.Section)
	}
	// RETENTION: 6-year payroll records, 26 M.R.S. § 630-A.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 6 {
		t.Fatalf("retention=%+v, want the 6-year payroll rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "630-A") {
		t.Fatalf("section=%q, want 26 M.R.S. § 630-A", pack.RetentionRules[0].Citation.Section)
	}
	// MINI_WARN: 100+-employee-worksite closures, 90-day notice plus
	// 1-week-per-year severance up to 26 weeks, 26 M.R.S. § 635.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	mini := pack.MiniWARNTriggers[0]
	if mini.EmployeeThreshold != 100 || mini.NoticeDays != 90 {
		t.Fatalf("mini-warn=%+v, want the 100-employee 90-day trigger", mini)
	}
	if !strings.Contains(mini.Note, "26 weeks") {
		t.Fatalf("mini-warn=%q, want the 26-week severance cap carried", mini.Note)
	}
	if !strings.Contains(mini.Citation.Section, "635") {
		t.Fatalf("section=%q, want 26 M.R.S. § 635", mini.Citation.Section)
	}
	// PAY_EQUITY_REVIEW: sex-based equal pay, 26 M.R.S. § 628.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	pe := pack.PayEquityReviews[0]
	if len(pe.ProtectedBases) != 1 || pe.ProtectedBases[0] != "sex" {
		t.Fatalf("bases=%+v, want exactly the sex basis § 628 states", pe.ProtectedBases)
	}
	if pe.ComparatorStandard != "equal work" {
		t.Fatalf("comparator=%q, want equal work", pe.ComparatorStandard)
	}
	if !strings.Contains(pe.Citation.Section, "628") || strings.Contains(pe.Citation.Section, "628-A") {
		t.Fatalf("section=%q, want 26 M.R.S. § 628, not the history-ban section", pe.Citation.Section)
	}
	// PAY_STATEMENT: itemized stubs each payday, electronic with consent,
	// 26 M.R.S. §§ 630-A, 631.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	ps := pack.PayStatements[0]
	for _, want := range []string{"gross_wages", "deductions", "net_wages"} {
		if !hasString(ps.RequiredFields, want) {
			t.Fatalf("fields=%+v, want %q carried", ps.RequiredFields, want)
		}
	}
	if ps.Delivery != "EITHER" || !ps.ConsentRequired {
		t.Fatalf("pay statement=%+v, want EITHER delivery with consent", ps)
	}
	// CLASSIFICATION: economic-realities contractor test,
	// 26 M.R.S. § 602. No salary threshold is stated for the test.
	if len(pack.Classifications) != 1 || pack.Classifications[0].Dimension != "CONTRACTOR" {
		t.Fatalf("classifications=%+v, want the contractor test", pack.Classifications)
	}
	classn := pack.Classifications[0]
	if !strings.Contains(classn.TestDescription, "economic realities") {
		t.Fatalf("test=%q, want the economic-realities test carried", classn.TestDescription)
	}
	if got := classn.SalaryThreshold.String(); got != "" {
		t.Fatalf("threshold=%q, the test states no salary threshold", got)
	}
	if !strings.Contains(classn.Citation.Section, "602") {
		t.Fatalf("section=%q, want 26 M.R.S. § 602", classn.Citation.Section)
	}
	// ANTI_RETALIATION: § 626-B public-policy exceptions, blocking.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	for _, want := range []string{"whistleblower_report", "jury_duty", "wage_claim"} {
		if !hasString(ar.ProtectedActivities, want) {
			t.Fatalf("activities=%+v, want %q carried", ar.ProtectedActivities, want)
		}
	}
	if ar.Disposition != "BLOCK" {
		t.Fatalf("disposition=%q, want BLOCK", ar.Disposition)
	}
	if !strings.Contains(ar.Citation.Section, "626-B") {
		t.Fatalf("section=%q, want 26 M.R.S. § 626-B", ar.Citation.Section)
	}
	// DRUG_TESTING (? with evidence): § 683 testing with written policy
	// notice, carried at VERIFY.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	for _, want := range []string{"pre_employment", "reasonable_suspicion", "post_accident"} {
		if !hasString(dt.PermittedBases, want) {
			t.Fatalf("bases=%+v, want %q carried", dt.PermittedBases, want)
		}
	}
	if !strings.Contains(dt.Citation.Section, "683") {
		t.Fatalf("section=%q, want 26 M.R.S. § 683", dt.Citation.Section)
	}
	if dt.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the research hedges § 683 edges")
	}
	// BREACH_NOTIFICATION: 30-day notice with AG notice at 100+
	// residents, 10 M.R.S. § 1348.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	bn := pack.BreachNotifications[0]
	if bn.SubjectDeadlineDays != 30 {
		t.Fatalf("deadline=%d, want the 30-day rule", bn.SubjectDeadlineDays)
	}
	if bn.AuthorityThresholdCount != 100 {
		t.Fatalf("threshold=%d, want the 100-resident AG trigger", bn.AuthorityThresholdCount)
	}
	if !strings.Contains(bn.Citation.Section, "1348") {
		t.Fatalf("section=%q, want 10 M.R.S. § 1348", bn.Citation.Section)
	}
	// F cells stay absent: pay transparency, e-verify, separation
	// filings, job security, automated decisions, monitoring consents.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
	return pack
}

// TestTodo_LEGAL_ST_ME_001 verifies the Maine draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_ME_001(t *testing.T) {
	checkMainePack(t, loadStateDraft(t, "ME"))
}

// TestTodo_LEGAL_ST_ME_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_ME_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-me.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "maine.golden.txt"))
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

// TestTodo_LEGAL_ST_ME_001_Conformance checks the Maine matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_ME_001_Conformance(t *testing.T) {
	pack := mainePack(t)
	// Table A Y cells: FIELD_RESTRICTION, WAGE_FLOOR, PAY_FREQUENCY,
	// PAY_STATEMENT, LEAVE, NON_COMPETE, CLASSIFICATION, PAY_EQUITY,
	// RETENTION, PERSONNEL_FILE. Table B Y cells: FINAL_PAY, MINI_WARN,
	// ANTI_RETALIATION, BREACH.
	if len(pack.FieldRestrictions) == 0 || len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 ||
		len(pack.PayStatements) == 0 || len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.Classifications) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: pay transparency. Table B F cells stay
	// absent: e-verify, job security, automated decisions. SEP_FILING is
	// ? with no identified form (LEGAL-TOOL-008 NONE_IDENTIFIED).
	if len(pack.PayTransparencyDuties) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_ME_001_Mutation seeds mutants into the Maine draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_ME_001_Mutation(t *testing.T) {
	checkPackMutation(t, loadStateDraft(t, "ME"))
}
