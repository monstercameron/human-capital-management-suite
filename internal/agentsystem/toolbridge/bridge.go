package toolbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// Config lists every required trust and effect boundary. There are no default
// allow implementations.
type Config struct {
	Admission    RunAdmission
	Skills       SkillCatalog
	Capabilities CapabilityCatalog
	Gate         SkillGate
	Costs        CostPolicy
	Replay       ReplayGuard
	Owner        CapabilityOwner
	Review       HumanReview
}

// Bridge validates model proposals before reaching the owner port.
type Bridge struct{ cfg Config }

// New refuses partial wiring so an omitted authorization or replay boundary
// cannot silently become an allow path.
func New(cfg Config) (*Bridge, error) {
	if cfg.Admission == nil || cfg.Skills == nil || cfg.Capabilities == nil || cfg.Gate == nil || cfg.Costs == nil || cfg.Replay == nil || cfg.Owner == nil || cfg.Review == nil {
		return nil, fmt.Errorf("%w: all admission, skill, capability, gate, cost, replay, owner and review ports are required", ErrInvalid)
	}
	return &Bridge{cfg: cfg}, nil
}

// ExecuteBatch preflights the full proposal set before any owner is invoked.
// It is intentionally serial after preflight; untrusted parallel bursts cannot
// fan out capability calls.
func (b *Bridge) ExecuteBatch(ctx context.Context, ref RunReference, calls []ToolCall) ([]Outcome, error) {
	if b == nil || ctx == nil || strings.TrimSpace(ref.RunID) == "" || strings.TrimSpace(ref.SourceKey) == "" || len(calls) == 0 || len(calls) > MaxParallelCalls {
		return nil, fmt.Errorf("%w: run, source and one to %d calls are required", ErrInvalid, MaxParallelCalls)
	}
	prepared := make([]preparedCall, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if _, ok := seen[call.ID]; call.ID != "" && ok {
			return nil, fmt.Errorf("%w: duplicate call id", ErrInvalid)
		}
		seen[call.ID] = struct{}{}
		item, err := b.prepare(ctx, ref, call)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}
	for _, item := range prepared {
		if err := b.cfg.Replay.Claim(ctx, item.replay); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrReplay, err)
		}
	}
	out := make([]Outcome, 0, len(prepared))
	for _, item := range prepared {
		result, err := b.run(ctx, item)
		if err != nil {
			return nil, err
		}
		out = append(out, result)
	}
	return out, nil
}

// Execute admits one proposal through the same batch preflight path.
func (b *Bridge) Execute(ctx context.Context, ref RunReference, call ToolCall) (Outcome, error) {
	out, err := b.ExecuteBatch(ctx, ref, []ToolCall{call})
	if err != nil {
		return Outcome{}, err
	}
	return out[0], nil
}

// Tools returns only exact skill versions pinned to an admitted run and
// currently authorized for the signed-in user and purpose.
func (b *Bridge) Tools(ctx context.Context, ref RunReference) ([]agentskills.MCPTool, error) {
	if b == nil || ctx == nil || strings.TrimSpace(ref.RunID) == "" || strings.TrimSpace(ref.SourceKey) == "" {
		return nil, ErrInvalid
	}
	run, err := b.cfg.Admission.Resolve(ctx, ref, "tool-discovery")
	if err != nil || !validRun(run, ref) {
		return nil, ErrAdmission
	}
	tools := make([]agentskills.MCPTool, 0, len(run.Pins))
	seen := make(map[agentskills.SkillKey]struct{}, len(run.Pins))
	for _, pin := range run.Pins {
		if _, duplicate := seen[pin.Key()]; duplicate {
			return nil, fmt.Errorf("%w: duplicate pinned skill", ErrAdmission)
		}
		seen[pin.Key()] = struct{}{}
		skill, err := b.cfg.Skills.ResolvePin(pin)
		if err != nil || skill.Status == agentskills.StatusRetired || validateTier(skill) != nil ||
			validateSchemaDocument(skill.Definition.InputSchema) != nil ||
			validateSchemaDocument(skill.Definition.OutputSchema) != nil || b.refreshCapabilities(&skill) != nil {
			continue
		}
		gateReq := run.Gate
		gateReq.Skill = pin
		if gateReq.Purpose == "" || skillPurpose(run, skill) == "" || gateReq.Actor.RunID != run.RunID {
			continue
		}
		decision, err := b.cfg.Gate.Authorize(ctx, gateReq)
		if err != nil || !validDecision(decision, skill, gateReq.Purpose) {
			continue
		}
		tools = append(tools, agentskills.MCPTool{
			Name: agentskills.MCPToolName(pin.Key()), Description: skill.Definition.Description,
			InputSchema:  append(json.RawMessage(nil), skill.Definition.InputSchema...),
			OutputSchema: append(json.RawMessage(nil), skill.Definition.OutputSchema...),
			Annotations:  agentskills.MCPToolAnnotations{ReadOnlyHint: skill.Definition.SideEffectTier == agentskills.TierRead, DestructiveHint: skill.Definition.SideEffectTier >= agentskills.TierSubmitGoverned},
		})
	}
	return tools, nil
}

