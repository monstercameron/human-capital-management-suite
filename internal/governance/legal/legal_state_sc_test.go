package legal

// LEGAL-ST-SC-001 verification tests.
//
// The South Carolina draft is agent-verified, not counsel-reviewed: the
// pack stays UNREVIEWED and unusable under any nonzero tenant review
// floor. These tests pin the verification half of the review — every
// GREEN parameter the research supports, the LEGAL-018 corrections held
// (no invented § 41-12 equal-pay chapter, no invented § 41-7 non-compete
// exception, no NCDOL reference), the matrix row matched — and the
// guardrail that keeps the pack out of evaluation until counsel approves
// it. They do not, and must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func southCarolinaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "SC").Candidate()
	if err != nil {
		t.Fatalf("Candidate(SC): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_SC_001 verifies the South Carolina draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_SC_001(t *testing.T) {
	def := loadStateDraft(t, "SC")
	if def.PackID != "us-sc-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the South Carolina draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "SC" {
		t.Fatalf("subdivision=%q, want SC", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := southCarolinaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only ($7.25/hr, no state override), so the pack
	// carries no state floor.
	if len(pack.WageFloors) != 0 {
		t.Fatalf("wage floors=%+v, want no state floor (federal-only)", pack.WageFloors)
	}
	// The LEGAL-018 corrections hold: no invented equal-pay chapter and
	// no non-compete statute — common-law non-competes stay untyped, so
	// the pack carries neither a pay-equity review nor a non-compete.
	if len(pack.PayEquityReviews) != 0 {
		t.Fatalf("pay equity=%+v, the invented § 41-12 chapter is back", pack.PayEquityReviews)
	}
	if len(pack.NonCompeteThresholds) != 0 {
		t.Fatalf("non-competes=%+v, the invented § 41-7 exception is back", pack.NonCompeteThresholds)
	}
	// NOTICE: written notice at hire of wages/hours/paydays/deductions
	// with 7-calendar-day advance notice for any change, § 41-10-30 —
	// the pack's one hard-figured NOTICE obligation.
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want the § 41-10-30 rule", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDirection != "BEFORE" || notice.Channel != "written" {
		t.Fatalf("notice=%+v, want advance written notice", notice)
	}
	if !hasString(notice.ContentFields, "effective_date") {
		t.Fatalf("notice=%+v, want the effective-date content carried", notice)
	}
	if !strings.Contains(notice.Citation.Section, "41-10-30") {
		t.Fatalf("section=%q, want § 41-10-30", notice.Citation.Section)
	}
	if !strings.Contains(notice.Citation.Note, "seven calendar days") {
		t.Fatalf("note=%q, want the 7-day advance rule", notice.Citation.Note)
	}
	// FINAL_PAY_DEADLINE: earlier of 48 hours or next regular payday,
	// capped at 30 days, § 41-10-50.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	final := pack.FinalPayDeadlines[0]
	if !strings.Contains(final.DeadlineDescription, "48 hours") {
		t.Fatalf("deadline=%q, want the 48-hour rule", final.DeadlineDescription)
	}
	if !strings.Contains(final.Citation.Section, "41-10-50") {
		t.Fatalf("section=%q, want § 41-10-50", final.Citation.Section)
	}
	// RETENTION: true and accurate wage/hour records, § 41-10-30(C).
	if len(pack.RetentionRules) != 1 {
		t.Fatalf("retention rules=%d, want one", len(pack.RetentionRules))
	}
	ret := pack.RetentionRules[0]
	if ret.RecordClass != "wage_records" {
		t.Fatalf("retention=%+v, want wage records", ret)
	}
	if !strings.Contains(ret.Citation.Section, "41-10-30") {
		t.Fatalf("section=%q, want § 41-10-30(C)", ret.Citation.Section)
	}
	// JOB_SECURITY: the HANDBOOK_DISCLAIMER standard — an
	// underlined-capital-letters, signed disclaimer defeats an
	// implied-contract claim, § 41-1-110.
	if len(pack.JobSecurityRules) != 1 {
		t.Fatalf("job security=%d, want the disclaimer standard", len(pack.JobSecurityRules))
	}
	js := pack.JobSecurityRules[0]
	if js.StandardKind != "HANDBOOK_DISCLAIMER" {
		t.Fatalf("standard=%q, want HANDBOOK_DISCLAIMER", js.StandardKind)
	}
	if !strings.Contains(js.Citation.Section, "41-1-110") {
		t.Fatalf("section=%q, want § 41-1-110", js.Citation.Section)
	}
	// E_VERIFY: mandatory for all private employers within 3 business
	// days, § 41-8-20(B).
	if len(pack.EVerifyChecks) != 1 || !pack.EVerifyChecks[0].RequiredOnNewHireOnly {
		t.Fatalf("e-verify=%+v, want the hiring-time check", pack.EVerifyChecks)
	}
	if !strings.Contains(pack.EVerifyChecks[0].Citation.Section, "41-8-20") {
		t.Fatalf("section=%q, want § 41-8-20(B)", pack.EVerifyChecks[0].Citation.Section)
	}
	// PAY_FREQUENCY (? at VERIFY, §§ 41-10-40/41-10-30), PAY_STATEMENT,
	// DRUG_TESTING (? at VERIFY, § 41-1-15), ANTI_RETALIATION
	// (workers-comp, § 41-1-80) and BREACH_NOTIFICATION (§ 39-1-90) each
	// appear once.
	if len(pack.PayFrequencyConstraints) != 1 {
		t.Fatalf("pay frequency=%+v, want the ?-cell emission", pack.PayFrequencyConstraints)
	}
	if !strings.Contains(pack.PayFrequencyConstraints[0].Citation.Section, "41-10-40") {
		t.Fatalf("section=%q, want § 41-10-40", pack.PayFrequencyConstraints[0].Citation.Section)
	}
	if pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks it ?")
	}
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want the ?-cell emission", len(pack.DrugTestingRules))
	}
	if !strings.Contains(pack.DrugTestingRules[0].Citation.Section, "41-1-15") {
		t.Fatalf("section=%q, want § 41-1-15", pack.DrugTestingRules[0].Citation.Section)
	}
	if pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks it ?")
	}
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "workers_compensation_claim") {
		t.Fatalf("anti-retaliation=%+v, want the § 41-1-80 rule", pack.AntiRetaliationRules)
	}
	if pack.AntiRetaliationRules[0].Disposition != "FLAG" {
		t.Fatalf("disposition=%q, want FLAG", pack.AntiRetaliationRules[0].Disposition)
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Section, "39-1-90") {
		t.Fatalf("section=%q, want § 39-1-90", pack.BreachNotifications[0].Citation.Section)
	}
	// All other F cells stay absent: transparency, field restrictions,
	// leave, classifications, personnel files, mini-warn, separation
	// filings, automated decisions, monitoring consents.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 || len(pack.MiniWARNTriggers) != 0 ||
		len(pack.SeparationFilings) != 0 || len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_SC_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_SC_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-sc.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "south-carolina.golden.txt"))
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

