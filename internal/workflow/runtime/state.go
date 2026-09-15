package runtime

import "sort"

// InstanceStatus is the runtime status of one workflow instance.
//
// It is a superset of internal/workflow.RuntimeStatus, which enumerates only
// the statuses an END node may declare. An instance additionally passes
// through CREATED, RUNNING, WAITING, PAUSE_REQUESTED, PAUSED and CANCELLING
// on the way to one of those, and none of those six is ever a terminal an END
// node can name.
type InstanceStatus string

// Instance runtime statuses, spelled exactly as the state diagram in
// planning/specs/workflow-runtime.md "Durable Runtime State" spells them.
const (
	InstanceCreated        InstanceStatus = "CREATED"
	InstanceRunning        InstanceStatus = "RUNNING"
	InstanceWaiting        InstanceStatus = "WAITING"
	InstancePauseRequested InstanceStatus = "PAUSE_REQUESTED"
	InstancePaused         InstanceStatus = "PAUSED"
	InstanceCancelling     InstanceStatus = "CANCELLING"
	InstanceBlocked        InstanceStatus = "BLOCKED"
	InstanceCompleted      InstanceStatus = "COMPLETED"
	InstanceCancelled      InstanceStatus = "CANCELLED"
	InstanceRepairRequired InstanceStatus = "REPAIR_REQUIRED"
	InstanceQuarantined    InstanceStatus = "QUARANTINED"
	InstanceSuperseded     InstanceStatus = "SUPERSEDED"
)

// instanceTransitions is the spec's instance state machine, read literally:
//
//	CREATED -> RUNNING -> WAITING -> RUNNING -> COMPLETED
//	              |          |
//	              |          +-> PAUSE_REQUESTED -> PAUSED
//	              +-> BLOCKED / REPAIR_REQUIRED / QUARANTINED
//	              +-> CANCELLING -> CANCELLED / REPAIR_REQUIRED
//	              +-> SUPERSEDED
//
// Two readings are made explicit rather than left implicit. The branches that
// hang off RUNNING also hang off WAITING, because a waiting instance is as
// cancellable, blockable and supersedable as a running one and the diagram
// draws the branch on the chain, not on one node of it. And PAUSED returns to
// RUNNING: a pause that could not be resumed would be a cancellation with a
// friendlier name.
//
// Everything else is refused. In particular COMPLETED, CANCELLED,
// REPAIR_REQUIRED, QUARANTINED and SUPERSEDED have no outgoing edge at all:
// history is immutable, and a repair is a new instance, not a resurrected one.
// A third reading is made explicit by WF-RUN-008. PAUSE_REQUESTED hangs off
// CREATED and RUNNING as well as WAITING, because this runtime records a
// parked instance as RUNNING with a work-item continuation and never as
// WAITING, and a pause request that could only be made in a state the
// runtime never durably occupies would be unrequestable. And a
// PAUSE_REQUESTED instance keeps every outgoing edge a RUNNING one has apart
// from RUNNING itself: a pause request is an overlay on a live instance, not
// a suspension of it, so the atomic region it is standing inside may still
// finish, block, need repair or be cancelled while the request stands.
// PAUSE_REQUESTED -> RUNNING is deliberately absent: it is the only edge that
// would silently drop a standing pause request.
var instanceTransitions = map[InstanceStatus][]InstanceStatus{
	InstanceCreated: {InstanceRunning, InstancePauseRequested, InstanceCancelling, InstanceSuperseded},
	InstanceRunning: {
		InstanceWaiting, InstancePauseRequested, InstanceCompleted, InstanceBlocked,
		InstanceRepairRequired, InstanceQuarantined, InstanceCancelling, InstanceSuperseded,
	},
	InstanceWaiting: {
		InstanceRunning, InstancePauseRequested, InstanceBlocked, InstanceRepairRequired,
		InstanceQuarantined, InstanceCancelling, InstanceSuperseded,
	},
	InstancePauseRequested: {
		InstancePaused, InstanceCompleted, InstanceBlocked, InstanceRepairRequired,
		InstanceQuarantined, InstanceCancelling, InstanceSuperseded,
	},
	InstancePaused:     {InstanceRunning, InstanceCancelling, InstanceSuperseded},
	InstanceCancelling: {InstanceCancelled, InstanceRepairRequired},
	InstanceBlocked: {
		InstanceRunning, InstanceRepairRequired, InstanceQuarantined, InstanceCancelling,
	},
	InstanceCompleted:      nil,
	InstanceCancelled:      nil,
	InstanceRepairRequired: nil,
	InstanceQuarantined:    nil,
	InstanceSuperseded:     nil,
}

// Valid reports whether s is a declared instance status.
func (s InstanceStatus) Valid() bool {
	_, ok := instanceTransitions[s]
	return ok
}