// ModelTools adapts the authorized pinned projection to the provider-neutral
// model request contract. The proposal still returns to Execute for fresh
// admission; tool schemas never grant execution authority by themselves.
func (b *Bridge) ModelTools(ctx context.Context, ref RunReference) ([]agentmodel.ToolSchema, error) {
	projection, err := b.Tools(ctx, ref)
	if err != nil {
		return nil, err
	}
	tools := make([]agentmodel.ToolSchema, 0, len(projection))
	for _, tool := range projection {
		tools = append(tools, agentmodel.ToolSchema{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.InputSchema...)})
	}
	return tools, nil
}

type preparedCall struct {
	run      AdmittedRun
	call     ToolCall
	skill    agentskills.SkillRecord
	decision agentgate.CallDecision
	args     json.RawMessage
	digest   string
	replay   ReplayKey
}

func (b *Bridge) prepare(ctx context.Context, ref RunReference, call ToolCall) (preparedCall, error) {
	if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
		return preparedCall{}, fmt.Errorf("%w: provider call id and tool name are required", ErrInvalid)
	}
	run, err := b.cfg.Admission.Resolve(ctx, ref, call.ID)
	if err != nil || !validRun(run, ref) {
		return preparedCall{}, fmt.Errorf("%w: source or call nonce is not admitted", ErrAdmission)
	}
	pin, ok := pinnedTool(run.Pins, call.Name)
	if !ok {
		return preparedCall{}, ErrUnknownTool
	}
	skill, err := b.cfg.Skills.ResolvePin(pin)
	if err != nil || skill.Status == agentskills.StatusRetired || b.refreshCapabilities(&skill) != nil {
		return preparedCall{}, fmt.Errorf("%w: pinned skill version is unavailable", ErrUnknownTool)
	}
	args, digest, err := validateArguments(skill.Definition.InputSchema, call.Arguments)
	if err != nil {
		return preparedCall{}, fmt.Errorf("%w: input schema rejected proposal", ErrArguments)
	}
	if run.Sponsored && skill.Definition.SideEffectTier >= agentskills.TierSubmitGoverned {
		return preparedCall{}, fmt.Errorf("%w: sponsored runs cannot request T3/T4", ErrHumanConfirmation)
	}
	if err := validateTier(skill); err != nil {
		return preparedCall{}, err
	}
	gateReq := run.Gate
	gateReq.Skill = pin
	if gateReq.Purpose == "" || gateReq.Purpose != skillPurpose(run, skill) || gateReq.Actor.RunID != run.RunID {
		return preparedCall{}, fmt.Errorf("%w: purpose or actor does not match admitted run", ErrAdmission)
	}
	decision, err := b.cfg.Gate.Authorize(ctx, gateReq)
	if err != nil || !validDecision(decision, skill, gateReq.Purpose) {
		if err == nil {
			err = ErrAdmission
		}
		return preparedCall{}, err
	}
	if err := b.cfg.Costs.Check(ctx, skill, run.RemainingBudgetMicros); err != nil {
		return preparedCall{}, fmt.Errorf("%w: cost policy denied call", ErrCost)
	}
	key := ReplayKey{RunID: run.RunID, SourceKey: run.SourceKey, CallID: call.ID, Nonce: run.Nonce, ToolName: call.Name, ArgumentsSHA: digest}
	return preparedCall{run: run, call: call, skill: skill, decision: decision, args: args, digest: digest, replay: key}, nil
}

