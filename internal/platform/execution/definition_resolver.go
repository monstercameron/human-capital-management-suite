package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TenantDefinitionStore resolves one immutable tenant-authored definition and
// the ACTIVE compiled plan it was published from.
type TenantDefinitionStore interface {
	ResolveActiveDefinition(context.Context, uuid.UUID, string, map[string]string, time.Time) (workflowversionstore.ActiveDefinition, bool, error)
}

// DefinitionFactSource derives selector facts from the caller's trusted,
// immutable start source. Implementations must not use request-supplied
// material outside its verified proposal or trigger record.
type DefinitionFactSource interface {
	Facts(context.Context, runtime.StartRequest) (map[string]string, error)
}

// DefinitionFactsFunc adapts an application-owned, source-verifying selector
// fact function to DefinitionFactSource.
type DefinitionFactsFunc func(context.Context, runtime.StartRequest) (map[string]string, error)

// Facts implements DefinitionFactSource.
func (f DefinitionFactsFunc) Facts(ctx context.Context, req runtime.StartRequest) (map[string]string, error) {
	return f(ctx, req)
}

// ActiveDefinitionResolver resolves new starts against the tenant's active
// authoring pointer and the exact compiled digest linked to that revision.
// Existing pinned instances remain on their digest through their own source.
type ActiveDefinitionResolver struct {
	Definitions TenantDefinitionStore
	Facts       DefinitionFactSource
}

// ResolveWorkflow implements runtime.WorkflowResolver.
func (r ActiveDefinitionResolver) ResolveWorkflow(ctx context.Context, req runtime.StartRequest) (runtime.WorkflowSelection, error) {
	if r.Definitions == nil || r.Facts == nil || req.TenantID == uuid.Nil || req.CreatedAt.IsZero() {
		return runtime.WorkflowSelection{}, errors.New("execution: tenant definition resolver is incomplete")
	}
	source := req.StartSourceValue()
	intentType := strings.TrimSpace(source.IntentType)
	if intentType == "" {
		return runtime.WorkflowSelection{}, errors.New("execution: start source has no trusted intent type")
	}
	facts, err := r.Facts.Facts(ctx, req)
	if err != nil {
		return runtime.WorkflowSelection{}, fmt.Errorf("execution: resolve workflow match facts: %w", err)
	}
	selected, found, err := r.Definitions.ResolveActiveDefinition(ctx, req.TenantID, intentType, facts, req.CreatedAt.UTC())
	if err != nil {
		return runtime.WorkflowSelection{}, err
	}
	if !found {
		return runtime.WorkflowSelection{}, fmt.Errorf("execution: no ACTIVE workflow matches tenant %s and intent type %s", req.TenantID, intentType)
	}
	planDigest := selected.Compiled.CompiledPlanDigest
	workflowID := selected.Definition.WorkflowID
	if selected.Plan == nil || workflowID == "" || planDigest == "" || selected.Plan.Digest() != planDigest {
		return runtime.WorkflowSelection{}, errors.New("execution: tenant definition store returned an invalid compiled selection")
	}
	return runtime.WorkflowSelection{WorkflowID: workflowID, Plan: selected.Plan, Pin: version.Pin{CompiledPlanDigest: planDigest}}, nil
}
