package legal

// LEGAL-ST-KS-001 verification tests.
//
// The Kansas draft is agent-verified, not counsel-reviewed: the pack
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

func kansasPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "KS").Candidate()
	if err != nil {
		t.Fatalf("Candidate(KS): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_KS_001 verifies the Kansas draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_KS_001(t *testing.T) {
	def := loadStateDraft(t, "KS")
	if def.PackID != "us-ks-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Kansas draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "KS" {
		t.Fatalf("subdivision=%q, want KS", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := kansasPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only floor marker (K.S.A. 44-1202). Kansas sets
	// no state premium, so the marker carries no amount, exactly like the
	// Alabama template.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, Kansas states no premium", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Section, "K.S.A. 44-1202") {
		t.Fatalf("section=%q, want K.S.A. 44-1202", pack.WageFloors[0].Citation.Section)
	}
	// NOTICE: written pay-rate-change notice with the reduction notice
	// preceding the affected work (K.S.A. 44-320, K.A.R. 49-20-1) — never
	// a federal cite.
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
	if !strings.Contains(notice.Citation.Section, "K.S.A. 44-320") {
		t.Fatalf("section=%q, want K.S.A. 44-320", notice.Citation.Section)
	}
	// PAY_FREQUENCY: at least semimonthly (K.S.A. 44-313 to 44-327,
	// operative minimum at 44-314).
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum=%q, want the semimonthly minimum", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "K.S.A. 44-31") {
		t.Fatalf("section=%q, want the K.S.A. 44-313 to 44-327 range", freq.Citation.Section)
	}
	// NON_COMPETE is a `?` cell: common-law reasonableness only (Weber v.
	// Tillman) — K.S.A. 50-163 does not reach non-competes — carried at
	// VERIFY with the SB 241 cite.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Citation.Section, "SB 241") {
		t.Fatalf("section=%q, want SB 241", nc.Citation.Section)
	}
	for _, want := range []string{"Weber", "does not reach"} {
		if !strings.Contains(nc.Rule, want) {
			t.Errorf("rule=%q, want %q", nc.Rule, want)
		}
	}
	if nc.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("non-compete must stay VERIFY: the matrix marks NON_COMPETE ?")
	}
	// CLASSIFICATION carries the contractor test (K.S.A. § 44-701): the
	// only reading the closed contract dimension enum can type. The SB
	// 241 non-solicit safe harbors live beyond the note budget and are
	// flagged for the covenant-vocabulary follow-up, not approximated
	// into the wrong dimension.
	if len(pack.Classifications) != 1 {
		t.Fatalf("classifications=%d, want one", len(pack.Classifications))
	}
	classification := pack.Classifications[0]
	if classification.Dimension != "CONTRACTOR" {
		t.Fatalf("dimension=%q, want CONTRACTOR", classification.Dimension)
	}
	if !strings.Contains(classification.Citation.Section, "44-701") {
		t.Fatalf("section=%q, want K.S.A. § 44-701", classification.Citation.Section)
	}
	// BREACH_NOTIFICATION: without unreasonable delay, typically 3-5
	// days (K.S.A. 50-7a01).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "K.S.A. 50-7a01") {
		t.Fatalf("section=%q, want K.S.A. 50-7a01", breach.Citation.Section)
	}
	for _, want := range []string{"without unreasonable delay", "days"} {
		if !strings.Contains(breach.Citation.Note, want) {
			t.Errorf("note=%q, want %q", breach.Citation.Note, want)
		}
	}
	// ANTI_RETALIATION names jury duty and workers' compensation claims
	// (matrix Y).
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	anti := pack.AntiRetaliationRules[0]
	if !hasString(anti.ProtectedActivities, "jury_duty") ||
		!hasString(anti.ProtectedActivities, "workers_compensation_claim") {
		t.Fatalf("activities=%v, want jury duty and workers' compensation", anti.ProtectedActivities)
	}
	// No state mini-WARN, no retention duty, no E-Verify mandate (all F).
	if len(pack.MiniWARNTriggers) != 0 {
		t.Fatalf("mini-warn triggers=%d, want none", len(pack.MiniWARNTriggers))
	}
	if len(pack.RetentionRules) != 0 {
		t.Fatalf("retention rules=%d, want none", len(pack.RetentionRules))
	}
	if len(pack.EVerifyChecks) != 0 {
		t.Fatalf("e-verify checks=%d, want none", len(pack.EVerifyChecks))
	}
	// No truncated extraction notes survive, and the only gaps left
	// visible are the separation-filing and drug-testing statutes the
	// research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ks.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 2 {
		t.Errorf("pack carries %d visible section gaps, want exactly the separation-filing and drug-testing ones", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_KS_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_KS_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ks.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "kansas.golden.txt"))
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

// TestTodo_LEGAL_ST_KS_001_Conformance checks the Kansas matrix row against
// the pack and proves every registered state draft still loads and
// validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_KS_001_Conformance(t *testing.T) {
	pack := kansasPack(t)
	// Table A Y cells: NOTICE, PAY_FREQ, CLASSIFICATION.
	// Table B Y cells: FINAL_PAY, ANTI_RETALIATION, BREACH_NOTIFICATION.
	if len(pack.Notices) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.Classifications) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions and the federal-only floor marker the PRIMARY test pins.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.PayStatements) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.PayEquityReviews) != 0 || len(pack.RetentionRules) != 0 ||
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
		t.Fatal("Kansas carries locality overlays the matrix marks F")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_KS_001_Mutation seeds mutants into the Kansas draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_KS_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "KS")
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
