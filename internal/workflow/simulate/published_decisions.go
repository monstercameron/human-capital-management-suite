package simulate

import (
	"context"
	"fmt"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/rulepayload"
)

// PublishedRuleResolver resolves a published executable by exact reference
// and content digest. Implementations must never substitute a latest version.
type PublishedRuleResolver interface {
	Resolve(workflow.Reference, string) (rulepayload.Payload, error)
}

// PublishedRuleDecisions evaluates a table or bounded expression resolved
// from the compiled node's immutable reference pin.
type PublishedRuleDecisions struct{ Payloads PublishedRuleResolver }

func (d PublishedRuleDecisions) Decide(ctx context.Context, req DecisionRequest) (result DecisionResult, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.simulate.published_rule_decide", req)
	defer func() { observe.Done(op, retErr) }()
	pin := req.Decision.Rule
	if d.Payloads == nil || pin == nil || pin.Kind != workflow.RefRule || pin.Status == workflow.ReferenceRetired || pin.Digest == "" {
		return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "DECISION has no published rule payload resolver or exact rule pin")
	}
	ref := workflow.Reference{Kind: pin.Kind, ID: pin.ID, Version: pin.Version}
	if req.Decision.RuleRef != "" && req.Decision.RuleRef != ref.ID {
		return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "compiled rule reference %q disagrees with its pin %q", req.Decision.RuleRef, ref.ID)
	}
	payload, err := d.Payloads.Resolve(ref, pin.Digest)
	if err != nil {
		return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "resolve published rule")
	}
	var route, trace, detail string
	switch payload.Kind {
	case rulepayload.KindDecisionTable:
		inputs, err := tableInputs(payload.Table, req.Inputs)
		if err != nil {
			return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "map decision-table inputs")
		}
		result, err := rules.Evaluate(*payload.Table, inputs)
		if err != nil {
			return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "evaluate decision table")
		}
		if result.Status == rules.StatusUnknown {
			return DecisionResult{RouteKey: string(workflow.OutcomeUnknown), TraceRef: result.TableID + "@" + result.TableVersion + "#UNKNOWN", Detail: "no published rule row matched"}, nil
		}
		if result.Status != rules.StatusMatched || len(result.Matches) != 1 || len(result.Matches[0].Outputs) == 0 {
			return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "decision table result is not one typed route")
		}
		outputIndex := -1
		for i, output := range payload.Table.Outputs {
			if output.Name == "route_key" {
				if outputIndex >= 0 {
					return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "decision table declares multiple route_key outputs")
				}
				outputIndex = i
			}
		}
		if outputIndex < 0 || outputIndex >= len(result.Matches[0].Outputs) {
			return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "decision table route_key output is absent")
		}
		value := result.Matches[0].Outputs[outputIndex]
		if value.Kind() != rules.KindString {
			return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "decision table output must be STRING route key, got %s", value.Kind())
		}
		route = value.String()
		trace = result.TableID + "@" + result.TableVersion + "#" + result.Matches[0].RowID
		detail = "published table " + trace
	case rulepayload.KindExpression:
		inputs, err := expressionInputs(payload.Expression, req.Inputs)
		if err != nil {
			return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "map expression inputs")
		}
		result, err := payload.Expression.Evaluate(inputs)
		if err != nil {
			return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "evaluate published expression")
		}
		if result.State == rules.EvalStateUnknown {
			return DecisionResult{RouteKey: string(workflow.OutcomeUnknown), TraceRef: result.Digest + "#UNKNOWN", Detail: "published expression returned UNKNOWN"}, nil
		}
		if result.State != rules.EvalStatePresent || result.Value.Kind() != rules.KindString {
			return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "decision expression must return a present STRING route key")
		}
		route = result.Value.String()
		trace = result.Digest
		detail = "published expression " + result.Digest + " evaluated in " + strconv.Itoa(len(result.Steps)) + " step(s)"
	default:
		return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "payload kind %q cannot evaluate a DECISION", payload.Kind)
	}
	if route != string(workflow.OutcomeUnknown) && !routeDeclared(req.Decision.Routes, route) {
		return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID, "published rule selected undeclared route %q", route)
	}
	return DecisionResult{RouteKey: route, TraceRef: trace, Detail: detail}, nil
}

func routeDeclared(routes []workflow.DecisionRoute, key string) bool {
	for _, route := range routes {
		if route.Key == key {
			return true
		}
	}
	return false
}

func tableInputs(table *rules.Table, bag Bag) (map[string]rules.Value, error) {
	if table == nil {
		return nil, fmt.Errorf("decision table body is absent")
	}
	inputs := make(map[string]rules.Value, len(table.Inputs))
	for _, column := range table.Inputs {
		value, err := bag.Get(column.Name)
		if err != nil {
			return nil, err
		}
		converted, err := toRuleValue(value, column.Kind)
		if err != nil {
			return nil, fmt.Errorf("input %s: %w", column.Name, err)
		}
		inputs[column.Name] = converted
	}
	return inputs, nil
}

func expressionInputs(expression *rules.CompiledExpression, bag Bag) (map[string]rules.Value, error) {
	if expression == nil {
		return nil, fmt.Errorf("expression body is absent")
	}
	inputs := make(map[string]rules.Value, len(expression.Inputs))
	for _, declaration := range expression.Inputs {
		value, err := bag.Get(declaration.Name)
		if err != nil {
			return nil, err
		}
		kind := expressionKind(declaration.Type)
		converted, err := toRuleValue(value, kind)
		if err != nil {
			return nil, fmt.Errorf("input %s: %w", declaration.Name, err)
		}
		inputs[declaration.Name] = converted
	}
	return inputs, nil
}

func expressionKind(kind rules.ExpressionType) rules.Kind {
	switch kind {
	case rules.ExpressionTypeBool:
		return rules.KindBool
	case rules.ExpressionTypeInt:
		return rules.KindInt
	case rules.ExpressionTypeDecimal:
		return rules.KindDecimal
	case rules.ExpressionTypeString, rules.ExpressionTypeDate:
		return rules.KindString
	case rules.ExpressionTypeList:
		return rules.KindList
	default:
		return rules.KindUnspecified
	}
}

func toRuleValue(value Value, kind rules.Kind) (rules.Value, error) {
	switch kind {
	case rules.KindString:
		if value.Type.Kind != workflow.KindString && value.Type.Kind != workflow.KindEnum && value.Type.Kind != workflow.KindLocalDate {
			break
		}
		return rules.StringValue(value.Text), nil
	case rules.KindBool:
		if value.Type.Kind != workflow.KindBool {
			break
		}
		b, err := value.Bool()
		if err != nil {
			return rules.Value{}, err
		}
		return rules.BoolValue(b), nil
	case rules.KindDecimal:
		if value.Type.Kind != workflow.KindDecimal {
			break
		}
		d, err := value.Decimal()
		if err != nil {
			return rules.Value{}, err
		}
		return rules.DecimalValue(d), nil
	case rules.KindInt:
		if value.Type.Kind != workflow.KindInteger {
			break
		}
		i, err := strconv.ParseInt(value.Text, 10, 64)
		if err != nil {
			return rules.Value{}, err
		}
		return rules.IntValue(i), nil
	case rules.KindList:
		return rules.Value{}, fmt.Errorf("list inputs require the expression list value codec")
	}
	return rules.Value{}, fmt.Errorf("workflow value %s cannot be read as rules kind %s", value.Type, kind)
}
