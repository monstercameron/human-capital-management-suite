package openapi

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// schemaRefPrefix is where every named schema lives.
const schemaRefPrefix = "#/components/schemas/"

// schemaBuilder renders message and enum descriptors as OpenAPI schemas under
// the Protobuf JSON mapping. Each named type is rendered once; a recursive
// message refers to itself through $ref, so the walk terminates.
type schemaBuilder struct {
	schemas map[string]omap
	pending []protoreflect.Descriptor
}

func newSchemaBuilder() *schemaBuilder {
	return &schemaBuilder{schemas: map[string]omap{}}
}

// ref returns a $ref to the named schema for d and queues d for rendering.
// Well-known types are inlined instead (see wellKnown).
func (b *schemaBuilder) ref(d protoreflect.Descriptor) omap {
	if md, ok := d.(protoreflect.MessageDescriptor); ok {
		if s, ok := wellKnown(md.FullName()); ok {
			return s
		}
	}
	name := string(d.FullName())
	if _, done := b.schemas[name]; !done {
		b.schemas[name] = nil // reserve: renders once, breaks recursion
		b.pending = append(b.pending, d)
	}
	return omap{{"$ref", schemaRefPrefix + name}}
}

// drain renders every queued descriptor until the reachable set is closed.
func (b *schemaBuilder) drain() {
	for len(b.pending) > 0 {
		d := b.pending[0]
		b.pending = b.pending[1:]
		switch t := d.(type) {
		case protoreflect.MessageDescriptor:
			b.schemas[string(t.FullName())] = b.message(t)
		case protoreflect.EnumDescriptor:
			b.schemas[string(t.FullName())] = enumSchema(t)
		}
	}
}

// names returns every rendered schema name, sorted.
func (b *schemaBuilder) names() []string {
	out := make([]string, 0, len(b.schemas))
	for n := range b.schemas {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// message renders one message: its fields in declaration order under their
// JSON names, with real oneofs described as mutually exclusive.
func (b *schemaBuilder) message(md protoreflect.MessageDescriptor) omap {
	desc := []string{fmt.Sprintf("Protobuf message `%s`.", md.FullName())}
	oneofMembers := map[protoreflect.FullName][]string{}
	for i := 0; i < md.Oneofs().Len(); i++ {
		od := md.Oneofs().Get(i)
		if od.IsSynthetic() {
			continue
		}
		var names []string
		for j := 0; j < od.Fields().Len(); j++ {
			names = append(names, od.Fields().Get(j).JSONName())
		}
		oneofMembers[od.FullName()] = names
		desc = append(desc, fmt.Sprintf("Oneof `%s`: at most one of %s may be set.", od.Name(), backtickList(names)))
	}
	s := omap{{"type", "object"}, {"description", strings.Join(desc, " ")}}
	props := omap{}
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		fs := b.field(fd)
		if od := fd.ContainingOneof(); od != nil && !od.IsSynthetic() {
			var others []string
			for _, n := range oneofMembers[od.FullName()] {
				if n != fd.JSONName() {
					others = append(others, n)
				}
			}
			note := fmt.Sprintf("Member of oneof `%s`.", od.Name())
			if len(others) > 0 {
				note += " Mutually exclusive with " + backtickList(others) + "."
			}
			fs = withDescription(fs, note)
		}
		props.set(fd.JSONName(), fs)
	}
	if len(props) > 0 {
		s.set("properties", props)
	}
	return s
}

// field renders a field including its cardinality.
func (b *schemaBuilder) field(fd protoreflect.FieldDescriptor) omap {
	if fd.IsMap() {
		return omap{
			{"type", "object"},
			{"additionalProperties", b.singular(fd.MapValue())},
			{"description", fmt.Sprintf("Map keyed by %s (JSON object keys are strings).", fd.MapKey().Kind())},
		}
	}
	if fd.IsList() {
		return omap{{"type", "array"}, {"items", b.singular(fd)}}
	}
	return b.singular(fd)
}

// singular renders the value type of fd, ignoring cardinality.
func (b *schemaBuilder) singular(fd protoreflect.FieldDescriptor) omap {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return omap{{"type", "boolean"}}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return omap{{"type", "integer"}, {"format", "int32"}}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return omap{{"type", "integer"}, {"format", "uint32"}, {"minimum", 0}}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return omap{{"type", "string"}, {"format", "int64"}}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return omap{{"type", "string"}, {"format", "uint64"}}
	case protoreflect.FloatKind:
		return omap{{"type", "number"}, {"format", "float"}}
	case protoreflect.DoubleKind:
		return omap{{"type", "number"}, {"format", "double"}}
	case protoreflect.StringKind:
		return omap{{"type", "string"}}
	case protoreflect.BytesKind:
		return omap{{"type", "string"}, {"format", "byte"}}
	case protoreflect.EnumKind:
		if fd.Enum().FullName() == "google.protobuf.NullValue" {
			return omap{{"type", "null"}}
		}
		return b.ref(fd.Enum())
	default: // MessageKind, GroupKind
		return b.ref(fd.Message())
	}
}

