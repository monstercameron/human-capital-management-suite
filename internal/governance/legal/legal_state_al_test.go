package legal

// LEGAL-ST-AL-001 verification tests.
//
// The Alabama draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the contradiction removed, the matrix
// row matched — and the guardrail that keeps the pack out of evaluation
// until counsel approves it. They do not, and must not, mark the pack
// reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func alabamaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "AL").Candidate()
	if err != nil {
		t.Fatalf("Candidate(AL): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_AL_001 verifies the Alabama draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_AL_001(t *testing.T) {
	def := loadStateDraft(t, "AL")
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	pack := alabamaPack(t)

	// Federal-only floor: no state amount, hourly basis.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, Alabama states no minimum", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	// Pay equity names sex and race and demands documentation.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	review := pack.PayEquityReviews[0]
	if !review.DocumentationRequired || !hasString(review.ProtectedBases, "sex") || !hasString(review.ProtectedBases, "race") {
		t.Fatalf("review=%+v, want documented sex/race review", review)
	}
	// Three-year wage retention tied to the equal-pay section.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year wage rule", pack.RetentionRules)
	}
	// Non-compete carries the duration presumptions and the recheck.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange || !strings.Contains(nc.Rule, "2 years") || !strings.Contains(nc.Rule, "18 months") {
		t.Fatalf("rule=%q, want durations and recheck", nc.Rule)
	}
	// E-Verify is mandatory with no size exception.
	if len(pack.EVerifyChecks) != 1 || !pack.EVerifyChecks[0].RequiredOnNewHireOnly {
		t.Fatalf("e-verify=%+v", pack.EVerifyChecks)
	}
	// Anti-retaliation blocks; breach notifies in 45 days with the AG
	// threshold the research states.
	if len(pack.AntiRetaliationRules) != 1 || len(pack.DrugTestingRules) != 1 || len(pack.BreachNotifications) != 1 {
		t.Fatal("anti-retaliation, drug-testing and breach duties must each appear once")
	}
	if pack.BreachNotifications[0].SubjectDeadlineDays != 45 {
		t.Fatalf("breach=%+v, want the 45-day deadline", pack.BreachNotifications[0])
	}
	// No VERIFY markers, no truncated notes, no contradicted protections.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-al.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, `"VERIFY"`) {
		t.Error("pack still carries VERIFY confidence markers")
	}
	if strings.Contains(body, "(Ala.\"") {
		t.Error("pack still carries truncated extraction notes")
	}
	if strings.Contains(body, "lawful_off_duty_cannabis_use") {
		t.Error("pack still protects off-duty cannabis use the research contradicts")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func mustStatePackPath(t *testing.T, file string) string {
	t.Helper()
	path, err := PackDefinitionPath("states", file)
	if err != nil {
		t.Fatalf("PackDefinitionPath: %v", err)
	}
	return path
}

// TestTodo_LEGAL_ST_AL_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_AL_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-al.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "alabama.golden.txt"))
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

// TestTodo_LEGAL_ST_AL_001_Conformance checks the Alabama matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_AL_001_Conformance(t *testing.T) {
	pack := alabamaPack(t)
	// Table A Y cells: NON_COMPETE, PAY_EQUITY, RETENTION. Table B Y
	// cells: E_VERIFY, DRUG_TESTING, ANTI_RETALIATION, BREACH_NOTIFICATION.
	if len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.EVerifyChecks) == 0 || len(pack.DrugTestingRules) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells the flow consumes must stay absent: notices, pay
	// transparency, field restrictions, pay frequency, pay statements,
	// leave interactions, classifications, personnel files.
	if len(pack.Notices) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.PayFrequencyConstraints) != 0 ||
		len(pack.PayStatements) != 0 || len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 ||
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
