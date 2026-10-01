package timeprofile

import "strings"

// PlanNode is the slice of a compiled plan node CheckPlan needs: its id and
// the capability classes it carries. A caller passes one PlanNode per node
// of the fully expanded plan, tenant overlay included, so a class an overlay
// added is checked exactly like one the base template declared.
type PlanNode struct {
	NodeID            string
	CapabilityClasses []string
}

// PlanConstraints is the required and forbidden capability-class policy for
// one profile category. It is data, not code: a new category's policy is a
// new value returned by ConstraintsFor, never a new branch at the call site.
type PlanConstraints struct {
	Category         WorkerCategory
	RequiredClasses  []string
	ForbiddenClasses []string
}

// ConstraintsFor derives the policy WTIME-006 requires from a valid profile.
// Contractor plans forbid every ControlClass in vocabulary.go, because each
// one is a control indicator under the ABC test, IR35 or the EU Platform
// Work Directive. A minor profile requires the minor-hours decision. A
// profile in a rest-duty or daily-recording-duty jurisdiction requires the
// matching decision, independent of category.
func ConstraintsFor(p TimeProfile) PlanConstraints {
	c := PlanConstraints{Category: p.Category}
	if p.Category == CategoryContractor {
		for _, class := range ControlClasses() {
			c.ForbiddenClasses = append(c.ForbiddenClasses, string(class))
		}
	}
	if p.MinorAgeBand.IsMinor() {
		c.RequiredClasses = append(c.RequiredClasses, string(DecisionMinorHours))
	}
	if p.RestPeriodDutyRequired {
		c.RequiredClasses = append(c.RequiredClasses, string(DecisionRestPeriod))
	}
	if p.DailyRecordingDutyRequired {
		c.RequiredClasses = append(c.RequiredClasses, string(DecisionDailyRecording))
	}
	return c
}

// CheckPlan enforces ConstraintsFor(profile) against the expanded plan:
// every node's capability classes must avoid the forbidden set, and the plan
// as a whole must carry every required class on at least one node. It
// returns a *PlanConstraintViolation naming the offending node for a
// forbidden class, or naming the missing class with no node when a required
// class is absent from every node. Matching is exact-string: a class is
// checked as the token it is, never fuzzed or case-folded, so a lookalike
// token can never stand in for the real one on either side of the check.
func CheckPlan(p TimeProfile, planNodes []PlanNode) error {
	if err := p.Validate(); err != nil {
		return err
	}
	constraints := ConstraintsFor(p)

	forbidden := make(map[string]struct{}, len(constraints.ForbiddenClasses))
	for _, class := range constraints.ForbiddenClasses {
		forbidden[class] = struct{}{}
	}

	present := make(map[string]struct{})
	for _, node := range planNodes {
		for _, class := range node.CapabilityClasses {
			if strings.TrimSpace(class) == "" {
				continue
			}
			present[class] = struct{}{}
			if _, isForbidden := forbidden[class]; isForbidden {
				return forbiddenClassViolation(node.NodeID, class)
			}
		}
	}

	for _, required := range constraints.RequiredClasses {
		if _, ok := present[required]; !ok {
			return missingRequiredClassViolation(required)
		}
	}
	return nil
}
