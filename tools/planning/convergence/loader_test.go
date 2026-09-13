package convergence

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

func TestSourceLayerClassifiesDownstreamFiles(t *testing.T) {
	for rel, want := range map[string]string{
		"internal/commercial/commercial_test.go":    LayerTest,
		"test/bootstrap/cell.go":                    LayerTest,
		"internal/transport/rpc/server.go":          LayerAPI,
		"internal/intent/modelbinding/binding.go":   LayerModel,
		"internal/commercial/commercial.go":         LayerImplementation,
		"cmd/hcmnext/main.go":                       LayerImplementation,
		"internal/transport/rpc/server_test.go":     LayerTest,
		"internal/intent/modelbinding/bind_test.go": LayerTest,
	} {
		if got := sourceLayer(rel); got != want {
			t.Errorf("sourceLayer(%s) = %s, want %s", rel, got, want)
		}
	}
}

func TestCarriedReturnsOnlyPresentTokensSorted(t *testing.T) {
	got := carried("pins b-digest and a/path.yaml", []string{"a/path.yaml", "", "zzz", "b-digest"})
	if !reflect.DeepEqual(got, []string{"a/path.yaml", "b-digest"}) {
		t.Fatalf("carried = %v", got)
	}
}

func TestSelectOwnersUsesIncludedIntentsBindingsAndBacklogPhases(t *testing.T) {
	m := gateevidence.P1AManifest{
		Release: "P1A",
		Intents: []gateevidence.Intent{{ID: fxAlpha, Disposition: "INCLUDED"}, {ID: fxGamma, Disposition: "EXCLUDED"}},
		SelectionBindings: []gateevidence.SelectionBinding{
			{TodoID: "SELECT-001", Path: "definitions/planning/gates/select-001-jurisdiction-profile.yaml", Digest: "abc"},
		},
	}
	var snap Snapshot
	intents := selectOwners(m, map[string]string{"SELECT-001": "P0", "NEXT-002": "P0"}, &snap)
	if !intents[fxAlpha] || intents[fxGamma] || len(intents) != 1 {
		t.Fatalf("selected intents = %v", intents)
	}
	phases := map[string]string{}
	for _, s := range snap.Selected {
		phases[s.Owner] = s.Phase
	}
	if phases[fxAlpha] != "P1A" || phases["SELECT-001"] != "P0" || phases[SelectionManifestOwner] != "P0" || len(snap.Selected) != 2+len(gateevidence.RequiredSelectionBindingTodoIDs) {
		t.Fatalf("selected owners = %+v", snap.Selected)
	}
	if len(snap.Facts) != 2 || snap.Facts[1].ID != "selection:SELECT-001" || !reflect.DeepEqual(snap.Facts[1].Tokens, []string{m.SelectionBindings[0].Path, "abc"}) {
		t.Fatalf("facts = %+v", snap.Facts)
	}
}

func TestLoadArtifactConsumersScansDownstreamRootsButNotTools(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const artifact = "definitions/planning/gates/select-y.yaml"
	write("internal/commercial/commercial.go", "package commercial\n// pins "+artifact+"\n")
	write("test/acceptance/pilot_test.go", "package acceptance\n// digest-y\n")
	write("tools/planning/pilot/pilot.go", "package pilot\n// "+artifact+"\n")
	write(ThreatRegisterPath, "# references "+artifact+"\n")
	write("internal/unrelated/unrelated.go", "package unrelated\n")

	snap := Snapshot{Facts: []Fact{{ID: "selection:Y", Kind: FactSelectionArtifact, Owner: "Y", Tokens: []string{artifact, "digest-y"}}}}
	if err := loadArtifactConsumers(root, &snap); err != nil {
		t.Fatalf("loadArtifactConsumers: %v", err)
	}
	got := map[string]string{}
	for _, c := range snap.Consumers {
		got[c.Ref] = c.Layer
	}
	want := map[string]string{
		"internal/commercial/commercial.go": LayerImplementation,
		"test/acceptance/pilot_test.go":     LayerTest,
		ThreatRegisterPath:                  LayerThreat,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("consumers = %v, want %v (tools/ must not count)", got, want)
	}
}

func TestLoadThreatConsumersJoinsRegisterSlicesToProductSliceIntents(t *testing.T) {
	root := repoRoot(t)
	ws := closurewitness.Snapshot{Slices: []closurewitness.SliceRow{{SliceID: "promotion", Intents: []string{fxAlpha}}, {SliceID: "unregistered", Intents: []string{fxBeta}}}}
	var snap Snapshot
	if err := loadThreatConsumers(root, ws, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Consumers) != 1 || snap.Consumers[0].Layer != LayerThreat || !strings.HasSuffix(snap.Consumers[0].Ref, "#promotion") || snap.Consumers[0].Tokens[0] != fxAlpha {
		t.Fatalf("threat consumers = %+v", snap.Consumers)
	}
	if err := loadThreatConsumers(t.TempDir(), ws, &snap); err == nil {
		t.Fatal("missing threat register loaded as empty")
	}
}

func TestLoadTodosClaimsOnlyOpenDirectTokens(t *testing.T) {
	var snap Snapshot
	phases, err := loadTodos(repoRoot(t), &snap)
	if err != nil {
		t.Fatal(err)
	}
	if phases["CLOSE-002"] == "" || len(snap.Todos) != len(phases) {
		t.Fatalf("phases for %d todos, %d todo rows", len(phases), len(snap.Todos))
	}
	open := map[string]bool{}
	for _, todo := range snap.Todos {
		open[todo.ID] = !todo.Done && !todo.Retired
	}
	for _, c := range snap.Claims {
		if !open[c.TodoID] || c.Owner == "none" || c.Contract != ContractTodo {
			t.Errorf("claim %+v from a closed todo or a none token", c)
		}
	}
	if _, err := loadTodos(t.TempDir(), &snap); err == nil {
		t.Fatal("missing backlog loaded as empty")
	}
}
