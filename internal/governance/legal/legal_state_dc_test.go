package legal

// LEGAL-ST-DC-001 verification tests.
//
// The District of Columbia draft is agent-verified, not counsel-reviewed: the
// pack stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review - every GREEN parameter
// the district-of-columbia.md research supports, the eight required kinds
// populated or visibly gap-marked, and the guardrail that keeps the pack out
// of evaluation until counsel approves it. They do not, and must not, mark
// the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func districtColumbiaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "DC").Candidate()
	if err != nil {
		t.Fatalf("Candidate(DC): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_DC_001 verifies the District draft against the
// district-of-columbia.md research file and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_DC_001(t *testing.T) {
	def := loadStateDraft(t, "DC")
	if def.PackID != "us-dc-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the District draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "DC" {
		t.Fatalf("subdivision=%q, want DC", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}

	pack := districtColumbiaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: $17.95/hr at the window start, $18.40 from 2026-07-01,
	// CPI-indexed (D.C. Code 32-1003).
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "17.95 USD" {
		t.Fatalf("floor=%q, want the $17.95/hr window-start floor", got)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Note, "18.40") {
		t.Fatalf("note=%q, want the July 2026 step carried", pack.WageFloors[0].Citation.Note)
	}
	if !strings.Contains(pack.WageFloors[0].Citation.Section, "32-1003") {
		t.Fatalf("section=%q, want D.C. Code 32-1003", pack.WageFloors[0].Citation.Section)
	}

	// NOTICE: bilingual written hire/change notice, re-triggered by every
	// promotion or pay change (WTPA 2014).
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written hire/change notice", pack.Notices)
	}
	if !hasString(pack.Notices[0].ContentFields, "pay_rate") {
		t.Fatalf("notice=%+v, want the pay-rate content carried", pack.Notices[0])
	}
	if !strings.Contains(pack.Notices[0].Citation.Note, "promotion") {
		t.Fatalf("note=%q, want the promotion re-trigger carried", pack.Notices[0].Citation.Note)
	}

	// PAY_TRANSPARENCY: good-faith min/max range in all postings plus the
	// salary-history ban (2023 Omnibus, eff. 2024-06-30).
	if len(pack.PayTransparencyDuties) != 1 || pack.PayTransparencyDuties[0].Trigger != "internal_promotion" {
		t.Fatalf("pay transparency=%+v, want the promotion range disclosure", pack.PayTransparencyDuties)
	}
	if !strings.Contains(pack.PayTransparencyDuties[0].RequiredDisclosure, "minimum and maximum") {
		t.Fatalf("disclosure=%q, want the min/max range carried", pack.PayTransparencyDuties[0].RequiredDisclosure)
	}
	if !strings.Contains(pack.PayTransparencyDuties[0].Citation.Section, "2023") {
		t.Fatalf("section=%q, want the 2023 Omnibus Act", pack.PayTransparencyDuties[0].Citation.Section)
	}

	// LEAVE_INTERACTION: employer-funded Universal Paid Leave (12 weeks
	// parental/family/medical plus 2 prenatal, 0.75% employer-only tax) and
	// the size-tiered sick-leave trio (1/37, 1/43, 1/87).
	if len(pack.LeaveInteractions) != 2 {
		t.Fatalf("leave interactions=%d, want UPL and sick leave", len(pack.LeaveInteractions))
	}
	var uplLeave, sickLeave *LeaveInteraction
	for i := range pack.LeaveInteractions {
		li := &pack.LeaveInteractions[i]
		switch {
		case strings.Contains(li.Citation.Section, "Universal Paid Leave"):
			uplLeave = li
		case strings.Contains(li.Citation.Section, "Sick and Safe Leave"):
			sickLeave = li
		}
	}
	if uplLeave == nil {
		t.Fatal("no UPL interaction citing the Universal Paid Leave Act")
	}
	for _, want := range []string{"12 weeks", "0.75%"} {
		if !strings.Contains(uplLeave.InteractionRule, want) {
			t.Fatalf("upl leave=%q, want %q carried", uplLeave.InteractionRule, want)
		}
	}
	if sickLeave == nil {
		t.Fatal("no sick-leave interaction citing the Sick and Safe Leave Act")
	}
	for _, want := range []string{"37", "43", "87"} {
		if !strings.Contains(sickLeave.InteractionRule, want) {
			t.Fatalf("sick leave=%q, want tier %q carried", sickLeave.InteractionRule, want)
		}
	}

	// NON_COMPETE: near-total ban with the $150k/$250k highly compensated
	// carve-outs and 365/730-day caps, eff. 2022-10-01.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	rule := pack.NonCompeteThresholds[0].Rule
	for _, want := range []string{"150,000", "250,000", "2022-10-01", "void"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("rule=%q, want %q carried", rule, want)
		}
	}

	// FINAL_PAY_DEADLINE: discharge by the next working day, resignation by
	// the next payday or within 7 days, whichever earlier (32-1303).
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	for _, want := range []string{"working day", "7 days"} {
		if !strings.Contains(finalPay.DeadlineDescription, want) {
			t.Fatalf("deadline=%q, want %q carried", finalPay.DeadlineDescription, want)
		}
	}
	if !strings.Contains(finalPay.Citation.Section, "32-1303") {
		t.Fatalf("section=%q, want D.C. Code 32-1303", finalPay.Citation.Section)
	}

	// RETENTION: 3-year payroll records (32-1008).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year rule", pack.RetentionRules)
	}
	if !strings.Contains(pack.RetentionRules[0].Citation.Section, "32-1008") {
		t.Fatalf("section=%q, want D.C. Code 32-1008", pack.RetentionRules[0].Citation.Section)
	}

	// PAY_STATEMENT: itemized stub each payday (32-1008(b)).
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	statement := pack.PayStatements[0]
	if !strings.Contains(statement.Citation.Section, "32-1008(b)") {
		t.Fatalf("section=%q, want D.C. Code 32-1008(b)", statement.Citation.Section)
	}
	for _, field := range []string{"gross_wages", "net_wages", "deductions"} {
		if !hasString(statement.RequiredFields, field) {
			t.Fatalf("fields=%+v, want the itemized statement fields", statement.RequiredFields)
		}
	}

	// BREACH_NOTIFICATION: most-expedient/without-unreasonable-delay
	// standard with AG notice at 50+ residents (28-3852). No fixed day
	// count exists, so the rule stays VERIFY.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	breach := pack.BreachNotifications[0]
	if !strings.Contains(breach.Citation.Section, "28-3852") {
		t.Fatalf("section=%q, want D.C. Code 28-3852", breach.Citation.Section)
	}
	if breach.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("breach notification must stay VERIFY: no fixed statutory day count exists")
	}

	// PAY_EQUITY_REVIEW is an explicit gap emission: no standalone
	// equal-pay statute confirmed, carried at VERIFY so the gap stays
	// visible rather than reading as an absence of duty.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want the visible gap emission", len(pack.PayEquityReviews))
	}
	if pack.PayEquityReviews[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay equity must stay VERIFY: DCHRA compensation coverage is unconfirmed")
	}

	// PERSONNEL_FILE is an explicit gap emission: no private-sector access
	// statute (1-631.05 is District-government-only), carried at VERIFY and
	// RECOMMENDED while granting no access window.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want the gap emission, not a dropped kind", len(pack.PersonnelFileRules))
	}
	personnelFile := pack.PersonnelFileRules[0]
	if personnelFile.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("personnel file must stay VERIFY: no private-sector right exists to confirm")
	}
	if personnelFile.ResponseDays != 0 {
		t.Fatalf("personnel file=%+v, want no access window granted", personnelFile)
	}
	if !strings.Contains(personnelFile.Citation.Section, "1-631.05") {
		t.Fatalf("section=%q, want the District-government-only citation", personnelFile.Citation.Section)
	}

	// No truncated extraction notes survive.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-dc.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	if strings.Contains(string(raw), "...") {
		t.Error("pack still carries truncated extraction notes")
	}

	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_DC_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_DC_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-dc.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "district-of-columbia.golden.txt"))
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

// TestTodo_LEGAL_ST_DC_001_Conformance checks the District draft carries
// every required kind and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_DC_001_Conformance(t *testing.T) {
	pack := districtColumbiaPack(t)
	if len(pack.Notices) == 0 || len(pack.WageFloors) == 0 ||
		len(pack.PayTransparencyDuties) == 0 || len(pack.LeaveInteractions) == 0 ||
		len(pack.NonCompeteThresholds) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.BreachNotifications) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.PersonnelFileRules) == 0 {
		t.Fatal("a required District kind has no pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}
