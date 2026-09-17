package legal

// LEGAL-ST-MD-001 verification tests.
//
// The Maryland draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the personnel-file resolution carried
// as a visible VERIFY ?-emission that grants nothing, the Montgomery
// County overlay carried as locality context on the state floor, the
// matrix row matched — and the guardrail that keeps the pack out of
// evaluation until counsel approves it. They do not, and must not, mark
// the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func marylandPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "MD").Candidate()
	if err != nil {
		t.Fatalf("Candidate(MD): %v", err)
	}
	return candidate.Pack()
}

// checkMarylandPack runs the PRIMARY assertions over a draft definition.
// The committed test loads the checked-in file; pre-registration staging
// runs this same function over the staged bytes, so both verify the
// identical contract.
func checkMarylandPack(t *testing.T, def PackDefinition) RulePack {
	t.Helper()
	if def.PackID != "us-md-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Maryland draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "MD" {
		t.Fatalf("subdivision=%q, want MD", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	candidate, err := def.Candidate()
	if err != nil {
		t.Fatalf("Candidate(MD): %v", err)
	}
	pack := candidate.Pack()
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $15.00/hr state floor (Md. Code, Lab. & Empl. § 3-413)
	// with the Montgomery County three-tier local overlay carried as
	// locality context ($17.65/$16.00/$15.50 by employer size).
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
	if !strings.Contains(floor.Citation.Note, "17.65") {
		t.Fatalf("note=%q, want the Montgomery County overlay carried", floor.Citation.Note)
	}
	if !strings.Contains(floor.Citation.Section, "3-413") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-413", floor.Citation.Section)
	}
	// NOTICE: written notice at least 1 pay period before a payday or
	// rate decrease; none required for increases (§ 3-504(a)).
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written notice", pack.Notices)
	}
	notice := pack.Notices[0]
	if notice.TimingDirection != "BEFORE" {
		t.Fatalf("direction=%q, want BEFORE", notice.TimingDirection)
	}
	if !hasString(notice.ContentFields, "pay_rate") || !hasString(notice.ContentFields, "payday") {
		t.Fatalf("notice=%+v, want rate and payday content carried", notice)
	}
	if !strings.Contains(notice.Citation.Note, "1 pay period") {
		t.Fatalf("note=%q, want the 1-pay-period rule carried", notice.Citation.Note)
	}
	if !strings.Contains(notice.Citation.Note, "increase") {
		t.Fatalf("note=%q, want the increase exception carried", notice.Citation.Note)
	}
	if !strings.Contains(notice.Citation.Section, "3-504") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-504(a)", notice.Citation.Section)
	}
	// PAY_TRANSPARENCY: wage-range-plus-benefits in all postings
	// (eff. 2024-10), salary-history ban (§§ 3-304.1, 3-304.2).
	if len(pack.PayTransparencyDuties) != 1 || pack.PayTransparencyDuties[0].Trigger != "internal_promotion" {
		t.Fatalf("pay transparency=%+v, want the posting duty", pack.PayTransparencyDuties)
	}
	pt := pack.PayTransparencyDuties[0]
	if !strings.Contains(pt.RequiredDisclosure, "benefits") {
		t.Fatalf("disclosure=%q, want benefits carried", pt.RequiredDisclosure)
	}
	if !strings.Contains(pt.Citation.Note, "2024-10") {
		t.Fatalf("note=%q, want the 2024-10 effective date carried", pt.Citation.Note)
	}
	if !strings.Contains(pt.Citation.Section, "3-304.2") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-304.2", pt.Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban (§ 3-304.1).
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "3-304.1") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-304.1", pack.FieldRestrictions[0].Citation.Section)
	}
	// LEAVE_INTERACTION: earned sick and safe leave at 1hr/30hrs with a
	// 40-hour cap, paid at 15+ employees (§ 3-1301 et seq.), plus FAMLI
	// 12-24-week paid leave at 90% wage replacement with contributions
	// from 2027-01-01 (Title 8.3).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if leave.LeaveType != "earned sick and safe leave" {
		t.Fatalf("leave type=%q, want earned sick and safe leave", leave.LeaveType)
	}
	if !strings.Contains(leave.InteractionRule, "40 hours") {
		t.Fatalf("leave=%q, want the 40-hour cap carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "Title 8.3") {
		t.Fatalf("leave=%q, want the FAMLI program carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "2027-01-01") {
		t.Fatalf("leave=%q, want the 2027-01-01 contribution start carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.Citation.Section, "3-1301") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-1301", leave.Citation.Section)
	}
	// NON_COMPETE: void at or below 150% of state minimum wage
	// ($22.50/hr, $46,800/yr), banned for healthcare workers under
	// $350,000 (§ 3-716).
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Rule, "22.50") || !strings.Contains(nc.Rule, "46,800") {
		t.Fatalf("rule=%q, want the 150%% wage threshold carried", nc.Rule)
	}
	if !strings.Contains(nc.Rule, "350,000") {
		t.Fatalf("rule=%q, want the healthcare-worker ban carried", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "3-716") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-716", nc.Citation.Section)
	}
	// MINI_WARN: 50+-employee 60-day notice for 25%-or-15-worker
	// reductions over 3 months, penalties up to $10,000/day
	// (§ 11-301 et seq.).
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	mini := pack.MiniWARNTriggers[0]
	if mini.EmployeeThreshold != 50 || mini.NoticeDays != 60 {
		t.Fatalf("mini-warn=%+v, want the 50-employee 60-day trigger", mini)
	}
	if !strings.Contains(mini.Note, "10,000") {
		t.Fatalf("mini-warn=%q, want the penalty carried", mini.Note)
	}
	if !strings.Contains(mini.Citation.Section, "11-301") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 11-301", mini.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: all earned wages on or before the next regular
	// payday following termination (§ 3-505).
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-payday rule", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "3-505") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-505", finalPay.Citation.Section)
	}
	// PAY_EQUITY_REVIEW: sex/gender equal work (§ 3-304).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	pe := pack.PayEquityReviews[0]
	if !hasString(pe.ProtectedBases, "sex") || !hasString(pe.ProtectedBases, "gender") {
		t.Fatalf("bases=%+v, want the sex/gender bases § 3-304 states", pe.ProtectedBases)
	}
	if pe.ComparatorStandard != "equal work" {
		t.Fatalf("comparator=%q, want equal work", pe.ComparatorStandard)
	}
	if !strings.Contains(pe.Citation.Section, "3-304") || strings.Contains(pe.Citation.Section, "3-304.") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-304", pe.Citation.Section)
	}
	// RETENTION (? with evidence): 3-year payroll records (§ 3-424),
	// carried at VERIFY.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year payroll rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "3-424") {
		t.Fatalf("section=%q, want Md. Code, Lab. & Empl. § 3-424", pack.RetentionRules[0].Citation.Section)
	}
	if pack.RetentionRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("retention must stay VERIFY: the matrix marks RETENTION ?")
	}
	// PAY_FREQUENCY (? with evidence): biweekly floor (§ 3-502),
	// carried at VERIFY.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "BIWEEKLY" {
		t.Fatalf("pay frequency=%+v, want the biweekly floor", pack.PayFrequencyConstraints)
	}
	if pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// PAY_STATEMENT (? with evidence): no state itemized-stub mandate
	// (LEGAL-TOOL-005 records NOT_MANDATED), carried at VERIFY.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want the ?-cell emission", len(pack.PayStatements))
	}
	if pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay statement must stay VERIFY: the matrix marks PAY_STMT ?")
	}
	// PERSONNEL_FILE: the confirmed resolution — no private-sector
	// statutory access right — carried as a visible VERIFY ?-emission,
	// correctly granting nothing.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want the resolution carried, not dropped", len(pack.PersonnelFileRules))
	}
	pf := pack.PersonnelFileRules[0]
	if pf.ResponseDays != 0 || pf.FrequencyCapPerYear != 0 || pf.CopyFeePermitted {
		t.Fatalf("personnel file=%+v, the resolution grants nothing", pf)
	}
	if !strings.Contains(pf.Citation.Note, "private-sector statutory") {
		t.Fatalf("note=%q, want the no-statutory-right resolution carried", pf.Citation.Note)
	}
	if pf.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("personnel file must stay VERIFY: there is no private-sector right to confirm")
	}
	// ANTI_RETALIATION (Y with hedged evidence): BLOCK disposition,
	// carried at VERIFY.
	if len(pack.AntiRetaliationRules) != 1 || pack.AntiRetaliationRules[0].Disposition != "BLOCK" {
		t.Fatalf("anti-retaliation=%+v, want the BLOCK rule", pack.AntiRetaliationRules)
	}
	if pack.AntiRetaliationRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("anti-retaliation must stay VERIFY: its evidence is hedged")
	}
	// DRUG_TESTING (? with evidence): no state statute, federal bases,
	// carried at VERIFY.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the ?-cell emission", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	for _, want := range []string{"pre_employment", "reasonable_suspicion", "post_accident"} {
		if !hasString(dt.PermittedBases, want) {
			t.Fatalf("bases=%+v, want %q carried", dt.PermittedBases, want)
		}
	}
	if dt.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: no state statute is stated")
	}
	// BREACH_NOTIFICATION: "without unreasonable delay"
	// (Com. Law § 14-3501); employee-data applicability is unverified,
	// so the marker stays VERIFY and no day count is typed.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	bn := pack.BreachNotifications[0]
	if bn.SubjectDeadlineDays != 0 {
		t.Fatalf("deadline=%d, the statute states no day count", bn.SubjectDeadlineDays)
	}
	if !strings.Contains(bn.Citation.Section, "14-3501") {
		t.Fatalf("section=%q, want Com. Law § 14-3501", bn.Citation.Section)
	}
	if bn.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("breach must stay VERIFY: employee-data applicability is unverified")
	}
	// F cells stay absent: classifications, e-verify, separation filings
	// (? NONE_IDENTIFIED), job security, automated decisions, monitoring
	// consents.
	if len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
	return pack
}

