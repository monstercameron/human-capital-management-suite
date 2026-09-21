package openapi

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	// Linked so the synthetic file below can import them.
	_ "google.golang.org/protobuf/types/known/durationpb"
	_ "google.golang.org/protobuf/types/known/structpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
	_ "google.golang.org/protobuf/types/known/wrapperspb"
)

func fieldProto(name string, num int32, typ descriptorpb.FieldDescriptorProto_Type, label descriptorpb.FieldDescriptorProto_Label, typeName string) *descriptorpb.FieldDescriptorProto {
	f := &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(num),
		Type:   typ.Enum(),
		Label:  label.Enum(),
	}
	if typeName != "" {
		f.TypeName = proto.String(typeName)
	}
	return f
}

// sampleFile builds a proto3 file exercising every JSON-mapping branch: all
// scalar kinds, an enum, a recursive message, a map, a repeated field, a real
// oneof, a proto3 optional (synthetic oneof) and well-known types. It also
// declares a service with unary and streaming methods.
func sampleFile(t *testing.T, withErrorDetail bool) (*protoregistry.Files, protoreflect.FileDescriptor) {
	t.Helper()
	const (
		opt = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		rep = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	)
	T := func(v descriptorpb.FieldDescriptorProto_Type) descriptorpb.FieldDescriptorProto_Type { return v }
	fields := []*descriptorpb.FieldDescriptorProto{
		fieldProto("b", 1, T(descriptorpb.FieldDescriptorProto_TYPE_BOOL), opt, ""),
		fieldProto("i32", 2, T(descriptorpb.FieldDescriptorProto_TYPE_INT32), opt, ""),
		fieldProto("u32", 3, T(descriptorpb.FieldDescriptorProto_TYPE_UINT32), opt, ""),
		fieldProto("i64", 4, T(descriptorpb.FieldDescriptorProto_TYPE_INT64), opt, ""),
		fieldProto("u64", 5, T(descriptorpb.FieldDescriptorProto_TYPE_FIXED64), opt, ""),
		fieldProto("f", 6, T(descriptorpb.FieldDescriptorProto_TYPE_FLOAT), opt, ""),
		fieldProto("d", 7, T(descriptorpb.FieldDescriptorProto_TYPE_DOUBLE), opt, ""),
		fieldProto("s", 8, T(descriptorpb.FieldDescriptorProto_TYPE_STRING), opt, ""),
		fieldProto("raw_bytes", 9, T(descriptorpb.FieldDescriptorProto_TYPE_BYTES), opt, ""),
		fieldProto("color", 10, T(descriptorpb.FieldDescriptorProto_TYPE_ENUM), opt, ".hcmnext.sample.v1.Color"),
		fieldProto("children", 11, T(descriptorpb.FieldDescriptorProto_TYPE_MESSAGE), rep, ".hcmnext.sample.v1.Node"),
		fieldProto("counts", 12, T(descriptorpb.FieldDescriptorProto_TYPE_MESSAGE), rep, ".hcmnext.sample.v1.Node.CountsEntry"),
		fieldProto("text", 13, T(descriptorpb.FieldDescriptorProto_TYPE_STRING), opt, ""),
		fieldProto("number", 14, T(descriptorpb.FieldDescriptorProto_TYPE_INT32), opt, ""),
		fieldProto("maybe", 15, T(descriptorpb.FieldDescriptorProto_TYPE_STRING), opt, ""),
		fieldProto("at", 16, T(descriptorpb.FieldDescriptorProto_TYPE_MESSAGE), opt, ".google.protobuf.Timestamp"),
		fieldProto("wait", 17, T(descriptorpb.FieldDescriptorProto_TYPE_MESSAGE), opt, ".google.protobuf.Duration"),
		fieldProto("wrapped", 18, T(descriptorpb.FieldDescriptorProto_TYPE_MESSAGE), opt, ".google.protobuf.Int64Value"),
		fieldProto("nothing", 19, T(descriptorpb.FieldDescriptorProto_TYPE_ENUM), opt, ".google.protobuf.NullValue"),
	}
	fields[12].OneofIndex = proto.Int32(0)
	fields[13].OneofIndex = proto.Int32(0)
	fields[14].OneofIndex = proto.Int32(1)
	fields[14].Proto3Optional = proto.Bool(true)
	file := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("hcmnext/sample/v1/sample.proto"),
		Package:    proto.String("hcmnext.sample.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/timestamp.proto", "google/protobuf/duration.proto", "google/protobuf/wrappers.proto", "google/protobuf/struct.proto"},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Color"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("COLOR_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("COLOR_RED"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:      proto.String("Node"),
			Field:     fields,
			OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("choice")}, {Name: proto.String("_maybe")}},
			NestedType: []*descriptorpb.DescriptorProto{{
				Name:    proto.String("CountsEntry"),
				Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
				Field: []*descriptorpb.FieldDescriptorProto{
					fieldProto("key", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, opt, ""),
					fieldProto("value", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64, opt, ""),
				},
			}},
		}, {
			Name: proto.String("Empty"),
		}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("SampleService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: proto.String("GetNode"), InputType: proto.String(".hcmnext.sample.v1.Empty"), OutputType: proto.String(".hcmnext.sample.v1.Node")},
				{Name: proto.String("WatchNode"), InputType: proto.String(".hcmnext.sample.v1.Empty"), OutputType: proto.String(".hcmnext.sample.v1.Node"), ServerStreaming: proto.Bool(true)},
				{Name: proto.String("PushNodes"), InputType: proto.String(".hcmnext.sample.v1.Node"), OutputType: proto.String(".hcmnext.sample.v1.Empty"), ClientStreaming: proto.Bool(true)},
				{Name: proto.String("ChatNodes"), InputType: proto.String(".hcmnext.sample.v1.Node"), OutputType: proto.String(".hcmnext.sample.v1.Node"), ClientStreaming: proto.Bool(true), ServerStreaming: proto.Bool(true)},
			},
		}},
	}
	fd, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	reg := new(protoregistry.Files)
	if err := reg.RegisterFile(fd); err != nil {
		t.Fatal(err)
	}
	if withErrorDetail {
		ed, err := protoregistry.GlobalFiles.FindFileByPath("hcmnext/common/v1/common.proto")
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.RegisterFile(ed); err != nil {
			t.Fatal(err)
		}
	}
	return reg, fd
}

