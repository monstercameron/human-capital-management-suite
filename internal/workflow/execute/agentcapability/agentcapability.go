package agentcapability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
)

// TaintAgentProduced marks every agent-produced node output. The taint travels
// in the answer's deterministic detail line, which the timeline renders next
// to what the run read, produced and cost.
const TaintAgentProduced = "AGENT_PRODUCED"

// capabilityIDPrefix namespaces agent-backed capabilities so a CAPABILITY node
// names an agent version exactly like any other capability.
const capabilityIDPrefix = "workflow.agent."

// CapabilityID returns the registry identity of an agent-backed capability.
func CapabilityID(agentID string) string { return capabilityIDPrefix + agentID }

// AgentStatus is the lifecycle of one published agent version. Only an active
// version runs; anything else fails closed at compile time and at run time.
type AgentStatus string

const (
	AgentStatusActive    AgentStatus = "ACTIVE"
	AgentStatusSuspended AgentStatus = "SUSPENDED"
	AgentStatusRetired   AgentStatus = "RETIRED"
)

// Budget is the spend ceiling the agent run executes under, from the agent's
// own published bindings.
type Budget struct {
	MaxCostMinor int64
	Currency     string
	MaxTokens    int64
}

// PublishedAgent is one evaluated, published agent version's bindings: the
// whole authority a workflow run may borrow. The registry manifest is
// generated from these bindings, never authored beside them, so the manifest
// cannot promise what the agent never published.
type PublishedAgent struct {
	AgentID   string
	Version   uint32
	Status    AgentStatus
	Evaluated bool

	InputSchema  capability.SchemaRef
	OutputSchema capability.SchemaRef
	DataScope    capability.DataDomainFieldSet
	Tools        []string
	Budget       Budget

	// ServicePrincipal is the identity the agent run executes as. The agent
	// never receives the workflow's credentials: the run request carries
	// this principal and the mapped inputs only.
	ServicePrincipal string

	// SideEffectingSkills states that the agent's skills can change records.
	// It defaults to false (read and draft only) and, when true, the
	// compiler refuses the node unless the path to it passes a gate.
	SideEffectingSkills bool

	// OutputFields are the declared typed outputs the result is validated
	// against before it becomes the node's output.
	OutputFields []workflow.Field

	// KillSwitch stops new runs when true and refuses in-flight nodes.
	KillSwitch bool
}

// Governance carries the registry-required references publication demands.
// They describe the capability's governance, never the agent's authority.
type Governance struct {
	OwnerDomain          string
	RiskClass            string
	IdempotencyPolicyRef string
	AuthZScopeRef        string
	LegalBasisRef        string
	EntitlementRef       string
	SLOClassRef          string
	TestRef              string
}

// CompileCheck refuses an agent capability at compile time: a version that is
// not active and evaluated never compiles, and a side-effecting agent
// compiles only when the path to its node passes a gate.
func CompileCheck(agent PublishedAgent, pathPassesGate bool) error {
	if agent.Status != AgentStatusActive {
		return fmt.Errorf("agentcapability: agent %s/v%d is %s, not ACTIVE", agent.AgentID, agent.Version, agent.Status)
	}
	if !agent.Evaluated {
		return fmt.Errorf("agentcapability: agent %s/v%d is not evaluated", agent.AgentID, agent.Version)
	}
	if agent.SideEffectingSkills && !pathPassesGate {
		return fmt.Errorf("agentcapability: agent %s/v%d has side-effecting skills and no gate on its path", agent.AgentID, agent.Version)
	}
	return nil
}

