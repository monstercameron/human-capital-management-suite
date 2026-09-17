package legal

// LEGAL-ST-OH-001 verification tests.
//
// The Ohio draft is agent-verified, not counsel-reviewed: the pack stays
// UNREVIEWED and unusable under any nonzero tenant review floor. These
// tests pin the verification half of the review — every GREEN parameter
// the research supports, the `?`-cell VERIFY marker carried rather than
// dropped, the corrected WARN citations, the locality deference — and
// the guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ohioPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "OH").Candidate()
	if err != nil {
		t.Fatalf("Candidate(OH): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_OH_001 verifies the Ohio draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is
// still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_OH_001(t *testing.T) {
	def := loadStateDraft(t, "OH")
	if def.PackID != "us-oh-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Ohio draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "OH" {
		t.Fatalf("subdivision=%q, want OH", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := ohioPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $11.00/hr (2026) for employers grossing over $405,000
	// with annual CPI-W indexation, $7.25/hr otherwise (Ohio Const.
	// Art. II § 34a) — not the "tipped employees" worker class the raw
	// extraction filed on the $11.00 tier.
	if len(pack.WageFloors) != 2 {
		t.Fatalf("wage floors=%d, want the large- and small-employer tiers", len(pack.WageFloors))
	}
	tiers := map[string]WageFloorRule{}
	for _, f := range pack.WageFloors {
		tiers[f.FloorAmount.String()] = f
		if !strings.Contains(f.Citation.Section, "34a") {
			t.Fatalf("section=%q, want Ohio Const. Art. II 34a", f.Citation.Section)
		}
		if f.Basis != "HOURLY" {
			t.Fatalf("basis=%q, want HOURLY", f.Basis)
		}
	}
	large, ok := tiers["11.00 USD"]
	if !ok {
		t.Fatalf("tiers=%v, want the $11.00 large-employer tier carried", tiers)
	}
	if !strings.Contains(large.WorkerClass, "405,000") {
		t.Fatalf("worker_class=%q, want the gross-receipts threshold carried", large.WorkerClass)
	}
	if large.Indexation != "CPI" {
		t.Fatalf("indexation=%q, want the annual CPI-W indexation", large.Indexation)
	}
	if _, ok := tiers["7.25 USD"]; !ok {
		t.Fatalf("tiers=%v, want the $7.25 small-employer tier carried", tiers)
	}
	// RETENTION: promotion documentation for at least 3 years with
	// director inspection rights (RC 4111.08).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "4111.08") {
		t.Fatalf("section=%q, want RC 4111.08", pack.RetentionRules[0].Citation.Section)
	}
	// LEAVE_INTERACTION: earned sick/safe time for all private employers
	// at 1hr/30hrs, eff. 2025 (Issue 1).
	if len(pack.LeaveInteractions) != 1 {
		t.Fatalf("leave interactions=%d, want one", len(pack.LeaveInteractions))
	}
	leave := pack.LeaveInteractions[0]
	if !strings.Contains(leave.Citation.Section, "Issue 1") {
		t.Fatalf("section=%q, want Issue 1 (2025)", leave.Citation.Section)
	}
	if !strings.Contains(leave.InteractionRule, "30 hours") {
		t.Fatalf("rule=%q, want the 1-hour-per-30-hours accrual carried", leave.InteractionRule)
	}
	// PAY_FREQUENCY: mandatory semimonthly schedule with advance notice
	// of payday changes (RC 4113.15).
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("minimum_frequency=%q, want the semimonthly floor", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "4113.15") {
		t.Fatalf("section=%q, want RC 4113.15", freq.Citation.Section)
	}
	// FINAL_PAY_DEADLINE: discharge or layoff on the next regular payday
	// or within 15 days, whichever sooner; resignation on the next
	// regular payday (RC 4113.15).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "4113.15") {
		t.Fatalf("section=%q, want RC 4113.15", finalPay.Citation.Section)
	}
	for _, want := range []string{"15 days", "next regular payday"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Fatalf("deadline=%q, want %q carried", finalPay.DeadlineDescription, want)
		}
	}
	// MINI_WARN: 100+-employee/50+-affected-site 60-day notice, eff.
	// 2025-09-29 (RC 4113.31) — the exact date LEGAL-016's Ohio boundary
	// vector straddles, and the corrected citation for the New Mexico
	// section numbers the raw extraction carried.
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.EmployeeThreshold != 100 || warn.NoticeDays != 60 {
		t.Fatalf("trigger=%+v, want the 100-employee/60-day rule", warn)
	}
	if !strings.Contains(warn.Citation.Section, "4113.31") {
		t.Fatalf("section=%q, want RC 4113.31", warn.Citation.Section)
	}
	if !strings.Contains(warn.Note, "September 29, 2025") {
		t.Fatalf("note=%q, want the effective date carried", warn.Note)
	}
	// PAY_EQUITY_REVIEW: equal work at equal skill/effort/
	// responsibility across race, color, religion, sex, age, national
	// origin and ancestry, all employers (RC 4111.17).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	for _, base := range []string{"race", "sex", "ancestry"} {
		if !hasString(equity.ProtectedBases, base) {
			t.Fatalf("bases=%+v, want %q covered", equity.ProtectedBases, base)
		}
	}
	if len(equity.ProtectedBases) != 7 {
		t.Fatalf("bases=%+v, want exactly the seven statutory classes", equity.ProtectedBases)
	}
	if equity.ComparatorStandard != "equal work" {
		t.Fatalf("comparator=%q, want equal work", equity.ComparatorStandard)
	}
	if !strings.Contains(equity.Citation.Section, "4111.17") {
		t.Fatalf("section=%q, want RC 4111.17", equity.Citation.Section)
	}
	// PAY_STATEMENT: itemized earnings-and-deductions stub each payday,
	// eff. 2025-04-09 (RC 4113.14, HB 106) — not the single "pay_rate"
	// field the raw extraction filed.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	statement := pack.PayStatements[0]
	if !strings.Contains(statement.Citation.Section, "4113.14") {
		t.Fatalf("section=%q, want RC 4113.14", statement.Citation.Section)
	}
	for _, field := range []string{"gross_wages", "net_wages", "deductions"} {
		if !hasString(statement.RequiredFields, field) {
			t.Fatalf("fields=%+v, want the itemized statement fields", statement.RequiredFields)
		}
	}
	// ANTI_RETALIATION: whistleblower protection for all employers
	// (RC 4113.52) with workers'-comp retaliation barred (RC 4123.90) —
	// not the federal PWFA citation the raw extraction filed.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !strings.Contains(ar.Citation.Section, "4113.52") {
		t.Fatalf("section=%q, want RC 4113.52", ar.Citation.Section)
	}
	if !hasString(ar.ProtectedActivities, "safety_violation_report") {
		t.Fatalf("activities=%+v, want the safety-violation report protected", ar.ProtectedActivities)
	}
	if ar.Disposition != "FLAG" {
		t.Fatalf("disposition=%q, want FLAG", ar.Disposition)
	}
	// DRUG_TESTING is a `?` cell: no restricting statute, with medical
	// marijuana under R.C. ch. 3796, carried at VERIFY.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	if !strings.Contains(dt.Citation.Section, "3796") {
		t.Fatalf("section=%q, want R.C. ch. 3796", dt.Citation.Section)
	}
	if dt.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks DRUG_TEST ?")
	}
	if !hasString(dt.PermittedBases, "pre_employment") {
		t.Fatalf("permitted=%+v, want pre-employment testing", dt.PermittedBases)
	}
	// BREACH_NOTIFICATION: 45-day notification rule (RC 1349.19).
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "1349.19") {
		t.Fatalf("section=%q, want RC 1349.19", breach.Citation.Section)
	}
	if breach.SubjectDeadlineDays != 45 {
		t.Fatalf("deadline=%d, want the 45-day rule", breach.SubjectDeadlineDays)
	}
	// Locality deference: salary-history and pay-range duties exist only
	// in Columbus/Cincinnati/Toledo ordinances, never statewide, so no
	// statewide transparency or field-restriction duty may ship; the
	// locality overlays themselves arrive with LEGAL-TOOL-009, not here.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 {
		t.Fatalf("transparency=%+v fields=%+v, want no statewide duties: the ordinances are locality-scoped",
			pack.PayTransparencyDuties, pack.FieldRestrictions)
	}
	// No truncated extraction notes and no section placeholders survive.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-oh.json"))
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

