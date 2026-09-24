package workerlifecycle

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

func TestTodo_REV_078_01(t *testing.T) {
	tracker, err := BeginOffboarding(offboardingPlan(t), offboardExpected())
	if err != nil {
		t.Fatal(err)
	}
	completed, err := CompleteEmploymentWithRehire(tracker, date(t, "2026-03-01"), people.RehireConditional, people.RehireReasonAgreement, "authority:exit-decision-1")
	if err != nil {
		t.Fatalf("complete employment with rehire decision: %v", err)
	}
	if !completed.EmploymentCompleted || completed.RehireEligibility == nil {
		t.Fatal("exit completion did not capture rehire eligibility")
	}
	record := completed.RehireEligibility
	if record.Worker != tracker.Worker || record.Employment != tracker.Employment || record.Status != people.RehireConditional || record.Reason != people.RehireReasonAgreement || record.EffectiveAt.String() != "2026-03-01" {
		t.Fatalf("recorded rehire eligibility = %+v", record)
	}
	if err := completed.Validate(); err != nil {
		t.Fatalf("completed tracker invalid: %v", err)
	}
	if _, err := CompleteEmploymentWithRehire(tracker, date(t, "2026-03-01"), people.RehireNotEligible, people.RehireReasonGoodStanding, "authority:exit-decision-2"); err == nil {
		t.Fatal("status and reason mismatch accepted")
	}
	history, err := people.AppendRehireEligibility(people.RehireEligibilityHistory{}, *record)
	if err != nil {
		t.Fatal(err)
	}
	correction := *record
	correction.Status = people.RehireEligible
	correction.Reason = people.RehireReasonGoodStanding
	correction.Revision = 2
	history, err = people.AppendRehireEligibility(history, correction)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Records) != 2 || history.Records[0].Status != people.RehireConditional || history.Records[1].Status != people.RehireEligible {
		t.Fatalf("rehire history did not preserve both decisions: %+v", history.Records)
	}
	if _, err := people.AppendRehireEligibility(history, correction); err == nil {
		t.Fatal("duplicate revision replaced or duplicated the history head")
	}
}
