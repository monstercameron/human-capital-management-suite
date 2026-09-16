package operator

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

// Family is the authority family one operator kind belongs to (WF-RUN-039).
//
// Before this vocabulary every material operator action drew on one
// undifferentiated pool of operator authority, so the person who approved an
// intervention could turn around and perform the repair, and a bypass in one
// area silently widened the blast radius of every other. A family is the unit
// suspension and separation of duties are measured in: an overdue bypass
// obligation suspends exactly its own family, and the repair families refuse
// an operator who is an approver of record on an outstanding obligation
// covering the same scope.
//
// The names match the capability families the governed workflow intervention
// surface publishes (workflow.instances.*, workflow.repair.*,
// workflow.override.decision), so an operator kind and the capability it
// admits are governed under one identifier.
type Family string

// The governed authority families. FamilyOperations is the fallback for the
// platform-wide kinds (failover, quarantine, key rotation, tenant suspension,
// diagnostic read) that belong to no workflow family.
const (
	// FamilyInstances is lifecycle control of a running instance: pause,
	// resume, cancel, retry. It changes what an instance does next; it never
	// rewrites what it already decided.
	FamilyInstances Family = "workflow.instances"
	// FamilyRepair is repair of durable execution state after a defect: a
	// database repair, a projection rebuild, a node intervention. It is the
	// operator kind an approver may not also perform.
	FamilyRepair Family = "workflow.repair"
	// FamilyOverride is overriding a recorded human decision. It is approval
	// authority, deliberately disjoint from repair authority.
	FamilyOverride Family = "workflow.override"
	// FamilyMigrate is moving pinned instances onto another plan or runtime
	// version. It carries repair authority's separation rule because it edits
	// what a running instance is pinned to.
	FamilyMigrate Family = "workflow.migrate"
	// FamilyOperations is everything outside the workflow families.
	FamilyOperations Family = "operations.general"
)

// Operator kinds this todo adds. They are distinct kinds, not modes of one
// kind: a JIT grant names the exact kind string it authorizes, so a grant for
// WORKFLOW_REPAIR does not authorize WORKFLOW_MIGRATE and neither authorizes
// WORKFLOW_OVERRIDE_DECISION.
const (
	// KindWorkflowRepair repairs durable execution state for named instances.
	KindWorkflowRepair Kind = "WORKFLOW_REPAIR"
	// KindWorkflowOverrideDecision overrides a recorded human decision.
	KindWorkflowOverrideDecision Kind = "WORKFLOW_OVERRIDE_DECISION"
	// KindWorkflowMigrate migrates instances onto another pinned version.
	KindWorkflowMigrate Kind = "WORKFLOW_MIGRATE"
)

// Families lists every family in a stable order.
func Families() []Family {
	return []Family{FamilyInstances, FamilyRepair, FamilyOverride, FamilyMigrate, FamilyOperations}
}

// familyByKind maps the kinds whose family is not the fallback. A kind absent
// from this table is FamilyOperations, so a kind added by another lane is
// governed the moment it exists rather than silently escaping the vocabulary.
var familyByKind = map[Kind]Family{
	KindWorkflowPause:            FamilyInstances,
	KindWorkflowResume:           FamilyInstances,
	KindWorkflowCancel:           FamilyInstances,
	KindWorkflowRetryNode:        FamilyInstances,
	KindWorkflowNodeIntervention: FamilyRepair,
	KindDatabaseRepair:           FamilyRepair,
	KindProjectionRebuild:        FamilyRepair,
	KindWorkflowRepair:           FamilyRepair,
	KindWorkflowOverrideDecision: FamilyOverride,
	KindWorkflowMigrate:          FamilyMigrate,
}

// Family reports the authority family k is governed under.
func (k Kind) Family() Family {
	if f, ok := familyByKind[k]; ok {
		return f
	}
	return FamilyOperations
}

// SeparatesRepairFromApproval reports whether k is a repair-authority kind,
// which the gateway refuses to an operator who is an approver of record on an
// outstanding obligation covering the same scope.
func (k Kind) SeparatesRepairFromApproval() bool {
	switch k.Family() {
	case FamilyRepair, FamilyMigrate:
		return true
	default:
		return false
	}
}

// authorityKinds lists the kinds declared in this file, in a stable order.
func authorityKinds() []Kind {
	return []Kind{KindWorkflowRepair, KindWorkflowOverrideDecision, KindWorkflowMigrate}
}

// authorityPolicyFor returns the policy of a kind declared in this file.
// [PolicyFor] consults it first, so the three families keep their policies
// next to their definitions instead of in one growing switch.
func authorityPolicyFor(k Kind) (Policy, bool) {
	p := Policy{IntentType: "hcmnext.operations." + strings.ToLower(string(k)) + ".v1", Material: true, DualControl: true}
	switch k {
	case KindWorkflowRepair:
		// Repair rewrites durable execution state: it is dry-run first and
		// needs a second person who is not the repair operator.
		p.Roles, p.SimulationRequired = []jit.Role{jit.RoleIntegrityRepair}, true
	case KindWorkflowMigrate:
		p.Roles, p.SimulationRequired = []jit.Role{jit.RoleIntegrityRepair}, true
	case KindWorkflowOverrideDecision:
		// Overriding a decision is approval authority, never repair authority:
		// the integrity-repair role does not authorize it.
		p.Roles = []jit.Role{jit.RoleIncidentResponder}
	default:
		return Policy{}, false
	}
	return p, true
}

// FamilyForCapability maps a published capability id to the authority family
// that governs it: the id is the family itself, or the family followed by a
// dot. It reports false for an id outside the governed families.
func FamilyForCapability(id string) (Family, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	for _, f := range Families() {
		if f == FamilyOperations {
			continue
		}
		if id == string(f) || strings.HasPrefix(id, string(f)+".") {
			return f, true
		}
	}
	return "", false
}