func TestSchemaBuilderFollowsTheProtobufJSONMapping(t *testing.T) {
	_, fd := sampleFile(t, false)
	sb := newSchemaBuilder()
	ref := sb.ref(fd.Messages().ByName("Node"))
	if v, _ := ref.get("$ref"); v != schemaRefPrefix+"hcmnext.sample.v1.Node" {
		t.Fatalf("ref = %v", ref)
	}
	sb.drain()
	names := strings.Join(sb.names(), ",")
	if names != "hcmnext.sample.v1.Color,hcmnext.sample.v1.Node" {
		t.Fatalf("schemas = %s (map entries and well-known types must not be named)", names)
	}
	node := sb.schemas["hcmnext.sample.v1.Node"]
	propsV, _ := node.get("properties")
	props := propsV.(omap)
	prop := func(name string) omap {
		v, ok := props.get(name)
		if !ok {
			t.Fatalf("property %s missing; have %v", name, props)
		}
		return v.(omap)
	}
	check := func(name, key string, want any) {
		t.Helper()
		got, _ := prop(name).get(key)
		if s, ok := want.(string); ok && got != s {
			t.Errorf("%s.%s = %v, want %v", name, key, got, want)
		}
		if n, ok := want.(int); ok && got != n {
			t.Errorf("%s.%s = %v, want %v", name, key, got, want)
		}
	}
	check("b", "type", "boolean")
	check("i32", "format", "int32")
	check("u32", "minimum", 0)
	check("i64", "type", "string")
	check("i64", "format", "int64")
	check("u64", "format", "uint64")
	check("f", "format", "float")
	check("d", "type", "number")
	check("s", "type", "string")
	check("rawBytes", "format", "byte")
	check("color", "$ref", schemaRefPrefix+"hcmnext.sample.v1.Color")
	check("children", "type", "array")
	check("counts", "type", "object")
	check("at", "format", "date-time")
	check("wait", "type", "string")
	check("nothing", "type", "null")
	items, _ := prop("children").get("items")
	if v, _ := items.(omap).get("$ref"); v != schemaRefPrefix+"hcmnext.sample.v1.Node" {
		t.Errorf("recursive children items = %v", items)
	}
	addl, _ := prop("counts").get("additionalProperties")
	if v, _ := addl.(omap).get("format"); v != "int64" {
		t.Errorf("map value schema = %v", addl)
	}
	wrapped, _ := prop("wrapped").get("type")
	if w, ok := wrapped.([]any); !ok || len(w) != 2 || w[1] != "null" {
		t.Errorf("wrapper type = %v", wrapped)
	}
	textDesc, _ := prop("text").get("description")
	if !strings.Contains(textDesc.(string), "Mutually exclusive with `number`") {
		t.Errorf("oneof member description = %v", textDesc)
	}
	if _, ok := prop("maybe").get("description"); ok {
		t.Error("proto3 optional was treated as a real oneof")
	}
	desc, _ := node.get("description")
	if !strings.Contains(desc.(string), "Oneof `choice`: at most one of `text`, `number`") || strings.Contains(desc.(string), "_maybe") {
		t.Errorf("message description = %v", desc)
	}
	enumValues, _ := sb.schemas["hcmnext.sample.v1.Color"].get("enum")
	if len(enumValues.([]any)) != 2 || enumValues.([]any)[1] != "COLOR_RED" {
		t.Errorf("enum = %v", enumValues)
	}

	emptySB := newSchemaBuilder()
	emptySB.ref(fd.Messages().ByName("Empty"))
	emptySB.drain()
	if _, ok := emptySB.schemas["hcmnext.sample.v1.Empty"].get("properties"); ok {
		t.Error("an empty message rendered properties")
	}
}

