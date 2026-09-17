package legal

// LEGAL-ST-MT-001 verification tests.
//
// The Montana draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the WDEA good-cause standard the
// JOB_SECURITY kind exists to model, the separation-filing gap left
// visible — and the guardrail that keeps the pack out of evaluation until
// counsel approves it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func montanaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "MT").Candidate()
	if err != nil {
		t.Fatalf("Candidate(MT): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_MT_001 verifies the Montana draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_MT_001(t *testing.T) {
	def := loadStateDraft(t, "MT")
	if def.PackID != "us-mt-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Montana draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "MT" {
		t.Fatalf("subdivision=%q, want MT", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := montanaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WDEA good cause after probation: the only statutory abolition of
	// at-will employment in the corpus, with the default 12-month
	// probation the workflow's HUMAN_TASK binding tracks.
	if len(pack.JobSecurityRules) != 1 {
		t.Fatalf("job security rules=%d, want the WDEA standard", len(pack.JobSecurityRules))
	}
	security := pack.JobSecurityRules[0]
	if security.StandardKind != "GOOD_CAUSE_AFTER_PROBATION" {
		t.Fatalf("standard=%q, want GOOD_CAUSE_AFTER_PROBATION", security.StandardKind)
	}
	if security.ProbationDays != 365 {
		t.Fatalf("probation=%d days, want the 12-month default", security.ProbationDays)
	}
	if !security.JustificationRequired {
		t.Fatal("post-probation discharge must require justification")
	}
	securityCite := draftObligation(t, "MT", "us-mt-job-security").Citation
	if !strings.Contains(securityCite.Section, "39-2-904") || !strings.Contains(securityCite.Section, "39-2-912") {
		t.Fatalf("job security citation=%+v, want probation and grievance sections", securityCite)
	}
	// The CPI-U-indexed floor with the statutory rounding note.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "10.85 USD" {
		t.Fatalf("floor=%q, want the $10.85 2026-01-01 rate", got)
	}
	if floor.Indexation != "CPI" {
		t.Fatalf("indexation=%q, want the CPI-U adjustment", floor.Indexation)
	}
	// Non-competes void outside business-sale and dissolution contexts:
	// the rule text must say void, or the section 6.3 void-wins
	// comparator has nothing to test against.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	compete := pack.NonCompeteThresholds[0]
	if !compete.ReCheckOnPayChange || !strings.Contains(compete.Rule, "void") {
		t.Fatalf("rule=%q, want voidance and recheck", compete.Rule)
	}
	// Four-year payroll retention, stricter than the federal three.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 4 {
		t.Fatalf("retention=%+v, want the 4-year payroll rule", pack.RetentionRules)
	}
	// No frequency mandate: the extractor's semi-monthly floor overstated
	// the research, so the verified body carries no minimum frequency.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	if got := pack.PayFrequencyConstraints[0].MinimumFrequency; got != "" {
		t.Fatalf("minimum frequency=%q, Montana mandates none", got)
	}
	// Final pay names the resignation rule alongside the discharge rule.
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	if got := pack.FinalPayDeadlines[0].DeadlineDescription; !strings.Contains(got, "resignations due next regular payday") {
		t.Fatalf("deadline=%q, want the resignation rule", got)
	}
	// Separation filing is matrix Y with no research evidence: the gap
	// stays visible as VERIFY rather than reading as no duty.
	if got := draftObligation(t, "MT", "us-mt-separation-filing").Citation.ConfidenceMarker; got != "VERIFY" {
		t.Fatalf("separation filing marker=%q, want the visible VERIFY gap", got)
	}
	// No state pay-equity rule: the F cell stays empty.
	if len(pack.PayEquityReviews) != 0 {
		t.Fatalf("pay equity=%+v, Montana states none", pack.PayEquityReviews)
	}
	// No truncated extraction notes survive the verification pass.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-mt.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	for _, stub := range []string{"paper vs.\",", "qualifying positions:\",", "affected...\",", "(MCA § 28-2-703): Flag"} {
		if strings.Contains(body, stub) {
			t.Errorf("pack still carries truncated note %q", stub)
		}
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_MT_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_MT_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-mt.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "montana.golden.txt"))
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

// TestTodo_LEGAL_ST_MT_001_Conformance checks the Montana matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_MT_001_Conformance(t *testing.T) {
	pack := montanaPack(t)
	// Table A Y cells: WAGE_FLOOR, PAY_FREQ, NON_COMPETE. Table B Y
	// cells: FINAL_PAY, SEP_FILING, ANTI_RETAL, JOB_SEC, DRUG_TESTING,
	// BREACH_NOTIFICATION. NOTICE, RETENTION and PAY_STMT are `?` with
	// evidence, kept at VERIFY.
	counts := pack.KindCounts()
	for _, kind := range []ObligationType{ObligationTypeWageFloor, ObligationTypePayFrequency, ObligationTypeNonCompete, ObligationTypeFinalPayDeadline, ObligationTypeSeparationFiling, ObligationTypeAntiRetaliation, ObligationTypeJobSecurity, ObligationTypeDrugTesting, ObligationTypeBreachNotification, ObligationTypeNotice, ObligationTypeRetention, ObligationTypePayStatement} {
		if counts[kind] == 0 {
			t.Errorf("matrix Y/`?`-with-evidence cell %s has no pack obligation", kind)
		}
	}
	// Table A/B F cells the flow consumes must stay absent: field
	// restrictions, leave interactions, pay transparency, classifications,
	// pay equity, personnel files, E-Verify, mini-WARN.
	if len(pack.FieldRestrictions) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.PayTransparencyDuties) != 0 || len(pack.Classifications) != 0 ||
		len(pack.PayEquityReviews) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.MiniWARNTriggers) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// No preemption assertions: section 6.4 records none for Montana.
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

// TestTodo_LEGAL_ST_MT_001_Mutation seeds mutants into the Montana
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_MT_001_Mutation(t *testing.T) {
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
		// The probation length is the good-cause trigger: an undeclared
		// standard kind must not load as job security.
		{"job security on an unknown standard", func(d *PackDefinition) {
			findObligationByKind(d, "JOB_SECURITY").Body.StandardKind = "JUST_CAUSE"
		}},
		// Retention durations cannot run backwards.
		{"negative retention duration", func(d *PackDefinition) {
			findObligationByKind(d, "RETENTION").Body.DurationYears = -4
		}},
		// A non-compete threshold must name its rule.
		{"non-compete without a rule", func(d *PackDefinition) {
			findObligationByKind(d, "NON_COMPETE").Body.Rule = ""
		}},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			rejectMutant(t, "MT", m.mutate)
		})
	}
}
