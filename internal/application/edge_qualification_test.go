package application

import "testing"

func TestTodo_EDGE_010_Integration(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())
	component, ok := composed.Graph().Component(ComponentEdgeQualification)
	if !ok {
		t.Fatal("served composition has no EDGE-010 qualification component")
	}
	if component.Kind != KindGovernance || component.Impl != "edge.Evidence" {
		t.Fatalf("served edge qualification = %+v, want governed edge.Evidence", component)
	}
	httpEdge, ok := composed.Graph().Component(ComponentHTTPEdge)
	if !ok {
		t.Fatal("served composition has no HTTP edge depending on the qualified edge")
	}
	if !containsComponentDependency(httpEdge.DependsOn, ComponentEdgeQualification) {
		t.Fatalf("HTTP edge dependencies = %v, want %q", httpEdge.DependsOn, ComponentEdgeQualification)
	}
}

func containsComponentDependency(dependencies []string, want string) bool {
	for _, dependency := range dependencies {
		if dependency == want {
			return true
		}
	}
	return false
}
