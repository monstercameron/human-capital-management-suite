package transport

import (
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	kit "github.com/monstercameron/human-capital-management-suite/tools/quality/bufprotovalidatekit"
)

func TestTodo_LIB_019_QualifiedValidatorProjectsOwnedViolations(t *testing.T) {
	descriptor := (&intentsv1.GetIntentRequest{}).ProtoReflect().Descriptor()
	compiled, err := kit.Compile(descriptor, kit.Spec{
		Message: string(descriptor.FullName()),
		Rules: []kit.Rule{{
			RuleRef: "get_intent.intent_id.utf8", FieldPath: "intent_id",
			Kind: kit.RuleUTF8, Scope: kit.ScopeStructural,
		}},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	interceptor, err := kit.NewInterceptor(map[string]*kit.Validator{
		"/hcmnext.intents.v1.IntentService/GetIntent": compiled,
	})
	if err != nil {
		t.Fatalf("NewInterceptor: %v", err)
	}
	validator := NewQualifiedValidator(nil, interceptor)
	got := validator.Validate("/hcmnext.intents.v1.IntentService/GetIntent", &intentsv1.GetIntentRequest{IntentId: string([]byte{'i', 'd', 0xff})})
	if got == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
	if len(got.Violations()) != 1 {
		t.Fatalf("violations=%+v", got.Violations())
	}
	violation := got.Violations()[0]
	if violation.FieldPath != "intent_id" || violation.RuleRef != "get_intent.intent_id.utf8" || violation.Description != "field is not valid UTF-8" {
		t.Fatalf("violation=%+v", violation)
	}
	if got.Code() != envelope.CodeInvalidArgument {
		t.Fatalf("code=%v want=%v", got.Code(), envelope.CodeInvalidArgument)
	}
}
