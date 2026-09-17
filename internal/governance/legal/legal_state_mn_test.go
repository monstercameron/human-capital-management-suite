package legal

// LEGAL-ST-MN-001 verification tests.
//
// The Minnesota draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, including the corrected § 181.173
// pay-range citation and the 48-hour overtime threshold distinct from
// every other state's 40-hour default, the MEAL_REST_BREAK rows carried
// by LEGAL-TOOL-002's registry, the matrix row matched — and the
// guardrail that keeps the pack out of evaluation until counsel approves
// it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/stateparams"
)

func minnesotaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "MN").Candidate()
	if err != nil {
		t.Fatalf("Candidate(MN): %v", err)
	}
	return candidate.Pack()
}

// checkMinnesotaPack runs the PRIMARY assertions over a draft definition.
// The committed test loads the checked-in file; pre-registration staging
// runs this same function over the staged bytes, so both verify the
// identical contract.
func checkMinnesotaPack(t *testing.T, def PackDefinition) RulePack {
	t.Helper()
	if def.PackID != "us-mn-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Minnesota draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "MN" {
		t.Fatalf("subdivision=%q, want MN", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	candidate, err := def.Candidate()
	if err != nil {
		t.Fatalf("Candidate(MN): %v", err)
	}
	pack := candidate.Pack()
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $11.41/hr statewide (2026) with the Minneapolis/St.
	// Paul $16.37/hr locality overlays carried as locality context
	// (§§ 177.24-177.25).
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "11.41 USD" {
		t.Fatalf("floor=%q, want the $11.41/hr statewide floor", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if !strings.Contains(floor.Citation.Note, "16.37") {
		t.Fatalf("note=%q, want the $16.37 locality overlay carried", floor.Citation.Note)
	}
	if !strings.Contains(floor.Citation.Section, "177.24") {
		t.Fatalf("section=%q, want Minn. Stat. §§ 177.24-177.25", floor.Citation.Section)
	}
	// MEAL_REST_BREAK: 30-minute meal and 15-minute rest rows carried
	// by LEGAL-TOOL-002's parameter registry (a vocabulary-3 kind the
	// v2 subdivision pack cannot type inline).
	checkMinnesotaBreaks(t)
	// CLASSIFICATION/overtime: 48-hour weekly threshold, not 40
	// (§§ 177.24-177.25). The exemption-verification hedge keeps the
	// marker at VERIFY.
	if len(pack.Classifications) != 1 || pack.Classifications[0].Dimension != "OVERTIME_THRESHOLD" {
		t.Fatalf("classifications=%+v, want the overtime threshold", pack.Classifications)
	}
	classn := pack.Classifications[0]
	if !strings.Contains(classn.TestDescription, "48-hour") {
		t.Fatalf("test=%q, want the 48-hour threshold carried", classn.TestDescription)
	}
	if !strings.Contains(classn.Citation.Section, "177.25") {
		t.Fatalf("section=%q, want Minn. Stat. § 177.25", classn.Citation.Section)
	}
	if classn.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("overtime classification must stay VERIFY: the research hedges exemption checks")
	}
	// NOTICE: written notice before the effective date of any
	// pay/basis/payday/deduction change, no same-day exception
	// (§ 181.032). The statute states no day count, so none is typed;
	// the language-preference hedge keeps the marker at VERIFY.
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written notice", pack.Notices)
	}
	notice := pack.Notices[0]
	if notice.TimingDays != 0 {
		t.Fatalf("timing=%d, the statute states no day count", notice.TimingDays)
	}
	if !hasString(notice.ContentFields, "pay_rate") || !hasString(notice.ContentFields, "effective_date") {
		t.Fatalf("notice=%+v, want rate and effective-date content carried", notice)
	}
	if !strings.Contains(notice.Citation.Section, "181.032") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.032", notice.Citation.Section)
	}
	if notice.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("notice must stay VERIFY: the research hedges notice edges")
	}
	// FIELD_RESTRICTION and PAY_TRANSPARENCY: salary-history ban
	// (§ 363A.08) plus pay-range posting for 30+ employers (§ 181.173,
	// corrected from the repealed § 181.9414).
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "363A.08") {
		t.Fatalf("section=%q, want Minn. Stat. § 363A.08", pack.FieldRestrictions[0].Citation.Section)
	}
	if len(pack.PayTransparencyDuties) != 1 || pack.PayTransparencyDuties[0].Trigger != "internal_promotion" {
		t.Fatalf("pay transparency=%+v, want the posting duty", pack.PayTransparencyDuties)
	}
	pt := pack.PayTransparencyDuties[0]
	if !strings.Contains(pt.RequiredDisclosure, "30+") {
		t.Fatalf("disclosure=%q, want the 30-employer threshold carried", pt.RequiredDisclosure)
	}
	if !strings.Contains(pt.Citation.Section, "181.173") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.173, not the repealed § 181.9414", pt.Citation.Section)
	}
	// LEAVE_INTERACTION: ESST at 1hr/30hrs with a 48-hour cap and
	// 80-hour carryover and no forfeiture on promotion
	// (§§ 181.9445-181.9448), plus PFML 12-week paid leave from
	// 2026-01-01 (ch. 268B). The PFMLA pending items keep the marker at
	// VERIFY.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if leave.LeaveType != "earned sick and safe leave" {
		t.Fatalf("leave type=%q, want earned sick and safe leave", leave.LeaveType)
	}
	if !strings.Contains(leave.InteractionRule, "48 hours") {
		t.Fatalf("leave=%q, want the 48-hour cap carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "80-hour") {
		t.Fatalf("leave=%q, want the 80-hour carryover carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.InteractionRule, "268B") {
		t.Fatalf("leave=%q, want the PFML program carried", leave.InteractionRule)
	}
	if !strings.Contains(leave.Citation.Section, "181.9445") {
		t.Fatalf("section=%q, want Minn. Stat. §§ 181.9445-181.9448", leave.Citation.Section)
	}
	if leave.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("leave must stay VERIFY: the research hedges PFMLA edges")
	}
	// NON_COMPETE: void except business sale or dissolution,
	// retroactively void from 2023-07-01 (§ 181.988).
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Rule, "void") {
		t.Fatalf("rule=%q, want the void rule carried", nc.Rule)
	}
	if !strings.Contains(nc.Rule, "2023-07-01") {
		t.Fatalf("rule=%q, want the retroactive date carried", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "181.988") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.988", nc.Citation.Section)
	}
	// PERSONNEL_FILE: twice-yearly inspection (current) and once-yearly
	// (former) with a 30-35-day response (§§ 181.960-181.966).
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	pf := pack.PersonnelFileRules[0]
	if pf.ResponseDays != 30 || pf.DayBasis != "CALENDAR" {
		t.Fatalf("personnel file=%+v, want the 30-calendar-day rule", pf)
	}
	if !strings.Contains(pf.Citation.Note, "35 days") {
		t.Fatalf("note=%q, want the 35-day extension carried", pf.Citation.Note)
	}
	if !strings.Contains(pf.Citation.Section, "181.960") {
		t.Fatalf("section=%q, want Minn. Stat. §§ 181.960-181.966", pf.Citation.Section)
	}
	// RETENTION: 3-year earnings and wage-history retention (§ 181.032).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year rule", pack.RetentionRules)
	}
	if pack.RetentionRules[0].RecordClass != "wage_records" {
		t.Fatalf("retention=%+v, want wage records", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "181.032") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.032", pack.RetentionRules[0].Citation.Section)
	}
	// PAY_FREQUENCY: payment at least once every 31 days (§ 181.101).
	// The "31 days" floor has no typed enum, so the gap stays visible.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency=%d, want the frequency rule", len(pack.PayFrequencyConstraints))
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Section, "181.101") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.101", pack.PayFrequencyConstraints[0].Citation.Section)
	}
	// FINAL_PAY_DEADLINE: 24-hour discharge rule and next-payday
	// resignation rule (§ 181.13/181.14). The pay-increase verification
	// hedge keeps the marker at VERIFY.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "24 hours") {
		t.Fatalf("deadline=%q, want the 24-hour rule carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "next payday") {
		t.Fatalf("deadline=%q, want the next-payday rule carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "181.13") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.13/181.14", finalPay.Citation.Section)
	}
	if finalPay.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("final pay must stay VERIFY: the research hedges pay-increase verification")
	}
	// PAY_STATEMENT: itemized statement each pay period (§ 181.032),
	// electronic acceptable, no consent gate.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	ps := pack.PayStatements[0]
	for _, want := range []string{"gross_wages", "net_wages", "deductions", "hours_worked"} {
		if !hasString(ps.RequiredFields, want) {
			t.Fatalf("fields=%+v, want %q carried", ps.RequiredFields, want)
		}
	}
	if ps.Delivery != "EITHER" || ps.ConsentRequired {
		t.Fatalf("pay statement=%+v, want EITHER delivery without consent", ps)
	}
	if !strings.Contains(ps.Citation.Section, "181.032") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.032", ps.Citation.Section)
	}
	// MINI_WARN (? with evidence): no state-specific WARN act, carried
	// at VERIFY rather than dropped.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want the ?-cell emission", len(pack.MiniWARNTriggers))
	}
	if !strings.Contains(pack.MiniWARNTriggers[0].Note, "no state-specific WARN") {
		t.Fatalf("mini-warn=%q, want the no-state-act finding carried", pack.MiniWARNTriggers[0].Note)
	}
	if pack.MiniWARNTriggers[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("mini-warn must stay VERIFY: the matrix marks MINI_WARN ?")
	}
	// PAY_EQUITY_REVIEW: sex-based equal work (§ 181.67).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	pe := pack.PayEquityReviews[0]
	if len(pe.ProtectedBases) != 1 || pe.ProtectedBases[0] != "sex" {
		t.Fatalf("bases=%+v, want exactly the sex basis § 181.67 states", pe.ProtectedBases)
	}
	if pe.ComparatorStandard != "equal work" {
		t.Fatalf("comparator=%q, want equal work", pe.ComparatorStandard)
	}
	if !strings.Contains(pe.Citation.Section, "181.67") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.67", pe.Citation.Section)
	}
	// ANTI_RETALIATION: wage-disclosure protection (§ 181.172), blocking.
	if len(pack.AntiRetaliationRules) != 1 || pack.AntiRetaliationRules[0].Disposition != "BLOCK" {
		t.Fatalf("anti-retaliation=%+v, want the BLOCK rule", pack.AntiRetaliationRules)
	}
	if !strings.Contains(pack.AntiRetaliationRules[0].Citation.Section, "181.172") {
		t.Fatalf("section=%q, want Minn. Stat. § 181.172", pack.AntiRetaliationRules[0].Citation.Section)
	}
	// DRUG_TESTING: safety-sensitive-only testing with off-duty cannabis
	// protection (§§ 181.950-181.957).
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	if !hasString(dt.PermittedBases, "safety_sensitive_role") {
		t.Fatalf("bases=%+v, want the safety-sensitive limit carried", dt.PermittedBases)
	}
	if !hasString(dt.ProtectedStatus, "lawful_off_duty_cannabis_use") {
		t.Fatalf("protected=%+v, want the off-duty cannabis protection carried", dt.ProtectedStatus)
	}
	if !strings.Contains(dt.Citation.Section, "181.950") {
		t.Fatalf("section=%q, want Minn. Stat. §§ 181.950-181.957", dt.Citation.Section)
	}
	// BREACH_NOTIFICATION: "without unreasonable delay"
	// (§ 325E.61). The statute states no day count and no monitoring
	// mandate, so neither is typed.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	bn := pack.BreachNotifications[0]
	if bn.SubjectDeadlineDays != 0 {
		t.Fatalf("deadline=%d, the statute states no day count", bn.SubjectDeadlineDays)
	}
	if bn.CreditMonitoringRequired {
		t.Fatal("monitoring is recommended-action content, not a mandate")
	}
	if !strings.Contains(bn.Citation.Section, "325E.61") {
		t.Fatalf("section=%q, want Minn. Stat. § 325E.61", bn.Citation.Section)
	}
	// F cells stay absent: e-verify, separation filings (? NONE_IDENTIFIED),
	// job security, automated decisions, monitoring consents.
	if len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
	return pack
}

