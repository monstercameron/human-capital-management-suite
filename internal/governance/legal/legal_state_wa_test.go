package legal

// LEGAL-ST-WA-001 verification tests.
//
// The Washington draft is agent-verified, not counsel-reviewed: the
// pack stays UNREVIEWED and unusable under any nonzero tenant review
// floor. These tests pin the verification half of the review — every
// GREEN parameter the research supports, the matrix row matched — and
// the guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func washingtonPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "WA").Candidate()
	if err != nil {
		t.Fatalf("Candidate(WA): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_WA_001 verifies the Washington draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_WA_001(t *testing.T) {
	def := loadStateDraft(t, "WA")
	if def.PackID != "us-wa-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Washington draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "WA" {
		t.Fatalf("subdivision=%q, want WA", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := washingtonPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $17.13/hr (2026), CPI-W-indexed annually (RCW 49.46).
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "17.13 USD" {
		t.Fatalf("floor=%q, want the $17.13/hr floor", got)
	}
	// PAY_TRANSPARENCY / FIELD_RESTRICTION: salary-history ban and
	// pay-range posting for 15+ employers (RCW 49.58, SB 5408).
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	if pack.PayTransparencyDuties[0].Trigger != "internal_promotion" {
		t.Fatalf("trigger=%q, want internal_promotion", pack.PayTransparencyDuties[0].Trigger)
	}
	if !strings.Contains(pack.PayTransparencyDuties[0].Citation.Section, "49.58") {
		t.Fatalf("section=%q, want RCW 49.58", pack.PayTransparencyDuties[0].Citation.Section)
	}
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	// FINAL_PAY_DEADLINE: all earned wages due by end of the pay period
	// (RCW 49.48.010).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].DeadlineDescription, "end of established pay period") {
		t.Fatalf("deadline=%q, want the end-of-pay-period limb carried", pack.FinalPayDeadlines[0].DeadlineDescription)
	}
	// LEAVE_INTERACTION: paid sick leave continuity plus the PFML
	// program (RCW 49.46.210 + RCW 50A).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	if !strings.Contains(pack.LeaveInteractions[0].InteractionRule, "50A") {
		t.Fatalf("leave=%q, want the PFML program carried", pack.LeaveInteractions[0].InteractionRule)
	}
	if !strings.Contains(pack.LeaveInteractions[0].InteractionRule, "carries forward") {
		t.Fatalf("leave=%q, want the accrual carryforward carried", pack.LeaveInteractions[0].InteractionRule)
	}
	// NON_COMPETE: unenforceable below the indexed income threshold,
	// 18-month maximum duration (RCW 49.62.020). The $123,394.17 2025
	// threshold adjusts annually.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	rule := pack.NonCompeteThresholds[0].Rule
	if !strings.Contains(rule, "123,394.17") {
		t.Fatalf("rule=%q, want the income threshold carried", rule)
	}
	if !strings.Contains(rule, "18-month") {
		t.Fatalf("rule=%q, want the 18-month maximum duration carried", rule)
	}
	// PERSONNEL_FILE: 21-day access window (RCW 49.12.240/250).
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	if pack.PersonnelFileRules[0].ResponseDays != 21 {
		t.Fatalf("personnel file=%+v, want the 21-day access window", pack.PersonnelFileRules[0])
	}
	// MINI_WARN: state-specific 60-day notice (SB 5525).
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	if pack.MiniWARNTriggers[0].NoticeDays != 60 {
		t.Fatalf("mini-warn=%+v, want the 60-day notice", pack.MiniWARNTriggers[0])
	}
	// PAY_EQUITY_REVIEW: >15% outlier flag against substantially
	// similar work, documented (RCW 49.58).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if equity.ComparatorStandard != "substantially similar work" || !equity.DocumentationRequired {
		t.Fatalf("review=%+v, want the documented substantially-similar-work review", equity)
	}
	if !strings.Contains(equity.Citation.Note, "15%") {
		t.Fatalf("note=%q, want the 15%% outlier flag carried", equity.Citation.Note)
	}
	// PAY_STATEMENT: itemized statement on every base-pay change.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	if !hasString(pack.PayStatements[0].RequiredFields, "pay_rate") {
		t.Fatalf("pay statement=%+v, want the new-rate field carried", pack.PayStatements[0])
	}
	// RETENTION (Y) is carried once.
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	// ANTI_RETALIATION (Y) appears once.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	// PAY_FREQUENCY and DRUG_TESTING (? cells with evidence) each
	// appear once at VERIFY.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("pay frequency=%+v, want the VERIFY ?-emission", pack.PayFrequencyConstraints)
	}
	if len(pack.DrugTestingRules) != 1 || pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("drug testing=%+v, want the VERIFY ?-emission", pack.DrugTestingRules)
	}
	// BREACH_NOTIFICATION: matrix Y with no research evidence, so the
	// gap is recorded visibly — uncited means VERIFY, never silent.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want the visible gap emission", len(pack.BreachNotifications))
	}
	// The extractor's "section not stated" marker sentence; a sourced
	// section replaces it when the research evidences one.
	const sectionNotStated = "(statutory section not stated in the research file)"
	if pack.BreachNotifications[0].Citation.Section == sectionNotStated &&
		pack.BreachNotifications[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("unsupported Y cell must stay VERIFY")
	}
	// F cells stay absent: notices, classifications, e-verify, job
	// security, automated decisions. Separation filings and locality
	// overlays are dropped ? cells.
	if len(pack.Notices) != 0 || len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F (or dropped ?) cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_WA_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_WA_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-wa.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "washington.golden.txt"))
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

// TestTodo_LEGAL_ST_WA_001_Conformance checks the Washington matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_WA_001_Conformance(t *testing.T) {
	pack := washingtonPack(t)
	// Table A Y cells: PAY_TRANSPARENCY, FIELD_RESTRICTION, WAGE_FLOOR,
	// PAY_STATEMENT, LEAVE, NON_COMPETE, PAY_EQUITY, RETENTION,
	// PERSONNEL_FILE. Table A ? cell with evidence: PAY_FREQUENCY.
	// Table B Y cells: FINAL_PAY, MINI_WARN, ANTI_RETALIATION, BREACH.
	// Table B ? cell with evidence: DRUG_TESTING.
	if len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 || len(pack.WageFloors) == 0 ||
		len(pack.PayStatements) == 0 || len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 ||
		len(pack.PayFrequencyConstraints) == 0 || len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 || len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix Y (or evidenced ?) cell has no pack obligation")
	}
	// Table A F cells stay absent: notices, classifications. Table B F
	// cells stay absent: e-verify, job security, automated decisions.
	if len(pack.Notices) != 0 || len(pack.Classifications) != 0 || len(pack.EVerifyChecks) != 0 ||
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

// TestTodo_LEGAL_ST_WA_001_Mutation seeds mutants into the Washington
// draft's guards and asserts the loader notices. A surviving mutant
// means the guard is decorative.
func TestTodo_LEGAL_ST_WA_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "WA")
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
