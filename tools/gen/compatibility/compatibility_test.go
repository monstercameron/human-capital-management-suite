package compatibility

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

func TestContractCompatibility(t *testing.T) {
	base := contract(KindDomain, "worker", 1, "sha256:material-v1", descriptorSet(descriptorMessage("Worker",
		field("worker_id", 1),
	)))

	tests := []struct {
		name         string
		previous     Contract
		current      Contract
		wantCode     ViolationCode
		wantForward  Classification
		wantBackward Classification
	}{
		{
			name:     "field number reuse is rejected",
			previous: base,
			current: contract(KindDomain, "worker", 2, "sha256:material-v1", descriptorSet(descriptorMessage("Worker",
				field("legacy_worker_id", 1),
			))),
			wantCode: ViolationFieldNumberReuse, wantForward: Incompatible, wantBackward: Incompatible,
		},
		{
			name:     "removed required semantics are rejected",
			previous: withRequired(base, "hcmnext.fixture.Worker.worker_id"),
			current: contract(KindDomain, "worker", 2, "sha256:material-v1", descriptorSet(descriptorMessage("Worker",
				field("worker_id", 1),
			))),
			wantCode: ViolationRequiredSemanticsRemoved, wantForward: Compatible, wantBackward: Incompatible,
		},
		{
			name: "incompatible oneof change is rejected",
			previous: contract(KindEvent, "worker.changed", 1, "sha256:material-v1", descriptorSet(messageWithOneof("WorkerChanged", "contact",
				oneofField("email", 1, 0), oneofField("phone", 2, 0),
			))),
			current: contract(KindEvent, "worker.changed", 2, "sha256:material-v1", descriptorSet(messageWithOneof("WorkerChanged", "contact",
				field("email", 1), oneofField("phone", 2, 0),
			))),
			wantCode: ViolationIncompatibleOneofChange, wantForward: Incompatible, wantBackward: Incompatible,
		},
		{
			name: "new oneof alternative is rejected",
			previous: contract(KindEvent, "worker.changed", 1, "sha256:material-v1", descriptorSet(messageWithOneof("WorkerChanged", "contact",
				oneofField("email", 1, 0),
			))),
			current: contract(KindEvent, "worker.changed", 2, "sha256:material-v1", descriptorSet(messageWithOneof("WorkerChanged", "contact",
				oneofField("email", 1, 0), oneofField("phone", 2, 0),
			))),
			wantCode: ViolationIncompatibleOneofChange, wantForward: Incompatible, wantBackward: Incompatible,
		},
		{
			name:     "material change requires a newer version",
			previous: base,
			current:  contract(KindDomain, "worker", 1, "sha256:material-v2", descriptorSet(descriptorMessage("Worker", field("worker_id", 1)))),
			wantCode: ViolationMaterialChangeWithoutVersionBump, wantForward: Incompatible, wantBackward: Incompatible,
		},
		{
			name:        "material change with a newer version is classified compatible",
			previous:    base,
			current:     contract(KindDomain, "worker", 2, "sha256:material-v2", descriptorSet(descriptorMessage("Worker", field("worker_id", 1)))),
			wantForward: Compatible, wantBackward: Compatible,
		},
		{
			name:     "additive optional field is forward and backward compatible",
			previous: base,
			current: contract(KindDomain, "worker", 1, "sha256:material-v1", descriptorSet(descriptorMessage("Worker",
				field("worker_id", 1), field("display_name", 2),
			))),
			wantForward: Compatible, wantBackward: Compatible,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := Check(tc.previous, tc.current)
			if tc.wantCode == "" {
				if !report.OK() {
					t.Fatalf("Check() violations = %+v, want none", report.Violations)
				}
			} else if !hasViolation(report, tc.wantCode) {
				t.Fatalf("Check() violations = %+v, want %s", report.Violations, tc.wantCode)
			}
			if report.Forward != tc.wantForward || report.Backward != tc.wantBackward {
				t.Fatalf("Check() classification = forward %s / backward %s, want %s / %s",
					report.Forward, report.Backward, tc.wantForward, tc.wantBackward)
			}
		})
	}
}