// ManifestDefinition generates the registry manifest from the agent's own
// bindings. By default the capability is READ_ONLY with no write scope: read
// and draft only. A side-effecting agent states INTERNAL_MUTATION with its
// write scope in the manifest.
func ManifestDefinition(agent PublishedAgent, gov Governance) (capability.Definition, error) {
	if err := CompileCheck(agent, true); err != nil {
		return capability.Definition{}, err
	}
	if agent.AgentID == "" {
		return capability.Definition{}, fmt.Errorf("agentcapability: agent id is required")
	}
	if agent.Version < 1 {
		return capability.Definition{}, fmt.Errorf("agentcapability: agent version must be >= 1")
	}
	if !agent.InputSchema.Valid() || !agent.OutputSchema.Valid() {
		return capability.Definition{}, fmt.Errorf("agentcapability: agent %s/v%d bindings lack valid input or output schemas", agent.AgentID, agent.Version)
	}
	if agent.ServicePrincipal == "" {
		return capability.Definition{}, fmt.Errorf("agentcapability: agent %s/v%d bindings lack a service principal", agent.AgentID, agent.Version)
	}
	effect := capability.EffectReadOnly
	var writes capability.DataDomainFieldSet
	if agent.SideEffectingSkills {
		effect = capability.EffectInternalMutation
		writes = agent.DataScope
	}
	return capability.Definition{
		ID:                   CapabilityID(agent.AgentID),
		Version:              agent.Version,
		OwnerDomain:          gov.OwnerDomain,
		RequestSchema:        agent.InputSchema,
		ResponseSchema:       agent.OutputSchema,
		ErrorSchema:          agent.OutputSchema,
		EffectClass:          effect,
		ReadData:             agent.DataScope,
		WriteData:            writes,
		RiskClass:            gov.RiskClass,
		IdempotencyPolicyRef: gov.IdempotencyPolicyRef,
		AgentEligible:        true,
		AuthZScopeRef:        gov.AuthZScopeRef,
		LegalBasisRef:        gov.LegalBasisRef,
		EntitlementRef:       gov.EntitlementRef,
		SLOClassRef:          gov.SLOClassRef,
		TestRef:              gov.TestRef,
	}, nil
}

// RunAccessPolicy is the workflow run's access policy as the handler consumes
// it: the domains and field paths the run may touch. WF-EXT-024 owns the
// durable policy; this shape is its read contract.
type RunAccessPolicy struct {
	AllowedDomains []string
	AllowedFields  []string
}

// IntersectScope limits the agent's data scope to the run access policy. The
// run executes inside the intersection, so a workflow can never give an agent
// authority the agent's own published bindings lack.
func IntersectScope(agent capability.DataDomainFieldSet, policy RunAccessPolicy) capability.DataDomainFieldSet {
	allowedDomains := make(map[string]struct{}, len(policy.AllowedDomains))
	for _, d := range policy.AllowedDomains {
		allowedDomains[d] = struct{}{}
	}
	allowedFields := make(map[string]struct{}, len(policy.AllowedFields))
	for _, f := range policy.AllowedFields {
		allowedFields[f] = struct{}{}
	}
	out := capability.DataDomainFieldSet{}
	for _, d := range agent.DataDomains {
		if _, ok := allowedDomains[d]; ok {
			out.DataDomains = append(out.DataDomains, d)
		}
	}
	for _, f := range agent.FieldPaths {
		if _, ok := allowedFields[f]; ok {
			out.FieldPaths = append(out.FieldPaths, f)
		}
	}
	// An agent that declares no scope runs scopeless, not forbidden: there is
	// nothing to intersect. An empty policy against a declared scope
	// intersects to nothing, which the runner enforces.
	if len(agent.DataDomains) == 0 && len(agent.FieldPaths) == 0 {
		return capability.DataDomainFieldSet{}
	}
	return out
}

