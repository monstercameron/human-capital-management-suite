package capability

import "testing"

func TestBootstrap_NewBootstrapRegistry(t *testing.T) {
	r, err := NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	if len(r.List()) != 11 {
		t.Fatalf("expected 11 definitions got %d", len(r.List()))
	}
}

func TestTodo_REV_014_01(t *testing.T) {
	r, err := NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	rec, ok := r.Lookup(Key{ID: "hcmnext.dataops.explain_field_history", Version: 1})
	if !ok {
		t.Fatal("effective-date debugger capability is not published")
	}
	if rec.Definition.EffectClass != EffectReadOnly || len(rec.Definition.WriteData.DataDomains) != 0 || len(rec.Definition.WriteData.FieldPaths) != 0 {
		t.Fatalf("debugger capability is not read-only: %+v", rec.Definition)
	}
}

func TestBootstrap_DefinitionsDistinct(t *testing.T) {
	defs := bootstrapDefinitions()
	seen := map[string]bool{}
	for _, d := range defs {
		if seen[d.ID] {
			t.Fatalf("duplicate id %s", d.ID)
		}
		seen[d.ID] = true
		if d.ID == "" || d.Version == 0 {
			t.Fatalf("invalid def %+v", d)
		}
	}
}

func TestBootstrap_Schema(t *testing.T) {
	s := bootstrapSchema("hcmnext.test.cap", "request")
	if !s.Valid() {
		t.Fatalf("schema not valid %+v", s)
	}
}

func TestBootstrap_EchoHandler(t *testing.T) {
	h := bootstrapEcho("test.cap")
	if h == nil {
		t.Fatal("nil handler")
	}
}
