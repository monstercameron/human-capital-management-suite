package execution

// registrations.go is WF-EXT-002's replacement for the two-plan Promotion
// switch: execution composes a list of WorkflowRegistration values and
// dispatches through them. Each registration names one executable workflow
// (its definition and compile entrypoint), the match predicate a start
// request satisfies to bind it, the step handlers keyed by the capability
// id the compiled node invokes, the approval-requirement compilers keyed by
// the approval node they route, and the intent types it admits. The policy
// resolver carries one entry per registration, and the transactional claim
// derives from the compiled node's effect role (see promotionStepRunner's
// RunsInTransaction), never a node-id list. The high-performer variant lands
// here as a third registration sharing the execute node set
// (internal/workflow/promotionhiperf), not a copied package.

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionhiperf"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// Registration names for the shipped workflows this composition serves.
const (
	RegistrationPromotionApproval      = "promotion-approval"
	RegistrationPromotionExecuteV1_0   = "promotion-execute-v1.0"
	RegistrationPromotionExecuteV1_1   = "promotion-execute-v1.1"
	RegistrationPromotionExecute       = "promotion-execute"
	RegistrationPromotionHighPerformer = "promotion-high-performer"
)

// capabilityReleaseHold is the budget-hold release the compensation node
// invokes. promotionexec leaves the constant unexported; the literal is
// pinned by TestTodo_WF_EXT_002, which requires every capability id a
// compiled node carries to resolve through the registration table.
const capabilityReleaseHold = "hcmnext.rewards.release_compensation_budget"

// StepRunFunc executes one node through the composed promotion runner. The
// runner is built per step from the late-bound ports, so handlers stay
// valid across the application's BindStepServices call.
type StepRunFunc func(ctx context.Context, runner *promotionsteps.Runner, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error)

// defaultStepRun dispatches through the shared promotion runner: the
// selection key was the registration's capability id, the execution is the
// one runner every plan already uses.
func defaultStepRun(ctx context.Context, runner *promotionsteps.Runner, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if runner == nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("platform execution: no step runner for node %q", req.Node.ID)
	}
	return runner.Run(ctx, req)
}

// unboundCapabilityStepRun fails a node whose capability no port serves yet
// closed, naming the capability. The high-performer variant's market-rate
// fetch resolves through this entry until the market-rate port is bound
// through the registration.
func unboundCapabilityStepRun(capabilityID string) StepRunFunc {
	return func(context.Context, *promotionsteps.Runner, execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("platform execution: capability %q is not bound to a step port in this composition", capabilityID)
	}
}

// StepHandler serves the nodes that invoke one capability id.
type StepHandler struct {
	// CapabilityID is the invoked capability this handler serves.
	CapabilityID string
	// Nodes lists the node ids served through it, for inspectors.
	Nodes []string
	// Run executes a node of this capability.
	Run StepRunFunc
}

// ApprovalCompiler compiles the approval requirement one approval node
// routes, for its owner at its deadline.
type ApprovalCompiler func(owner string, decideBy time.Time) (humanwork.ApprovalRequirement, error)

// WorkflowRegistration is one executable workflow the execution authority
// serves: its graph, its match predicate, its capability-keyed step
// handlers, its approval compilers and the intent types it admits.
type WorkflowRegistration struct {
	// Name is the registration's stable key (Registration* constants).
	Name string
	// WorkflowID is the published workflow identity.
	WorkflowID string
	// SemanticVersion is the human-facing version this registration serves.
	SemanticVersion string
	// Definition returns the workflow's definition graph.
	Definition func() workflow.Definition
	// Compile returns the served EXECUTE projection.
	Compile func() (*workflow.CompiledWorkflow, error)
	// StepHandlers dispatches capability nodes, keyed by capability id.
	StepHandlers map[string]StepHandler
	// ApprovalCompilers compiles approval requirements, keyed by node id.
	ApprovalCompilers map[string]ApprovalCompiler
	// AdmittedIntentTypes is the intent-type set this workflow admits.
	AdmittedIntentTypes map[string]bool
}

