package agentskills

import (
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

const (
	SupportCreateTicket = "support.create_ticket"
	SupportAlertChannel = "support.alert_channel"
	SupportDailyLimit   = 25
)

// SupportEffectSkill binds the budget and effect to the immutable skill pin.
// Ordinary project writes require T3 in this repository; T1 cannot contain
// these effects. No exception to the capability tier ladder is introduced.
type SupportEffectSkill struct {
	Skill          SkillDefinition
	Effect         capability.EffectClass
	DailyLimit     uint32
	IdempotencyKey string
}

func SupportEffectSkills() []SupportEffectSkill {
	var out []SupportEffectSkill
	for _, id := range []string{SupportCreateTicket, SupportAlertChannel} {
		input := json.RawMessage(`{"type":"object","properties":{"email_id":{"type":"string"}},"required":["email_id"],"additionalProperties":false}`)
		if id == SupportCreateTicket {
			input = json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":80},"summary":{"type":"string","minLength":1,"maxLength":2000},"severity":{"type":"string","enum":["Low","Normal","High"]},"needs_human_review":{"type":"boolean"}},"required":["title","summary","severity","needs_human_review"],"additionalProperties":false}`)
		}
		key := "tenant + support email message id + " + id
		def := SkillDefinition{ID: id, Version: 1, Owner: "customer-support", Description: "Create one source-bound support ticket or publish its bounded incident alert", InputSchema: input, OutputSchema: json.RawMessage(`{"type":"object","properties":{"reference":{"type":"string"}},"required":["reference"],"additionalProperties":false}`), Operations: []OperationRef{{Kind: OperationCapability, Capability: capability.Key{ID: id, Version: 1}}}, SideEffectTier: TierT3, RequiredPurposes: []string{"customer-support"}, DataClassesRead: []string{"UNTRUSTED_CUSTOMER_EMAIL"}, DataClassesWritten: []string{"ORDINARY_PROJECT", "INTERNAL"}, IdempotencyRule: fmt.Sprintf("effect=INTERNAL_MUTATION;daily_limit=%d;key=%s", SupportDailyLimit, key), CostClass: "bounded", EvalRefs: []string{"AGENTUX-054.support-desk/v1"}}
		out = append(out, SupportEffectSkill{Skill: def, Effect: capability.EffectInternalMutation, DailyLimit: SupportDailyLimit, IdempotencyKey: key})
	}
	return out
}

func (s SupportEffectSkill) Validate() error {
	if s.DailyLimit == 0 || s.DailyLimit > SupportDailyLimit || s.IdempotencyKey == "" || s.Effect != capability.EffectInternalMutation || s.Skill.SideEffectTier < MinimumTierForEffect(s.Effect) || s.Skill.IdempotencyRule != fmt.Sprintf("effect=INTERNAL_MUTATION;daily_limit=%d;key=%s", s.DailyLimit, s.IdempotencyKey) {
		return ErrInvalidDefinition
	}
	return validateDefinition(s.Skill)
}