// checkMinnesotaBreaks pins the LEGAL-TOOL-002 meal/rest registry rows
// the Minnesota GREEN depends on: a 30-minute unpaid meal row and a
// 15-minute paid rest row sourced to the Minnesota research file.
func checkMinnesotaBreaks(t *testing.T) {
	t.Helper()
	fixture, err := stateparams.LoadYAMLFile(filepath.Join("stateparams", "testdata", "state-parameters-wire.yaml"))
	if err != nil {
		t.Fatalf("LoadYAMLFile: %v", err)
	}
	var set stateparams.ParameterSet
	found := false
	for _, s := range fixture.ParameterSets {
		if s.StateCode == "MN" {
			set, found = s, true
		}
	}
	if !found {
		t.Fatal("Minnesota has no parameter set in the break registry")
	}
	if len(set.Breaks) != 2 {
		t.Fatalf("breaks=%d, want the meal and rest rows", len(set.Breaks))
	}
	var meal, rest *stateparams.BreakRule
	for i := range set.Breaks {
		switch set.Breaks[i].BreakType {
		case "MEAL":
			meal = &set.Breaks[i]
		case "REST":
			rest = &set.Breaks[i]
		}
	}
	if meal == nil || meal.DurationMinutes != 30 || meal.Paid {
		t.Fatalf("meal=%+v, want the 30-minute unpaid meal row", meal)
	}
	if rest == nil || rest.DurationMinutes != 15 || !rest.Paid {
		t.Fatalf("rest=%+v, want the 15-minute paid rest row", rest)
	}
	for _, b := range []*stateparams.BreakRule{meal, rest} {
		if !strings.Contains(b.Citation.SourceFile, "minnesota.md") {
			t.Fatalf("break=%+v, want the Minnesota research file sourced", b)
		}
	}
}

