package legal

// LEGAL-ST-AZ-001 verification tests.
//
// The Arizona draft is agent-verified, not counsel-reviewed: the pack
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

func arizonaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "AZ").Candidate()
	if err != nil {
		t.Fatalf("Candidate(AZ): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_AZ_001 verifies the Arizona draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_AZ_001(t *testing.T) {
	def := loadStateDraft(t, "AZ")
	if def.PackID != "us-az-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Arizona draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "AZ" {
		t.Fatalf("subdivision=%q, want AZ", def.Jurisdiction.Subdivision)
	}
	if def.Jurisdiction.Level != "SUBDIVISION" || len(def.Jurisdiction.LocalityPath) != 0 {
		t.Fatal("the subdivision pack carries no locality overlay: Flagstaff and Tucson attach as LOCALITY-level releases")
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := arizonaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $15.15/hr statewide (2026), CPI-U-indexed annually
	// under Prop. 206 (A.R.S. 23-363). Flagstaff ($18.35, no tip credit)
	// and Tucson ($15.45, $3.00 tip credit) are locality overlays named
	// in the citation, never inline obligations.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the statewide floor", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "15.15 USD" {
		t.Fatalf("floor=%q, want the research-stated $15.15/hr", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if floor.Indexation != "CPI" {
		t.Fatalf("indexation=%q, want the Prop. 206 CPI-U indexation", floor.Indexation)
	}
	if !strings.Contains(floor.Citation.Note, "Flagstaff") ||
		!strings.Contains(floor.Citation.Note, "Tucson") {
		t.Fatalf("note=%q, want the locality overlays named as locality releases", floor.Citation.Note)
	}
	// PAY_FREQUENCY: two-plus paydays/month, 16 days apart at most,
	// wages within 5 business days of period end (A.R.S. 23-351). The
	// raw extraction typed the floor as monthly.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum_frequency=%q, want the twice-monthly floor", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "23-351") {
		t.Fatalf("section=%q, want A.R.S. 23-351", freq.Citation.Section)
	}
	if !strings.Contains(freq.Citation.Note, "16 days") {
		t.Fatalf("note=%q, want the 16-day spread limit", freq.Citation.Note)
	}
	// FINAL_PAY_DEADLINE: discharge pays the earlier of 7 working days
	// or the next regular payday; resignation pays on the next regular
	// payday (A.R.S. 23-353).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if finalPay.Trigger != "termination_any" {
		t.Fatalf("trigger=%q, want the termination deadline", finalPay.Trigger)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "7 working days") ||
		!strings.Contains(finalPay.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the discharge and resignation rules", finalPay.DeadlineDescription)
	}
	// LEAVE_INTERACTION: earned paid sick time, 1hr/30hrs, cap 40hrs
	// (15+ employees) or 24hrs (<15), usable after 90 days, 9-month
	// rehire reinstatement (A.R.S. 23-371 et seq.).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	for _, want := range []string{"1 hour per 30 hours", "40 hours", "24 hours", "9 months"} {
		if !strings.Contains(leave.InteractionRule, want) {
			t.Fatalf("rule=%q, want %q carried", leave.InteractionRule, want)
		}
	}
	if !strings.Contains(leave.Citation.Section, "23-371") {
		t.Fatalf("section=%q, want A.R.S. 23-371", leave.Citation.Section)
	}
	// E_VERIFY: mandatory for all employers regardless of size (A.R.S.
	// 23-214). The research states the mandate without qualification,
	// so the verified pack reads CONFIRMED.
	if len(pack.EVerifyChecks) != 1 {
		t.Fatalf("e-verify checks=%d, want one", len(pack.EVerifyChecks))
	}
	everify := pack.EVerifyChecks[0]
	if !everify.RequiredOnNewHireOnly {
		t.Fatal("e-verify must be required on new hire with no size exception")
	}
	if !strings.Contains(everify.Citation.Section, "23-214") {
		t.Fatalf("section=%q, want A.R.S. 23-214", everify.Citation.Section)
	}
	if everify.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("e-verify must read CONFIRMED: the all-employer mandate is stated law")
	}
	// RETENTION: 4-year payroll records (A.R.S. 23-364). The matrix
	// marks RETENTION `?`, so the obligation stays VERIFY — and the raw
	// extraction dropped the duration the research states.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 4 {
		t.Fatalf("retention=%+v, want the 4-year payroll rule", pack.RetentionRules)
	}
	if pack.RetentionRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("retention must stay VERIFY: the matrix marks RETENTION ?")
	}
	// PAY_STATEMENT is a `?` cell: the research states no detailed
	// content requirement, only the section 23-364 recordkeeping duty.
	// The verified pack carries that duty at VERIFY/RECOMMENDED instead
	// of the invented field list the raw extraction emitted.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want the ?-cell emission", len(pack.PayStatements))
	}
	statement := pack.PayStatements[0]
	if len(statement.RequiredFields) != 0 {
		t.Fatalf("fields=%v, no detailed statement-content rule is stated", statement.RequiredFields)
	}
	if statement.Standard != RuleStandardRecommended {
		t.Fatalf("standard=%s, the recordkeeping practice is a recommendation", statement.Standard)
	}
	if statement.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay statement must stay VERIFY: the matrix marks PAY_STMT ?")
	}
	// ANTI_RETALIATION: sick-time use is protected; a pay reduction
	// during or within 30 days of use flags retaliation (A.R.S. 23-373).
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	retaliation := pack.AntiRetaliationRules[0]
	if retaliation.Disposition != "FLAG" || retaliation.LookbackDays != 30 {
		t.Fatalf("rule=%+v, want the 30-day FLAG", retaliation)
	}
	if !hasString(retaliation.ProtectedActivities, "protected_leave_request") {
		t.Fatalf("activities=%v, want sick-leave use protected", retaliation.ProtectedActivities)
	}
	// JOB_SECURITY: pure at-will employment (A.R.S. 23-1501).
	if len(pack.JobSecurityRules) != 1 || pack.JobSecurityRules[0].StandardKind != "AT_WILL" {
		t.Fatalf("job security=%+v, want the at-will standard", pack.JobSecurityRules)
	}
	// DRUG_TESTING: written policy before testing (A.R.S. 23-493.04);
	// registered medical-marijuana cardholders are protected unless
	// impaired or on-premises use (A.R.S. 36-2813).
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	drug := pack.DrugTestingRules[0]
	if !drug.WrittenPolicyRequired {
		t.Fatal("the advance written testing policy is a stated duty")
	}
	if !hasString(drug.ProtectedStatus, "medical_cannabis_patient") {
		t.Fatalf("protected=%v, want cardholder protection", drug.ProtectedStatus)
	}
	if !strings.Contains(drug.Citation.Section, "23-493.04") {
		t.Fatalf("section=%q, want the testing-policy statute", drug.Citation.Section)
	}
	// BREACH_NOTIFICATION: 45-day notice for exposed personal
	// information including biometric-plus-name (A.R.S. 18-551).
	if len(pack.BreachNotifications) != 1 || pack.BreachNotifications[0].SubjectDeadlineDays != 45 {
		t.Fatalf("breach=%+v, want the 45-day deadline", pack.BreachNotifications)
	}
	// F cells stay absent: notices, pay transparency, field
	// restrictions, non-competes, classifications, pay equity,
	// personnel files, mini-warn, automated decisions, monitoring.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 ||
		len(pack.FieldRestrictions) != 0 || len(pack.NonCompeteThresholds) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 ||
		len(pack.PersonnelFileRules) != 0 || len(pack.MiniWARNTriggers) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// No truncated extraction notes survive, and no stated section is
	// left as a gap.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-az.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") || strings.Contains(body, "(A.R.S.\"") {
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

// TestTodo_LEGAL_ST_AZ_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_AZ_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-az.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "arizona.golden.txt"))
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

// TestTodo_LEGAL_ST_AZ_001_Conformance checks the Arizona matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_AZ_001_Conformance(t *testing.T) {
	pack := arizonaPack(t)
	// Table A Y cells: WAGE_FLOOR, PAY_FREQ, LEAVE. Table B Y cells:
	// FINAL_PAY, E_VERIFY, DRUG_TEST, ANTI_RETALIATION, JOB_SECURITY,
	// BREACH_NOTIFICATION.
	if len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.EVerifyChecks) == 0 || len(pack.DrugTestingRules) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.JobSecurityRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions the PRIMARY test pins.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 ||
		len(pack.FieldRestrictions) != 0 || len(pack.NonCompeteThresholds) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 ||
		len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_AZ_001_Mutation seeds mutants into the Arizona
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_AZ_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "AZ")
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
