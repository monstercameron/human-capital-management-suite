package legal

// LEGAL-ST-MO-001 verification tests.
//
// The Missouri draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the HB 567 repeal applied to the typed
// body (not just the note), the matrix row matched — and the guardrail that
// keeps the pack out of evaluation until counsel approves it. They do not,
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

func missouriPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "MO").Candidate()
	if err != nil {
		t.Fatalf("Candidate(MO): %v", err)
	}
	return candidate.Pack()
}

// allStatePackCodes is the fifty-state iteration order for the conformance
// sweeps: every registered draft must still load and validate underneath
// each state's review.
func allStatePackCodes() []string {
	return []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"}
}

// draftObligation returns one obligation of a state's draft by id.
func draftObligation(t *testing.T, code, id string) ObligationJSON {
	t.Helper()
	for _, o := range loadStateDraft(t, code).Obligations {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("%s has no obligation %q", code, id)
	return ObligationJSON{}
}

// rejectMutant applies mutate to a copy of the state's draft and asserts
// the loader or candidate validation refuses it. A surviving mutant means
// the guard is decorative.
func rejectMutant(t *testing.T, code string, mutate func(*PackDefinition)) {
	t.Helper()
	def := loadStateDraft(t, code)
	def.Obligations = append([]ObligationJSON(nil), def.Obligations...)
	def.Preemptions = append([]PreemptionJSON(nil), def.Preemptions...)
	mutate(&def)
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
}

func findObligationByKind(def *PackDefinition, kind string) *ObligationJSON {
	for i := range def.Obligations {
		if def.Obligations[i].Kind == kind {
			return &def.Obligations[i]
		}
	}
	return nil
}

// TestTodo_LEGAL_ST_MO_001 verifies the Missouri draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_MO_001(t *testing.T) {
	def := loadStateDraft(t, "MO")
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.PackID != "us-mo-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Missouri draft", def.PackID)
	}
	if def.Jurisdiction.Subdivision != "MO" {
		t.Fatalf("subdivision=%q, want MO", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := missouriPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// Post-HB-567 floor in the typed body, not just the note: $15.00 with
	// indexing removed. A body still carrying 13.75/CPI models repealed law.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want the $15.00 post-HB-567 floor", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "15.00 USD" {
		t.Fatalf("floor=%q, want the $15.00 2026-01-01 rate", got)
	}
	if floor.Indexation != "NONE" {
		t.Fatalf("indexation=%q, HB 567 removed CPI adjustments", floor.Indexation)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	// The 30-day RSMo 290.100 reduction notice is the matrix Y rule; the
	// hire-notice absence it replaced is an F-shaped fact with no home here.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want the reduction-notice rule", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDirection != "BEFORE" || notice.TimingDays != 30 {
		t.Fatalf("notice=%+v, want 30 days BEFORE the reduction", notice)
	}
	noticeCite := draftObligation(t, "MO", "us-mo-notice").Citation
	if !strings.Contains(noticeCite.Section, "290.100") || !strings.Contains(noticeCite.Note, "$50") {
		t.Fatalf("notice citation=%+v, want RSMo 290.100 with the $50 penalty", noticeCite)
	}
	// Semi-monthly floor for corporations, monthly statements on record.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("pay frequency=%+v, want the corporate semi-monthly floor", pack.PayFrequencyConstraints)
	}
	// Immediate final pay with the 7-day-request/60-day-cap penalty trigger.
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	if got := pack.FinalPayDeadlines[0].DeadlineDescription; !strings.Contains(got, "60 days") || !strings.Contains(got, "day of discharge") {
		t.Fatalf("deadline=%q, want immediate pay with the 60-day cap", got)
	}
	// Sex-based equal-pay review with documentation.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	review := pack.PayEquityReviews[0]
	if !review.DocumentationRequired || !hasString(review.ProtectedBases, "sex") {
		t.Fatalf("review=%+v, want a documented sex-based review", review)
	}
	// Non-solicit safe harbor: one year or less presumed reasonable.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange || !strings.Contains(nc.Rule, "1 year") {
		t.Fatalf("rule=%q, want the one-year safe harbor and recheck", nc.Rule)
	}
	// Proposition A is repealed: no leave interaction may survive.
	if len(pack.LeaveInteractions) != 0 {
		t.Fatalf("leave interactions=%+v, Proposition A was repealed by HB 567", pack.LeaveInteractions)
	}
	// E-Verify for public contractors is matrix L: a subdivision pack
	// carries no such obligation; the locality/public-contract release owns it.
	if len(pack.EVerifyChecks) != 0 {
		t.Fatalf("e-verify=%+v, matrix L stays out of the subdivision pack", pack.EVerifyChecks)
	}
	// The DOL-8 separation filing names its form; its filing deadline is
	// still unverified in the research, so the marker stays VERIFY.
	if len(pack.SeparationFilings) != 1 || pack.SeparationFilings[0].FormName != "DOL-8 Separation Notice" {
		t.Fatalf("separation filings=%+v, want the named DOL-8 form", pack.SeparationFilings)
	}
	if got := draftObligation(t, "MO", "us-mo-separation-filing").Citation.ConfidenceMarker; got != "VERIFY" {
		t.Fatalf("separation filing marker=%q, the deadline is still unverified", got)
	}
	// Anti-retaliation is matrix Y with no evidenced section: the gap stays
	// visible as VERIFY rather than reading as no duty.
	if got := draftObligation(t, "MO", "us-mo-anti-retaliation").Citation.ConfidenceMarker; got != "VERIFY" {
		t.Fatalf("anti-retaliation marker=%q, want the visible VERIFY gap", got)
	}
	// No truncated extraction notes survive the verification pass.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-mo.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	for _, stub := range []string{"(RSMo § 290.080):", "(RSMo § 290.110):", "(RSMo § 290.410):", "PERSONAL INFORMATION EXPOSURE):", "Separation Notice:"} {
		if strings.Contains(body, stub) {
			t.Errorf("pack still carries truncated note %q", stub)
		}
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_MO_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_MO_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-mo.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "missouri.golden.txt"))
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

