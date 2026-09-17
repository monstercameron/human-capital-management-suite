package legal

// LEGAL-ST-SD-001 verification tests.
//
// The South Dakota draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the no-decrease CPI indexation shape,
// the matrix row matched — and the guardrail that keeps the pack out of
// evaluation until counsel approves it. They do not, and must not, mark
// the pack reviewed.
//
// Sioux Falls/Spearfish/Vermillion anti-discrimination ordinances are
// locality-only: LEGAL-TOOL-009 is the only mechanism that may attach
// them, so the state pack carries no locality overlay. The PAY_STMT ?
// cell has no research item and stays absent — a visible gap, never read
// as an absence of duty.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func southDakotaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "SD").Candidate()
	if err != nil {
		t.Fatalf("Candidate(SD): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_SD_001 verifies the South Dakota draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_SD_001(t *testing.T) {
	def := loadStateDraft(t, "SD")
	if def.PackID != "us-sd-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the South Dakota draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "SD" {
		t.Fatalf("subdivision=%q, want SD", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := southDakotaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $11.85/hr (2026), CPI-U-indexed annually, SDCL
	// 60-11-3. The research's no-decrease shape (SDCL 60-11-3.2 — the
	// rate may rise but never fall) is carried only as CPI indexation,
	// and the tipped $5.925/hr sub-minimum (SDCL 60-11-3.1) has no
	// separate typed floor here.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "11.85 USD" {
		t.Fatalf("floor=%q, want the $11.85/hr floor", got)
	}
	if floor.Basis != "HOURLY" || floor.Indexation != "CPI" {
		t.Fatalf("floor=%+v, want the HOURLY CPI-indexed floor", floor)
	}
	if !strings.Contains(floor.Citation.Note, "60-11-3") {
		t.Fatalf("note=%q, want SDCL 60-11-3 carried", floor.Citation.Note)
	}
	if floor.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("floor must stay VERIFY: the research states no citable section in the item")
	}
	// PAY_FREQUENCY: monthly or regular agreed payday communicated at
	// hire, SDCL 60-11-9.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "MONTHLY" {
		t.Fatalf("pay frequency=%+v, want the monthly floor", pack.PayFrequencyConstraints)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Note, "60-11-9") {
		t.Fatalf("note=%q, want SDCL 60-11-9 carried", pack.PayFrequencyConstraints[0].Citation.Note)
	}
	// FINAL_PAY_DEADLINE: next regular payday or reasonable time, with
	// the property-return hold and 5-day payment on written demand,
	// SDCL 60-11-10.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	final := pack.FinalPayDeadlines[0]
	if !strings.Contains(final.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-regular-payday rule", final.DeadlineDescription)
	}
	if !strings.Contains(final.Citation.Note, "60-11-10") {
		t.Fatalf("note=%q, want SDCL 60-11-10 carried", final.Citation.Note)
	}
	// NON_COMPETE: 2-year cap within a specified area where the employer
	// continues like business, void for healthcare practitioners eff.
	// 2023-07-01, SDCL 53-9-11, 53-9-11.2.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	if !strings.Contains(pack.NonCompeteThresholds[0].Citation.Note, "53-9-11") {
		t.Fatalf("note=%q, want SDCL 53-9-11 carried", pack.NonCompeteThresholds[0].Citation.Note)
	}
	// PAY_EQUITY_REVIEW: "comparable work," explicitly excluding
	// physical strength, SDCL 60-12-15.
	if len(pack.PayEquityReviews) != 1 || !pack.PayEquityReviews[0].DocumentationRequired {
		t.Fatalf("pay equity=%+v, want the documented review", pack.PayEquityReviews)
	}
	pe := pack.PayEquityReviews[0]
	if pe.ComparatorStandard != "comparable work" {
		t.Fatalf("comparator=%q, want comparable work", pe.ComparatorStandard)
	}
	if !hasString(pe.ProtectedBases, "sex") {
		t.Fatalf("review=%+v, want the sex-based review", pe)
	}
	if !strings.Contains(pe.Citation.Note, "60-12-15") {
		t.Fatalf("note=%q, want SDCL 60-12-15 carried", pe.Citation.Note)
	}
	// RETENTION: 3-year wage-record rule, SDCL 60-12-17.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year wage rule", pack.RetentionRules)
	}
	if pack.RetentionRules[0].RecordClass != "wage_records" {
		t.Fatalf("retention=%+v, want wage records", pack.RetentionRules[0])
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Note, "60-12-17") {
		t.Fatalf("note=%q, want SDCL 60-12-17 carried", pack.RetentionRules[0].Citation.Note)
	}
	// ANTI_RETALIATION: workers-comp retaliation check, SDCL 62-1-16.
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "workers_compensation_claim") {
		t.Fatalf("anti-retaliation=%+v, want the workers-comp rule", pack.AntiRetaliationRules)
	}
	if !strings.Contains(pack.AntiRetaliationRules[0].Citation.Note, "62-1-16") {
		t.Fatalf("note=%q, want SDCL 62-1-16 carried", pack.AntiRetaliationRules[0].Citation.Note)
	}
	// BREACH_NOTIFICATION: SDCL 22-40-20.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Note, "22-40-20") {
		t.Fatalf("note=%q, want SDCL 22-40-20 carried", pack.BreachNotifications[0].Citation.Note)
	}
	// F cells stay absent: notices, transparency, field restrictions,
	// leave, classifications, personnel files, mini-warn, e-verify, drug
	// testing, separation filings, job security, automated decisions,
	// monitoring consents.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.DrugTestingRules) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 ||
		len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_SD_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_SD_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-sd.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "south-dakota.golden.txt"))
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

// TestTodo_LEGAL_ST_SD_001_Conformance checks the South Dakota matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_SD_001_Conformance(t *testing.T) {
	pack := southDakotaPack(t)
	// Table A Y cells: WAGE_FLOOR, PAY_FREQ, NON_COMPETE, PAY_EQUITY,
	// RETENTION. Table B Y cells: FINAL_PAY, ANTI_RETAL, BREACH.
	if len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: notices, transparency, field
	// restrictions, leave, classifications, personnel files.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent: mini-warn, e-verify, drug testing,
	// job security, automated decisions.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.DrugTestingRules) != 0 ||
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

// TestTodo_LEGAL_ST_SD_001_Mutation seeds mutants into the South Dakota
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_SD_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "SD")
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
