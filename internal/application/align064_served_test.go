package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

// TestTodo_ALIGN_064_Served proves the real serve composition records and
// validates the default product-slice gate rather than leaving it library-only.
func TestTodo_ALIGN_064_Served(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())
	component, ok := composed.Graph().Component(ComponentProductSliceReleaseGate)
	if !ok {
		t.Fatal("served graph has no product-slice release gate")
	}
	if component.Kind != KindGovernance || component.Impl != "string" {
		t.Fatalf("release gate component = %+v, want governance/string", component)
	}
	if conformance.ServingContractID == "" {
		t.Fatal("serving gate contract has no stable identity")
	}
}
