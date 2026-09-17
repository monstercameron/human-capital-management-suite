package legal

// LEGAL-ST-DE-001 verification tests.
//
// The Delaware draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched, the tiering
// and `?`-cell gaps carried at VERIFY rather than dropped — and the
// guardrail that keeps the pack out of evaluation until counsel approves
// it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func delawarePack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "DE").Candidate()
	if err != nil {
		t.Fatalf("Candidate(DE): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_DE_001 verifies the Delaware draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_DE_001(t *testing.T) {
	def := loadStateDraft(t, "DE")
	if def.PackID != "us-de-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Delaware draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "DE" {
		t.Fatalf("subdivision=%q, want DE", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := delawarePack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $15.00/hr under 19 Del. C. § 902.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "15.00 USD" {
		t.Fatalf("floor=%q, want the $15.00/hr floor", got)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Section, "902") {
		t.Fatalf("section=%q, want 19 Del. C. § 902", pack.WageFloors[0].Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban under 19 Del. C. § 709B.
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "709B") {
		t.Fatalf("section=%q, want § 709B", pack.FieldRestrictions[0].Citation.Section)
	}
	// PAY_FREQUENCY: monthly minimum (Y cell without a citable research
	// item, carried at VERIFY rather than dropped).
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "MONTHLY" {
		t.Fatalf("pay frequency=%+v, want the monthly floor", pack.PayFrequencyConstraints)
	}
	if pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: no research item evidences it")
	}
	// FINAL_PAY_DEADLINE: next regular payday on layoff (Y cell carried
	// at VERIFY).
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-regular-payday rule", pack.FinalPayDeadlines[0].DeadlineDescription)
	}
	// LEAVE_INTERACTION: Delaware Paid Leave with the employer-size
	// tiering named in the rule — 10-24 parental-only, 25+ all-reasons —
	// carried at VERIFY.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.InteractionRule, "10") || !strings.Contains(leave.InteractionRule, "25+") {
		t.Fatalf("leave=%q, want the headcount tiering named", leave.InteractionRule)
	}
	if leave.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("leave must stay VERIFY: the tiering has no typed field")
	}
	// PERSONNEL_FILE: once/year inspection, $1,000-$5,000 penalty per
	// refusal, 19 Del. C. §§ 730-735.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	if !strings.Contains(pack.PersonnelFileRules[0].Citation.Section, "730") {
		t.Fatalf("section=%q, want §§ 730-735", pack.PersonnelFileRules[0].Citation.Section)
	}
	if !strings.Contains(pack.PersonnelFileRules[0].Citation.Note, "$1,000") {
		t.Fatalf("note=%q, want the per-refusal penalty carried", pack.PersonnelFileRules[0].Citation.Note)
	}
	// MINI_WARN: 50-affected 60-day notice (Y cell carried at VERIFY).
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	mini := pack.MiniWARNTriggers[0]
	if mini.EmployeeThreshold != 50 || mini.NoticeDays != 60 {
		t.Fatalf("mini-warn=%+v, want the 50-affected 60-day trigger", mini)
	}
	// BREACH_NOTIFICATION: 60-day notice, 500+-resident AG notice, 1-year
	// credit monitoring for SSN breaches, 6 Del. C. § 12B-102.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if breach.SubjectDeadlineDays != 60 || !breach.CreditMonitoringRequired {
		t.Fatalf("breach=%+v, want the 60-day clock with credit monitoring", breach)
	}
	if !strings.Contains(breach.Citation.Section, "12B-102") {
		t.Fatalf("section=%q, want § 12B-102", breach.Citation.Section)
	}
	// RETENTION: 3-year payroll records.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year payroll rule", pack.RetentionRules)
	}
	// PAY_EQUITY and PAY_STATEMENT are ? cells with evidence, carried at
	// VERIFY; ANTI_RETALIATION is a Y cell carried at VERIFY with a BLOCK
	// disposition.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want the ?-cell emission", len(pack.PayEquityReviews))
	}
	if pack.PayEquityReviews[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay equity must stay VERIFY: the matrix marks PAY_EQUITY ?")
	}
	if len(pack.PayStatements) != 1 || !hasString(pack.PayStatements[0].RequiredFields, "gross_wages") {
		t.Fatalf("pay statements=%+v, want the itemized-statement ? emission", pack.PayStatements)
	}
	if len(pack.AntiRetaliationRules) != 1 || pack.AntiRetaliationRules[0].Disposition != "BLOCK" {
		t.Fatalf("anti-retaliation=%+v, want the blocking rule", pack.AntiRetaliationRules)
	}
	// F cells stay absent: notices, transparency, classifications,
	// e-verify, separation filings, job security, drug testing,
	// automated decisions, monitoring consents.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.Classifications) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.DrugTestingRules) != 0 || len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_DE_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_DE_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-de.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "delaware.golden.txt"))
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

// TestTodo_LEGAL_ST_DE_001_Conformance checks the Delaware matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_DE_001_Conformance(t *testing.T) {
	pack := delawarePack(t)
	// Table A Y cells: FIELD_RESTRICTION, WAGE_FLOOR, PAY_FREQUENCY,
	// LEAVE, RETENTION, PERSONNEL_FILE. Table B Y cells: FINAL_PAY,
	// MINI_WARN, ANTI_RETALIATION, BREACH (60d).
	if len(pack.FieldRestrictions) == 0 || len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	if pack.BreachNotifications[0].SubjectDeadlineDays != 60 {
		t.Fatalf("breach=%+v, want the matrix-annotated 60-day clock", pack.BreachNotifications[0])
	}
	// Table A F cells stay absent: notices, transparency,
	// classifications.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.Classifications) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_DE_001_Mutation seeds mutants into the Delaware
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_DE_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "DE")
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