// CauseOf derives the one cause identifier for a node call: the node, the
// exact capability version and the canonical typed inputs. Retries of a node
// present identical mappings, so they share one cause, one run and one
// charge.
func CauseOf(call capabilityrunner.CapabilityCall) string {
	paths := make([]string, 0, len(call.Inputs))
	for path := range call.Inputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	h := sha256.New()
	write := func(s string) {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	write("hcmnext.workflow.agent_cause/v1")
	write(call.NodeID)
	write(call.Capability.ID)
	write(fmt.Sprintf("%d", call.Capability.Version))
	for _, path := range paths {
		v := call.Inputs[path]
		write(path)
		write(v.Type.String())
		write(v.Text)
	}
	return "agentcause:" + hex.EncodeToString(h.Sum(nil))
}

// CauseID derives a cause identifier from explicit workflow identity for
// composition roots that carry the instance. The handler's default cause
// (CauseOf) is content-addressed so retries collide without instance
// plumbing.
func CauseID(tenant, instance, node string) string {
	sum := sha256.Sum256([]byte("hcmnext.workflow.agent_cause/v1\x00" + tenant + "\x00" + instance + "\x00" + node))
	return "agentcause:" + hex.EncodeToString(sum[:])
}

// AgentRunRequest is one governed agent run start. It carries the cause, the
// agent's service principal, the intersected scope and the mapped inputs. It
// deliberately has no credential field: the agent never receives the
// workflow's credentials.
type AgentRunRequest struct {
	CauseID          string
	NodeID           string
	AgentID          string
	AgentVersion     uint32
	ServicePrincipal string
	Scope            capability.DataDomainFieldSet
	Tools            []string
	Budget           Budget
	Inputs           map[string]capabilityrunner.ResolvedValue
}

// TypedText is one agent-produced typed value in canonical text form.
type TypedText struct {
	Type workflow.ValueType
	Text string
}

// AgentVerdict is the agent run's own disposition.
type AgentVerdict string

const (
	VerdictSucceeded AgentVerdict = "succeeded"
	VerdictRejected  AgentVerdict = "rejected"
	VerdictUnknown   AgentVerdict = "unknown"
	VerdictAmbiguous AgentVerdict = "ambiguous"
)

// MapVerdict maps an agent verdict to the capability outcome the driver
// routes on. An unrecognized verdict maps to UNKNOWN: the node takes its
// unknown route rather than invent a success.
func MapVerdict(v AgentVerdict) workflow.Outcome {
	switch v {
	case VerdictSucceeded:
		return workflow.OutcomeSucceeded
	case VerdictRejected:
		return workflow.OutcomeRejected
	case VerdictAmbiguous:
		return workflow.OutcomeAmbiguous
	default:
		return workflow.OutcomeUnknown
	}
}

// AgentRunResult is one completed agent run: its verdict, typed outputs, what
// it read, and its cost.
type AgentRunResult struct {
	RunID   string
	Verdict AgentVerdict
	Outputs map[string]TypedText
	Reads   []string
	Cost    string
}

// AgentRunner starts agent runs and re-attaches to recorded ones. Start must
// treat CauseID as an idempotency key: a repeated Start for one cause
// charges once. Attach resumes a run the ledger recorded when the node
// crashed between run start and node completion.
type AgentRunner interface {
	Start(ctx context.Context, req AgentRunRequest) (AgentRunResult, error)
	Attach(ctx context.Context, runID string) (AgentRunResult, error)
}

// RunLedger records the run one cause started, so retries and crash recovery
// attach instead of starting a second run.
type RunLedger interface {
	Lookup(causeID string) (runID string, ok bool)
	Record(causeID, runID string)
}

// MemoryLedger is an in-process RunLedger for composition roots without a
// durable run store yet.
type MemoryLedger struct {
	mu   sync.Mutex
	runs map[string]string
}

// NewMemoryLedger returns an empty ledger.
func NewMemoryLedger() *MemoryLedger { return &MemoryLedger{runs: map[string]string{}} }

// Lookup returns the recorded run for a cause.
func (l *MemoryLedger) Lookup(causeID string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	runID, ok := l.runs[causeID]
	return runID, ok
}

// Record binds a cause to its run. The first record wins: a racing retry
// attaches to the recorded run rather than starting its own.
func (l *MemoryLedger) Record(causeID, runID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.runs[causeID]; !ok {
		l.runs[causeID] = runID
	}
}

// Deps wires an agent capability to the agent runtime and the run's policy.
type Deps struct {
	// Runner starts and re-attaches agent runs. Required.
	Runner AgentRunner
	// Ledger records cause-to-run bindings. Required.
	Ledger RunLedger
	// Policy is the workflow run's access policy the agent scope intersects
	// with. Zero runs inside the agent's bindings alone.
	Policy RunAccessPolicy
	// Tenant is the tenant key the kill-switch check reads. Empty skips the
	// tenant check.
	Tenant string
	// StatusOf reports the live status of the agent version. Nil reads the
	// registered bindings. A suspension that lands after registration fails
	// closed at run time.
	StatusOf func(PublishedAgent) AgentStatus
	// CapabilityKilled reports a capability-level kill switch. Nil kills nothing.
	CapabilityKilled func(capability.Key) bool
	// TenantKilled reports a tenant-level kill switch. Nil kills nothing.
	TenantKilled func(tenant string) bool
	// CauseOf derives the cause identifier. Nil uses CauseOf.
	CauseOf func(capabilityrunner.CapabilityCall) string
}

func (d Deps) causeOf(call capabilityrunner.CapabilityCall) string {
	if d.CauseOf != nil {
		return d.CauseOf(call)
	}
	return CauseOf(call)
}

func (d Deps) statusOf(agent PublishedAgent) AgentStatus {
	if d.StatusOf != nil {
		return d.StatusOf(agent)
	}
	return agent.Status
}

// RegisterAgentCapability publishes one agent version as an agent-eligible
// capability whose handler runs the agent through deps.
func RegisterAgentCapability(registry *capability.Registry, agent PublishedAgent, gov Governance, deps Deps) error {
	if registry == nil {
		return fmt.Errorf("agentcapability: registry is required")
	}
	if deps.Runner == nil {
		return fmt.Errorf("agentcapability: runner is required")
	}
	if deps.Ledger == nil {
		return fmt.Errorf("agentcapability: ledger is required")
	}
	def, err := ManifestDefinition(agent, gov)
	if err != nil {
		return err
	}
	return registry.Register(def, Handler(agent, deps))
}

// fallbackForCause routes an in-flight agent node to its declared fallback
// route when a kill switch engages after its run started: the cause already
// binds a run in the ledger, so the node answers UNKNOWN (the safe-harbor
// outcome every CAPABILITY node declares an edge for) instead of failing.
// It touches neither Start nor Attach: a killed run is dead, and the answer
// carries no agent outputs. False means the cause never started: the caller
// refuses the new run.
func fallbackForCause(deps Deps, call capabilityrunner.CapabilityCall, scope string) (capabilityrunner.CapabilityAnswer, bool) {
	if deps.Ledger == nil {
		return capabilityrunner.CapabilityAnswer{}, false
	}
	cause := deps.causeOf(call)
	runID, ok := deps.Ledger.Lookup(cause)
	if !ok {
		return capabilityrunner.CapabilityAnswer{}, false
	}
	return capabilityrunner.CapabilityAnswer{
		Outcome: workflow.OutcomeUnknown,
		Outputs: map[string]capabilityrunner.ResolvedValue{},
		Detail:  fmt.Sprintf("agentcapability: %s kill switch engaged during run %s for cause %s: the node takes its fallback route", scope, runID, cause),
	}, true
}

// Handler serves a CAPABILITY node naming an agent version. It fails closed
// on kill switches and on a version that is no longer active, intersects the
// run policy with the agent's bindings, starts or resumes the one run for the
// cause, and schema-validates, grounds and taint-marks the result before it
// becomes the node's output.
func Handler(agent PublishedAgent, deps Deps) capability.Handler {
	return func(ctx context.Context, payload any) (any, error) {
		call, ok := payload.(capabilityrunner.CapabilityCall)
		if !ok {
			return nil, fmt.Errorf("agentcapability: payload is %T, want capabilityrunner.CapabilityCall", payload)
		}
		key := capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version}
		if agent.KillSwitch {
			if answer, ok := fallbackForCause(deps, call, fmt.Sprintf("agent %s", key)); ok {
				return answer, nil
			}
			return nil, fmt.Errorf("agentcapability: agent %s kill switch is engaged: new runs stop, in-flight nodes take their fallback", key)
		}
		if deps.CapabilityKilled != nil && deps.CapabilityKilled(key) {
			if answer, ok := fallbackForCause(deps, call, fmt.Sprintf("capability %s", key)); ok {
				return answer, nil
			}
			return nil, fmt.Errorf("agentcapability: capability %s kill switch is engaged: new runs stop, in-flight nodes take their fallback", key)
		}
		if deps.Tenant != "" && deps.TenantKilled != nil && deps.TenantKilled(deps.Tenant) {
			if answer, ok := fallbackForCause(deps, call, fmt.Sprintf("tenant %s", deps.Tenant)); ok {
				return answer, nil
			}
			return nil, fmt.Errorf("agentcapability: tenant %s kill switch is engaged: new runs stop, in-flight nodes take their fallback", deps.Tenant)
		}
		if status := deps.statusOf(agent); status != AgentStatusActive {
			return nil, fmt.Errorf("agentcapability: agent %s is %s at run time: suspended and retired versions fail closed", key, status)
		}
		if !agent.Evaluated {
			return nil, fmt.Errorf("agentcapability: agent %s is not evaluated", key)
		}

		cause := deps.causeOf(call)
		scope := IntersectScope(agent.DataScope, deps.Policy)
		var result AgentRunResult
		if runID, ok := deps.Ledger.Lookup(cause); ok {
			resumed, err := deps.Runner.Attach(ctx, runID)
			if err != nil {
				return nil, fmt.Errorf("agentcapability: attach run %s for cause %s: %w", runID, cause, err)
			}
			result = resumed
			result.RunID = runID
		} else {
			started, err := deps.Runner.Start(ctx, AgentRunRequest{
				CauseID:          cause,
				NodeID:           call.NodeID,
				AgentID:          agent.AgentID,
				AgentVersion:     agent.Version,
				ServicePrincipal: agent.ServicePrincipal,
				Scope:            scope,
				Tools:            append([]string(nil), agent.Tools...),
				Budget:           agent.Budget,
				Inputs:           cloneInputs(call.Inputs),
			})
			if err != nil {
				return nil, fmt.Errorf("agentcapability: start run for cause %s: %w", cause, err)
			}
			if started.RunID == "" {
				return nil, fmt.Errorf("agentcapability: runner started an unidentified run for cause %s", cause)
			}
			deps.Ledger.Record(cause, started.RunID)
			result = started
		}

		outputs, err := validatedOutputs(call.Outputs, result.Outputs)
		if err != nil {
			return nil, err
		}
		outcome := MapVerdict(result.Verdict)
		if outcome == workflow.OutcomeSucceeded && len(result.Reads) == 0 {
			// A success that cites nothing it read is ungrounded: it takes
			// the unknown route rather than launder an uncited answer into a
			// success.
			outcome = workflow.OutcomeUnknown
		}
		return capabilityrunner.CapabilityAnswer{
			Outcome: outcome,
			Outputs: outputs,
			Detail:  detail(agent, result, outcome),
		}, nil
	}
}