func TestWellKnownTypes(t *testing.T) {
	for name, wantType := range map[protoreflect.FullName]string{
		"google.protobuf.Timestamp": "string", "google.protobuf.Duration": "string",
		"google.protobuf.Struct": "object", "google.protobuf.ListValue": "array",
		"google.protobuf.Empty": "object", "google.protobuf.FieldMask": "string",
		"google.protobuf.Any": "object",
	} {
		s, ok := wellKnown(name)
		if got, _ := s.get("type"); !ok || got != wantType {
			t.Errorf("%s type = %v", name, got)
		}
	}
	if s, ok := wellKnown("google.protobuf.Value"); !ok || len(s) != 0 {
		t.Errorf("Value = %v, want the any-schema {}", s)
	}
	for name, want := range map[protoreflect.FullName]string{
		"google.protobuf.BoolValue": "boolean", "google.protobuf.StringValue": "string",
		"google.protobuf.BytesValue": "string", "google.protobuf.Int32Value": "integer",
		"google.protobuf.UInt32Value": "integer", "google.protobuf.Int64Value": "string",
		"google.protobuf.UInt64Value": "string", "google.protobuf.FloatValue": "number",
		"google.protobuf.DoubleValue": "number",
	} {
		s, ok := wellKnown(name)
		typ, _ := s.get("type")
		if list, isList := typ.([]any); !ok || !isList || list[0] != want || list[1] != "null" {
			t.Errorf("%s type = %v", name, typ)
		}
	}
	if _, ok := wellKnown("hcmnext.common.v1.Money"); ok {
		t.Error("a non-WKT was treated as well known")
	}
}

func TestWithDescriptionAppendsOrSets(t *testing.T) {
	a := withDescription(omap{{"type", "string"}}, "note")
	if v, _ := a.get("description"); v != "note" {
		t.Fatalf("set = %v", a)
	}
	b := withDescription(omap{{"description", "base"}}, "note")
	if v, _ := b.get("description"); v != "base note" || len(b) != 1 {
		t.Fatalf("append = %v", b)
	}
}
