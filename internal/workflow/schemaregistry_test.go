// TestTodo_WF_DATA_003 proves a published custom-object definition and a
// registered external document type each yield a pinned schema-registry
// entry carrying a generated protobuf descriptor, accepted by the
// existing MESSAGE and typed-path checks unchanged.
package workflow_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/customobject"
	"github.com/monstercameron/human-capital-management-suite/internal/documents/template"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func wfd3CustomType() customobject.CustomObjectType {
	return customobject.CustomObjectType{
		Name: "PurchaseOrder", Namespace: "acme.procurement", Owner: "procurement-ops", Version: 2,
		Fields: []customobject.Field{
			{Name: "total", Type: "Money", Required: true},
			{Name: "note", Type: "string"},
			{Name: "approved", Type: "bool"},
		},
	}
}

func wfd3Template(t *testing.T) template.Definition {
	t.Helper()
	return template.Definition{
		ID: "offer", Version: "3", Type: "OFFER", Purpose: "employment",
		SourceLocale: "en-US", Locale: "en-US", Jurisdiction: "US-NC",
		Classification: "CONFIDENTIAL", AccessibilityVersion: "wcag-2.2",
		Format: "text/html", Source: "Hello {{.name}} ({{.start}})",
		Placeholders: []template.Placeholder{
			{Name: "name", Kind: template.String, Required: true},
			{Name: "start", Kind: template.Date, Required: true},
		},
	}
}

func wfd3PublishedTemplate(t *testing.T, def template.Definition) template.Template {
	t.Helper()
	draft, err := template.New(def)
	if err != nil {
		t.Fatalf("new template: %v", err)
	}
	published, err := draft.Publish(template.Publication{
		TemplateDigest: draft.Digest(),
		Fixtures: []template.Fixture{{Name: "golden", Bindings: map[string]template.Binding{
			"name":  {Kind: template.String, Value: "Ada"},
			"start": {Kind: template.Date, Value: "2026-09-03"},
		}}},
		Approval:      template.Approval{ApprovalID: "approval-1", ApprovedBy: "hr"},
		Applicability: template.Applicability{Locales: []string{"en-US"}, Jurisdictions: []string{"US-NC"}, Classifications: []string{"CONFIDENTIAL"}},
	})
	if err != nil {
		t.Fatalf("publish template: %v", err)
	}
	return published
}

func wfd3Entry(t *testing.T, id, version, digest, descriptor string) workflow.ResolvedReference {
	t.Helper()
	entry, err := workflow.NewSchemaEntry(id, version, digest, descriptor, workflow.ReferencePublished)
	if err != nil {
		t.Fatalf("schema entry: %v", err)
	}
	return entry
}

