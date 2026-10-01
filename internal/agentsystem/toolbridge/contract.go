package toolbridge

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

const MaxParallelCalls = 4

var (
	ErrInvalid           = errors.New("toolbridge: invalid request")
	ErrAdmission         = errors.New("toolbridge: run admission failed")
	ErrUnknownTool       = errors.New("toolbridge: tool is not exposed by the pinned run")
	ErrArguments         = errors.New("toolbridge: arguments do not match the pinned schema")
	ErrCost              = errors.New("toolbridge: call exceeds the admitted cost policy")
	ErrReplay            = errors.New("toolbridge: call nonce or idempotency binding was already used")
	ErrHumanConfirmation = errors.New("toolbridge: T2 action requires user confirmation")
	ErrOwner             = errors.New("toolbridge: capability owner failed")
)

// RunReference is a locator verified by the run-admission owner. SourceKey
// binds the model turn to the admitted source material; it carries no authority
// until Resolve verifies it.
type RunReference struct {
	RunID     string
	SourceKey string
}

// AdmittedRun is a server-resolved snapshot produced for this one model call.
// Its nonce is minted by the admission owner and is never accepted from model
// arguments. Gate contains current user context, not agent-owned authority.
type AdmittedRun struct {
	RunID                 string
	SourceKey             string
	Nonce                 string
	Gate                  agentgate.CallRequest
	Pins                  []agentskills.SkillPin
	RemainingBudgetMicros int64
	Sponsored             bool
}

// RunAdmission verifies the run/source binding and returns call-specific
// server state. CallID is provider-generated correlation data, not a credential.
type RunAdmission interface {
	Resolve(context.Context, RunReference, string) (AdmittedRun, error)
}

// SkillCatalog resolves immutable versions from AGENT2-004.
type SkillCatalog interface {
	ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error)
}

// CapabilityCatalog resolves the current lifecycle state of each exact owner
// operation captured by a skill manifest.
type CapabilityCatalog interface {
	Lookup(capability.Key) (capability.Record, bool)
}

// SkillGate rechecks the signed-in user's current grant, purpose, consent and
// per-capability authorization on every discovery and invocation.
type SkillGate interface {
	Authorize(context.Context, agentgate.CallRequest) (agentgate.CallDecision, error)
}

// CostPolicy checks the pinned skill's declared cost class against the
// current run budget without reserving or consuming it.
type CostPolicy interface {
	Check(context.Context, agentskills.SkillRecord, int64) error
}

// ReplayGuard atomically claims one call binding. Implementations must reject
// both a repeated key and a key reused with a different digest; they must never
// treat a retry as a fresh capability execution.
type ReplayGuard interface {
	Claim(context.Context, ReplayKey) error
}

// ReplayKey binds one model call to its admitted source and exact arguments.
type ReplayKey struct {
	RunID        string
	SourceKey    string
	CallID       string
	Nonce        string
	ToolName     string
	ArgumentsSHA string
}

// CapabilityCall is the typed owner boundary. Skill is the immutable versioned
// manifest, Capability is the exact registered owner operation, and Arguments
// has already passed the pinned input schema. Owners still validate their own
// domain request before applying any effect.
type CapabilityCall struct {
	RunID          string
	CallID         string
	Purpose        string
	Decision       agentgate.CallDecision
	Skill          agentskills.SkillRecord
	Capability     capability.Record
	Arguments      json.RawMessage
	IdempotencyKey string
}

// CapabilityOwner is the only port that can invoke a business capability.
type CapabilityOwner interface {
	Invoke(context.Context, CapabilityCall) (any, error)
}

// ReviewRequest is the immutable proposal handoff for T3/T4 approval or T2
// destination confirmation. The bridge never interprets the returned handle as
// approval and never invokes the owner for these tiers.
type ReviewRequest struct {
	RunID          string
	CallID         string
	SourceKey      string
	Purpose        string
	Tier           agentskills.SideEffectTier
	Skill          agentskills.SkillRecord
	Arguments      json.RawMessage
	ArgumentsSHA   string
	Decision       agentgate.CallDecision
	IdempotencyKey string
}

// HumanReview creates the appropriate product-owned confirmation or exact
// AGENT2-006 approval item. A returned reference is pending, never consent.
type HumanReview interface {
	Request(context.Context, ReviewRequest) (string, error)
}

// ToolCall aliases the provider-neutral untrusted model proposal.
type ToolCall = agentmodel.ToolProposal

// Outcome is either a validated owner result or a pending human review.
type Outcome struct {
	CallID        string
	ToolName      string
	Output        json.RawMessage
	ReviewID      string
	ReviewPending bool
}
