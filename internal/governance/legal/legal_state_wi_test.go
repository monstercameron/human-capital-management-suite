package legal

// LEGAL-ST-WI-001 verification tests.
//
// The Wisconsin draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the corpus-broadest preemption
// assertion carried as data for LEGAL-013, the `?`-cell VERIFY markers
// carried rather than dropped — and the guardrail that keeps the pack
// out of evaluation until counsel approves it. They do not, and must
// not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wisconsinPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "WI").Candidate()
	if err != nil {
		t.Fatalf("Candidate(WI): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_WI_001 verifies the Wisconsin draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_WI_001(t *testing.T) {
	def := loadStateDraft(t, "WI")
	if def.PackID != "us-wi-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Wisconsin draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "WI" {
		t.Fatalf("subdivision=%q, want WI", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := wisconsinPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// PreemptionAssertion: the corpus's broadest — WAGE_FLOOR,
	// LEAVE_INTERACTION, FIELD_RESTRICTION and PAY_TRANSPARENCY all
	// preempted at locality level statewide (Wis. Stat. §§ 104.001(2),
	// 103.10(1m), AB 748). Data for LEGAL-013's evaluation stage and
	// LEGAL-016's Milwaukee-overlay boundary vector, never an inline
	// exception in a comparator.
	if len(pack.PreemptionAssertions) != 4 {
		t.Fatalf("preemption assertions=%d, want the four-kind preemption", len(pack.PreemptionAssertions))
	}
	asserted := map[string]bool{}
	for _, a := range pack.PreemptionAssertions {
		if a.Scope != "LOCALITY_ONLY" {
			t.Fatalf("preemption scope=%q, want LOCALITY_ONLY", a.Scope)
		}
		if !strings.Contains(a.Citation.Section, "104.001(2)") {
			t.Fatalf("section=%q, want Wis. Stat. § 104.001(2)", a.Citation.Section)
		}
		asserted[a.Kind.String()] = true
	}
	for _, want := range []string{"WAGE_FLOOR", "LEAVE_INTERACTION", "FIELD_RESTRICTION", "PAY_TRANSPARENCY"} {
		if !asserted[want] {
			t.Errorf("preemption missing for %s", want)
		}
	}
	// WAGE_FLOOR: preempted (P cell), so the pack carries no state
	// figure — Wisconsin defers to the federal $7.25/hr floor
	// (§ 104.035), with no local ordinance permitted above it.
	if len(pack.WageFloors) != 0 {
		t.Fatalf("wage floors=%d, the P cell carries no obligation", len(pack.WageFloors))
	}
	// PAY_TRANSPARENCY, FIELD_RESTRICTION, LEAVE_INTERACTION: preempted
	// (P cells) — asserted above, never obligations. No statewide
	// salary-range duty and no salary-history ban exist (AB 748 bars
	// even local ones); no statewide paid sick leave exists.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.LeaveInteractions) != 0 {
		t.Fatal("a preempted cell gained a pack obligation")
	}
	// FINAL_PAY_DEADLINE: next regularly scheduled payday, or within 24
	// hours when employment ends in a business separation (merger,
	// liquidation, relocation, closure) (§ 109.03).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	for _, want := range []string{"next regularly scheduled payday", "24 hours"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Errorf("deadline=%q, want %q", finalPay.DeadlineDescription, want)
		}
	}
	if !strings.Contains(finalPay.Citation.Section, "109.03") {
		t.Fatalf("section=%q, want § 109.03", finalPay.Citation.Section)
	}
	// NON_COMPETE: reasonableness test with blue-pencil reformation
	// (Runzheimer, 2015 WI 45, § 103.465).
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	rule := pack.NonCompeteThresholds[0].Rule
	for _, want := range []string{"Runzheimer", "blue-pencil"} {
		if !strings.Contains(rule, want) {
			t.Errorf("rule=%q, want %q carried", rule, want)
		}
	}
	if !strings.Contains(pack.NonCompeteThresholds[0].Citation.Section, "103.465") {
		t.Fatalf("section=%q, want § 103.465", pack.NonCompeteThresholds[0].Citation.Section)
	}
	// PERSONNEL_FILE: inspection twice per calendar year within 7
	// working days, reasonable copy fees (§ 103.13).
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	personnel := pack.PersonnelFileRules[0]
	if personnel.ResponseDays != 7 || personnel.DayBasis != "BUSINESS" {
		t.Fatalf("personnel=%+v, want the 7-working-day response window", personnel)
	}
	if personnel.FrequencyCapPerYear != 2 {
		t.Fatalf("personnel=%+v, want the twice-per-year inspection cap", personnel)
	}
	if !personnel.CopyFeePermitted {
		t.Fatalf("personnel=%+v, want the copy-fee permission carried", personnel)
	}
	if !strings.Contains(personnel.Citation.Section, "103.13") {
		t.Fatalf("section=%q, want § 103.13", personnel.Citation.Section)
	}
	// MINI_WARN: 50+-employee employers give 60 days' notice before a
	// mass layoff (25% of the workforce or 25+ workers) or a business
	// closing (25+ workers) (§ 109.07). The threshold counts the
	// employer, not the affected workers.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.EmployeeThreshold != 50 || warn.NoticeDays != 60 {
		t.Fatalf("trigger=%+v, want the 50-employee 60-day rule", warn)
	}
	if !strings.Contains(warn.Citation.Section, "109.07") {
		t.Fatalf("section=%q, want § 109.07", warn.Citation.Section)
	}
	if !strings.Contains(warn.Citation.Note, "25%") {
		t.Fatalf("note=%q, want the 25%% layoff definition carried", warn.Citation.Note)
	}
	// PAY_EQUITY_REVIEW: WFEA sex-discrimination coverage
	// (§§ 111.31-111.395), with no standalone equal-pay-for-equal-work
	// standard — so no comparator is carried.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if !hasString(equity.ProtectedBases, "sex") {
		t.Fatalf("bases=%v, want sex", equity.ProtectedBases)
	}
	if equity.ComparatorStandard != "" {
		t.Fatalf("comparator=%q, the research states no standalone equal-pay standard", equity.ComparatorStandard)
	}
	if !strings.Contains(equity.Citation.Section, "111.31") {
		t.Fatalf("section=%q, want WFEA §§ 111.31-111.395", equity.Citation.Section)
	}
	// PAY_STATEMENT: itemized hours, rate, and deduction reasons
	// (DWD 272.11(1)).
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	statement := pack.PayStatements[0]
	for _, want := range []string{"hours_worked", "pay_rate", "deductions"} {
		if !hasString(statement.RequiredFields, want) {
			t.Errorf("pay statement=%+v, want %q carried", statement, want)
		}
	}
	if !strings.Contains(statement.Citation.Section, "272.11") {
		t.Fatalf("section=%q, want DWD 272.11(1)", statement.Citation.Section)
	}
	if statement.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatalf("marker=%s, the itemized-statement rule is confirmed research", statement.Citation.ConfidenceMarker)
	}
	// PAY_FREQUENCY (? with evidence): wages at least monthly
	// (§ 109.03(1)), carried once at VERIFY.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("pay frequency=%+v, want the VERIFY ?-emission", pack.PayFrequencyConstraints)
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "MONTHLY" {
		t.Fatalf("frequency=%q, want MONTHLY", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "109.03") {
		t.Fatalf("section=%q, want § 109.03", freq.Citation.Section)
	}
	// RETENTION: 3-year payroll records (DWD 272.11(1)).
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	retention := pack.RetentionRules[0]
	if retention.RecordClass != "payroll_records" || retention.DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year payroll rule", retention)
	}
	if !strings.Contains(retention.Citation.Section, "272.11") {
		t.Fatalf("section=%q, want DWD 272.11(1)", retention.Citation.Section)
	}
	if retention.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("marker=%s, the ?-cell retention emission stays VERIFY", retention.Citation.ConfidenceMarker)
	}
	// ANTI_RETALIATION: the matrix Y cell has no research-evidenced
	// section, so the gap is recorded visibly — uncited means VERIFY,
	// never silent.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want the visible gap emission", len(pack.AntiRetaliationRules))
	}
	anti := pack.AntiRetaliationRules[0]
	if !hasString(anti.ProtectedActivities, "workers_compensation_claim") {
		t.Fatalf("anti-retaliation=%+v, want the workers-compensation rule", anti)
	}
	const sectionNotStated = "(statutory section not stated in the research file)"
	if anti.Citation.Section != sectionNotStated || anti.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("anti-retaliation=%+v, the unsupported Y cell must stay a VERIFY gap", anti)
	}
	// SEP_FILING (? without evidence) is a confirmed absence: the
	// research evidences no separation-filing rule.
	if len(pack.SeparationFilings) != 0 {
		t.Fatalf("separation filings=%d, want the confirmed absence", len(pack.SeparationFilings))
	}
	// DRUG_TESTING (? with evidence): no state statute regulates
	// private-sector testing — carried once at VERIFY as a
	// recommendation, never a mandate.
	if len(pack.DrugTestingRules) != 1 || pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("drug testing=%+v, want the VERIFY ?-emission", pack.DrugTestingRules)
	}
	drug := pack.DrugTestingRules[0]
	if drug.Standard.String() != "RECOMMENDED" {
		t.Fatalf("drug testing=%+v, want RECOMMENDED, not a mandate", drug)
	}
	if !strings.Contains(drug.Citation.Note, "No Wisconsin state statute") {
		t.Fatalf("note=%q, want the no-statute finding carried", drug.Citation.Note)
	}
	// BREACH_NOTIFICATION: 45-day individual notice (§ 134.98), with
	// the no-material-risk exemption carried in the note.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if breach.SubjectDeadlineDays != 45 {
		t.Fatalf("breach=%+v, want the 45-day deadline", breach)
	}
	if !strings.Contains(breach.Citation.Section, "134.98") {
		t.Fatalf("section=%q, want § 134.98", breach.Citation.Section)
	}
	if breach.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatalf("marker=%s, the 45-day rule is confirmed research", breach.Citation.ConfidenceMarker)
	}
	// F cells stay absent: notices, classifications, e-verify, job
	// security, automated decisions. No locality overlays where the
	// matrix marks LOCAL P at subdivision level.
	if len(pack.Notices) != 0 || len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("Wisconsin carries locality overlays at subdivision level")
	}
	// No truncated extraction notes survive; the only visible section
	// gaps are the two the research never states (anti-retaliation,
	// drug testing).
	raw, err := os.ReadFile(mustStatePackPath(t, "us-wi.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 2 {
		t.Errorf("pack carries %d visible section gaps, want exactly the two evidenced ones", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_WI_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_WI_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-wi.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "wisconsin.golden.txt"))
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

// TestTodo_LEGAL_ST_WI_001_Conformance checks the Wisconsin matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_WI_001_Conformance(t *testing.T) {
	pack := wisconsinPack(t)
	// Table A Y cells: PAY_STATEMENT, NON_COMPETE, PAY_EQUITY,
	// PERSONNEL_FILE. Table A ? cell with evidence: PAY_FREQUENCY.
	// Table A P cells: PAY_TRANSPARENCY, FIELD_RESTRICTION, WAGE_FLOOR,
	// LEAVE (preempted, asserted, never obligations). Table B Y cells:
	// FINAL_PAY, MINI_WARN, ANTI_RETALIATION, BREACH_NOTIFICATION.
	// Table B ? cells with evidence: RETENTION, DRUG_TESTING (both
	// VERIFY, per the contract's ?-cell rule). Table B ? cell without
	// evidence: SEP_FILING (confirmed absence).
	if len(pack.PayStatements) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.PersonnelFileRules) == 0 ||
		len(pack.PayFrequencyConstraints) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.MiniWARNTriggers) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix Y (or evidenced ?) cell has no pack obligation")
	}
	if len(pack.PreemptionAssertions) != 4 {
		t.Fatal("the four-kind preemption assertion is missing")
	}
	// Table A F cells stay absent: notices, classifications. Table B F
	// cells stay absent: e-verify, job security, automated decisions.
	if len(pack.Notices) != 0 || len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	for _, a := range pack.PreemptionAssertions {
		if a.Scope != "LOCALITY_ONLY" {
			t.Fatalf("preemption scope=%q, want LOCALITY_ONLY", a.Scope)
		}
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_WI_001_Mutation seeds mutants into the Wisconsin
// draft's guards and asserts the loader notices. A surviving mutant
// means the guard is decorative.
func TestTodo_LEGAL_ST_WI_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "WI")
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
