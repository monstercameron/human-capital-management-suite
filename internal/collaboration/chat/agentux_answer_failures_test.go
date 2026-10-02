package chat

import (
	"strings"
	"testing"
)

func TestAgentUXQuality_FailureMapping(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, code := range strings.Split("ADMISSION_REFUSED,AUTHOR_NOT_HUMAN,OUT_OF_SCOPE,MODEL_BINDING_INVALID,TOOL_POLICY_UNAVAILABLE,TOOL_ADMISSION_FAILED,TOOL_EXECUTION_FAILED,CONTEXT_UNAVAILABLE,MODEL_UNAVAILABLE,MODEL_REFUSED_OR_INCOMPLETE,MODEL_RESULT_INVALID,OUTPUT_REJECTED,OUTPUT_BINDING_INVALID,DELIVERY_FAILED,DELIVERY_RECEIPT_INVALID,MODEL_TIMEOUT,MODEL_LIMIT,ANSWER_INTERRUPTED,PERSONA_NOT_INSTALLED,PERSONA_NOT_CURRENT,PERSONA_SUSPENDED,NO_EFFECTIVE_SKILLS,NOTHING_FOUND,authority,installation,grant,tool_scope,budget,audience,model_route,model_call,model_output,tool_call,output_grounding,output_schema,delivery_audience,delivery_write,deadline,stopped", ",") {
			copy := AgentAnswerFailureFor(locale, "Policy Helper", code)
			if copy.Class == "" || copy.Sentence == "" || copy.NextStep == "" || !strings.Contains(copy.Sentence, "Policy Helper") || strings.Contains(code, "_") && strings.Contains(copy.Sentence+copy.NextStep, code) {
				t.Fatalf("unmapped or technical copy %s/%s: %+v", locale, code, copy)
			}
			if copy.Retryable && copy.Class != "timeout" && copy.Class != "service" && copy.Class != "interrupted" {
				t.Fatalf("permanent refusal offers retry: %+v", copy)
			}
		}
	}
}
