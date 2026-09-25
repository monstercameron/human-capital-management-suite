package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/workorder"
)

// ErrWorkOrderCompletionProof means that an order has no matching, durable
// successful node execution for the pinned workflow instance.
var ErrWorkOrderCompletionProof = errors.New("work order: workflow completion proof unavailable")

// WorkOrderRegistrationSpec describes a candidate executable registration.
// Match must be supplied by the authority that resolves the current intent
// and exact template pin. This constructor never admits the intent into a
// served ExecutionAuthority.
type WorkOrderRegistrationSpec struct {
	Template workorder.Template
	Pin      workorder.TemplatePin
	Match    func(runtime.StartRequest) bool
}

// CandidateWorkOrderRegistration compiles one exact, published work-order
// template pin into the generic workflow registration shape. It is
// intentionally candidate-only: no intent types are admitted, and each
// capability handler refuses until a domain adapter with current
// authorization and evidence ports is explicitly bound.
func CandidateWorkOrderRegistration(spec WorkOrderRegistrationSpec) (PinnedRegistration, error) {
	if spec.Match == nil {
		return PinnedRegistration{}, fmt.Errorf("work order registration: current-intent matcher is required")
	}
	if strings.TrimSpace(spec.Pin.TemplateID) == "" || strings.TrimSpace(spec.Pin.Version) == "" || strings.TrimSpace(spec.Pin.Digest) == "" {
		return PinnedRegistration{}, workorder.ErrInvalidTemplatePin
	}
	plan, err := workorder.CompilePinned(spec.Template, spec.Pin)
	if err != nil {
		return PinnedRegistration{}, fmt.Errorf("work order registration: compile pinned template: %w", err)
	}
	wantWorkflowID := workorder.WorkflowIDForTemplate(spec.Pin.TemplateID)
	if plan.WorkflowID != wantWorkflowID || plan.Version == 0 {
		return PinnedRegistration{}, fmt.Errorf("work order registration: compiled workflow identity is invalid")
	}
	_, err = workorder.DefinitionForPin(spec.Template, spec.Pin)
	if err != nil {
		return PinnedRegistration{}, err
	}
	name := "work-order:" + spec.Pin.TemplateID + "@" + spec.Pin.Version
	registration := WorkflowRegistration{
		Name:            name,
		WorkflowID:      plan.WorkflowID,
		SemanticVersion: spec.Pin.Version,
		Definition: func() workflow.Definition {
			fresh, _ := workorder.DefinitionForPin(spec.Template, spec.Pin)
			return fresh
		},
		Compile: func() (*workflow.CompiledWorkflow, error) { return workorder.CompilePinned(spec.Template, spec.Pin) },
		StepHandlers: map[string]StepHandler{
			workorder.CapabilitySubmitRequest: refusingWorkOrderHandler(workorder.CapabilitySubmitRequest, workorder.NodeSubmitRequest),
			workorder.CapabilityRelease:       refusingWorkOrderHandler(workorder.CapabilityRelease, workorder.NodeRelease),
			workorder.CapabilityClose:         refusingWorkOrderHandler(workorder.CapabilityClose, workorder.NodeClose),
		},
		ApprovalCompilers:   map[string]ApprovalCompiler{},
		AdmittedIntentTypes: map[string]bool{},
	}
	return PinnedRegistration{
		Registration: registration,
		Plan:         plan,
		Pin:          version.Pin{CompiledPlanDigest: plan.Digest()},
		Match: func(req runtime.StartRequest) bool {
			if req.Source == nil || req.Source.Kind != runtime.StartSourceProposal || req.Source.Proposal == nil ||
				req.Source.IntentType != "WORK_ORDER_EXECUTE" || req.PinnedCompiledPlanDigest != "" && req.PinnedCompiledPlanDigest != plan.Digest() {
				return false
			}
			return spec.Match(req)
		},
	}, nil
}

