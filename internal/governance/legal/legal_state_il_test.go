package legal

// LEGAL-ST-IL-001 verification tests.
//
// The Illinois draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the `?`-cell VERIFY markers carried
// rather than dropped, the corrected citations — and the guardrail that
// keeps the pack out of evaluation until counsel approves it. They do
// not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func illinoisPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "IL").Candidate()
	if err != nil {
		t.Fatalf("Candidate(IL): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_IL_001 verifies the Illinois draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_IL_001(t *testing.T) {
	def := loadStateDraft(t, "IL")
	if def.PackID != "us-il-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Illinois draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "IL" {
		t.Fatalf("subdivision=%q, want IL", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := illinoisPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// NOTICE: 1-full-pay-period advance notice of any pay-rate, basis,
	// payday, or deduction change (820 ILCS 115/3).
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDirection != "BEFORE" || notice.Channel != "written" {
		t.Fatalf("notice=%+v, want written advance notice", notice)
	}
	if !hasString(notice.ContentFields, "pay_rate") || !hasString(notice.ContentFields, "payday") {
		t.Fatalf("content=%v, want pay_rate and payday", notice.ContentFields)
	}
	if !strings.Contains(notice.Citation.Section, "115/3") {
		t.Fatalf("section=%q, want 820 ILCS 115/3", notice.Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban for 15+ employers (820 ILCS
	// 112) — not the credit-history rule (820 ILCS 70).
	if len(pack.FieldRestrictions) != 1 {
		t.Fatalf("field restrictions=%d, want one", len(pack.FieldRestrictions))
	}
	fields := pack.FieldRestrictions[0]
	if !hasString(fields.RestrictedFields, "salary_history") {
		t.Fatalf("restricted=%v, want salary_history", fields.RestrictedFields)
	}
	if !strings.Contains(fields.Citation.Section, "112") {
		t.Fatalf("section=%q, want 820 ILCS 112", fields.Citation.Section)
	}
	// PAY_TRANSPARENCY: pay scale in postings, salary-history screen ban,
	// pay-range disclosure, race-extended equal pay, and the 100+ Equal
	// Pay Registration Certificate (820 ILCS 112).
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	transparency := pack.PayTransparencyDuties[0]
	if !strings.Contains(transparency.Citation.Section, "112") {
		t.Fatalf("section=%q, want 820 ILCS 112", transparency.Citation.Section)
	}
	for _, want := range []string{"pay scale", "salary history", "Registration Certificate"} {
		if !strings.Contains(transparency.RequiredDisclosure, want) {
			t.Errorf("disclosure=%q, want %q", transparency.RequiredDisclosure, want)
		}
	}
	// LEAVE_INTERACTION: Paid Leave for All Workers Act 40-hour annual
	// minimum with no forfeiture on role change (820 ILCS 192).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.Citation.Section, "192") {
		t.Fatalf("section=%q, want 820 ILCS 192", leave.Citation.Section)
	}
	for _, want := range []string{"40 hours", "cannot be forfeited"} {
		if !strings.Contains(leave.InteractionRule, want) {
			t.Errorf("rule=%q, want %q", leave.InteractionRule, want)
		}
	}
	// PAY_FREQUENCY: at least semimonthly (§ 115/4).
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum=%q, want the semimonthly minimum", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "115/4") {
		t.Fatalf("section=%q, want § 115/4", freq.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: final-pay calculation with accrued vacation and
	// paid leave in the payout (§ 115/5) — not the leave-preservation
	// note citing 820 ILCS 192.
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "115/5") {
		t.Fatalf("section=%q, want § 115/5", finalPay.Citation.Section)
	}
	for _, want := range []string{"accrued vacation", "paid leave"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Errorf("deadline=%q, want %q", finalPay.DeadlineDescription, want)
		}
	}
	// NON_COMPETE: void below $75,000 (non-competes) / $45,000
	// (non-solicits) with scheduled increases through 2037 (820 ILCS 90).
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Citation.Section, "820 ILCS 90") {
		t.Fatalf("section=%q, want 820 ILCS 90", nc.Citation.Section)
	}
	for _, want := range []string{"$75,000", "$45,000"} {
		if !strings.Contains(nc.Rule, want) {
			t.Errorf("rule=%q, want threshold %s", nc.Rule, want)
		}
	}
	// MINI_WARN: 50+ employees in 30 days triggers a 60-day notice
	// (820 ILCS 65) — the notice period, not the layoff window.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.EmployeeThreshold != 50 || warn.NoticeDays != 60 {
		t.Fatalf("trigger=%+v, want the 50-employee 60-day rule", warn)
	}
	// WAGE_FLOOR: $15.00 state minimum with Chicago/Cook County locality
	// overlays noted for the locality stage (820 ILCS 105).
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "15.00 USD" {
		t.Fatalf("floor=%q, want the $15.00 state minimum", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	// PAY_EQUITY_REVIEW: sex- and race-based substantially-similar-work
	// review (820 ILCS 112) — not the temporary-labor rule (820 ILCS 175).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if !strings.Contains(equity.Citation.Section, "112") {
		t.Fatalf("section=%q, want 820 ILCS 112", equity.Citation.Section)
	}
	if !hasString(equity.ProtectedBases, "sex") || !hasString(equity.ProtectedBases, "race") {
		t.Fatalf("bases=%v, want sex and race", equity.ProtectedBases)
	}
	if equity.ComparatorStandard != "substantially similar work" {
		t.Fatalf("comparator=%q, want substantially similar work", equity.ComparatorStandard)
	}
	// PAY_STATEMENT carries the enhanced itemized-stub fields (§ 115/9).
	// The 3-year stub retention the todo prose names against a 5-year
	// payroll-retention research record is flagged for counsel: the draft
	// carries the research's cited 5-year payroll rule below.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	stmt := pack.PayStatements[0]
	for _, want := range []string{"gross_wages", "deductions", "net_wages"} {
		if !hasString(stmt.RequiredFields, want) {
			t.Errorf("fields=%v, want %s", stmt.RequiredFields, want)
		}
	}
	// RETENTION: 5-year payroll records (§ 115/14), as the research
	// states it.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 5 {
		t.Fatalf("retention=%+v, want the cited 5-year payroll rule", pack.RetentionRules)
	}
	// PERSONNEL_FILE: 7-business-day inspection window (820 ILCS 40).
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	personnel := pack.PersonnelFileRules[0]
	if personnel.ResponseDays != 7 || personnel.DayBasis != "BUSINESS" {
		t.Fatalf("personnel=%+v, want a 7-business-day window", personnel)
	}
	if !strings.Contains(personnel.Citation.Section, "820 ILCS 40") {
		t.Fatalf("section=%q, want 820 ILCS 40", personnel.Citation.Section)
	}
	// MONITORING_CONSENT: written BIPA release with the $1,000-$5,000
	// per-violation damages carried (740 ILCS 14).
	if len(pack.MonitoringConsents) != 1 {
		t.Fatalf("monitoring consents=%d, want one", len(pack.MonitoringConsents))
	}
	consent := pack.MonitoringConsents[0]
	if !strings.Contains(consent.Citation.Section, "740 ILCS 14") {
		t.Fatalf("section=%q, want 740 ILCS 14", consent.Citation.Section)
	}
	if !hasString(consent.DataCategories, "biometric_identifiers") || consent.ConsentForm != "WRITTEN" {
		t.Fatalf("consent=%+v, want written biometric consent", consent)
	}
	if !strings.Contains(consent.Citation.Note, "$1,000") {
		t.Fatalf("note=%q, want the per-violation damages carried", consent.Citation.Note)
	}
	// Matrix Y cells present: anti-retaliation; breach and
	// automated-decision stay honestly gapped (VERIFY, section not
	// stated) until the research evidences the PIPA timeline and the
	// zip-code-proxy ban.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if pack.BreachNotifications[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("breach must stay VERIFY: the research states no timeline")
	}
	if len(pack.AutomatedDecisions) != 1 {
		t.Fatalf("automated decisions=%d, want the gap carried, not dropped", len(pack.AutomatedDecisions))
	}
	if pack.AutomatedDecisions[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("automated decisions must stay VERIFY: the research states no AI-hiring rule")
	}
	// No contractor-classification rule (matrix F).
	if len(pack.Classifications) != 0 {
		t.Fatalf("classifications=%d, want none", len(pack.Classifications))
	}
	// No truncated extraction notes survive, and the only gaps left
	// visible are the drug-testing, breach-timeline, and AI-hiring rules
	// the research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-il.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 3 {
		t.Errorf("pack carries %d visible section gaps, want exactly the drug-testing, breach, and AI-hiring ones", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_IL_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_IL_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-il.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "illinois.golden.txt"))
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

// TestTodo_LEGAL_ST_IL_001_Conformance checks the Illinois matrix row
// against the pack and proves every registered state draft still loads and
// validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_IL_001_Conformance(t *testing.T) {
	pack := illinoisPack(t)
	// Table A Y cells: everything but CLASSIFICATION.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 || len(pack.AutomatedDecisions) == 0 ||
		len(pack.MonitoringConsents) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent.
	if len(pack.Classifications) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent (locality overlays ride with the
	// locality stage, never in the state pack).
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_IL_001_Mutation seeds mutants into the Illinois draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_IL_001_Mutation(t *testing.T) {
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
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			def := loadStateDraft(t, "IL")
			def.Obligations = append([]ObligationJSON(nil), def.Obligations...)
			def.Preemptions = append([]PreemptionJSON(nil), def.Preemptions...)
			m.mutate(&def)
			encoded, err := json.Marshal(def)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			reloaded, err := LoadPackDefinition(encoded)
			if err == nil {
				_, err = reloaded.Candidate()
			}
			if err == nil {
				t.Fatal("mutant survived: the loader accepted it")
			}
		})
	}
}
