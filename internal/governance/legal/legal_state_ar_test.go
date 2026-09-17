package legal

// LEGAL-ST-AR-001 verification tests.
//
// The Arkansas draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the `?`-cell VERIFY markers and the
// unsourced-citation gaps carried rather than dropped, the corrected
// citations — and the guardrail that keeps the pack out of evaluation
// until counsel approves it. They do not, and must not, mark the pack
// reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func arkansasPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "AR").Candidate()
	if err != nil {
		t.Fatalf("Candidate(AR): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_AR_001 verifies the Arkansas draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_AR_001(t *testing.T) {
	def := loadStateDraft(t, "AR")
	if def.PackID != "us-ar-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Arkansas draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "AR" {
		t.Fatalf("subdivision=%q, want AR", def.Jurisdiction.Subdivision)
	}
	if def.Jurisdiction.Level != "SUBDIVISION" || len(def.Jurisdiction.LocalityPath) != 0 {
		t.Fatal("the subdivision pack carries no locality overlay: Little Rock, Pulaski County and Pine Bluff attach as LOCALITY-level releases")
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := arkansasPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $11.00/hr for 4+ employee employers, with 1.5x
	// overtime over 40/week on actual hours (Ark. Code 11-4-211).
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "11.00 USD" {
		t.Fatalf("floor=%q, want the research-stated $11.00/hr", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if !strings.Contains(floor.Citation.Section, "11-4-211") {
		t.Fatalf("section=%q, want Ark. Code 11-4-211", floor.Citation.Section)
	}
	if !strings.Contains(floor.Citation.Note, "4+") {
		t.Fatalf("note=%q, want the 4-or-more-employee threshold", floor.Citation.Note)
	}
	// PAY_FREQUENCY: semi-monthly for corporate employers; $500k+
	// corporations may pay exempt management monthly (Ark. Code
	// 11-4-401).
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum_frequency=%q, want the semi-monthly floor", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "11-4-401") {
		t.Fatalf("section=%q, want Ark. Code 11-4-401", freq.Citation.Section)
	}
	if !strings.Contains(freq.Citation.Note, "$500k") {
		t.Fatalf("note=%q, want the monthly-pay exception carried", freq.Citation.Note)
	}
	// FINAL_PAY_DEADLINE: 7 days after employee demand or next regular
	// payday, with double-wage penalty for late payment (Ark. Code
	// 11-4-405).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if finalPay.Trigger != "termination_any" {
		t.Fatalf("trigger=%q, want the termination deadline", finalPay.Trigger)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "7 days") ||
		!strings.Contains(strings.ToLower(finalPay.DeadlineDescription), "double") {
		t.Fatalf("deadline=%q, want the 7-day rule and the double-wage penalty", finalPay.DeadlineDescription)
	}
	// NON_COMPETE: Act 921 reasonableness with mandatory blue-pencil
	// reformation; physician non-competes void as of 2025 (Ark. Code
	// 4-75-101). The research states the test outright, so the
	// verified pack reads CONFIRMED.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange {
		t.Fatalf("rule=%q, want the pay-change recheck", nc.Rule)
	}
	for _, want := range []string{"blue-pencil", "physician", "void"} {
		if !strings.Contains(strings.ToLower(nc.Rule), want) {
			t.Fatalf("rule=%q, want %q carried", nc.Rule, want)
		}
	}
	if !strings.Contains(nc.Citation.Section, "4-75-101") {
		t.Fatalf("section=%q, want Ark. Code 4-75-101", nc.Citation.Section)
	}
	if nc.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("non-compete must read CONFIRMED: Act 921 states the test")
	}
	// PAY_EQUITY_REVIEW: sex-based, any employer size (Ark. Code 11-4-405
	// wage-discrimination provision). The raw extraction added an age
	// basis the pay statute never states.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	review := pack.PayEquityReviews[0]
	if len(review.ProtectedBases) != 1 || !hasString(review.ProtectedBases, "sex") {
		t.Fatalf("bases=%v, want exactly the sex-based review", review.ProtectedBases)
	}
	// RETENTION is a `?` cell with no confirmed private-sector statute:
	// the 5-year agency standard and the 3-year federal payroll rule
	// stay visible at VERIFY with no stated section.
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want the gap carried, not dropped", len(pack.RetentionRules))
	}
	retention := pack.RetentionRules[0]
	if retention.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("retention must stay VERIFY: no private-sector statute is confirmed")
	}
	if !strings.Contains(retention.Citation.Section, "not stated") {
		t.Fatalf("section=%q, want the visible gap marker", retention.Citation.Section)
	}
	// ANTI_RETALIATION is a Y cell the corpus never sources (spec
	// section 12 extraction finding): carried at VERIFY with no stated
	// section until counsel sources it, never an absence of duty.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want the gap carried, not dropped", len(pack.AntiRetaliationRules))
	}
	if pack.AntiRetaliationRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("anti-retaliation must stay VERIFY: its citation is unsourced")
	}
	if !strings.Contains(pack.AntiRetaliationRules[0].Citation.Section, "not stated") {
		t.Fatalf("section=%q, want the visible gap marker", pack.AntiRetaliationRules[0].Citation.Section)
	}
	// DRUG_TESTING is a `?` cell: no state testing statute — testing is
	// allowed with notice under a consistently applied policy — so the
	// pack carries the practice at VERIFY with no stated section. The
	// raw extraction left a bare heading as the note.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the ?-cell emission", len(pack.DrugTestingRules))
	}
	drug := pack.DrugTestingRules[0]
	if drug.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks DRUG_TEST ?")
	}
	if !strings.Contains(drug.Citation.Note, "notice") {
		t.Fatalf("note=%q, want the notice-conditioned practice stated", drug.Citation.Note)
	}
	// BREACH_NOTIFICATION: personal-information (including biometric)
	// breaches posing identity-theft risk notify affected persons
	// without unreasonable delay (Arkansas Personal Information
	// Protection Act, Title 4, Chapter 110).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "Chapter 110") {
		t.Fatalf("section=%q, want Title 4, Chapter 110", breach.Citation.Section)
	}
	if breach.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("breach notification must read CONFIRMED: the Act states the duty")
	}
	// F cells stay absent: notices, pay transparency, field
	// restrictions, pay statements, leave, classifications, personnel
	// files, e-verify, mini-warn, job security, automated decisions,
	// monitoring consents.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 ||
		len(pack.FieldRestrictions) != 0 || len(pack.PayStatements) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 ||
		len(pack.PersonnelFileRules) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.MiniWARNTriggers) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// No truncated extraction notes and no bare-heading notes survive;
	// exactly the three statutorily-unsourced citations stay visible as
	// gaps: retention, anti-retaliation and drug testing.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ar.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") || strings.Contains(body, "(Ark.\"") ||
		strings.Contains(body, "\"note\": \"###") {
		t.Error("pack still carries truncated or bare-heading extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 3 {
		t.Errorf("pack carries %d visible section gaps, want exactly retention, anti-retaliation and drug testing", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_AR_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_AR_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ar.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "arkansas.golden.txt"))
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

// TestTodo_LEGAL_ST_AR_001_Conformance checks the Arkansas matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_AR_001_Conformance(t *testing.T) {
	pack := arkansasPack(t)
	// Table A Y cells: WAGE_FLOOR, PAY_FREQ, NON_COMPETE, PAY_EQUITY.
	// Table B Y cells: FINAL_PAY, ANTI_RETALIATION, BREACH_NOTIFICATION.
	if len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions the PRIMARY test pins.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 ||
		len(pack.FieldRestrictions) != 0 || len(pack.PayStatements) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 ||
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

// TestTodo_LEGAL_ST_AR_001_Mutation seeds mutants into the Arkansas
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_AR_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "AR")
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