func refusingWorkOrderHandler(capabilityID string, nodeID string) StepHandler {
	return StepHandler{
		CapabilityID: capabilityID,
		Nodes:        []string{nodeID},
		Run:          unboundCapabilityStepRun(capabilityID),
	}
}

// WorkOrderRuntimePin is the order's persisted identity for the workflow
// instance that owns its phase transitions. The digest must be the compiled
// plan digest, not a caller-provided template digest.
type WorkOrderRuntimePin struct {
	TenantID         uuid.UUID
	InstanceID       uuid.UUID
	WorkOrderID      string
	OrderRevision    uint64
	WorkflowID       string
	WorkflowVersion  uint32
	CompiledPlanHash string
}

// WorkOrderRuntimeReader is the narrow durable runtime read surface required
// to verify an order's phase completion. runtime.Store satisfies it.
type WorkOrderRuntimeReader interface {
	LoadInstance(context.Context, runtime.Executor, uuid.UUID, uuid.UUID) (runtime.Instance, error)
	LoadNodeExecutions(context.Context, runtime.Executor, uuid.UUID, uuid.UUID) ([]runtime.NodeExecution, error)
}

// WorkOrderNodeBindingReader verifies the durable input/effect evidence that
// ties one completed runtime node to the order's exact current revision. The
// runtime node table alone does not carry these business keys, so a
// production composition must resolve them from the governed workflow input,
// approval, or capability evidence record.
type WorkOrderNodeBindingReader interface {
	BindsWorkOrderRevision(context.Context, runtime.NodeExecution, WorkOrderRuntimePin) (bool, error)
}

// RequireWorkOrderNodeCompleted proves the requested node completed
// successfully in the runtime instance pinned by the order. Merely reaching
// an HTTP route, finding a template phase, or finding a node from another
// instance/version does not satisfy this check.
func RequireWorkOrderNodeCompleted(ctx context.Context, reader WorkOrderRuntimeReader, binding WorkOrderNodeBindingReader, ex runtime.Executor, pin WorkOrderRuntimePin, nodeID string) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.work_order_completion_proof", pin, nodeID)
	defer func() { observe.DoneWith(obsOp, retErr, nil) }()
	if reader == nil || binding == nil || ex == nil || pin.TenantID == uuid.Nil || pin.InstanceID == uuid.Nil ||
		strings.TrimSpace(pin.WorkOrderID) == "" || pin.OrderRevision == 0 ||
		strings.TrimSpace(pin.WorkflowID) == "" || pin.WorkflowVersion == 0 ||
		strings.TrimSpace(pin.CompiledPlanHash) == "" || strings.TrimSpace(nodeID) == "" {
		return ErrWorkOrderCompletionProof
	}
	instance, err := reader.LoadInstance(ctx, ex, pin.TenantID, pin.InstanceID)
	if err != nil {
		return fmt.Errorf("%w: load pinned instance: %v", ErrWorkOrderCompletionProof, err)
	}
	if instance.TenantID != pin.TenantID || instance.InstanceID != pin.InstanceID ||
		instance.WorkflowID != pin.WorkflowID || instance.WorkflowVersion != pin.WorkflowVersion ||
		instance.CompiledPlanHash != pin.CompiledPlanHash {
		return ErrWorkOrderCompletionProof
	}
	nodes, err := reader.LoadNodeExecutions(ctx, ex, pin.TenantID, pin.InstanceID)
	if err != nil {
		return fmt.Errorf("%w: load node executions: %v", ErrWorkOrderCompletionProof, err)
	}
	for _, node := range nodes {
		if node.TenantID == pin.TenantID && node.InstanceID == pin.InstanceID && node.NodeID == nodeID &&
			node.Status == runtime.NodeSucceeded && node.CompletedAt != nil {
			bound, bindErr := binding.BindsWorkOrderRevision(ctx, node, pin)
			if bindErr != nil {
				return fmt.Errorf("%w: verify node business binding: %v", ErrWorkOrderCompletionProof, bindErr)
			}
			if bound {
				return nil
			}
		}
	}
	return ErrWorkOrderCompletionProof
}
