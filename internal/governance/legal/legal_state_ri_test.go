package legal

// LEGAL-ST-RI-001 verification tests.
//
// The Rhode Island draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the fully-Y matrix row matched — and
// the guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.
//
// Rhode Island is Y across nearly every Table A column. The Sunday/holiday
// premium-pay pyramiding rule has no typed field distinguishing it from
// ordinary overtime, so it is not invented here.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rhodeIslandPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "RI").Candidate()
	if err != nil {
		t.Fatalf("Candidate(RI): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_RI_001 verifies the Rhode Island draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_RI_001(t *testing.T) {
	def := loadStateDraft(t, "RI")
	if def.PackID != "us-ri-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Rhode Island draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "RI" {
		t.Fatalf("subdivision=%q, want RI", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := rhodeIslandPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $16.00/hr eff. 2026-01-01, CPI-adjusted to $17.00/hr in
	// 2027, § 28-12-3 — no tip credit, so the floor is hourly with no
	// tipped sub-minimum.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "16.00 USD" {
		t.Fatalf("floor=%q, want the $16.00/hr floor", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY with no tip credit", floor.Basis)
	}
	if !strings.Contains(floor.Citation.Section, "28-12-3") {
		t.Fatalf("section=%q, want § 28-12-3", floor.Citation.Section)
	}
	// PAY_TRANSPARENCY and FIELD_RESTRICTION: wage-range disclosure at
	// hire/promotion/on-request and the salary-history ban, Pay Equity
	// Act §§ 28-6-17 et seq., $10,000 special damages for violation.
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	pt := pack.PayTransparencyDuties[0]
	if pt.Trigger != "internal_promotion" {
		t.Fatalf("trigger=%q, want internal_promotion", pt.Trigger)
	}
	if !strings.Contains(pt.Citation.Section, "28-6-19") {
		t.Fatalf("section=%q, want § 28-6-19", pt.Citation.Section)
	}
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "28-6-19") {
		t.Fatalf("section=%q, want § 28-6-19", pack.FieldRestrictions[0].Citation.Section)
	}
	// NOTICE: written notice at hire and in advance of any wage,
	// deduction, or frequency change, § 28-14-2.
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written-notice rule", pack.Notices)
	}
	if !hasString(pack.Notices[0].ContentFields, "pay_rate") {
		t.Fatalf("notice=%+v, want the pay-rate content carried", pack.Notices[0])
	}
	if !strings.Contains(pack.Notices[0].Citation.Section, "28-14-2") {
		t.Fatalf("section=%q, want § 28-14-2", pack.Notices[0].Citation.Section)
	}
	// PAY_FREQUENCY: weekly by default with bondable exceptions,
	// § 28-14-2.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency=%+v, want the § 28-14-2 rule", pack.PayFrequencyConstraints)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Section, "28-14-2") {
		t.Fatalf("section=%q, want § 28-14-2", pack.PayFrequencyConstraints[0].Citation.Section)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Note, "Weekly") {
		t.Fatalf("note=%q, want the weekly default carried", pack.PayFrequencyConstraints[0].Citation.Note)
	}
	// FINAL_PAY_DEADLINE: next regular payday, § 28-14-4.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-regular-payday rule", pack.FinalPayDeadlines[0].DeadlineDescription)
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].Citation.Section, "28-14-4") {
		t.Fatalf("section=%q, want § 28-14-4", pack.FinalPayDeadlines[0].Citation.Section)
	}
	// LEAVE_INTERACTION: 40-hour paid sick/safe leave for 18+
	// employers with the 150-day waiting period, § 28-57-1 et seq. —
	// plus the TDI caregiver program and the 13-week parental/family
	// leave in the research behind it.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.LeaveType, "sick") {
		t.Fatalf("leave type=%q, want the sick-leave rule", leave.LeaveType)
	}
	if !strings.Contains(leave.InteractionRule, "35 hours") {
		t.Fatalf("leave=%q, want the 1-hour-per-35-hours accrual", leave.InteractionRule)
	}
	if !strings.Contains(leave.Citation.Section, "28-57-1") {
		t.Fatalf("section=%q, want § 28-57-1", leave.Citation.Section)
	}
	// NON_COMPETE: void below $37,650/yr or for non-exempt workers,
	// § 28-59.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Rule, "37,650") {
		t.Fatalf("rule=%q, want the $37,650 threshold carried", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "28-59") {
		t.Fatalf("section=%q, want § 28-59", nc.Citation.Section)
	}
	// MINI_WARN (? with the non-compete-shaped research note) stays a
	// visible VERIFY emission, never a silent absence.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want the ?-cell emission", len(pack.MiniWARNTriggers))
	}
	if pack.MiniWARNTriggers[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("mini-warn must stay VERIFY: the matrix marks it ?")
	}
	// PAY_EQUITY_REVIEW: documented review, § 28-6-17 et seq.
	if len(pack.PayEquityReviews) != 1 || !pack.PayEquityReviews[0].DocumentationRequired {
		t.Fatalf("pay equity=%+v, want the documented review", pack.PayEquityReviews)
	}
	pe := pack.PayEquityReviews[0]
	if pe.ComparatorStandard != "substantially similar work" {
		t.Fatalf("comparator=%q, want substantially similar work", pe.ComparatorStandard)
	}
	if !strings.Contains(pe.Citation.Section, "28-6-17") {
		t.Fatalf("section=%q, want § 28-6-17", pe.Citation.Section)
	}
	// PAY_STATEMENT: hours, rate, deductions, gross/net, § 28-14-2.1.
	if len(pack.PayStatements) != 1 || !hasString(pack.PayStatements[0].RequiredFields, "pay_rate") {
		t.Fatalf("pay statements=%+v, want the itemized-statement rule", pack.PayStatements)
	}
	if !strings.Contains(pack.PayStatements[0].Citation.Section, "28-14-2.1") {
		t.Fatalf("section=%q, want § 28-14-2.1", pack.PayStatements[0].Citation.Section)
	}
	// CLASSIFICATION: no Rhode Island-specific ABC test in the statute —
	// carried at VERIFY, never filled in.
	if len(pack.Classifications) != 1 {
		t.Fatalf("classifications=%d, want the ?-cell emission", len(pack.Classifications))
	}
	if pack.Classifications[0].Dimension != "CONTRACTOR" {
		t.Fatalf("dimension=%q, want CONTRACTOR", pack.Classifications[0].Dimension)
	}
	if pack.Classifications[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("classification must stay VERIFY: no state ABC test exists to confirm")
	}
	// PERSONNEL_FILE: § 28-14-19.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	if !strings.Contains(pack.PersonnelFileRules[0].Citation.Section, "28-14-19") {
		t.Fatalf("section=%q, want § 28-14-19", pack.PersonnelFileRules[0].Citation.Section)
	}
	// RETENTION: the § 28-14-2 pay-change notice record.
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "28-14-2") {
		t.Fatalf("section=%q, want § 28-14-2", pack.RetentionRules[0].Citation.Section)
	}
	// ANTI_RETALIATION: wage-claim, workers-comp, whistleblower and jury
	// protections, Whistleblowers' Protection Act § 28-50-1.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !hasString(ar.ProtectedActivities, "workers_compensation_claim") || !hasString(ar.ProtectedActivities, "whistleblower_report") {
		t.Fatalf("anti-retaliation=%+v, want the workers-comp and whistleblower rules", ar)
	}
	if !strings.Contains(ar.Citation.Section, "28-50-1") {
		t.Fatalf("section=%q, want § 28-50-1", ar.Citation.Section)
	}
	// DRUG_TESTING (?), § 28-6.5-1(a)(1), and BREACH_NOTIFICATION, R.I.
	// Gen. Laws § 11-49.3-4, each appear once.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the ?-cell emission", len(pack.DrugTestingRules))
	}
	if !strings.Contains(pack.DrugTestingRules[0].Citation.Section, "28-6.5") {
		t.Fatalf("section=%q, want § 28-6.5", pack.DrugTestingRules[0].Citation.Section)
	}
	if pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks it ?")
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Section, "11-49.3") {
		t.Fatalf("section=%q, want § 11-49.3", pack.BreachNotifications[0].Citation.Section)
	}
	// Table B F cells stay absent: e-verify, separation filings, job
	// security, automated decisions, monitoring consents.
	if len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_RI_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_RI_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ri.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "rhode-island.golden.txt"))
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

// TestTodo_LEGAL_ST_RI_001_Conformance checks the Rhode Island matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_RI_001_Conformance(t *testing.T) {
	pack := rhodeIslandPack(t)
	// Table A is fully Y: every kind has a pack obligation.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 || len(pack.Classifications) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table B Y cells: FINAL_PAY, ANTI_RETAL, BREACH.
	if len(pack.FinalPayDeadlines) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// The ? cells are carried, not dropped.
	if len(pack.MiniWARNTriggers) == 0 || len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix ? cell has no pack emission")
	}
	// Table B F cells stay absent: e-verify, separation filings, job
	// security, automated decisions.
	if len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_RI_001_Mutation seeds mutants into the Rhode Island
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_RI_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "RI")
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
