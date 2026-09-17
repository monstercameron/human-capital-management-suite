package legal

// LEGAL-ST-UT-001 verification tests.
//
// The Utah draft is agent-verified, not counsel-reviewed: the pack
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

func utahPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "UT").Candidate()
	if err != nil {
		t.Fatalf("Candidate(UT): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_UT_001 verifies the Utah draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is
// still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_UT_001(t *testing.T) {
	def := loadStateDraft(t, "UT")
	if def.PackID != "us-ut-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Utah draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "UT" {
		t.Fatalf("subdivision=%q, want UT", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := utahPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only (F cell, no tip-credit sub-minimum). No
	// state figure above the federal $7.25/hr floor may appear.
	for _, floor := range pack.WageFloors {
		if got := floor.FloorAmount.String(); got != "" {
			t.Fatalf("floor=%q, Utah states no minimum above federal", got)
		}
	}
	// PAY_FREQUENCY: semimonthly or more frequent, monthly allowed for
	// salaried employees (§ 34-28-3).
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("pay frequency=%+v, want the semimonthly floor", pack.PayFrequencyConstraints)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Section, "34-28-3") {
		t.Fatalf("section=%q, want § 34-28-3", pack.PayFrequencyConstraints[0].Citation.Section)
	}
	// FINAL_PAY_DEADLINE: 24 hours on involuntary discharge, next regular
	// payday on resignation, with the 60-day willful-withholding penalty
	// accumulation (§ 34-28-5).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "24 hours") {
		t.Fatalf("deadline=%q, want the 24-hour discharge limb carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "penalty") {
		t.Fatalf("deadline=%q, want the willful-withholding penalty carried", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "34-28-5") {
		t.Fatalf("section=%q, want § 34-28-5", finalPay.Citation.Section)
	}
	// NON_COMPETE: flat 1-year post-employment cap, unconditional
	// (§ 34-51-102, eff. 2016-05-10) — no key-employee or compensation
	// gate, unlike Idaho's conditional cap.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	rule := pack.NonCompeteThresholds[0].Rule
	if !strings.Contains(rule, "1 year") {
		t.Fatalf("rule=%q, want the flat 1-year cap carried", rule)
	}
	if !strings.Contains(rule, "2016") {
		t.Fatalf("rule=%q, want the 2016-05-10 effective date carried", rule)
	}
	// E_VERIFY: mandatory for 150+ employees, no penalty for
	// non-compliance (§ 13-47-101). The headcount lives in the rule
	// note: the loader carries no typed E-Verify threshold.
	if len(pack.EVerifyChecks) != 1 {
		t.Fatalf("e-verify=%+v, want one", pack.EVerifyChecks)
	}
	if note := pack.EVerifyChecks[0].Note; !strings.Contains(note, "150+") {
		t.Fatalf("note=%q, want the 150-employee threshold carried", note)
	}
	// BREACH_NOTIFICATION: "without unreasonable delay" where misuse is
	// reasonably likely (§ 13-44-101).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if note := pack.BreachNotifications[0].Citation.Note; !strings.Contains(note, "without unreasonable delay") {
		t.Fatalf("note=%q, want the delay standard carried", note)
	}
	// PAY_EQUITY_REVIEW: same-establishment comparable-work review,
	// documented (§ 34A-5-106(1)(a)).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if equity.ComparatorStandard != "comparable work" || !equity.DocumentationRequired {
		t.Fatalf("review=%+v, want the documented comparable-work review", equity)
	}
	// ANTI_RETALIATION (Y) appears once; RETENTION (Y) carries the 1-year
	// wage-record rule (§ 34-28-10).
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "whistleblower_report") {
		t.Fatalf("anti-retaliation=%+v, want the whistleblower rule", pack.AntiRetaliationRules)
	}
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 1 {
		t.Fatalf("retention=%+v, want the 1-year wage-record rule", pack.RetentionRules)
	}
	// PAY_STATEMENT and DRUG_TESTING (? cells with evidence) each appear
	// once at VERIFY.
	if len(pack.PayStatements) != 1 || pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("pay statements=%+v, want the VERIFY ?-emission", pack.PayStatements)
	}
	if len(pack.DrugTestingRules) != 1 || pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("drug testing=%+v, want the VERIFY ?-emission", pack.DrugTestingRules)
	}
	// F cells stay absent: notices, pay transparency, field
	// restrictions, leave interactions, classifications, personnel
	// files, mini-warn, separation filings (dropped ?), job security,
	// automated decisions. (WAGE_FLOOR is federal-only: no state
	// figure, asserted above.)
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.MiniWARNTriggers) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F (or dropped ?) cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_UT_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_UT_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ut.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "utah.golden.txt"))
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

// TestTodo_LEGAL_ST_UT_001_Conformance checks the Utah matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_UT_001_Conformance(t *testing.T) {
	pack := utahPack(t)
	// Table A Y cells: PAY_FREQUENCY, NON_COMPETE, PAY_EQUITY,
	// RETENTION. Table A ? cells with evidence: PAY_STATEMENT. Table B
	// Y cells: FINAL_PAY, E_VERIFY, ANTI_RETALIATION, BREACH.
	// Table B ? cells with evidence: DRUG_TESTING. Table B ? cell
	// without evidence: SEP_FILING (dropped).
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.EVerifyChecks) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 ||
		len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix Y (or evidenced ?) cell has no pack obligation")
	}
	// Table A F cells stay absent: notices, pay transparency, field
	// restrictions, leave interactions, classifications, personnel
	// files. Table B F cells stay absent: mini-warn, job security,
	// automated decisions.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.MiniWARNTriggers) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_UT_001_Mutation seeds mutants into the Utah draft's
// guards and asserts the loader notices. A surviving mutant means the
// guard is decorative.
func TestTodo_LEGAL_ST_UT_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "UT")
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
