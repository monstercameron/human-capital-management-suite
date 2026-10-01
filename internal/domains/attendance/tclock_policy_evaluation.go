package attendance

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

// PolicyInterval binds one raw worked interval to the immutable policy and
// shift that govern its evaluated result. Raw attendance punches remain the
// input to Evaluate; this bridge never rewrites them.
type PolicyInterval struct {
	ShiftID      string
	Raw          punchpolicy.Interval
	Policy       punchpolicy.Policy
	Shift        punchpolicy.Shift
	Attestations []punchpolicy.Attestation
}

// PolicyEvaluationRequest combines the existing attendance request with the
// policy evaluations required for its worked intervals. Policy computation is
// delegated to punchpolicy.Evaluate exactly once per interval.
type PolicyEvaluationRequest struct {
	Attendance Request
	Intervals  []PolicyInterval
}

// PolicyEvaluation is one policy-pinned result keyed to its attendance shift.
type PolicyEvaluation struct {
	ShiftID string
	Result  punchpolicy.EvaluatedInterval
}

// PolicyEvaluationResult preserves the existing attendance result and adds
// policy-pinned evaluated intervals. Consumers can use attendance exceptions
// for compliance and the evaluated intervals for paid-time calculation.
type PolicyEvaluationResult struct {
	Attendance  Result
	Evaluations []PolicyEvaluation
}

// EvaluateWithPunchPolicies evaluates attendance from the original request,
// then evaluates each explicitly supplied raw interval through punchpolicy.
// The two domains keep their distinct responsibilities: attendance determines
// schedule compliance, while punchpolicy determines rounding and deductions.
func EvaluateWithPunchPolicies(req PolicyEvaluationRequest) (PolicyEvaluationResult, error) {
	if err := validatePolicyIntervals(req); err != nil {
		return PolicyEvaluationResult{}, err
	}
	attendanceResult, err := Evaluate(req.Attendance)
	if err != nil {
		return PolicyEvaluationResult{}, err
	}
	out := PolicyEvaluationResult{
		Attendance:  attendanceResult,
		Evaluations: make([]PolicyEvaluation, 0, len(req.Intervals)),
	}
	for _, interval := range req.Intervals {
		evaluated, err := punchpolicy.Evaluate(interval.Policy, interval.Raw, interval.Shift, interval.Attestations)
		if err != nil {
			return PolicyEvaluationResult{}, fmt.Errorf("attendance: evaluate policy for shift %q: %w", interval.ShiftID, err)
		}
		out.Evaluations = append(out.Evaluations, PolicyEvaluation{ShiftID: interval.ShiftID, Result: evaluated})
	}
	return out, nil
}

func validatePolicyIntervals(req PolicyEvaluationRequest) error {
	shiftIDs := make(map[string]bool, len(req.Attendance.Schedule.Shifts))
	for _, shift := range req.Attendance.Schedule.Shifts {
		shiftIDs[shift.ID] = true
	}
	seen := make(map[string]bool, len(req.Intervals))
	for _, interval := range req.Intervals {
		if interval.ShiftID == "" || !shiftIDs[interval.ShiftID] {
			return fmt.Errorf("attendance: policy interval names unknown shift %q", interval.ShiftID)
		}
		if seen[interval.ShiftID] {
			return fmt.Errorf("attendance: duplicate policy interval for shift %q", interval.ShiftID)
		}
		seen[interval.ShiftID] = true
		if interval.Shift.ID != interval.ShiftID {
			return fmt.Errorf("attendance: policy shift id %q does not match interval shift %q", interval.Shift.ID, interval.ShiftID)
		}
	}
	return nil
}
