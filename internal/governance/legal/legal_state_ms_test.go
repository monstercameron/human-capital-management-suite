package legal

// LEGAL-ST-MS-001 verification tests.
//
// The Mississippi draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the all-employer E-Verify mandate
// distinguished from the size-gated versions, the matrix row matched —
// and the guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.
//
// Mississippi has no state-specific overtime or final-pay computation, so
// the TEST MATRIX carries no MUTATION row: there is no mutation-tested
// arithmetic in this pack.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mississippiPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "MS").Candidate()
	if err != nil {
		t.Fatalf("Candidate(MS): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_MS_001 verifies the Mississippi draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_MS_001(t *testing.T) {
	def := loadStateDraft(t, "MS")
	if def.PackID != "us-ms-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Mississippi draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "MS" {
		t.Fatalf("subdivision=%q, want MS", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := mississippiPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// Federal-only floor: no state amount, hourly basis. The state's
	// $5.15/hr figure is preempted (Miss. Code 71-1-51), so the pack
	// carries the federal baseline explicitly rather than omitting it.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, Mississippi states no minimum", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	floorCite := draftObligation(t, "MS", "us-ms-wage-floor").Citation
	if !strings.Contains(floorCite.Section, "71-1-51") || !strings.Contains(floorCite.Note, "preempted") {
		t.Fatalf("floor citation=%+v, want the 71-1-51 preemption", floorCite)
	}
	// E-Verify is the strictest mandate in the corpus: all employers, no
	// size floor. The typed check must not admit a size gate.
	if len(pack.EVerifyChecks) != 1 || !pack.EVerifyChecks[0].RequiredOnNewHireOnly {
		t.Fatalf("e-verify=%+v, want the all-employer check", pack.EVerifyChecks)
	}
	everifyCite := draftObligation(t, "MS", "us-ms-e-verify").Citation
	if everifyCite.ConfidenceMarker != "CONFIRMED" {
		t.Fatalf("e-verify marker=%q, the all-employer mandate is stated law", everifyCite.ConfidenceMarker)
	}
	if !strings.Contains(everifyCite.Note, "regardless of size") {
		t.Fatalf("e-verify note=%q, want the no-size-exemption rule", everifyCite.Note)
	}
	// Pay frequency binds 50+-employee manufacturers and public-service
	// corporations to bi-weekly or better.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "BIWEEKLY" {
		t.Fatalf("pay frequency=%+v, want the bi-weekly floor", pack.PayFrequencyConstraints)
	}
	if got := draftObligation(t, "MS", "us-ms-pay-frequency").Citation.Note; !strings.Contains(got, "2nd and 4th Saturdays") {
		t.Fatalf("pay frequency note=%q, want the full statutory schedule", got)
	}
	// Sex-based equal-pay review for 5+ employers, privately enforced.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	review := pack.PayEquityReviews[0]
	if !review.DocumentationRequired || !hasString(review.ProtectedBases, "sex") {
		t.Fatalf("review=%+v, want a documented sex-based review", review)
	}
	if review.EmployerSizeFloor != 5 {
		t.Fatalf("employer size floor=%d, want the 5-employee threshold", review.EmployerSizeFloor)
	}
	// Jury-duty protection, drug-testing limits, and breach duties appear once each.
	if len(pack.AntiRetaliationRules) != 1 || len(pack.DrugTestingRules) != 1 || len(pack.BreachNotifications) != 1 {
		t.Fatal("anti-retaliation, drug-testing and breach duties must each appear once")
	}
	if got := draftObligation(t, "MS", "us-ms-breach-notification").Citation.Note; !strings.Contains(got, "most expedient time possible") {
		t.Fatalf("breach note=%q, want the statutory timing phrase", got)
	}
	// No VERIFY markers and no truncated notes survive the verification pass.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ms.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, `"VERIFY"`) {
		t.Error("pack still carries VERIFY confidence markers")
	}
	for _, stub := range []string{"per Miss.\",", "Notice: \\\"Miss.\",", "Jury Duty Leave: Miss.\","} {
		if strings.Contains(body, stub) {
			t.Errorf("pack still carries truncated note %q", stub)
		}
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_MS_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_MS_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ms.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "mississippi.golden.txt"))
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

// TestTodo_LEGAL_ST_MS_001_Conformance checks the Mississippi matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_MS_001_Conformance(t *testing.T) {
	pack := mississippiPack(t)
	// Table A Y cells: PAY_FREQ, PAY_EQUITY. Table B Y cells: E_VERIFY,
	// DRUG_TESTING, ANTI_RETALIATION, BREACH_NOTIFICATION. The federal-only
	// WAGE_FLOOR is the review's explicit addition for the F cell.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.EVerifyChecks) == 0 || len(pack.DrugTestingRules) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 ||
		len(pack.WageFloors) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A/B F cells the flow consumes must stay absent: notices, field
	// restrictions, leave interactions, pay statements, non-competes,
	// classifications, personnel files, final-pay deadlines, mini-WARN,
	// job security, separation filings.
	if len(pack.Notices) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.PayStatements) != 0 || len(pack.NonCompeteThresholds) != 0 || len(pack.Classifications) != 0 ||
		len(pack.PersonnelFileRules) != 0 || len(pack.FinalPayDeadlines) != 0 || len(pack.MiniWARNTriggers) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.SeparationFilings) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// No preemption assertions: section 6.4 records none for Mississippi.
	if len(pack.PreemptionAssertions) != 0 {
		t.Fatalf("preemptions=%+v, want none", pack.PreemptionAssertions)
	}
	// Every registered state draft loads and validates.
	for _, code := range allStatePackCodes() {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}
