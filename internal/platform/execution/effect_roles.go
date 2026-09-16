package execution

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// composedEffectRoles is the served driver's WF-RUN-037 policy: a failed
// DOWNSTREAM_EFFECT or DERIVED_UPDATE node lands a durable
// runtime.EffectRoleSettlementStore row against the committed core and takes
// its compiled failure route, instead of aborting the run.
func composedEffectRoles() *execute.EffectRolePolicy {
	return &execute.EffectRolePolicy{Settlements: runtime.EffectRoleSettlementStore{}}
}
