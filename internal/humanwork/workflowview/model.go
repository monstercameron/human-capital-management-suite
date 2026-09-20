// Package workflowview projects immutable workflow publications and the
// authorized runtime inspector into a presentation-safe, read-only graph.
//
// It deliberately owns no rendering and no workflow behavior. The browser,
// an accessible outline, an export, and a future native client can all consume
// the same projection without learning how compiled plans or inspector
// redactions are represented internally.
package workflowview

import "time"

// NodeState is the small visual vocabulary shared by the graph and outline.
// Raw runtime status remains available on Node.Status for precise copy.
type NodeState string

const (
	NodeNotStarted NodeState = "not-started"
	NodeCurrent    NodeState = "current"
	NodeRunning    NodeState = "running"
	NodeWaiting    NodeState = "waiting"
	NodeSucceeded  NodeState = "succeeded"
	NodeFailed     NodeState = "failed"
	NodeCancelled  NodeState = "cancelled"
	NodeUnknown    NodeState = "unknown"
)

// Route is one explicitly named transition leaving a node.
type Route struct {
	Key      string
	TargetID string
}

// Node is one compiled step plus the latest runtime attempt the authorized
// inspector disclosed. Depth and Lane form a deterministic, presentation-only
// layout; neither is workflow state.
type Node struct {
	ID           string
	Label        string
	StepType     string
	Depth        int
	Lane         int
	Start        bool
	Terminal     bool
	Current      bool
	Attempt      int
	Status       string
	State        NodeState
	Routes       []Route
	StartedAt    *time.Time
	CompletedAt  *time.Time
	RuntimeKnown bool
	RuntimeGap   bool
}

// Edge is one compiled, explicitly routed control-flow connection.
type Edge struct {
	ID       string
	FromID   string
	ToID     string
	RouteKey string
}

// View is the complete product projection. Nodes are in the compiled
// reachability order and are therefore also the accessible outline order.
// Edges never imply an ordering of their own.
type View struct {
	WorkflowID        string
	Name              string
	Version           uint32
	SemanticVersion   string
	PlanDigest        string
	PublicationStatus string

	Nodes []Node
	Edges []Edge

	HasRun        bool
	RunDisclosed  bool
	InstanceID    string
	RuntimeStatus string
	Completeness  bool
	Redactions    []string
	Gaps          []string

	MaxDepth int
	MaxLane  int
}
