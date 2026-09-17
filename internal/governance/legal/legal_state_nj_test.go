package legal

// LEGAL-ST-NJ-001 verification tests.
//
// The New Jersey draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the `?`-cell VERIFY markers carried
// rather than dropped, the confirmed personnel-file absence, the
// corrected citations — and the guardrail that keeps the pack out of
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

func newJerseyPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "NJ").Candidate()
	if err != nil {
		t.Fatalf("Candidate(NJ): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_NJ_001 verifies the New Jersey draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_NJ_001(t *testing.T) {
	def := loadStateDraft(t, "NJ")
	if def.PackID != "us-nj-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the New Jersey draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "NJ" {
		t.Fatalf("subdivision=%q, want NJ", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := newJerseyPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// NOTICE: written notice at hire and in advance of any wage, payday
	// or deduction change, with the Wage Theft Act's 6-year limitations
	// and 200% liquidated damages (N.J.S.A. 34:11-4.1 et seq.).
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.Channel != "written" {
		t.Fatalf("channel=%q, want written", notice.Channel)
	}
	if !strings.Contains(notice.Citation.Section, "34:11-4.1") {
		t.Fatalf("section=%q, want N.J.S.A. 34:11-4.1", notice.Citation.Section)
	}
	for _, want := range []string{"6-year", "200%"} {
		if !strings.Contains(notice.Citation.Note, want) {
			t.Fatalf("note=%q, want %q carried", notice.Citation.Note, want)
		}
	}
	// FIELD_RESTRICTION: salary-history ban (N.J.S.A. 34:6B-20).
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "34:6B-20") {
		t.Fatalf("section=%q, want N.J.S.A. 34:6B-20", pack.FieldRestrictions[0].Citation.Section)
	}
	// RETENTION: 5-year payroll records (N.J.S.A. 34:11D-6), with the
	// 3-year general and 2-year personnel-file tiers named.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 5 {
		t.Fatalf("retention=%+v, want the 5-year payroll rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "34:11D-6") {
		t.Fatalf("section=%q, want N.J.S.A. 34:11D-6", pack.RetentionRules[0].Citation.Section)
	}
	// LEAVE_INTERACTION: earned sick leave at 1hr/30hrs with the 40-hour
	// cap (N.J.S.A. 34:11D-1) plus the state-funded family
	// leave/TDI program covering 15+ employers from July 2026
	// (N.J.S.A. 34:11B, 43:21-25 et seq.).
	if len(pack.LeaveInteractions) != 2 {
		t.Fatalf("leave interactions=%d, want sick leave plus family leave", len(pack.LeaveInteractions))
	}
	var sickLeave, familyLeave *LeaveInteraction
	for i := range pack.LeaveInteractions {
		li := &pack.LeaveInteractions[i]
		switch {
		case strings.Contains(li.Citation.Section, "34:11D-1"):
			sickLeave = li
		case strings.Contains(li.Citation.Section, "34:11B"):
			familyLeave = li
		}
	}
	if sickLeave == nil {
		t.Fatal("no sick-leave interaction citing N.J.S.A. 34:11D-1")
	}
	for _, want := range []string{"1 hour per 30 hours", "40"} {
		if !strings.Contains(sickLeave.InteractionRule, want) {
			t.Fatalf("sick leave=%q, want %q carried", sickLeave.InteractionRule, want)
		}
	}
	if familyLeave == nil {
		t.Fatal("no family-leave interaction citing N.J.S.A. 34:11B")
	}
	for _, want := range []string{"15", "2026"} {
		if !strings.Contains(familyLeave.InteractionRule, want) {
			t.Fatalf("family leave=%q, want %q carried", familyLeave.InteractionRule, want)
		}
	}
	// PAY_FREQUENCY is a `?` cell: twice-monthly paydays
	// (N.J.S.A. 34:11-4.2), carried at VERIFY.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum_frequency=%q, want the twice-monthly floor", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "34:11-4.2") {
		t.Fatalf("section=%q, want N.J.S.A. 34:11-4.2", freq.Citation.Section)
	}
	if freq.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// FINAL_PAY_DEADLINE: next regular payday after any termination
	// (N.J.S.A. 34:11-4.3).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "34:11-4.3") {
		t.Fatalf("section=%q, want N.J.S.A. 34:11-4.3", finalPay.Citation.Section)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-payday rule", finalPay.DeadlineDescription)
	}
	// PAY_TRANSPARENCY: wage-range-plus-benefits in postings and
	// internal-promotion notices for 10+ employers, eff. 2025-06-01
	// (N.J.S.A. 34:6B-23).
	if len(pack.PayTransparencyDuties) != 1 || pack.PayTransparencyDuties[0].Trigger != "internal_promotion" {
		t.Fatalf("pay transparency=%+v, want the promotion range disclosure", pack.PayTransparencyDuties)
	}
	transparency := pack.PayTransparencyDuties[0]
	if !strings.Contains(transparency.Citation.Section, "34:6B-23") {
		t.Fatalf("section=%q, want N.J.S.A. 34:6B-23", transparency.Citation.Section)
	}
	if !strings.Contains(transparency.RequiredDisclosure, "10 or more") {
		t.Fatalf("disclosure=%q, want the 10-or-more-employer threshold carried", transparency.RequiredDisclosure)
	}
	// MINI_WARN: NJ WARN 90-day notice for 100+-employee mass layoffs
	// with mandatory 1-week-per-year severance plus 4 additional weeks
	// for inadequate notice (N.J.S.A. 34:21-1 et seq.).
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.EmployeeThreshold != 100 || warn.NoticeDays != 90 {
		t.Fatalf("trigger=%+v, want the 100-employee/90-day rule", warn)
	}
	if !strings.Contains(warn.Citation.Section, "34:21-1") {
		t.Fatalf("section=%q, want N.J.S.A. 34:21-1", warn.Citation.Section)
	}
	if !strings.Contains(warn.Note, "severance") {
		t.Fatalf("note=%q, want the mandatory severance carried", warn.Note)
	}
	// WAGE_FLOOR: three 2026 tiers — $15.92/hr (6+ employees), $15.23/hr
	// (5 or fewer), $14.20/hr agricultural (N.J.S.A. 34:11-56a4).
	if len(pack.WageFloors) != 3 {
		t.Fatalf("wage floors=%d, want the three 2026 tiers", len(pack.WageFloors))
	}
	tiers := map[string]string{}
	for _, f := range pack.WageFloors {
		tiers[f.FloorAmount.String()] = f.WorkerClass
		if !strings.Contains(f.Citation.Section, "34:11-56a4") {
			t.Fatalf("section=%q, want N.J.S.A. 34:11-56a4", f.Citation.Section)
		}
		if f.Basis != "HOURLY" {
			t.Fatalf("basis=%q, want HOURLY", f.Basis)
		}
	}
	for _, want := range []string{"15.92 USD", "15.23 USD", "14.20 USD"} {
		if _, ok := tiers[want]; !ok {
			t.Fatalf("tiers=%v, want the %s tier carried", tiers, want)
		}
	}
	// PAY_EQUITY_REVIEW: 21 protected classes under the Diane B. Allen
	// Equal Pay Act (N.J.S.A. 10:5-12) — not the lone "age" basis the
	// raw extraction filed.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if !hasString(equity.ProtectedBases, "sex") || len(equity.ProtectedBases) < 18 {
		t.Fatalf("bases=%+v, want the 21-class review", equity.ProtectedBases)
	}
	if equity.ComparatorStandard != "substantially similar work" {
		t.Fatalf("comparator=%q, want substantially similar work", equity.ComparatorStandard)
	}
	if !strings.Contains(equity.Citation.Section, "10:5-12") {
		t.Fatalf("section=%q, want N.J.S.A. 10:5-12", equity.Citation.Section)
	}
	// PERSONNEL_FILE: New Jersey has no general private-sector
	// personnel-file-access statute — the fabricated "N.J.S.A. 34:8B-1
	// Personnel Files Act" citation named the Temporary Help Service
	// Firms Act — so the kind correctly carries no obligation here.
	if len(pack.PersonnelFileRules) != 0 {
		t.Fatalf("personnel file rules=%+v, want none: the absence is confirmed, not unresearched", pack.PersonnelFileRules)
	}
	// ANTI_RETALIATION: Wage Theft Act compliance with 200% liquidated
	// damages (N.J.S.A. 34:11-4.1).
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !hasString(ar.ProtectedActivities, "wage_theft_complaint") {
		t.Fatalf("activities=%+v, want the wage-theft complaint protected", ar.ProtectedActivities)
	}
	if !strings.Contains(ar.Citation.Note, "200%") {
		t.Fatalf("note=%q, want the 200-percent damages carried", ar.Citation.Note)
	}
	// DRUG_TESTING is a `?` cell: no private-sector testing statute, with
	// the N.J.S.A. 24:6I-52 cannabis-metabolites exception, carried at
	// VERIFY.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	if !strings.Contains(dt.Citation.Section, "24:6I-52") {
		t.Fatalf("section=%q, want N.J.S.A. 24:6I-52", dt.Citation.Section)
	}
	if dt.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks DRUG_TEST ?")
	}
	if !hasString(dt.ProtectedStatus, "lawful_off_duty_cannabis_use") {
		t.Fatalf("protected=%+v, want the cannabis-metabolites protection", dt.ProtectedStatus)
	}
	// BREACH_NOTIFICATION: Identity Theft Prevention Act (N.J.S.A.
	// 56:8-163) with the State Police report-first rule.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "56:8-163") {
		t.Fatalf("section=%q, want N.J.S.A. 56:8-163", breach.Citation.Section)
	}
	if !strings.Contains(breach.Citation.Note, "State Police") {
		t.Fatalf("note=%q, want the State Police report carried", breach.Citation.Note)
	}
	// No truncated extraction notes and no section placeholders survive.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nj.json"))
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

