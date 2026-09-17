package legal

// LEGAL-ST-WY-001 verification tests.
//
// The Wyoming draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched, the
// drug-testing gap carried as a visible VERIFY emission — and the
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

func wyomingPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "WY").Candidate()
	if err != nil {
		t.Fatalf("Candidate(WY): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_WY_001 verifies the Wyoming draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_WY_001(t *testing.T) {
	def := loadStateDraft(t, "WY")
	if def.PackID != "us-wy-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Wyoming draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "WY" {
		t.Fatalf("subdivision=%q, want WY", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := wyomingPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only (F cell, tipped $2.13/hr with the
	// wage-guarantee top-up). No state figure above the federal floor
	// may appear.
	for _, floor := range pack.WageFloors {
		if got := floor.FloorAmount.String(); got != "" {
			t.Fatalf("floor=%q, Wyoming states no minimum above federal", got)
		}
	}
	// PAY_FREQUENCY: semimonthly for industrial/factory operations,
	// itemized stub required (§ 27-4-101).
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("pay frequency=%+v, want the semimonthly floor", pack.PayFrequencyConstraints)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Section, "27-4-101") {
		t.Fatalf("section=%q, want § 27-4-101", pack.PayFrequencyConstraints[0].Citation.Section)
	}
	// FINAL_PAY_DEADLINE: next regular payday or the employer's usual
	// schedule (§ 27-4-104).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].Citation.Section, "27-4-104") {
		t.Fatalf("section=%q, want § 27-4-104", pack.FinalPayDeadlines[0].Citation.Section)
	}
	// NON_COMPETE: void except for executives/managerial staff, business
	// sale, trade-secret protection, or training/relocation-expense
	// recovery, eff. 2025-07-01 (SF 107, § 6-3-501 et seq.).
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	rule := pack.NonCompeteThresholds[0].Rule
	if !strings.Contains(rule, "2025-07-01") {
		t.Fatalf("rule=%q, want the SF 107 effective date carried", rule)
	}
	if !strings.Contains(rule, "unenforceable") {
		t.Fatalf("rule=%q, want the post-employment voidness carried", rule)
	}
	if !strings.Contains(rule, "executive") {
		t.Fatalf("rule=%q, want the executive carve-out carried", rule)
	}
	// PAY_EQUITY_REVIEW: comparable work within the same establishment
	// (§ 27-4-302).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if equity.ComparatorStandard != "comparable work" {
		t.Fatalf("review=%+v, want the comparable-work standard", equity)
	}
	if !strings.Contains(equity.Citation.Section, "27-4-302") {
		t.Fatalf("section=%q, want § 27-4-302", equity.Citation.Section)
	}
	// PAY_STATEMENT: itemized stub with deductions and rate
	// (§ 27-4-101(b)).
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	if !hasString(pack.PayStatements[0].RequiredFields, "deductions") || !hasString(pack.PayStatements[0].RequiredFields, "pay_rate") {
		t.Fatalf("pay statement=%+v, want the itemized-stub fields carried", pack.PayStatements[0])
	}
	// ANTI_RETALIATION: workers-compensation retaliation (FLAG).
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "workers_compensation_claim") {
		t.Fatalf("anti-retaliation=%+v, want the workers-compensation rule", pack.AntiRetaliationRules)
	}
	// JOB_SECURITY: HANDBOOK_DISCLAIMER standard — a clear,
	// conspicuous, prominent disclaimer preserves at-will status
	// (Trabing v. Kinko's, 2002).
	if len(pack.JobSecurityRules) != 1 {
		t.Fatalf("job security rules=%d, want one", len(pack.JobSecurityRules))
	}
	if pack.JobSecurityRules[0].StandardKind != "HANDBOOK_DISCLAIMER" {
		t.Fatalf("job security=%+v, want the HANDBOOK_DISCLAIMER standard", pack.JobSecurityRules[0])
	}
	if note := pack.JobSecurityRules[0].Citation.Note; !strings.Contains(note, "conspicuous") {
		t.Fatalf("note=%q, want the conspicuous-disclaimer requirement carried", note)
	}
	// DRUG_TESTING: the corpus extraction finding — no locatable
	// statutory section — is carried as a VERIFY gap emission, exactly
	// as the contract requires, never as a silent absence.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the visible gap emission", len(pack.DrugTestingRules))
	}
	const sectionNotStated = "(statutory section not stated in the research file)"
	if pack.DrugTestingRules[0].Citation.Section == sectionNotStated &&
		pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("unsupported Y cell must stay VERIFY")
	}
	// BREACH_NOTIFICATION appears once (§ 40-12-501); RETENTION carries
	// the 2-year wage-record rule.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Section, "40-12-501") {
		t.Fatalf("section=%q, want § 40-12-501", pack.BreachNotifications[0].Citation.Section)
	}
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 2 {
		t.Fatalf("retention=%+v, want the 2-year wage-record rule", pack.RetentionRules)
	}
	// F cells stay absent: notices, pay transparency, field
	// restrictions, leave interactions, classifications, personnel
	// files, mini-warn, automated decisions. E-Verify is locality-scoped
	// (L pub) and separation filings are a dropped ? cell. (WAGE_FLOOR
	// is federal-only: no state figure, asserted above.)
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 ||
		len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F/L (or dropped ?) cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_WY_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_WY_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-wy.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "wyoming.golden.txt"))
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

// TestTodo_LEGAL_ST_WY_001_Conformance checks the Wyoming matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_WY_001_Conformance(t *testing.T) {
	pack := wyomingPack(t)
	// Table A Y cells: PAY_FREQUENCY, PAY_STATEMENT, NON_COMPETE,
	// PAY_EQUITY, RETENTION. Table B Y cells: FINAL_PAY,
	// DRUG_TESTING, ANTI_RETALIATION, JOB_SECURITY, BREACH.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.DrugTestingRules) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.JobSecurityRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: notices, pay transparency, field
	// restrictions, leave interactions, classifications, personnel
	// files. Table B F cells stay absent: mini-warn, automated
	// decisions. E-Verify is locality-scoped (L pub), never a
	// subdivision obligation.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F/L cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_WY_001_Mutation seeds mutants into the Wyoming
// draft's guards and asserts the loader notices. A surviving mutant
// means the guard is decorative.
func TestTodo_LEGAL_ST_WY_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "WY")
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
