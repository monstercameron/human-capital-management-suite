package legal

// LEGAL-ST-NV-001 verification tests.
//
// The Nevada draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the one `?`-free VERIFY the corpus
// forces (no explicit equal-pay statute), the corrected citations — and
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

func nevadaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "NV").Candidate()
	if err != nil {
		t.Fatalf("Candidate(NV): %v", err)
	}
	return candidate.Pack()
}

// TestTodo_LEGAL_ST_NV_001 verifies the Nevada draft against the reviewed
// research file and the section 5 matrix row, and proves the pack is still
// unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_NV_001(t *testing.T) {
	def := loadStateDraft(t, "NV")
	if def.PackID != "us-nv-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Nevada draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "NV" {
		t.Fatalf("subdivision=%q, want NV", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	pack := nevadaPack(t)
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: flat $12.00/hr, no tip credit (constitutional
	// amendment, NRS 608.250).
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
	if !strings.Contains(floor.Citation.Section, "608.250") {
		t.Fatalf("section=%q, want NRS 608.250", floor.Citation.Section)
	}
	// NOTICE: 7-day written wage-decrease notice (NRS 608.100).
	if len(pack.Notices) != 1 {
		t.Fatalf("notices=%d, want one", len(pack.Notices))
	}
	notice := pack.Notices[0]
	if notice.TimingDays != 7 || notice.Channel != "written" {
		t.Fatalf("notice=%+v, want the 7-day written decrease notice", notice)
	}
	if !strings.Contains(notice.Citation.Section, "608.100") {
		t.Fatalf("section=%q, want NRS 608.100", notice.Citation.Section)
	}
	// FIELD_RESTRICTION: salary-history ban (NRS 613.133).
	if len(pack.FieldRestrictions) != 1 || !hasString(pack.FieldRestrictions[0].RestrictedFields, "salary_history") {
		t.Fatalf("field restrictions=%+v, want the salary-history ban", pack.FieldRestrictions)
	}
	if !strings.Contains(pack.FieldRestrictions[0].Citation.Section, "613.133") {
		t.Fatalf("section=%q, want NRS 613.133", pack.FieldRestrictions[0].Citation.Section)
	}
	// PAY_FREQUENCY: semimonthly on the 15th and last day (NRS 608.060).
	if len(pack.PayFrequencyConstraints) != 1 || pack.PayFrequencyConstraints[0].MinimumFrequency != "SEMIMONTHLY" {
		t.Fatalf("pay frequency=%+v, want the semimonthly floor", pack.PayFrequencyConstraints)
	}
	// FINAL_PAY_DEADLINE: immediate on discharge, earlier of 7 days or
	// next payday on resignation — NRS 608.020 and 608.030, not the
	// pregnancy-accommodation statute the raw extraction cited.
	if len(pack.FinalPayDeadlines) != 1 {
		t.Fatalf("final pay deadlines=%d, want one", len(pack.FinalPayDeadlines))
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.Citation.Section, "608.020") || !strings.Contains(finalPay.Citation.Section, "608.030") {
		t.Fatalf("section=%q, want NRS 608.020 and 608.030", finalPay.Citation.Section)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "immediate") || !strings.Contains(finalPay.DeadlineDescription, "7 days") {
		t.Fatalf("deadline=%q, want immediate-on-discharge and 7-day resignation rules", finalPay.DeadlineDescription)
	}
	// LEAVE_INTERACTION: 50+-employee paid leave at 0.01923 hrs/hr, the
	// 40-hour annual minimum, usable after 90 days (NRS 608.0197), plus
	// the separate domestic-violence leave up to 160 hours/12 months
	// (NRS 608.0198).
	if len(pack.LeaveInteractions) != 2 {
		t.Fatalf("leave interactions=%d, want paid leave plus domestic-violence leave", len(pack.LeaveInteractions))
	}
	var paidLeave, dvLeave *LeaveInteraction
	for i := range pack.LeaveInteractions {
		li := &pack.LeaveInteractions[i]
		switch {
		case strings.Contains(li.Citation.Section, "608.0197"):
			paidLeave = li
		case strings.Contains(li.Citation.Section, "608.0198"):
			dvLeave = li
		}
	}
	if paidLeave == nil {
		t.Fatal("no paid-leave interaction citing NRS 608.0197")
	}
	for _, want := range []string{"0.01923", "40", "90"} {
		if !strings.Contains(paidLeave.InteractionRule, want) {
			t.Fatalf("paid leave=%q, want %q carried", paidLeave.InteractionRule, want)
		}
	}
	if dvLeave == nil || !strings.Contains(dvLeave.InteractionRule, "160") {
		t.Fatalf("domestic-violence leave=%+v, want the 160-hour rule", dvLeave)
	}
	// NON_COMPETE: unenforceable for hourly employees, business-sale
	// exception only (NRS 613.195, amended 2021).
	if len(pack.NonCompeteThresholds) != 1 {
		t.Fatalf("non-competes=%d, want one", len(pack.NonCompeteThresholds))
	}
	nc := pack.NonCompeteThresholds[0]
	if !nc.ReCheckOnPayChange || !strings.Contains(nc.Rule, "hourly") || !strings.Contains(nc.Rule, "void") {
		t.Fatalf("rule=%q, want the hourly-employee voidness rule with recheck", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "613.195") {
		t.Fatalf("section=%q, want NRS 613.195", nc.Citation.Section)
	}
	// PERSONNEL_FILE: inspection right, copy denial permitted under 60
	// days' tenure — NRS 613.075, not the pregnancy-accommodation statute
	// the raw extraction cited.
	if len(pack.PersonnelFileRules) != 1 {
		t.Fatalf("personnel file rules=%d, want one", len(pack.PersonnelFileRules))
	}
	pf := pack.PersonnelFileRules[0]
	if !strings.Contains(pf.Citation.Section, "613.075") {
		t.Fatalf("section=%q, want NRS 613.075", pf.Citation.Section)
	}
	if !strings.Contains(pf.Citation.Note, "60 days") {
		t.Fatalf("note=%q, want the 60-day copy-denial rule", pf.Citation.Note)
	}
	// RETENTION: 2-year payroll records (NRS 608.115).
	if len(pack.RetentionRules) != 1 || pack.RetentionRules[0].DurationYears != 2 {
		t.Fatalf("retention=%+v, want the 2-year payroll rule", pack.RetentionRules)
	}
	// ANTI_RETALIATION: wage discussion is protected activity and
	// pay-secrecy policies are banned — NRS 613.330, not the
	// pregnancy-accommodation statute the raw extraction cited.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if !strings.Contains(ar.Citation.Section, "613.330") {
		t.Fatalf("section=%q, want NRS 613.330", ar.Citation.Section)
	}
	if !hasString(ar.ProtectedActivities, "wage_discussion") {
		t.Fatalf("activities=%+v, want wage_discussion protected", ar.ProtectedActivities)
	}
	// DRUG_TESTING: no prohibiting statute; marijuana screening is
	// permitted with narrow exceptions (NRS 613.132) — not the
	// pay-secrecy note the raw extraction filed here.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	if !strings.Contains(dt.Citation.Section, "613.132") {
		t.Fatalf("section=%q, want NRS 613.132", dt.Citation.Section)
	}
	if !hasString(dt.PermittedBases, "pre_employment") {
		t.Fatalf("permitted=%+v, want pre-employment testing", dt.PermittedBases)
	}
	// CLASSIFICATION: independent-contractor presumption (NRS 608.0155).
	// The raw extraction truncated the test mid-sentence.
	if len(pack.Classifications) != 1 || pack.Classifications[0].Dimension != "CONTRACTOR" {
		t.Fatalf("classifications=%+v, want the contractor-presumption test", pack.Classifications)
	}
	if !strings.Contains(pack.Classifications[0].TestDescription, "three or more") {
		t.Fatalf("test=%q, want the complete three-or-more-factors test", pack.Classifications[0].TestDescription)
	}
	// PAY_TRANSPARENCY: post-interview wage-range disclosure (NRS 613.133).
	if len(pack.PayTransparencyDuties) != 1 || pack.PayTransparencyDuties[0].Trigger != "internal_promotion" {
		t.Fatalf("pay transparency=%+v, want the promotion range disclosure", pack.PayTransparencyDuties)
	}
	if !strings.Contains(pack.PayTransparencyDuties[0].RequiredDisclosure, "range") {
		t.Fatalf("disclosure=%q, want the wage-range disclosure", pack.PayTransparencyDuties[0].RequiredDisclosure)
	}
	// PAY_EQUITY_REVIEW: sex-based review under the general
	// anti-discrimination rule (NRS 613.330). Nevada states no explicit
	// equal-pay statute, so this is the pack's one legitimate VERIFY.
	if len(pack.PayEquityReviews) != 1 || !hasString(pack.PayEquityReviews[0].ProtectedBases, "sex") {
		t.Fatalf("pay equity=%+v, want the sex-based review", pack.PayEquityReviews)
	}
	if pack.PayEquityReviews[0].Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("pay equity must stay VERIFY: Nevada states no explicit equal-pay statute")
	}
	// PAY_STATEMENT: itemized statement (NRS 608.115).
	if len(pack.PayStatements) != 1 {
		t.Fatalf("pay statements=%d, want one", len(pack.PayStatements))
	}
	for _, field := range []string{"gross_wages", "net_wages", "deductions", "hours_worked"} {
		if !hasString(pack.PayStatements[0].RequiredFields, field) {
			t.Fatalf("fields=%+v, want the itemized statement fields", pack.PayStatements[0].RequiredFields)
		}
	}
	// BREACH_NOTIFICATION: NRS 603A breach notification, not the bare
	// "NRS 603" section the raw extraction cited.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	if !strings.Contains(pack.BreachNotifications[0].Citation.Section, "603A") {
		t.Fatalf("section=%q, want NRS 603A", pack.BreachNotifications[0].Citation.Section)
	}
	// No truncated extraction notes survive, no section placeholders
	// survive, and the only VERIFY left is the pay-equity rule.
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nv.json"))
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
	if got := strings.Count(body, `"confidence_marker": "VERIFY"`); got != 1 {
		t.Errorf("VERIFY markers=%d, want exactly the pay-equity one", got)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
}

