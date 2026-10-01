package clockpunch

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

const (
	// WorkflowID identifies the published clock session workflow.
	WorkflowID = "hcmnext.workflows.time.clock_in_out"
	// IntentType identifies the governed clock punch trigger.
	IntentType = "hcmnext.time.record_punch/v1"
	// NodeCommitPunch commits the authoritative clock-in observation.
	NodeCommitPunch = "commit_punch"
	// NodeAwaitClockOut parks the session until its clock-out signal arrives.
	NodeAwaitClockOut = "await_clock_out"
	// NodeCommitClockOut commits the authoritative clock-out observation.
	NodeCommitClockOut = "commit_clock_out"
	// NodeClosed is the successfully closed session terminal.
	NodeClosed = "session_closed"
	// NodeMissingOut records an expired session without inventing an out time.
	NodeMissingOut = "missing_clock_out"
	// NodeRepair records a failed clock operation requiring repair.
	NodeRepair = "clock_repair_required"
	// NodeCancelled is the governed cancellation terminal.
	NodeCancelled = "clock_cancelled"
	// CapabilityCommit identifies the canonical observation mutation capability.
	CapabilityCommit = "hcmnext.time.commit_punch_observation"
)

// Definition hand-authors a bounded clock-in, wait, clock-out, close path.
// Breaks and transfers belong to the fuller time session template; this graph
// explicitly admits only clock-out events and retains missing-out evidence.
func Definition() workflow.Definition {
	return workflow.Definition{
		WorkflowID: WorkflowID, Version: 1, Name: "Clock in and clock out", IntentType: IntentType,
		InputSchema: schema("Input"), OutputSchema: schema("Result"), VariablesSchema: schema("Variables"),
		TenantScope: "tenant-template", OrganizationScope: "tenant-template/time", RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID: NodeCommitPunch, Limits: workflow.Limits{MaxFanOut: 5, MaxDepth: 8, MaxNodes: 7},
		Outputs:          []workflow.Field{{Path: "terminal_code", Type: workflow.ValueType{Kind: workflow.KindString}}},
		FailurePolicyRef: "policy.workflow.failure.time/v1", CancellationPolicyRef: "policy.workflow.cancellation.time/v1",
		MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.time-records/v1",
		Nodes: []workflow.Node{
			commitNode(NodeCommitPunch), awaitClockOut(), commitNode(NodeCommitClockOut),
			terminal(NodeClosed, "TIME_SESSION_CLOSED", workflow.RuntimeCompleted, builders.Completion("CLOSED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED")),
			terminal(NodeMissingOut, "TIME_SESSION_MISSING_OUT", workflow.RuntimeBlocked, builders.Completion("SUBMITTED", "BLOCKED", "UNKNOWN", "PENDING_OBSERVATION", "PENDING")),
			terminal(NodeRepair, "TIME_SESSION_REPAIR_REQUIRED", workflow.RuntimeRepairRequired, builders.Completion("SUBMITTED", "REPAIR_REQUIRED", "UNKNOWN", "DEGRADED", "PENDING")),
			terminal(NodeCancelled, "TIME_SESSION_CANCELLED", workflow.RuntimeCancelled, builders.Completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE")),
		},
		Edges: []workflow.Edge{
			{From: NodeCommitPunch, To: NodeAwaitClockOut, RouteKey: "SUCCEEDED"},
			{From: NodeCommitPunch, To: NodeRepair, RouteKey: "REJECTED"},
			{From: NodeCommitPunch, To: NodeRepair, RouteKey: "AMBIGUOUS"},
			{From: NodeCommitPunch, To: NodeRepair, RouteKey: "UNKNOWN"},
			{From: NodeAwaitClockOut, To: NodeCommitClockOut, RouteKey: "SUCCEEDED"},
			{From: NodeAwaitClockOut, To: NodeMissingOut, RouteKey: "TIMED_OUT"},
			{From: NodeAwaitClockOut, To: NodeCancelled, RouteKey: "CANCELLED"},
			{From: NodeCommitClockOut, To: NodeClosed, RouteKey: "SUCCEEDED"},
			{From: NodeCommitClockOut, To: NodeRepair, RouteKey: "REJECTED"},
			{From: NodeCommitClockOut, To: NodeRepair, RouteKey: "AMBIGUOUS"},
			{From: NodeCommitClockOut, To: NodeRepair, RouteKey: "UNKNOWN"},
		},
	}
}

func schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: WorkflowID + "." + name + "/v1", Version: 1, ProtobufFullName: "hcmnext.time.workflow." + name}
}

