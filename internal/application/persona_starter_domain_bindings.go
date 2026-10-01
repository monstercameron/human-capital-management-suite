package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/availability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaOnboardingPort is the narrow application seam for the sealed
// workerlifecycle plan and its governed child task projection. It is supplied
// by composition; the starter skill never fabricates checklist state.
type PersonaOnboardingPort interface {
	Checklist(context.Context, values.EntityRef) (workerlifecycle.ReadinessResolution, error)
	Tasks(context.Context, values.EntityRef) ([]workerlifecycle.ChildIntent, error)
	DraftWelcome(context.Context, values.EntityRef) (string, error)
	PrepareWelcomePost(context.Context, *trust.Principal, values.EntityRef, string, string) (PersonaWelcomePostIntent, error)
}

type PersonaOnboardingCall struct{ Worker values.EntityRef }
type PersonaOnboardingTasksResult struct{ Tasks []PersonaOnboardingTaskState }

// PersonaOnboardingTaskState preserves the lifecycle owner's retained state.
// A pending child has no emitted intent or observation identity.
type PersonaOnboardingTaskState struct {
	ChildID        string
	Intent         *workerlifecycle.ChildIntent `json:",omitempty"`
	State          workerlifecycle.ChildState
	Outcome        workerlifecycle.ChildOutcome `json:",omitempty"`
	ObservationRef string                       `json:",omitempty"`
}

type PersonaOnboardingTaskStatePort interface {
	TaskStates(context.Context, values.EntityRef) ([]PersonaOnboardingTaskState, error)
}
type PersonaReadinessResult struct {
	RequirementID string
	Owner         string
	Kind          string
	Status        string
	Blocker       string
	ObservedAt    string
	Evidence      string
	Summary       string
	Protected     bool
}
type PersonaReadinessDTO struct {
	PlanDigest string
	AsOf       string
	Results    []PersonaReadinessResult
	Aggregate  string
	Blockers   []string
	Waivers    []workerlifecycle.Waiver
	Digest     string
}
type PersonaWelcomeDraftCall struct {
	Worker values.EntityRef
}
type PersonaWelcomeDraftResult struct{ Draft string }
type PersonaWelcomePostCall struct {
	Worker  values.EntityRef
	Draft   string
	Channel string
}

type PersonaWelcomePostIntent struct {
	Worker                values.EntityRef
	WorkerRevision        values.RevisionToken
	SnapshotRevision      uint64
	PlanDigest            string
	Channel               string
	ChannelRevision       uint64
	ChannelPolicyRevision int64
	Draft                 string
}

// BindPersonaOnboardingSkills publishes only when the real lifecycle and chat
// adapters are composed. The post adapter owns the T2 authorization check.
func BindPersonaOnboardingSkills(caps *capability.Registry, skills *agentskills.Registry, port PersonaOnboardingPort) ([]agentskills.SkillPin, error) {
	if caps == nil || skills == nil || port == nil {
		return nil, errPersonaStarterBinding
	}
	if _, ok := port.(PersonaOnboardingTaskStatePort); !ok {
		return nil, errPersonaStarterBinding
	}
	items := []struct {
		def capability.Definition
		h   capability.Handler
		s   agentskills.SkillDefinition
	}{
		{personaStarterDefinition("hcmnext.persona.onboarding_checklist", "workerlifecycle", capability.EffectReadOnly, "ONBOARDING"), personaOnboardingChecklistHandler{port}.handle, personaStarterOperationSkill("hcmnext.skill.onboarding_checklist_read", "Read the authorized onboarding checklist.", "hcmnext.persona.onboarding_checklist", agentskills.TierT0, "ONBOARDING")},
		{personaStarterDefinition("hcmnext.persona.onboarding_tasks", "workerlifecycle", capability.EffectReadOnly, "ONBOARDING"), personaOnboardingTasksHandler{port}.handle, personaStarterOperationSkill("hcmnext.skill.onboarding_task_read", "Read authorized onboarding tasks.", "hcmnext.persona.onboarding_tasks", agentskills.TierT0, "ONBOARDING")},
		{personaStarterDefinition("hcmnext.persona.welcome_note_draft", "workerlifecycle", capability.EffectPure, "ONBOARDING"), personaWelcomeDraftHandler{port}.handle, personaStarterOperationSkill("hcmnext.skill.welcome_note_draft", "Draft a private welcome note.", "hcmnext.persona.welcome_note_draft", agentskills.TierT1, "ONBOARDING")},
		// The capability prepares a governed post intent. Actual delivery is
		// admitted by the existing sealed persona output gateway after T2
		// confirmation, so this capability remains pure and can stay T2.
		{personaStarterDefinition("hcmnext.persona.onboarding_channel_post", "collaboration", capability.EffectPure, "ONBOARDING"), personaWelcomePostHandler{port}.handle, personaStarterOperationSkill("hcmnext.skill.onboarding_channel_post", "Prepare an approved onboarding welcome for governed delivery.", "hcmnext.persona.onboarding_channel_post", agentskills.TierT2, "ONBOARDING")},
	}
	return publishPersonaStarterBindings(caps, skills, items)
}

