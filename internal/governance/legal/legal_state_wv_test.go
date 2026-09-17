package legal

// LEGAL-ST-WV-001 verification tests.
//
// The West Virginia draft is agent-verified, not counsel-reviewed: the
// pack stays UNREVIEWED and unusable under any nonzero tenant review
// floor. These tests pin the verification half of the review — every
// GREEN parameter the research supports, the matrix row matched, the
// personnel-file resolution carried as a visible VERIFY emission rather
// than a silent absence — and the guardrail that keeps the pack out of
// evaluation until counsel approves it. They do not, and must not, mark
// the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func westVirginiaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "WV").Candidate()
	if err != nil {
		t.Fatalf("Candidate(WV): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_WV_001 verifies the West Virginia draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_WV_001(t *testing.T) {
	def := loadStateDraft(t, "WV")
	if def.PackID != "us-wv-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the West Virginia draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "WV" {
		t.Fatalf("subdivision=%q, want WV", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := westVirginiaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $8.75/hr for 6+-employee employers, federal $7.25/hr
	// otherwise (W. Va. Code § 21-5C-2).
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "8.75 USD" {
		t.Fatalf("floor=%q, want the $8.75/hr floor", got)
	}
	if !strings.Contains(floor.Citation.Note, "6+") {
		t.Fatalf("note=%q, want the 6-employee threshold carried", floor.Citation.Note)
	}
	if !strings.Contains(floor.Citation.Section, "21-5C-2") {
		t.Fatalf("section=%q, want § 21-5C-2", floor.Citation.Section)
	}
	// NOTICE: written notice at hire and 1-full-pay-period advance
	// notice of any pay change, with mandatory reimbursement of the
	// reduction amount on noncompliance (§ 21-5-9(1)-(4)).
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written pay-change notice", pack.Notices)
	}
	if !hasString(pack.Notices[0].ContentFields, "pay_rate") {
		t.Fatalf("notice=%+v, want the pay-rate content carried", pack.Notices[0])
	}
	if !strings.Contains(pack.Notices[0].Citation.Section, "21-5-9") {
		t.Fatalf("section=%q, want § 21-5-9", pack.Notices[0].Citation.Section)
	}
	if note := pack.Notices[0].Citation.Note; !strings.Contains(note, "pay period") {
		t.Fatalf("note=%q, want the 1-full-pay-period advance notice carried", note)
	}
	// PAY_FREQUENCY: semi-monthly, at most 19 days between paydays
	// (§ 21-5-3).
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("pay frequency=%+v, want the semi-monthly floor", pack.PayFrequencyConstraints)
	}
	if note := pack.PayFrequencyConstraints[0].Citation.Note; !strings.Contains(note, "19 days") {
		t.Fatalf("note=%q, want the 19-day spacing carried", note)
	}
	// FINAL_PAY_DEADLINE: on or before the next regular payday
	// (§ 21-5-4).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-payday limb carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "21-5-4") {
		t.Fatalf("section=%q, want § 21-5-4", finalPay.Citation.Section)
	}
	// PAY_EQUITY_REVIEW: sex-based comparable-character work (§ 21-5B).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	if !strings.Contains(pack.PayEquityReviews[0].Citation.Section, "21-5B") {
		t.Fatalf("section=%q, want § 21-5B", pack.PayEquityReviews[0].Citation.Section)
	}
	// RETENTION: 2-year minimum payroll records (§ 21-5C-5).
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	retention := pack.RetentionRules[0]
	if retention.DurationYears != 2 {
		t.Fatalf("retention=%+v, want the 2-year payroll rule", retention)
	}
	if !strings.Contains(retention.Citation.Section, "21-5C-5") {
		t.Fatalf("section=%q, want § 21-5C-5", retention.Citation.Section)
	}
	// PERSONNEL_FILE: the LEGAL-018 resolution — no private-sector
	// personnel-file-access statute at all (W. Va. Code § 21-3-22 is a
	// public-improvement construction-safety statute) — carried as a
	// visible VERIFY ?-emission at RECOMMENDED, correctly granting
	// nothing.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want the resolution carried, not dropped", len(pack.PersonnelFileRules))
	}
	personnel := pack.PersonnelFileRules[0]
	if personnel.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("personnel file must stay VERIFY: there is no private-sector right to confirm")
	}
	if personnel.Standard != RuleStandardRecommended {
		t.Fatalf("standard=%q, want RECOMMENDED, never a statutory requirement", personnel.Standard)
	}
	if personnel.ResponseDays != 0 {
		t.Fatalf("personnel file=%+v, want no access window granted", personnel)
	}
	// NON_COMPETE: physician limits with the pay-change recheck
	// (§ 47-11E-2).
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	if !strings.Contains(pack.NonCompeteThresholds[0].Rule, "1 year") {
		t.Fatalf("rule=%q, want the 1-year physician limit carried", pack.NonCompeteThresholds[0].Rule)
	}
	// ANTI_RETALIATION (Y) appears once; SEPARATION_FILING (Y) carries
	// the 10-day unemployment-insurance notice.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	if len(pack.SeparationFilings) != 1 {
		t.Fatalf("separation filings=%d, want one", len(pack.SeparationFilings))
	}
	if pack.SeparationFilings[0].DeadlineDays != 10 {
		t.Fatalf("separation filing=%+v, want the 10-day notice", pack.SeparationFilings[0])
	}
	// DRUG_TESTING (Y) appears once.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	if !strings.Contains(pack.DrugTestingRules[0].Citation.Section, "21-3E") {
		t.Fatalf("section=%q, want § 21-3E", pack.DrugTestingRules[0].Citation.Section)
	}
	// BREACH_NOTIFICATION: matrix Y with no state privacy statute, so
	// the gap is recorded visibly — uncited means VERIFY, never silent.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want the visible gap emission", len(pack.BreachNotifications))
	}
	const sectionNotStated = "(statutory section not stated in the research file)"
	if pack.BreachNotifications[0].Citation.Section == sectionNotStated &&
		pack.BreachNotifications[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("unsupported Y cell must stay VERIFY")
	}
	// F cells stay absent: pay transparency, field restrictions, leave
	// interactions, classifications, mini-warn, e-verify, job security,
	// automated decisions. Pay statements are a dropped ? cell.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.PayStatements) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F (or dropped ?) cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_WV_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_WV_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-wv.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "west-virginia.golden.txt"))
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

// TestTodo_LEGAL_ST_WV_001_Conformance checks the West Virginia matrix
// row against the pack and proves every registered state draft still
// loads and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_WV_001_Conformance(t *testing.T) {
	pack := westVirginiaPack(t)
	// Table A Y cells: NOTICE, WAGE_FLOOR, PAY_FREQUENCY, NON_COMPETE,
	// PAY_EQUITY, RETENTION. Table A ? cell with evidence:
	// PERSONNEL_FILE. Table B Y cells: FINAL_PAY, SEP_FILING,
	// DRUG_TESTING, ANTI_RETALIATION, BREACH_NOTIFICATION.
	if len(pack.Notices) == 0 || len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 || len(pack.SeparationFilings) == 0 ||
		len(pack.DrugTestingRules) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y (or evidenced ?) cell has no pack obligation")
	}
	// Table A F cells stay absent: pay transparency, field
	// restrictions, leave interactions, classifications. Table B F
	// cells stay absent: mini-warn, e-verify, job security, automated
	// decisions.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
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

// TestTodo_LEGAL_ST_WV_001_Mutation seeds mutants into the West
// Virginia draft's guards and asserts the loader notices. A surviving
// mutant means the guard is decorative.
func TestTodo_LEGAL_ST_WV_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "WV")
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
