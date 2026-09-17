package legal

// LEGAL-ST-CA-001 verification tests.
//
// The California draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the locality overlays kept out of the
// subdivision pack, the corrected citations — and the guardrail that
// keeps the pack out of evaluation until counsel approves it. They do
// not, and must not, mark the pack reviewed.
//
// The GREEN clause's MEAL_REST_BREAK kind does not exist in the
// obligation vocabulary yet, so no pack can carry it; the PRIMARY test
// pins everything the vocabulary can express and the report names the
// gap for the vocabulary owner.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func californiaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "CA").Candidate()
	if err != nil {
		t.Fatalf("Candidate(CA): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_CA_001 verifies the California draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_CA_001(t *testing.T) {
	def := loadStateDraft(t, "CA")
	if def.PackID != "us-ca-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the California draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "CA" {
		t.Fatalf("subdivision=%q, want CA", def.Jurisdiction.Subdivision)
	}
	if def.Jurisdiction.Level != "SUBDIVISION" || len(def.Jurisdiction.LocalityPath) != 0 {
		t.Fatal("the subdivision pack carries no locality overlay: San Francisco, Los Angeles, Santa Clara and San Diego attach as LOCALITY-level releases")
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := californiaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: general $16.90/hr CPI-W-indexed (Lab. Code 1182.12),
	// fast-food $20/hr (1474), healthcare $21-25/hr by facility type
	// (1182.14), plus locality overlays named in the citation.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the statewide floor", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "16.90 USD" {
		t.Fatalf("floor=%q, want the research-stated $16.90/hr", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if floor.Indexation != "CPI" {
		t.Fatalf("indexation=%q, want the CPI-W indexation", floor.Indexation)
	}
	for _, want := range []string{"1474", "1182.14", "local"} {
		if !strings.Contains(floor.Citation.Note, want) {
			t.Fatalf("note=%q, want %q carried", floor.Citation.Note, want)
		}
	}
	// NOTICE: wage-theft-prevention notice at hire and 7-calendar-day
	// change notice (Lab. Code 2810.5).
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDays != 7 {
		t.Fatalf("timing_days=%d, want the 7-day change notice", notice.TimingDays)
	}
	if !strings.Contains(notice.Citation.Section, "2810.5") {
		t.Fatalf("section=%q, want Lab. Code 2810.5", notice.Citation.Section)
	}
	if !strings.Contains(notice.Citation.Note, "7 calendar days") {
		t.Fatalf("note=%q, want the change-notice clock stated", notice.Citation.Note)
	}
	// PAY_TRANSPARENCY and FIELD_RESTRICTION: salary-history ban,
	// pay-scale-on-request and in-posting for 15+ employees (Lab. Code
	// 432.3).
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	pt := pack.PayTransparencyDuties[0]
	if pt.Trigger != "internal_promotion" || !strings.Contains(pt.RequiredDisclosure, "pay scale") {
		t.Fatalf("duty=%+v, want the promotion pay-scale duty", pt)
	}
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "432.3") {
		t.Fatalf("section=%q, want Lab. Code 432.3", pack.FieldRestrictions[0].Citation.Section)
	}
	// RETENTION: 3-year wage/title retention (Lab. Code 226 and 432.3).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year wage rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Note, "432.3") {
		t.Fatalf("note=%q, want the title-history duty named", pack.RetentionRules[0].Citation.Note)
	}
	// PAY_FREQUENCY: semimonthly (Lab. Code 204).
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("pay frequency=%+v, want the semimonthly floor", pack.PayFrequencyConstraints)
	}
	// PAY_STATEMENT: itemized statement covering the research-stated
	// fields, electronic with consent (Lab. Code 226).
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	statement := pack.PayStatements[0]
	if !strings.Contains(statement.Citation.Section, "226") {
		t.Fatalf("section=%q, want Lab. Code 226", statement.Citation.Section)
	}
	if statement.Delivery != "ELECTRONIC" || !statement.ConsentRequired {
		t.Fatalf("statement=%+v, want electronic-with-consent delivery", statement)
	}
	for _, want := range []string{"gross_wages", "net_wages", "deductions", "hours_worked", "pay_rate", "pay_period", "employer_identification"} {
		if !hasString(statement.RequiredFields, want) {
			t.Fatalf("fields=%v, want %q itemized", statement.RequiredFields, want)
		}
	}
	// LEAVE_INTERACTION: 40-hour/5-day PSL with no forfeiture on role
	// change (Lab. Code 246), plus CFRA and PFL. The raw extraction
	// filed a final-pay item under this kind.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.Citation.Section, "246") {
		t.Fatalf("section=%q, want Lab. Code 246", leave.Citation.Section)
	}
	for _, want := range []string{"40 hours", "no forfeiture", "CFRA"} {
		if !strings.Contains(leave.InteractionRule, want) {
			t.Fatalf("rule=%q, want %q carried", leave.InteractionRule, want)
		}
	}
	// NON_COMPETE: void except business-sale (B&P 16600), with the AB
	// 1076 void-clause notice.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange {
		t.Fatalf("rule=%q, want the pay-change recheck", nc.Rule)
	}
	if !strings.Contains(strings.ToLower(nc.Rule), "void") {
		t.Fatalf("rule=%q, want voidness stated", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "16600") {
		t.Fatalf("section=%q, want B&P 16600", nc.Citation.Section)
	}
	// CLASSIFICATION: AB 5 ABC test (Lab. Code 2750.5), not a bare
	// "AB 5" citation.
	if len(pack.Classifications) != 1 || pack.Classifications[0].Dimension != "CONTRACTOR" {
		t.Fatalf("classifications=%+v, want the contractor ABC test", pack.Classifications)
	}
	if !strings.Contains(pack.Classifications[0].Citation.Section, "2750.5") {
		t.Fatalf("section=%q, want Lab. Code 2750.5", pack.Classifications[0].Citation.Section)
	}
	// PAY_EQUITY_REVIEW: Lab. Code 1197.5 sex-based review plus the
	// 100+-employer pay-data reporting duty (Gov. Code 12999). The raw
	// extraction added an age basis the equal-pay statute never states.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	review := pack.PayEquityReviews[0]
	if len(review.ProtectedBases) != 1 || !hasString(review.ProtectedBases, "sex") {
		t.Fatalf("bases=%v, want exactly the sex-based review", review.ProtectedBases)
	}
	if review.ComparatorStandard != "substantially similar work" {
		t.Fatalf("comparator=%q, want the statutory standard", review.ComparatorStandard)
	}
	if !strings.Contains(review.Citation.Note, "12999") {
		t.Fatalf("note=%q, want the pay-data reporting duty named", review.Citation.Note)
	}
	// PERSONNEL_FILE: 30-day inspection, extendable to 35 by agreement
	// (Lab. Code 1198.5).
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	personnel := pack.PersonnelFileRules[0]
	if personnel.ResponseDays != 30 || personnel.DayBasis != "CALENDAR" {
		t.Fatalf("personnel file=%+v, want the 30-calendar-day rule", personnel)
	}
	if !strings.Contains(personnel.Citation.Section, "1198.5") {
		t.Fatalf("section=%q, want Lab. Code 1198.5", personnel.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: immediate on discharge, 72 hours on
	// unnoticed resignation, 30-day daily-wage penalty (Lab. Code
	// 201-203).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "72 hours") ||
		!strings.Contains(finalPay.DeadlineDescription, "30 days") {
		t.Fatalf("deadline=%q, want the resignation clock and the penalty cap", finalPay.DeadlineDescription)
	}
	// MINI_WARN: Cal-WARN 60-day notice for 50+ layoffs at 75+
	// establishments (Lab. Code 1400-1408).
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.NoticeDays != 60 || warn.EmployeeThreshold != 50 {
		t.Fatalf("mini-warn=%+v, want the 60-day/50-employee trigger", warn)
	}
	if !strings.Contains(warn.Note, "75") {
		t.Fatalf("note=%q, want the covered-establishment size named", warn.Note)
	}
	// ANTI_RETALIATION: whistleblower protection for reporting
	// workplace violations, not waivable (Lab. Code 1102.5). The raw
	// extraction filed an at-will item under this kind.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	retaliation := pack.AntiRetaliationRules[0]
	if !strings.Contains(retaliation.Citation.Section, "1102.5") {
		t.Fatalf("section=%q, want Lab. Code 1102.5", retaliation.Citation.Section)
	}
	if retaliation.Disposition != "FLAG" {
		t.Fatalf("disposition=%q, want FLAG", retaliation.Disposition)
	}
	// DRUG_TESTING: off-duty cannabis-use discrimination barred (AB
	// 2188, Lab. Code 12954); pre-employment testing permitted with a
	// written policy. The raw extraction cited the domestic-violence
	// leave statute.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	drug := pack.DrugTestingRules[0]
	if !strings.Contains(drug.Citation.Section, "12954") {
		t.Fatalf("section=%q, want Lab. Code 12954", drug.Citation.Section)
	}
	if !hasString(drug.ProtectedStatus, "lawful_off_duty_cannabis_use") {
		t.Fatalf("protected=%v, want off-duty cannabis use protected", drug.ProtectedStatus)
	}
	if !drug.WrittenPolicyRequired {
		t.Fatal("the written drug-free-workplace policy is a stated condition")
	}
	// BREACH_NOTIFICATION: unencrypted personal-information breaches
	// notify without unreasonable delay (Civ. Code 1798.82). The raw
	// extraction cited the private-right-of-action section and carried
	// the 45-day CPRA request clock as a breach clock.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "1798.82") {
		t.Fatalf("section=%q, want Civ. Code 1798.82", breach.Citation.Section)
	}
	if breach.SubjectDeadlineDays != 0 {
		t.Fatalf("subject deadline=%d, no fixed statutory breach clock is stated", breach.SubjectDeadlineDays)
	}
	if breach.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("breach notification must read CONFIRMED: section 1798.82 states the duty")
	}
	// MONITORING_CONSENT: CCPA/CPRA covers employee personal
	// information since the 2023-01-01 exemption expiry (Civ. Code
	// 1798.100 et seq.). Biometric consent scope stays VERIFY: the
	// research flags its case law as unverified.
	if len(pack.MonitoringConsents) != 1 {
		t.Fatalf("monitoring consents=%d, want one", len(pack.MonitoringConsents))
	}
	monitoring := pack.MonitoringConsents[0]
	if !strings.Contains(monitoring.Citation.Section, "1798.100") {
		t.Fatalf("section=%q, want Civ. Code 1798.100", monitoring.Citation.Section)
	}
	if !strings.Contains(monitoring.Citation.Note, "2023-01-01") {
		t.Fatalf("note=%q, want the exemption-expiry date carried", monitoring.Citation.Note)
	}
	if monitoring.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("monitoring consent must stay VERIFY: biometric consent scope is unverified")
	}
	// F cells stay absent: e-verify and job security. The `?`
	// separation-filing and automated-decision cells carry no evidence
	// and stay dropped, never invented.
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F or empty-? cell gained a pack obligation")
	}
	// No truncated extraction notes and no section gaps survive: every
	// California obligation the research evidences names its statute.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ca.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if strings.Contains(body, "not stated") {
		t.Error("pack still carries a section gap the research evidences")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_CA_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_CA_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ca.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "california.golden.txt"))
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

// TestTodo_LEGAL_ST_CA_001_Conformance checks the California matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_CA_001_Conformance(t *testing.T) {
	pack := californiaPack(t)
	// Table A is all Y; Table B Y cells are FINAL_PAY, MINI_WARN,
	// DRUG_TEST, ANTI_RETALIATION and BREACH_NOTIFICATION, with
	// MONITORING_CONSENT carried from the section 5.2 prose.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 ||
		len(pack.FieldRestrictions) == 0 || len(pack.WageFloors) == 0 ||
		len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.Classifications) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.DrugTestingRules) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 || len(pack.MonitoringConsents) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table B F cells stay absent: e-verify and job security.
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

// TestTodo_LEGAL_ST_CA_001_Mutation seeds mutants into the California
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_CA_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "CA")
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
