package legal

// LEGAL-ST-MI-001 verification tests.
//
// The Michigan draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, including the corrected small-employer
// ESTA dual entitlement (40 paid hours plus 32 unpaid hours, both, not
// either) and the MCL 445.774a non-compete rule the F cell leaves
// untyped, the matrix row matched — and the guardrail that keeps the pack
// out of evaluation until counsel approves it. They do not, and must not,
// mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func michiganPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "MI").Candidate()
	if err != nil {
		t.Fatalf("Candidate(MI): %v", err)
	}
	return candidate.Pack()
}

// checkMichiganPack runs the PRIMARY assertions over a draft definition.
// The committed test loads the checked-in file; pre-registration staging
// runs this same function over the staged bytes, so both verify the
// identical contract.
func checkMichiganPack(t *testing.T, def PackDefinition) RulePack {
	t.Helper()
	if def.PackID != "us-mi-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Michigan draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "MI" {
		t.Fatalf("subdivision=%q, want MI", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	candidate, err := def.Candidate()
	if err != nil {
		t.Fatalf("Candidate(MI): %v", err)
	}
	pack := candidate.Pack()
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $13.73/hr (2026), scheduled to $14.05/hr (2027-10),
	// tipped wage phasing from 40% to 100% of minimum by 2030
	// (Improved Workforce Opportunity Wage Act, MCL 408.934a/d).
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "13.73 USD" {
		t.Fatalf("floor=%q, want the $13.73/hr 2026 floor", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if !strings.Contains(floor.Citation.Note, "14.05") {
		t.Fatalf("note=%q, want the $14.05 scheduled step carried", floor.Citation.Note)
	}
	if !strings.Contains(floor.Citation.Note, "40%") {
		t.Fatalf("note=%q, want the tipped-wage phase carried", floor.Citation.Note)
	}
	if !strings.Contains(floor.Citation.Section, "408.934") {
		t.Fatalf("section=%q, want MCL 408.934", floor.Citation.Section)
	}
	// LEAVE_INTERACTION: Earned Sick Time Act — 72 hours paid for 11+
	// employees; for 10-or-fewer employers, 40 hours paid PLUS 32 hours
	// unpaid (both, not either), eff. 2025-10-01 (MCL 408.963).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.InteractionRule, "72 hours") {
		t.Fatalf("leave=%q, want the 72-hour large-employer entitlement carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "40 hours paid") {
		t.Fatalf("leave=%q, want the 40-hour small-employer paid tier carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "32 hours unpaid") {
		t.Fatalf("leave=%q, want the additive 32-hour unpaid tier carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "2025-10-01") {
		t.Fatalf("leave=%q, want the 2025-10-01 small-employer date carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.Citation.Section, "408.963") {
		t.Fatalf("section=%q, want MCL 408.963", leave.Citation.Section)
	}
	// NON_COMPETE: MCL 445.774a reasonableness test with blue-pencil.
	// The matrix marks NON_COMPETE F, so the extractor types nothing;
	// the review carries the rule the research states.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Rule, "reasonable") {
		t.Fatalf("rule=%q, want the reasonableness test carried", nc.Rule)
	}
	if !strings.Contains(nc.Rule, "blue-pencil") {
		t.Fatalf("rule=%q, want the blue-pencil remedy carried", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "445.774a") {
		t.Fatalf("section=%q, want MCL 445.774a", nc.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: immediate on discharge once the amount can
	// with due diligence be determined; next regular payday on
	// resignation (MCL 408.475).
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-payday rule", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "408.475") {
		t.Fatalf("section=%q, want MCL 408.475", finalPay.Citation.Section)
	}
	// NOTICE (Y with no citable section): written pay-change notice,
	// carried at VERIFY rather than dropped.
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written notice", pack.Notices)
	}
	if !hasString(pack.Notices[0].ContentFields, "pay_frequency") {
		t.Fatalf("notice=%+v, want the pay-frequency content carried", pack.Notices[0])
	}
	if pack.Notices[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("notice must stay VERIFY: no research item states a section")
	}
	// RETENTION: 3-year payroll records (MCL 408.479).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year payroll rule", pack.RetentionRules)
	}
	if pack.RetentionRules[0].RecordClass != "payroll_records" {
		t.Fatalf("retention=%+v, want payroll records", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "408.479") {
		t.Fatalf("section=%q, want MCL 408.479", pack.RetentionRules[0].Citation.Section)
	}
	// PAY_FREQUENCY (? with evidence): monthly-split-or-weekly/biweekly
	// payday rule (MCL 408.472) with no single minimum, carried at VERIFY
	// with the gap visible.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency=%d, want the ?-cell emission", len(pack.PayFrequencyConstraints))
	}
	if pack.PayFrequencyConstraints[0].MinimumFrequency != "" {
		t.Fatalf("frequency=%q, the rule states no single minimum", pack.PayFrequencyConstraints[0].MinimumFrequency)
	}
	if pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// PERSONNEL_FILE: Bullard-Plawecki inspection and copies
	// (MCL 423.501-512). Reasonable time states no day count.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	pf := pack.PersonnelFileRules[0]
	if !pf.CopyFeePermitted {
		t.Fatalf("personnel file=%+v, want the copy fee carried", pf)
	}
	if pf.ResponseDays != 0 {
		t.Fatalf("response=%d, reasonable time states no day count", pf.ResponseDays)
	}
	if !strings.Contains(pf.Citation.Section, "423.5") {
		t.Fatalf("section=%q, want MCL 423.501-512", pf.Citation.Section)
	}
	// PAY_STATEMENT (? with a LEGAL-TOOL-005 MANDATORY resolution):
	// itemized statement each payday (MCL 408.479), carried at VERIFY.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want the resolved mandate carried", len(pack.PayStatements))
	}
	ps := pack.PayStatements[0]
	for _, want := range []string{"hours_worked", "gross_wages", "deductions"} {
		if !hasString(ps.RequiredFields, want) {
			t.Fatalf("fields=%+v, want %q carried", ps.RequiredFields, want)
		}
	}
	if ps.Delivery != "EITHER" || ps.ConsentRequired {
		t.Fatalf("pay statement=%+v, want EITHER delivery without consent", ps)
	}
	if !strings.Contains(ps.Citation.Section, "408.479") {
		t.Fatalf("section=%q, want MCL 408.479", ps.Citation.Section)
	}
	if ps.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay statement must stay VERIFY: the matrix marks PAY_STMT ?")
	}
	// ANTI_RETALIATION: Whistleblowers' Protection Act (MCL 15.361 et
	// seq.) with workers-compensation retaliation coverage.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	for _, want := range []string{"workers_compensation_claim", "whistleblower_report"} {
		if !hasString(ar.ProtectedActivities, want) {
			t.Fatalf("activities=%+v, want %q carried", ar.ProtectedActivities, want)
		}
	}
	if !strings.Contains(ar.Citation.Section, "15.361") {
		t.Fatalf("section=%q, want MCL 15.361", ar.Citation.Section)
	}
	// DRUG_TESTING (? with evidence): no state statute, wide flexibility,
	// carried at VERIFY. The research denies THC protection, so none is
	// typed — the AL cannabis contradiction is not repeated here.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the ?-cell emission", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	if len(dt.ProtectedStatus) != 0 {
		t.Fatalf("protected=%+v, the research grants no testing protection", dt.ProtectedStatus)
	}
	if dt.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: no state statute is stated")
	}
	// BREACH_NOTIFICATION: "without unreasonable delay"
	// (MCL 445.72). The statute states no day count, so none is typed.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	bn := pack.BreachNotifications[0]
	if bn.SubjectDeadlineDays != 0 {
		t.Fatalf("deadline=%d, the statute states no day count", bn.SubjectDeadlineDays)
	}
	if !strings.Contains(bn.Citation.Section, "445.72") {
		t.Fatalf("section=%q, want MCL 445.72", bn.Citation.Section)
	}
	// F cells stay absent: pay transparency, field restrictions (the
	// Detroit/Washtenaw County ban-the-box rules are locality-level),
	// pay equity, classifications, e-verify, mini-warn, job security,
	// automated decisions, monitoring consents. SEP_FILING is ? with no
	// identified form (LEGAL-TOOL-008 NONE_IDENTIFIED).
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.PayEquityReviews) != 0 ||
		len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.MiniWARNTriggers) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 ||
		len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
	return pack
}

