package idempotency

import (
	"strings"
)

// CapabilityRetentionRegistry is immutable after construction and is the
// authority for capability-specific post-expiry behavior. Exact entries take
// precedence over namespace entries; protected capability names may only be
// mapped to permanent tombstones.
type CapabilityRetentionRegistry struct {
	exact    map[string]RetentionClass
	prefixes map[string]RetentionClass
}

// NewCapabilityRetentionRegistry builds an immutable registry from exact
// capability IDs and namespace prefixes. It copies both maps so later caller
// mutation cannot change policy after composition.
func NewCapabilityRetentionRegistry(
	exact map[string]RetentionClass,
	prefixes map[string]RetentionClass,
) (CapabilityRetentionRegistry, error) {
	registry := CapabilityRetentionRegistry{
		exact:    make(map[string]RetentionClass, len(exact)),
		prefixes: make(map[string]RetentionClass, len(prefixes)),
	}
	for capability, class := range exact {
		if strings.TrimSpace(capability) == "" || !validRetentionClass(class) {
			return CapabilityRetentionRegistry{}, &Error{Code: CodeInvalidRecord, Detail: "retention registry contains an invalid capability or class"}
		}
		if protectedCapabilityName(capability) && class != RetentionPermanentTombstone {
			return CapabilityRetentionRegistry{}, &Error{Code: CodeRetentionPolicyConflict, Scope: Scope{Capability: capability}, Detail: "financial, government, and irreversible capabilities require permanent tombstones"}
		}
		registry.exact[capability] = class
	}
	for prefix, class := range prefixes {
		if !strings.HasSuffix(prefix, ".") || !validRetentionClass(class) {
			return CapabilityRetentionRegistry{}, &Error{Code: CodeInvalidRecord, Detail: "retention registry prefixes must end in a dot and use a valid class"}
		}
		if protectedCapabilityName(prefix) && class != RetentionPermanentTombstone {
			return CapabilityRetentionRegistry{}, &Error{Code: CodeRetentionPolicyConflict, Scope: Scope{Capability: prefix}, Detail: "financial, government, and irreversible capability namespaces require permanent tombstones"}
		}
		registry.prefixes[prefix] = class
	}
	return registry, nil
}

// DefaultCapabilityRetentionRegistry declares permanent retention for the
// canonical payroll, tax, financial and government capability namespaces.
// Additional protected capability names fail closed until registered.
func DefaultCapabilityRetentionRegistry() CapabilityRetentionRegistry {
	registry, _ := NewCapabilityRetentionRegistry(map[string]RetentionClass{
		"intent:hcmnext.mobility.mobility_tax_assess":                      RetentionPermanentTombstone,
		"intent:hcmnext.employee_self_service_portal.benefits_self_enroll": RetentionPermanentTombstone,
		"transaction.commit": RetentionPermanentTombstone,
	}, map[string]RetentionClass{
		"intent:hcmnext.payroll.":      RetentionPermanentTombstone,
		"hcmnext.payroll.":             RetentionPermanentTombstone,
		"payroll.":                     RetentionPermanentTombstone,
		"intent:hcmnext.rewards.":      RetentionPermanentTombstone,
		"intent:hcmnext.benefits.":     RetentionPermanentTombstone,
		"intent:hcmnext.regulatory.":   RetentionPermanentTombstone,
		"hcmnext.regulatory.":          RetentionPermanentTombstone,
		"intent:hcmnext.finance.":      RetentionPermanentTombstone,
		"intent:hcmnext.financial.":    RetentionPermanentTombstone,
		"intent:hcmnext.payment.":      RetentionPermanentTombstone,
		"intent:hcmnext.government.":   RetentionPermanentTombstone,
		"intent:hcmnext.irreversible.": RetentionPermanentTombstone,
		"hcmnext.workflows.":           RetentionPermanentTombstone,
		"people.workflows.":            RetentionPermanentTombstone,
		"fixture.workflows.":           RetentionPermanentTombstone,
		"customer.workflow.":           RetentionPermanentTombstone,
		"workflow.":                    RetentionPermanentTombstone,
		"promotion.":                   RetentionPermanentTombstone,
		"hcmnext.finance.":             RetentionPermanentTombstone,
		"hcmnext.financial.":           RetentionPermanentTombstone,
		"hcmnext.payment.":             RetentionPermanentTombstone,
		"hcmnext.government.":          RetentionPermanentTombstone,
		"hcmnext.irreversible.":        RetentionPermanentTombstone,
		"finance.":                     RetentionPermanentTombstone,
		"financial.":                   RetentionPermanentTombstone,
		"payment.":                     RetentionPermanentTombstone,
		"government.":                  RetentionPermanentTombstone,
		"regulatory.":                  RetentionPermanentTombstone,
		"irreversible.":                RetentionPermanentTombstone,
		"rewards.":                     RetentionPermanentTombstone,
		"benefits.":                    RetentionPermanentTombstone,
	})
	return registry
}

func (r CapabilityRetentionRegistry) classFor(capability string) (RetentionClass, bool) {
	if class, ok := r.exact[capability]; ok {
		return class, true
	}
	longest := ""
	var class RetentionClass
	for prefix, candidate := range r.prefixes {
		if strings.HasPrefix(capability, prefix) && len(prefix) > len(longest) {
			longest = prefix
			class = candidate
		}
	}
	return class, longest != ""
}

func validRetentionClass(class RetentionClass) bool {
	return class == RetentionExpiring || class == RetentionPermanentTombstone
}

// protectedCapabilityName catches high-risk capability identifiers that
// have not yet been entered into a registry. Such names fail closed instead
// of silently inheriting ordinary expiry.
func protectedCapabilityName(capability string) bool {
	name := strings.ToLower(strings.TrimSpace(capability))
	for _, marker := range []string{
		"payroll", "financial", "finance", "payment", "government", "regulatory", "transaction.commit",
		"tax_", "tax.", "irreversible", "compensation", "base_pay", "salary", "wage",
		"budget", "rewards", "benefits", "workflow",
	} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}
