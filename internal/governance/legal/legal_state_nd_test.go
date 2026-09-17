package legal

// LEGAL-ST-ND-001 verification tests.
//
// The North Dakota draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the strict non-compete voidance the
// section 6.3 void-wins comparator tests against, the matrix row matched —
// and the guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func northDakotaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "ND").Candidate()
	if err != nil {
		t.Fatalf("Candidate(ND): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_ND_001 verifies the North Dakota draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_ND_001(t *testing.T) {
	def := loadStateDraft(t, "ND")
	if def.PackID != "us-nd-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the North Dakota draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "ND" {
		t.Fatalf("subdivision=%q, want ND", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := northDakotaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// Federal-only floor: no state amount, hourly basis. No increase
	// since 2009.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, North Dakota states no minimum", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	floorCite := draftObligation(t, "ND", "us-nd-wage-floor").Citation
	if !strings.Contains(floorCite.Section, "34-06-22") {
		t.Fatalf("floor citation=%+v, want section 34-06-22", floorCite)
	}
	// Final pay by the earlier of next payday or 15 days, with accrued
	// leave paid unless the narrow exception is met.
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalCite := draftObligation(t, "ND", "us-nd-final-pay-deadline").Citation
	if finalCite.ConfidenceMarker != "CONFIRMED" {
		t.Fatalf("final pay marker=%q, the deadline is stated law", finalCite.ConfidenceMarker)
	}
	if got := pack.FinalPayDeadlines[0].DeadlineDescription; !strings.Contains(got, "15 days") || !strings.Contains(got, "accrued leave") {
		t.Fatalf("deadline=%q, want the 15-day rule with leave payout", got)
	}
	// One of the strictest voidance rules in the corpus: post-employment
	// non-competes and non-solicits are void outside business-sale and
	// partnership-dissolution contexts.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	compete := pack.NonCompeteThresholds[0]
	if !compete.ReCheckOnPayChange || !strings.Contains(compete.Rule, "void") {
		t.Fatalf("rule=%q, want voidance and recheck", compete.Rule)
	}
	competeCite := draftObligation(t, "ND", "us-nd-non-compete").Citation
	if competeCite.ConfidenceMarker != "CONFIRMED" {
		t.Fatalf("non-compete marker=%q, the voidance rule is stated law", competeCite.ConfidenceMarker)
	}
	// Whistleblower plus lawful off-duty activity protection.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	retaliation := pack.AntiRetaliationRules[0]
	for _, activity := range []string{"whistleblower_report", "lawful_off_duty_activity"} {
		if !hasString(retaliation.ProtectedActivities, activity) {
			t.Fatalf("activities=%v, want %q", retaliation.ProtectedActivities, activity)
		}
	}
	retaliationCite := draftObligation(t, "ND", "us-nd-anti-retaliation").Citation
	if !strings.Contains(retaliationCite.Section, "14-02.4") {
		t.Fatalf("anti-retaliation citation=%+v, want the off-duty-activity section", retaliationCite)
	}
	// Drug-testing discretion with the employer-pays rule.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	if len(pack.DrugTestingRules[0].PermittedBases) != 4 {
		t.Fatalf("permitted bases=%v, want the four permitted bases", pack.DrugTestingRules[0].PermittedBases)
	}
	// Breach penalties up to $5,000 per violation.
	if got := draftObligation(t, "ND", "us-nd-breach-notification").Citation.Note; !strings.Contains(got, "$5,000") {
		t.Fatalf("breach note=%q, want the penalty", got)
	}
	// Overtime is matrix F: the 40-hour federal threshold with paid leave
	// excluded from the count has no typed classification home distinct
	// from the federal baseline, so the kind stays absent.
	if len(pack.Classifications) != 0 {
		t.Fatalf("classifications=%+v, matrix F carries none", pack.Classifications)
	}
	// No truncated extraction notes survive the verification pass.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nd.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	for _, stub := range []string{"within 15...\",", "require manager...\",", "Drug testing (NDCC § 34-11.1):\",", "Data Breach Notification (NDCC § 51-30):\",", "subject to non-compete or non-solicit.\""} {
		if strings.Contains(body, stub) {
			t.Errorf("pack still carries truncated note %q", stub)
		}
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_ND_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_ND_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nd.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "north-dakota.golden.txt"))
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

// TestTodo_LEGAL_ST_ND_001_Conformance checks the North Dakota matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_ND_001_Conformance(t *testing.T) {
	pack := northDakotaPack(t)
	// Table A Y cells: PAY_FREQ, NON_COMPETE, PAY_EQUITY, RETENTION.
	// Table B Y cells: FINAL_PAY, ANTI_RETAL, BREACH_NOTIFICATION.
	// DRUG_TESTING is `?` with evidence, kept at VERIFY. The federal-only
	// WAGE_FLOOR is the review's explicit addition for the F cell.
	counts := pack.KindCounts()
	for _, kind := range []ObligationType{ObligationTypePayFrequency, ObligationTypeNonCompete, ObligationTypePayEquityReview, ObligationTypeRetention, ObligationTypeFinalPayDeadline, ObligationTypeAntiRetaliation, ObligationTypeBreachNotification, ObligationTypeDrugTesting, ObligationTypeWageFloor} {
		if counts[kind] == 0 {
			t.Errorf("matrix Y/`?`-with-evidence cell %s has no pack obligation", kind)
		}
	}
	// Table A/B F cells the flow consumes must stay absent: notices, field
	// restrictions, leave interactions, pay transparency, classifications,
	// personnel files, E-Verify, mini-WARN, job security, separation
	// filings.
	if len(pack.Notices) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.PayTransparencyDuties) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.SeparationFilings) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// No preemption assertions: section 6.4 records none for North Dakota.
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

// TestTodo_LEGAL_ST_ND_001_Mutation seeds mutants into the North Dakota
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_ND_001_Mutation(t *testing.T) {
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
		// A voidance rule must name its rule: without it the void-wins
		// comparator has nothing to read.
		{"non-compete without a rule", func(d *PackDefinition) {
			findObligationByKind(d, "NON_COMPETE").Body.Rule = ""
		}},
		// Retention durations cannot run backwards.
		{"negative retention duration", func(d *PackDefinition) {
			findObligationByKind(d, "RETENTION").Body.DurationYears = -2
		}},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			rejectMutant(t, "ND", m.mutate)
		})
	}
}
