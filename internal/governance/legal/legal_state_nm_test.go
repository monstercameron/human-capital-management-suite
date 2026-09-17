package legal

// LEGAL-ST-NM-001 verification tests.
//
// The New Mexico draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the `?`-cell VERIFY markers carried
// rather than dropped, the corrected citations — and the guardrail that
// keeps the pack out of evaluation until counsel approves it. They do
// not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newMexicoPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "NM").Candidate()
	if err != nil {
		t.Fatalf("Candidate(NM): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_NM_001 verifies the New Mexico draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_NM_001(t *testing.T) {
	def := loadStateDraft(t, "NM")
	if def.PackID != "us-nm-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the New Mexico draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "NM" {
		t.Fatalf("subdivision=%q, want NM", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := newMexicoPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $12.00/hr statewide with the Santa Fe, Albuquerque,
	// Las Cruces and Bernalillo County overlays named (NMSA 1978
	// § 50-4-22). The LOCALITY-level overlay rows themselves arrive
	// with LEGAL-TOOL-009, not here.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want the single statewide floor", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "12.00 USD" {
		t.Fatalf("floor=%q, want the research-stated $12.00/hr", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if !strings.Contains(floor.Citation.Section, "50-4-22") {
		t.Fatalf("section=%q, want NMSA 50-4-22", floor.Citation.Section)
	}
	for _, want := range []string{"Santa Fe", "Las Cruces", "Bernalillo"} {
		if !strings.Contains(floor.Citation.Note, want) {
			t.Fatalf("note=%q, want the %s overlay named", floor.Citation.Note, want)
		}
	}
	// NOTICE is a `?` cell: written payday notice at hire with advance
	// notice of payday changes (§ 50-4-2), carried at VERIFY.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.Channel != "written" {
		t.Fatalf("channel=%q, want written", notice.Channel)
	}
	if !strings.Contains(notice.Citation.Section, "50-4-2") {
		t.Fatalf("section=%q, want NMSA 50-4-2", notice.Citation.Section)
	}
	if notice.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("notice must stay VERIFY: the matrix marks NOTICE ?")
	}
	// RETENTION: promotion records for a minimum 1 year with labor
	// commissioner inspection rights (§ 50-4-9).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 1 {
		t.Fatalf("retention=%+v, want the 1-year rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "50-4-9") {
		t.Fatalf("section=%q, want NMSA 50-4-9", pack.RetentionRules[0].Citation.Section)
	}
	// LEAVE_INTERACTION: Healthy Workplaces Act at 1hr/30hrs with the
	// 64-hour carryover cap, eff. 2022-07-01 (§ 50-17-1 et seq.).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.Citation.Section, "50-17-1") {
		t.Fatalf("section=%q, want NMSA 50-17-1", leave.Citation.Section)
	}
	for _, want := range []string{"1 hour per 30 hours", "64", "2022"} {
		if !strings.Contains(leave.InteractionRule, want) {
			t.Fatalf("rule=%q, want %q carried", leave.InteractionRule, want)
		}
	}
	// PAY_FREQUENCY: semimonthly payday floor, paydays no more than 16
	// days apart (§ 50-4-2).
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum_frequency=%q, want the semimonthly floor", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "50-4-2") {
		t.Fatalf("section=%q, want NMSA 50-4-2", freq.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: fixed wages within 5 days and other
	// compensation within 10 days on discharge, next payday on
	// resignation (§§ 50-4-4, 50-4-5).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "50-4-4") {
		t.Fatalf("section=%q, want NMSA 50-4-4", finalPay.Citation.Section)
	}
	for _, want := range []string{"5 days", "10 days", "next regular payday"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Fatalf("deadline=%q, want %q carried", finalPay.DeadlineDescription, want)
		}
	}
	// NON_COMPETE: void for physicians and the eight other listed
	// licensed healthcare professions, with 1-year non-solicitation
	// permitted (§ 50-4A-1).
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange {
		t.Fatal("non-compete must recheck on pay change: promotion into a practitioner role changes enforceability")
	}
	if !strings.Contains(nc.Citation.Section, "50-4A-1") {
		t.Fatalf("section=%q, want NMSA 50-4A-1", nc.Citation.Section)
	}
	if !strings.Contains(nc.Rule, "solicit") {
		t.Fatalf("rule=%q, want the 1-year non-solicitation permission carried", nc.Rule)
	}
	// PAY_EQUITY_REVIEW: Fair Pay for Women Act, sex-based, 4+ employers
	// (§ 28-23-1 et seq.) — not the "gender and age" bases the raw
	// extraction filed.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if len(equity.ProtectedBases) != 1 || !hasString(equity.ProtectedBases, "sex") {
		t.Fatalf("bases=%+v, want the sex-based review", equity.ProtectedBases)
	}
	if equity.EmployerSizeFloor != 4 {
		t.Fatalf("size floor=%d, want the 4-employee threshold", equity.EmployerSizeFloor)
	}
	if !strings.Contains(equity.Citation.Section, "28-23-1") {
		t.Fatalf("section=%q, want NMSA 28-23-1", equity.Citation.Section)
	}
	// ANTI_RETALIATION: private-employer ban-the-box (§ 28-2-3.1) — not
	// the section-less generic retaliation note the raw extraction
	// filed.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !strings.Contains(ar.Citation.Section, "28-2-3.1") {
		t.Fatalf("section=%q, want NMSA 28-2-3.1", ar.Citation.Section)
	}
	if !strings.Contains(ar.Citation.Note, "initial application") {
		t.Fatalf("note=%q, want the initial-application inquiry ban carried", ar.Citation.Note)
	}
	// DRUG_TESTING is a `?` cell: no restricting statute, with medical
	// marijuana under the Lynn and Erin Compassionate Use Act
	// (§ 26-2B), carried at VERIFY.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	if !strings.Contains(dt.Citation.Section, "26-2B") {
		t.Fatalf("section=%q, want NMSA 26-2B", dt.Citation.Section)
	}
	if dt.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks DRUG_TEST ?")
	}
	if !hasString(dt.PermittedBases, "pre_employment") {
		t.Fatalf("permitted=%+v, want pre-employment testing", dt.PermittedBases)
	}
	// BREACH_NOTIFICATION: 45-day notification rule (§ 57-12C-1 et seq.).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "57-12C-1") {
		t.Fatalf("section=%q, want NMSA 57-12C-1", breach.Citation.Section)
	}
	if breach.SubjectDeadlineDays != 45 {
		t.Fatalf("deadline=%d, want the 45-day rule", breach.SubjectDeadlineDays)
	}
	// No truncated extraction notes and no section placeholders survive.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nm.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if strings.Contains(body, "not stated") {
		t.Error("pack still carries extractor section placeholders")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_NM_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_NM_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nm.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "new-mexico.golden.txt"))
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