type PersonaSchedulePort interface {
	Schedule(context.Context, values.EntityRef) (availability.VersionedWorkSchedule, error)
	DraftSwap(context.Context, *trust.Principal, string, string, string, uint64, string) (PersonaShiftSwapProposal, error)
	SubmitShiftChange(context.Context, *trust.Principal, string, uint64) (schedopt.ShiftSelfServiceResult, error)
}

// PersonaShiftSwapProposal is a zero-effect T1 intent. It deliberately does
// not use schedopt.OfferTrade: OfferTrade persists an offer and is therefore
// reserved for a confirmed governed operation.
type PersonaShiftSwapProposal struct {
	OfferID, AssignmentID, RequestedAssignmentID string
	Fence                                        uint64
	Reason                                       string
	Worker                                       values.EntityRef
}

// PersonaScheduleReader is the only remaining read-side composition seam.
// PersonaScheduleActorPort supplies the mutation half from the existing
// authenticated ShiftSelfServiceActor, preserving worker resolution and
// standing checks in that boundary.
type PersonaScheduleReader interface {
	Schedule(context.Context, values.EntityRef) (availability.VersionedWorkSchedule, error)
}

// PersonaScheduleSwapDrafter validates a proposal against the current owner-
// published schedule without persisting an offer or changing an assignment.
type PersonaScheduleSwapDrafter interface {
	DraftSwap(context.Context, *trust.Principal, string, string, string, uint64, string) (PersonaShiftSwapProposal, error)
}

type PersonaScheduleActorPort struct {
	Actor  ShiftSelfServiceActor
	Reader PersonaScheduleReader
}

func (p PersonaScheduleActorPort) Schedule(ctx context.Context, worker values.EntityRef) (availability.VersionedWorkSchedule, error) {
	if p.Reader == nil {
		return availability.VersionedWorkSchedule{}, ErrShiftSelfServiceFacts
	}
	return p.Reader.Schedule(ctx, worker)
}

func (p PersonaScheduleActorPort) DraftSwap(ctx context.Context, principal *trust.Principal, offerID, assignmentID, requestedAssignmentID string, fence uint64, reason string) (PersonaShiftSwapProposal, error) {
	if principal == nil || principal.SubjectKind() != trust.SubjectKindHuman && principal.SubjectKind() != trust.SubjectKindAgent || offerID == "" || assignmentID == "" || requestedAssignmentID == "" || assignmentID == requestedAssignmentID || reason == "" {
		return PersonaShiftSwapProposal{}, errPersonaStarterBinding
	}
	drafter, ok := p.Reader.(PersonaScheduleSwapDrafter)
	if !ok {
		return PersonaShiftSwapProposal{}, ErrShiftSelfServiceFacts
	}
	return drafter.DraftSwap(ctx, principal, offerID, assignmentID, requestedAssignmentID, fence, reason)
}

func (p PersonaScheduleActorPort) SubmitShiftChange(ctx context.Context, principal *trust.Principal, offerID string, fence uint64) (schedopt.ShiftSelfServiceResult, error) {
	return p.Actor.AcceptTrade(ctx, principal, offerID, fence)
}

type PersonaScheduleReadCall struct{ Worker values.EntityRef }
type PersonaScheduleSwapCall struct {
	OfferID, AssignmentID, RequestedAssignmentID string
	Fence                                        uint64
	Reason                                       string
}
type PersonaShiftSubmitCall struct {
	OfferID string
	Fence   uint64
}

