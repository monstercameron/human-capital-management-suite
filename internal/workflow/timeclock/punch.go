package timeclock

import (
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Punch session identity (WTIME-003).
const (
	PunchWorkflowID   = "hcmnext.workflows.time.punch_session"
	PunchVersion      = 1
	IntentRecordPunch = "hcmnext.time.record_punch/v1"
)

// Punch session node ids.
const (
	NodeCommitPunch         = "commit_punch"
	NodeReadPunchFacts      = "read_punch_facts"
	NodeClassifyPunch       = "classify_punch"
	NodeApproveHold         = "approve_hold_override"
	NodeReviewPunch         = "review_punch"
	NodeSessionNextStep     = "session_next_step"
	NodeAwaitSessionEvent   = "await_session_event"
	NodeRouteSessionEvent   = "route_session_event"
	NodeAwaitMissingOut     = "await_missing_out_deadline"
	NodeFoldSession         = "fold_session"
	NodeEndSessionClosed    = "end_session_closed"
	NodeEndPunchHeld        = "end_punch_held"
	NodeEndMissingOut       = "end_missing_out"
	NodeEndSessionFailed    = "end_session_failed"
	NodeEndSessionCancelled = "end_session_cancelled"
)

// Classification routes. Every reason timesession.Classify can produce is its
// own route, reusing the domain's exact token, plus ACCEPTED.
const (
	RouteAccepted       = "ACCEPTED"
	RouteDuplicatePunch = string(timesession.ReasonDuplicatePunch)
	RouteRestBreach     = string(timesession.ReasonRestBreach)
	RouteMinorWindow    = string(timesession.ReasonMinorWindow)
	RouteEarlyLockout   = string(timesession.ReasonEarlyLockout)
	RouteGeofence       = string(timesession.ReasonGeofence)
	RouteSpoofSuspected = string(timesession.ReasonSpoofSuspected)
	RouteOfflineReplay  = string(timesession.ReasonOfflineReplay)
	RouteDSTFold        = string(timesession.ReasonDSTFold)
	RouteClockSkew      = string(timesession.ReasonClockSkew)
)

// Session event kinds the typed punch signal carries after the in punch.
const (
	EventBreakStart     = "BREAK_START"
	EventBreakEnd       = "BREAK_END"
	EventJobTransfer    = "JOB_TRANSFER"
	EventTravelTransfer = "TRAVEL_TRANSFER"
	EventOutPunch       = "OUT_PUNCH"
)

// Session next-step routes.
const (
	RouteSessionOpen    = "SESSION_OPEN"
	RouteSessionClosing = "SESSION_CLOSING"
)

// Punch session vocabulary shared with the period run and the inspector.
const (
	EventTypeSessionPunch     = "hcmnext.events.time.session_punch"
	CorrelationTimeSession    = "subject:time_session"
	TransformFoldSession      = "transforms.time.fold_session/v1"
	ApprovalHoldOverride      = "approval.time.punch_hold_override/v1"
	ObligationPeriodCollect   = "obligation.time.period_collect"
	ObligationPunchCorrection = "obligation.time.punch_correction"
	ObligationMissedPunch     = "obligation.time.missed_punch_request"
	TerminalSessionClosed     = "TIME_SESSION_CLOSED"
	TerminalPunchHeld         = "TIME_SESSION_PUNCH_HELD"
	TerminalMissingOut        = "TIME_SESSION_MISSING_OUT"
	TerminalSessionFailed     = "TIME_SESSION_REPAIR_REQUIRED"
	TerminalSessionCancelled  = "TIME_SESSION_CANCELLED"
)

// ClassificationRoutes lists the classify_punch routes in precedence order,
// which is timesession.Classify's own order followed by ACCEPTED.
func ClassificationRoutes() []string {
	return []string{RouteDuplicatePunch, RouteRestBreach, RouteMinorWindow, RouteEarlyLockout, RouteGeofence,
		RouteSpoofSuspected, RouteOfflineReplay, RouteDSTFold, RouteClockSkew, RouteAccepted}
}

// holdRoutes are the legal or compliance holds a supervisor may override.
func holdRoutes() []string {
	return []string{RouteRestBreach, RouteMinorWindow, RouteEarlyLockout, RouteGeofence}
}

// reviewRoutes are the integrity concerns a timekeeper reviews.
func reviewRoutes() []string {
	return []string{RouteSpoofSuspected, RouteOfflineReplay, RouteDSTFold, RouteClockSkew}
}

// SessionEventKinds lists the typed kinds the session signal accepts.
func SessionEventKinds() []string {
	return []string{EventBreakStart, EventBreakEnd, EventJobTransfer, EventTravelTransfer, EventOutPunch}
}

// punchFactFields are the typed facts read_punch_facts returns and
// classify_punch reads. They mirror timesession.PunchFacts field for field.
func punchFactFields() []workflow.Field {
	names := []string{"duplicate_punch_detected", "rest_breach_detected", "minor_window_violation", "early_lockout",
		"outside_geofence", "shared_device_spoof_suspected", "offline_replay_detected", "dst_fold_ambiguous",
		"clock_skew_within_tolerance"}
	out := make([]workflow.Field, 0, len(names))
	for _, n := range names {
		out = append(out, workflow.Field{Path: n, Type: workflow.ValueType{Kind: workflow.KindBool}})
	}
	return out
}

// PunchSessionDefinition returns the punch session template.
//
// The in punch is committed by the start node, so the ingest request can
// return a receipt as soon as the first node's transaction commits (WTIME-004);
// classification, review and the rest of the session continue asynchronously.
// Break, transfer and out punches are the typed session signal inside a
// declared cycle guarded by session_next_step (the WF-EXT-014 stand-in for
// multi-accept). A session with no out punch waits for its deadline and ends
// with a missing-out exception; it never invents a time.
func PunchSessionDefinition(p Params) workflow.Definition {
	p = p.normalized()
	k := kit{workflowID: PunchWorkflowID, purpose: "TIME_RECORD_PUNCH", classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE",
		manifest: "data-access.time.punch_session/v1", subjectBrand: "TimeSessionKey", params: p}

	facts := punchFactFields()
	classifyMaps := make([]workflow.Mapping, 0, len(facts))
	for _, f := range facts {
		classifyMaps = append(classifyMaps, workflow.Mapping{Target: f.Path, Source: builders.FromNode(NodeReadPunchFacts, f.Path)})
	}

	nodes := []workflow.Node{
		k.capability(NodeCommitPunch, CapCommitPunch, workflow.RoleAuthoritativeCore, NodeEndSessionFailed,
			workflow.Field{Path: "observation_id", Type: workflow.ValueType{Kind: workflow.KindString}},
			workflow.Field{Path: "session_id", Type: workflow.ValueType{Kind: workflow.KindString}}),
		k.capability(NodeReadPunchFacts, CapReadPunchFacts, "", NodeEndSessionFailed, facts...),
		k.decision(NodeClassifyPunch, RuleClassifyPunch, ClassificationRoutes(), facts, classifyMaps),
		k.approval(NodeApproveHold, ApprovalHoldOverride),
		k.task(NodeReviewPunch, FormReviewPunch, "TimekeeperFor(assignment)"),
		k.decision(NodeSessionNextStep, RuleSessionNextStep, []string{RouteSessionOpen, RouteSessionClosing}, nil, nil),
		k.signal(NodeAwaitSessionEvent, EventTypeSessionPunch, CorrelationTimeSession, p.SessionCloseAfterSeconds,
			NodeEndSessionFailed, GapTypedSignal, SourceFirstPartyClock, SourceKiosk, SourceDeviceAdapter, SourcePunchImport),
		k.decision(NodeRouteSessionEvent, RuleSessionEventKind, SessionEventKinds(), nil, nil),
		k.wait(NodeAwaitMissingOut, NodeEndSessionFailed),
		k.transform(NodeFoldSession, TransformFoldSession, ""),
		k.end(NodeEndSessionClosed, TerminalSessionClosed, workflow.RuntimeCompleted, dimsSubmittedPending(),
			endOpts{receipt: "receipt.time.session/v1", outstanding: []string{ObligationPeriodCollect}, obligations: []string{ObligationPeriodCollect}}),
		k.end(NodeEndPunchHeld, TerminalPunchHeld, workflow.RuntimeCompleted, dimsHeld(),
			endOpts{receipt: "receipt.time.session/v1", outstanding: []string{ObligationPeriodCollect, ObligationPunchCorrection},
				obligations: []string{ObligationPeriodCollect, ObligationPunchCorrection}}),
		k.end(NodeEndMissingOut, TerminalMissingOut, workflow.RuntimeBlocked, dimsBlocked(),
			endOpts{repair: "repair.time.missed_punch_request/v1", outstanding: []string{ObligationPeriodCollect, ObligationMissedPunch},
				obligations: []string{ObligationPeriodCollect, ObligationMissedPunch}}),
		k.end(NodeEndSessionFailed, TerminalSessionFailed, workflow.RuntimeRepairRequired, dimsRepair(),
			endOpts{repair: "repair.time.session/v1"}),
		k.end(NodeEndSessionCancelled, TerminalSessionCancelled, workflow.RuntimeCancelled, dimsCancelled(), endOpts{}),
	}
	nodes[1].Metadata = map[string]string{"reads": "device_clock_context,geofence,minor_window,rest_ledger"}

	edges := capabilityEdges(NodeCommitPunch, NodeReadPunchFacts, NodeEndSessionFailed)
	edges = append(edges, capabilityEdges(NodeReadPunchFacts, NodeClassifyPunch, NodeEndSessionFailed)...)
	// A duplicate is absorbed idempotently: the session carries on.
	edges = append(edges, edge(NodeClassifyPunch, NodeSessionNextStep, RouteDuplicatePunch))
	edges = append(edges, routesTo(NodeClassifyPunch, NodeApproveHold, holdRoutes()...)...)
	edges = append(edges, routesTo(NodeClassifyPunch, NodeReviewPunch, reviewRoutes()...)...)
	edges = append(edges,
		edge(NodeClassifyPunch, NodeSessionNextStep, RouteAccepted),
		edge(NodeClassifyPunch, NodeEndSessionFailed, string(workflow.OutcomeUnknown)),

		edge(NodeApproveHold, NodeSessionNextStep, "APPROVED"),
		edge(NodeApproveHold, NodeEndPunchHeld, string(workflow.OutcomeRejected)),
		edge(NodeApproveHold, NodeEndPunchHeld, "EXPIRED"),
		edge(NodeApproveHold, NodeEndPunchHeld, "INVALIDATED"),
		edge(NodeApproveHold, NodeEndSessionCancelled, "CANCELLED"),

		edge(NodeReviewPunch, NodeSessionNextStep, string(workflow.OutcomeSucceeded)),
		edge(NodeReviewPunch, NodeEndPunchHeld, string(workflow.OutcomeRejected)),
		edge(NodeReviewPunch, NodeEndPunchHeld, "EXPIRED"),
		edge(NodeReviewPunch, NodeEndSessionCancelled, "CANCELLED"),

		edge(NodeSessionNextStep, NodeAwaitSessionEvent, RouteSessionOpen),
		edge(NodeSessionNextStep, NodeFoldSession, RouteSessionClosing),
		edge(NodeSessionNextStep, NodeEndSessionFailed, string(workflow.OutcomeUnknown)),

		edge(NodeAwaitSessionEvent, NodeRouteSessionEvent, string(workflow.OutcomeSucceeded)),
		edge(NodeAwaitSessionEvent, NodeAwaitMissingOut, "TIMED_OUT"),
		edge(NodeAwaitSessionEvent, NodeEndSessionCancelled, "CANCELLED"),
	)
	edges = append(edges, routesTo(NodeRouteSessionEvent, NodeCommitPunch, SessionEventKinds()...)...)
	edges = append(edges,
		edge(NodeRouteSessionEvent, NodeEndSessionFailed, string(workflow.OutcomeUnknown)),

		edge(NodeAwaitMissingOut, NodeEndMissingOut, string(workflow.OutcomeSucceeded)),
		edge(NodeAwaitMissingOut, NodeEndMissingOut, "LATE"),
		edge(NodeAwaitMissingOut, NodeEndSessionCancelled, "CANCELLED"),

		edge(NodeFoldSession, NodeEndSessionClosed, string(workflow.OutcomeSucceeded)),
		edge(NodeFoldSession, NodeEndSessionFailed, string(workflow.OutcomeFailed)),
	)

	def := k.definition(PunchVersion, "Clock session", IntentRecordPunch, NodeCommitPunch, nodes, edges)
	def.ApprovalRequirements = []workflow.ApprovalRequirement{approvalRequirement(ApprovalHoldOverride, "SupervisorFor(assignment)", p)}
	def.Obligations = []workflow.ObligationRequirement{
		obligation(ObligationPeriodCollect, "The period timecard run collects this session", "time.period"),
		obligation(ObligationPunchCorrection, "Route the held punch to correction; the raw punch is retained", "time.correction"),
		obligation(ObligationMissedPunch, "Raise a missed-punch request; no out time is invented", "time.correction"),
	}
	def.Limits = workflow.Limits{MaxFanOut: 12, MaxDepth: 16, MaxNodes: 24,
		DeclaredCycles: []workflow.CycleDeclaration{{EntryNodeID: NodeCommitPunch, GuardNodeID: NodeSessionNextStep, MaxIterations: p.MaxPunchesPerSession}}}
	return def
}