func TestTodo_WF_DATA_003(t *testing.T) {
	t.Run("custom_definition_yields_pinned_entry", func(t *testing.T) {
		compiled, err := customobject.Compile(wfd3CustomType())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if compiled.Descriptor != "acme.procurement.PurchaseOrder" {
			t.Fatalf("descriptor = %q, want acme.procurement.PurchaseOrder", compiled.Descriptor)
		}
		entry := wfd3Entry(t, compiled.Descriptor, "2", compiled.Digest, compiled.Descriptor)
		registry, err := workflow.NewReferenceRegistry(entry)
		if err != nil {
			t.Fatalf("registry: %v", err)
		}
		got, ok := registry.ResolveReference(workflow.SchemaReference(workflow.SchemaRef{
			SchemaID: compiled.Descriptor, Version: 2, ProtobufFullName: compiled.Descriptor,
		}))
		if !ok || got != entry {
			t.Fatalf("resolve = %+v ok=%v, want the published entry", got, ok)
		}
	})

	t.Run("document_type_yields_pinned_entry", func(t *testing.T) {
		published := wfd3PublishedTemplate(t, wfd3Template(t))
		descriptor, err := published.Definition.Descriptor()
		if err != nil {
			t.Fatalf("descriptor: %v", err)
		}
		if descriptor != "doc.offer" {
			t.Fatalf("descriptor = %q, want doc.offer", descriptor)
		}
		entry := wfd3Entry(t, descriptor, published.Definition.Version, published.Digest(), descriptor)
		registry, err := workflow.NewReferenceRegistry(entry)
		if err != nil {
			t.Fatalf("registry: %v", err)
		}
		got, ok := registry.ResolveReference(workflow.Reference{Kind: workflow.RefSchema, ID: descriptor, Version: "3"})
		if !ok || got.Digest != published.Digest() || got.Descriptor != descriptor {
			t.Fatalf("resolve = %+v ok=%v, want the pinned document entry", got, ok)
		}
	})

	t.Run("compiler_accepts_generated_entry_unchanged", func(t *testing.T) {
		compiled, err := customobject.Compile(wfd3CustomType())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		def := syntheticDefinition([]string{synEndOK})
		def.Nodes[0].InputSchema = workflow.SchemaRef{SchemaID: compiled.Descriptor, Version: 2, ProtobufFullName: compiled.Descriptor}
		message := workflow.ValueType{Kind: workflow.KindMessage, MessageRef: compiled.Descriptor}
		def.Inputs = append(def.Inputs, workflow.Field{Path: "order", Type: message})
		def.Nodes[0].Inputs = append(def.Nodes[0].Inputs, workflow.Field{Path: "order", Type: message})
		def.Nodes[0].InputMappings = append(def.Nodes[0].InputMappings, workflow.Mapping{Target: "order", Source: inputSource("order")})
		declared := []workflow.SchemaRef{def.InputSchema, def.OutputSchema, def.VariablesSchema, def.Nodes[0].OutputSchema}
		entries := make([]workflow.ResolvedReference, 0, len(declared)+1)
		for _, ref := range declared {
			entries = append(entries, wfd3Entry(t,
				ref.SchemaID, fmt.Sprint(ref.Version), "sha256:"+"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd", ref.ProtobufFullName))
		}
		mine := wfd3Entry(t, compiled.Descriptor, "2", compiled.Digest, compiled.Descriptor)
		full, err := workflow.NewReferenceRegistry(append(entries, mine)...)
		if err != nil {
			t.Fatalf("registry: %v", err)
		}
		plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, References: full})
		if err != nil {
			t.Fatalf("compile with generated entry: %v", err)
		}
		if plan.Digest() == "" {
			t.Fatal("compiled plan carries no digest")
		}
		without, err := workflow.NewReferenceRegistry(entries...)
		if err != nil {
			t.Fatalf("registry: %v", err)
		}
		mustReject(t, def, workflow.Options{Phase: workflow.PhaseP1A, References: without}, workflow.CodeUnresolvedRef)
	})

	t.Run("entry_and_descriptor_refusals", func(t *testing.T) {
		for name, mutate := range map[string]func(*[5]string){
			"blank id":         func(a *[5]string) { a[0] = "" },
			"blank version":    func(a *[5]string) { a[1] = "" },
			"blank digest":     func(a *[5]string) { a[2] = "" },
			"blank descriptor": func(a *[5]string) { a[3] = "" },
		} {
			args := [5]string{"acme.procurement.PurchaseOrder", "2", "sha256:aa", "acme.procurement.PurchaseOrder", "x"}
			mutate(&args)
			if _, err := workflow.NewSchemaEntry(args[0], args[1], args[2], args[3], workflow.ReferencePublished); !errors.Is(err, workflow.ErrInvalidReference) {
				t.Fatalf("%s err = %v, want ErrInvalidReference", name, err)
			}
		}
		if _, err := workflow.NewSchemaEntry("id", "1", "sha256:aa", "desc", workflow.ReferenceStatus("DRAFT")); !errors.Is(err, workflow.ErrInvalidReference) {
			t.Fatalf("bad status err = %v, want ErrInvalidReference", err)
		}
		for _, bad := range []string{"", "OFFER LETTER", "offer.letter", "9LIVES", "offer-letter"} {
			def := template.Definition{Type: bad}
			if _, err := def.Descriptor(); !errors.Is(err, template.ErrInvalid) {
				t.Fatalf("type %q err = %v, want ErrInvalid", bad, err)
			}
		}
	})
}