// TestTodo_LEGAL_ST_MN_001 verifies the Minnesota draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_MN_001(t *testing.T) {
	checkMinnesotaPack(t, loadStateDraft(t, "MN"))
}

// TestTodo_LEGAL_ST_MN_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_MN_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-mn.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "minnesota.golden.txt"))
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

// TestTodo_LEGAL_ST_MN_001_Conformance checks the Minnesota matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_MN_001_Conformance(t *testing.T) {
	pack := minnesotaPack(t)
	// Table A Y cells: NOTICE, PAY_TRANSPARENCY, FIELD_RESTRICTION,
	// WAGE_FLOOR, PAY_FREQUENCY, PAY_STATEMENT, LEAVE, NON_COMPETE,
	// CLASSIFICATION, PAY_EQUITY, RETENTION, PERSONNEL_FILE. Table B Y
	// cells: FINAL_PAY, DRUG_TESTING, ANTI_RETALIATION, BREACH. Breaks
	// ride LEGAL-TOOL-002's registry, verified in PRIMARY.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 || len(pack.Classifications) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.DrugTestingRules) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table B F cells stay absent: e-verify, job security, automated
	// decisions. SEP_FILING is ? with no identified form
	// (LEGAL-TOOL-008 NONE_IDENTIFIED). The Minneapolis/St. Paul overlay
	// rides the locality family, never the subdivision pack inline.
	if len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_MN_001_Mutation seeds mutants into the Minnesota
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_MN_001_Mutation(t *testing.T) {
	checkPackMutation(t, loadStateDraft(t, "MN"))
}
