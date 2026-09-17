package legal

// LEGAL-ST-NH-001 verification tests.
//
// The New Hampshire draft is agent-verified, not counsel-reviewed: the
// pack stays UNREVIEWED and unusable under any nonzero tenant review
// floor. These tests pin the verification half of the review — every
// GREEN parameter the research supports, the `?`-cell VERIFY markers
// carried rather than dropped, the corrected citations — and the
// guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newHampshirePack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "NH").Candidate()
	if err != nil {
		t.Fatalf("Candidate(NH): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_NH_001 verifies the New Hampshire draft against the
// reviewed research file and the section 5 matrix row, and proves the
// pack is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_NH_001(t *testing.T) {
	def := loadStateDraft(t, "NH")
	if def.PackID != "us-nh-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the New Hampshire draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "NH" {
		t.Fatalf("subdivision=%q, want NH", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := newHampshirePack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only floor marker, F ($7.25/hr, RSA 279:21).
	// New Hampshire sets no state minimum, so the marker carries no
	// amount, exactly like the Alabama template.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, New Hampshire states no minimum", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Section, "279:21") {
		t.Fatalf("section=%q, want RSA 279:21", pack.WageFloors[0].Citation.Section)
	}
	// NOTICE: written advance notice of any wage/salary/payday change
	// with signed employee acknowledgment (RSA 275:49).
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.Channel != "written" {
		t.Fatalf("channel=%q, want written", notice.Channel)
	}
	if !strings.Contains(notice.Citation.Section, "275:49") {
		t.Fatalf("section=%q, want RSA 275:49", notice.Citation.Section)
	}
	if !strings.Contains(notice.Citation.Note, "acknowledgment") {
		t.Fatalf("note=%q, want the signed-acknowledgment rule carried", notice.Citation.Note)
	}
	// RETENTION: personnel and payroll records for 15+-employee
	// employers, 1 year (RSA 275:56).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 1 {
		t.Fatalf("retention=%+v, want the 1-year rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "275:56") {
		t.Fatalf("section=%q, want RSA 275:56", pack.RetentionRules[0].Citation.Section)
	}
	// PAY_FREQUENCY is a `?` cell: weekly-or-biweekly default with
	// Commissioner-approved exceptions (RSA 275:43), carried at VERIFY.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency constraints=%d, want one", len(pack.PayFrequencyConstraints))
	}
	freq := pack.PayFrequencyConstraints[0]
	if freq.MinimumFrequency != "BIWEEKLY" {
		t.Fatalf("minimum_frequency=%q, want the biweekly default floor", freq.MinimumFrequency)
	}
	if !strings.Contains(freq.Citation.Section, "275:43") {
		t.Fatalf("section=%q, want RSA 275:43", freq.Citation.Section)
	}
	if freq.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// FINAL_PAY_DEADLINE: 72 hours on discharge (RSA 275:44).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "275:44") {
		t.Fatalf("section=%q, want RSA 275:44", finalPay.Citation.Section)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "72 hours") {
		t.Fatalf("deadline=%q, want the 72-hour discharge rule", finalPay.DeadlineDescription)
	}
	// NON_COMPETE: void at or below 200% of the federal minimum wage
	// (about $14.50/hr), physician/dentist restrictions (RSA 275:70-a,
	// RSA 329:31-a).
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange {
		t.Fatal("non-compete must recheck on pay change: promotion across the threshold changes enforceability")
	}
	if !strings.Contains(nc.Rule, "14.50") {
		t.Fatalf("rule=%q, want the 200-percent-of-minimum threshold carried", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "275:70-a") {
		t.Fatalf("section=%q, want RSA 275:70-a", nc.Citation.Section)
	}
	// MINI_WARN: mirrors federal WARN's 60-day/100+-employee rule rather
	// than adding a distinct state trigger (RSA 275-F).
	if len(pack.MiniWARNTriggers) != 1 {
		t.Fatalf("mini-warn triggers=%d, want one", len(pack.MiniWARNTriggers))
	}
	warn := pack.MiniWARNTriggers[0]
	if warn.EmployeeThreshold != 100 || warn.NoticeDays != 60 {
		t.Fatalf("trigger=%+v, want the mirrored 100-employee/60-day rule", warn)
	}
	if !strings.Contains(warn.Citation.Section, "275-F") {
		t.Fatalf("section=%q, want RSA 275-F", warn.Citation.Section)
	}
	// PAY_EQUITY_REVIEW: sex-based, with wage-discussion protection
	// (RSA 275:37, RSA 275:41-b) — not the lone "age" basis the raw
	// extraction filed.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	equity := pack.PayEquityReviews[0]
	if len(equity.ProtectedBases) != 1 || !hasString(equity.ProtectedBases, "sex") {
		t.Fatalf("bases=%+v, want the sex-based review", equity.ProtectedBases)
	}
	if !strings.Contains(equity.Citation.Section, "275:37") {
		t.Fatalf("section=%q, want RSA 275:37", equity.Citation.Section)
	}
	// PAY_STATEMENT is a `?` cell: no state-mandated itemized format, so
	// the federal-documentation duty is carried at VERIFY.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	if !strings.Contains(pack.PayStatements[0].Citation.Section, "275:56") {
		t.Fatalf("section=%q, want RSA 275:56", pack.PayStatements[0].Citation.Section)
	}
	if pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay statement must stay VERIFY: the matrix marks PAY_STMT ?")
	}
	// PERSONNEL_FILE: statutory inspection right with 1-year retention
	// for 15+-employee employers (RSA 275:56) — not the non-compete
	// disclosure note the raw extraction filed here.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	pf := pack.PersonnelFileRules[0]
	if !strings.Contains(pf.Citation.Section, "275:56") {
		t.Fatalf("section=%q, want RSA 275:56", pf.Citation.Section)
	}
	if !strings.Contains(pf.Citation.Note, "inspect") {
		t.Fatalf("note=%q, want the inspection right carried", pf.Citation.Note)
	}
	// ANTI_RETALIATION: wage discussion protection (RSA 275:41-b).
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !strings.Contains(ar.Citation.Section, "275:41-b") {
		t.Fatalf("section=%q, want RSA 275:41-b", ar.Citation.Section)
	}
	if !hasString(ar.ProtectedActivities, "wage_discussion") {
		t.Fatalf("activities=%+v, want wage_discussion protected", ar.ProtectedActivities)
	}
	// DRUG_TESTING is a `?` cell: no state law restricts private-sector
	// drug testing, so the gap stays visible at VERIFY with no stated
	// section, never an absence of duty.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the gap carried, not dropped", len(pack.DrugTestingRules))
	}
	if pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: no state statute states the rule")
	}
	if !strings.Contains(pack.DrugTestingRules[0].Citation.Section, "not stated") {
		t.Fatalf("section=%q, want the visible gap marker", pack.DrugTestingRules[0].Citation.Section)
	}
	if !hasString(pack.DrugTestingRules[0].PermittedBases, "pre_employment") {
		t.Fatalf("permitted=%+v, want pre-employment testing", pack.DrugTestingRules[0].PermittedBases)
	}
	// BREACH_NOTIFICATION: RSA 359-C:20 expedient notification.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Section, "359-C:20") {
		t.Fatalf("section=%q, want RSA 359-C:20", pack.BreachNotifications[0].Citation.Section)
	}
	// No truncated extraction notes survive, and the only gap left
	// visible is the drug-testing statute the research never states.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nh.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "...") {
		t.Error("pack still carries truncated extraction notes")
	}
	if got := strings.Count(body, "not stated"); got != 1 {
		t.Errorf("pack carries %d visible section gaps, want exactly the drug-testing one", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_NH_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_NH_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nh.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "new-hampshire.golden.txt"))
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

// TestTodo_LEGAL_ST_NH_001_Conformance checks the New Hampshire matrix
// row against the pack and proves every registered state draft still
// loads and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_NH_001_Conformance(t *testing.T) {
	pack := newHampshirePack(t)
	// Table A Y cells: NOTICE, NON_COMPETE, PAY_EQUITY, RETENTION,
	// PERSONNEL_FILE. Table B Y cells: FINAL_PAY, MINI_WARN,
	// ANTI_RETALIATION, DRUG_TESTING, BREACH_NOTIFICATION.
	if len(pack.Notices) == 0 || len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.MiniWARNTriggers) == 0 || len(pack.AntiRetaliationRules) == 0 || len(pack.DrugTestingRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions and the federal-only floor marker the PRIMARY test pins.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent, and no locality overlays where the
	// matrix marks LOCAL F.
	if len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a Table B F cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("New Hampshire carries locality overlays the matrix marks F")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}