// TestTodo_LEGAL_ST_MI_001 verifies the Michigan draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_MI_001(t *testing.T) {
	checkMichiganPack(t, loadStateDraft(t, "MI"))
}

// TestTodo_LEGAL_ST_MI_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_MI_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-mi.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "michigan.golden.txt"))
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

// TestTodo_LEGAL_ST_MI_001_Conformance checks the Michigan matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_MI_001_Conformance(t *testing.T) {
	pack := michiganPack(t)
	// Table A Y cells: NOTICE, WAGE_FLOOR, LEAVE, RETENTION,
	// PERSONNEL_FILE. Table B Y cells: FINAL_PAY, ANTI_RETALIATION,
	// BREACH. NON_COMPETE and PAY_STATEMENT ride along as reviewed
	// resolutions of an F cell and a TOOL-005 ? cell.
	if len(pack.Notices) == 0 || len(pack.WageFloors) == 0 || len(pack.LeaveInteractions) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayStatements) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: pay transparency, field restrictions
	// (locality ban-the-box rides the locality family), pay equity,
	// classifications. Table B F cells stay absent: e-verify, mini-warn,
	// job security, automated decisions.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.PayEquityReviews) != 0 ||
		len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.MiniWARNTriggers) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 ||
		len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_MI_001_Mutation seeds mutants into the Michigan
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_MI_001_Mutation(t *testing.T) {
	checkPackMutation(t, loadStateDraft(t, "MI"))
}
