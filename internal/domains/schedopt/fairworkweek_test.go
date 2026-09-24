package schedopt

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func fairWorkweekInputs(t *testing.T) (Publication, FairWorkweekRule, values.Rate) {
	t.Helper()
	posted := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	pub, err := PublishSchedule(schedopt007Approved(t), "fair-workweek-fixture", map[string]string{}, posted)
	if err != nil {
		t.Fatal(err)
	}
	if !pub.PublishedAt.Equal(posted) {
		t.Fatalf("publication must retain its posting instant: got %s want %s", pub.PublishedAt, posted)
	}
	hours, err := values.NewQuantity("1.5", "HOUR", 1, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	rateMoney, err := values.NewMoney("20.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	rate, err := values.NewMoneyRate(rateMoney, "HOUR")
	if err != nil {
		t.Fatal(err)
	}
	return pub, FairWorkweekRule{Jurisdiction: "TEST:JURISDICTION", RuleRef: "fixture:reviewed-rule", RuleVersion: "fixture/1", CitationRef: "fixture:citation", MinimumNotice: 7 * 24 * time.Hour, PremiumHours: hours}, rate
}

func fixtureManualChange() ManualChange {
	return ManualChange{Kind: ChangeMove, AssignmentID: "assignment-1", Reason: "coverage correction", AuthorityRef: "reviewer:test"}
}

// TestTodo_REV_045_02 records publication time and prices a late manual
// schedule change as a separate, rule-bound obligation.
func TestTodo_REV_045_02(t *testing.T) {
	pub, rule, rate := fairWorkweekInputs(t)
	changed := pub.PublishedAt.Add(24 * time.Hour)
	got, err := AssessPublishedScheduleChange(pub, fixtureManualChange(), changed, rule.Jurisdiction, map[string]FairWorkweekRule{rule.Jurisdiction: rule}, rate)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "PREMIUM" || got.Obligation == nil {
		t.Fatalf("short notice must create obligation: %+v", got)
	}
	o := got.Obligation
	if o.Kind != "SCHEDULE_CHANGE_PREMIUM" || o.Amount.String() != "30.00 USD" || o.Basis != PremiumBasisRegularRateHours {
		t.Fatalf("obligation must carry amount and typed basis: %+v amount=%s", o, o.Amount.String())
	}
	if o.RuleRef != rule.RuleRef || o.RuleVersion != rule.RuleVersion || o.CitationRef != rule.CitationRef || !o.PostingInstant.Equal(pub.PublishedAt) || !o.ChangeInstant.Equal(changed) {
		t.Fatalf("obligation must retain legal and temporal provenance: %+v", o)
	}
	ordinary := values.Money{}
	if o.Amount == ordinary {
		t.Fatal("premium amount must be an independent typed money value")
	}

	good, err := AssessPublishedScheduleChange(pub, fixtureManualChange(), pub.PublishedAt.Add(rule.MinimumNotice), rule.Jurisdiction, map[string]FairWorkweekRule{rule.Jurisdiction: rule}, rate)
	if err != nil || good.State != "COMPLIANT" || good.Obligation != nil {
		t.Fatalf("sufficient notice should not create premium: %+v err=%v", good, err)
	}
}

func TestTodo_REV_045_02_ApplyReviewIntegration(t *testing.T) {
	base := schedopt006Schedule()
	approved, err := ApplyReview(base, nil, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	postedAt := schedopt006At.Add(time.Hour)
	pub, err := PublishSchedule(approved, "fair-workweek-apply-review", map[string]string{}, postedAt)
	if err != nil {
		t.Fatal(err)
	}
	_, rule, rate := fairWorkweekInputs(t)
	// Use the published instant as the posting reference while keeping this
	// test's legal inputs synthetic and explicitly cited.
	rule.Jurisdiction = "TEST:JURISDICTION"
	changedAt := postedAt.Add(24 * time.Hour)
	change := ManualChange{Kind: ChangeMove, AssignmentID: "a-1", WorkerRef: "worker-target", WindowRef: "window-tue-am", Reason: "coverage correction", AuthorityRef: "scheduler:maya", ChangedAt: changedAt}
	destinationMoney, err := values.NewMoney("50.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	destinationRate, err := values.NewMoneyRate(destinationMoney, "HOUR")
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyPublishedReview(base, pub, rule.Jurisdiction,
		map[string]FairWorkweekRule{rule.Jurisdiction: rule}, map[string]values.Rate{"worker-1": rate, "worker-target": destinationRate},
		[]ManualChange{change}, "scheduler:maya", changedAt)
	if err != nil {
		t.Fatalf("published change should be assessed and recorded: %v", err)
	}
	if len(result.FairWorkweekAssessments) != 1 || len(result.PremiumObligations) != 1 {
		t.Fatalf("late manual change must produce a separate obligation: %+v", result)
	}
	if result.PremiumObligations[0].ChangeDigest != manualChangeDigest(change, changedAt) || result.BoundDigest != result.computedDigest() {
		t.Fatal("obligation must bind to the precise approved manual change")
	}
	if result.PremiumObligations[0].Amount.String() != "30.00 USD" {
		t.Fatalf("MOVE must price the impacted published worker, not its destination worker: %s", result.PremiumObligations[0].Amount.String())
	}
	mutated := result
	mutated.PremiumObligations = append([]PremiumObligation(nil), result.PremiumObligations...)
	mutated.PremiumObligations[0].ChangeDigest = "sha256:other-change"
	if mutated.computedDigest() == result.BoundDigest {
		t.Fatal("approval digest must bind obligation to change identity")
	}

	_, err = ApplyPublishedReview(base, pub, "TEST:UNKNOWN", nil, map[string]values.Rate{"worker-1": rate, "worker-target": destinationRate}, []ManualChange{change}, "scheduler:maya", changedAt)
	var rejected *ReviewRejection
	if !errors.As(err, &rejected) || rejected.State != "UNKNOWN_REVIEW" {
		t.Fatalf("unresolved jurisdiction must block approval with UNKNOWN_REVIEW: %v", err)
	}
}

func TestTodo_REV_045_02_CompliantDigestBindsRuleProvenance(t *testing.T) {
	base := schedopt006Schedule()
	pre, err := ApplyReview(base, nil, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	posted := schedopt006At.Add(time.Hour)
	pub, err := PublishSchedule(pre, "fair-workweek-compliant-binding", map[string]string{}, posted)
	if err != nil {
		t.Fatal(err)
	}
	_, rule, rate := fairWorkweekInputs(t)
	rule.Jurisdiction = "TEST:JURISDICTION"
	change := ManualChange{Kind: ChangeMove, AssignmentID: "a-1", WorkerRef: "worker-1", WindowRef: "window-tue-am", Reason: "coverage", AuthorityRef: "scheduler:maya", ChangedAt: posted.Add(rule.MinimumNotice)}
	result, err := ApplyPublishedReview(base, pub, rule.Jurisdiction, map[string]FairWorkweekRule{rule.Jurisdiction: rule}, map[string]values.Rate{"worker-1": rate}, []ManualChange{change}, "scheduler:maya", change.ChangedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FairWorkweekAssessments) != 1 || result.FairWorkweekAssessments[0].State != "COMPLIANT" || len(result.PremiumObligations) != 0 {
		t.Fatalf("sufficient notice should retain a bound compliant assessment: %+v", result)
	}
	mutations := []func(*FairWorkweekAssessment){
		func(a *FairWorkweekAssessment) { a.Jurisdiction = "TEST:OTHER" },
		func(a *FairWorkweekAssessment) { a.RuleRef = "fixture:other-rule" },
		func(a *FairWorkweekAssessment) { a.RuleVersion = "fixture/2" },
		func(a *FairWorkweekAssessment) { a.CitationRef = "fixture:other-citation" },
	}
	for i, mutate := range mutations {
		changed := result
		changed.FairWorkweekAssessments = append([]FairWorkweekAssessment(nil), result.FairWorkweekAssessments...)
		mutate(&changed.FairWorkweekAssessments[0])
		if changed.computedDigest() == result.BoundDigest {
			t.Fatalf("rule provenance mutation %d must change approved digest", i)
		}
	}
}

func TestTodo_REV_045_02_Property(t *testing.T) {
	pub, rule, rate := fairWorkweekInputs(t)
	unknown, err := AssessPublishedScheduleChange(pub, fixtureManualChange(), pub.PublishedAt.Add(time.Hour), "TEST:UNKNOWN", nil, rate)
	if err != nil || unknown.State != "UNKNOWN_REVIEW" || unknown.ReviewCode == "" {
		t.Fatalf("unresolved jurisdiction must require review: %+v err=%v", unknown, err)
	}
	missingJurisdiction, err := AssessPublishedScheduleChange(pub, fixtureManualChange(), pub.PublishedAt.Add(time.Hour), "", nil, rate)
	if err != nil || missingJurisdiction.State != "UNKNOWN_REVIEW" {
		t.Fatalf("missing jurisdiction must require review: %+v err=%v", missingJurisdiction, err)
	}
	_, err = AssessPublishedScheduleChange(pub, fixtureManualChange(), pub.PublishedAt.Add(time.Hour), rule.Jurisdiction, map[string]FairWorkweekRule{rule.Jurisdiction: rule}, rate)
	if err != nil {
		t.Fatal(err)
	}
	bad := rule
	bad.RuleRef = ""
	result, err := AssessPublishedScheduleChange(pub, fixtureManualChange(), pub.PublishedAt.Add(time.Hour), rule.Jurisdiction, map[string]FairWorkweekRule{rule.Jurisdiction: bad}, rate)
	if err != nil || result.State != "UNKNOWN_REVIEW" {
		t.Fatalf("unreviewed rule must require review: %+v err=%v", result, err)
	}
	first, err := AssessPublishedScheduleChange(pub, fixtureManualChange(), pub.PublishedAt.Add(time.Hour), rule.Jurisdiction, map[string]FairWorkweekRule{rule.Jurisdiction: rule}, rate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AssessPublishedScheduleChange(pub, fixtureManualChange(), pub.PublishedAt.Add(2*time.Hour), rule.Jurisdiction, map[string]FairWorkweekRule{rule.Jurisdiction: rule}, rate)
	if err != nil {
		t.Fatal(err)
	}
	if first.Obligation.Amount.String() != second.Obligation.Amount.String() {
		t.Fatal("premium pricing must depend on declared rule basis and rate, not elapsed notice")
	}
	unspecifiedLifecycle := schedopt006Schedule()
	unspecifiedLifecycle.ReviewLifecycle = ""
	_, err = ApplyReview(unspecifiedLifecycle, []ManualChange{{Kind: ChangeRemove, AssignmentID: "a-1", Reason: "coverage", AuthorityRef: "scheduler:maya", ChangedAt: pub.PublishedAt.Add(time.Hour)}}, "scheduler:maya", pub.PublishedAt.Add(time.Hour))
	var rejected *ReviewRejection
	if !errors.As(err, &rejected) || rejected.State != "UNKNOWN_REVIEW" {
		t.Fatalf("missing publication context and prepublication declaration must fail closed: %v", err)
	}
}

func TestTodo_REV_045_02_Golden(t *testing.T) {
	pub, rule, rate := fairWorkweekInputs(t)
	changed := pub.PublishedAt.Add(24 * time.Hour)
	got, err := AssessPublishedScheduleChange(pub, fixtureManualChange(), changed, rule.Jurisdiction, map[string]FairWorkweekRule{rule.Jurisdiction: rule}, rate)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "PREMIUM" || got.Notice != 24*time.Hour || got.Obligation.Amount.String() != "30.00 USD" || got.Obligation.PremiumHours.String() != "1.5 HOUR" {
		t.Fatalf("golden fair-workweek vector changed: %+v amount=%q hours=%q", got, got.Obligation.Amount.String(), got.Obligation.PremiumHours.String())
	}
}
