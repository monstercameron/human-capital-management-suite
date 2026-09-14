package shadow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// RowCounts is the only durable surface a shadow run may observe. A pgtest
// adapter can implement RowCounter and prove these four counts are unchanged.
type RowCounts struct {
	LedgerEvent   int64 `json:"ledger_event"`
	WorkItem      int64 `json:"work_item"`
	WorkflowTimer int64 `json:"workflow_timer"`
	Outbox        int64 `json:"outbox"`
}

func (c RowCounts) Equal(other RowCounts) bool { return c == other }

type RowCounter interface {
	Counts(context.Context) (RowCounts, error)
}

// StepResult is the candidate node result. It carries no successor; the shadow
// coordinator derives the next node only from the compiled edge table.
type StepResult struct {
	Outcome      workflow.Outcome
	RouteKey     string
	OutputDigest string
	Await        frontier.AwaitKind
	AwaitRef     string
	TerminalCode string
}

type StepRequest struct {
	Node    workflow.CompiledNode
	Attempt int
	At      time.Time
	View    *View
	Ports   Ports
}

type StepRunner interface {
	Run(context.Context, StepRequest) (StepResult, error)
}
type StepRunnerFunc func(context.Context, StepRequest) (StepResult, error)

func (f StepRunnerFunc) Run(ctx context.Context, req StepRequest) (StepResult, error) {
	return f(ctx, req)
}

type Options struct {
	Plan       *workflow.CompiledWorkflow
	Contract   intent.ModeContract
	Definition intent.Definition
	Instance   intent.Instance
	State      State
	Reads      State
	Steps      StepRunner
	Clock      func() time.Time
	RowCounter RowCounter
	MaxSteps   int
}

type Result struct {
	Trace           Trace
	State           State
	WorkItems       []WorkItem
	Timers          []Timer
	Effects         []EffectAttempt
	Terminal        TerminalRecord
	RowCountsBefore RowCounts
	RowCountsAfter  RowCounts
	Complete        bool
}

type Runner struct {
	opts     Options
	plan     *workflow.CompiledWorkflow
	contract intent.ModeContract
}

func ContractFor(env intent.Environment) (intent.ModeContract, error) {
	return intent.ModeContractFor(intent.ModeShadow, env)
}

// Admit checks the immutable SHADOW row and refuses every other mode row.
func Admit(contract intent.ModeContract, def intent.Definition, inst intent.Instance) error {
	if contract.Mode != intent.ModeShadow {
		return refuse(CodeModeRefused, "", "SHADOW requires the SHADOW mode row; %s was supplied", contract.Mode)
	}
	expected, err := intent.ModeContractFor(intent.ModeShadow, contract.Environment)
	if err != nil {
		return wrap(CodeModeRefused, "", err, "resolve the SHADOW contract")
	}
	if diffs := intent.CompareContracts(expected, contract); len(diffs) != 0 {
		return refuse(CodeModeRefused, "", "supplied SHADOW contract differs at %s", diffs[0])
	}
	if def.Ref.TypeID != "" {
		if err := contract.Permit(def, intent.AttemptGovernedRead); err != nil {
			return wrap(CodeModeRefused, "", err, "definition does not admit SHADOW")
		}
	}
	if inst.IntentID != "" {
		if err := intent.CausalSeparation(contract, inst); err != nil {
			return wrap(CodeModeRefused, "", err, "shadow identity")
		}
	}
	return nil
}

func New(opts Options) (*Runner, error) {
	if opts.Plan == nil {
		return nil, refuse(CodeInvalidOptions, "", "no compiled plan")
	}
	if opts.Steps == nil {
		return nil, refuse(CodeInvalidOptions, "", "no candidate StepRunner")
	}
	if err := Admit(opts.Contract, opts.Definition, opts.Instance); err != nil {
		return nil, err
	}
	if err := opts.Plan.Verify(); err != nil {
		return nil, wrap(CodeModeRefused, "", err, "compiled plan is not verified")
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Time{} }
	}
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 4*len(opts.Plan.Nodes) + 8
	}
	return &Runner{opts: opts, plan: opts.Plan, contract: opts.Contract}, nil
}

func Run(ctx context.Context, plan *workflow.CompiledWorkflow, opts Options) (Result, error) {
	opts.Plan = plan
	runner, err := New(opts)
	if err != nil {
		return Result{}, err
	}
	return runner.Run(ctx)
}