// TestTodo_TOOL_006_Golden pins the stable report for an additive event
// evolution. Its compact JSON form is suitable for a checked-in compatibility
// receipt: classification and paths do not depend on descriptor-set ordering.
func TestTodo_TOOL_006_Golden(t *testing.T) {
	previous := contract(KindEvent, "worker.changed", 1, "sha256:event-v1", descriptorSet(descriptorMessage("WorkerChanged",
		field("worker_id", 1),
	)))
	current := contract(KindEvent, "worker.changed", 1, "sha256:event-v1", descriptorSet(descriptorMessage("WorkerChanged",
		field("worker_id", 1), field("effective_date", 2),
	)))

	got, err := json.MarshalIndent(Check(previous, current), "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	const want = `{
  "kind": "event",
  "name": "worker.changed",
  "previous": 1,
  "current": 1,
  "forward": "compatible",
  "backward": "compatible",
  "changes": [
    {
      "code": "field_added",
      "path": "hcmnext.fixture.WorkerChanged.effective_date",
      "detail": "field number 2 was added"
    }
  ],
  "violations": []
}`
	if string(got) != want {
		t.Errorf("golden report changed:\n got %s\nwant %s", got, want)
	}
}

// TestTodo_TOOL_006_Security proves malformed input cannot use a version bump
// to bypass the policy, and invalid descriptor/required-path input is rejected
// rather than silently skipped.
func TestTodo_TOOL_006_Security(t *testing.T) {
	previous := contract(KindConnector, "workday", 1, "sha256:connector-v1", descriptorSet(descriptorMessage("Connection",
		field("connection_id", 1),
	)))

	t.Run("version bump cannot waive field number reuse", func(t *testing.T) {
		current := contract(KindConnector, "workday", 99, "sha256:connector-v2", descriptorSet(descriptorMessage("Connection",
			field("credential_ref", 1),
		)))
		report := Check(previous, current)
		if report.OK() || !hasViolation(report, ViolationFieldNumberReuse) {
			t.Fatalf("Check() = %+v, want a field-number-reuse rejection", report)
		}
	})

	t.Run("duplicate field numbers are invalid", func(t *testing.T) {
		invalid := contract(KindFile, "worker.csv", 1, "sha256:file-v1", descriptorSet(descriptorMessage("Row",
			field("worker_id", 1), field("external_id", 1),
		)))
		report := Check(invalid, invalid)
		if report.OK() || !hasViolation(report, ViolationInvalidContract) {
			t.Fatalf("Check() = %+v, want invalid-contract rejection", report)
		}
	})

	t.Run("undeclared required path is invalid", func(t *testing.T) {
		invalid := withRequired(previous, "hcmnext.fixture.Connection.missing")
		report := Check(invalid, invalid)
		if report.OK() || !hasViolation(report, ViolationInvalidContract) {
			t.Fatalf("Check() = %+v, want invalid-contract rejection", report)
		}
	})
}

// TestTodo_TOOL_006_Conformance runs the same policy over descriptors emitted
// by the checked-in generated contracts. No network, registry, or Buf command
// is involved: the descriptors linked into generated Go are sufficient for an
// offline domain, event, file, and connector compatibility check.
func TestTodo_TOOL_006_Conformance(t *testing.T) {
	tests := []struct {
		kind     Kind
		name     string
		fd       protoreflect.FileDescriptor
		required string
	}{
		{KindDomain, "intent-definition", intentsv1.File_hcmnext_intents_v1_business_intent_proto, "hcmnext.intents.v1.DefinitionReference.intent_type_id"},
		{KindEvent, "entity-reference", commonv1.File_hcmnext_common_v1_common_proto, "hcmnext.common.v1.EntityRef.id"},
		{KindFile, "zero-effect-receipt", evidencev1.File_hcmnext_evidence_v1_receipt_proto, "hcmnext.evidence.v1.ZeroEffectReceipt.intent_type"},
		{KindConnector, "connector-definition", integrationv1.File_hcmnext_integration_v1_connector_proto, "hcmnext.integration.v1.ConnectorDefinition.connector_id"},
	}
	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			contract := Contract{
				Kind:           tc.kind,
				Name:           tc.name,
				Version:        1,
				MaterialDigest: "sha256:checked-in-generated-descriptor",
				Descriptors:    descriptorSetFrom(tc.fd),
				RequiredPaths:  []string{tc.required},
			}
			report := Check(contract, contract)
			if !report.OK() || report.Forward != Compatible || report.Backward != Compatible {
				t.Fatalf("generated %s contract report = %+v, want compatible", tc.kind, report)
			}
		})
	}
}

func contract(kind Kind, name string, version uint32, material string, descriptors *descriptorpb.FileDescriptorSet) Contract {
	return Contract{Kind: kind, Name: name, Version: version, MaterialDigest: material, Descriptors: descriptors}
}

func withRequired(c Contract, paths ...string) Contract {
	c.RequiredPaths = append([]string(nil), paths...)
	return c
}

func descriptorSet(messages ...*descriptorpb.DescriptorProto) *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{
		Name:        stringPtr("fixture.proto"),
		Package:     stringPtr("hcmnext.fixture"),
		Syntax:      stringPtr("proto3"),
		MessageType: messages,
	}}}
}

func descriptorSetFrom(fd protoreflect.FileDescriptor) *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{protodesc.ToFileDescriptorProto(fd)}}
}

func descriptorMessage(name string, fields ...*descriptorpb.FieldDescriptorProto) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{Name: stringPtr(name), Field: fields}
}

func messageWithOneof(name, oneof string, fields ...*descriptorpb.FieldDescriptorProto) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{
		Name:      stringPtr(name),
		Field:     fields,
		OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: stringPtr(oneof)}},
	}
}

func field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   stringPtr(name),
		Number: int32Ptr(number),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
	}
}

func oneofField(name string, number, index int32) *descriptorpb.FieldDescriptorProto {
	f := field(name, number)
	f.OneofIndex = int32Ptr(index)
	return f
}

func stringPtr(v string) *string { return &v }
func int32Ptr(v int32) *int32    { return &v }

func hasViolation(report Report, code ViolationCode) bool {
	for _, violation := range report.Violations {
		if violation.Code == code {
			return true
		}
	}
	return false
}

func TestTodo_TOOL_006_ReportOrdering(t *testing.T) {
	// A tiny regression check for descriptor map iteration: Check must sort its
	// observable report rather than leak Go's randomized map order into CI.
	previous := contract(KindEvent, "ordering", 1, "sha256:v1", descriptorSet(descriptorMessage("Order", field("b", 1), field("a", 2))))
	current := contract(KindEvent, "ordering", 1, "sha256:v1", descriptorSet(descriptorMessage("Order", field("b", 1))))
	first := Check(previous, current)
	for range 32 {
		if got := Check(previous, current); !sameReport(first, got) {
			t.Fatalf("report order changed: first=%+v got=%+v", first, got)
		}
	}
}

func sameReport(a, b Report) bool {
	if a.Kind != b.Kind || a.Name != b.Name || a.Previous != b.Previous || a.Current != b.Current || a.Forward != b.Forward || a.Backward != b.Backward || len(a.Changes) != len(b.Changes) || len(a.Violations) != len(b.Violations) {
		return false
	}
	for i := range a.Changes {
		if a.Changes[i] != b.Changes[i] {
			return false
		}
	}
	for i := range a.Violations {
		if a.Violations[i] != b.Violations[i] {
			return false
		}
	}
	return true
}

func TestTodo_TOOL_006_RequiredPathFormat(t *testing.T) {
	// A malformed path must not be accepted by accidental string containment.
	contract := withRequired(contract(KindEvent, "path", 1, "sha256:v1", descriptorSet(descriptorMessage("Path", field("value", 1)))), "hcmnext.fixture.Path.value.extra")
	report := Check(contract, contract)
	if report.OK() || !strings.Contains(report.Violations[0].Detail, "required path") {
		t.Fatalf("Check() = %+v, want exact required-path rejection", report)
	}
}