// TestTodo_LEGAL_ST_MO_001_Conformance checks the Missouri matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_MO_001_Conformance(t *testing.T) {
	pack := missouriPack(t)
	// Table A Y cells: NOTICE, WAGE_FLOOR, PAY_FREQ, PAY_STMT,
	// NON_COMPETE, PAY_EQUITY. Table B Y cells: FINAL_PAY, SEP_FILING,
	// ANTI_RETAL, BREACH. DRUG_TESTING is `?` with evidence, kept at VERIFY.
	counts := pack.KindCounts()
	for _, kind := range []ObligationType{ObligationTypeNotice, ObligationTypeWageFloor, ObligationTypePayFrequency, ObligationTypePayStatement, ObligationTypeNonCompete, ObligationTypePayEquityReview, ObligationTypeFinalPayDeadline, ObligationTypeSeparationFiling, ObligationTypeAntiRetaliation, ObligationTypeDrugTesting, ObligationTypeBreachNotification} {
		if counts[kind] == 0 {
			t.Errorf("matrix Y/`?`-with-evidence cell %s has no pack obligation", kind)
		}
	}
	// Table A/B F cells the flow consumes must stay absent; E_VERIFY is L
	// and LEAVE_INTERACTION is F after the Proposition A repeal.
	if len(pack.FieldRestrictions) != 0 || len(pack.RetentionRules) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.PayTransparencyDuties) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F/L cell gained a pack obligation")
	}
	// No preemption assertions: section 6.4 records none for Missouri.
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

// TestTodo_LEGAL_ST_MO_001_Mutation seeds mutants into the Missouri
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_MO_001_Mutation(t *testing.T) {
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
		// The HB 567 repeal moved the floor off any index: a wage basis the
		// vocabulary never named must not load as a floor.
		{"wage floor on an unknown basis", func(d *PackDefinition) {
			findObligationByKind(d, "WAGE_FLOOR").Body.Basis = "DAILY"
		}},
		// The 30-day reduction notice is a timing guard: a negative window
		// must not validate.
		{"negative notice window", func(d *PackDefinition) {
			findObligationByKind(d, "NOTICE").Body.TimingDays = -1
		}},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			rejectMutant(t, "MO", m.mutate)
		})
	}
}
