package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// promotionThresholdRuleRef is the rule reference the Promotion workflows'
// raise-threshold DECISION declares (internal/workflow's reference definition
// and internal/platform/execution/promotionsteps' output digest both name it).
const promotionThresholdRuleRef = "rules.compensation.raise_threshold/v3"

// RuleTableBinding is one decision table version a [RulesDecisionCandidate]
// can evaluate, and how its result maps onto a node's declared routes.
type RuleTableBinding struct {
	// RuleRef is the compiled DECISION's rule reference this table answers.
	RuleRef string
	// Table is the table at exactly one version. A pinned version whose id,
	// version or digest differs is not this binding.
	Table rules.Table
	// DecimalScale is the scale decimal inputs are parsed at; a recorded value
	// that does not fit exactly is refused rather than rounded.
	DecimalScale int32
	// Routes maps the matched row's first output value to candidate route
	// keys; the first one the node declares is taken.
	Routes map[string][]string
}

// PromotionThresholdBinding is the reference Promotion approval-threshold
// table (internal/engines/rules) bound to the raise-threshold DECISION. The
// route mapping is the one internal/platform/execution/promotionsteps applies
// in production: a standard tier stays within the threshold, a finance or
// executive tier exceeds it, and an unresolved input is UNKNOWN.
func PromotionThresholdBinding() RuleTableBinding {
	exceeds := []string{"EXCEEDS_THRESHOLD", "ABOVE_THRESHOLD"}
	return RuleTableBinding{
		RuleRef:      promotionThresholdRuleRef,
		Table:        rules.PromotionApprovalThresholdTable(),
		DecimalScale: rules.IncreasePercentScale,
		Routes: map[string][]string{
			string(rules.ApprovalTierStandard):          {"WITHIN_THRESHOLD"},
			string(rules.ApprovalTierFinanceRequired):   exceeds,
			string(rules.ApprovalTierExecutiveRequired): exceeds,
			string(rules.ApprovalTierUnknownBlocked):    {string(workflow.OutcomeUnknown)},
		},
	}
}

// RulesDecisionCandidate recomputes a DECISION node by evaluating the rule
// table version the historical attempt pinned against the inputs it recorded.
type RulesDecisionCandidate struct {
	Bindings []RuleTableBinding
}

var _ Candidate = RulesDecisionCandidate{}

// Recompute implements [Candidate].
func (c RulesDecisionCandidate) Recompute(ctx context.Context, req CandidateRequest) (ret0 CandidateOutcome, retErr error) {
	_, obsOp := observe.Begin(ctx, "workflow.replay.recompute_rule_decision", req.Node.ID, req.Attempt)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	node := req.Node
	if node.Type != workflow.StepDecision || node.Decision == nil {
		return CandidateOutcome{}, refuse(CodeCandidateFailed, node.ID, "a rule-table candidate recomputes DECISION nodes only, not %s", node.Type)
	}
	pinned, ok := req.Inputs.Version(runtime.PinnedVersionRuleTable)
	if !ok {
		return CandidateOutcome{}, refuse(CodeArtifactUnavailable, node.ID,
			"attempt %d recorded no pinned rule table version", req.Attempt)
	}
	binding, err := c.binding(node, pinned)
	if err != nil {
		return CandidateOutcome{}, err
	}

	inputs := make(map[string]rules.Value, len(binding.Table.Inputs))
	for _, col := range binding.Table.Inputs {
		text, ok := req.Inputs.Input(col.Name)
		if !ok {
			return CandidateOutcome{}, refuse(CodeArtifactUnavailable, node.ID,
				"attempt %d recorded no value for rule input %q", req.Attempt, col.Name)
		}
		v, err := parseRuleValue(col.Kind, text, binding.DecimalScale)
		if err != nil {
			return CandidateOutcome{}, wrap(CodeRecordInvalid, node.ID, err, "recorded rule input %q", col.Name)
		}
		inputs[col.Name] = v
	}
	result, err := rules.Evaluate(binding.Table, inputs)
	if err != nil {
		return CandidateOutcome{}, wrap(CodeCandidateFailed, node.ID, err, "evaluate %s@%s", pinned.Ref, pinned.Version)
	}

	value, row := string(rules.ApprovalTierUnknownBlocked), ""
	if result.Status == rules.StatusMatched && len(result.Matches[0].Outputs) > 0 {
		value, row = result.Matches[0].Outputs[0].String(), result.Matches[0].RowID
	}
	route := string(workflow.OutcomeUnknown)
	for _, key := range binding.Routes[value] {
		if declaresRoute(node, key) {
			route = key
			break
		}
	}
	return CandidateOutcome{
		RouteKey:     route,
		OutputDigest: ruleDecisionDigest(node.Decision.RuleRef, result.TableDigest, row, value),
	}, nil
}

// binding finds the table version the attempt pinned and proves it is the
// same content, by digest, that the attempt was evaluated against.
func (c RulesDecisionCandidate) binding(node workflow.CompiledNode, pinned runtime.PinnedArtifactVersion) (RuleTableBinding, error) {
	for _, b := range c.Bindings {
		if b.RuleRef != node.Decision.RuleRef || b.Table.ID != pinned.Ref || b.Table.Version != pinned.Version {
			continue
		}
		digest, err := b.Table.Digest()
		if err != nil {
			return RuleTableBinding{}, wrap(CodeCandidateFailed, node.ID, err, "digest %s@%s", pinned.Ref, pinned.Version)
		}
		if digest != pinned.Digest {
			return RuleTableBinding{}, refuse(CodeArtifactUnavailable, node.ID,
				"pinned rule table %s@%s digests to %s; the version available digests to %s",
				pinned.Ref, pinned.Version, pinned.Digest, digest)
		}
		return b, nil
	}
	return RuleTableBinding{}, refuse(CodeArtifactUnavailable, node.ID,
		"no rule table %s@%s for rule %s is available to recompute this decision",
		pinned.Ref, pinned.Version, node.Decision.RuleRef)
}

func declaresRoute(node workflow.CompiledNode, key string) bool {
	for _, r := range node.Routes {
		if r == key {
			return true
		}
	}
	return false
}

// parseRuleValue reads a recorded input as its table column's kind, exactly.
func parseRuleValue(kind rules.Kind, text string, scale int32) (rules.Value, error) {
	switch kind {
	case rules.KindDecimal:
		d, err := values.NewDecimal(text, scale, values.RoundingExactRequired)
		if err != nil {
			return rules.Value{}, err
		}
		return rules.DecimalValue(d), nil
	case rules.KindBool:
		b, err := strconv.ParseBool(text)
		if err != nil || (text != "true" && text != "false") {
			return rules.Value{}, refuse(CodeRecordInvalid, "", "%q is not a canonical boolean", text)
		}
		return rules.BoolValue(b), nil
	case rules.KindInt:
		i, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return rules.Value{}, err
		}
		return rules.IntValue(i), nil
	default:
		return rules.StringValue(text), nil
	}
}

// ruleDecisionDigest is the DECISION output digest the production threshold
// port (internal/platform/execution/promotionsteps.RulesThresholdPort)
// records: sha256 over rule ref, table digest, matched row and result, each
// NUL-terminated. Recomputing it with the same formula is what lets a replay
// compare against a real recorded run rather than only against itself.
func ruleDecisionDigest(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