// TestTodo_LEGAL_ST_NV_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_NV_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-nv.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "nevada.golden.txt"))
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

// TestTodo_LEGAL_ST_NV_001_Conformance checks the Nevada matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_NV_001_Conformance(t *testing.T) {
	pack := nevadaPack(t)
	// Table A is Y across the row: every kind the flow consumes is asserted.
	if len(pack.Notices) == 0 || len(pack.PayTransparencyDuties) == 0 || len(pack.FieldRestrictions) == 0 ||
		len(pack.WageFloors) == 0 || len(pack.PayFrequencyConstraints) == 0 || len(pack.PayStatements) == 0 ||
		len(pack.LeaveInteractions) == 0 || len(pack.NonCompeteThresholds) == 0 || len(pack.Classifications) == 0 ||
		len(pack.PayEquityReviews) == 0 || len(pack.RetentionRules) == 0 || len(pack.PersonnelFileRules) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table B Y cells: final pay, drug testing, anti-retaliation, breach.
	if len(pack.FinalPayDeadlines) == 0 || len(pack.DrugTestingRules) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 {
		t.Fatal("a Table B Y cell has no pack obligation")
	}
	// Table A and B F cells stay absent: nothing beyond what the matrix
	// asserts, and no locality overlays where the matrix marks LOCAL F.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.EVerifyChecks) != 0 || len(pack.JobSecurityRules) != 0 ||
		len(pack.AutomatedDecisions) != 0 || len(pack.SeparationFilings) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	if pack.Jurisdiction.Locality != "" {
		t.Fatal("Nevada carries locality overlays the matrix marks F")
	}
	// Every registered state draft loads and validates.
	for _, code := range []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY"} {
		if _, err := loadStateDraft(t, code).Candidate(); err != nil {
			t.Errorf("Candidate(%s): %v", code, err)
		}
	}
}

// TestTodo_LEGAL_ST_NV_001_Mutation seeds mutants into the Nevada draft
// and asserts the loader and the release digest notice. A surviving
// mutant means the guard is decorative.
func TestTodo_LEGAL_ST_NV_001_Mutation(t *testing.T) {
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
			def := loadStateDraft(t, "NV")
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
		baseline := sign(t, loadStateDraft(t, "NV"))
		mutant := loadStateDraft(t, "NV")
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
