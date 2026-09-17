package legal

// LEGAL-ST-KY-001 verification tests.
//
// The Kentucky draft is agent-verified, not counsel-reviewed: the pack
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

func kentuckyPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "KY").Candidate()
	if err != nil {
		t.Fatalf("Candidate(KY): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_KY_001 verifies the Kentucky draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_KY_001(t *testing.T) {
	def := loadStateDraft(t, "KY")
	if def.PackID != "us-ky-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Kentucky draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "KY" {
		t.Fatalf("subdivision=%q, want KY", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := kentuckyPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only floor marker (KRS 337.275). Kentucky sets
	// no state premium, so the marker carries no amount, exactly like the
	// Alabama template.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, Kentucky states no premium", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Section, "337.275") {
		t.Fatalf("section=%q, want KRS 337.275", pack.WageFloors[0].Citation.Section)
	}
	// CLASSIFICATION: seventh-consecutive-day premium (KRS 337.285),
	// stricter than federal — not the contractor test.
	if len(pack.Classifications) != 1 {
		t.Fatalf("classifications=%d, want one", len(pack.Classifications))
	}
	classification := pack.Classifications[0]
	if classification.Dimension != "OVERTIME_THRESHOLD" {
		t.Fatalf("dimension=%q, want OVERTIME_THRESHOLD", classification.Dimension)
	}
	if !strings.Contains(classification.Citation.Section, "337.285") {
		t.Fatalf("section=%q, want KRS 337.285", classification.Citation.Section)
	}
	if !strings.Contains(classification.TestDescription, "eventh-day") {
		t.Fatalf("test=%q, want the seventh-day premium", classification.TestDescription)
	}
	// PAY_FREQUENCY: at least semi-monthly (KRS 337.020).
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum=%q, want the semi-monthly minimum", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "337.020") {
		t.Fatalf("section=%q, want KRS 337.020", freq.Citation.Section)
	}
	// FINAL_PAY_DEADLINE is a `?` cell: LATER_OF next payday and
	// termination-plus-14-days (KRS 337.055), carried at VERIFY with both
	// arms dated in the text. The typed LATER_OF comparator is
	// evaluation-stage work.
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "337.055") {
		t.Fatalf("section=%q, want KRS 337.055", finalPay.Citation.Section)
	}
	for _, want := range []string{"14 days", "next regular payday", "occurs last"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Errorf("deadline=%q, want %q", finalPay.DeadlineDescription, want)
		}
	}
	if finalPay.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("final pay must stay VERIFY: the matrix marks FINAL_PAY ?")
	}
	// RETENTION: 4-year payroll records (KRS 337.320).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 4 {
		t.Fatalf("retention=%+v, want the 4-year rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "337.320") {
		t.Fatalf("section=%q, want KRS 337.320", pack.RetentionRules[0].Citation.Section)
	}
	// PAY_EQUITY_REVIEW: sex-based comparable-work review
	// (KRS 337.420-337.433) — not the KRS 344 checklist paraphrase.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if !strings.Contains(equity.Citation.Section, "337.420") {
		t.Fatalf("section=%q, want KRS 337.420-337.433", equity.Citation.Section)
	}
	if !hasString(equity.ProtectedBases, "sex") {
		t.Fatalf("bases=%v, want sex", equity.ProtectedBases)
	}
	if equity.ComparatorStandard != "comparable work" {
		t.Fatalf("comparator=%q, want comparable work", equity.ComparatorStandard)
	}
	// Matrix Y cells present: UI separation filing and breach. The filing
	// carries the visible gap (no stated form); the breach carries the
	// hedged KRS 365.732 cite at VERIFY.
	if len(pack.SeparationFilings) != 1 {
		t.Fatalf("separation filings=%d, want one", len(pack.SeparationFilings))
	}
	sep := pack.SeparationFilings[0]
	if !strings.Contains(sep.Citation.Section, "not stated") {
		t.Fatalf("section=%q, want the visible gap marker", sep.Citation.Section)
	}
	if sep.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("separation filing must stay VERIFY: the research states no form")
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "365.732") {
		t.Fatalf("section=%q, want KRS 365.732", breach.Citation.Section)
	}
	if breach.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("breach must stay VERIFY: the research hedges the cite")
	}
	// No state mini-WARN (matrix F: federal threshold only).
	if len(pack.MiniWARNTriggers) != 0 {
		t.Fatalf("mini-warn triggers=%d, want none", len(pack.MiniWARNTriggers))
	}
	// No truncated extraction notes survive, and the only gaps left
	// visible are the anti-retaliation, separation-filing, and
	// drug-testing statutes the research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ky.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 3 {
		t.Errorf("pack carries %d visible section gaps, want exactly the anti-retaliation, separation-filing, and drug-testing ones", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_KY_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_KY_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ky.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "kentucky.golden.txt"))
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

// TestTodo_LEGAL_ST_KY_001_Conformance checks the Kentucky matrix row
// against the pack and proves every registered state draft still loads and
// validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_KY_001_Conformance(t *testing.T) {
	pack := kentuckyPack(t)
	// Table A Y cells: PAY_FREQ, CLASSIFICATION, PAY_EQUITY, RETENTION.
	// Table B Y cells: SEP_FILING, ANTI_RETALIATION, BREACH_NOTIFICATION.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.Classifications) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.SeparationFilings) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions and the federal-only floor marker the PRIMARY test pins.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.NonCompeteThresholds) != 0 ||
		len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent, and no locality overlays where the
	// matrix marks LOCAL F.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("Kentucky carries locality overlays the matrix marks F")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_KY_001_Mutation seeds mutants into the Kentucky draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_KY_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "KY")
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
