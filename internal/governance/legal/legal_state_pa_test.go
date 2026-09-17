package legal

// LEGAL-ST-PA-001 verification tests.
//
// The Pennsylvania draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the locality-only Philadelphia and
// Pittsburgh rules kept out of the state pack, the matrix row matched —
// and the guardrail that keeps the pack out of evaluation until counsel
// approves it. They do not, and must not, mark the pack reviewed.
//
// Philadelphia's salary-history ban and paid-sick-leave ordinance and
// Pittsburgh's paid-sick-leave ordinance (eff. 2026-01-01) are
// locality-only with no statewide equivalent: LEGAL-TOOL-009 is the only
// mechanism that may attach them, so the state pack carries no field
// restriction and no leave interaction.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pennsylvaniaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "PA").Candidate()
	if err != nil {
		t.Fatalf("Candidate(PA): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_PA_001 verifies the Pennsylvania draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_PA_001(t *testing.T) {
	def := loadStateDraft(t, "PA")
	if def.PackID != "us-pa-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Pennsylvania draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "PA" {
		t.Fatalf("subdivision=%q, want PA", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := pennsylvaniaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only. 43 P.S. § 333.101 et seq. holds $7.25/hr
	// ($2.83/hr tipped), so the pack carries no state floor.
	if len(pack.WageFloors) != 0 {
		t.Fatalf("wage floors=%+v, want no state floor (federal-only)", pack.WageFloors)
	}
	// NOTICE: written notice at hire and in advance of any
	// wage/frequency/deduction/benefit change, 43 P.S. § 260.4.
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written-notice rule", pack.Notices)
	}
	notice := pack.Notices[0]
	if !hasString(notice.ContentFields, "pay_rate") {
		t.Fatalf("notice=%+v, want the pay-rate content carried", notice)
	}
	if !strings.Contains(notice.Citation.Section, "260") {
		t.Fatalf("section=%q, want 43 P.S. § 260.4", notice.Citation.Section)
	}
	if !strings.Contains(notice.Citation.Note, "WPCL") {
		t.Fatalf("note=%q, want the WPCL compliance flag", notice.Citation.Note)
	}
	// FINAL_PAY_DEADLINE: next regular payday regardless of separation
	// reason, 25% liquidated damages plus attorney fees for withholding,
	// WPCL 43 P.S. § 260 et seq.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	if !strings.Contains(pack.FinalPayDeadlines[0].Citation.Section, "260.3") {
		t.Fatalf("section=%q, want 43 P.S. § 260.3", pack.FinalPayDeadlines[0].Citation.Section)
	}
	// PAY_EQUITY_REVIEW: sex-only equal-pay statute, 43 P.S. § 336.1.
	if len(pack.PayEquityReviews) != 1 {
		t.Fatalf("pay equity reviews=%d, want one", len(pack.PayEquityReviews))
	}
	pe := pack.PayEquityReviews[0]
	if !hasString(pe.ProtectedBases, "sex") {
		t.Fatalf("review=%+v, want the sex-only review", pe)
	}
	if !strings.Contains(pe.Citation.Section, "336.1") {
		t.Fatalf("section=%q, want 43 P.S. § 336.1", pe.Citation.Section)
	}
	if pe.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay equity must stay VERIFY: the research marks the scope for verification")
	}
	// NON_COMPETE: common-law reasonableness, with the healthcare
	// 1-year cap, Act 74 eff. 2025-01-01.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Citation.Note, "Act 74") {
		t.Fatalf("note=%q, want the Act 74 healthcare cap carried", nc.Citation.Note)
	}
	if nc.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("non-compete must stay VERIFY: the research states no statutory section")
	}
	// PERSONNEL_FILE: Personnel Files Act, 43 P.S. §§ 1321-1324.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	if !strings.Contains(pack.PersonnelFileRules[0].Citation.Section, "1321") {
		t.Fatalf("section=%q, want 43 P.S. §§ 1321-1324", pack.PersonnelFileRules[0].Citation.Section)
	}
	// ? cells carried at VERIFY, never dropped: pay frequency, pay
	// statement, retention, drug testing.
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks it ?")
	}
	if len(pack.PayStatements) != 1 || pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay statement must stay VERIFY: the matrix marks it ?")
	}
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("retention must stay VERIFY: the matrix marks it ?")
	}
	if len(pack.DrugTestingRules) != 1 || pack.DrugTestingRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("drug testing must stay VERIFY: the matrix marks it ?")
	}
	// ANTI_RETALIATION: jury-duty protection.
	if len(pack.AntiRetaliationRules) != 1 || !hasString(pack.AntiRetaliationRules[0].ProtectedActivities, "jury_duty") {
		t.Fatalf("anti-retaliation=%+v, want the jury-duty rule", pack.AntiRetaliationRules)
	}
	if pack.AntiRetaliationRules[0].Disposition != "BLOCK" {
		t.Fatalf("disposition=%q, want BLOCK", pack.AntiRetaliationRules[0].Disposition)
	}
	// BREACH_NOTIFICATION: 73 P.S. § 2301.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Section, "2301") {
		t.Fatalf("section=%q, want 73 P.S. § 2301", pack.BreachNotifications[0].Citation.Section)
	}
	// L cells stay out of the state pack: no field restriction
	// (Philadelphia-only ban) and no leave interaction
	// (Philadelphia/Pittsburgh-only sick leave).
	if len(pack.FieldRestrictions) != 0 {
		t.Fatalf("field restrictions=%+v, the Philadelphia ban is locality-only", pack.FieldRestrictions)
	}
	if len(pack.LeaveInteractions) != 0 {
		t.Fatalf("leave=%+v, paid sick leave is locality-only", pack.LeaveInteractions)
	}
	// All other F cells stay absent: transparency, classifications,
	// mini-warn, e-verify, separation filings, job security, automated
	// decisions, monitoring consents.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.Classifications) != 0 || len(pack.MiniWARNTriggers) != 0 ||
		len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_PA_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_PA_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-pa.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "pennsylvania.golden.txt"))
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

// TestTodo_LEGAL_ST_PA_001_Conformance checks the Pennsylvania matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_PA_001_Conformance(t *testing.T) {
	pack := pennsylvaniaPack(t)
	// Table A Y cells: NOTICE, NON_COMPETE, PAY_EQUITY, PERSONNEL_FILE.
	// Table B Y cells: FINAL_PAY, ANTI_RETAL, BREACH.
	if len(pack.Notices) == 0 || len(pack.NonCompeteThresholds) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.PersonnelFileRules) == 0 ||
		len(pack.FinalPayDeadlines) == 0 || len(pack.AntiRetaliationRules) == 0 ||
		len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// The ? cells are carried, not dropped.
	if len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.RetentionRules) == 0 || len(pack.DrugTestingRules) == 0 {
		t.Fatal("a matrix ? cell has no pack emission")
	}
	// Table A L cells stay out of the state pack.
	if len(pack.FieldRestrictions) != 0 || len(pack.LeaveInteractions) != 0 {
		t.Fatal("a locality-only rule leaked into the state pack")
	}
	// Table A F cells stay absent: transparency, wage floor
	// (federal-only), classifications.
	if len(pack.PayTransparencyDuties) != 0 || len(pack.WageFloors) != 0 || len(pack.Classifications) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent: mini-warn, e-verify, job security,
	// automated decisions.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 ||
		len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_PA_001_Mutation seeds mutants into the Pennsylvania
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_PA_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "PA")
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
