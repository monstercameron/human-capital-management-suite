package legal

// LEGAL-ST-NY-001 verification tests.
//
// The New York draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the `?`-cell VERIFY markers carried
// rather than dropped, the DISPUTED personnel-file kind, the corrected
// citations — and the guardrail that keeps the pack out of evaluation
// until counsel approves it. They do not, and must not, mark the pack
// reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newYorkPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "NY").Candidate()
	if err != nil {
		t.Fatalf("Candidate(NY): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_NY_001 verifies the New York draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_NY_001(t *testing.T) {
	def := loadStateDraft(t, "NY")
	if def.PackID != "us-ny-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the New York draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "NY" {
		t.Fatalf("subdivision=%q, want NY", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := newYorkPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: regional tiers — NYC/Long Island/Westchester
	// $17.00/hr, rest-of-state $16.00/hr, annual CPI-W adjustment
	// (Labor Law § 652).
	if len(pack.WageFloors) != 2 {
		t.Fatalf("wage floors=%d, want the downstate and upstate tiers", len(pack.WageFloors))
	}
	tiers := map[string]string{}
	for _, f := range pack.WageFloors {
		tiers[f.FloorAmount.String()] = f.WorkerClass
		if !strings.Contains(f.Citation.Section, "652") {
			t.Fatalf("section=%q, want Labor Law 652", f.Citation.Section)
		}
		if f.Basis != "HOURLY" {
			t.Fatalf("basis=%q, want HOURLY", f.Basis)
		}
	}
	for _, want := range []string{"17.00 USD", "16.00 USD"} {
		if _, ok := tiers[want]; !ok {
			t.Fatalf("tiers=%v, want the %s tier carried", tiers, want)
		}
	}
	// NOTICE: Wage Theft Prevention Act hire notice plus 7-day
	// pre-decrease notice (Labor Law § 195) — not the § 194-b
	// transparency citation the raw extraction filed here.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if !strings.Contains(notice.Citation.Section, "195") || strings.Contains(notice.Citation.Section, "194") {
		t.Fatalf("section=%q, want Labor Law 195", notice.Citation.Section)
	}
	if !strings.Contains(notice.Citation.Note, "7 days") {
		t.Fatalf("note=%q, want the 7-day pre-decrease rule carried", notice.Citation.Note)
	}
	// FIELD_RESTRICTION: salary-history ban (§ 194-a).
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "194-a") {
		t.Fatalf("section=%q, want Labor Law 194-a", pack.FieldRestrictions[0].Citation.Section)
	}
	// RETENTION: wage and hour records for 6 years (§ 195(4)) — not the
	// personnel-file note under a § 210-b citation the raw extraction
	// filed here.
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	retention := pack.RetentionRules[0]
	if retention.DurationYears != 6 {
		t.Fatalf("duration=%d, want the 6-year records rule", retention.DurationYears)
	}
	if !strings.Contains(retention.Citation.Section, "195(4)") {
		t.Fatalf("section=%q, want Labor Law 195(4)", retention.Citation.Section)
	}
	// LEAVE_INTERACTION: 40-hour paid sick leave (§ 196-b) plus Paid
	// Family Leave (12 weeks, 67% AWW) and Paid Prenatal Leave (20
	// hours/52 weeks, eff. 2025-01-01, § 196-c).
	if len(pack.LeaveInteractions) != 3 {
		t.Fatalf("leave interactions=%d, want sick, family and prenatal leave", len(pack.LeaveInteractions))
	}
	var sickLeave, familyLeave, prenatalLeave *LeaveInteraction
	for i := range pack.LeaveInteractions {
		li := &pack.LeaveInteractions[i]
		switch {
		case strings.Contains(li.Citation.Section, "196-b"):
			sickLeave = li
		case strings.Contains(li.Citation.Section, "196-c"):
			prenatalLeave = li
		case strings.Contains(li.Citation.Section, "Art. 9"):
			familyLeave = li
		}
	}
	if sickLeave == nil {
		t.Fatal("no sick-leave interaction citing § 196-b")
	}
	for _, want := range []string{"1 hour per 30 hours", "40"} {
		if !strings.Contains(sickLeave.InteractionRule, want) {
			t.Fatalf("sick leave=%q, want %q carried", sickLeave.InteractionRule, want)
		}
	}
	if familyLeave == nil {
		t.Fatal("no family-leave interaction citing Workers' Comp Law Art. 9")
	}
	for _, want := range []string{"12 weeks", "67%"} {
		if !strings.Contains(familyLeave.InteractionRule, want) {
			t.Fatalf("family leave=%q, want %q carried", familyLeave.InteractionRule, want)
		}
	}
	if prenatalLeave == nil {
		t.Fatal("no prenatal-leave interaction citing § 196-c")
	}
	if !strings.Contains(prenatalLeave.InteractionRule, "20 hours") {
		t.Fatalf("prenatal leave=%q, want the 20-hour rule carried", prenatalLeave.InteractionRule)
	}
	// PAY_FREQUENCY is a `?` cell: manual workers weekly, others no less
	// often than semi-monthly (§ 191), carried at VERIFY.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "WEEKLY" {
		t.Fatalf("minimum_frequency=%q, want the manual-worker weekly floor", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "191") {
		t.Fatalf("section=%q, want Labor Law 191", freq.Citation.Section)
	}
	if freq.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// FINAL_PAY_DEADLINE is a `?` cell: next regular payday after any
	// termination (§ 195) — not the records-retention note the raw
	// extraction filed here — carried at VERIFY.
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "195") || strings.Contains(finalPay.Citation.Section, "194") {
		t.Fatalf("section=%q, want Labor Law 195", finalPay.Citation.Section)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-payday rule", finalPay.DeadlineDescription)
	}
	if finalPay.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("final pay must stay VERIFY: the matrix marks FINAL_PAY ?")
	}
	// PAY_TRANSPARENCY: good-faith wage ranges in all
	// postings/transfers/promotions for 4+ employers (§ 194-b).
	if len(pack.PayTransparencyDuties) != 1 || pack.PayTransparencyDuties[0].Trigger != "internal_promotion" {
		t.Fatalf("pay transparency=%+v, want the promotion range disclosure", pack.PayTransparencyDuties)
	}
	if !strings.Contains(pack.PayTransparencyDuties[0].Citation.Section, "194-b") {
		t.Fatalf("section=%q, want Labor Law 194-b", pack.PayTransparencyDuties[0].Citation.Section)
	}
	if !strings.Contains(pack.PayTransparencyDuties[0].RequiredDisclosure, "4 or more") {
		t.Fatalf("disclosure=%q, want the 4-or-more-employer threshold carried", pack.PayTransparencyDuties[0].RequiredDisclosure)
	}
	// MINI_WARN: NY WARN at 50+ employees (not 100), 90-day notice (not
	// 60), 25+-layoff trigger (Labor Law Art. 25-A) with mandatory
	// severance — not the employer-threshold-25 the raw extraction
	// filed.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.EmployeeThreshold != 50 || warn.NoticeDays != 90 {
		t.Fatalf("trigger=%+v, want the 50-employee/90-day rule", warn)
	}
	if !strings.Contains(warn.Citation.Section, "25-A") {
		t.Fatalf("section=%q, want Labor Law Art. 25-A", warn.Citation.Section)
	}
	if !strings.Contains(warn.Note, "severance") {
		t.Fatalf("note=%q, want the mandatory severance carried", warn.Note)
	}
	// PAY_EQUITY_REVIEW: 21 protected classes for substantially similar
	// work (§ 194) — not the lone "age" basis the raw extraction filed.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if !hasString(equity.ProtectedBases, "sex") || len(equity.ProtectedBases) < 15 {
		t.Fatalf("bases=%+v, want the 21-class review", equity.ProtectedBases)
	}
	if equity.ComparatorStandard != "substantially similar work" {
		t.Fatalf("comparator=%q, want substantially similar work", equity.ComparatorStandard)
	}
	if !strings.Contains(equity.Citation.Section, "194") || strings.Contains(equity.Citation.Section, "194-") {
		t.Fatalf("section=%q, want Labor Law 194", equity.Citation.Section)
	}
	// PAY_STATEMENT: itemized statement each payday (§ 195(3)) — not the
	// pay-decrease notice content the raw extraction filed here.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	statement := pack.PayStatements[0]
	if !strings.Contains(statement.Citation.Section, "195(3)") {
		t.Fatalf("section=%q, want Labor Law 195(3)", statement.Citation.Section)
	}
	for _, field := range []string{"gross_wages", "net_wages", "deductions"} {
		if !hasString(statement.RequiredFields, field) {
			t.Fatalf("fields=%+v, want the itemized statement fields", statement.RequiredFields)
		}
	}
	// PERSONNEL_FILE is DISPUTED per LEGAL-018: S.3460/A.2107 passed
	// both chambers but signature is unconfirmed, so the kind stays in
	// the pack at DISPUTED and releasable only at UNREVIEWED.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want the disputed kind carried, not dropped", len(pack.PersonnelFileRules))
	}
	personnelFile := pack.PersonnelFileRules[0]
	if personnelFile.Citation.ConfidenceMarker != ConfidenceMarkerDisputed {
		t.Fatal("personnel file must read DISPUTED until the S.3460/A.2107 signature resolves")
	}
	if !strings.Contains(personnelFile.Citation.Section, "210-b") {
		t.Fatalf("section=%q, want the pending Labor Law 210-b citation", personnelFile.Citation.Section)
	}
	// ANTI_RETALIATION: reporting and whistleblowing protection with the
	// 6-year limitations (Labor Law § 215, § 740) — not the 7-day
	// lookback the raw extraction filed.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !strings.Contains(ar.Citation.Section, "215") {
		t.Fatalf("section=%q, want Labor Law 215", ar.Citation.Section)
	}
	if !hasString(ar.ProtectedActivities, "wage_complaint") {
		t.Fatalf("activities=%+v, want the wage complaint protected", ar.ProtectedActivities)
	}
	if ar.Disposition != "FLAG" {
		t.Fatalf("disposition=%q, want FLAG", ar.Disposition)
	}
	// DRUG_TESTING is a `?` cell: no testing ban, with the Executive Law
	// § 201-d cannabis-metabolites protection, carried at VERIFY.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	if !strings.Contains(dt.Citation.Section, "201-d") {
		t.Fatalf("section=%q, want Executive Law 201-d", dt.Citation.Section)
	}
	if dt.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks DRUG_TEST ?")
	}
	// BREACH_NOTIFICATION: the Information Security Breach and
	// Notification Act (eff. 2025-03-21) — not the matrix-meta note the
	// raw extraction filed. The 30-day timing is industry standard, so
	// the rule stays VERIFY.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "Breach") {
		t.Fatalf("section=%q, want the Breach and Notification Act", breach.Citation.Section)
	}
	if strings.Contains(breach.Citation.Note, "no item") {
		t.Fatalf("note=%q, extractor meta-commentary must not ship", breach.Citation.Note)
	}
	if breach.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("breach notification must stay VERIFY: the 30-day timing is industry standard, not statute")
	}
	// MONITORING_CONSENT: electronic-monitoring written notice with
	// acknowledgment and posting (Civil Rights Law § 201-i, eff.
	// 2022-05-07) — not the § 203-e reproductive-health citation the raw
	// extraction filed.
	if len(pack.MonitoringConsents) != 1 {
		t.Fatalf("monitoring consents=%d, want one", len(pack.MonitoringConsents))
	}
	monitoring := pack.MonitoringConsents[0]
	if !strings.Contains(monitoring.Citation.Section, "201-i") {
		t.Fatalf("section=%q, want Civil Rights Law 201-i", monitoring.Citation.Section)
	}
	if !hasString(monitoring.DataCategories, "email") {
		t.Fatalf("categories=%+v, want the monitored channels carried", monitoring.DataCategories)
	}
	// AUTOMATED_DECISION: NYC Local Law 144 bias audit with 10
	// business days' candidate notice.
	if len(pack.AutomatedDecisions) != 1 {
		t.Fatalf("automated decisions=%d, want the Local Law 144 rule", len(pack.AutomatedDecisions))
	}
	aedt := pack.AutomatedDecisions[0]
	if !aedt.BiasAuditRequired || aedt.CandidateNoticeDays != 10 {
		t.Fatalf("aedt=%+v, want the bias audit with 10-day candidate notice", aedt)
	}
	if !strings.Contains(aedt.Citation.Section, "144") {
		t.Fatalf("section=%q, want NYC Local Law 144", aedt.Citation.Section)
	}
	// No truncated extraction notes and no section placeholders survive.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ny.json"))
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

