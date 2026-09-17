package legal

// LEGAL-ST-CT-001 verification tests.
//
// The Connecticut draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the matrix row matched, the `?`-cell
// VERIFY markers carried rather than dropped — and the guardrail that keeps
// the pack out of evaluation until counsel approves it. They do not, and
// must not, mark the pack reviewed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func connecticutPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "CT").Candidate()
	if err != nil {
		t.Fatalf("Candidate(CT): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_CT_001 verifies the Connecticut draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_CT_001(t *testing.T) {
	def := loadStateDraft(t, "CT")
	if def.PackID != "us-ct-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Connecticut draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "CT" {
		t.Fatalf("subdivision=%q, want CT", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := connecticutPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: the research states $16.35/hr (2025-01-01); the pack
	// carries the research figure, hourly basis.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly one", len(pack.WageFloors))
	}
	if got := pack.WageFloors[0].FloorAmount.String(); got != "16.35 USD" {
		t.Fatalf("floor=%q, want the research-stated $16.35/hr", got)
	}
	if pack.WageFloors[0].Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", pack.WageFloors[0].Basis)
	}
	// PAY_TRANSPARENCY: wage-range disclosure on promotion.
	if len(pack.PayTransparencyDuties) != 1 {
		t.Fatalf("pay transparency duties=%d, want one", len(pack.PayTransparencyDuties))
	}
	pt := pack.PayTransparencyDuties[0]
	if pt.Trigger != "internal_promotion" || !strings.Contains(pt.RequiredDisclosure, "wage range") {
		t.Fatalf("duty=%+v, want the internal-promotion wage-range duty", pt)
	}
	// FIELD_RESTRICTION: salary-history ban.
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "31-40z") {
		t.Fatalf("section=%q, want the § 31-40z ban", pack.FieldRestrictions[0].Citation.Section)
	}
	// PAY_FREQUENCY: weekly (? cell carried at VERIFY, not dropped).
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "WEEKLY" {
		t.Fatalf("pay frequency=%+v, want the weekly floor", pack.PayFrequencyConstraints)
	}
	if pack.PayFrequencyConstraints[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay frequency must stay VERIFY: the matrix marks PAY_FREQ ?")
	}
	// FINAL_PAY_DEADLINE: next business day on discharge, next payday on
	// layoff, per § 31-71c.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	// LEAVE_INTERACTION: CTFMLA 16-week/24-month leave.
	if len(pack.LeaveInteractions) != 1 || !strings.Contains(pack.LeaveInteractions[0].InteractionRule, "16 weeks") {
		t.Fatalf("leave=%+v, want the 16-week CTFMLA rule", pack.LeaveInteractions)
	}
	// NON_COMPETE: § 31-49h recheck on pay change.
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange {
		t.Fatalf("rule=%q, want the pay-change recheck", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "31-49h") {
		t.Fatalf("section=%q, want § 31-49h", nc.Citation.Section)
	}
	// PERSONNEL_FILE: § 31-48h inspection.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	if !strings.Contains(pack.PersonnelFileRules[0].Citation.Section, "31-48h") {
		t.Fatalf("section=%q, want § 31-48h", pack.PersonnelFileRules[0].Citation.Section)
	}
	// MINI_WARN: § 31-51u severance at 2 months' pay per year of service.
	if len(pack.MiniWARNTriggers) != 1 || !strings.Contains(pack.MiniWARNTriggers[0].Note, "2 months") {
		t.Fatalf("mini-warn=%+v, want the 2-months-per-year severance", pack.MiniWARNTriggers)
	}
	// PAY_EQUITY_REVIEW: sex-based equal-pay review.
	if len(pack.PayEquityReviews) != 1 || !hasString(pack.PayEquityReviews[0].ProtectedBases, "sex") {
		t.Fatalf("pay equity=%+v, want the sex-based review", pack.PayEquityReviews)
	}
	// RETENTION: 3-year payroll records.
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 3 {
		t.Fatalf("retention=%+v, want the 3-year payroll rule", pack.RetentionRules)
	}
	// NOTICE and CLASSIFICATION are ? cells with evidence, so they are
	// carried at VERIFY: written notice, ABC-test contractor dimension.
	if len(pack.Notices) != 1 || pack.Notices[0].Channel != "written" {
		t.Fatalf("notices=%+v, want the written-notice ? emission", pack.Notices)
	}
	if len(pack.Classifications) != 1 || pack.Classifications[0].Dimension != "CONTRACTOR" {
		t.Fatalf("classifications=%+v, want the contractor-test ? emission", pack.Classifications)
	}
	// PAY_STATEMENT is a ? cell with evidence, carried at VERIFY.
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want the ?-cell emission", len(pack.PayStatements))
	}
	if pack.PayStatements[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay statement must stay VERIFY: the matrix marks PAY_STMT ?")
	}
	// ANTI_RETALIATION: the corpus's own citation gap stays visible as a
	// VERIFY obligation with no stated section, never as an absence of duty.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want the gap carried, not dropped", len(pack.AntiRetaliationRules))
	}
	if pack.AntiRetaliationRules[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("anti-retaliation must stay VERIFY: its citation gap is unverified")
	}
	if !strings.Contains(pack.AntiRetaliationRules[0].Citation.Section, "not stated") {
		t.Fatalf("section=%q, want the visible gap marker", pack.AntiRetaliationRules[0].Citation.Section)
	}
	// DRUG_TESTING (? with evidence) and BREACH_NOTIFICATION (Y) each
	// appear once.
	if len(pack.DrugTestingRules) != 1 || !hasString(pack.DrugTestingRules[0].PermittedBases, "pre_employment") {
		t.Fatalf("drug testing=%+v, want the federal-baseline rule", pack.DrugTestingRules)
	}
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	// F cells stay absent: e-verify, separation filings, job security,
	// automated decisions, monitoring consents.
	if len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_CT_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_CT_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-ct.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "connecticut.golden.txt"))
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

// TestTodo_LEGAL_ST_CT_001_Conformance checks the Connecticut matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_CT_001_Conformance(t *testing.T) {
	pack := connecticutPack(t)
	// Table A Y cells: PAY_TRANSPARENCY, FIELD_RESTRICTION, WAGE_FLOOR,
	// LEAVE, NON_COMPETE, PAY_EQUITY, PERSONNEL_FILE. Table B Y cells:
	// FINAL_PAY, MINI_WARN, ANTI_RETALIATION, BREACH.
	if len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 || len(pack.WageFloors) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 || len(pack.PayEquityReviews) == 0 ||
		len(pack.PersonnelFileRules) == 0 || len(pack.FinalPayDeadlines) == 0 || len(pack.MiniWARNTriggers) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: nothing new beyond the ?-cell
	// emissions the PRIMARY test pins.
	if len(pack.EVerifyChecks) != 0 || len(pack.SeparationFilings) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_CT_001_Mutation seeds mutants into the Connecticut
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_CT_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "CT")
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
