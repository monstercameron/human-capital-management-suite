package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	personaCompBandCapabilityID     = "hcmnext.persona.comp_band_read"
	personaCompaCapabilityID        = "hcmnext.persona.compa_ratio_read"
	personaCompScenarioCapabilityID = "hcmnext.persona.comp_scenario_draft"
)

var errPersonaStarterBinding = errors.New("application: persona starter binding unavailable")

func authorizePersonaStarterTenant(ctx context.Context, tenant values.TenantId) error {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || (principal.SubjectKind() != trust.SubjectKindHuman && principal.SubjectKind() != trust.SubjectKindAgent) || principal.Tenant() != tenant {
		return fmt.Errorf("%w: authenticated principal tenant does not match request", errPersonaStarterBinding)
	}
	return nil
}

// PersonaCompensationPort is the narrow, governed domain port required by
// the compensation starter skills. The application never invents band facts;
// composition must provide a real rewards catalog.
type PersonaCompensationPort interface {
	LookupBand(context.Context, rewards.BandQuery) (rewards.BandRecord, error)
}

// PersonaCompensationScenarioPort is the native compensation adapter. It
// resolves the authoritative current package and server annualization before
// invoking the rewards calculation; callers cannot supply those baselines.
type PersonaCompensationScenarioPort interface {
	SimulateCompensation(context.Context, rewards.SimulateCompensationInput) (rewards.SimulateCompensationResult, error)
}

// PersonaCompaRatioPort resolves the worker's canonical compensation fact and
// governing band before calculating position. Callers cannot provide an
// arbitrary salary amount to the compa-ratio skill.
type PersonaCompaRatioPort interface {
	PayBandPosition(context.Context, values.EntityRef, values.Instant) (rewards.PayBandEvaluation, error)
}

type PersonaCompBandCall struct{ Query rewards.BandQuery }

type PersonaCompaRatioCall struct {
	Worker values.EntityRef
	AsOf   values.Instant
}

// BindPersonaCompensationSkills registers the three real rewards adapters and
// publishes their operation-capability skills. A nil port leaves the catalog
// fail-closed and returns an error instead of publishing a fake connection.
func BindPersonaCompensationSkills(caps *capability.Registry, skills *agentskills.Registry, port PersonaCompensationPort, compa PersonaCompaRatioPort, scenario PersonaCompensationScenarioPort) ([]agentskills.SkillPin, error) {
	if caps == nil || skills == nil || port == nil || compa == nil || scenario == nil {
		return nil, errPersonaStarterBinding
	}
	bindings := []struct {
		definition capability.Definition
		handler    capability.Handler
		skill      agentskills.SkillDefinition
	}{
		{personaCompBandDefinition(), personaCompBandHandler{port}.handle, personaCompBandSkill()},
		{personaCompaDefinition(), personaCompaHandler{source: compa}.handle, personaCompaSkill()},
		{personaCompScenarioDefinition(), personaCompScenarioHandler{scenario}.handle, personaCompScenarioSkill()},
	}
	for _, binding := range bindings {
		if err := caps.Register(binding.definition, binding.handler); err != nil {
			return nil, fmt.Errorf("%w: register %s: %v", errPersonaStarterBinding, binding.definition.ID, err)
		}
		if err := skills.Publish(binding.skill); err != nil {
			return nil, fmt.Errorf("%w: publish %s: %v", errPersonaStarterBinding, binding.skill.ID, err)
		}
	}
	pins := make([]agentskills.SkillPin, 0, len(bindings))
	for _, binding := range bindings {
		pin, err := skills.Pin(binding.skill.Key())
		if err != nil {
			return nil, fmt.Errorf("%w: pin %s: %v", errPersonaStarterBinding, binding.skill.ID, err)
		}
		pins = append(pins, pin)
	}
	return pins, nil
}

func personaCompBandDefinition() capability.Definition {
	return personaCompDefinition(personaCompBandCapabilityID, "hcmnext.rewards.BandQuery", "hcmnext.rewards.BandRecord", capability.EffectReadOnly)
}

func personaCompaDefinition() capability.Definition {
	return personaCompDefinition(personaCompaCapabilityID, "hcmnext.persona.CompaRatioCall", "hcmnext.rewards.PayBandEvaluation", capability.EffectReadOnly)
}

func personaCompScenarioDefinition() capability.Definition {
	return personaCompDefinition(personaCompScenarioCapabilityID, "hcmnext.rewards.SimulateCompensationInput", "hcmnext.rewards.SimulateCompensationResult", capability.EffectPure)
}

