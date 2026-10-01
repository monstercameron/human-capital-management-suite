package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

func TestTodo_AGENTP_008_PrivateChatReplySkillRegistration(t *testing.T) {
	caps := capability.NewRegistry()
	skills := agentskills.NewRegistry(caps)
	pin, err := bindPersonaChatReplySkill(caps, skills)
	if err != nil {
		t.Fatalf("bind private reply skill: %v", err)
	}
	if pin.ID != personaChatReplySkillID || pin.Version != personaChatReplySkillVersion || pin.Digest == "" {
		t.Fatalf("private reply pin is not derived from the published definition: %+v", pin)
	}
	t.Logf("private reply pin digest: %s", pin.Digest)

	key := capability.Key{ID: agentgate.PrivateChatReplyCapability, Version: 1}
	capRecord, ok := caps.Lookup(key)
	if !ok || capRecord.Status != capability.StatusActive {
		t.Fatalf("private reply capability missing or inactive: %+v, %v", capRecord, ok)
	}
	definition := capRecord.Definition
	if definition.EffectClass != capability.EffectPure || !definition.AgentEligible || definition.AuthZScopeRef != agentgate.PrivateChatReplyScope ||
		definition.ID != "persona.reply" || definition.Version != 1 || len(definition.ReadData.DataDomains) != 0 || len(definition.WriteData.DataDomains) != 0 {
		t.Fatalf("capability widens beyond a pure private reply output contract: %+v", definition)
	}

	record, err := skills.ResolvePin(pin)
	if err != nil {
		t.Fatalf("resolve derived pin: %v", err)
	}
	if record.Status != agentskills.StatusActive || record.Definition.SideEffectTier != agentskills.TierT0 ||
		record.Definition.ID != "persona.chat_reply" || record.Definition.Version != 1 ||
		len(record.Definition.Operations) != 1 || record.Definition.Operations[0].Capability != key ||
		len(record.ResolvedOperations) != 1 || !record.ResolvedOperations[0].HasCapability ||
		record.ResolvedOperations[0].Capability.Definition.AuthZScopeRef != "chat.current" ||
		len(record.Definition.RequiredPurposes) != 1 || record.Definition.RequiredPurposes[0] != "persona-mention" ||
		len(record.Definition.DataClassesRead) != 0 || len(record.Definition.DataClassesWritten) != 0 {
		t.Fatalf("skill does not resolve to exactly the narrow T0 private output capability: %+v", record)
	}

	if _, err := refuseGenericPersonaChatReply(context.Background(), struct{}{}); !errors.Is(err, errPersonaChatReplyCapabilityOutputOnly) {
		t.Fatalf("generic capability invocation did not fail closed: %v", err)
	}
}

func TestTodo_AGENTP_008_PrivateChatReplySkillRegistrationRejectsMissingAndDuplicateRegistries(t *testing.T) {
	if _, err := bindPersonaChatReplySkill(nil, agentskills.NewRegistry(nil)); !errors.Is(err, errPersonaChatReplySkillRegistration) {
		t.Fatalf("nil capability registry error = %v", err)
	}
	caps := capability.NewRegistry()
	skills := agentskills.NewRegistry(caps)
	if _, err := bindPersonaChatReplySkill(caps, skills); err != nil {
		t.Fatal(err)
	}
	if _, err := bindPersonaChatReplySkill(caps, skills); !errors.Is(err, errPersonaChatReplySkillRegistration) {
		t.Fatalf("duplicate registration error = %v", err)
	}
	if len(caps.List()) != 1 || len(skills.List()) != 1 {
		t.Fatalf("duplicate attempt changed published registries: capabilities=%d skills=%d", len(caps.List()), len(skills.List()))
	}
}