// TestTodo_LEGAL_ST_SC_001_Conformance checks the South Carolina matrix
// row against the pack and proves every registered state draft still
// loads and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_SC_001_Conformance(t *testing.T) {
	pack := southCarolinaPack(t)
	// Table A Y cells: NOTICE, PAY_STMT, RETENTION. Table B Y cells:
	// FINAL_PAY, E_VERIFY, ANTI_RETAL, JOB_SEC, BREACH.
	if len(pack.Notices) == 0 || len(pack.PayStatements) == 0 || len(pack.RetentionRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.EVerifyChecks) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.JobSecurityRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// The ? cells are carried, not dropped.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix ? cell has no pack emission")
	}
	// The LEGAL-018 corrections survive conformance: no pay-equity
	// chapter, no non-compete statute.
	if len(pack.PayEquityReviews) != 0 || len(pack.NonCompeteThresholds) != 0 {
		t.Fatal("an invented chapter is back in the pack")
	}
	// Table A F cells stay absent: transparency, field restrictions,
	// wage floor (federal-only), leave, classifications, personnel files.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 || len(pack.WageFloors) != 0 ||
		len(pack.LeaveInteractions) != 0 || len(pack.Classifications) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent: mini-warn, separation filings,
	// automated decisions.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.SeparationFilings) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_SC_001_Mutation seeds mutants into the South Carolina
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_SC_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "SC")
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
