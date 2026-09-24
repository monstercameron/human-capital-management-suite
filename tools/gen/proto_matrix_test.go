package gen

import (
	"bytes"
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
)

func TestTodo_PROTO_001_Property(t *testing.T) {
	for i := int64(-100); i <= 100; i++ {
		want := &commonv1.Decimal{Sign: commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE, UnscaledMagnitude: []byte{byte(i + 100)}, Scale: int32((i + 100) % 9)}
		if i < 0 {
			want.Sign = commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE
		}
		got := roundTrip(t, want).(*commonv1.Decimal)
		if !proto.Equal(want, got) {
			t.Fatalf("decimal %d changed in wire round trip: %v", i, got)
		}
	}
}

func TestTodo_PROTO_001_Golden(t *testing.T) {
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(&commonv1.EntityRef{TenantId: "t", Kind: "worker", Id: "w"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x0a, 0x01, 't', 0x12, 0x06, 'w', 'o', 'r', 'k', 'e', 'r', 0x1a, 0x01, 'w'}
	if !bytes.Equal(wire, want) {
		t.Fatalf("EntityRef golden changed: %x", wire)
	}
}

func FuzzTodo_PROTO_001(f *testing.F) {
	f.Add([]byte{1, 2, 3}, int32(2))
	f.Add([]byte{}, int32(0))
	f.Fuzz(func(t *testing.T, magnitude []byte, scale int32) {
		if len(magnitude) > 4096 {
			t.Skip()
		}
		want := &commonv1.Decimal{Sign: commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE, UnscaledMagnitude: magnitude, Scale: scale}
		got := roundTrip(t, want).(*commonv1.Decimal)
		if !proto.Equal(want, got) {
			t.Fatalf("decimal changed: %v", got)
		}
	})
}

func TestTodo_PROTO_001_Integration(t *testing.T) {
	_, fds := buildDescriptorSet(t, findRepoRoot(t), t.TempDir(), "common.binpb")
	var found bool
	for _, fd := range fds.GetFile() {
		if fd.GetPackage() == "hcmnext.common.v1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("buf descriptor set omitted common wire primitives")
	}
}

func TestTodo_PROTO_001_Fault(t *testing.T) {
	for _, wire := range [][]byte{{0x0a, 0x05, 'x'}, {0x80}} {
		if err := proto.Unmarshal(wire, &commonv1.EntityRef{}); err == nil {
			t.Errorf("malformed wire %x was accepted", wire)
		}
	}
}

func TestTodo_PROTO_001_Mutation(t *testing.T) {
	original := &commonv1.EntityRef{TenantId: "tenant-a", Kind: "worker", Id: "w-1"}
	wire, err := proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	mutated := append([]byte(nil), wire...)
	mutated[len(mutated)-1] ^= 1
	var got commonv1.EntityRef
	if err := proto.Unmarshal(mutated, &got); err != nil {
		t.Fatal(err)
	}
	if proto.Equal(original, &got) {
		t.Fatal("mutating the identifier wire byte did not change the decoded identifier")
	}
}

func TestTodo_PROTO_002_Golden(t *testing.T) {
	md := (&registryv1.ListIntentDefinitionsRequest{}).ProtoReflect().Descriptor()
	for _, name := range []string{"scope", "page"} {
		if md.Fields().ByName(protoreflectName(name)) == nil {
			t.Fatalf("ListIntentDefinitionsRequest lost %s", name)
		}
	}
}

func FuzzTodo_PROTO_002(f *testing.F) {
	f.Add("worker", uint32(1))
	f.Add("", uint32(0))
	f.Fuzz(func(t *testing.T, id string, version uint32) {
		if len(id) > 4096 {
			t.Skip()
		}
		want := &intentsv1.DefinitionReference{IntentTypeId: id, Version: version}
		got := roundTrip(t, want).(*intentsv1.DefinitionReference)
		if !proto.Equal(want, got) {
			t.Fatalf("definition reference changed: %v", got)
		}
	})
}

func TestTodo_PROTO_002_Integration(t *testing.T) {
	_, fds := buildDescriptorSet(t, findRepoRoot(t), t.TempDir(), "services.binpb")
	want := map[string]bool{"IntentService": false, "RegistryService": false}
	for _, fd := range fds.GetFile() {
		for _, svc := range fd.GetService() {
			if _, ok := want[svc.GetName()]; ok {
				want[svc.GetName()] = len(svc.GetMethod()) > 0
			}
		}
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("generated descriptor lacks callable %s", name)
		}
	}
}

func TestTodo_PROTO_002_Race(t *testing.T) {
	const n = 16
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			m := &intentsv1.DefinitionReference{IntentTypeId: fmt.Sprintf("intent.%d", i), Version: uint32(i + 1)}
			b, e := proto.MarshalOptions{Deterministic: true}.Marshal(m)
			if e == nil {
				var got intentsv1.DefinitionReference
				e = proto.Unmarshal(b, &got)
				if e == nil && !proto.Equal(m, &got) {
					e = fmt.Errorf("parallel round trip mismatch")
				}
			}
			errs <- e
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_PROTO_002_Security(t *testing.T) {
	md := (&registryv1.ListCapabilitiesRequest{}).ProtoReflect().Descriptor()
	for _, name := range []string{"scope", "page"} {
		if md.Fields().ByName(protoreflectName(name)) == nil {
			t.Fatalf("authorization-safe pagination contract lost %s", name)
		}
	}
}

func protoreflectName(s string) protoreflect.Name { return protoreflect.Name(s) }
