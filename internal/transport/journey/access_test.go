package journey

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// The page gate judges declared pairs against the injected registry, not the
// UI catalog it defaults to: a registry that declares nothing denies a pair
// the product registry declares, and a registry that declares everything
// honors rows the product registry never declared.
func TestTodo_JOURNEY_FeatureRegistryIsInjectable(t *testing.T) {
	ctx := context.Background()
	principal := rt15Principal(t)
	inv := &transport.Invocation{}

	denyAll := &server{deps: Dependencies{
		RoleAccess: &rt15Store{snapshot: rt15SeededSnapshot()},
		Features:   func(pageID, featureID string) bool { return false },
	}}
	if err := denyAll.requireFeatureAction(ctx, principal, inv, "journeys", "content", roleaccess.ActionView); err == nil {
		t.Fatal("the gate allowed a pair the injected registry declares nothing for")
	}

	allowAll := &server{deps: Dependencies{
		RoleAccess: &rt15Store{snapshot: rt15ForgedSnapshot()},
		Features:   func(pageID, featureID string) bool { return true },
	}}
	if err := allowAll.requireFeatureAction(ctx, principal, inv, "shadowconsole", "shadow_feature", roleaccess.ActionView); err != nil {
		t.Fatalf("the gate refused a pair the injected registry declares: %v", err)
	}
}
