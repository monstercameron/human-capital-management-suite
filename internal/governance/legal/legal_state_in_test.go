package legal

// LEGAL-ST-IN-001 verification tests.
//
// The Indiana draft is agent-verified, not counsel-reviewed: the pack
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

func indianaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "IN").Candidate()
	if err != nil {
		t.Fatalf("Candidate(IN): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_IN_001 verifies the Indiana draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_IN_001(t *testing.T) {
	def := loadStateDraft(t, "IN")
	if def.PackID != "us-in-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Indiana draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "IN" {
		t.Fatalf("subdivision=%q, want IN", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := indianaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only floor marker (IC 22-2-2, preempting local
	// ordinances per IC 22-2-2-10.5). Indiana sets no state premium, so
	// the marker carries no amount, exactly like the Alabama template.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, Indiana states no premium", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Section, "IC 22-2-2") {
		t.Fatalf("section=%q, want IC 22-2-2", pack.WageFloors[0].Citation.Section)
	}
	// PAY_FREQUENCY: semimonthly or biweekly, wages earned within 10
	// business days prior (IC 22-2-5-1) — never a federal cite.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum=%q, want the semimonthly minimum", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "IC 22-2-5-1") {
		t.Fatalf("section=%q, want IC 22-2-5-1", freq.Citation.Section)
	}
	if !strings.Contains(freq.Citation.Note, "10 business days") {
		t.Fatalf("note=%q, want the 10-business-day earned-wage window", freq.Citation.Note)
	}
	// FINAL_PAY_DEADLINE: next regular payday on voluntary resignation
	// (IC 22-2-5-2).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "IC 22-2-5-2") {
		t.Fatalf("section=%q, want IC 22-2-5-2", finalPay.Citation.Section)
	}
	for _, want := range []string{"next regular payday", "voluntar"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Errorf("deadline=%q, want %q", finalPay.DeadlineDescription, want)
		}
	}
	// RETENTION: 4-year payroll records, stricter than the federal 3
	// (IC 22-2-8).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 4 {
		t.Fatalf("retention=%+v, want the 4-year rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "IC 22-2-8") {
		t.Fatalf("section=%q, want IC 22-2-8", pack.RetentionRules[0].Citation.Section)
	}
	// NON_COMPETE: physician non-competes voided for hospital-system
	// agreements from 2025-07-01 (IC 25-22.5-5.5, SEA 475).
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Citation.Section, "IC 25-22.5-5.5") {
		t.Fatalf("section=%q, want IC 25-22.5-5.5", nc.Citation.Section)
	}
	for _, want := range []string{"hysician", "July 1, 2025", "voids"} {
		if !strings.Contains(nc.Rule, want) {
			t.Errorf("rule=%q, want %q", nc.Rule, want)
		}
	}
	// SEPARATION_FILING: UI-14 Separation Notice on the 4-year retention
	// cycle (IC 22-2-8). The form name is carried; the filing schema is
	// evaluation-stage work.
	if len(pack.SeparationFilings) != 1 {
		t.Fatalf("separation filings=%d, want one", len(pack.SeparationFilings))
	}
	sep := pack.SeparationFilings[0]
	if sep.FormName != "UI-14" {
		t.Fatalf("form=%q, want the UI-14", sep.FormName)
	}
	if !strings.Contains(sep.Citation.Section, "IC 22-2-8") {
		t.Fatalf("section=%q, want IC 22-2-8", sep.Citation.Section)
	}
	// BREACH_NOTIFICATION carries the hedged IC 24-4.9 cite at VERIFY:
	// the research itself flags the section for verification.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "IC 24-4.9") {
		t.Fatalf("section=%q, want IC 24-4.9", breach.Citation.Section)
	}
	if breach.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("breach must stay VERIFY: the research hedges the cite")
	}
	// Matrix Y cells present: anti-retaliation.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	// No state mini-WARN (matrix F: federal WARN only).
	if len(pack.MiniWARNTriggers) != 0 {
		t.Fatalf("mini-warn triggers=%d, want none", len(pack.MiniWARNTriggers))
	}
	// No truncated extraction notes survive, and the only gap left
	// visible is the drug-testing statute the research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-in.json"))
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

// TestTodo_LEGAL_ST_IN_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_IN_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-in.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "indiana.golden.txt"))
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

// TestTodo_LEGAL_ST_IN_001_Conformance checks the Indiana matrix row
// against the pack and proves every registered state draft still loads and
// validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_IN_001_Conformance(t *testing.T) {
	pack := indianaPack(t)
	// Table A Y cells: PAY_FREQ, NON_COMPETE, RETENTION.
	// Table B Y cells: FINAL_PAY, SEP_FILING, ANTI_RETALIATION, BREACH.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.SeparationFilings) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions and the federal-only floor marker the PRIMARY test pins.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.PayStatements) != 0 || len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 ||
		len(pack.PayEquityReviews) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent (E-Verify is locality-scoped).
	if len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("Indiana carries locality overlays the matrix marks F")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_IN_001_Mutation seeds mutants into the Indiana draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_IN_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "IN")
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
