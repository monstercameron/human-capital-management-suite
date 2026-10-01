package edge

import "testing"

func TestTodo_EDGE_010_ProductionManifest(t *testing.T) {
	evidence, err := QualifyProduction()
	if err != nil {
		t.Fatalf("QualifyProduction: %v", err)
	}
	if !evidence.Ready || evidence.Status != "READY" {
		t.Fatalf("production edge is not ready: %+v", evidence)
	}
	if len(evidence.Controls) != 7 || evidence.ManifestDigest == "" {
		t.Fatalf("production qualification omitted controls or digest: %+v", evidence)
	}
	manifest := ProductionManifest()
	if len(manifest.Routes) != 1 || manifest.Routes[0].Methods[0] != "POST" {
		t.Fatalf("production route is not exact: %+v", manifest.Routes)
	}
	if !manifest.Egress[0].Logged || manifest.Egress[0].FailureAction != "DENY" {
		t.Fatalf("production egress is not logged and fail-closed: %+v", manifest.Egress[0])
	}
}
