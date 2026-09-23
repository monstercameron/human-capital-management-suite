package journey

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

// TestTodo_RBAC_RT_016 is the PRIMARY matrix entry: the declarative table
// maps every served call to its page, feature and action, including reads,
// and every entry names a registry-declared pair. A served method with no
// entry fails here, which is what proves none is missed.
func TestTodo_RBAC_RT_016(t *testing.T) {
	desc := journeyv1.JourneyService_ServiceDesc

	seen := map[string]bool{}
	for _, method := range desc.Methods {
		seen[method.MethodName] = true
	}
	for _, stream := range desc.Streams {
		seen[stream.StreamName] = true
	}
	for name := range seen {
		gates, ok := gatesForServedCall(name)
		if !ok || len(gates) == 0 {
			t.Fatalf("served method %s has no call-gate entry", name)
		}
		for _, gate := range gates {
			if !registryFeatureDeclared(gate.pageID, gate.featureID) {
				t.Fatalf("method %s gates on undeclared pair %s/%s", name, gate.pageID, gate.featureID)
			}
			switch gate.action {
			case roleaccess.ActionView, roleaccess.ActionCreate, roleaccess.ActionUpdate, roleaccess.ActionDelete:
			default:
				t.Fatalf("method %s gates on unknown action %q", name, gate.action)
			}
		}
	}
	for name := range servedCallGates {
		if !seen[name] {
			t.Fatalf("call-gate entry %s names no served method", name)
		}
	}

	actions := map[string]bool{}
	for _, gates := range servedCallGates {
		for _, gate := range gates {
			actions[gate.action] = true
		}
	}
	for _, action := range []string{roleaccess.ActionView, roleaccess.ActionCreate, roleaccess.ActionUpdate} {
		if !actions[action] {
			t.Fatalf("no served call maps to the %s action", action)
		}
	}
	if actions[roleaccess.ActionDelete] {
		t.Fatal("a served call maps to the delete action with no deleting RPC behind it")
	}

	if _, ok := gatesForServedCall("NoSuchMethod"); ok {
		t.Fatal("unmapped method resolved to grants instead of denying")
	}
}
