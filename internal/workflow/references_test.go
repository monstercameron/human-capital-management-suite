package workflow_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func fixtureRuleTable(id, version string) rules.Table {
	return rules.Table{
		ID:        id,
		Version:   version,
		Inputs:    []rules.Column{{Name: "country", Kind: rules.KindString}},
		Outputs:   []rules.Column{{Name: "tier", Kind: rules.KindString}},
		HitPolicy: rules.HitPolicyFirst,
		Rows: []rules.Row{{
			ID:         "us",
			Conditions: []rules.Condition{rules.Equal(rules.StringValue("US"))},
			Outputs:    []rules.Value{rules.StringValue("FINANCE_REQUIRED")},
		}},
	}
}

func TestReferenceKindAndStatusValidity(t *testing.T) {
	for _, k := range []workflow.ReferenceKind{workflow.RefSchema, workflow.RefRule, workflow.RefResolver,
		workflow.RefTimeoutPolicy, workflow.RefCompensation} {
		if !k.Valid() {
			t.Errorf("kind %s must be valid", k)
		}
	}
	if workflow.ReferenceKind("CAPABILITY").Valid() {
		t.Error("CAPABILITY is not a reference kind this resolver answers")
	}
	for _, s := range []workflow.ReferenceStatus{workflow.ReferencePublished, workflow.ReferenceDeprecated, workflow.ReferenceRetired} {
		if !s.Valid() {
			t.Errorf("status %s must be valid", s)
		}
	}
	if workflow.ReferenceStatus("DRAFT").Valid() {
		t.Error("DRAFT is not a published reference status")
	}
}

func TestReferenceStringsAndSchemaReference(t *testing.T) {
	if got := (workflow.VersionedRef{ID: "timeout.approval", Version: "3"}).String(); got != "timeout.approval@3" {
		t.Errorf("VersionedRef.String = %q", got)
	}
	ref := workflow.SchemaReference(workflow.SchemaRef{SchemaID: "schema.a", Version: 12, ProtobufFullName: "x.A"})
	if ref != (workflow.Reference{Kind: workflow.RefSchema, ID: "schema.a", Version: "12"}) {
		t.Errorf("SchemaReference = %+v", ref)
	}
	if got := ref.String(); got != "SCHEMA:schema.a@12" {
		t.Errorf("Reference.String = %q", got)
	}
}

func TestNewReferenceRegistryRefusesUnpublishableEntries(t *testing.T) {
	good := workflow.ResolvedReference{Kind: workflow.RefResolver, ID: "resolver.manager", Version: "1",
		Digest: "sha256:aa", Status: workflow.ReferencePublished}
	cases := map[string]func(r *workflow.ResolvedReference){
		"unknown kind":          func(r *workflow.ResolvedReference) { r.Kind = "CAPABILITY" },
		"blank id":              func(r *workflow.ResolvedReference) { r.ID = "" },
		"blank version":         func(r *workflow.ResolvedReference) { r.Version = "" },
		"no digest":             func(r *workflow.ResolvedReference) { r.Digest = "" },
		"unknown status":        func(r *workflow.ResolvedReference) { r.Status = "DRAFT" },
		"schema w/o descriptor": func(r *workflow.ResolvedReference) { r.Kind = workflow.RefSchema },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := good
			mutate(&bad)
			if _, err := workflow.NewReferenceRegistry(bad); !errors.Is(err, workflow.ErrInvalidReference) {
				t.Fatalf("err = %v, want ErrInvalidReference", err)
			}
		})
	}
	if _, err := workflow.NewReferenceRegistry(good, good); !errors.Is(err, workflow.ErrInvalidReference) {
		t.Fatalf("duplicate publication err = %v, want ErrInvalidReference", err)
	}

	reg, err := workflow.NewReferenceRegistry(good)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	got, ok := reg.ResolveReference(workflow.Reference{Kind: workflow.RefResolver, ID: "resolver.manager", Version: "1"})
	if !ok || got != good {
		t.Fatalf("resolve = %+v, %v", got, ok)
	}
	if _, ok := reg.ResolveReference(workflow.Reference{Kind: workflow.RefCompensation, ID: "resolver.manager", Version: "1"}); ok {
		t.Fatal("a reference resolves only under its own kind")
	}
	var nilReg *workflow.ReferenceRegistry
	if _, ok := nilReg.ResolveReference(workflow.Reference{}); ok {
		t.Fatal("a nil registry resolves nothing")
	}
}

func TestRuleTableReferencePinsTheTableDigest(t *testing.T) {
	tbl := fixtureRuleTable("rules.raise_threshold", "2026.1")
	ref, err := workflow.RuleTableReference(tbl, workflow.ReferencePublished)
	if err != nil {
		t.Fatalf("RuleTableReference: %v", err)
	}
	want, _ := tbl.Digest()
	if ref.Kind != workflow.RefRule || ref.ID != tbl.ID || ref.Version != tbl.Version || ref.Digest != want {
		t.Fatalf("ref = %+v, want the table identity pinned by %s", ref, want)
	}
	tbl.Rows = nil
	if _, err := workflow.RuleTableReference(tbl, workflow.ReferencePublished); !errors.Is(err, workflow.ErrInvalidReference) {
		t.Fatalf("invalid table err = %v, want ErrInvalidReference", err)
	}
}
