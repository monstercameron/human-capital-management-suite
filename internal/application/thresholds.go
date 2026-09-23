package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
)

// thresholdTableContractVersion is the served contract revision of the
// threshold table projection below. The rules release the served rows come
// from rides in ThresholdTable.VersionRef (RULE-003 table identity), so a
// new table release changes VersionRef, never this number; this number
// changes only when the projection itself does.
const thresholdTableContractVersion = 1

// servedThresholds resolves WorkService.GetThresholdTable through the
// reference promotion-approval decision table (RULE-003). P1A serves the
// one table compiled from tenant configuration; the reference table is
// what that resolution answers until a tenant publishes its own, so the
// read is implemented, not merely present in the contract.
type servedThresholds struct{}

// newServedThresholds composes the threshold table port the served
// WorkService reads through. It carries no state: the table is a compiled
// package constant in internal/engines/rules, and tenant scoping is the
// caller's tenant stamped onto the answer.
func newServedThresholds() transporthumanwork.Thresholds {
	return servedThresholds{}
}

// GetThresholdTable implements transporthumanwork.Thresholds.
func (servedThresholds) GetThresholdTable(_ context.Context, tenant, tableID string) (transporthumanwork.ThresholdTable, error) {
	table := rules.PromotionApprovalThresholdTable()
	if tableID != "" && tableID != table.ID {
		return transporthumanwork.ThresholdTable{}, transporthumanwork.ErrThresholdNotFound
	}
	projected, err := projectThresholdTable(table)
	if err != nil {
		return transporthumanwork.ThresholdTable{}, err
	}
	projected.TenantID = tenant
	return projected, nil
}

// projectThresholdTable renders the rules engine's table onto the port's
// read-only value. Conditions render as stable "OPERATOR(operand)" literals
// — the same tokens rules.Operator.String and rules.Value.String produce
// in traces — so a served row cites exactly what the engine would evaluate.
// Wildcard (ANY) conditions are omitted: a row of only wildcards is the
// "otherwise" row the wire represents with no conditions. Anything the
// projection cannot render faithfully — an unknown operator, a row whose
// outputs are not exactly one token — is an error rather than a served
// approximation, because a decision table that mislabels a row is worse
// than an unavailable read.
func projectThresholdTable(table rules.Table) (transporthumanwork.ThresholdTable, error) {
	out := transporthumanwork.ThresholdTable{
		TableID:    table.ID,
		Version:    thresholdTableContractVersion,
		VersionRef: table.Version,
	}
	switch table.HitPolicy {
	case rules.HitPolicyFirst:
		out.HitPolicy = transporthumanwork.ThresholdHitPolicyFirst
	case rules.HitPolicyUnique:
		out.HitPolicy = transporthumanwork.ThresholdHitPolicyUnique
	case rules.HitPolicyCollect:
		out.HitPolicy = transporthumanwork.ThresholdHitPolicyCollect
	default:
		return transporthumanwork.ThresholdTable{}, fmt.Errorf("application: threshold table %s@%s has an unknown hit policy", table.ID, table.Version)
	}
	for _, input := range table.Inputs {
		out.InputNames = append(out.InputNames, input.Name)
	}
	for _, row := range table.Rows {
		if len(row.Outputs) != 1 {
			return transporthumanwork.ThresholdTable{}, fmt.Errorf("application: threshold table %s@%s row %q has %d outputs, want exactly one outcome token", table.ID, table.Version, row.ID, len(row.Outputs))
		}
		if len(row.Conditions) != len(table.Inputs) {
			return transporthumanwork.ThresholdTable{}, fmt.Errorf("application: threshold table %s@%s row %q has %d conditions for %d inputs", table.ID, table.Version, row.ID, len(row.Conditions), len(table.Inputs))
		}
		projected := transporthumanwork.ThresholdRow{Outcome: row.Outputs[0].String()}
		for i, cond := range row.Conditions {
			if cond.Op == rules.OpAny {
				continue
			}
			rendered, err := renderThresholdCondition(cond)
			if err != nil {
				return transporthumanwork.ThresholdTable{}, fmt.Errorf("application: threshold table %s@%s row %q: %w", table.ID, table.Version, row.ID, err)
			}
			projected.Conditions = append(projected.Conditions, transporthumanwork.ThresholdCondition{
				InputName:  table.Inputs[i].Name,
				Comparison: rendered,
			})
		}
		out.Rows = append(out.Rows, projected)
	}
	return out, nil
}

// renderThresholdCondition renders one non-wildcard condition as a bounded
// literal such as "GREATER_THAN(20.0000)" or "BETWEEN(1, 3)".
func renderThresholdCondition(cond rules.Condition) (string, error) {
	switch cond.Op {
	case rules.OpEqual, rules.OpNotEqual,
		rules.OpLessThan, rules.OpLessOrEqual,
		rules.OpGreaterThan, rules.OpGreaterOrEqual:
		return cond.Op.String() + "(" + cond.Operand.String() + ")", nil
	case rules.OpIn, rules.OpNotIn:
		operands := make([]string, 0, len(cond.Operands))
		for _, operand := range cond.Operands {
			operands = append(operands, operand.String())
		}
		return cond.Op.String() + "(" + strings.Join(operands, ", ") + ")", nil
	case rules.OpBetween:
		return cond.Op.String() + "(" + cond.Low.String() + ", " + cond.High.String() + ")", nil
	default:
		return "", fmt.Errorf("unknown threshold operator %q", cond.Op.String())
	}
}
