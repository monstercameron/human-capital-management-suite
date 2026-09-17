package legal

// LEGAL-ST-ID-001 verification tests.
//
// The Idaho draft is agent-verified, not counsel-reviewed: the pack stays
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

func idahoPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "ID").Candidate()
	if err != nil {
		t.Fatalf("Candidate(ID): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_ID_001 verifies the Idaho draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_ID_001(t *testing.T) {
	def := loadStateDraft(t, "ID")
	if def.PackID != "us-id-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Idaho draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "ID" {
		t.Fatalf("subdivision=%q, want ID", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := idahoPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-tracking floor marker (Idaho Code § 44-1502).
	// The $7.25 floor auto-tracks federal with no state premium, so the
	// marker carries no amount; the youth and tipped figures ride in the
	// note, exactly like the Alabama template.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-tracking floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, Idaho states no premium", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Section, "44-1502") {
		t.Fatalf("section=%q, want Idaho Code § 44-1502", pack.WageFloors[0].Citation.Section)
	}
	// NOTICE: written wage-reduction notice before the affected work is
	// performed (Idaho Code § 45-610) — not the retention item that merely
	// mentions notice of wage reductions.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDirection != "BEFORE" || notice.Channel != "written" {
		t.Fatalf("notice=%+v, want written pre-work notice", notice)
	}
	if !hasString(notice.ContentFields, "pay_rate") {
		t.Fatalf("content=%v, want pay_rate", notice.ContentFields)
	}
	if !strings.Contains(notice.Citation.Section, "45-610") {
		t.Fatalf("section=%q, want Idaho Code § 45-610", notice.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: earlier of next payday or 10 days, 48 hours on
	// written employee demand (Idaho Code § 45-606).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "45-606") {
		t.Fatalf("section=%q, want Idaho Code § 45-606", finalPay.Citation.Section)
	}
	for _, want := range []string{"next regular payday", "10 days", "48-hour"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Errorf("deadline=%q, want %q", finalPay.DeadlineDescription, want)
		}
	}
	// NON_COMPETE: key-employee-only, 18-month presumption
	// (Idaho Code §§ 44-2701/44-2704). The key-employee gate rides in rule
	// text until the vocabulary gains a typed gate.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Citation.Section, "44-2701") {
		t.Fatalf("section=%q, want Idaho Code §§ 44-2701/44-2704", nc.Citation.Section)
	}
	for _, want := range []string{"key employee", "18 month"} {
		if !strings.Contains(nc.Rule, want) {
			t.Errorf("rule=%q, want %q", nc.Rule, want)
		}
	}
	// RETENTION: 3-year employment records (Idaho Code § 45-610).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "45-610") {
		t.Fatalf("section=%q, want Idaho Code § 45-610", pack.RetentionRules[0].Citation.Section)
	}
	// BREACH_NOTIFICATION: without unreasonable delay, with the 24-hour
	// state-agency AG notice (Idaho Code § 28-51-105).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "28-51-105") {
		t.Fatalf("section=%q, want Idaho Code § 28-51-105", breach.Citation.Section)
	}
	for _, want := range []string{"without unreasonable delay", "24 hours"} {
		if !strings.Contains(breach.Citation.Note, want) {
			t.Errorf("note=%q, want %q", breach.Citation.Note, want)
		}
	}
	// PAY_FREQUENCY is a `?` cell: § 45-606 sets final-pay timing but no
	// minimum frequency, so the obligation carries a section and no
	// invented minimum.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "" {
		t.Fatalf("minimum=%q, the research denies a minimum frequency", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "45-606") {
		t.Fatalf("section=%q, want Idaho Code § 45-606", freq.Citation.Section)
	}
	if freq.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// PAY_STATEMENT carries the § 45-609 itemized-statement duty (matrix Y).
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	stmt := pack.PayStatements[0]
	if !strings.Contains(stmt.Citation.Section, "45-609") {
		t.Fatalf("section=%q, want Idaho Code § 45-609", stmt.Citation.Section)
	}
	if !hasString(stmt.RequiredFields, "deductions") {
		t.Fatalf("fields=%v, want deductions", stmt.RequiredFields)
	}
	// No truncated extraction notes survive, and the only gaps left
	// visible are the anti-retaliation and drug-testing statutes the
	// research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-id.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 2 {
		t.Errorf("pack carries %d visible section gaps, want exactly the anti-retaliation and drug-testing ones", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_ID_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_ID_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-id.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "idaho.golden.txt"))
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

// TestTodo_LEGAL_ST_ID_001_Conformance checks the Idaho matrix row against
// the pack and proves every registered state draft still loads and
// validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_ID_001_Conformance(t *testing.T) {
	pack := idahoPack(t)
	// Table A Y cells: NOTICE, PAY_STMT, NON_COMPETE, RETENTION.
	// Table B Y cells: FINAL_PAY, ANTI_RETALIATION, BREACH_NOTIFICATION.
	if len(pack.Notices) == 0 || len(pack.PayStatements) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions and the federal-tracking floor marker the PRIMARY test
	// pins.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 ||
		len(pack.PayEquityReviews) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent, and no locality overlays where the
	// matrix marks LOCAL F.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("Idaho carries locality overlays the matrix marks F")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_ID_001_Mutation seeds mutants into the Idaho draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_ID_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "ID")
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
