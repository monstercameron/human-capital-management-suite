package legal

// LEGAL-ST-OK-001 verification tests.
//
// The Oklahoma draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the section 6.4 preemption assertion
// carried as data for LEGAL-013 (never an inline comparator exception),
// the matrix row matched — and the guardrail that keeps the pack out of
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

func oklahomaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "OK").Candidate()
	if err != nil {
		t.Fatalf("Candidate(OK): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_OK_001 verifies the Oklahoma draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_OK_001(t *testing.T) {
	def := loadStateDraft(t, "OK")
	if def.PackID != "us-ok-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Oklahoma draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "OK" {
		t.Fatalf("subdivision=%q, want OK", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := oklahomaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only. State Question 832 ($15/hr by 2029) was
	// rejected by voters in June 2026, so 40 O.S. § 197.2 leaves the $7.25
	// FLSA floor and the pack carries no state floor.
	if len(pack.WageFloors) != 0 {
		t.Fatalf("wage floors=%+v, want no state floor (federal-only)", pack.WageFloors)
	}
	// PAY_FREQUENCY: semimonthly minimum with the 11-day period-to-payday
	// gap, 40 O.S. § 165.2.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency=%+v, want the § 165.2 rule", pack.PayFrequencyConstraints)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Section, "165.2") {
		t.Fatalf("section=%q, want 40 O.S. § 165.2", pack.PayFrequencyConstraints[0].Citation.Section)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Note, "payday") {
		t.Fatalf("note=%q, want the payday-change flag carried", pack.PayFrequencyConstraints[0].Citation.Note)
	}
	// FINAL_PAY_DEADLINE: next designated payday, with the 2%/day
	// liquidated-damages penalty for willful non-bona-fide withholding,
	// 40 O.S. § 165.3.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	final := pack.FinalPayDeadlines[0]
	if !strings.Contains(final.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-payday rule", final.DeadlineDescription)
	}
	if !strings.Contains(final.Citation.Note, "2%") {
		t.Fatalf("note=%q, want the 2%% daily penalty carried", final.Citation.Note)
	}
	if !strings.Contains(final.Citation.Section, "165.3") {
		t.Fatalf("section=%q, want 40 O.S. § 165.3", final.Citation.Section)
	}
	// NON_COMPETE: "pure" non-competes void, non-solicitation of
	// established customers/employees enforceable under strict compliance,
	// 15 O.S. §§ 219A, 219B.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Citation.Section, "219A") {
		t.Fatalf("section=%q, want 15 O.S. § 219A", nc.Citation.Section)
	}
	if !strings.Contains(nc.Rule, "15 O.S.") {
		t.Fatalf("rule=%q, want the title-15 enforceability check", nc.Rule)
	}
	// PreemptionAssertion: LEAVE_INTERACTION preempted at locality level
	// statewide, 40 O.S. § 160 — data for LEGAL-013's evaluation stage,
	// never an inline exception in a comparator.
	if len(pack.PreemptionAssertions) != 1 {
		t.Fatalf("preemptions=%+v, want the § 160 assertion", pack.PreemptionAssertions)
	}
	pre := pack.PreemptionAssertions[0]
	if pre.Kind != ObligationTypeLeaveInteraction || pre.Scope != "LOCALITY_ONLY" {
		t.Fatalf("preemption=%+v, want LEAVE_INTERACTION at LOCALITY_ONLY", pre)
	}
	if !strings.Contains(pre.Citation.Section, "160") {
		t.Fatalf("section=%q, want 40 O.S. § 160", pre.Citation.Section)
	}
	// The preempted kind stays out of the state pack.
	if len(pack.LeaveInteractions) != 0 {
		t.Fatalf("leave=%+v, preempted locality kind must stay absent", pack.LeaveInteractions)
	}
	// DRUG_TESTING: written policy and 10-day employee notice, 40 O.S.
	// §§ 551-563, with the medical-marijuana safety-sensitive
	// zero-tolerance carve-out eff. 2026-11-01, 63 O.S. § 427.8 (HB 3127).
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	if !dt.WrittenPolicyRequired {
		t.Fatalf("drug testing=%+v, want the written-policy requirement", dt)
	}
	if !hasString(dt.PermittedBases, "safety_sensitive_role") {
		t.Fatalf("drug testing=%+v, want the safety-sensitive basis", dt)
	}
	if !hasString(dt.ProtectedStatus, "medical_cannabis_patient") {
		t.Fatalf("drug testing=%+v, want the patient protection carried", dt)
	}
	if !strings.Contains(dt.Citation.Section, "427.8") {
		t.Fatalf("section=%q, want 63 O.S. § 427.8", dt.Citation.Section)
	}
	// PAY_EQUITY_REVIEW, RETENTION, PAY_STATEMENT (? at VERIFY),
	// ANTI_RETALIATION and BREACH each appear once.
	if len(pack.PayEquityReviews) != 1 || !pack.PayEquityReviews[0].DocumentationRequired ||
		!hasString(pack.PayEquityReviews[0].ProtectedBases, "sex") {
		t.Fatalf("pay equity=%+v, want the documented review", pack.PayEquityReviews)
	}
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 1 {
		t.Fatalf("retention=%+v, want the 1-year § 165.4 rule", pack.RetentionRules)
	}
	if len(pack.PayStatements) != 1 || !hasString(pack.PayStatements[0].RequiredFields, "pay_rate") {
		t.Fatalf("pay statements=%+v, want the ?-cell emission", pack.PayStatements)
	}
	if pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay statement must stay VERIFY: the matrix marks PAY_STMT ?")
	}
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "workers_compensation_claim") {
		t.Fatalf("anti-retaliation=%+v, want the workers-comp rule", pack.AntiRetaliationRules)
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Section, "161") {
		t.Fatalf("section=%q, want the § 161 breach rule", pack.BreachNotifications[0].Citation.Section)
	}
	// All other F cells stay absent: notices, transparency, field
	// restrictions, classifications, personnel files, e-verify
	// (locality-only public-contract rule, never a state pack duty),
	// mini-warn, separation filings, job security, automated decisions,
	// monitoring consents.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.MiniWARNTriggers) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_OK_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_OK_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ok.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "oklahoma.golden.txt"))
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

// TestTodo_LEGAL_ST_OK_001_Conformance checks the Oklahoma matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_OK_001_Conformance(t *testing.T) {
	pack := oklahomaPack(t)
	// Table A Y cells: PAY_FREQ, NON_COMPETE, PAY_EQUITY, RETENTION.
	// Table B Y cells: FINAL_PAY, DRUG_TEST, ANTI_RETAL, BREACH.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.DrugTestingRules) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// The ? cell is carried, not dropped.
	if len(pack.PayStatements) == 0 {
		t.Fatal("the PAY_STMT ? cell has no pack emission")
	}
	// Table A P cell stays absent at state level with the assertion
	// carried as data.
	if len(pack.LeaveInteractions) != 0 || len(pack.PreemptionAssertions) != 1 {
		t.Fatal("the preempted LEAVE cell is mishandled")
	}
	// Table A F cells stay absent: notices, transparency, field
	// restrictions, wage floor (federal-only), classifications,
	// personnel files.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.WageFloors) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_OK_001_Mutation seeds mutants into the Oklahoma
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_OK_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "OK")
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
