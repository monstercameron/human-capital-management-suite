package mappingprofile

import (
	"encoding/json"
	"testing"
)

func TestTodo_REV_038_01(t *testing.T) {
	p := Profile{MappingID: "pilot", Version: "v1", Rules: []Rule{
		{Source: "name", Target: "person.name", Op: "TRIM"},
		{Source: "dept", Target: "department.id", Op: "ENUM", Lookup: map[string]string{"eng": "ENG-1"}},
		{Source: "optional", Target: "optional", Op: "IDENTITY", Null: NullOmit},
	}}
	c, err := Compile(p)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	result, err := c.MapShared(map[string]string{"name": " Ada ", "dept": "eng"})
	if err != nil {
		t.Fatalf("MapShared: %v", err)
	}
	got, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Frozen legacy output generated from Compiled.Execute at a11035e8 before
	// the interpreter was removed. Keep the complete bytes to guard migration
	// compatibility, including result digest and diagnostics.
	const want = `{"Fields":[{"Target":"department.id","Value":"ENG-1","Deleted":false},{"Target":"person.name","Value":"Ada","Deleted":false}],"Diagnostics":[{"Target":"optional","Code":"source.missing","Detail":"optional"}],"Digest":"sha256:7ad41fff0d82a9d57d0d692e795c8d022534a46412e702ca2efb7a4adf9322f4"}`
	if string(got) != want {
		t.Fatalf("shared profile bytes = %s, want legacy bytes %s", got, want)
	}
}

func TestTodo_REV_038_01_EmptyConstantsGolden(t *testing.T) {
	p := Profile{MappingID: "literal", Version: "v1", Rules: []Rule{
		{Target: "constant.empty", Op: "CONSTANT", Argument: ""},
		{Source: "missing", Target: "default.empty", Op: "IDENTITY"},
		{Source: "ignored", Target: "constant.value", Op: "CONSTANT", Argument: "fixed"},
		{Source: "missing.value", Target: "default.value", Op: "IDENTITY", Default: "fallback"},
	}, Defaults: []Default{{Target: "default.empty", Value: ""}}}
	c, err := Compile(p)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	result, err := c.MapShared(map[string]string{"ignored": "input"})
	if err != nil {
		t.Fatalf("MapShared: %v", err)
	}
	got, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	const want = `{"Fields":[{"Target":"constant.empty","Value":"","Deleted":false},{"Target":"constant.value","Value":"fixed","Deleted":false},{"Target":"default.empty","Value":"","Deleted":false},{"Target":"default.value","Value":"fallback","Deleted":false}],"Diagnostics":[],"Digest":"sha256:941d2e585be43830d2b685ad1d593fcc0deeadc9e641a16dae3a65f892618748"}`
	if string(got) != want {
		t.Fatalf("literal profile bytes = %s, want %s", got, want)
	}
}

func TestTodo_INTG_006(t *testing.T) {
	p := Profile{MappingID: "pilot", Version: "v1", Rules: []Rule{
		{Source: "name", Target: "person.name", Op: "TRIM"},
		{Source: "dept", Target: "department.id", Op: "ENUM", Lookup: map[string]string{"eng": "ENG-1"}},
		{Source: "optional", Target: "optional", Op: "IDENTITY", Null: NullOmit},
	}}
	c, err := Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.MapShared(map[string]string{"name": " Ada ", "dept": "eng"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Fields) != 2 || a.Fields[0].Target != "department.id" || a.Fields[1].Value != "Ada" {
		t.Fatalf("fields: %#v", a.Fields)
	}
	b, err := c.MapShared(map[string]string{"dept": "eng", "name": " Ada "})
	if err != nil || a.Digest != b.Digest {
		t.Fatalf("replay digest changed: %v %v", err, b.Digest)
	}
}

func TestTodo_INTG_006_Fault(t *testing.T) {
	if _, err := Compile(Profile{MappingID: "x", Version: "v1", Rules: []Rule{{Source: "x", Target: "y", Op: "EVAL"}}}); err == nil {
		t.Fatal("arbitrary operation accepted")
	}
	if _, err := Compile(Profile{MappingID: "x", Version: "v1", Rules: []Rule{{Source: "x", Target: "y", Op: "DATE", Argument: "2006-01-02 15:04"}}}); err == nil {
		t.Fatal("unbounded date layout accepted")
	}
}

func TestTodo_INTG_006_Golden(t *testing.T) {
	p := Profile{MappingID: "x", Version: "v1", Rules: []Rule{{Source: "x", Target: "y", Op: "TRIM"}}}
	a, _ := Compile(p)
	b, _ := Compile(p)
	if a.Digest() == "" || a.Digest() != b.Digest() {
		t.Fatalf("unstable profile digest")
	}
}