func BindPersonaScheduleSkills(caps *capability.Registry, skills *agentskills.Registry, port PersonaSchedulePort) ([]agentskills.SkillPin, error) {
	if caps == nil || skills == nil || port == nil {
		return nil, errPersonaStarterBinding
	}
	items := []struct {
		def capability.Definition
		h   capability.Handler
		s   agentskills.SkillDefinition
	}{
		{personaStarterDefinition("hcmnext.persona.schedule_read", "availability", capability.EffectReadOnly, "SCHEDULE"), personaScheduleReadHandler{port}.handle, personaStarterOperationSkill("hcmnext.skill.schedule_read", "Read the authorized work schedule.", "hcmnext.persona.schedule_read", agentskills.TierT0, "SCHEDULE")},
		{personaStarterDefinition("hcmnext.persona.schedule_swap_draft", "schedopt", capability.EffectPure, "SCHEDULE"), personaScheduleSwapHandler{port}.handle, personaStarterOperationSkill("hcmnext.skill.schedule_swap_draft", "Draft a private shift swap.", "hcmnext.persona.schedule_swap_draft", agentskills.TierT1, "SCHEDULE")},
		{personaStarterDefinition("hcmnext.persona.shift_change_submit", "schedopt", capability.EffectInternalMutation, "SCHEDULE"), personaShiftSubmitHandler{port}.handle, personaStarterOperationSkill("hcmnext.skill.shift_change_submit", "Submit an approved shift change.", "hcmnext.persona.shift_change_submit", agentskills.TierT3, "SCHEDULE")},
	}
	return publishPersonaStarterBindings(caps, skills, items)
}

func personaStarterDefinition(id, owner string, effect capability.EffectClass, domain string) capability.Definition {
	return capability.Definition{ID: id, Version: 1, OwnerDomain: owner,
		RequestSchema: capability.SchemaRef{SchemaID: id + ".Request", Version: 1, ProtobufFullName: id + ".Request"}, ResponseSchema: capability.SchemaRef{SchemaID: id + ".Response", Version: 1, ProtobufFullName: id + ".Response"}, ErrorSchema: capability.SchemaRef{SchemaID: "google.rpc.Status", Version: 1, ProtobufFullName: "google.rpc.Status"}, EffectClass: effect, ReadData: capability.DataDomainFieldSet{DataDomains: []string{domain}}, RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AgentEligible: true, AuthZScopeRef: "persona:" + domain + ":read", LegalBasisRef: "legal.p1a.observation-only.v1", EntitlementRef: "entitlement.pilot.p1a.v1", SLOClassRef: "slo.interactive.p95-2s.v1", TestRef: "test:TestTodo_PERSONA_STARTER_DOMAIN"}
}

