package legal

// LEGAL-ST-CO-001 verification tests.
//
// The Colorado draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the `?`-cell and open-question VERIFY
// markers carried rather than dropped, the corrected citations — and the
// guardrail that keeps the pack out of evaluation until counsel approves
// it. They do not, and must not, mark the pack reviewed.
//
// The GREEN clause's MEAL_REST_BREAK kind does not exist in the
// obligation vocabulary yet, so no pack can carry the COMPS Order meal
// rule; the PRIMARY test pins everything the vocabulary can express and
// the report names the gap for the vocabulary owner.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func coloradoPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "CO").Candidate()
	if err != nil {
		t.Fatalf("Candidate(CO): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_CO_001 verifies the Colorado draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_CO_001(t *testing.T) {
	def := loadStateDraft(t, "CO")
	if def.PackID != "us-co-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Colorado draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "CO" {
		t.Fatalf("subdivision=%q, want CO", def.Jurisdiction.Subdivision)
	}
	if def.Jurisdiction.Level != "SUBDIVISION" || len(def.Jurisdiction.LocalityPath) != 0 {
		t.Fatal("the subdivision pack carries no locality overlay: Denver, Boulder and Edgewater attach as LOCALITY-level releases")
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := coloradoPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $15.16/hr statewide (2026) with the Denver $19.29
	// locality overlay named in the citation (CRS 8-6-102). The floor
	// binds every worker, so the pack carries no tipped-worker
	// subclass.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the statewide floor", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "15.16 USD" {
		t.Fatalf("floor=%q, want the research-stated $15.16/hr", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if floor.WorkerClass != "" {
		t.Fatalf("worker_class=%q, the floor binds every worker", floor.WorkerClass)
	}
	if !strings.Contains(floor.Citation.Note, "Denver") ||
		!strings.Contains(floor.Citation.Note, "19.29") {
		t.Fatalf("note=%q, want the Denver overlay named as a locality release", floor.Citation.Note)
	}
	if floor.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("wage floor must read CONFIRMED: the statewide rate is stated law")
	}
	// NOTICE is a `?` cell with no hire/change-notice statute behind
	// it: the EPEWA posting notices live under PAY_TRANSPARENCY, so the
	// subdivision pack carries no NOTICE obligation at all.
	if len(pack.Notices) != 0 {
		t.Fatalf("notices=%d, no hire/change-notice statute is stated", len(pack.Notices))
	}
	// PAY_TRANSPARENCY: EPEWA range-in-posting, benefits description,
	// close date, pre-selection promotion notice and post-selection
	// notice within 30 days (CRS 8-5-101 et seq.).
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	pt := pack.PayTransparencyDuties[0]
	if pt.Trigger != "internal_promotion" {
		t.Fatalf("trigger=%q, want the promotion duty", pt.Trigger)
	}
	if !strings.Contains(pt.RequiredDisclosure, "pay range") ||
		!strings.Contains(pt.RequiredDisclosure, "30 days") {
		t.Fatalf("disclosure=%q, want the range and 30-day selection notice", pt.RequiredDisclosure)
	}
	if !strings.Contains(pt.Citation.Section, "8-5-101") {
		t.Fatalf("section=%q, want CRS 8-5-101", pt.Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban and pay-secrecy-policy ban
	// (CRS 8-5-103).
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	// PAY_FREQUENCY is a `?` cell: wages at least monthly, monthly
	// periods payable within 10 business days (CRS 8-4-102/103),
	// carried at VERIFY.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want the ?-cell emission", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "MONTHLY" {
		t.Fatalf("minimum_frequency=%q, want the monthly floor", freq.MinimumFrequency)
	}
	if freq.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// FINAL_PAY_DEADLINE: immediate on discharge (6-hour grace if
	// payroll closed), next payday on resignation, vacation payout
	// within 14 days of demand, treble damages for willful violation
	// (CRS 8-4-109, 8-4-105).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	for _, want := range []string{"immediately", "6 hours", "14 days"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Fatalf("deadline=%q, want %q carried", finalPay.DeadlineDescription, want)
		}
	}
	// LEAVE_INTERACTION: HFWA 48-hour paid sick leave plus FAMLI
	// 12-week paid leave (CRS 8-13.3-401 and 8-13.3-501).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	for _, want := range []string{"48 hours", "12 weeks", "FAMLI"} {
		if !strings.Contains(leave.InteractionRule, want) {
			t.Fatalf("rule=%q, want %q carried", leave.InteractionRule, want)
		}
	}
	// NON_COMPETE: void unless the worker clears the highly-compensated
	// threshold and the restriction is no broader than trade-secret
	// protection (CRS 8-2-113). The threshold amount itself is an open
	// research question, so the obligation stays VERIFY.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange {
		t.Fatalf("rule=%q, want the pay-change recheck", nc.Rule)
	}
	if !strings.Contains(nc.Rule, "highly compensated") ||
		!strings.Contains(nc.Rule, "trade secret") {
		t.Fatalf("rule=%q, want the two enforceability conditions", nc.Rule)
	}
	if nc.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("non-compete must stay VERIFY: the threshold amount is an open question")
	}
	// MINI_WARN is a `?` cell: no Colorado mini-WARN exists, so the
	// federal WARN trigger the research states rides along at VERIFY,
	// never as state law.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want the ?-cell emission", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.NoticeDays != 60 || warn.EmployeeThreshold != 100 {
		t.Fatalf("mini-warn=%+v, want the federal 60-day/100-employee trigger", warn)
	}
	if warn.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("mini-warn must stay VERIFY: no state mini-WARN is stated")
	}
	// PAY_EQUITY_REVIEW: EPEWA sex-based review for substantially
	// similar work (CRS 8-5-101). The raw extraction left the protected
	// bases empty.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	review := pack.PayEquityReviews[0]
	if !hasString(review.ProtectedBases, "sex") {
		t.Fatalf("bases=%v, want the sex-based review", review.ProtectedBases)
	}
	if review.ComparatorStandard != "substantially similar work" {
		t.Fatalf("comparator=%q, want the statutory standard", review.ComparatorStandard)
	}
	if review.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("pay equity must read CONFIRMED: EPEWA states the rule")
	}
	// PAY_STATEMENT: itemized statement each payday — gross, deductions
	// with reason, net, period dates, rates — in a retainable format
	// (CRS 8-4-103). The raw extraction cited the pay-frequency
	// section and a single field.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	statement := pack.PayStatements[0]
	if !strings.Contains(statement.Citation.Section, "8-4-103") {
		t.Fatalf("section=%q, want CRS 8-4-103", statement.Citation.Section)
	}
	if statement.Delivery != "EITHER" {
		t.Fatalf("delivery=%q, paper or electronic statements satisfy the rule", statement.Delivery)
	}
	for _, want := range []string{"gross_wages", "deductions", "net_wages", "pay_period", "pay_rate"} {
		if !hasString(statement.RequiredFields, want) {
			t.Fatalf("fields=%v, want %q itemized", statement.RequiredFields, want)
		}
	}
	// CLASSIFICATION: modified ABC test with no usual-course prong
	// (CRS 8-6-101 et seq.).
	if len(pack.Classifications) != 1 || pack.Classifications[0].Dimension != "CONTRACTOR" {
		t.Fatalf("classifications=%+v, want the contractor test", pack.Classifications)
	}
	if !strings.Contains(pack.Classifications[0].Citation.Section, "8-6-101") {
		t.Fatalf("section=%q, want CRS 8-6-101", pack.Classifications[0].Citation.Section)
	}
	// PERSONNEL_FILE: once-per-calendar-year inspection (CRS 8-2-129).
	// The raw extraction typed a 10-business-day deadline the research
	// states only as typical guidance.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	personnel := pack.PersonnelFileRules[0]
	if personnel.FrequencyCapPerYear != 1 {
		t.Fatalf("personnel file=%+v, want the annual inspection cap", personnel)
	}
	if !strings.Contains(personnel.Citation.Section, "8-2-129") {
		t.Fatalf("section=%q, want CRS 8-2-129", personnel.Citation.Section)
	}
	// ANTI_RETALIATION: CADA retaliation and wage-claim retaliation bar
	// discharge, demotion, pay cuts and schedule changes (CRS
	// 24-34-402). The raw extraction cited the deductions statute.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	retaliation := pack.AntiRetaliationRules[0]
	if !strings.Contains(retaliation.Citation.Section, "24-34-402") {
		t.Fatalf("section=%q, want CRS 24-34-402", retaliation.Citation.Section)
	}
	if !hasString(retaliation.ProtectedActivities, "discrimination_complaint") ||
		!hasString(retaliation.ProtectedActivities, "wage_claim") {
		t.Fatalf("activities=%v, want CADA and wage-claim activity protected", retaliation.ProtectedActivities)
	}
	if retaliation.Disposition != "FLAG" {
		t.Fatalf("disposition=%q, want FLAG", retaliation.Disposition)
	}
	// SEPARATION_FILING: written separation notice to the departing
	// employee at or before separation, stating UI eligibility and the
	// reason (SB 22-234, CRS 8-90-107). The raw extraction carried a
	// 14-day deadline that belongs to vacation payout.
	if len(pack.SeparationFilings) != 1 {
		t.Fatalf("separation filings=%d, want one", len(pack.SeparationFilings))
	}
	filing := pack.SeparationFilings[0]
	if !strings.Contains(filing.Citation.Section, "8-90-107") {
		t.Fatalf("section=%q, want CRS 8-90-107", filing.Citation.Section)
	}
	if filing.DeadlineDays != 0 {
		t.Fatalf("deadline=%d, notice is due at or before separation", filing.DeadlineDays)
	}
	if !strings.Contains(strings.ToLower(filing.FormName), "separation notice") {
		t.Fatalf("form=%q, want the written separation notice", filing.FormName)
	}
	// DRUG_TESTING is a `?` cell: no state testing statute, so the gap
	// stays visible at VERIFY with no stated section.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the gap carried, not dropped", len(pack.DrugTestingRules))
	}
	if pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: no state statute states the rule")
	}
	if !strings.Contains(pack.DrugTestingRules[0].Citation.Section, "not stated") {
		t.Fatalf("section=%q, want the visible gap marker", pack.DrugTestingRules[0].Citation.Section)
	}
	// BREACH_NOTIFICATION: notice without unreasonable delay, at
	// latest 30 days after discovery (CRS 6-1-716).
	if len(pack.BreachNotifications) != 1 || pack.BreachNotifications[0].SubjectDeadlineDays != 30 {
		t.Fatalf("breach=%+v, want the 30-day deadline", pack.BreachNotifications)
	}
	// AUTOMATED_DECISION: high-risk AI employment decisions need impact
	// assessments, documentation and employee notice (SB 24-205). The
	// effective date is an open research question, so VERIFY stays.
	if len(pack.AutomatedDecisions) != 1 {
		t.Fatalf("automated decisions=%d, want one", len(pack.AutomatedDecisions))
	}
	auto := pack.AutomatedDecisions[0]
	if !auto.BiasAuditRequired || !auto.DisclosureRequired {
		t.Fatalf("automated decision=%+v, want audit and disclosure", auto)
	}
	if !strings.Contains(auto.Citation.Section, "24-205") {
		t.Fatalf("section=%q, want SB 24-205", auto.Citation.Section)
	}
	if auto.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("automated decision must stay VERIFY: the effective date is an open question")
	}
	// MONITORING_CONSENT: biometric identifiers need informed written
	// consent with a written retention/deletion policy (HB 24-1130).
	if len(pack.MonitoringConsents) != 1 || pack.MonitoringConsents[0].ConsentForm != "WRITTEN" {
		t.Fatalf("monitoring=%+v, want written biometric consent", pack.MonitoringConsents)
	}
	if !hasString(pack.MonitoringConsents[0].DataCategories, "biometric_identifiers") {
		t.Fatalf("categories=%v, want biometric identifiers covered", pack.MonitoringConsents[0].DataCategories)
	}
	// RETENTION: payroll records 3+ years, selection records
	// employment-plus-3, personnel records 5 for CADA defense (CRS
	// 8-4-103). The raw extraction filed a biometric-consent item
	// under this kind with an empty body.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year payroll rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "8-4-103") {
		t.Fatalf("section=%q, want CRS 8-4-103", pack.RetentionRules[0].Citation.Section)
	}
	// F cells stay absent: e-verify and job security.
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// No truncated extraction notes survive; the only gap left visible
	// is the drug-testing statute the research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-co.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 1 {
		t.Errorf("pack carries %d visible section gaps, want exactly the drug-testing one", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_CO_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_CO_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-co.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "colorado.golden.txt"))
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

// TestTodo_LEGAL_ST_CO_001_Conformance checks the Colorado matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_CO_001_Conformance(t *testing.T) {
	pack := coloradoPack(t)
	// Table A Y cells: everything but the NOTICE and PAY_FREQ ?-cells
	// the PRIMARY test pins. Table B Y cells:
	// FINAL_PAY, SEP_FILING, ANTI_RETALIATION, BREACH and AUTO_DEC,
	// with MONITORING_CONSENT carried from the section 5.2 prose.
	if len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.Classifications) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.SeparationFilings) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 ||
		len(pack.AutomatedDecisions) == 0 || len(pack.MonitoringConsents) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A/B F cells stay absent: e-verify and job security, with
	// the empty NOTICE ?-cell pinned in PRIMARY.
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_CO_001_Mutation seeds mutants into the Colorado
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_CO_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "CO")
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
