package application

import (
	"context"
	"errors"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// BindAgentUX079AmbientSkills publishes the ambient offer skills into the
// served registry from the same definitions the starters pin, so the pins'
// digests match without translation. Offer skills carry connection
// operations, not capabilities, so no capability registration is needed.
func BindAgentUX079AmbientSkills(skills *agentskills.Registry) ([]agentskills.SkillPin, error) {
	if skills == nil {
		return nil, errors.New("application: ambient skills need a registry")
	}
	var pins []agentskills.SkillPin
	for _, definition := range agentskills.AmbientOfferSkills() {
		if err := skills.Publish(definition); err != nil {
			return nil, err
		}
		pin, err := skills.Pin(definition.Key())
		if err != nil {
			return nil, err
		}
		pins = append(pins, pin)
	}
	return pins, nil
}

var errAgentUX079SupportEffectOutputOnly = errors.New("application: support effects run only inside the support desk worker")

// refuseAgentUX079SupportEffect refuses generic dispatch of Support Desk's
// effects: a ticket is filed and its alert posted only by the desk worker,
// through the prepared project, the grant ledger and the daily limit. The
// capabilities exist so the skills publish and list, never to execute.
func refuseAgentUX079SupportEffect(context.Context, any) (any, error) {
	return nil, errAgentUX079SupportEffectOutputOnly
}

// BindAgentUX079SupportSkills registers Support Desk's effect capabilities
// and publishes its skills into the served registries from the same
// definitions the starter pins. Execution stays inside the desk worker; the
// registered handlers refuse.
func BindAgentUX079SupportSkills(caps *capability.Registry, skills *agentskills.Registry) ([]agentskills.SkillPin, error) {
	if caps == nil || skills == nil {
		return nil, errors.New("application: support skills need capability and skill registries")
	}
	for _, id := range []string{agentskills.SupportCreateTicket, agentskills.SupportAlertChannel} {
		err := caps.Register(capability.Definition{ID: id, Version: 1, OwnerDomain: "customer-support",
			RequestSchema:  capability.SchemaRef{SchemaID: "support-desk.effect.request/v1", Version: 1, ProtobufFullName: "google.protobuf.Struct"},
			ResponseSchema: capability.SchemaRef{SchemaID: "support-desk.effect.response/v1", Version: 1, ProtobufFullName: "google.protobuf.Struct"},
			ErrorSchema:    capability.SchemaRef{SchemaID: "support-desk.effect.error/v1", Version: 1, ProtobufFullName: "google.rpc.Status"},
			EffectClass:    capability.EffectInternalMutation, RiskClass: "MEDIUM", IdempotencyPolicyRef: "idempotency:tenant+email+effect",
			AgentEligible: true, AuthZScopeRef: "support:service", LegalBasisRef: "support-desk-worker", EntitlementRef: "support-project-grant",
			SLOClassRef: "interactive", TestRef: "test:TestTodo_AGENTUX_079"}, refuseAgentUX079SupportEffect)
		if err != nil {
			return nil, err
		}
	}
	var pins []agentskills.SkillPin
	for _, effect := range agentskills.SupportEffectSkills() {
		if err := effect.Validate(); err != nil {
			return nil, err
		}
		if err := skills.Publish(effect.Skill); err != nil {
			return nil, err
		}
		pin, err := skills.Pin(effect.Skill.Key())
		if err != nil {
			return nil, err
		}
		pins = append(pins, pin)
	}
	return pins, nil
}

// agentSkillRegistryPurposes unites the purposes the served registry's skills
// require, so the Agents page discovers every pinned skill. A registry with
// no skills, or none at all, falls back to the chat purpose alone.
func agentSkillRegistryPurposes(skills *agentskills.Registry) []string {
	seen := map[string]bool{personaChatReplyPurpose: true}
	if skills != nil {
		for _, record := range skills.List() {
			for _, purpose := range record.Definition.RequiredPurposes {
				seen[purpose] = true
			}
		}
	}
	purposes := make([]string, 0, len(seen))
	for purpose := range seen {
		purposes = append(purposes, purpose)
	}
	sort.Strings(purposes)
	return purposes
}

// discoverAgentSkillsForPurposes discovers the viewer's granted skills under
// every purpose, de-duplicated by skill. An empty purpose list means the chat
// purpose alone, which is the behavior before multi-skill agents. Any purpose
// that fails fails the discovery: a partially known grant set must not pass
// the exact-pins gate.
func discoverAgentSkillsForPurposes(ctx context.Context, discoverer AgentSkillDiscoverer, principal *trust.Principal, purposes []string) ([]agentskills.SkillRecord, error) {
	if len(purposes) == 0 {
		purposes = []string{personaChatReplyPurpose}
	}
	seen := map[agentskills.SkillKey]bool{}
	var out []agentskills.SkillRecord
	for _, purpose := range purposes {
		records, err := discoverer.Discover(ctx, principal, purpose)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			key := record.Definition.Key()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, record)
		}
	}
	return out, nil
}
