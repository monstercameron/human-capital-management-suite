package legal

// LEGAL-ST-IA-001 verification tests.
//
// The Iowa draft is agent-verified, not counsel-reviewed: the pack stays
// UNREVIEWED and unusable under any nonzero tenant review floor. These
// tests pin the verification half of the review — every GREEN parameter
// the research supports, the `?`-cell VERIFY markers carried rather than
// dropped, the corrected citations — and the guardrail that keeps the pack
// out of evaluation until counsel approves it. They do not, and must not,
// mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func iowaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "IA").Candidate()
	if err != nil {
		t.Fatalf("Candidate(IA): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_IA_001 verifies the Iowa draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_IA_001(t *testing.T) {
	def := loadStateDraft(t, "IA")
	if def.PackID != "us-ia-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Iowa draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "IA" {
		t.Fatalf("subdivision=%q, want IA", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := iowaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only floor marker, F ($7.25/hr, Iowa Code
	// § 91D.1). Iowa sets no state premium, so the marker carries no
	// amount, exactly like the Alabama template.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, Iowa states no premium", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Section, "91D.1") {
		t.Fatalf("section=%q, want Iowa Code § 91D.1", pack.WageFloors[0].Citation.Section)
	}
	// NOTICE: written pay-rate change notice at least 3 days before the
	// change (Iowa Code § 91A.3), not the pay-frequency heading that merely
	// trips the evidence words.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDirection != "BEFORE" || notice.TimingDays != 3 || notice.Channel != "written" {
		t.Fatalf("notice=%+v, want written 3-day advance notice", notice)
	}
	if !hasString(notice.ContentFields, "pay_rate") {
		t.Fatalf("content=%v, want pay_rate", notice.ContentFields)
	}
	if !strings.Contains(notice.Citation.Section, "91A.3") {
		t.Fatalf("section=%q, want Iowa Code § 91A.3", notice.Citation.Section)
	}
	if !strings.Contains(notice.Citation.Note, "at least 3 days") {
		t.Fatalf("note=%q, want the 3-day advance rule carried", notice.Citation.Note)
	}
	// FINAL_PAY_DEADLINE: next regular payday or within 5 days, whichever
	// first, plus the civil penalty (Iowa Code § 91A.4).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "91A.4") {
		t.Fatalf("section=%q, want Iowa Code § 91A.4", finalPay.Citation.Section)
	}
	for _, want := range []string{"next regular payday", "5 days"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Errorf("deadline=%q, want %q", finalPay.DeadlineDescription, want)
		}
	}
	// PERSONNEL_FILE: 3-business-day access with copy-fee permission
	// (Iowa Code § 91B.1).
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	personnel := pack.PersonnelFileRules[0]
	if personnel.ResponseDays != 3 || personnel.DayBasis != "BUSINESS" {
		t.Fatalf("personnel=%+v, want a 3-business-day response window", personnel)
	}
	if !strings.Contains(personnel.Citation.Section, "91B.1") {
		t.Fatalf("section=%q, want Iowa Code § 91B.1", personnel.Citation.Section)
	}
	// MINI_WARN: Iowa-specific 30-day notice for 25+-employee layoffs
	// (Iowa Code § 84C), layered on top of — not restating — the federal
	// 100+-employer/60-day rule.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.EmployeeThreshold != 25 || warn.NoticeDays != 30 {
		t.Fatalf("trigger=%+v, want the 25-employee 30-day Iowa rule", warn)
	}
	if !strings.Contains(warn.Citation.Section, "84C") {
		t.Fatalf("section=%q, want Iowa Code § 84C", warn.Citation.Section)
	}
	// ANTI_RETALIATION: discrimination coverage excludes gender identity
	// as of SF 418 (2025-07-01, Iowa Code ch. 216) — carried explicitly.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	anti := pack.AntiRetaliationRules[0]
	if !strings.Contains(anti.Citation.Section, "SF 418") {
		t.Fatalf("section=%q, want SF 418", anti.Citation.Section)
	}
	if !strings.Contains(anti.Citation.Note, "gender identity") {
		t.Fatalf("note=%q, want the gender-identity exclusion carried", anti.Citation.Note)
	}
	// RETENTION: 4-year payroll records (Iowa Code § 91A.6).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 4 {
		t.Fatalf("retention=%+v, want the 4-year payroll rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "91A.6") {
		t.Fatalf("section=%q, want Iowa Code § 91A.6", pack.RetentionRules[0].Citation.Section)
	}
	// PAY_EQUITY_REVIEW carries the § 91D.2 comparator the merged evidence
	// states (matrix Y).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if !strings.Contains(equity.Citation.Section, "91D.2") {
		t.Fatalf("section=%q, want Iowa Code § 91D.2", equity.Citation.Section)
	}
	if equity.ComparatorStandard != "substantially similar work" {
		t.Fatalf("comparator=%q, want substantially similar work", equity.ComparatorStandard)
	}
	// BREACH_NOTIFICATION: 5-business-day employee notice (Iowa Code
	// § 715C.1).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "715C.1") {
		t.Fatalf("section=%q, want Iowa Code § 715C.1", breach.Citation.Section)
	}
	if breach.SubjectDeadlineDays != 5 {
		t.Fatalf("breach deadline=%d, want the 5-day employee notice", breach.SubjectDeadlineDays)
	}
	// No truncated extraction notes survive, and the only gap left
	// visible is the separation-filing statute the research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ia.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 1 {
		t.Errorf("pack carries %d visible section gaps, want exactly the separation-filing one", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_IA_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_IA_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ia.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "iowa.golden.txt"))
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

// TestTodo_LEGAL_ST_IA_001_Conformance checks the Iowa matrix row against
// the pack and proves every registered state draft still loads and
// validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_IA_001_Conformance(t *testing.T) {
	pack := iowaPack(t)
	// Table A Y cells: NOTICE, PAY_EQUITY, RETENTION, PERSONNEL_FILE.
	// Table B Y cells: FINAL_PAY, MINI_WARN, ANTI_RETALIATION,
	// BREACH_NOTIFICATION.
	if len(pack.Notices) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.MiniWARNTriggers) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions and the federal-only floor marker the PRIMARY test pins.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.NonCompeteThresholds) != 0 ||
		len(pack.Classifications) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent, and no locality overlays where the
	// matrix marks LOCAL F.
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("Iowa carries locality overlays the matrix marks F")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_IA_001_Mutation seeds mutants into the Iowa draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_IA_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "IA")
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
