package timeclock

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// ErrUnmanifestedNode means a compiled node binds a capability the time
// manifests do not declare, so its control classes are unknown. Unknown is
// refused, never read as "no classes".
var ErrUnmanifestedNode = errors.New("time workflow: node binds a capability with no time manifest")

// NodeClasses returns the policy classes one compiled node carries, read
// from digest-material facts only: a CAPABILITY or OBSERVE node's capability
// id, a DECISION's rule_ref and a TASK's form (its output schema). Node
// metadata is deliberately ignored because it is outside the plan digest.
func NodeClasses(node workflow.CompiledNode) ([]string, error) {
	switch {
	case node.Capability != nil:
		m, ok := ManifestFor(node.Capability.ID)
		if !ok {
			return nil, fmt.Errorf("%w: node %s capability %s", ErrUnmanifestedNode, node.ID, node.Capability.ID)
		}
		return append([]string(nil), m.Classes...), nil
	case node.Decision != nil:
		classes, ok := RuleClasses()[node.Decision.RuleRef]
		if !ok {
			return nil, fmt.Errorf("%w: node %s rule %s", ErrUnmanifestedNode, node.ID, node.Decision.RuleRef)
		}
		return append([]string(nil), classes...), nil
	case node.Type == workflow.StepTask:
		form := strings.TrimSuffix(node.OutputSchema.SchemaID, "/v1")
		classes, ok := FormClasses()[form]
		if !ok {
			return nil, fmt.Errorf("%w: node %s form %s", ErrUnmanifestedNode, node.ID, form)
		}
		return append([]string(nil), classes...), nil
	default:
		return nil, nil
	}
}

// PlanNodes projects compiled plans onto the slice timeprofile.CheckPlan
// evaluates. Node ids are qualified by workflow id when more than one plan is
// given, so a violation names exactly which plan's node offended.
func PlanNodes(plans ...*workflow.CompiledWorkflow) ([]timeprofile.PlanNode, error) {
	var out []timeprofile.PlanNode
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		for _, node := range plan.Nodes {
			cls, err := NodeClasses(node)
			if err != nil {
				return nil, err
			}
			id := node.ID
			if len(plans) > 1 {
				id = plan.WorkflowID + "#" + node.ID
			}
			out = append(out, timeprofile.PlanNode{NodeID: id, CapabilityClasses: cls})
		}
	}
	return out, nil
}

// CheckCompiled evaluates timeprofile.CheckPlan on the expanded, compiled
// plans a profile's time runs through (WTIME-006 engine half): the capture
// plan alone, or the capture plan together with the period timecard. It runs
// on compiled plans, tenant overlays included, never on the template that
// produced them, so a control class an overlay added is caught at publish.
// A violation is a *timeprofile.PlanConstraintViolation naming the node.
func CheckCompiled(profile timeprofile.TimeProfile, plans ...*workflow.CompiledWorkflow) error {
	if len(plans) == 0 {
		return fmt.Errorf("%w: no compiled plan to check", timeprofile.ErrPlanConstraintViolated)
	}
	validPlan := false
	for _, plan := range plans {
		if plan != nil {
			validPlan = true
			break
		}
	}
	if !validPlan {
		return fmt.Errorf("%w: no compiled plan to check", timeprofile.ErrPlanConstraintViolated)
	}
	nodes, err := PlanNodes(plans...)
	if err != nil {
		return err
	}
	return timeprofile.CheckPlan(profile, nodes)
}