func personaStarterOperationSkill(id, description, capabilityID string, tier agentskills.SideEffectTier, dataClass string) agentskills.SkillDefinition {
	input := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Worker":{"type":"string"}},"required":["Worker"]}`)
	output := json.RawMessage(`{"type":"object","additionalProperties":false}`)
	switch id {
	case "hcmnext.skill.welcome_note_draft":
		input = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Worker":{"type":"string"}},"required":["Worker"]}`)
	case "hcmnext.skill.onboarding_channel_post":
		input = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Worker":{"type":"string"},"Draft":{"type":"string","minLength":1},"Channel":{"type":"string","minLength":1}},"required":["Worker","Draft","Channel"]}`)
	case "hcmnext.skill.schedule_swap_draft":
		input = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"OfferID":{"type":"string"},"AssignmentID":{"type":"string"},"RequestedAssignmentID":{"type":"string"},"Fence":{"type":"integer","minimum":1},"Reason":{"type":"string","minLength":1}},"required":["OfferID","AssignmentID","RequestedAssignmentID","Fence","Reason"]}`)
	case "hcmnext.skill.shift_change_submit":
		input = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"OfferID":{"type":"string"},"Fence":{"type":"integer","minimum":1}},"required":["OfferID","Fence"]}`)
	}
	switch id {
	case "hcmnext.skill.onboarding_checklist_read":
		output = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"PlanDigest":{"type":"string"},"AsOf":{"type":"string"},"Results":{"type":"array"},"Aggregate":{"type":"string"},"Blockers":{"type":"array"},"Waivers":{"type":"array"},"Digest":{"type":"string"}},"required":["PlanDigest","AsOf","Results","Aggregate","Blockers","Waivers","Digest"]}`)
	case "hcmnext.skill.onboarding_task_read":
		output = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Tasks":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"ChildID":{"type":"string"},"Intent":{"type":"object"},"State":{"type":"string","enum":["PENDING","EMITTED","OBSERVED","FAILED"]},"Outcome":{"type":"string","enum":["OBSERVED","FAILED"]},"ObservationRef":{"type":"string"}},"required":["ChildID","State"]}}},"required":["Tasks"]}`)
	case "hcmnext.skill.welcome_note_draft":
		output = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Draft":{"type":"string","minLength":1}},"required":["Draft"]}`)
	case "hcmnext.skill.schedule_read":
		output = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"ScheduleID":{"type":"string"},"Assignment":{"type":"string"},"Revision":{"type":"string"},"Calendar":{"type":"object"},"Timezone":{"type":"object"},"WorkingWeekdays":{"type":"object"},"Shifts":{"type":"array"},"Holidays":{"type":"array"}},"required":["ScheduleID","Assignment","Revision","Calendar","Timezone","WorkingWeekdays","Shifts","Holidays"]}`)
	case "hcmnext.skill.schedule_swap_draft":
		output = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"OfferID":{"type":"string"},"AssignmentID":{"type":"string"},"RequestedAssignmentID":{"type":"string"},"Fence":{"type":"integer"},"Reason":{"type":"string"},"Worker":{"type":"string"}},"required":["OfferID","AssignmentID","RequestedAssignmentID","Fence","Reason","Worker"]}`)
	case "hcmnext.skill.shift_change_submit":
		output = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Approved":{"type":"object"},"Publication":{"type":"object"},"FencingToken":{"type":"integer"}},"required":["Approved","Publication","FencingToken"]}`)
	case "hcmnext.skill.onboarding_channel_post":
		output = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Worker":{"type":"string"},"WorkerRevision":{"type":"string"},"SnapshotRevision":{"type":"integer"},"PlanDigest":{"type":"string"},"Channel":{"type":"string"},"ChannelRevision":{"type":"integer"},"ChannelPolicyRevision":{"type":"integer"},"Draft":{"type":"string"}},"required":["Worker","WorkerRevision","SnapshotRevision","PlanDigest","Channel","ChannelRevision","ChannelPolicyRevision","Draft"]}`)
	}
	return agentskills.SkillDefinition{ID: id, Version: 1, Owner: "people", Description: description, InputSchema: input, OutputSchema: output, Operations: []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: capabilityID, Version: 1}}}, SideEffectTier: tier, RequiredPurposes: []string{"persona-mention"}, DataClassesRead: []string{dataClass}, IdempotencyRule: "read-only", CostClass: "LOW", EvalRefs: []string{"AGENTP-021"}}
}

func publishPersonaStarterBindings(caps *capability.Registry, skills *agentskills.Registry, items []struct {
	def capability.Definition
	h   capability.Handler
	s   agentskills.SkillDefinition
}) ([]agentskills.SkillPin, error) {
	for _, item := range items {
		if err := caps.Register(item.def, item.h); err != nil {
			return nil, fmt.Errorf("%w: register %s: %v", errPersonaStarterBinding, item.def.ID, err)
		}
		if err := skills.Publish(item.s); err != nil {
			return nil, fmt.Errorf("%w: publish %s: %v", errPersonaStarterBinding, item.s.ID, err)
		}
	}
	pins := make([]agentskills.SkillPin, 0, len(items))
	for _, item := range items {
		pin, err := skills.Pin(item.s.Key())
		if err != nil {
			return nil, err
		}
		pins = append(pins, pin)
	}
	return pins, nil
}

func personaWorker(ctx context.Context, worker values.EntityRef) error {
	if err := authorizePersonaStarterTenant(ctx, worker.Tenant); err != nil {
		return err
	}
	return worker.Validate()
}

type personaOnboardingChecklistHandler struct{ port PersonaOnboardingPort }

