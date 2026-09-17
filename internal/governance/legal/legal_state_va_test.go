package legal

// LEGAL-ST-VA-001 verification tests.
//
// The Virginia draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the `?`-cell VERIFY markers carried
// rather than dropped, the L-scope leave rule correctly absent from the
// subdivision pack — and the guardrail that keeps the pack out of
// evaluation until counsel approves it. They do not, and must not,
// mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func virginiaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "VA").Candidate()
	if err != nil {
		t.Fatalf("Candidate(VA): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_VA_001 verifies the Virginia draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_VA_001(t *testing.T) {
	def := loadStateDraft(t, "VA")
	if def.PackID != "us-va-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Virginia draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "VA" {
		t.Fatalf("subdivision=%q, want VA", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := virginiaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $12.77/hr (2026-2027) on a statutory schedule rising
	// through $13.75 (2027-2028) to $15.00 (2028-2029), then CPI-indexed
	// (§ 40.1-28.10). No tip credit: tipped workers get the full floor.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "12.77 USD" {
		t.Fatalf("floor=%q, want the $12.77/hr floor", got)
	}
	if floor.Basis != "HOURLY" || floor.Indexation != "SCHEDULE" {
		t.Fatalf("floor=%+v, want HOURLY on a SCHEDULE indexation", floor)
	}
	if !strings.Contains(floor.Citation.Section, "40.1-28.10") {
		t.Fatalf("section=%q, want § 40.1-28.10", floor.Citation.Section)
	}
	for _, want := range []string{"$13.75", "$15.00", "No tip credit"} {
		if !strings.Contains(floor.Citation.Note, want) {
			t.Errorf("note=%q, want %q carried", floor.Citation.Note, want)
		}
	}
	if floor.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatalf("marker=%s, the stepped floor is confirmed research", floor.Citation.ConfidenceMarker)
	}
	// PAY_TRANSPARENCY: wage/salary-range disclosure in all public and
	// internal postings, in good faith, eff. 2026-09-03 (§ 40.1-28.7:12).
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	pt := pack.PayTransparencyDuties[0]
	if pt.Trigger != "internal_promotion" {
		t.Fatalf("trigger=%q, want internal_promotion", pt.Trigger)
	}
	for _, want := range []string{"2026-09-03", "good faith"} {
		if !strings.Contains(pt.RequiredDisclosure, want) {
			t.Errorf("disclosure=%q, want %q carried", pt.RequiredDisclosure, want)
		}
	}
	if !strings.Contains(pt.Citation.Section, "40.1-28.7:12") {
		t.Fatalf("section=%q, want § 40.1-28.7:12", pt.Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban, eff. 2026-09-03
	// (§ 40.1-28.7:12). Volunteered history may only justify a higher
	// offer, never set the offer.
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	fr := pack.FieldRestrictions[0]
	if !strings.Contains(fr.Citation.Section, "40.1-28.7:12") {
		t.Fatalf("section=%q, want § 40.1-28.7:12", fr.Citation.Section)
	}
	if !strings.Contains(fr.Citation.Note, "2026-09-03") {
		t.Fatalf("note=%q, want the 2026-09-03 effective date carried", fr.Citation.Note)
	}
	// NOTICE: written notice of pay decreases 1+ pay periods in advance
	// (§ 40.1-29). Increases take effect immediately, so the duty is
	// decreases-only.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDirection != "BEFORE" || notice.Channel != "written" {
		t.Fatalf("notice=%+v, want written advance notice", notice)
	}
	if !hasString(notice.ContentFields, "pay_rate") {
		t.Fatalf("content=%v, want pay_rate", notice.ContentFields)
	}
	if !strings.Contains(notice.Citation.Section, "40.1-29") {
		t.Fatalf("section=%q, want § 40.1-29", notice.Citation.Section)
	}
	if !strings.Contains(notice.Citation.Note, "decrease") {
		t.Fatalf("note=%q, want the decreases-only rule carried", notice.Citation.Note)
	}
	// PAY_FREQUENCY: hourly workers biweekly or semimonthly, salaried
	// workers monthly (§ 40.1-29). One constraint per class, not a
	// single floor that over-requires salaried workers.
	if len(pack.PayFrequencyConstraints) != 2 {
		t.Fatalf("pay frequency constraints=%d, want the hourly and salaried limbs", len(pack.PayFrequencyConstraints))
	}
	seenFreq := map[string]string{}
	for _, c := range pack.PayFrequencyConstraints {
		if !strings.Contains(c.Citation.Section, "40.1-29") {
			t.Fatalf("section=%q, want § 40.1-29", c.Citation.Section)
		}
		seenFreq[c.AppliesToWorkerClass] = c.MinimumFrequency
	}
	if seenFreq["hourly employees"] != "BIWEEKLY" {
		t.Fatalf("hourly=%q, want the BIWEEKLY floor", seenFreq["hourly employees"])
	}
	if seenFreq["salaried employees"] != "MONTHLY" {
		t.Fatalf("salaried=%q, want the MONTHLY floor", seenFreq["salaried employees"])
	}
	// FINAL_PAY_DEADLINE: all earned wages on or before the next regular
	// payday following separation, with treble-damages exposure for
	// knowing violations (§§ 40.1-29, 40.1-29.2).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	for _, want := range []string{"next regular payday", "separation"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Errorf("deadline=%q, want %q", finalPay.DeadlineDescription, want)
		}
	}
	if !strings.Contains(finalPay.Citation.Section, "40.1-29") {
		t.Fatalf("section=%q, want § 40.1-29", finalPay.Citation.Section)
	}
	// NON_COMPETE: void for low-wage employees, FLSA-overtime-eligible
	// employees, and all healthcare professionals (SB 1218, eff.
	// 2025-07-01, § 40.1-28.7:8).
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	rule := pack.NonCompeteThresholds[0].Rule
	for _, want := range []string{"health", "FLSA", "2025-07-01"} {
		if !strings.Contains(rule, want) {
			t.Errorf("rule=%q, want %q carried", rule, want)
		}
	}
	if !strings.Contains(pack.NonCompeteThresholds[0].Citation.Section, "40.1-28.7:8") {
		t.Fatalf("section=%q, want § 40.1-28.7:8", pack.NonCompeteThresholds[0].Citation.Section)
	}
	// LEAVE_INTERACTION: the matrix marks LEAVE L — the only statutory
	// paid sick leave covers home health workers (§ 40.1-33.3 et seq.),
	// a limited scope the subdivision pack must not generalize into a
	// workforce-wide duty.
	if len(pack.LeaveInteractions) != 0 {
		t.Fatalf("leave interactions=%d, the L-scope rule belongs to no subdivision pack", len(pack.LeaveInteractions))
	}
	// PAY_EQUITY_REVIEW: equal-work comparator (§ 40.1-28.6).
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if equity.ComparatorStandard != "equal work" {
		t.Fatalf("comparator=%q, want equal work", equity.ComparatorStandard)
	}
	if !strings.Contains(equity.Citation.Section, "40.1-28.6") {
		t.Fatalf("section=%q, want § 40.1-28.6", equity.Citation.Section)
	}
	// PAY_STATEMENT (? with evidence) is carried once at VERIFY with
	// the § 40.1-29 content fields.
	if len(pack.PayStatements) != 1 || pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("pay statements=%+v, want the VERIFY ?-emission", pack.PayStatements)
	}
	if !hasString(pack.PayStatements[0].RequiredFields, "pay_rate") {
		t.Fatalf("pay statement=%+v, want the rate field carried", pack.PayStatements[0])
	}
	// CLASSIFICATION: common-law/IRS control test, no ABC statutory
	// test (§ 40.1-28.7:7).
	if len(pack.Classifications) != 1 {
		t.Fatalf("classifications=%d, want one", len(pack.Classifications))
	}
	classification := pack.Classifications[0]
	if classification.Dimension != "CONTRACTOR" {
		t.Fatalf("dimension=%q, want CONTRACTOR", classification.Dimension)
	}
	if !strings.Contains(classification.TestDescription, "control") {
		t.Fatalf("test=%q, want the control test carried", classification.TestDescription)
	}
	// RETENTION: 3-year payroll records (§ 40.1-29).
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	retention := pack.RetentionRules[0]
	if retention.RecordClass != "payroll_records" || retention.DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year payroll rule", retention)
	}
	if !strings.Contains(retention.Citation.Section, "40.1-29") {
		t.Fatalf("section=%q, want § 40.1-29", retention.Citation.Section)
	}
	// PERSONNEL_FILE (? with evidence): the § 8.01-413.1 records right
	// — 30 days on written request, reasonable copying fees — carried
	// once at VERIFY. It is narrower than a full personnel-file law.
	if len(pack.PersonnelFileRules) != 1 || pack.PersonnelFileRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("personnel file rules=%+v, want the VERIFY ?-emission", pack.PersonnelFileRules)
	}
	personnel := pack.PersonnelFileRules[0]
	if personnel.ResponseDays != 30 || personnel.DayBasis != "CALENDAR" {
		t.Fatalf("personnel=%+v, want the 30-calendar-day response window", personnel)
	}
	if !personnel.CopyFeePermitted {
		t.Fatalf("personnel=%+v, want the copying-fee permission carried", personnel)
	}
	if !strings.Contains(personnel.Citation.Section, "8.01-413.1") {
		t.Fatalf("section=%q, want § 8.01-413.1", personnel.Citation.Section)
	}
	// ANTI_RETALIATION: wage-discussion protection (§ 40.1-28.7:9).
	if len(pack.AntiRetaliationRules) != 1 || pack.AntiRetaliationRules[0].Disposition != "BLOCK" {
		t.Fatalf("anti-retaliation=%+v, want the wage-discussion block", pack.AntiRetaliationRules)
	}
	if !strings.Contains(pack.AntiRetaliationRules[0].Citation.Section, "40.1-28.7:9") {
		t.Fatalf("section=%q, want § 40.1-28.7:9", pack.AntiRetaliationRules[0].Citation.Section)
	}
	// DRUG_TESTING (? with evidence): no state-level regulation of
	// private-sector testing — carried once at VERIFY as a
	// recommendation, never a mandate.
	if len(pack.DrugTestingRules) != 1 || pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatalf("drug testing=%+v, want the VERIFY ?-emission", pack.DrugTestingRules)
	}
	drug := pack.DrugTestingRules[0]
	if drug.Standard.String() != "RECOMMENDED" {
		t.Fatalf("drug testing=%+v, want RECOMMENDED, not a mandate", drug)
	}
	if !strings.Contains(drug.Citation.Note, "No state-level regulation") {
		t.Fatalf("note=%q, want the no-regulation finding carried", drug.Citation.Note)
	}
	// BREACH_NOTIFICATION: § 18.2-186.6 states no day-count deadline
	// ("without unreasonable delay"), so none is carried.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "18.2-186.6") {
		t.Fatalf("section=%q, want § 18.2-186.6", breach.Citation.Section)
	}
	if breach.SubjectDeadlineDays != 0 {
		t.Fatalf("breach deadline=%d, the research states no day count", breach.SubjectDeadlineDays)
	}
	// F cells stay absent: mini-warn (federal WARN only), e-verify,
	// job security, automated decisions, locality overlays. The
	// SEP_FILING ? cell is dropped: the research evidences no
	// separation-filing rule.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F (or dropped ?) cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("Virginia carries locality overlays the matrix marks F")
	}
	// No truncated extraction notes survive, and every citation is
	// sourced: no visible section gaps remain.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-va.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 0 {
		t.Errorf("pack carries %d visible section gaps, want none", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_VA_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_VA_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-va.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "virginia.golden.txt"))
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