func personaCompDefinition(id, request, response string, effect capability.EffectClass) capability.Definition {
	return capability.Definition{
		ID: id, Version: 1, OwnerDomain: "rewards",
		RequestSchema:  capability.SchemaRef{SchemaID: request, Version: 1, ProtobufFullName: request},
		ResponseSchema: capability.SchemaRef{SchemaID: response, Version: 1, ProtobufFullName: response},
		ErrorSchema:    capability.SchemaRef{SchemaID: "google.rpc.Status", Version: 1, ProtobufFullName: "google.rpc.Status"},
		EffectClass:    effect, ReadData: capability.DataDomainFieldSet{DataDomains: []string{"compensation"}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AgentEligible: true,
		AuthZScopeRef: "compensation:read", LegalBasisRef: "legal.p1a.observation-only.v1",
		EntitlementRef: "entitlement.pilot.p1a.v1", SLOClassRef: "slo.interactive.p95-2s.v1",
		TestRef: "test:TestTodo_PERSONA_STARTER_COMPENSATION",
	}
}

func personaCompBandSkill() agentskills.SkillDefinition {
	return personaCompSkill("hcmnext.skill.comp_band_read", "Read an authorized compensation band.", personaCompBandCapabilityID, agentskills.TierT0, json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Query":{"type":"object"}},"required":["Query"]}`), json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Band":{"type":"object"},"CatalogVersion":{"type":"string"},"Blocking":{"type":"boolean"},"Authority":{"type":"object"},"Provenance":{"type":"object"}},"required":["Band","CatalogVersion","Blocking","Authority","Provenance"]}`))
}

func personaCompaSkill() agentskills.SkillDefinition {
	return personaCompSkill("hcmnext.skill.compa_ratio_read", "Read an authorized worker compa-ratio.", personaCompaCapabilityID, agentskills.TierT0, json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Worker":{"type":"string"},"AsOf":{"type":"string","format":"date-time"}},"required":["Worker","AsOf"]}`), json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"IntentType":{"type":"string"},"IntentVersion":{"type":"string"},"Query":{"type":"object"},"Position":{"type":"object"},"Outcome":{"type":"integer"},"CatalogVersion":{"type":"string"},"RulePackVersion":{"type":"string"},"Authority":{"type":"object"},"Provenance":{"type":"object"},"InputsDigest":{"type":"string"},"ResultDigest":{"type":"string"},"Effects":{"type":"object"}},"required":["IntentType","IntentVersion","Query","Position","Outcome","CatalogVersion","RulePackVersion","Authority","Provenance","InputsDigest","ResultDigest","Effects"]}`))
}

func personaCompScenarioSkill() agentskills.SkillDefinition {
	return personaCompSkill("hcmnext.skill.comp_scenario_draft", "Draft a private compensation scenario from authorized facts.", personaCompScenarioCapabilityID, agentskills.TierT1, json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"Tenant":{"type":"string"},"Subject":{"type":"object"},"Proposed":{"type":"object"},"EffectiveDate":{"type":"string"},"Market":{"type":"object"}},"required":["Tenant","Subject","Proposed","EffectiveDate"]}`), json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"IntentType":{"type":"string"},"IntentVersion":{"type":"string"},"Tenant":{"type":"string"},"Subject":{"type":"object"},"EffectiveDate":{"type":"string"},"Current":{"type":"object"},"Proposed":{"type":"object"},"Delta":{"type":"object"},"Band":{"type":"object"},"Market":{"type":"object"},"Assumptions":{"type":"array"},"RulePackVersion":{"type":"string"},"AnnualizationVersion":{"type":"string"},"CatalogVersion":{"type":"string"},"InputsDigest":{"type":"string"},"ResultDigest":{"type":"string"},"Effects":{"type":"object"},"Receipt":{"type":"object"}},"required":["IntentType","IntentVersion","Tenant","Subject","EffectiveDate","Current","Proposed","Delta","Band","Assumptions","RulePackVersion","AnnualizationVersion","CatalogVersion","InputsDigest","ResultDigest","Effects","Receipt"]}`))
}

func personaCompSkill(id, description, capabilityID string, tier agentskills.SideEffectTier, input, output json.RawMessage) agentskills.SkillDefinition {
	return agentskills.SkillDefinition{ID: id, Version: 1, Owner: "people", Description: description, InputSchema: input, OutputSchema: output,
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: capabilityID, Version: 1}}},
		SideEffectTier: tier, RequiredPurposes: []string{"compensation_review"}, DataClassesRead: []string{"COMPENSATION"}, IdempotencyRule: "read-only", CostClass: "LOW",
		EvalRefs: []string{"AGENTP-021.comp_analyst"}}
}

type personaCompBandHandler struct{ port PersonaCompensationPort }

func (h personaCompBandHandler) handle(ctx context.Context, payload any) (any, error) {
	call, ok := payload.(PersonaCompBandCall)
	if !ok || h.port == nil {
		return nil, errPersonaStarterBinding
	}
	if err := authorizePersonaStarterTenant(ctx, call.Query.Tenant); err != nil {
		return nil, err
	}
	if err := call.Query.Validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid band query: %v", errPersonaStarterBinding, err)
	}
	return h.port.LookupBand(ctx, call.Query)
}

type personaCompaHandler struct{ source PersonaCompaRatioPort }

func (h personaCompaHandler) handle(ctx context.Context, payload any) (any, error) {
	call, ok := payload.(PersonaCompaRatioCall)
	if !ok || h.source == nil {
		return nil, errPersonaStarterBinding
	}
	if err := authorizePersonaStarterTenant(ctx, call.Worker.Tenant); err != nil {
		return nil, err
	}
	if err := call.Worker.Validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid worker: %v", errPersonaStarterBinding, err)
	}
	if err := call.AsOf.Validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid as-of: %v", errPersonaStarterBinding, err)
	}
	return h.source.PayBandPosition(ctx, call.Worker, call.AsOf)
}

type personaCompScenarioHandler struct {
	scenario PersonaCompensationScenarioPort
}

func (h personaCompScenarioHandler) handle(ctx context.Context, payload any) (any, error) {
	in, ok := payload.(rewards.SimulateCompensationInput)
	if !ok || h.scenario == nil {
		return nil, errPersonaStarterBinding
	}
	if err := authorizePersonaStarterTenant(ctx, in.Tenant); err != nil {
		return nil, err
	}
	if err := in.Tenant.Validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid scenario tenant: %v", errPersonaStarterBinding, err)
	}
	return h.scenario.SimulateCompensation(ctx, in)
}
