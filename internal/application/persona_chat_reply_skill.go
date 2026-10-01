package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

const (
	personaChatReplySkillID      = "persona.chat_reply"
	personaChatReplySkillVersion = uint32(1)
	personaChatReplyOwner        = "chat"
	personaChatReplyPurpose      = "persona-mention"
)

var (
	errPersonaChatReplySkillRegistration    = errors.New("application: private persona reply skill registration unavailable")
	errPersonaChatReplyCapabilityOutputOnly = errors.New("application: persona replies require the sealed output gateway")
)

// bindPersonaChatReplySkill publishes the exact T0 skill and its capability
// descriptor. The capability handler is deliberately output-only and refuses
// generic gateway execution: private replies are admitted and sealed by the
// persona output ToolGateway after the current private-chat grant is rechecked.
// This registration does not issue or imply a grant.
func bindPersonaChatReplySkill(caps *capability.Registry, skills *agentskills.Registry) (agentskills.SkillPin, error) {
	if caps == nil || skills == nil {
		return agentskills.SkillPin{}, errPersonaChatReplySkillRegistration
	}
	key := agentskills.SkillKey{ID: personaChatReplySkillID, Version: personaChatReplySkillVersion}
	if _, exists := skills.Lookup(key); exists {
		return agentskills.SkillPin{}, fmt.Errorf("%w: skill already published", errPersonaChatReplySkillRegistration)
	}
	definition := personaChatReplyCapabilityDefinition()
	if _, exists := caps.Lookup(definition.Key()); exists {
		return agentskills.SkillPin{}, fmt.Errorf("%w: capability already registered", errPersonaChatReplySkillRegistration)
	}
	if err := caps.Register(definition, refuseGenericPersonaChatReply); err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: register capability: %v", errPersonaChatReplySkillRegistration, err)
	}
	if err := skills.Publish(personaChatReplySkillDefinition()); err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: publish skill: %v", errPersonaChatReplySkillRegistration, err)
	}
	pin, err := skills.Pin(key)
	if err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: derive exact pin: %v", errPersonaChatReplySkillRegistration, err)
	}
	return pin, nil
}

func personaChatReplyCapabilityDefinition() capability.Definition {
	return capability.Definition{
		ID: agentgate.PrivateChatReplyCapability, Version: 1, OwnerDomain: personaChatReplyOwner,
		RequestSchema:  capability.SchemaRef{SchemaID: "persona.chat-reply.request/v1", Version: 1, ProtobufFullName: "google.protobuf.Struct"},
		ResponseSchema: capability.SchemaRef{SchemaID: "persona.chat-reply.response/v1", Version: 1, ProtobufFullName: "google.protobuf.Struct"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "persona.chat-reply.error/v1", Version: 1, ProtobufFullName: "google.rpc.Status"},
		EffectClass:    capability.EffectPure, RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AgentEligible: true,
		AuthZScopeRef: agentgate.PrivateChatReplyScope, LegalBasisRef: "legal.private-chat.reply.v1",
		EntitlementRef: "entitlement.chat.v1", SLOClassRef: "slo.interactive.v1",
		TestRef: "test:TestTodo_AGENTP_008_PrivateChatReplySkillRegistration",
	}
}

func personaChatReplySkillDefinition() agentskills.SkillDefinition {
	return agentskills.SkillDefinition{
		ID: personaChatReplySkillID, Version: personaChatReplySkillVersion, Owner: personaChatReplyOwner,
		Description:    "Prepare a bounded plain-text persona reply for the current private chat. Delivery requires a fresh private-chat authorization and a sealed output admission.",
		InputSchema:    json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":16384}},"required":["text"],"additionalProperties":false}`),
		OutputSchema:   json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":16384}},"required":["text"],"additionalProperties":false}`),
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: agentgate.PrivateChatReplyCapability, Version: 1}}},
		SideEffectTier: agentskills.TierT0, RequiredPurposes: []string{personaChatReplyPurpose},
		IdempotencyRule: "pure-output-draft", CostClass: "LOW",
		EvalRefs: []string{"AGENTP-021.persona-chat-reply"},
	}
}

func refuseGenericPersonaChatReply(context.Context, any) (any, error) {
	return nil, errPersonaChatReplyCapabilityOutputOnly
}
