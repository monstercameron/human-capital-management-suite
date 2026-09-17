package legal

// LEGAL-ST-AK-001 verification tests.
//
// The Alaska draft is agent-verified, not counsel-reviewed: the pack
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

func alaskaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "AK").Candidate()
	if err != nil {
		t.Fatalf("Candidate(AK): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_AK_001 verifies the Alaska draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_AK_001(t *testing.T) {
	def := loadStateDraft(t, "AK")
	if def.PackID != "us-ak-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Alaska draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "AK" {
		t.Fatalf("subdivision=%q, want AK", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := alaskaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $14.00/hr from 2026-07-01, no tip credit, Anchorage
	// CPI-U indexation from 2028 (AS 23.10.065). The floor binds every
	// worker, so the pack carries no tipped-worker subclass.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "14.00 USD" {
		t.Fatalf("floor=%q, want the research-stated $14.00/hr", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if floor.Indexation != "CPI" {
		t.Fatalf("indexation=%q, want the Anchorage CPI-U indexation", floor.Indexation)
	}
	if floor.WorkerClass != "" {
		t.Fatalf("worker_class=%q, the no-tip-credit floor binds every worker", floor.WorkerClass)
	}
	if !strings.Contains(floor.Citation.Note, "No tip credit") {
		t.Fatalf("note=%q, want the no-tip-credit rule carried", floor.Citation.Note)
	}
	// NOTICE: change notice due "on the payday before the time of
	// change" (AS 23.05.160), stricter than "before the next pay period".
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.Channel != "written" {
		t.Fatalf("channel=%q, want written", notice.Channel)
	}
	if !strings.Contains(notice.Citation.Section, "23.05.160") {
		t.Fatalf("section=%q, want the AS 23.05.160 hire/change notice", notice.Citation.Section)
	}
	if !strings.Contains(notice.Citation.Note, "payday before the time of change") {
		t.Fatalf("note=%q, want the exact statutory lead time", notice.Citation.Note)
	}
	// PAY_FREQUENCY: semi-monthly minimum under AS 23.05.140, not the
	// notice statute the raw extraction cited.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum_frequency=%q, want the semi-monthly floor", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "23.05.140") {
		t.Fatalf("section=%q, want AS 23.05.140", freq.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: 3 working days on discharge, next payday
	// (minimum 3 days after notice) on resignation, penalty wages up to
	// 90 working days (AS 23.05.140(f)-(g)).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if finalPay.Trigger != "termination_any" {
		t.Fatalf("trigger=%q, want the termination deadline", finalPay.Trigger)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "3 working days") ||
		!strings.Contains(finalPay.DeadlineDescription, "90") {
		t.Fatalf("deadline=%q, want the 3-day rule and the 90-day penalty", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "23.05.140") {
		t.Fatalf("section=%q, want AS 23.05.140", finalPay.Citation.Section)
	}
	// LEAVE_INTERACTION: Ballot Measure 1 sick leave, 1hr/30hrs, cap
	// 40hrs (<15 FTE) or 56hrs (>=15 FTE), full carryover, no payout
	// mandate, 6-month rehire reinstatement (AS 23.10.066-.067).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	for _, want := range []string{"1 hour per 30 hours", "40 hours", "56 hours", "6 months"} {
		if !strings.Contains(leave.InteractionRule, want) {
			t.Fatalf("rule=%q, want %q carried", leave.InteractionRule, want)
		}
	}
	if !strings.Contains(leave.Citation.Section, "23.10.066") {
		t.Fatalf("section=%q, want AS 23.10.066", leave.Citation.Section)
	}
	if leave.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("sick leave must read CONFIRMED: Ballot Measure 1 accrual is stated law")
	}
	// PERSONNEL_FILE: employee/former-employee inspection with
	// reasonable-cost copying (AS 23.10.430).
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	personnel := pack.PersonnelFileRules[0]
	if !strings.Contains(personnel.Citation.Section, "23.10.430") {
		t.Fatalf("section=%q, want AS 23.10.430", personnel.Citation.Section)
	}
	if !personnel.CopyFeePermitted {
		t.Fatal("copying at reasonable actual cost must be permitted")
	}
	// RETENTION: 3-year payroll records (AS 23.10.100). The matrix marks
	// RETENTION `?`, so the obligation stays VERIFY, never silently
	// confirmed.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year payroll rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "23.10.100") {
		t.Fatalf("section=%q, want the corrected AS 23.10.100 citation", pack.RetentionRules[0].Citation.Section)
	}
	if pack.RetentionRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("retention must stay VERIFY: the matrix marks RETENTION ?")
	}
	// PAY_STATEMENT: written or electronic stubs itemizing hours, rates
	// and employer identity (AS 23.05.140(d)).
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	statement := pack.PayStatements[0]
	if !strings.Contains(statement.Citation.Section, "23.05.140") {
		t.Fatalf("section=%q, want AS 23.05.140", statement.Citation.Section)
	}
	if statement.Delivery != "EITHER" {
		t.Fatalf("delivery=%q, written or electronic stubs satisfy the rule", statement.Delivery)
	}
	if !hasString(statement.RequiredFields, "hours_worked") ||
		!hasString(statement.RequiredFields, "employer_identification") {
		t.Fatalf("fields=%v, want hours and employer identity itemized", statement.RequiredFields)
	}
	// CLASSIFICATION: the ABC test for contractor status. The raw
	// extraction truncated the section mid-citation.
	if len(pack.Classifications) != 1 || pack.Classifications[0].Dimension != "CONTRACTOR" {
		t.Fatalf("classifications=%+v, want the contractor ABC test", pack.Classifications)
	}
	if !strings.Contains(pack.Classifications[0].Citation.Section, "23.20.525") {
		t.Fatalf("section=%q, want the full AS 23.20.525 citation", pack.Classifications[0].Citation.Section)
	}
	// ANTI_RETALIATION: captive-audience-meeting ban; retaliation for
	// refusal is prohibited (AS 23.10.135).
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	retaliation := pack.AntiRetaliationRules[0]
	if !strings.Contains(retaliation.Citation.Section, "23.10.135") {
		t.Fatalf("section=%q, want AS 23.10.135", retaliation.Citation.Section)
	}
	if retaliation.Disposition != "BLOCK" {
		t.Fatalf("disposition=%q, want BLOCK", retaliation.Disposition)
	}
	// DRUG_TESTING is a `?` cell: no state private-sector statute, so the
	// gap stays visible at VERIFY with no stated section, never an
	// absence of duty.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the gap carried, not dropped", len(pack.DrugTestingRules))
	}
	if pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: no state statute states the rule")
	}
	if !strings.Contains(pack.DrugTestingRules[0].Citation.Section, "not stated") {
		t.Fatalf("section=%q, want the visible gap marker", pack.DrugTestingRules[0].Citation.Section)
	}
	// BREACH_NOTIFICATION: unencrypted personal-information breaches
	// notify without unreasonable delay, with AG notice at 1,000+
	// residents (AS 45.48.010) — not the biometric-identifiers statute
	// the raw extraction cited.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "45.48.010") {
		t.Fatalf("section=%q, want AS 45.48.010", breach.Citation.Section)
	}
	if breach.AuthorityThresholdCount != 1000 {
		t.Fatalf("authority threshold=%d, want the 1,000-resident AG notice", breach.AuthorityThresholdCount)
	}
	// F cells stay absent: pay transparency, field restrictions,
	// non-competes, pay equity, e-verify, separation filings, job
	// security, mini-warn, automated decisions, monitoring consents.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.NonCompeteThresholds) != 0 || len(pack.PayEquityReviews) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.MiniWARNTriggers) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// No truncated extraction notes survive, and the only gap left
	// visible is the drug-testing statute the research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ak.json"))
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

// TestTodo_LEGAL_ST_AK_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_AK_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ak.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "alaska.golden.txt"))
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

// TestTodo_LEGAL_ST_AK_001_Conformance checks the Alaska matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_AK_001_Conformance(t *testing.T) {
	pack := alaskaPack(t)
	// Table A Y cells: NOTICE, WAGE_FLOOR, PAY_FREQ, PAY_STMT, LEAVE,
	// CLASSIFN, PERSONNEL_FILE. Table B Y cells: FINAL_PAY,
	// ANTI_RETALIATION, BREACH_NOTIFICATION.
	if len(pack.Notices) == 0 || len(pack.WageFloors) == 0 ||
		len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.Classifications) == 0 ||
		len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions the PRIMARY test pins.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.NonCompeteThresholds) != 0 || len(pack.PayEquityReviews) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_AK_001_Mutation seeds mutants into the Alaska
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_AK_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "AK")
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
