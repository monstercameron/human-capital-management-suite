package productui

import "testing"

func TestTodo_WEB_241(t *testing.T) {
	pages := PageDefinitions()
	if len(pages) == 0 {
		t.Fatal("page registry is empty")
	}
	for _, page := range pages {
		seen := map[FeatureID]bool{}
		for _, feature := range page.Features {
			if feature.ID == "" || seen[feature.ID] {
				t.Fatalf("page %q has an empty or duplicate feature ID %q", page.ID, feature.ID)
			}
			seen[feature.ID] = true
			if !feature.View {
				t.Fatalf("page %q feature %q is not viewable", page.ID, feature.ID)
			}
			if (feature.Create || feature.Update || feature.Delete) && !feature.View {
				t.Fatalf("page %q feature %q mutates without view", page.ID, feature.ID)
			}
		}
		if !seen[FeatureContent] || !seen[FeatureActions] {
			t.Fatalf("page %q lacks its required content/actions access boundaries", page.ID)
		}
	}
}

func TestTodo_WEB_241_Property(t *testing.T) {
	one := FeatureDefinitionsForPage(PagePeople)
	if len(one) == 0 {
		t.Fatal("people feature catalog is empty")
	}
	one[0].Label = "mutated"
	two := FeatureDefinitionsForPage(PagePeople)
	if two[0].Label == "mutated" {
		t.Fatal("caller mutated the registered feature catalog")
	}
	flattened := FlattenFeatureDefinitions()
	for i := 1; i < len(flattened); i++ {
		previous, current := flattened[i-1], flattened[i]
		if previous.Page > current.Page || previous.Page == current.Page && previous.Feature.ID > current.Feature.ID {
			t.Fatalf("flattened catalog is unstable at %d: %#v then %#v", i, previous, current)
		}
	}
}

func TestTodo_WEB_241_Security(t *testing.T) {
	pageGrant := RolePagePermission{Page: PagePeople, View: true, Create: true, Update: true}
	featureGrant := RoleFeaturePermission{Page: PagePeople, Feature: "workflow_actions", View: true, Create: true}

	view := NewView(PagePeople, "tenant", "person", "scope")
	view.EffectivePermissions = []RolePagePermission{pageGrant}
	view.EffectiveFeatures = []RoleFeaturePermission{featureGrant}
	if !view.CanFeature(PagePeople, "workflow_actions", "create") {
		t.Fatal("matching page and feature create grants were denied")
	}
	if view.CanFeature(PagePeople, "workflow_actions", "update") {
		t.Fatal("feature update was allowed beyond the feature grant")
	}

	view.EffectivePermissions[0].Create = false
	if view.CanFeature(PagePeople, "workflow_actions", "create") {
		t.Fatal("feature grant escaped the containing page boundary")
	}

	view.EffectivePermissions[0].Create = true
	view.EffectiveFeatures = []RoleFeaturePermission{}
	if view.CanFeature(PagePeople, "workflow_actions", "create") {
		t.Fatal("authoritative empty feature projection did not fail closed")
	}

	view.EffectiveFeatures = nil
	if !view.CanFeature(PagePeople, "workflow_actions", "create") {
		t.Fatal("rolling-upgrade page-only compatibility path was denied")
	}
}

func TestTodo_WEB_241_Regression(t *testing.T) {
	view := NewView(PagePeople, "tenant", "person", "scope")
	view.EffectivePermissions = []RolePagePermission{{Page: PagePeople, View: true, Create: true}}
	view.EffectiveFeatures = []RoleFeaturePermission{
		{Page: PagePeople, Feature: FeatureContent, View: true},
		{Page: PagePeople, Feature: FeatureActions, View: true, Create: false},
	}
	if !view.Can(PagePeople, "view") {
		t.Fatal("core content view grant was denied")
	}
	if view.Can(PagePeople, "create") {
		t.Fatal("generic create ignored the core actions feature boundary")
	}
}