func (b *Bridge) run(ctx context.Context, item preparedCall) (Outcome, error) {
	tier := item.skill.Definition.SideEffectTier
	if tier == agentskills.TierCommunicate || tier >= agentskills.TierSubmitGoverned {
		key := idempotencyKey(item.replay)
		reviewID, err := b.cfg.Review.Request(ctx, ReviewRequest{
			RunID: item.run.RunID, CallID: item.call.ID, SourceKey: item.run.SourceKey,
			Purpose: item.run.Gate.Purpose, Tier: tier, Skill: item.skill,
			Arguments: append(json.RawMessage(nil), item.args...), ArgumentsSHA: item.digest,
			Decision: item.decision, IdempotencyKey: key,
		})
		if err != nil {
			return Outcome{}, err
		}
		if strings.TrimSpace(reviewID) == "" {
			return Outcome{}, fmt.Errorf("%w: review port returned no pending reference", ErrInvalid)
		}
		return Outcome{CallID: item.call.ID, ToolName: item.call.Name, ReviewID: reviewID, ReviewPending: true}, nil
	}
	if len(item.skill.ResolvedOperations) != 1 || !item.skill.ResolvedOperations[0].HasCapability {
		return Outcome{}, fmt.Errorf("%w: skill needs exactly one registered capability operation", ErrInvalid)
	}
	capabilityRecord := item.skill.ResolvedOperations[0].Capability
	if capabilityRecord.Status == capability.StatusRetired || !capabilityRecord.Definition.AgentEligible || capabilityRecord.Definition.EffectClass.IsWrite() {
		return Outcome{}, fmt.Errorf("%w: capability is retired, ineligible or has a side effect", ErrInvalid)
	}
	response, err := b.cfg.Owner.Invoke(ctx, CapabilityCall{
		RunID: item.run.RunID, CallID: item.call.ID, Purpose: item.run.Gate.Purpose,
		Decision: item.decision, Skill: item.skill, Capability: capabilityRecord,
		Arguments: append(json.RawMessage(nil), item.args...), IdempotencyKey: idempotencyKey(item.replay),
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: %v", ErrOwner, err)
	}
	output, err := json.Marshal(response)
	if err != nil || validateJSONSchema(item.skill.Definition.OutputSchema, output) != nil {
		return Outcome{}, fmt.Errorf("%w: owner result failed pinned output schema", ErrArguments)
	}
	return Outcome{CallID: item.call.ID, ToolName: item.call.Name, Output: output}, nil
}

func validRun(run AdmittedRun, ref RunReference) bool {
	return run.RunID == ref.RunID && run.SourceKey == ref.SourceKey && strings.TrimSpace(run.Nonce) != "" &&
		run.Gate.Actor.RunID == run.RunID && run.Gate.Purpose != "" && len(run.Pins) > 0 && run.RemainingBudgetMicros > 0
}

func skillPurpose(run AdmittedRun, skill agentskills.SkillRecord) string {
	for _, purpose := range skill.Definition.RequiredPurposes {
		if purpose == run.Gate.Purpose {
			return purpose
		}
	}
	return ""
}

func pinnedTool(pins []agentskills.SkillPin, name string) (agentskills.SkillPin, bool) {
	for _, pin := range pins {
		if agentskills.MCPToolName(pin.Key()) == name {
			return pin, true
		}
	}
	return agentskills.SkillPin{}, false
}

func validateTier(skill agentskills.SkillRecord) error {
	if !skill.Definition.SideEffectTier.Valid() || skill.Definition.SideEffectTier < skill.HighestCapabilityTier {
		return fmt.Errorf("%w: declared side-effect tier understates its capabilities", ErrInvalid)
	}
	return nil
}

func (b *Bridge) refreshCapabilities(skill *agentskills.SkillRecord) error {
	if skill == nil || len(skill.ResolvedOperations) != 1 {
		return ErrInvalid
	}
	operation := &skill.ResolvedOperations[0]
	if !operation.HasCapability {
		return ErrInvalid
	}
	current, ok := b.cfg.Capabilities.Lookup(operation.Reference.Capability)
	if !ok || current.Status == capability.StatusRetired || !current.Definition.AgentEligible || current.Digest == "" || current.Digest != operation.Capability.Digest {
		return ErrInvalid
	}
	operation.Capability = current
	return nil
}

func validDecision(decision agentgate.CallDecision, skill agentskills.SkillRecord, purpose string) bool {
	if decision.Skill != skill.Definition.Key() || decision.Purpose != purpose || strings.TrimSpace(decision.GrantID) == "" || len(decision.Capabilities) != len(skill.ResolvedOperations) {
		return false
	}
	for i, operation := range skill.ResolvedOperations {
		if decision.Capabilities[i].Capability != operation.Capability.Definition.Key() || !decision.Capabilities[i].Allowed {
			return false
		}
	}
	return true
}

func idempotencyKey(key ReplayKey) string {
	value := strings.Join([]string{"agent-tool-v1", key.RunID, key.SourceKey, key.CallID, key.Nonce, key.ToolName}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