// TestTodo_LEGAL_ST_OH_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_OH_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-oh.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "ohio.golden.txt"))
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

// TestTodo_LEGAL_ST_OH_001_Conformance checks the Ohio matrix row against
// the pack and proves every registered state draft still loads and
// validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_OH_001_Conformance(t *testing.T) {
	pack := ohioPack(t)
	// Table A Y cells: WAGE_FLOOR, PAY_FREQ, PAY_STMT, LEAVE,
	// PAY_EQUITY, RETENTION. Table B Y cells: FINAL_PAY, MINI_WARN,
	// ANTI_RETALIATION, DRUG_TESTING, BREACH_NOTIFICATION. The LOCAL Y
	// cell and the construction-scoped E-Verify rule arrive with other
	// lanes, not here.
	if len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.DrugTestingRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F and L cells stay free of statewide duties: no notice
	// duty, no personnel-file duty, common-law-only non-competes and
	// classifications, and locality-scoped transparency/field rules
	// never modeled as statewide.
	if len(pack.Notices) != 0 || len(pack.PersonnelFileRules) != 0 || len(pack.NonCompeteThresholds) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 {
		t.Fatal("a matrix F/L cell gained a statewide pack obligation")
	}
	// Table B F cells stay absent.
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_OH_001_Mutation seeds mutants into the Ohio draft and
// asserts the loader and the release digest notice. A surviving mutant
// means the guard is decorative.
func TestTodo_LEGAL_ST_OH_001_Mutation(t *testing.T) {
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
		{"retention duration inverted", func(d *PackDefinition) {
			for i := range d.Obligations {
				if d.Obligations[i].Kind == "RETENTION" {
					d.Obligations[i].Body.DurationYears = -1
				}
			}
		}},
		{"inverted effective window", func(d *PackDefinition) { d.Window.End = "2025-01-01" }},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			def := loadStateDraft(t, "OH")
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
		baseline := sign(t, loadStateDraft(t, "OH"))
		mutant := loadStateDraft(t, "OH")
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