// Terminal reports whether s ends the instance. A terminal status has no
// outgoing transition and is the only kind of status that may carry a
// completion instant.
func (s InstanceStatus) Terminal() bool {
	return s.Valid() && len(instanceTransitions[s]) == 0
}

// LegalInstanceTransition reports whether from -> to is allowed. A status may
// always be re-recorded as itself: recording the frontier twice at the same
// status is an idempotent write, not a transition.
func LegalInstanceTransition(from, to InstanceStatus) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	if from == to {
		return true
	}
	for _, next := range instanceTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// cancellingHop reports whether an advancement from -> to must first record
// CANCELLING: to is CANCELLED, from may not reach it directly, and from may
// begin cancelling. It returns the hop status when one is needed.
func cancellingHop(from, to InstanceStatus) (InstanceStatus, bool) {
	if to != InstanceCancelled || LegalInstanceTransition(from, to) || !LegalInstanceTransition(from, InstanceCancelling) {
		return "", false
	}
	return InstanceCancelling, true
}

// InstanceStatuses returns every declared instance status, sorted, so a
// migration CHECK constraint and a test can be compared against one list.
func InstanceStatuses() []InstanceStatus {
	out := make([]InstanceStatus, 0, len(instanceTransitions))
	for s := range instanceTransitions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// NodeStatus is the status of one node execution attempt.
type NodeStatus string

// Node execution statuses, from planning/specs/workflow-runtime.md "Durable
// Node Execution".
const (
	NodeReady       NodeStatus = "READY"
	NodeRunning     NodeStatus = "RUNNING"
	NodeWaiting     NodeStatus = "WAITING"
	NodeSucceeded   NodeStatus = "SUCCEEDED"
	NodeFailed      NodeStatus = "FAILED"
	NodeRetrying    NodeStatus = "RETRYING"
	NodeSkipped     NodeStatus = "SKIPPED"
	NodeOverridden  NodeStatus = "OVERRIDDEN"
	NodeCompensated NodeStatus = "COMPENSATED"
	NodeCancelled   NodeStatus = "CANCELLED"
)

// nodeTransitions is the spec's node state machine:
//
//	READY -> RUNNING -> SUCCEEDED
//	          |
//	          +-> WAITING
//	          +-> FAILED -> RETRYING -> READY
//	          +-> SKIPPED / OVERRIDDEN / COMPENSATED / CANCELLED
//
// NodeRetrying is a persisted state, not a scheduler: nothing in this phase
// computes a backoff, writes a retry instant or wakes a node up. A driver that
// decides to retry records RETRYING and then records the next attempt's READY
// itself. WF-RUN-000 gates the machinery that would do it automatically.
var nodeTransitions = map[NodeStatus][]NodeStatus{
	NodeReady: {NodeRunning, NodeSkipped, NodeOverridden, NodeCancelled},
	NodeRunning: {
		NodeSucceeded, NodeWaiting, NodeFailed, NodeSkipped, NodeOverridden,
		NodeCompensated, NodeCancelled,
	},
	NodeWaiting:  {NodeRunning, NodeFailed, NodeCancelled},
	NodeFailed:   {NodeRetrying, NodeOverridden, NodeCancelled},
	NodeRetrying: {NodeReady},
	// A successful node may still be compensated later; nothing else follows a
	// finished node.
	NodeSucceeded:   {NodeCompensated},
	NodeSkipped:     nil,
	NodeOverridden:  nil,
	NodeCompensated: nil,
	NodeCancelled:   nil,
}

// Valid reports whether s is a declared node status.
func (s NodeStatus) Valid() bool {
	_, ok := nodeTransitions[s]
	return ok
}

// Terminal reports whether s ends the attempt.
func (s NodeStatus) Terminal() bool {
	return s.Valid() && len(nodeTransitions[s]) == 0
}

// Finished reports whether s is a status that may carry a completion instant.
// SUCCEEDED and FAILED are finished without being terminal: a succeeded node
// can still be compensated, and a failed one can still be retried into a new
// attempt.
func (s NodeStatus) Finished() bool {
	switch s {
	case NodeSucceeded, NodeFailed, NodeSkipped, NodeOverridden, NodeCompensated, NodeCancelled:
		return true
	default:
		return false
	}
}

// LegalNodeTransition reports whether from -> to is allowed, treating a
// re-record of the same status as legal.
func LegalNodeTransition(from, to NodeStatus) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	if from == to {
		return true
	}
	for _, next := range nodeTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// NodeStatuses returns every declared node status, sorted.
func NodeStatuses() []NodeStatus {
	out := make([]NodeStatus, 0, len(nodeTransitions))
	for s := range nodeTransitions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