// TestTodo_LEGAL_ST_MD_001 verifies the Maryland draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_MD_001(t *testing.T) {
	checkMarylandPack(t, loadStateDraft(t, "MD"))
}

// TestTodo_LEGAL_ST_MD_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_MD_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-md.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "maryland.golden.txt"))
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

// TestTodo_LEGAL_ST_MD_001_Conformance checks the Maryland matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_MD_001_Conformance(t *testing.T) {
	pack := marylandPack(t)
	// Table A Y cells: NOTICE, PAY_TRANSPARENCY, FIELD_RESTRICTION,
	// WAGE_FLOOR, LEAVE, NON_COMPETE, PAY_EQUITY. Table B Y cells:
	// FINAL_PAY, MINI_WARN, ANTI_RETALIATION, BREACH.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: classifications. Table B F cells stay
	// absent: e-verify, job security, automated decisions. SEP_FILING is
	// ? with no identified form (LEGAL-TOOL-008 NONE_IDENTIFIED). The
	// Montgomery County overlay rides the locality family, never the
	// subdivision pack inline.
	if len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
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

// TestTodo_LEGAL_ST_MD_001_Mutation seeds mutants into the Maryland
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_MD_001_Mutation(t *testing.T) {
	checkPackMutation(t, loadStateDraft(t, "MD"))
}