// enumSchema renders an enum as its value names.
func enumSchema(ed protoreflect.EnumDescriptor) omap {
	values := make([]any, 0, ed.Values().Len())
	for i := 0; i < ed.Values().Len(); i++ {
		values = append(values, string(ed.Values().Get(i).Name()))
	}
	return omap{
		{"type", "string"},
		{"description", fmt.Sprintf("Protobuf enum `%s`. The JSON mapping writes value names; parsers also accept the numeric value.", ed.FullName())},
		{"enum", values},
	}
}

// wellKnown returns the inline JSON-mapping schema of a google.protobuf
// well-known type.
func wellKnown(name protoreflect.FullName) (omap, bool) {
	nullable := func(typ, format string) omap {
		s := omap{{"type", []any{typ, "null"}}}
		if format != "" {
			s.set("format", format)
		}
		return s
	}
	switch name {
	case "google.protobuf.Timestamp":
		return omap{{"type", "string"}, {"format", "date-time"}, {"description", "RFC 3339 timestamp in UTC, e.g. 2026-09-19T12:00:00Z."}}, true
	case "google.protobuf.Duration":
		return omap{{"type", "string"}, {"pattern", `^-?[0-9]+(\.[0-9]+)?s$`}, {"description", "Duration in seconds with an s suffix, e.g. 3.5s."}}, true
	case "google.protobuf.Struct":
		return omap{{"type", "object"}, {"additionalProperties", true}}, true
	case "google.protobuf.Value":
		return omap{}, true
	case "google.protobuf.ListValue":
		return omap{{"type", "array"}, {"items", omap{}}}, true
	case "google.protobuf.Empty":
		return omap{{"type", "object"}}, true
	case "google.protobuf.FieldMask":
		return omap{{"type", "string"}, {"description", "Comma-separated lowerCamelCase field paths."}}, true
	case "google.protobuf.Any":
		return omap{{"type", "object"}, {"properties", omap{{"@type", omap{{"type", "string"}}}}}, {"additionalProperties", true}}, true
	case "google.protobuf.BoolValue":
		return nullable("boolean", ""), true
	case "google.protobuf.StringValue":
		return nullable("string", ""), true
	case "google.protobuf.BytesValue":
		return nullable("string", "byte"), true
	case "google.protobuf.Int32Value":
		return nullable("integer", "int32"), true
	case "google.protobuf.UInt32Value":
		return nullable("integer", "uint32"), true
	case "google.protobuf.Int64Value":
		return nullable("string", "int64"), true
	case "google.protobuf.UInt64Value":
		return nullable("string", "uint64"), true
	case "google.protobuf.FloatValue":
		return nullable("number", "float"), true
	case "google.protobuf.DoubleValue":
		return nullable("number", "double"), true
	}
	return nil, false
}

// withDescription returns s with note appended to (or set as) its
// description.
func withDescription(s omap, note string) omap {
	out := make(omap, 0, len(s)+1)
	found := false
	for _, e := range s {
		if e.k == "description" {
			e.v = e.v.(string) + " " + note
			found = true
		}
		out = append(out, e)
	}
	if !found {
		out.set("description", note)
	}
	return out
}

func backtickList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, ", ")
}
