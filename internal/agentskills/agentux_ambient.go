package agentskills

import "encoding/json"

// AmbientOfferSkills declares the T1 draft operations. Adding a task or setting
// a reminder is a separate human command; message text cannot invoke it.
func AmbientOfferSkills() []SkillDefinition {
	var result []SkillDefinition
	for _, kind := range []string{"task", "reminder"} {
		result = append(result, SkillDefinition{ID: "hcmnext.ambient." + kind + ".offer", Version: 1, Owner: "hcmnext", Description: "Propose a source-bound " + kind + " for human confirmation", SideEffectTier: TierT1, InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"source_post_id":{"type":"string"},"source_revision":{"type":"integer","minimum":1}},"required":["source_post_id","source_revision"]}`), OutputSchema: json.RawMessage(`{"type":"object","properties":{"offer_id":{"type":"string"}},"required":["offer_id"],"additionalProperties":false}`), Operations: []OperationRef{{Kind: OperationConnection, ConnectionID: "chat.ambient", Operation: "offer_" + kind}}, RequiredPurposes: []string{"chat.ambient.offer"}, DataClassesRead: []string{"chat_message"}, DataClassesWritten: []string{"recipient_scoped_offer"}, IdempotencyRule: "tenant + source post + agent; source revisions are fenced", CostClass: "bounded", EvalRefs: []string{"AGENTUX-067.task-catcher/v1", "AGENTUX-068.reminder/v1"}})
	}
	return result
}
