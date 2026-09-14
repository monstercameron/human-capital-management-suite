package convergence

import "testing"

func TestContractVocabularyIsClosedAndUnique(t *testing.T) {
	seen := map[Contract]bool{}
	for _, c := range Contracts() {
		if !c.Valid() || seen[c] {
			t.Errorf("contract %q invalid or duplicated", c)
		}
		seen[c] = true
	}
	for _, bad := range []Contract{"", "test", "TESTS", "SELECTION"} {
		if bad.Valid() {
			t.Errorf("contract %q must not be valid", bad)
		}
	}
}

func TestValidLayerAcceptsOnlyDownstreamLayers(t *testing.T) {
	for _, layer := range []string{LayerSlice, LayerModel, LayerAPI, LayerThreat, LayerTest, LayerImplementation} {
		if !validLayer(layer) {
			t.Errorf("layer %s rejected", layer)
		}
	}
	for _, layer := range []string{"", "GOVERNANCE", "slice"} {
		if validLayer(layer) {
			t.Errorf("layer %q accepted", layer)
		}
	}
}
