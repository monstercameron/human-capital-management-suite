package roleaccess

import "strings"

// VisibilityEvaluator is the unit-scope portion of a resolved directory
// policy. It is deliberately separate from authentication, page grants,
// manager-chain authority and mandatory denies. A policy preview may reuse it
// only after those surrounding decisions are resolved by the server.
type VisibilityEvaluator struct {
	all       bool
	ownUnit   string
	allowOwn  bool
	allowed   map[string]bool
	denyLists []map[string]bool
}

// NewVisibilityEvaluator compiles additive role policies once for a viewer.
// Unknown modes grant nothing; an empty denylist grants every unit, matching
// the live directory's existing role-policy semantics.
func NewVisibilityEvaluator(policies []VisibilityPolicy, ownUnit string) VisibilityEvaluator {
	evaluator := VisibilityEvaluator{ownUnit: normalizedUnit(ownUnit), allowed: make(map[string]bool)}
	for _, policy := range policies {
		switch strings.ToUpper(strings.TrimSpace(policy.Mode)) {
		case VisibilityAll:
			evaluator.all = true
		case VisibilityOwnUnit:
			evaluator.allowOwn = true
		case VisibilityAllowlist:
			for _, unit := range policy.OrganizationUnits {
				if key := normalizedUnit(unit); key != "" {
					evaluator.allowed[key] = true
				}
			}
		case VisibilityDenylist:
			denied := make(map[string]bool, len(policy.OrganizationUnits))
			for _, unit := range policy.OrganizationUnits {
				if key := normalizedUnit(unit); key != "" {
					denied[key] = true
				}
			}
			evaluator.denyLists = append(evaluator.denyLists, denied)
		}
	}
	return evaluator
}

// Allows reports only whether the additive unit policies admit a worker. The
// viewer's own record remains discoverable even without a matching policy.
func (e VisibilityEvaluator) Allows(targetUnit string, self bool) bool {
	if self || e.all {
		return true
	}
	unit := normalizedUnit(targetUnit)
	if e.allowOwn && e.ownUnit != "" && unit == e.ownUnit || e.allowed[unit] {
		return true
	}
	for _, denied := range e.denyLists {
		if !denied[unit] {
			return true
		}
	}
	return false
}

func normalizedUnit(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