func governance(boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
	return workflow.NodeGovernance{Purpose: "TIME_RECORD_PUNCH", Classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE", RevalidationBoundary: boundary, DataAccessManifestRef: "data-access.time.punch_session/v1"}
}

func commitNode(id string) workflow.Node {
	gov := governance(workflow.RevalidatePreEffect)
	gov.RequiredDecisions = []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk}
	return workflow.Node{ID: id, Type: workflow.StepCapability, InputSchema: capabilitySchema("request"), OutputSchema: capabilitySchema("response"),
		Outputs:        []workflow.Field{{Path: "observation_id", Type: workflow.ValueType{Kind: workflow.KindString}}, {Path: "session_id", Type: workflow.ValueType{Kind: workflow.KindString}}},
		DeclaredEffect: capability.EffectInternalMutation, EffectRole: workflow.RoleAuthoritativeCore, SafePointRequested: true, FailureRoute: NodeRepair,
		Capability: &workflow.CapabilityRef{ID: CapabilityCommit, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:time.punch.write"}, IdempotencyKeyMapping: "observation_id", EffectBinding: id + ".observation_id"}, Governance: gov}
}

func awaitClockOut() workflow.Node {
	return workflow.Node{ID: NodeAwaitClockOut, Type: workflow.StepSignal, InputSchema: schema("ClockOutWaitInput"), OutputSchema: schema("ClockOutWaitResult"), DeclaredEffect: capability.EffectPure, FailureRoute: NodeRepair,
		Signal: &workflow.SignalSpec{EventType: "hcmnext.events.time.clock_out", CorrelationKeyExpression: "subject:time_session", ExpectedSchemaRef: schema("ClockOutSignal"), AcceptedSources: []string{"hcmnext.time.clock", "hcmnext.time.kiosk", "hcmnext.integrations.time_device"}, Ordering: workflow.SignalOrderingMonotonicSequence, CloseAfterSeconds: 57600}, Governance: governance(workflow.RevalidatePreExecution)}
}

func terminal(id, code string, status workflow.RuntimeStatus, dimensions map[string]string) workflow.Node {
	end := &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: dimensions}
	if id == NodeClosed {
		end.CommitReceiptRef = "receipt.time.session/v1"
	}
	if id == NodeRepair {
		end.RepairRefs = []string{"repair.time.session/v1"}
	}
	return workflow.Node{ID: id, Type: workflow.StepEnd, InputSchema: schema(id + "Input"), OutputSchema: schema(id + "Result"), DeclaredEffect: capability.EffectPure,
		Inputs:        []workflow.Field{{Path: "terminal_code", Type: workflow.ValueType{Kind: workflow.KindString}}},
		InputMappings: []workflow.Mapping{{Target: "terminal_code", Source: builders.Constant(code, workflow.ValueType{Kind: workflow.KindString})}},
		End:           end, Governance: governance(workflow.RevalidatePreClosure)}
}

// ClockOutSignalSchemaRef is the schema reference a clock-out signal must carry
// to satisfy the wait node: the node's expected schema in the engine's own
// spelling, so a delivery cannot drift from the plan it resumes.
func ClockOutSignalSchemaRef() string { return schema("ClockOutSignal").String() }
