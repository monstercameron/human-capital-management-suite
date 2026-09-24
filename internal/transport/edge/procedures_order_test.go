package edge

import (
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	toolsinvocationpath "github.com/monstercameron/human-capital-management-suite/tools/policy/invocationpath"
)

func TestProceduresReturnsDeterministicSortedInventory(t *testing.T) {
	first := Procedures()
	second := Procedures()
	want := []string{
		"/hcmnext.intents.v1.IntentService/CreateIntent",
		"/hcmnext.intents.v1.IntentService/GetIntent",
		"/hcmnext.intents.v1.IntentService/ListIntents",
		"/hcmnext.intents.v1.IntentService/SimulateIntent",
		"/hcmnext.intents.v1.IntentService/ExecuteIntent",
		"/hcmnext.intents.v1.IntentService/SubmitIntent",
		"/hcmnext.intents.v1.IntentService/CancelIntent",
		"/hcmnext.intents.v1.IntentService/SupersedeIntent",
		"/hcmnext.intents.v1.IntentService/ExplainIntent",
		"/hcmnext.intents.v1.IntentService/ListIntentTimeline",
		"/hcmnext.intents.v1.IntentService/RecommendIntentAction",
		"/hcmnext.intents.v1.IntentService/GetIntentDeepLink",
		"/hcmnext.intents.v1.IntentService/InspectIntentFields",
		"/hcmnext.intents.v1.IntentService/ExportIntentFields",
		"/hcmnext.registry.v1.RegistryService/ListIntentDefinitions",
		"/hcmnext.registry.v1.RegistryService/GetIntentDefinition",
		"/hcmnext.registry.v1.RegistryService/ListCapabilities",
		"/hcmnext.registry.v1.RegistryService/GetCapability",
	}
	sort.Strings(want)
	if !sort.StringsAreSorted(first) {
		t.Fatalf("procedure inventory is not sorted: %v", first)
	}
	if len(first) != len(want) {
		t.Fatalf("procedure inventory has %d entries, want %d", len(first), len(want))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("procedure inventory changed between calls at %d: %q != %q", i, first[i], second[i])
		}
		if first[i] != want[i] {
			t.Fatalf("procedure inventory differs at %d: got %q, want %q", i, first[i], want[i])
		}
	}
}

func TestTodo_REV_007_04_Conformance(t *testing.T) {
	m, err := manifest.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	manifestByProcedure := make(map[string]manifest.EndpointDefinition, len(m.Endpoints))
	for _, endpoint := range m.Endpoints {
		manifestByProcedure["/"+endpoint.EndpointID] = endpoint
	}
	channelManifest := &manifest.EndpointManifest{SchemaVersion: m.SchemaVersion}
	actualRoutes := make([]toolsinvocationpath.Route, 0, len(Procedures()))
	for _, procedure := range Procedures() {
		endpoint, ok := manifestByProcedure[procedure]
		if !ok {
			t.Fatalf("edge publishes %s without a manifest endpoint", procedure)
		}
		channelManifest.Endpoints = append(channelManifest.Endpoints, endpoint)
		actualRoutes = append(actualRoutes, toolsinvocationpath.Route{
			EndpointID:        endpoint.EndpointID,
			Capabilities:      endpoint.CapabilityRefs,
			IntentDefinitions: endpoint.AcceptedIntentDefinitionRefs,
		})
	}
	if findings := toolsinvocationpath.CheckManifestConformance(channelManifest, []toolsinvocationpath.Channel{{Name: "edge", Enabled: true, Routes: actualRoutes}}); len(findings) != 0 {
		t.Fatalf("conformance findings: %+v", findings)
	}
}