// TestTodo_LEGAL_ST_NY_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_NY_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ny.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "new-york.golden.txt"))
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

// TestTodo_LEGAL_ST_NY_001_Conformance checks the New York matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_NY_001_Conformance(t *testing.T) {
	pack := newYorkPack(t)
	// Table A Y cells: NOTICE, PAY_TRANSP, FIELD_RESTR, WAGE_FLOOR,
	// PAY_STMT, LEAVE, PAY_EQUITY, RETENTION. Table B Y cells:
	// MINI_WARN, ANTI_RETALIATION, BREACH_NOTIFICATION, plus the NYC
	// locality AUTOMATED_DECISION. The LOCAL Y cell arrives with
	// LEGAL-TOOL-009, not here.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.PayStatements) == 0 || len(pack.LeaveInteractions) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 || len(pack.AutomatedDecisions) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: non-competes and classifications are
	// common-law only.
	if len(pack.NonCompeteThresholds) != 0 || len(pack.Classifications) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent.
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_NY_001_Mutation seeds mutants into the New York draft
// and asserts the loader and the release digest notice. A surviving
// mutant means the guard is decorative.
func TestTodo_LEGAL_ST_NY_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "NY")
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
		baseline := sign(t, loadStateDraft(t, "NY"))
		mutant := loadStateDraft(t, "NY")
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