// TestTodo_LEGAL_ST_NM_001_Conformance checks the New Mexico matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_NM_001_Conformance(t *testing.T) {
	pack := newMexicoPack(t)
	// Table A Y cells: WAGE_FLOOR, PAY_FREQ, LEAVE, NON_COMPETE,
	// PAY_EQUITY, RETENTION. Table B Y cells: FINAL_PAY,
	// ANTI_RETALIATION, BREACH_NOTIFICATION. The LOCAL Y cell arrives
	// with LEGAL-TOOL-009, not here.
	if len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.LeaveInteractions) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions the PRIMARY test pins.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_NM_001_Mutation seeds mutants into the New Mexico
// draft and asserts the loader and the release digest notice. A surviving
// mutant means the guard is decorative.
func TestTodo_LEGAL_ST_NM_001_Mutation(t *testing.T) {
	mutants := []struct {
		name   string
		mutate func(*PackDefinition)
	}{
		{"schema version drift", func(d *PackDefinition) { d.SchemaVersion = 99 }},
		{"missing pack id", func(d *PackDefinition) { d.PackID = "" }},
		{"unknown jurisdiction level", func(d *PackDefinition) { d.Jurisdiction.Level = "GALAXY" }},
		{"unknown obligation kind", func(d *PackDefinition) { d.Obligations[0].Kind = "VIBES" }},
		{"citation without a section", func(d *PackDefinition) { d.Obligations[0].Citation.Section = "" }},
		{"final pay deadline emptied", func(d *PackDefinition) {
			for i := range d.Obligations {
				if d.Obligations[i].Kind == "FINAL_PAY_DEADLINE" {
					d.Obligations[i].Body.DeadlineDescription = ""
				}
			}
		}},
		{"non-compete rule emptied", func(d *PackDefinition) {
			for i := range d.Obligations {
				if d.Obligations[i].Kind == "NON_COMPETE" {
					d.Obligations[i].Body.Rule = ""
				}
			}
		}},
		{"inverted effective window", func(d *PackDefinition) { d.Window.End = "2025-01-01" }},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			def := loadStateDraft(t, "NM")
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

	t.Run("wage floor amount is inside the release digest", func(t *testing.T) {
		sign := func(t *testing.T, def PackDefinition) string {
			t.Helper()
			candidate, err := def.Candidate()
			if err != nil {
				t.Fatalf("Candidate: %v", err)
			}
			release, err := candidate.Sign(SigningRoleReleasePublisher, fixedSigner(t, 0xA1))
			if err != nil {
				t.Fatalf("Sign: %v", err)
			}
			if release.Digest == "" {
				t.Fatal("signed release carries no digest")
			}
			return release.Digest
		}
		baseline := sign(t, loadStateDraft(t, "NM"))
		mutant := loadStateDraft(t, "NM")
		hit := false
		for i := range mutant.Obligations {
			if mutant.Obligations[i].Kind == "WAGE_FLOOR" && mutant.Obligations[i].Body.FloorAmount != nil {
				mutant.Obligations[i].Body.FloorAmount.Amount = "99.99"
				hit = true
			}
		}
		if !hit {
			t.Fatal("no wage floor amount to mutate")
		}
		if got := sign(t, mutant); got == baseline {
			t.Fatal("mutant survived: a changed wage floor signs to the same digest")
		}
	})
}