func cloneInputs(inputs map[string]capabilityrunner.ResolvedValue) map[string]capabilityrunner.ResolvedValue {
	out := make(map[string]capabilityrunner.ResolvedValue, len(inputs))
	for k, v := range inputs {
		out[k] = v
	}
	return out
}

// validatedOutputs schema-validates the agent result against the node's
// declared outputs: every declared field present with its declared type, and
// nothing undeclared. Anything else fails closed: an undeclared or mistyped
// value never becomes node output.
func validatedOutputs(declared []workflow.Field, produced map[string]TypedText) (map[string]capabilityrunner.ResolvedValue, error) {
	out := make(map[string]capabilityrunner.ResolvedValue, len(declared))
	for _, field := range declared {
		value, ok := produced[field.Path]
		if !ok {
			return nil, fmt.Errorf("agentcapability: agent result omits declared output %q", field.Path)
		}
		if value.Type != field.Type {
			return nil, fmt.Errorf("agentcapability: agent output %q has type %v, want %v", field.Path, value.Type, field.Type)
		}
		out[field.Path] = capabilityrunner.ResolvedValue{Type: value.Type, Text: value.Text}
	}
	for path := range produced {
		if _, ok := out[path]; !ok {
			return nil, fmt.Errorf("agentcapability: agent result carries undeclared output %q", path)
		}
	}
	return out, nil
}

// detail renders the deterministic timeline line for the run: what it read,
// what it produced, its cost, and the taint every agent output carries.
func detail(agent PublishedAgent, result AgentRunResult, outcome workflow.Outcome) string {
	produced := make([]string, 0, len(result.Outputs))
	for path := range result.Outputs {
		produced = append(produced, path)
	}
	sort.Strings(produced)
	reads := append([]string(nil), result.Reads...)
	sort.Strings(reads)
	cost := result.Cost
	if cost == "" {
		cost = "unstated"
	}
	return fmt.Sprintf("agent %s/v%d run %s: %s; read=[%s]; produced=[%s]; cost=%s; taint=%s",
		agent.AgentID, agent.Version, result.RunID, strings.ToLower(string(outcome)),
		strings.Join(reads, ","), strings.Join(produced, ","), cost, TaintAgentProduced)
}
