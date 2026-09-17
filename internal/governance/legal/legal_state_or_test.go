package legal

// LEGAL-ST-OR-001 verification tests.
//
// The Oregon draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched, the
// scheduling gap left to LEGAL-TOOL-013 rather than invented here — and
// the guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.
//
// The statewide Fair Workweek/predictive-scheduling statute the research
// file never mentions is not carried: scheduling obligations wait on
// LEGAL-TOOL-013. Portland-metro/standard/nonurban wage tiering lives in
// the floor note; locality overlays for Portland attach via
// LEGAL-TOOL-009, never as state rules.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oregonPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "OR").Candidate()
	if err != nil {
		t.Fatalf("Candidate(OR): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_OR_001 verifies the Oregon draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_OR_001(t *testing.T) {
	def := loadStateDraft(t, "OR")
	if def.PackID != "us-or-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Oregon draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "OR" {
		t.Fatalf("subdivision=%q, want OR", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := oregonPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: three tiers — Portland metro $16.80/hr, standard
	// $15.55/hr, nonurban $14.55/hr, CPI-indexed annually, eff.
	// 2026-07-01. The tiering lives in the floor note; the typed amount
	// carries the Portland-metro head of the schedule.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "16.80 USD" {
		t.Fatalf("floor=%q, want the $16.80/hr Portland-metro head", got)
	}
	if floor.Indexation != "CPI" {
		t.Fatalf("indexation=%q, want the annual CPI schedule", floor.Indexation)
	}
	for _, tier := range []string{"16.80", "15.55", "14.55"} {
		if !strings.Contains(floor.Citation.Note, tier) {
			t.Fatalf("note=%q, want the three-tier schedule carried", floor.Citation.Note)
		}
	}
	if floor.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("floor must stay VERIFY: the research states no citable section for the tiering")
	}
	// FIELD_RESTRICTION: salary-history ban with the
	// voluntary-disclosure exception, ORS 659A.357.
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Note, "659A.357") {
		t.Fatalf("note=%q, want ORS 659A.357", pack.FieldRestrictions[0].Citation.Note)
	}
	// PAY_EQUITY_REVIEW: "comparable character" standard, ORS 652.220.
	if len(pack.PayEquityReviews) != 1 || !pack.PayEquityReviews[0].DocumentationRequired {
		t.Fatalf("pay equity=%+v, want the documented review", pack.PayEquityReviews)
	}
	pe := pack.PayEquityReviews[0]
	if !strings.Contains(pe.Citation.Section, "652.220") {
		t.Fatalf("section=%q, want ORS 652.220", pe.Citation.Section)
	}
	if !strings.Contains(pe.Citation.Note, "comparable character") {
		t.Fatalf("note=%q, want the comparable-character standard", pe.Citation.Note)
	}
	// LEAVE_INTERACTION: Oregon Sick Time carryover, ORS 653.601-661 —
	// the 40-hour/6+ employee shape and the Paid Leave Oregon 12-week
	// program stay in the research until a typed leave parameter exists
	// for them, so the emission stays VERIFY.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.InteractionRule, "Sick Time") {
		t.Fatalf("leave=%q, want the sick-time carryover rule", leave.InteractionRule)
	}
	if !strings.Contains(leave.Citation.Section, "653.601") {
		t.Fatalf("section=%q, want ORS 653.601", leave.Citation.Section)
	}
	if leave.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("leave must stay VERIFY: the broader leave program shapes have no typed field")
	}
	// NON_COMPETE: void below $116,427/yr (indexed), 2-week pre-hire
	// notice, 12-month max, 50%+ garden leave, ORS 653.295.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Rule, "116,427") {
		t.Fatalf("rule=%q, want the $116,427 threshold carried", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "653.295") {
		t.Fatalf("section=%q, want ORS 653.295", nc.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: discharge/end of next business day,
	// resignation-notice shape, ORS 652.140.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].DeadlineDescription, "next business day") {
		t.Fatalf("deadline=%q, want the next-business-day rule", pack.FinalPayDeadlines[0].DeadlineDescription)
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].Citation.Section, "652.140") {
		t.Fatalf("section=%q, want ORS 652.140", pack.FinalPayDeadlines[0].Citation.Section)
	}
	// PAY_STATEMENT: itemized statement on any pay-rate change, ORS
	// 652.610 (SB 906).
	if len(pack.PayStatements) != 1 || !hasString(pack.PayStatements[0].RequiredFields, "pay_rate") {
		t.Fatalf("pay statements=%+v, want the itemized-statement rule", pack.PayStatements)
	}
	if !strings.Contains(pack.PayStatements[0].Citation.Section, "652.610") {
		t.Fatalf("section=%q, want ORS 652.610", pack.PayStatements[0].Citation.Section)
	}
	// ? cells carried at VERIFY, never dropped: pay frequency (regular
	// payday in advance), retention (OFLA/PLO continuity), mini-warn (no
	// state notice beyond federal), personnel file, drug testing,
	// breach notification.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency=%+v, want the ?-cell emission", pack.PayFrequencyConstraints)
	}
	if pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: no research item states a section")
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Note, "payday") {
		t.Fatalf("note=%q, want the advance-payday rule", pack.PayFrequencyConstraints[0].Citation.Note)
	}
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("retention must stay VERIFY: the continuity note has no citable section")
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Note, "OFLA") {
		t.Fatalf("note=%q, want the OFLA continuity carried", pack.RetentionRules[0].Citation.Note)
	}
	if len(pack.MiniWARNTriggers) != 1 || pack.MiniWARNTriggers[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("mini-warn must stay VERIFY: Oregon adds no state notice beyond federal")
	}
	if !strings.Contains(pack.MiniWARNTriggers[0].Citation.Note, "no additional state") {
		t.Fatalf("note=%q, want the federal-only WARN shape", pack.MiniWARNTriggers[0].Citation.Note)
	}
	if len(pack.PersonnelFileRules) != 1 || pack.PersonnelFileRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("personnel file must stay VERIFY: no research item states a section")
	}
	if len(pack.DrugTestingRules) != 1 || pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks it ?")
	}
	if len(pack.BreachNotifications) != 1 || pack.BreachNotifications[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("breach must stay VERIFY: no research item evidences the rule")
	}
	// ANTI_RETALIATION: wage-discussion protection, ORS 659A.355.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	if !strings.Contains(pack.AntiRetaliationRules[0].Citation.Note, "659A.355") {
		t.Fatalf("note=%q, want ORS 659A.355", pack.AntiRetaliationRules[0].Citation.Note)
	}
	// F cells stay absent: notices, pay transparency, classifications,
	// e-verify, separation filings, job security, automated decisions,
	// monitoring consents.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.Classifications) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_OR_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_OR_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-or.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "oregon.golden.txt"))
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

// TestTodo_LEGAL_ST_OR_001_Conformance checks the Oregon matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_OR_001_Conformance(t *testing.T) {
	pack := oregonPack(t)
	// Table A Y cells: FIELD_RESTR, WAGE_FLOOR, PAY_STMT, LEAVE,
	// NON_COMPETE, PAY_EQUITY, RETENTION, PERSONNEL_FILE. Table B Y
	// cells: FINAL_PAY, ANTI_RETAL, BREACH.
	if len(pack.FieldRestrictions) == 0 || len(pack.WageFloors) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// The ? cells are carried, not dropped.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.MiniWARNTriggers) == 0 || len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix ? cell has no pack emission")
	}
	// Table A F cells stay absent: notices, transparency, classifications.
	// Table B F cells stay absent: e-verify, separation filings, job
	// security, automated decisions.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.Classifications) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_OR_001_Mutation seeds mutants into the Oregon draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_OR_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "OR")
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