// HandlesCapability reports whether the registration serves a node invoking
// the capability id.
func (r WorkflowRegistration) HandlesCapability(id string) bool {
	_, ok := r.StepHandlers[id]
	return ok
}

// HandlerFor resolves the capability-keyed handler for a compiled node: the
// selection key is the node's own compiled capability id, never its node
// id. Nodes without a capability (approvals, waits, tasks, decisions,
// signals, ends) resolve no handler and run the legacy runner path.
func (r WorkflowRegistration) HandlerFor(node workflow.CompiledNode) (StepHandler, bool) {
	if node.Capability == nil {
		return StepHandler{}, false
	}
	handler, ok := r.StepHandlers[node.Capability.ID]
	if !ok || handler.Run == nil {
		return StepHandler{}, false
	}
	return handler, true
}

// CompileAndPin compiles the served plan and binds its exact pin.
func (r WorkflowRegistration) CompileAndPin() (*workflow.CompiledWorkflow, version.Pin, error) {
	if r.Compile == nil {
		return nil, version.Pin{}, fmt.Errorf("platform execution: registration %q names no compile entrypoint", r.Name)
	}
	plan, err := r.Compile()
	if err != nil {
		return nil, version.Pin{}, err
	}
	return plan, version.Pin{CompiledPlanDigest: plan.Digest()}, nil
}

// promotionCapabilityHandlers is the capability-keyed step table the execute
// graph's capability nodes dispatch through: one entry per capability id
// the compiled execute nodes invoke.
func promotionCapabilityHandlers() map[string]StepHandler {
	add := func(table map[string]StepHandler, capabilityID string, nodes ...string) {
		table[capabilityID] = StepHandler{CapabilityID: capabilityID, Nodes: append([]string(nil), nodes...), Run: defaultStepRun}
	}
	table := map[string]StepHandler{}
	add(table, promotionexec.CapabilitySnapshotWorker, promotionexec.NodeSnapshotWorker)
	add(table, promotionexec.CapabilitySimulateCompensation, promotionexec.NodeSimulateCompensation)
	add(table, promotionexec.CapabilityEvaluateBand, promotionexec.NodeEvaluateBand)
	add(table, promotionexec.CapabilityRevalidate, promotionexec.NodeRevalidate)
	add(table, promotionexec.CapabilityExecutePromotion, promotionexec.NodeExecutePromotion)
	add(table, capabilityReleaseHold, promotionexec.NodeCompensateHold)
	add(table, promotionexec.CapabilityObservePayroll, promotionexec.NodeObservePayroll)
	add(table, promotionexec.CapabilityObserveAccess, promotionexec.NodeObserveAccess)
	add(table, promotionexec.CapabilityObserveReconciliation, promotionexec.NodeObserveReconciliation)
	return table
}

// promotionApprovalCompilers compiles the execute graph's approval
// requirements, keyed by approval node id. Both execute versions share
// their approval requirements.
func promotionApprovalCompilers() map[string]ApprovalCompiler {
	return map[string]ApprovalCompiler{
		promotionexec.NodeApproveFinance: promotionexec.CompileFinanceApprovalRequirement,
		promotionexec.NodeApproveManager: promotionexec.CompileManagerApprovalRequirement,
	}
}

// promotionIntentTypes is the intent-type set every promotion registration
// admits.
func promotionIntentTypes() map[string]bool {
	return map[string]bool{promotion.IntentType: true}
}

