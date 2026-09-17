package legal

// LEGAL-ST-TN-001 verification tests.
//
// The Tennessee draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched, the
// preemption assertions carried as data — and the guardrail that keeps
// the pack out of evaluation until counsel approves it. They do not,
// and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tennesseePack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "TN").Candidate()
	if err != nil {
		t.Fatalf("Candidate(TN): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_TN_001 verifies the Tennessee draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_TN_001(t *testing.T) {
	def := loadStateDraft(t, "TN")
	if def.PackID != "us-tn-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Tennessee draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "TN" {
		t.Fatalf("subdivision=%q, want TN", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := tennesseePack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only (F cell). No state figure above the
	// federal $7.25/hr floor may appear; a federal-baseline marker with
	// no state amount is the only other shape the extractor may emit.
	for _, floor := range pack.WageFloors {
		if got := floor.FloorAmount.String(); got != "" {
			t.Fatalf("floor=%q, Tennessee states no minimum above federal", got)
		}
	}
	// PAY_FREQUENCY: at least monthly (§ 50-2-103).
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "MONTHLY" {
		t.Fatalf("pay frequency=%+v, want the at-least-monthly floor", pack.PayFrequencyConstraints)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Section, "50-2-103") {
		t.Fatalf("section=%q, want § 50-2-103", pack.PayFrequencyConstraints[0].Citation.Section)
	}
	// FINAL_PAY_DEADLINE: LATER_OF next regular payday or 21 days after
	// separation (§ 50-2-103).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "21 days") {
		t.Fatalf("deadline=%q, want the 21-day limb carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "later") {
		t.Fatalf("deadline=%q, want the LATER_OF comparator carried", finalPay.DeadlineDescription)
	}
	// LEAVE_INTERACTION: carried once (matrix Y). The federal FMLA floor
	// and the locality-preemption interaction are the extracted facet.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	if !strings.Contains(pack.LeaveInteractions[0].InteractionRule, "FMLA") {
		t.Fatalf("leave=%q, want the FMLA floor carried", pack.LeaveInteractions[0].InteractionRule)
	}
	// NON_COMPETE: common-law reasonableness with the pay-change recheck
	// (§ 63-1-148).
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	if !strings.Contains(pack.NonCompeteThresholds[0].Rule, "reasonableness") {
		t.Fatalf("rule=%q, want the common-law reasonableness test", pack.NonCompeteThresholds[0].Rule)
	}
	// PreemptionAssertion: WAGE_FLOOR and LEAVE_INTERACTION preempted at
	// locality level statewide (Tenn. Code § 50-2-112). Data for
	// LEGAL-013's evaluation stage, never an inline comparator exception.
	if len(pack.PreemptionAssertions) != 2 {
		t.Fatalf("preemption assertions=%d, want WAGE_FLOOR and LEAVE_INTERACTION", len(pack.PreemptionAssertions))
	}
	for _, a := range pack.PreemptionAssertions {
		if a.Scope != "LOCALITY_ONLY" {
			t.Fatalf("preemption scope=%q, want LOCALITY_ONLY", a.Scope)
		}
		if !strings.Contains(a.Citation.Section, "50-2-112") {
			t.Fatalf("section=%q, want Tenn. Code § 50-2-112", a.Citation.Section)
		}
	}
	// E_VERIFY: mandatory for 35+-FTE private employers (§ 50-1-703).
	// The headcount lives in the rule note: the loader carries no
	// typed E-Verify threshold.
	if len(pack.EVerifyChecks) != 1 {
		t.Fatalf("e-verify=%+v, want one", pack.EVerifyChecks)
	}
	if note := pack.EVerifyChecks[0].Note; !strings.Contains(note, "35+") {
		t.Fatalf("note=%q, want the 35-employee threshold carried", note)
	}
	// SEPARATION_FILING: matrix Y with no research evidence, so the gap
	// is recorded visibly at VERIFY rather than read as an absence.
	if len(pack.SeparationFilings) != 1 {
		t.Fatalf("separation filings=%d, want the visible gap emission", len(pack.SeparationFilings))
	}
	if pack.SeparationFilings[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("unsupported Y cell must stay VERIFY")
	}
	// MINI_WARN: 50-99-employee plant-closure notification (§ 50-1-601).
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	if !strings.Contains(pack.MiniWARNTriggers[0].Citation.Section, "50-1-601") {
		t.Fatalf("section=%q, want § 50-1-601", pack.MiniWARNTriggers[0].Citation.Section)
	}
	// PAY_STATEMENT (? with evidence) and DRUG_TESTING (?) each appear
	// once at VERIFY; ANTI_RETALIATION (Y) and BREACH_NOTIFICATION (Y)
	// each appear once.
	if len(pack.PayStatements) != 1 || pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("pay statements=%+v, want the VERIFY ?-emission", pack.PayStatements)
	}
	if len(pack.DrugTestingRules) != 1 || pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("drug testing=%+v, want the VERIFY ?-emission", pack.DrugTestingRules)
	}
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "whistleblower_report") {
		t.Fatalf("anti-retaliation=%+v, want the whistleblower rule", pack.AntiRetaliationRules)
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Section, "47-18-2107") {
		t.Fatalf("section=%q, want § 47-18-2107", pack.BreachNotifications[0].Citation.Section)
	}
	// RETENTION (? with evidence) appears once at VERIFY.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("retention=%+v, want the VERIFY ?-emission", pack.RetentionRules)
	}
	// F cells stay absent: notices, pay transparency, field
	// restrictions, classifications, pay equity, personnel files, job
	// security, automated decisions. (WAGE_FLOOR is federal-only: no
	// state figure, asserted above.)
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_TN_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_TN_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-tn.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "tennessee.golden.txt"))
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

// TestTodo_LEGAL_ST_TN_001_Conformance checks the Tennessee matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_TN_001_Conformance(t *testing.T) {
	pack := tennesseePack(t)
	// Table A Y cells: PAY_FREQUENCY, LEAVE, NON_COMPETE. Table A ?
	// cells with evidence: PAY_STATEMENT, RETENTION. Table B Y cells:
	// FINAL_PAY, MINI_WARN, SEP_FILING, E_VERIFY, ANTI_RETALIATION,
	// BREACH_NOTIFICATION. Table B ? cell with evidence: DRUG_TESTING.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.LeaveInteractions) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayStatements) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 || len(pack.SeparationFilings) == 0 ||
		len(pack.EVerifyChecks) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 ||
		len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix Y (or evidenced ?) cell has no pack obligation")
	}
	// Table A F cells stay absent: notices, pay transparency, field
	// restrictions, classifications, pay equity, personnel files. Table
	// B F cells stay absent: job security, automated decisions. Table B
	// P cell (LOCAL) carries no locality obligation: the subdivision
	// pack has an empty locality path.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 || len(pack.PersonnelFileRules) != 0 ||
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

// TestTodo_LEGAL_ST_TN_001_Mutation seeds mutants into the Tennessee
// draft's guards and asserts the loader notices. A surviving mutant
// means the guard is decorative.
func TestTodo_LEGAL_ST_TN_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "TN")
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
