package legal

// LEGAL-ST-VT-001 verification tests.
//
// The Vermont draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched — and the
// guardrail that keeps the pack out of evaluation until counsel
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

func vermontPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "VT").Candidate()
	if err != nil {
		t.Fatalf("Candidate(VT): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_VT_001 verifies the Vermont draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_VT_001(t *testing.T) {
	def := loadStateDraft(t, "VT")
	if def.PackID != "us-vt-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Vermont draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "VT" {
		t.Fatalf("subdivision=%q, want VT", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := vermontPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $14.42/hr (2026), indexed annually by the lower of 5%
	// or CPI (21 V.S.A. § 384).
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "14.42 USD" {
		t.Fatalf("floor=%q, want the $14.42/hr floor", got)
	}
	if floor.Basis != "HOURLY" || floor.Indexation != "CPI" {
		t.Fatalf("floor=%+v, want HOURLY on CPI indexation", floor)
	}
	if !strings.Contains(floor.Citation.Note, "lower of 5%") {
		t.Fatalf("note=%q, want the lower-of-5%%-or-CPI rule carried", floor.Citation.Note)
	}
	if !strings.Contains(floor.Citation.Section, "384") {
		t.Fatalf("section=%q, want 21 V.S.A. § 384", floor.Citation.Section)
	}
	// PAY_TRANSPARENCY: wage/salary-range disclosure for 5+ employers,
	// eff. 2025-07-01 (Act 155, § 495n).
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	pt := pack.PayTransparencyDuties[0]
	if pt.Trigger != "internal_promotion" {
		t.Fatalf("trigger=%q, want internal_promotion", pt.Trigger)
	}
	if !strings.Contains(pt.RequiredDisclosure, "5+") {
		t.Fatalf("disclosure=%q, want the 5-employer threshold carried", pt.RequiredDisclosure)
	}
	if !strings.Contains(pt.RequiredDisclosure, "2025-07-01") {
		t.Fatalf("disclosure=%q, want the Act 155 effective date carried", pt.RequiredDisclosure)
	}
	if !strings.Contains(pt.Citation.Section, "495n") {
		t.Fatalf("section=%q, want § 495n", pt.Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban, no compensation
	// min/max hiring condition (§ 495m).
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "495m") {
		t.Fatalf("section=%q, want § 495m", pack.FieldRestrictions[0].Citation.Section)
	}
	// NOTICE: written pay-rate change notice (§ 342).
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written pay-rate notice", pack.Notices)
	}
	if !hasString(pack.Notices[0].ContentFields, "pay_rate") {
		t.Fatalf("notice=%+v, want the pay-rate content carried", pack.Notices[0])
	}
	// FINAL_PAY_DEADLINE: all wages due within 72 hours of discharge,
	// next payday or following Friday on resignation (§ 342a).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "72 hours") {
		t.Fatalf("deadline=%q, want the 72-hour discharge limb carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "342a") {
		t.Fatalf("section=%q, want § 342a", finalPay.Citation.Section)
	}
	// LEAVE_INTERACTION: unpaid parental leave at 10+ employers,
	// family leave at 15+ employers. The voluntary PFML insurance
	// program is never modeled as a mandatory employer contribution.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leaveRule := pack.LeaveInteractions[0].InteractionRule
	if !strings.Contains(leaveRule, "10+") || !strings.Contains(leaveRule, "15+") {
		t.Fatalf("leave=%q, want the 10+/15+ employer thresholds carried", leaveRule)
	}
	// PAY_EQUITY_REVIEW: equal-pay audit (§ 495).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	if !strings.Contains(pack.PayEquityReviews[0].Citation.Section, "495") {
		t.Fatalf("section=%q, want § 495", pack.PayEquityReviews[0].Citation.Section)
	}
	// ANTI_RETALIATION (Y) and DRUG_TESTING (Y) each appear once.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	// BREACH_NOTIFICATION: 45-day notice, 14-day AG notice
	// (9 V.S.A. § 2435).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if pack.BreachNotifications[0].SubjectDeadlineDays != 45 {
		t.Fatalf("breach=%+v, want the 45-day deadline", pack.BreachNotifications[0])
	}
	if note := pack.BreachNotifications[0].Citation.Note; !strings.Contains(note, "14 days") {
		t.Fatalf("note=%q, want the 14-day AG notice carried", note)
	}
	// RETENTION and MINI_WARN (? cells with evidence) each appear once
	// at VERIFY.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("retention=%+v, want the VERIFY ?-emission", pack.RetentionRules)
	}
	if len(pack.MiniWARNTriggers) != 1 || pack.MiniWARNTriggers[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("mini-warn=%+v, want the VERIFY ?-emission", pack.MiniWARNTriggers)
	}
	// F cells stay absent: non-competes, classifications, personnel
	// files, pay statements (dropped ?), separation filings (dropped
	// ?), e-verify, job security, automated decisions.
	if len(pack.NonCompeteThresholds) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.PayStatements) != 0 || len(pack.SeparationFilings) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F (or dropped ?) cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_VT_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_VT_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-vt.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "vermont.golden.txt"))
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

// TestTodo_LEGAL_ST_VT_001_Conformance checks the Vermont matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_VT_001_Conformance(t *testing.T) {
	pack := vermontPack(t)
	// Table A Y cells: NOTICE, PAY_TRANSPARENCY, FIELD_RESTRICTION,
	// WAGE_FLOOR, PAY_FREQUENCY, LEAVE, PAY_EQUITY. Table A ? cells
	// with evidence: RETENTION. Table B Y cells: FINAL_PAY,
	// DRUG_TESTING, ANTI_RETALIATION, BREACH_NOTIFICATION. Table B ?
	// cell with evidence: MINI_WARN.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.LeaveInteractions) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.DrugTestingRules) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 ||
		len(pack.MiniWARNTriggers) == 0 {
		t.Fatal("a matrix Y (or evidenced ?) cell has no pack obligation")
	}
	// Table A F cells stay absent: non-competes, classifications,
	// personnel files. Table B F cells stay absent: e-verify, job
	// security, automated decisions.
	if len(pack.NonCompeteThresholds) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_VT_001_Mutation seeds mutants into the Vermont
// draft's guards and asserts the loader notices. A surviving mutant
// means the guard is decorative.
func TestTodo_LEGAL_ST_VT_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "VT")
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
