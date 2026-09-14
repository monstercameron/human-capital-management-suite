package app

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// composeWorkflowControl builds EP-WF-002's governed workflow controls over
// the execution database: operator authority is the operator's current JIT
// grant in the durable trust store (an instance-scoped grant carries the
// dual-control second approval a cancel needs), a retry is dry-run first so
// its simulation evidence is sealed into the receipt, the pinned plan is
// resolved from the promotion plans this cell runs, and every control is
// journaled by the operator gateway. It returns nil controls, never an error, when the cell has
// no execution database or tenant mapping -- the transport then refuses
// controls with FAILED_PRECONDITION.
func composeWorkflowControl(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) (*workflowcontrol.Controller, workflowcontrol.TenantIDs, error) {
	if db == nil || tenantUUID == nil {
		return nil, nil, nil
	}
	ids := func(t values.TenantId) (uuid.UUID, error) {
		id := tenantUUID(t)
		if id == uuid.Nil {
			return uuid.Nil, fmt.Errorf("%w: tenant %s has no storage identity", workflowcontrol.ErrInvalidCommand, t)
		}
		return id, nil
	}
	plan, err := promotionexec.Compile()
	if err != nil {
		return nil, nil, fmt.Errorf("app: compile promotion plan for workflow control: %w", err)
	}
	simulation, err := promotionexec.CompileSimulation()
	if err != nil {
		return nil, nil, fmt.Errorf("app: compile promotion simulation plan for workflow control: %w", err)
	}
	authority := workflowcontrol.JITAuthority{Grants: truststore.New(db), TenantIDs: ids, Clock: now}
	ctrl, err := workflowcontrol.New(db, operator.NewMemoryJournal(), workflowcontrol.NewPlanSet(plan, simulation), authority, now,
		workflowcontrol.WithPreflightSimulation())
	if err != nil {
		return nil, nil, fmt.Errorf("app: compose workflow control: %w", err)
	}
	return ctrl, ids, nil
}
