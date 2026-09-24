package workflow_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestDeclaredSchemaResolverPinsExactDefinitionSchemas(t *testing.T) {
	def := workflow.Definition{
		WorkflowID: "workflow.schema-test", Version: 1,
		InputSchema:  workflow.SchemaRef{SchemaID: "schema.input/v2", Version: 2, ProtobufFullName: "test.v2.Input"},
		OutputSchema: workflow.SchemaRef{SchemaID: "schema.output/v1", Version: 1, ProtobufFullName: "test.v1.Output"},
		Nodes:        []workflow.Node{{ID: "step", InputSchema: workflow.SchemaRef{SchemaID: "schema.input/v2", Version: 2, ProtobufFullName: "test.v2.Input"}}},
	}
	resolver, err := workflow.DeclaredSchemaResolver(def)
	if err != nil {
		t.Fatalf("DeclaredSchemaResolver: %v", err)
	}
	input := workflow.SchemaReference(def.InputSchema)
	got, ok := resolver.ResolveReference(input)
	if !ok || got.Kind != workflow.RefSchema || got.Descriptor != "test.v2.Input" || got.Status != workflow.ReferencePublished || got.Digest == "" {
		t.Fatalf("input schema resolution = %+v, %v", got, ok)
	}
	if again, ok := resolver.ResolveReference(input); !ok || again != got {
		t.Fatalf("schema resolution is not stable: %+v, %v; first %+v", again, ok, got)
	}
	if _, ok := resolver.ResolveReference(workflow.Reference{Kind: workflow.RefSchema, ID: "schema.missing/v1", Version: "1"}); ok {
		t.Fatal("resolver matched a schema absent from the definition")
	}
}

func TestDeclaredSchemaResolverRejectsConflictingDescriptor(t *testing.T) {
	def := workflow.Definition{
		WorkflowID: "workflow.schema-conflict", Version: 1,
		InputSchema: workflow.SchemaRef{SchemaID: "schema.shared/v1", Version: 1, ProtobufFullName: "test.Input"},
		Nodes:       []workflow.Node{{ID: "step", InputSchema: workflow.SchemaRef{SchemaID: "schema.shared/v1", Version: 1, ProtobufFullName: "test.OtherInput"}}},
	}
	if _, err := workflow.DeclaredSchemaResolver(def); err == nil {
		t.Fatal("conflicting descriptor declarations were accepted")
	}
}

func TestComposeReferenceResolversPrefersAuthoritativeResolver(t *testing.T) {
	def := workflow.Definition{InputSchema: workflow.SchemaRef{SchemaID: "schema.input/v1", Version: 1, ProtobufFullName: "test.Input"}}
	derived, err := workflow.DeclaredSchemaResolver(def)
	if err != nil {
		t.Fatalf("DeclaredSchemaResolver: %v", err)
	}
	ref := workflow.SchemaReference(def.InputSchema)
	authoritative := workflow.ResolvedReference{
		Kind: workflow.RefSchema, ID: ref.ID, Version: ref.Version, Digest: "sha256:registry", Status: workflow.ReferencePublished, Descriptor: "test.Input",
	}
	registry, err := workflow.NewReferenceRegistry(authoritative)
	if err != nil {
		t.Fatalf("NewReferenceRegistry: %v", err)
	}
	composed := workflow.ComposeReferenceResolvers(registry, derived)
	got, ok := composed.ResolveReference(ref)
	if !ok || got != authoritative {
		t.Fatalf("composed resolver = %+v, %v; want authoritative %+v", got, ok, authoritative)
	}
}
