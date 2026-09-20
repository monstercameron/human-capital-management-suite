package designerpalette_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
)

func TestTodo_WF_UI_005(t *testing.T) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatal(err)
	}
	allowed := capability.Key{ID: "hcmnext.people.promote_worker", Version: 1}
	catalog := designerpalette.Catalog{
		Capabilities: registry,
		Policy: draftcompile.CapabilityPolicyFunc(func(_ context.Context, tenant values.TenantId, key capability.Key) bool {
			return tenant == "tenant-a" && key == allowed
		}),
		Extensions: []designerpalette.Entry{
			{ID: "fragment.promotion-review", Version: 1, Name: "Promotion review", Kind: designerpalette.KindFragment, Domain: "People", RequiredCapabilities: []capability.Key{allowed}, EffectClass: capability.EffectPure, Reversal: "NO_EFFECT", Status: "ACTIVE"},
			{ID: "template.promotion", Version: 1, Name: "Promotion", Kind: designerpalette.KindTemplate, Domain: "People", RequiredCapabilities: []capability.Key{allowed}, EffectClass: capability.EffectInternalMutation, Reversal: "COMPENSATION_REQUIRED", Status: "ACTIVE"},
		},
	}
	entries := catalog.List(context.Background(), "tenant-a")
	var ids []string
	var kinds []designerpalette.Kind
	for _, entry := range entries {
		ids = append(ids, entry.ID)
		kinds = append(kinds, entry.Kind)
	}
	for _, want := range []string{"kernel.approval", allowed.ID, "fragment.promotion-review", "template.promotion"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("palette omits %q: %v", want, ids)
		}
	}
	for _, want := range []designerpalette.Kind{designerpalette.KindBlock, designerpalette.KindFragment, designerpalette.KindTemplate} {
		if !slices.Contains(kinds, want) {
			t.Fatalf("palette omits kind %q: %v", want, kinds)
		}
	}
	if fmt.Sprint(entries) != fmt.Sprint(catalog.List(context.Background(), "tenant-a")) {
		t.Fatal("palette order is not deterministic")
	}
}

func TestTodo_WF_UI_005_Security(t *testing.T) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, catalog := range []designerpalette.Catalog{
		{Capabilities: registry},
		{Capabilities: registry, Policy: draftcompile.CapabilityPolicyFunc(func(context.Context, values.TenantId, capability.Key) bool { return false }), Extensions: []designerpalette.Entry{{ID: "fragment.hidden", Version: 1, Name: "Hidden", Kind: designerpalette.KindFragment, RequiredCapabilities: []capability.Key{{ID: "hcmnext.people.promote_worker", Version: 1}}}}},
	} {
		for _, entry := range catalog.List(context.Background(), "tenant-a") {
			if len(entry.RequiredCapabilities) != 0 || entry.StepType == "CAPABILITY" {
				t.Fatalf("tenant received unallowlisted registry entry: %+v", entry)
			}
		}
	}
}

func TestTodo_WF_UI_005_RegistryExpansionDoesNotAliasCallers(t *testing.T) {
	template := workflow.Definition{
		WorkflowID: "template.one",
		Nodes:      []workflow.Node{{ID: "template-node", Metadata: map[string]string{"owner": "registry"}}},
	}
	catalog := designerpalette.Catalog{Extensions: []designerpalette.Entry{{
		ID: "fragment.review", Version: 1, Name: "Review", Kind: designerpalette.KindFragment,
		Expansion: designerpalette.Expansion{
			Template: &template,
			Nodes:    []workflow.Node{{ID: "review", Metadata: map[string]string{"owner": "registry"}}},
		},
	}}}

	find := func(entries []designerpalette.Entry) designerpalette.Entry {
		t.Helper()
		for _, entry := range entries {
			if entry.ID == "fragment.review" {
				return entry
			}
		}
		t.Fatal("catalog returned no fragment.review entry")
		return designerpalette.Entry{}
	}
	entry := find(catalog.List(context.Background(), "tenant-a"))
	entry.Expansion.Nodes[0].Metadata["owner"] = "caller"
	entry.Expansion.Template.Nodes[0].Metadata["owner"] = "caller"

	entry = find(catalog.List(context.Background(), "tenant-a"))
	if got := entry.Expansion.Nodes[0].Metadata["owner"]; got != "registry" {
		t.Fatalf("fragment expansion aliases caller mutation: owner = %q", got)
	}
	if got := entry.Expansion.Template.Nodes[0].Metadata["owner"]; got != "registry" {
		t.Fatalf("template expansion aliases caller mutation: owner = %q", got)
	}
}
