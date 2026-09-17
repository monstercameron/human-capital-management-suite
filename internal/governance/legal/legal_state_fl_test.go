package legal

// LEGAL-ST-FL-001 verification tests.
//
// The Florida draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched — and the
// guardrail that keeps the pack out of evaluation until counsel approves
// it. They do not, and must not, mark the pack reviewed.
//
// Two RED-clause facts shape this pack: Florida has no state-specific
// overtime or final-pay computation (both defer to the federal FLSA), so
// no arithmetic-bearing obligation appears; and the matrix marks
// PAY_EQUITY F, so the pack carries no pay-equity review even though the
// research names a sex-based statute — an F cell is federal-baseline, not
// a pack obligation.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func floridaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "FL").Candidate()
	if err != nil {
		t.Fatalf("Candidate(FL): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_FL_001 verifies the Florida draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_FL_001(t *testing.T) {
	def := loadStateDraft(t, "FL")
	if def.PackID != "us-fl-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Florida draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "FL" {
		t.Fatalf("subdivision=%q, want FL", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := floridaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $14.00/hr general, $10.98/hr tipped (2025-09-30),
	// CPI-indexed toward $15.00/hr, Fla. Const. Art. X § 24.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "14.00 USD" {
		t.Fatalf("floor=%q, want the $14.00/hr general floor", got)
	}
	if floor.Indexation != "CPI" {
		t.Fatalf("indexation=%q, want CPI", floor.Indexation)
	}
	if !strings.Contains(floor.Citation.Section, "Art. X") {
		t.Fatalf("section=%q, want Fla. Const. Art. X § 24", floor.Citation.Section)
	}
	if !strings.Contains(floor.Citation.Note, "10.98") {
		t.Fatalf("note=%q, want the tipped figure carried", floor.Citation.Note)
	}
	// NON_COMPETE: Fla. Stat. § 542.335 reasonableness rule with the
	// pay-change recheck.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	if !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("rule=%q, want the pay-change recheck", pack.NonCompeteThresholds[0].Rule)
	}
	if !strings.Contains(pack.NonCompeteThresholds[0].Citation.Section, "542.335") {
		t.Fatalf("section=%q, want § 542.335", pack.NonCompeteThresholds[0].Citation.Section)
	}
	// E_VERIFY: mandatory for 25+-employee private employers.
	if len(pack.EVerifyChecks) != 1 || !pack.EVerifyChecks[0].RequiredOnNewHireOnly {
		t.Fatalf("e-verify=%+v, want the hiring-time check", pack.EVerifyChecks)
	}
	if !strings.Contains(pack.EVerifyChecks[0].Note, "25+") {
		t.Fatalf("note=%q, want the 25-employee threshold carried", pack.EVerifyChecks[0].Note)
	}
	// ANTI_RETALIATION: whistleblower protection that requires written
	// internal notice first, Fla. Stat. §§ 448.101-105.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !hasString(ar.ProtectedActivities, "whistleblower_report") {
		t.Fatalf("activities=%+v, want the whistleblower protection", ar.ProtectedActivities)
	}
	if !strings.Contains(ar.Citation.Section, "448.101") {
		t.Fatalf("section=%q, want §§ 448.101-105", ar.Citation.Section)
	}
	// DRUG_TESTING: drug-free workplace program, Fla. Stat. § 440.102.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	if !strings.Contains(pack.DrugTestingRules[0].Citation.Section, "440.102") {
		t.Fatalf("section=%q, want § 440.102", pack.DrugTestingRules[0].Citation.Section)
	}
	// BREACH_NOTIFICATION: 30-day notice, Fla. Stat. § 501.171.
	if len(pack.BreachNotifications) != 1 || pack.BreachNotifications[0].SubjectDeadlineDays != 30 {
		t.Fatalf("breach=%+v, want the 30-day clock", pack.BreachNotifications)
	}
	// SEP_FILING is a ? cell with evidence, carried at VERIFY.
	if len(pack.SeparationFilings) != 1 {
		t.Fatalf("separation filings=%d, want the ?-cell emission", len(pack.SeparationFilings))
	}
	if pack.SeparationFilings[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("separation filing must stay VERIFY: the matrix marks SEP_FILING ?")
	}
	// No state-specific final-pay computation: FINAL_PAY stays absent and
	// defers to the federal FLSA.
	if len(pack.FinalPayDeadlines) != 0 {
		t.Fatalf("final pay=%+v, want no state-specific computation", pack.FinalPayDeadlines)
	}
	// All other F cells stay absent: notices, transparency, field
	// restrictions, pay frequency, pay statements, leave, classifications,
	// pay equity, retention, personnel files, mini-warn, job security,
	// automated decisions, monitoring consents.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.PayFrequencyConstraints) != 0 || len(pack.PayStatements) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 || len(pack.RetentionRules) != 0 ||
		len(pack.PersonnelFileRules) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_FL_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_FL_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-fl.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "florida.golden.txt"))
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

// TestTodo_LEGAL_ST_FL_001_Conformance checks the Florida matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_FL_001_Conformance(t *testing.T) {
	pack := floridaPack(t)
	// Table A Y cells: WAGE_FLOOR, NON_COMPETE. Table B Y cells:
	// E_VERIFY (25+), DRUG_TESTING, ANTI_RETALIATION, BREACH (30d).
	if len(pack.WageFloors) == 0 || len(pack.NonCompeteThresholds) == 0 || len(pack.EVerifyChecks) == 0 ||
		len(pack.DrugTestingRules) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	if pack.BreachNotifications[0].SubjectDeadlineDays != 30 {
		t.Fatalf("breach=%+v, want the matrix-annotated 30-day clock", pack.BreachNotifications[0])
	}
	// Table A F cells stay absent: notices, transparency, field
	// restrictions, pay frequency, pay statements, leave,
	// classifications, pay equity, retention, personnel files.
	if len(pack.Notices) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.PayFrequencyConstraints) != 0 ||
		len(pack.PayStatements) != 0 || len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 ||
		len(pack.PayEquityReviews) != 0 || len(pack.RetentionRules) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}
