package people

import (
	"reflect"
	"testing"
)

func TestTodo_REV_078_01_Golden(t *testing.T) {
	statuses := []RehireStatus{RehireEligible, RehireNotEligible, RehireConditional}
	wantStatuses := []RehireStatus{"ELIGIBLE", "NOT_ELIGIBLE", "CONDITIONAL"}
	if !reflect.DeepEqual(statuses, wantStatuses) {
		t.Fatalf("rehire statuses = %v, want %v", statuses, wantStatuses)
	}
	reasons := []RehireReason{RehireReasonGoodStanding, RehireReasonReduction, RehireReasonPerformance, RehireReasonMisconduct, RehireReasonPolicy, RehireReasonAgreement}
	wantReasons := []RehireReason{"GOOD_STANDING", "REDUCTION_IN_FORCE", "PERFORMANCE", "MISCONDUCT", "POLICY_REVIEW", "AGREEMENT_REQUIRED"}
	if !reflect.DeepEqual(reasons, wantReasons) {
		t.Fatalf("rehire reason codes = %v, want %v", reasons, wantReasons)
	}
}