func TestTodo_WF_DATA_003_Golden(t *testing.T) {
	compiled, err := customobject.Compile(wfd3CustomType())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	entry, err := workflow.NewSchemaEntry(compiled.Descriptor, "2", compiled.Digest, compiled.Descriptor, workflow.ReferencePublished)
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	got, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"kind":"SCHEMA","id":"acme.procurement.PurchaseOrder","version":"2","digest":"sha256:aaf77581a514c652e8c2c5f9ef13227e5bdf78338371bf7e54e3b502b119ec15","status":"PUBLISHED","descriptor":"acme.procurement.PurchaseOrder"}`
	if string(got) != want {
		t.Fatalf("entry golden changed:\n got %s\nwant %s", got, want)
	}
}

func TestTodo_WF_DATA_003_Property(t *testing.T) {
	t.Run("same_definition_yields_same_descriptor_and_digest", func(t *testing.T) {
		base, err := customobject.Compile(wfd3CustomType())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		fields := wfd3CustomType().Fields
		perms := [][]customobject.Field{
			{fields[0], fields[1], fields[2]}, {fields[0], fields[2], fields[1]},
			{fields[1], fields[0], fields[2]}, {fields[1], fields[2], fields[0]},
			{fields[2], fields[0], fields[1]}, {fields[2], fields[1], fields[0]},
		}
		for i, order := range perms {
			src := wfd3CustomType()
			src.Fields = order
			got, err := customobject.Compile(src)
			if err != nil {
				t.Fatalf("perm %d: %v", i, err)
			}
			if got.Descriptor != base.Descriptor || got.Digest != base.Digest {
				t.Fatalf("perm %d = (%s, %s), want (%s, %s)", i, got.Descriptor, got.Digest, base.Descriptor, base.Digest)
			}
		}
		for i := 0; i < 25; i++ {
			got, err := customobject.Compile(wfd3CustomType())
			if err != nil {
				t.Fatalf("repeat %d: %v", i, err)
			}
			if got.Descriptor != base.Descriptor || got.Digest != base.Digest {
				t.Fatalf("repeat %d differs", i)
			}
		}
	})

	t.Run("shape_change_moves_digest_not_descriptor", func(t *testing.T) {
		base, err := customobject.Compile(wfd3CustomType())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		changed := wfd3CustomType()
		changed.Fields[1].Type = "int64"
		got, err := customobject.Compile(changed)
		if err != nil {
			t.Fatalf("compile changed: %v", err)
		}
		if got.Descriptor != base.Descriptor {
			t.Fatalf("descriptor moved: %s vs %s", got.Descriptor, base.Descriptor)
		}
		if got.Digest == base.Digest {
			t.Fatal("digest did not move with the shape")
		}
	})

	t.Run("document_descriptor_ignores_placeholder_order", func(t *testing.T) {
		def := wfd3Template(t)
		first, err := def.Descriptor()
		if err != nil {
			t.Fatalf("descriptor: %v", err)
		}
		def.Placeholders[0], def.Placeholders[1] = def.Placeholders[1], def.Placeholders[0]
		second, err := def.Descriptor()
		if err != nil {
			t.Fatalf("descriptor: %v", err)
		}
		if first != second || first != "doc.offer" {
			t.Fatalf("descriptors %q vs %q, want stable doc.offer", first, second)
		}
	})

	t.Run("descriptors_are_tenant_relative", func(t *testing.T) {
		compiled, err := customobject.Compile(wfd3CustomType())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		again, err := customobject.Compile(wfd3CustomType())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if compiled.Descriptor != again.Descriptor {
			t.Fatal("the same type must name itself identically in every tenant")
		}
		entry, err := workflow.NewSchemaEntry(compiled.Descriptor, "2", compiled.Digest, compiled.Descriptor, workflow.ReferencePublished)
		if err != nil {
			t.Fatalf("entry: %v", err)
		}
		if entry.ID != compiled.Descriptor || entry.Descriptor != compiled.Descriptor {
			t.Fatalf("entry = %+v, want descriptor-carrying identity", entry)
		}
	})
}
