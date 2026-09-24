package mapping

import (
	"errors"
	"sync"
	"testing"
)

func validProfile() MappingProfile {
	return MappingProfile{
		MappingID: "promotion-worker", Version: 1, SourceSystemRef: "workday.worker@v1", TargetEntity: "worker",
		TargetFields: []TargetField{
			{Name: "worker.external_id", Classification: ClassificationInternal},
			{Name: "person.name.given", Classification: ClassificationPII},
			{Name: "person.name.family", Classification: ClassificationPII},
		},
		FieldMappings: []FieldMapping{
			{SourceField: "Worker_ID", TargetField: "worker.external_id", TransformationIRDigest: IdentityTransformation, Required: true, Classification: ClassificationInternal},
			{SourceField: "Legal_First_Name", TargetField: "person.name.given", TransformationIRDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Required: true, Classification: ClassificationPII, SourceClassification: ClassificationPII},
		},
	}
}

func TestTodo_INTG_006(t *testing.T) {
	compiled, err := Compile(validProfile())
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Digest() == "" || len(compiled.Mappings()) != 2 || compiled.Explain() == "" {
		t.Fatalf("incomplete compiled mapping profile: %s", compiled.Explain())
	}
	profile := compiled.Profile()
	profile.TargetFields.([]TargetField)[0].Name = "mutated"
	if compiled.Profile().TargetFields.([]TargetField)[0].Name == "mutated" {
		t.Fatal("compiled profile aliases caller/accessor memory")
	}
}

func TestTodo_INTG_006_Golden(t *testing.T) {
	compiled, err := Compile(validProfile())
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:431fae998e4f6bf24383abe0ac73842b9e06a7936e94a9398aa2239b24d6466b"
	if compiled.Digest() != want {
		t.Fatalf("compiled profile digest = %s, want %s", compiled.Digest(), want)
	}
}

func TestTodo_INTG_006_Fault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*MappingProfile)
		want   error
	}{
		{"unknown target", func(p *MappingProfile) { p.FieldMappings[0].TargetField = "worker.unknown" }, ErrUnknownTargetField},
		{"duplicate mapping", func(p *MappingProfile) { p.FieldMappings = append(p.FieldMappings, p.FieldMappings[0]) }, ErrDuplicateMapping},
		{"invalid transform", func(p *MappingProfile) { p.FieldMappings[0].TransformationIRDigest = "sha256:not-an-ir" }, ErrInvalidTransformation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validProfile()
			tc.mutate(&p)
			if _, err := Compile(p); !errors.Is(err, tc.want) {
				t.Fatalf("Compile error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_INTG_006_Mutation(t *testing.T) {
	p := validProfile()
	p.FieldMappings[0].Classification = ClassificationPublic
	if _, err := Compile(p); !errors.Is(err, ErrClassificationDowngrade) {
		t.Fatalf("classification downgrade error = %v", err)
	}
	p = validProfile()
	p.FieldMappings[0].Required = true
	p.FieldMappings[0].Optional = true
	if _, err := Compile(p); err == nil {
		t.Fatal("required and optional mapping was accepted")
	}
}

func TestTodo_INTG_006_Integration(t *testing.T) {
	p := validProfile()
	first, err := Compile(p, []string{"worker.external_id", "person.name.given", "person.name.family"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(p)
	if err != nil || first.Digest() != second.Digest() {
		t.Fatalf("equivalent target schema changed digest: %v %s %s", err, first.Digest(), second.Digest())
	}

	const n = 24
	digests := make([]string, n)
	var wg sync.WaitGroup
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			compiled, err := Compile(validProfile())
			if err != nil {
				t.Errorf("compile: %v", err)
				return
			}
			digests[i] = compiled.Digest()
		}(i)
	}
	wg.Wait()
	for _, got := range digests[1:] {
		if got != digests[0] {
			t.Fatalf("digest changed across goroutines: %s != %s", got, digests[0])
		}
	}
}

// FuzzTodo_INTG_006 checks that arbitrary profile identifiers and transform
// digests either fail compilation cleanly or produce repeatable versions.
func FuzzTodo_INTG_006(f *testing.F) {
	f.Add("worker.external_id", IdentityTransformation)
	f.Add("worker.unknown", "sha256:not-an-ir")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, target, transform string) {
		profile := validProfile()
		profile.FieldMappings[0].TargetField = target
		profile.FieldMappings[0].TransformationIRDigest = transform
		first, err := Compile(profile)
		if err != nil {
			return
		}
		second, err := Compile(profile)
		if err != nil {
			t.Fatalf("same profile failed second compilation: %v", err)
		}
		if first.Digest() == "" || first.Digest() != second.Digest() {
			t.Fatalf("compilation digest is empty or unstable: %q != %q", first.Digest(), second.Digest())
		}
	})
}