// ShippedRegistrations lists every executable workflow this composition can
// serve: the prototype approval, the frozen execute 1.0.0, the current
// execute graph, and the high-performer variant as a third registration
// sharing the execute node set. The list is data: matching and pinning
// happen in ComposeWorkflowRegistrations.
func ShippedRegistrations() []WorkflowRegistration {
	handlers := promotionCapabilityHandlers()
	variantHandlers := make(map[string]StepHandler, len(handlers)+1)
	for id, handler := range handlers {
		variantHandlers[id] = handler
	}
	variantHandlers[promotionhiperf.CapabilityMarketRate] = StepHandler{
		CapabilityID: promotionhiperf.CapabilityMarketRate,
		Nodes:        []string{promotionhiperf.NodeFetchMarketRate},
		Run:          unboundCapabilityStepRun(promotionhiperf.CapabilityMarketRate),
	}
	return []WorkflowRegistration{
		{
			Name:            RegistrationPromotionApproval,
			WorkflowID:      prototype.ApprovalWorkflowID,
			SemanticVersion: "1.0.0",
			Definition:      prototype.ApprovalDefinition,
			Compile:         prototype.CompileApproval,
			StepHandlers:    map[string]StepHandler{},
			ApprovalCompilers: map[string]ApprovalCompiler{
				prototype.NodeApproval: prototype.CompileApprovalRequirement,
			},
			AdmittedIntentTypes: promotionIntentTypes(),
		},
		{
			Name:                RegistrationPromotionExecuteV1_0,
			WorkflowID:          promotionexec.WorkflowID,
			SemanticVersion:     promotionexec.SemanticVersionV1_0,
			Definition:          promotionexec.DefinitionV1_0,
			Compile:             func() (*workflow.CompiledWorkflow, error) { return promotionexec.CompileV1_0() },
			StepHandlers:        handlers,
			ApprovalCompilers:   promotionApprovalCompilers(),
			AdmittedIntentTypes: promotionIntentTypes(),
		},
		{
			Name:                RegistrationPromotionExecuteV1_1,
			WorkflowID:          promotionexec.WorkflowID,
			SemanticVersion:     promotionexec.SemanticVersionV1_1,
			Definition:          promotionexec.DefinitionV1_1,
			Compile:             func() (*workflow.CompiledWorkflow, error) { return promotionexec.CompileV1_1() },
			StepHandlers:        handlers,
			ApprovalCompilers:   promotionApprovalCompilers(),
			AdmittedIntentTypes: promotionIntentTypes(),
		},
		{
			Name:                RegistrationPromotionExecute,
			WorkflowID:          promotionexec.WorkflowID,
			SemanticVersion:     promotionexec.SemanticVersion,
			Definition:          promotionexec.Definition,
			Compile:             func() (*workflow.CompiledWorkflow, error) { return promotionexec.Compile() },
			StepHandlers:        handlers,
			ApprovalCompilers:   promotionApprovalCompilers(),
			AdmittedIntentTypes: promotionIntentTypes(),
		},
		{
			Name:                RegistrationPromotionHighPerformer,
			WorkflowID:          promotionhiperf.WorkflowID,
			SemanticVersion:     promotionhiperf.SemanticVersion,
			Definition:          promotionhiperf.Definition,
			Compile:             func() (*workflow.CompiledWorkflow, error) { return promotionhiperf.Compile() },
			StepHandlers:        variantHandlers,
			ApprovalCompilers:   promotionApprovalCompilers(),
			AdmittedIntentTypes: promotionIntentTypes(),
		},
	}
}

// PinnedRegistration is one registration with its served compiled plan,
// exact pin and match predicate bound.
type PinnedRegistration struct {
	Registration WorkflowRegistration
	Plan         *workflow.CompiledWorkflow
	Pin          version.Pin
	// Match reports whether this registration answers req.
	Match func(req runtime.StartRequest) bool
}