// TestTodo_LEGAL_ST_NJ_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_NJ_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nj.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "new-jersey.golden.txt"))
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

// TestTodo_LEGAL_ST_NJ_001_Conformance checks the New Jersey matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_NJ_001_Conformance(t *testing.T) {
	pack := newJerseyPack(t)
	// Table A Y cells: NOTICE, PAY_TRANSP, FIELD_RESTR, WAGE_FLOOR,
	// LEAVE, PAY_EQUITY, RETENTION. Table B Y cells: FINAL_PAY,
	// MINI_WARN, ANTI_RETALIATION, DRUG_TESTING, BREACH_NOTIFICATION.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.LeaveInteractions) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.DrugTestingRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions the PRIMARY test pins — and the confirmed
	// personnel-file absence stays absent.
	if len(pack.NonCompeteThresholds) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F (or confirmed-absent) cell gained a pack obligation")
	}
	// Table B F cells stay absent, and no locality overlays where the
	// matrix marks LOCAL F.
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("New Jersey carries locality overlays the matrix marks F")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_NJ_001_Mutation seeds mutants into the New Jersey
// draft and asserts the loader and the release digest notice. A surviving
// mutant means the guard is decorative.
func TestTodo_LEGAL_ST_NJ_001_Mutation(t *testing.T) {
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
		{"pay transparency disclosure emptied", func(d *PackDefinition) {
			for i := range d.Obligations {
				if d.Obligations[i].Kind == "PAY_TRANSPARENCY" {
					d.Obligations[i].Body.RequiredDisclosure = ""
				}
			}
		}},
		{"inverted effective window", func(d *PackDefinition) { d.Window.End = "2025-01-01" }},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			def := loadStateDraft(t, "NJ")
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

	t.Run("mini-warn notice period is inside the release digest", func(t *testing.T) {
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
		baseline := sign(t, loadStateDraft(t, "NJ"))
		mutant := loadStateDraft(t, "NJ")
		hit := false
		for i := range mutant.Obligations {
			if mutant.Obligations[i].Kind == "MINI_WARN" {
				mutant.Obligations[i].Body.NoticeDays = 60
				hit = true
			}
		}
		if !hit {
			t.Fatal("no mini-warn notice period to mutate")
		}
		if got := sign(t, mutant); got == baseline {
			t.Fatal("mutant survived: a federal 60-day notice signs to the same digest as the 90-day NJ rule")
		}
	})
}
