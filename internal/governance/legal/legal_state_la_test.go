package legal

// LEGAL-ST-LA-001 verification tests.
//
// The Louisiana draft is agent-verified, not counsel-reviewed: the pack
// stays UNREVIEWED and unusable under any nonzero tenant review floor.
// These tests pin the verification half of the review — every GREEN
// parameter the research supports, the LEAVE_INTERACTION preemption
// assertion carried as evaluation data for LEGAL-013, the matrix row
// matched — and the guardrail that keeps the pack out of evaluation
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

func louisianaPack(t *testing.T) RulePack {
	t.Helper()
	candidate, err := loadStateDraft(t, "LA").Candidate()
	if err != nil {
		t.Fatalf("Candidate(LA): %v", err)
	}
	return candidate.Pack()
}

// checkLouisianaPack runs the PRIMARY assertions over a draft definition.
// The committed test loads the checked-in file; pre-registration staging
// runs this same function over the staged bytes, so both verify the
// identical contract.
func checkLouisianaPack(t *testing.T, def PackDefinition) RulePack {
	t.Helper()
	if def.PackID != "us-la-promotion-base-pay-change-draft" {
		t.Fatalf("pack_id=%q, want the Louisiana draft", def.PackID)
	}
	if def.ReviewStatus != "UNREVIEWED" {
		t.Fatalf("review_status=%q, agent verification is not counsel review", def.ReviewStatus)
	}
	if def.Jurisdiction.Subdivision != "LA" {
		t.Fatalf("subdivision=%q, want LA", def.Jurisdiction.Subdivision)
	}
	if def.Window.Start != "2026-01-01" {
		t.Fatalf("window start=%q, want the draft fixture window", def.Window.Start)
	}
	candidate, err := def.Candidate()
	if err != nil {
		t.Fatalf("Candidate(LA): %v", err)
	}
	pack := candidate.Pack()
	if pack.SourceType != SourceTypeStatute {
		t.Fatalf("source=%s, want STATUTE", pack.SourceType)
	}
	if pack.VocabularyVersion != VocabularyVersion2 {
		t.Fatalf("vocabulary=%d, want v2", pack.VocabularyVersion)
	}

	// WAGE_FLOOR: federal-only, F. Louisiana has no state minimum-wage
	// statute, so the floor carries no state amount on an hourly basis.
	if len(pack.WageFloors) != 1 {
		t.Fatalf("wage floors=%d, want exactly the federal-only floor", len(pack.WageFloors))
	}
	floor := pack.WageFloors[0]
	if got := floor.FloorAmount.String(); got != "" {
		t.Fatalf("floor=%q, Louisiana states no minimum", got)
	}
	if floor.Basis != "HOURLY" {
		t.Fatalf("basis=%q, want HOURLY", floor.Basis)
	}
	if !strings.Contains(floor.Citation.Section, "206") {
		t.Fatalf("section=%q, want 29 U.S.C. § 206", floor.Citation.Section)
	}
	if !strings.Contains(floor.Citation.Note, "7.25") {
		t.Fatalf("note=%q, want the $7.25 federal floor carried", floor.Citation.Note)
	}
	// FINAL_PAY_DEADLINE: earlier of 15 days or next regular payday,
	// penalty interest for violation, La. R.S. 23:631-632.
	if len(pack.FinalPayDeadlines) != 1 || pack.FinalPayDeadlines[0].Trigger != "termination_any" {
		t.Fatalf("final pay=%+v, want the termination deadline", pack.FinalPayDeadlines)
	}
	finalPay := pack.FinalPayDeadlines[0]
	if !strings.Contains(finalPay.DeadlineDescription, "15 days") {
		t.Fatalf("deadline=%q, want the 15-day rule", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.DeadlineDescription, "next regular payday") {
		t.Fatalf("deadline=%q, want the next-payday rule", finalPay.DeadlineDescription)
	}
	if !strings.Contains(finalPay.Citation.Section, "23:631") {
		t.Fatalf("section=%q, want La. R.S. 23:631-632", finalPay.Citation.Section)
	}
	if finalPay.Citation.ConfidenceMarker != ConfidenceMarkerConfirmed {
		t.Fatal("final pay must read CONFIRMED: the research states the rule plainly")
	}
	// NON_COMPETE: 2-year cap, reasonable scope and territory,
	// legitimate business interest, La. R.S. 23:921.
	if len(pack.NonCompeteThresholds) != 1 || !pack.NonCompeteThresholds[0].ReCheckOnPayChange {
		t.Fatalf("non-competes=%+v, want the pay-change recheck", pack.NonCompeteThresholds)
	}
	nc := pack.NonCompeteThresholds[0]
	if !strings.Contains(nc.Rule, "2 years") {
		t.Fatalf("rule=%q, want the 2-year cap", nc.Rule)
	}
	if !strings.Contains(nc.Rule, "legitimate business interest") {
		t.Fatalf("rule=%q, want the business-interest requirement", nc.Rule)
	}
	if !strings.Contains(nc.Citation.Section, "23:921") {
		t.Fatalf("section=%q, want La. R.S. 23:921", nc.Citation.Section)
	}
	// ANTI_RETALIATION: 90-day presumption over jury duty, voting,
	// workers-compensation, FMLA and whistleblower activity.
	if len(pack.AntiRetaliationRules) != 1 {
		t.Fatalf("anti-retaliation rules=%d, want one", len(pack.AntiRetaliationRules))
	}
	ar := pack.AntiRetaliationRules[0]
	if ar.LookbackDays != 90 {
		t.Fatalf("lookback=%d, want the 90-day presumption", ar.LookbackDays)
	}
	for _, want := range []string{"workers_compensation_claim", "whistleblower_report", "jury_duty"} {
		if !hasString(ar.ProtectedActivities, want) {
			t.Fatalf("activities=%+v, want %q carried", ar.ProtectedActivities, want)
		}
	}
	if !strings.Contains(ar.Citation.Section, "23:967") {
		t.Fatalf("section=%q, want La. R.S. 23:967", ar.Citation.Section)
	}
	// SEPARATION_FILING: UI Separation Notice with LWC. No state form is
	// identified (LEGAL-TOOL-008 records NONE_IDENTIFIED), so the duty is
	// carried at VERIFY rather than dropped.
	if len(pack.SeparationFilings) != 1 {
		t.Fatalf("separation filings=%d, want one", len(pack.SeparationFilings))
	}
	sf := pack.SeparationFilings[0]
	if sf.RecipientAuthority != "state unemployment insurance agency" {
		t.Fatalf("recipient=%q, want the LWC unemployment agency", sf.RecipientAuthority)
	}
	if sf.FormName != "" {
		t.Fatalf("form=%q, no state form is identified", sf.FormName)
	}
	if sf.Citation.ConfidenceMarker != ConfidenceMarkerVerify {
		t.Fatal("separation filing must stay VERIFY: no state form is identified")
	}
	// DRUG_TESTING: pre-employment, post-accident and reasonable-suspicion
	// testing with notice and procedures, La. R.S. 23:1017.1 et seq.
	if len(pack.DrugTestingRules) != 1 {
		t.Fatalf("drug testing rules=%d, want one", len(pack.DrugTestingRules))
	}
	dt := pack.DrugTestingRules[0]
	for _, want := range []string{"pre_employment", "post_accident", "reasonable_suspicion"} {
		if !hasString(dt.PermittedBases, want) {
			t.Fatalf("bases=%+v, want %q carried", dt.PermittedBases, want)
		}
	}
	if !strings.Contains(dt.Citation.Section, "1017.1") {
		t.Fatalf("section=%q, want La. R.S. 23:1017.1", dt.Citation.Section)
	}
	// BREACH_NOTIFICATION: "without unreasonable delay", La. R.S.
	// 51:3071 et seq. The statute states no day count, so none is typed.
	if len(pack.BreachNotifications) != 1 {
		t.Fatalf("breach notifications=%d, want one", len(pack.BreachNotifications))
	}
	bn := pack.BreachNotifications[0]
	if bn.SubjectDeadlineDays != 0 {
		t.Fatalf("deadline=%d, the statute states no day count", bn.SubjectDeadlineDays)
	}
	if !strings.Contains(bn.Citation.Section, "51:3071") {
		t.Fatalf("section=%q, want La. R.S. 51:3071", bn.Citation.Section)
	}
	// PAY_EQUITY_REVIEW is F: no state-specific rule, correctly absent.
	// E_VERIFY is L pub (public contracts only): a locality-scoped rule
	// that belongs to a locality-level release, correctly absent here.
	if len(pack.PayEquityReviews) != 0 || len(pack.EVerifyChecks) != 0 {
		t.Fatal("PAY_EQUITY (F) and E_VERIFY (L) must stay out of the subdivision pack")
	}
	// Table A F cells stay absent: notices, pay transparency, field
	// restrictions, pay frequency, pay statements, leave interactions,
	// classifications, retention, personnel files.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.PayFrequencyConstraints) != 0 || len(pack.PayStatements) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.RetentionRules) != 0 || len(pack.PersonnelFileRules) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Table B F cells stay absent: mini-warn, job security, automated
	// decisions.
	if len(pack.MiniWARNTriggers) != 0 || len(pack.JobSecurityRules) != 0 || len(pack.AutomatedDecisions) != 0 ||
		len(pack.MonitoringConsents) != 0 {
		t.Fatal("a matrix F cell gained a pack obligation")
	}
	// Preemption: LEAVE_INTERACTION is preempted at locality level
	// statewide (La. R.S. 23:642). The assertion is data for LEGAL-013's
	// evaluation stage, never an inline exception in a comparator.
	if len(pack.PreemptionAssertions) != 1 {
		t.Fatalf("preemption assertions=%d, want the LEAVE_INTERACTION assertion", len(pack.PreemptionAssertions))
	}
	pre := pack.PreemptionAssertions[0]
	if pre.Kind != ObligationTypeLeaveInteraction || pre.Scope != "LOCALITY_ONLY" {
		t.Fatalf("preemption=%+v, want LEAVE_INTERACTION at LOCALITY_ONLY", pre)
	}
	if !strings.Contains(pre.Citation.Section, "23:642") {
		t.Fatalf("section=%q, want La. R.S. 23:642", pre.Citation.Section)
	}
	// Guardrail: the verified draft is still unreleasable.
	if pack.ReviewStatus.Releasable() {
		t.Fatal("UNREVIEWED pack reports releasable")
	}
	return pack
}

// TestTodo_LEGAL_ST_LA_001 verifies the Louisiana draft against the
// reviewed research file and the section 5 matrix row, and proves the pack
// is still unreleasable pending counsel approval.
func TestTodo_LEGAL_ST_LA_001(t *testing.T) {
	checkLouisianaPack(t, loadStateDraft(t, "LA"))
}

// TestTodo_LEGAL_ST_LA_001_Golden pins the verified draft bytes: any
// silent edit to the pack breaks this test before it reaches evaluation.
func TestTodo_LEGAL_ST_LA_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(mustStatePackPath(t, "us-la.json"))
	if err != nil {
		t.Fatalf("read pack file: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(sum[:])
	want, err := os.ReadFile(filepath.Join("testdata", "louisiana.golden.txt"))
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

// TestTodo_LEGAL_ST_LA_001_Conformance checks the Louisiana matrix row
// against the pack and proves every registered state draft still loads
// and validates, so no other draft rotted underneath this review.
func TestTodo_LEGAL_ST_LA_001_Conformance(t *testing.T) {
	pack := louisianaPack(t)
	// Table A Y cells: NON_COMPETE. Table B Y cells: FINAL_PAY,
	// SEP_FILING, DRUG_TESTING, ANTI_RETALIATION, BREACH. The federal-only
	// WAGE_FLOOR rides along as the F-baseline record.
	if len(pack.NonCompeteThresholds) == 0 || len(pack.FinalPayDeadlines) == 0 ||
		len(pack.SeparationFilings) == 0 || len(pack.DrugTestingRules) == 0 ||
		len(pack.AntiRetaliationRules) == 0 || len(pack.BreachNotifications) == 0 ||
		len(pack.WageFloors) == 0 {
		t.Fatal("a matrix Y cell has no pack obligation")
	}
	// Table A F cells stay absent: notices, pay transparency, field
	// restrictions, pay frequency, pay statements, leave interactions,
	// classifications, pay equity, retention, personnel files. Table B F
	// cells stay absent: mini-warn, job security, automated decisions.
	if len(pack.Notices) != 0 || len(pack.PayTransparencyDuties) != 0 || len(pack.FieldRestrictions) != 0 ||
		len(pack.PayFrequencyConstraints) != 0 || len(pack.PayStatements) != 0 || len(pack.LeaveInteractions) != 0 ||
		len(pack.Classifications) != 0 || len(pack.PayEquityReviews) != 0 || len(pack.RetentionRules) != 0 ||
		len(pack.PersonnelFileRules) != 0 || len(pack.MiniWARNTriggers) != 0 || len(pack.JobSecurityRules) != 0 ||
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

// TestTodo_LEGAL_ST_LA_001_Mutation seeds mutants into the Louisiana
// draft's guards and asserts the loader notices. A surviving mutant means
// the guard is decorative.
func TestTodo_LEGAL_ST_LA_001_Mutation(t *testing.T) {
	checkPackMutation(t, loadStateDraft(t, "LA"))
}

// checkPackMutation runs the loader-guard mutants over a draft definition.
func checkPackMutation(t *testing.T, def PackDefinition) {
	t.Helper()
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
			mut := def
			mut.Obligations = append([]ObligationJSON(nil), def.Obligations...)
			mut.Preemptions = append([]PreemptionJSON(nil), def.Preemptions...)
			m.mutate(&mut)
			encoded, err := json.Marshal(mut)
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