func (r *Runner) Run(ctx context.Context) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.shadow.run")
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	before, err := r.counts(ctx)
	if err != nil {
		return Result{}, err
	}
	view := NewView(r.opts.State)
	ports := Ports{Capabilities: NewRecorder(r.opts.Reads), Effects: NewRecorder(r.opts.Reads), WorkItems: &WorkItemRecorder{}, Timers: &TimerRecorder{}, Terminal: &TerminalRecorder{}}
	trace := Trace{TenantID: string(r.opts.Instance.Tenant), InstanceID: r.opts.Instance.IntentID, WorkflowID: r.plan.WorkflowID, PlanDigest: r.plan.Digest()}
	nodeID := r.plan.StartNodeID
	for step := 1; step <= r.opts.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return Result{Trace: trace, State: view.Snapshot(), RowCountsBefore: before}, err
		}
		node, ok := r.plan.Node(nodeID)
		if !ok {
			return Result{Trace: trace, State: view.Snapshot(), RowCountsBefore: before}, refuse(CodeInvalidOptions, nodeID, "frontier names no compiled node")
		}
		at := r.opts.Clock().UTC()
		outcome, err := r.opts.Steps.Run(ctx, StepRequest{Node: node, Attempt: 1, At: at, View: view, Ports: ports})
		if err != nil {
			return Result{Trace: trace, State: view.Snapshot(), RowCountsBefore: before}, wrap(CodeStepFailed, nodeID, err, "candidate step failed")
		}
		route := outcome.RouteKey
		if route == "" {
			route = string(outcome.Outcome)
		}
		trace.Entries = append(trace.Entries, TraceEntry{Sequence: step, NodeID: nodeID, Attempt: 1, StepType: node.Type, Outcome: outcome.Outcome, RouteKey: route, OutputDigest: outcome.OutputDigest, StateDigest: digestState(view.Snapshot()), ObservedAt: at})
		if outcome.Await != frontier.AwaitNone {
			switch outcome.Await {
			case frontier.AwaitWorkItem:
				ports.WorkItems.Schedule(WorkItem{NodeID: nodeID, ID: outcome.AwaitRef, Ref: outcome.AwaitRef})
			case frontier.AwaitTimer:
				ports.Timers.Schedule(Timer{NodeID: nodeID, ID: outcome.AwaitRef, DueAt: at})
			case frontier.AwaitSignal:
				ports.WorkItems.Schedule(WorkItem{NodeID: nodeID, ID: outcome.AwaitRef, Ref: "SIGNAL"})
			}
			return r.finish(ctx, trace, view, ports, before, false)
		}
		if node.Type == workflow.StepEnd || outcome.TerminalCode != "" {
			businessCode := outcome.TerminalCode
			if businessCode == "" && node.Terminal != nil {
				businessCode = node.Terminal.TerminalCode
			}
			trace.Terminal = ports.Terminal.Record(TerminalRequest{TenantID: string(r.opts.Instance.Tenant), InstanceID: r.opts.Instance.IntentID, WorkflowID: r.plan.WorkflowID, PlanDigest: r.plan.Digest(), BusinessCode: businessCode, OutputDigest: outcome.OutputDigest, CorrelationID: r.opts.Instance.CorrelationID, IdempotencyKey: r.opts.Instance.IdempotencyKey})
			return r.finish(ctx, trace, view, ports, before, true)
		}
		next, found := nextNode(r.plan, nodeID, route)
		if !found {
			return Result{Trace: trace, State: view.Snapshot(), RowCountsBefore: before}, refuse(CodeNoRoute, nodeID, "route %q is not declared", route)
		}
		nodeID = next
	}
	return Result{Trace: trace, State: view.Snapshot(), RowCountsBefore: before}, refuse(CodeStepBudget, nodeID, "candidate exceeded %d steps", r.opts.MaxSteps)
}

func (r *Runner) finish(ctx context.Context, trace Trace, view *View, ports Ports, before RowCounts, complete bool) (Result, error) {
	trace.digest = computeTraceDigest(trace)
	after, err := r.counts(ctx)
	if err != nil {
		return Result{Trace: trace, State: view.Snapshot(), RowCountsBefore: before, Complete: complete}, err
	}
	if !before.Equal(after) {
		return Result{Trace: trace, State: view.Snapshot(), RowCountsBefore: before, RowCountsAfter: after, Complete: complete}, wrap(CodeDurableMutation, "", ErrDurableMutation, "row counts changed from %#v to %#v", before, after)
	}
	return Result{Trace: trace, State: view.Snapshot(), WorkItems: ports.WorkItems.Items(), Timers: ports.Timers.Timers(), Effects: append(ports.Capabilities.Attempts(), ports.Effects.Attempts()...), Terminal: trace.Terminal, RowCountsBefore: before, RowCountsAfter: after, Complete: complete}, nil
}

func (r *Runner) counts(ctx context.Context) (RowCounts, error) {
	if r.opts.RowCounter == nil {
		return RowCounts{}, nil
	}
	return r.opts.RowCounter.Counts(ctx)
}
func nextNode(plan *workflow.CompiledWorkflow, from, route string) (string, bool) {
	for _, edge := range plan.Edges {
		if edge.From == from && strings.TrimSpace(edge.RouteKey) == route {
			return edge.To, true
		}
	}
	return "", false
}

// Explain returns a concise operational summary suitable for telemetry.
func Explain(result Result) string {
	return fmt.Sprintf("SHADOW %s steps=%d terminal=%s effects_refused=%d", result.Trace.WorkflowID, len(result.Trace.Entries), result.Terminal.Code, len(result.Effects))
}
func Version() int { return 1 }
