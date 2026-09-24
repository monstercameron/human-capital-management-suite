package transformation

import (
	"errors"
	"testing"
)

func definition() TransformationDefinition {
	return TransformationDefinition{Version: ContractVersion, Name: "people-copy", Owner: "shared-engines", Phase: "P1A",
		Source:        Schema{Name: "people", Version: 1, Fields: []Field{{Name: "given", Type: TypeString, Required: true}, {Name: "age", Type: TypeInt}}},
		Destination:   Schema{Name: "worker", Version: 1, Fields: []Field{{Name: "name", Type: TypeString, Required: true}, {Name: "age", Type: TypeInt}}},
		Operations:    []Operation{{Kind: OpCopy, Source: &Path{Schema: "people", Field: "given", Type: TypeString}, Destination: Path{Schema: "worker", Field: "name", Type: TypeString}}},
		Compatibility: Compatibility{MinimumSourceVersion: 1}, Limits: ResourceLimits{MaxOperations: 4, MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxExpansion: 1}, Failure: FailureReject, SideEffects: SideEffectsNone}
}

func TestValidateAndDigest(t *testing.T) {
	d := definition()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	digest, err := d.Digest()
	if err != nil || len(digest) != 71 {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
}
func TestRejectsUntypedAndArbitraryOperations(t *testing.T) {
	d := definition()
	d.Operations[0].Source.Type = TypeBool
	if !errors.Is(d.Validate(), ErrUntypedPath) {
		t.Fatal("expected typed path error")
	}
	d = definition()
	d.Operations[0].Kind = "script"
	if !errors.Is(d.Validate(), ErrArbitraryCode) {
		t.Fatal("expected code error")
	}
}
func TestDigestStableAcrossSchemaFieldOrder(t *testing.T) {
	a := definition()
	b := definition()
	b.Source.Fields[0], b.Source.Fields[1] = b.Source.Fields[1], b.Source.Fields[0]
	da, _ := a.Digest()
	db, _ := b.Digest()
	if da != db {
		t.Fatalf("digest changed: %s != %s", da, db)
	}
}

// The registry's XFORM-001 matrix is intentionally kept beside the contract;
// these tests are small independent checks so a future implementation cannot
// satisfy the primary case while dropping one of the safety boundaries.
func TestTodo_XFORM_001(t *testing.T) {
	d := definition()
	if err := d.Validate(); err != nil {
		t.Fatalf("well-typed copy contract was rejected: %v", err)
	}
	digest, err := d.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != len("sha256:")+64 || digest[:len("sha256:")] != "sha256:" {
		t.Fatalf("contract digest=%q, want sha256 digest", digest)
	}
	if d.Operations[0].Source.Type != TypeString || d.Operations[0].Destination.Type != TypeString {
		t.Fatalf("copy contract paths lost their declared types: %+v", d.Operations[0])
	}
}

func TestTodo_XFORM_001_Property(t *testing.T) {
	d := definition()
	for _, op := range []OperationKind{OpCopy, OpRename} {
		d.Operations[0].Kind = op
		if err := d.Validate(); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	d.Operations[0].Kind = OpConvert
	d.Operations[0].TargetType = TypeInt
	d.Operations[0].Destination = Path{Schema: "worker", Field: "age", Type: TypeInt}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_XFORM_001_Golden(t *testing.T) {
	d := definition()
	digest, err := d.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:"
	if len(digest) != len(want)+64 || digest[:len(want)] != want {
		t.Fatalf("digest %q is not a sha256 golden shape", digest)
	}
}

func FuzzTodo_XFORM_001(f *testing.F) {
	f.Add("people-copy", "shared-engines", "P1A")
	f.Add("", "owner", "phase")
	f.Fuzz(func(t *testing.T, name, owner, phase string) {
		d := definition()
		d.Name, d.Owner, d.Phase = name, owner, phase
		// Validation must be total for arbitrary metadata and never panic.
		_ = d.Validate()
	})
}

func TestTodo_XFORM_001_Conformance(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*TransformationDefinition)
		want   error
	}{
		{"ambient", func(d *TransformationDefinition) { d.AmbientDependencies = []string{"TZ"} }, ErrAmbientDependency},
		{"arbitrary", func(d *TransformationDefinition) { d.Operations[0].Kind = "script" }, ErrArbitraryCode},
		{"side-effects", func(d *TransformationDefinition) { d.SideEffects = SideEffectPolicy("write") }, ErrUndeclaredEffect},
		{"untyped", func(d *TransformationDefinition) { d.Operations[0].Destination.Type = TypeBool }, ErrUntypedPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := definition()
			tc.mutate(&d)
			if !errors.Is(d.Validate(), tc.want) {
				t.Fatalf("got %v, want %v", d.Validate(), tc.want)
			}
		})
	}
}

func TestTodo_XFORM_001_Mutation(t *testing.T) {
	d := definition()
	d.Limits.MaxOperations = 0
	if !errors.Is(d.Validate(), ErrInvalidDefinition) {
		t.Fatal("zero operation budget must be rejected")
	}
	d = definition()
	d.Compatibility.MaximumSourceVersion = 0
	d.Compatibility.MinimumSourceVersion = 2
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}