// TestTodo_LEGAL_ST_VA_001_Conformance checks the Virginia matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_VA_001_Conformance(t *testing.T) {
	pack := virginiaPack(t)
	// Table A Y cells: NOTICE, PAY_TRANSPARENCY, FIELD_RESTRICTION,
	// WAGE_FLOOR, PAY_FREQUENCY, NON_COMPETE, CLASSIFICATION,
	// PAY_EQUITY, RETENTION. Table A ? cells with evidence:
	// PAY_STATEMENT, PERSONNEL_FILE. Table A L cell: LEAVE (limited
	// scope, never a subdivision obligation). Table B Y cells:
	// FINAL_PAY, ANTI_RETALIATION, BREACH_NOTIFICATION. Table B ?
	// cell with evidence: DRUG_TESTING.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.Classifications) == 0 || len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.PayStatements) == 0 || len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 || len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix Y (or evidenced ?) cell has no pack obligation")
	}
	// Table A L cell stays obligation-free at subdivision level; Table
	// B F cells and the dropped SEP_FILING ? cell stay absent.
	if len(pack.LeaveInteractions) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix L, F (or dropped ?) cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_VA_001_Mutation seeds mutants into the Virginia
// draft's guards and asserts the loader notices. A surviving mutant
// means the guard is decorative.
func TestTodo_LEGAL_ST_VA_001_Mutation(t *testing.T) {
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
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			def := loadStateDraft(t, "VA")
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
}