func (h personaOnboardingChecklistHandler) handle(ctx context.Context, payload any) (any, error) {
	c, ok := payload.(PersonaOnboardingCall)
	if !ok || h.port == nil {
		return nil, errPersonaStarterBinding
	}
	if err := personaWorker(ctx, c.Worker); err != nil {
		return nil, err
	}
	readiness, err := h.port.Checklist(ctx, c.Worker)
	if err != nil {
		return nil, err
	}
	result := PersonaReadinessDTO{PlanDigest: readiness.PlanDigest, AsOf: readiness.AsOf.String(), Aggregate: string(readiness.Aggregate), Blockers: readiness.Blockers, Waivers: readiness.Waivers, Digest: readiness.Digest}
	result.Results = make([]PersonaReadinessResult, 0, len(readiness.Results))
	for _, item := range readiness.Results {
		evidence := ""
		if raw := item.Evidence.Canonical(); raw != nil {
			evidence = string(raw)
		}
		observed := ""
		if item.ObservedAt.Canonical() != nil {
			observed = item.ObservedAt.String()
		}
		result.Results = append(result.Results, PersonaReadinessResult{RequirementID: item.RequirementID, Owner: item.Owner, Kind: string(item.Kind), Status: string(item.Status), Blocker: item.Blocker, ObservedAt: observed, Evidence: evidence, Summary: item.Summary, Protected: item.Protected})
	}
	return result, nil
}

type personaOnboardingTasksHandler struct{ port PersonaOnboardingPort }

func (h personaOnboardingTasksHandler) handle(ctx context.Context, payload any) (any, error) {
	c, ok := payload.(PersonaOnboardingCall)
	if !ok || h.port == nil {
		return nil, errPersonaStarterBinding
	}
	if err := personaWorker(ctx, c.Worker); err != nil {
		return nil, err
	}
	states, ok := h.port.(PersonaOnboardingTaskStatePort)
	if !ok {
		return nil, errPersonaStarterBinding
	}
	tasks, err := states.TaskStates(ctx, c.Worker)
	if err != nil {
		return nil, err
	}
	return PersonaOnboardingTasksResult{Tasks: tasks}, nil
}

type personaWelcomeDraftHandler struct{ port PersonaOnboardingPort }

func (h personaWelcomeDraftHandler) handle(ctx context.Context, payload any) (any, error) {
	c, ok := payload.(PersonaWelcomeDraftCall)
	if !ok || h.port == nil {
		return nil, errPersonaStarterBinding
	}
	if err := personaWorker(ctx, c.Worker); err != nil {
		return nil, err
	}
	draft, err := h.port.DraftWelcome(ctx, c.Worker)
	if err != nil {
		return nil, err
	}
	return PersonaWelcomeDraftResult{Draft: draft}, nil
}

type personaWelcomePostHandler struct{ port PersonaOnboardingPort }

func (h personaWelcomePostHandler) handle(ctx context.Context, payload any) (any, error) {
	c, ok := payload.(PersonaWelcomePostCall)
	if !ok || h.port == nil || c.Draft == "" || c.Channel == "" {
		return nil, errPersonaStarterBinding
	}
	if err := personaWorker(ctx, c.Worker); err != nil {
		return nil, err
	}
	p, err := trust.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return h.port.PrepareWelcomePost(ctx, p, c.Worker, c.Draft, c.Channel)
}

type personaScheduleReadHandler struct{ port PersonaSchedulePort }

func (h personaScheduleReadHandler) handle(ctx context.Context, payload any) (any, error) {
	c, ok := payload.(PersonaScheduleReadCall)
	if !ok || h.port == nil {
		return nil, errPersonaStarterBinding
	}
	if err := personaWorker(ctx, c.Worker); err != nil {
		return nil, err
	}
	return h.port.Schedule(ctx, c.Worker)
}

type personaScheduleSwapHandler struct{ port PersonaSchedulePort }

func (h personaScheduleSwapHandler) handle(ctx context.Context, payload any) (any, error) {
	c, ok := payload.(PersonaScheduleSwapCall)
	if !ok || h.port == nil {
		return nil, errPersonaStarterBinding
	}
	p, err := trust.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if p.SubjectKind() != trust.SubjectKindHuman && p.SubjectKind() != trust.SubjectKindAgent {
		return nil, errPersonaStarterBinding
	}
	return h.port.DraftSwap(ctx, p, c.OfferID, c.AssignmentID, c.RequestedAssignmentID, c.Fence, c.Reason)
}

type personaShiftSubmitHandler struct{ port PersonaSchedulePort }

func (h personaShiftSubmitHandler) handle(ctx context.Context, payload any) (any, error) {
	c, ok := payload.(PersonaShiftSubmitCall)
	if !ok || h.port == nil {
		return nil, errPersonaStarterBinding
	}
	p, err := trust.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if p.SubjectKind() != trust.SubjectKindHuman && p.SubjectKind() != trust.SubjectKindAgent {
		return nil, errPersonaStarterBinding
	}
	return h.port.SubmitShiftChange(ctx, p, c.OfferID, c.Fence)
}
