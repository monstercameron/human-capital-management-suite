package agentcharter

import (
	"reflect"
	"testing"
)

// A deny-only layer supplies no allow ceiling: it narrows whatever ceiling the
// other layers establish, wherever it sits in the chain. This mirrors
// TestResolve_DenyOnlyLayerPreservesEarlierAllowCeiling with the deny-only
// layer first, where composition must behave the same.
func TestResolve_DenyOnlyFirstLayerPreservesLaterAllowCeiling(t *testing.T) {
	input := agent010Input()
	input.Layers[0].Settings.Tools = Constraint[CapabilityID]{
		Specified: true,
		Deny:      []CapabilityID{"workflow.write"},
	}
	result, err := Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []CapabilityID{"people.read", "policy.read"}
	if !reflect.DeepEqual(result.Tools.Allowed, want) {
		t.Fatalf("deny-only first layer erased the later ceiling: got %v, want %v", result.Tools.Allowed, want)
	}
	if !reflect.DeepEqual(result.Tools.Denied, []CapabilityID{"workflow.write"}) {
		t.Fatalf("deny-only first layer lost denial: %v", result.Tools.Denied)
	}
}
