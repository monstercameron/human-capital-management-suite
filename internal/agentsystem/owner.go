package agentsystem

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// ToolOwner is the boundary to the systems that own the data and effects:
// capability owners, connector adapters and the approval surface. The gate
// asks it what a step wants to send and write, and only then, with a
// verified delegated credential in hand, asks it to perform the call. It is
// also the agentrun.OwnerVerifier for VERIFY steps.
type ToolOwner interface {
	agentrun.OwnerVerifier
	// Prepare describes the step's outbound payload, write arguments and
	// caller identity. Nothing is sent or written by Prepare.
	Prepare(ctx context.Context, req PrepareRequest) (Prepared, error)
	// Invoke performs the owner call. It is reached only after the skill
	// gate, egress and (for writes) argument binding have all passed.
	Invoke(ctx context.Context, call Invocation) (Result, error)
	// Retain stores a quarantined extraction under ref so later steps read
	// typed, tainted values instead of raw external content.
	Retain(ctx context.Context, task agentrun.AgentTask, step agentrun.PlanStep, ref string, extraction agentsecurity.QuarantineExtraction) error
}

// PrepareRequest carries the step and the pinned skill it runs.
type PrepareRequest struct {
	Task  agentrun.AgentTask
	Step  agentrun.PlanStep
	Skill agentskills.SkillRecord
	// Context is rebuilt from the durable ledger before every model step.
	// The owner must declare its fields through egress before model disclosure.
	Context *agentrun.TaskContext
}

// TaskSourceReader performs fresh owner reads under this task's authority.
// Owners that retain source-derived ledger entries implement this seam.
type TaskSourceReader interface {
	ReadTaskSource(context.Context, agentrun.AgentTask, string) (agentrun.OwnerRead, error)
}

// Prepared is the owner's declaration for one step.
type Prepared struct {
	Purpose string
	// Egress is required for model steps and for connection steps.
	Egress *EgressCall
	// Write is required for tiers T2 to T4.
	Write *WriteCall
	// User, Operation and Destination are required when the step names a
	// system connection.
	User        agentconnect.UserContext
	Operation   custody.Operation
	Destination string
}

// EgressCall is the declared outbound payload and its target policy.
type EgressCall struct {
	Profile        agentegress.Profile
	Region         string
	DeclaredFields []string
	Fields         []agentegress.Field
	Task           agentegress.TaskPolicy
}

// WriteCall holds the exact arguments of a T2 to T4 effect, the user's
// approval card for them, and the current-authority validator.
type WriteCall struct {
	Args     []agentsecurity.WriteArgument
	Approval *agentsecurity.WriteApprovalCard
	Owner    agentsecurity.WriteOwner
}

// Invocation is everything the owner receives for an admitted call.
type Invocation struct {
	Task       agentrun.AgentTask
	Step       agentrun.PlanStep
	Skill      agentskills.SkillRecord
	Claims     agentdelegation.Claims
	Credential string
	// Payload is the minimized outbound payload when Prepared.Egress was set.
	Payload []byte
	// Lease and Evidence are set for connection steps.
	Lease    *agentconnect.CallLease
	Evidence *lease.Evidence
	// Write holds the bound arguments for T2 to T4.
	Write []agentsecurity.WriteArgument
}

// Result is the owner's answer. Content is raw untrusted text (an email
// body, a connector record) and is admitted only through quarantine;
// Typed is an already validated, tainted connection result.
type Result struct {
	Ref    string
	Digest string
	// Content and Source describe untrusted text; Schema is the extraction
	// schema it is reduced to. Content without a schema is refused.
	Content  string
	Source   agentsecurity.SourceKind
	SourceID string
	Schema   agentsecurity.ExtractionSchema
	// Inbound is the classified connection result, checked by egress.
	Inbound *InboundResult
}

// InboundResult is a typed connection result awaiting egress clearance.
type InboundResult struct {
	Profile agentegress.Profile
	Region  string
	Task    agentegress.TaskPolicy
	Result  agentsecurity.TypedResult
	Fields  []agentegress.Field
}
