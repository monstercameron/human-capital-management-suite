package transport

import (
	"encoding/hex"
	"strings"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"google.golang.org/protobuf/proto"
)

func TestTodo_REV_103_05(t *testing.T) {
	method := "/hcmnext.intents.v1.IntentService/GetIntent"
	if err := (DefaultValidator{}).Validate(method, &intentsv1.GetIntentRequest{IntentId: "intent-1"}); err != nil {
		t.Fatalf("valid request rejected: %+v", err)
	}
	got := Validate(method, &intentsv1.GetIntentRequest{})
	if got == nil || got.Code().String() != "INVALID_ARGUMENT" || got.ReasonRef() != reasonStructuralRejection {
		t.Fatalf("missing required field error = %+v", got)
	}
	if len(got.Violations()) != 1 || got.Violations()[0].FieldPath != "intent_id" || got.Violations()[0].RuleRef != ruleRequiredField {
		t.Fatalf("required-field violations = %+v", got.Violations())
	}

	// Admission must validate known payload shape recursively, including list
	// elements, while retaining every error path in one response.
	malformed := &intentsv1.CreateIntentRequest{
		IdempotencyKey: strings.Repeat("x", maxStringFieldBytes+1),
	}
	malformed.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	got = Validate("/hcmnext.intents.v1.IntentService/CreateIntent", malformed)
	if got == nil {
		t.Fatal("malformed request passed structural validation")
	}
	paths := map[string]string{}
	for _, violation := range got.Violations() {
		paths[violation.FieldPath] = violation.RuleRef
	}
	if paths["(request)"] != ruleUnknownField || paths["idempotency_key"] != ruleStringBound || paths["definition.intent_type_id"] != ruleRequiredField || paths["request.schema.schema_id"] != ruleRequiredField {
		t.Fatalf("combined validation paths = %#v", paths)
	}
}

func TestTodo_REV_103_05_Golden(t *testing.T) {
	method := "/hcmnext.intents.v1.IntentService/ListIntents"
	for _, size := range []int32{-1, maxPageSize + 1} {
		msg := &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: size}}
		got := Validate(method, msg)
		if got == nil || got.ReasonRef() != reasonStructuralRejection {
			t.Fatalf("page size %d was not rejected: %+v", size, got)
		}
		if len(got.Violations()) != 1 || got.Violations()[0].FieldPath != "page.page_size" || got.Violations()[0].RuleRef != rulePageSizeBound {
			t.Fatalf("page size %d violations = %+v", size, got.Violations())
		}
	}
	golden := Validate(method, &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: -1}})
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(golden.Detail())
	if err != nil {
		t.Fatal(err)
	}
	const wantWire = "0801125b0a0e706167652e706167655f73697a65122874686520706167652073697a65206d757374206265206265747765656e203020616e6420313030301a1f7374727563747572616c5f76616c69646174696f6e2e706167655f73697a65321b7374727563747572616c2e726571756573745f72656a6563746564"
	if got := hex.EncodeToString(wire); got != wantWire {
		t.Fatalf("owned validation detail wire = %s", got)
	}
	accepted := &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: maxPageSize}}
	if got := Validate(method, accepted); got != nil {
		t.Fatalf("upper-bound page size rejected: %+v", got)
	}

	// The validator also rejects unknown wire fields hidden in a nested message
	// and never modifies the decoded request while inspecting it.
	nested := &intentsv1.GetIntentRequest{IntentId: "intent-1", Scope: &commonv1.ScopeContext{Purpose: "hcm_operations"}}
	nested.Scope.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	before := proto.Clone(nested)
	got := Validate("/hcmnext.intents.v1.IntentService/GetIntent", nested)
	if got == nil || len(got.Violations()) != 1 || got.Violations()[0].FieldPath != "scope" {
		t.Fatalf("nested unknown-field violations = %+v", got)
	}
	if !proto.Equal(before, nested) {
		t.Fatal("validation mutated the request")
	}
}
