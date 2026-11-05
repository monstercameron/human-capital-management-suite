package transport

import (
	"testing"

	"google.golang.org/protobuf/proto"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// fakeDescriptorValidator is the local DescriptorValidator double: it
// reports one canned structural violation for the known method, standing
// in for a future qualified interceptor without importing the rejected
// Buf/Protovalidate qualification-kit runtime.
type fakeDescriptorValidator struct{}

func (fakeDescriptorValidator) Validate(method string, msg proto.Message) []DescriptorViolation {
	if method != "/hcmnext.intents.v1.IntentService/GetIntent" || msg == nil {
		return nil
	}
	return []DescriptorViolation{{
		FieldPath: "intent_id", Description: "field is not valid UTF-8", RuleRef: "get_intent.intent_id.utf8",
	}}
}

func TestTodo_LIB_019_QualifiedValidatorProjectsOwnedViolations(t *testing.T) {
	validator := NewQualifiedValidator(nil, fakeDescriptorValidator{})
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
