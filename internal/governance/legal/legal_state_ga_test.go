package legal

// LEGAL-ST-GA-001 verification tests.
//
// The Georgia draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the corrected § 34-4-5 retention
// citation carried into a typed RETENTION obligation, the matrix row
// matched — and the guardrail that keeps the pack out of evaluation until
// counsel approves it. They do not, and must not, mark the pack reviewed.
//
// Georgia has no state-specific overtime or final-pay computation
// (federal-only), so this pack exercises no mutation-tested arithmetic.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func georgiaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "GA").Candidate()
	if err != nil {
		t.Fatalf("Candidate(GA): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_GA_001 verifies the Georgia draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_GA_001(t *testing.T) {
	def := loadStateDraft(t, "GA")
	if def.PackID != "us-ga-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Georgia draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "GA" {
		t.Fatalf("subdivision=%q, want GA", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := georgiaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only. The $5.15/hr state figure is superseded
	// by the FLSA, so the pack carries no state floor.
	if len(pack.WageFloors) != 0 {
		t.Fatalf("wage floors=%+v, want no state floor (federal-only)", pack.WageFloors)
	}
	// PAY_FREQUENCY: semi-monthly under O.C.G.A. § 34-7-2.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("pay frequency=%+v, want the semi-monthly floor", pack.PayFrequencyConstraints)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Section, "34-7-2") {
		t.Fatalf("section=%q, want § 34-7-2", pack.PayFrequencyConstraints[0].Citation.Section)
	}
	// RETENTION: the LEGAL-018 correction carried into a typed
	// obligation — 4-year best-practice tied to § 34-4-5 (records open to
	// inspection), not § 34-7-2 (pay frequency).
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	ret := pack.RetentionRules[0]
	if ret.DurationYears != 4 {
		t.Fatalf("retention=%+v, want the 4-year best-practice rule", ret)
	}
	if !strings.Contains(ret.Citation.Section, "34-4-5") {
		t.Fatalf("section=%q, want § 34-4-5, never § 34-7-2", ret.Citation.Section)
	}
	if strings.Contains(ret.Citation.Section, "34-7-2") {
		t.Fatalf("section=%q, the § 34-7-2 miscitation is back", ret.Citation.Section)
	}
	// NON_COMPETE: Restrictive Covenants Act recheck on pay change.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	// PAY_EQUITY_REVIEW: sex-based, 10+ employees, documented.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	pe := pack.PayEquityReviews[0]
	if !pe.DocumentationRequired || !hasString(pe.ProtectedBases, "sex") {
		t.Fatalf("review=%+v, want the documented sex-based review", pe)
	}
	if !strings.Contains(pe.Citation.Section, "34-5-3") {
		t.Fatalf("section=%q, want the §§ 34-5-1 to 34-5-7 equal-pay range", pe.Citation.Section)
	}
	// E_VERIFY: mandatory for 11+ employees.
	if len(pack.EVerifyChecks) != 1 || !pack.EVerifyChecks[0].RequiredOnNewHireOnly {
		t.Fatalf("e-verify=%+v, want the hiring-time check", pack.EVerifyChecks)
	}
	if !strings.Contains(pack.EVerifyChecks[0].Note, "11+") {
		t.Fatalf("note=%q, want the 11-employee threshold carried", pack.EVerifyChecks[0].Note)
	}
	// MINI_WARN: federal WARN shape (100-employee 60-day trigger); the
	// state Mass Separation Notice (DOL-402A, 48 hours, 25+ same-day
	// separations) is the SEPARATION_FILING below, distinct from WARN.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	if pack.MiniWARNTriggers[0].EmployeeThreshold != 100 || pack.MiniWARNTriggers[0].NoticeDays != 60 {
		t.Fatalf("mini-warn=%+v, want the federal WARN shape", pack.MiniWARNTriggers[0])
	}
	if len(pack.SeparationFilings) != 1 || pack.SeparationFilings[0].FormName != "DOL-402A" {
		t.Fatalf("separation filings=%+v, want the DOL-402A mass-separation notice", pack.SeparationFilings)
	}
	// LEAVE_INTERACTION: carried once.
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	// DRUG_TESTING (? with evidence) and BREACH_NOTIFICATION each appear
	// once; ANTI_RETALIATION is a ?-cell emission at VERIFY.
	if len(pack.DrugTestingRules) != 1 || !hasString(pack.DrugTestingRules[0].PermittedBases, "pre_employment") {
		t.Fatalf("drug testing=%+v, want the pre-employment rule", pack.DrugTestingRules)
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want the ?-cell emission", len(pack.AntiRetaliationRules))
	}
	if pack.AntiRetaliationRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("anti-retaliation must stay VERIFY: the matrix marks it ?")
	}
	// No state-specific final-pay computation: FINAL_PAY stays absent.
	if len(pack.FinalPayDeadlines) != 0 {
		t.Fatalf("final pay=%+v, want no state-specific computation", pack.FinalPayDeadlines)
	}
	// All other F cells stay absent: notices, transparency, field
	// restrictions, pay statements, classifications, personnel files,
	// job security, automated decisions, monitoring consents.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.PayStatements) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_GA_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_GA_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ga.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "georgia.golden.txt"))
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

// TestTodo_LEGAL_ST_GA_001_Conformance checks the Georgia matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_GA_001_Conformance(t *testing.T) {
	pack := georgiaPack(t)
	// Table A Y cells: PAY_FREQUENCY, LEAVE, NON_COMPETE, PAY_EQUITY,
	// RETENTION. Table B Y cells: MINI_WARN, SEP_FILING (DOL-402A),
	// E_VERIFY (11+), ANTI_RETALIATION, BREACH.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.LeaveInteractions) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.MiniWARNTriggers) == 0 || len(pack.SeparationFilings) == 0 || len(pack.EVerifyChecks) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// The corrected retention citation survives conformance too.
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "34-4-5") {
		t.Fatalf("retention section=%q, want § 34-4-5", pack.RetentionRules[0].Citation.Section)
	}
	// Table A F cells stay absent: notices, transparency, field
	// restrictions, wage floor (federal-only), pay statements,
	// classifications, personnel files.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.WageFloors) != 0 || len(pack.PayStatements) != 0 || len(pack.Classifications) != 0 ||
		len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}