// ComposeWorkflowRegistrations compiles every shipped registration and binds
// one match predicate per registration. Under PLAN_EXECUTE one cell serves
// all four: frozen-version continuations, variant continuations, new starts
// and current-version continuations on the execute graphs, and prototype
// continuations on the approval graph. Under PLAN_PROTOTYPE only the
// approval registration serves, preserving the pre-execute behavior.
func ComposeWorkflowRegistrations(plan PromotionPlan) ([]PinnedRegistration, error) {
	regs := ShippedRegistrations()
	pinned := make([]PinnedRegistration, 0, len(regs))
	digests := map[string]string{}
	for _, reg := range regs {
		compiled, pin, err := reg.CompileAndPin()
		if err != nil {
			return nil, fmt.Errorf("platform execution: compile registration %q: %w", reg.Name, err)
		}
		if compiled.WorkflowID != reg.WorkflowID {
			return nil, fmt.Errorf("platform execution: registration %q compiles workflow %q, want %q", reg.Name, compiled.WorkflowID, reg.WorkflowID)
		}
		pinned = append(pinned, PinnedRegistration{Registration: reg, Plan: compiled, Pin: pin})
		digests[reg.Name] = pin.CompiledPlanDigest
	}
	bind := func(name string, match func(req runtime.StartRequest) bool) {
		for i := range pinned {
			if pinned[i].Registration.Name == name {
				pinned[i].Match = match
			}
		}
	}
	v1_0, v1_1, current, variant, approval := digests[RegistrationPromotionExecuteV1_0], digests[RegistrationPromotionExecuteV1_1], digests[RegistrationPromotionExecute], digests[RegistrationPromotionHighPerformer], digests[RegistrationPromotionApproval]
	if plan == PLAN_EXECUTE {
		bind(RegistrationPromotionExecuteV1_0, func(req runtime.StartRequest) bool {
			return req.PinnedCompiledPlanDigest == v1_0
		})
		bind(RegistrationPromotionExecuteV1_1, func(req runtime.StartRequest) bool {
			return req.PinnedCompiledPlanDigest == v1_1
		})
		bind(RegistrationPromotionHighPerformer, func(req runtime.StartRequest) bool {
			return req.PinnedCompiledPlanDigest == variant
		})
		bind(RegistrationPromotionExecute, func(req runtime.StartRequest) bool {
			return req.PinnedCompiledPlanDigest == "" || req.PinnedCompiledPlanDigest == current
		})
		bind(RegistrationPromotionApproval, func(req runtime.StartRequest) bool {
			return req.PinnedCompiledPlanDigest == approval
		})
		return pinned, nil
	}
	bind(RegistrationPromotionApproval, nil)
	return pinned[:1], nil
}

// ResolverForRegistrations builds the policy resolver carrying one entry per
// pinned registration, in composition order. A request no registration
// claims is refused rather than defaulted.
func ResolverForRegistrations(pinned []PinnedRegistration) effects.PolicyResolver {
	entries := make([]effects.PolicyEntry, 0, len(pinned))
	for _, p := range pinned {
		entries = append(entries, effects.PolicyEntry{
			WorkflowID: p.Plan.WorkflowID,
			Pin:        p.Pin,
			Plan:       p.Plan,
			Match:      p.Match,
		})
	}
	return effects.PolicyResolver{Entries: entries}
}

// UnionAdmittedIntentTypes unions every registration's admitted intent
// types into the execution authority's closed set.
func UnionAdmittedIntentTypes(pinned []PinnedRegistration) map[string]bool {
	union := map[string]bool{}
	for _, p := range pinned {
		for intentType := range p.Registration.AdmittedIntentTypes {
			union[intentType] = true
		}
	}
	return union
}

// capabilityDispatch unions the capability-keyed step tables of the served
// execute registrations (current and variant): shared capabilities dispatch
// identically, and the variant's market-rate fetch fails closed until its
// port is bound through the registration.
func capabilityDispatch(pinned []PinnedRegistration) map[string]StepHandler {
	table := map[string]StepHandler{}
	for _, p := range pinned {
		if p.Registration.Name != RegistrationPromotionExecute && p.Registration.Name != RegistrationPromotionHighPerformer {
			continue
		}
		for id, handler := range p.Registration.StepHandlers {
			if _, ok := table[id]; !ok {
				table[id] = handler
			}
		}
	}
	return table
}

// dispatchCapability resolves the capability-keyed handler for a compiled
// execute node. The second result is false for nodes without a capability
// or without a registered handler, which run the legacy runner path.
func dispatchCapability(table map[string]StepHandler, node workflow.CompiledNode) (StepHandler, bool) {
	if node.Capability == nil {
		return StepHandler{}, false
	}
	handler, ok := table[node.Capability.ID]
	if !ok || handler.Run == nil {
		return StepHandler{}, false
	}
	return handler, true
}
