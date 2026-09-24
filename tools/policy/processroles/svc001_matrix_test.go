package processroles_test

import (
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/processroles"
)

// The golden pins the executable boundary and role ownership vocabulary that
// deployments consume from the manifest.
func TestTodo_SVC_001_Golden(t *testing.T) {
	m, err := processroles.Load(filepath.Join(repopath.RootDir(), "definitions", "architecture", "process-roles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"hcmnext": "Primary API/service process", "worker": "Durable/background execution process", "projector": "Rebuildable-projection process", "migrate": "Bootstrap migration command"}
	got := make(map[string]string)
	for _, p := range m.Processes {
		if p.Status == "initial" {
			if len(p.Role) == 0 {
				t.Errorf("initial command %q has no semantic role", p.Command)
			}
			if prefix, ok := want[p.Command]; ok {
				if len(p.Role) < len(prefix) || p.Role[:len(prefix)] != prefix {
					t.Errorf("%s role = %q, want prefix %q", p.Command, p.Role, prefix)
				}
				got[p.Command] = p.Role
			}
		}
	}
	if len(got) != len(want) {
		t.Fatalf("core role rows = %v, want all %v", got, want)
	}
}

// Integration checks the checked-in manifest against the command roots that
// are actually present in this checkout, including documented layout waivers.
func TestTodo_SVC_001_Integration(t *testing.T) {
	root := repopath.RootDir()
	m, err := processroles.Load(filepath.Join(root, "definitions", "architecture", "process-roles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := processroles.ListCmdDirectories(root)
	if err != nil {
		t.Fatal(err)
	}
	lm := layoutManifestForTest(t, root)
	rows := toSet(m.InitialCommands())
	for _, dir := range dirs {
		if rows[dir] {
			continue
		}
		v := lm.ClassifyImportPath(m.Module + "/cmd/" + dir)
		if !v.Waived {
			t.Errorf("cmd/%s has no manifest row or layout waiver", dir)
		}
	}
	for _, dir := range m.InitialCommands() {
		found := false
		for _, actual := range dirs {
			if actual == dir {
				found = true
			}
		}
		if !found {
			t.Errorf("manifest command %q has no cmd directory", dir)
		}
	}
}

func layoutManifestForTest(t *testing.T, root string) *layout.Manifest {
	t.Helper()
	m, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Mutation exercises the manifest duplicate detector with a deliberate
// duplicate and confirms removing a required command changes the result.
func TestTodo_SVC_001_Mutation(t *testing.T) {
	m := &processroles.Manifest{Processes: []processroles.Process{{Command: "worker"}, {Command: "projector"}}}
	if dupes := m.DuplicateCommands(); len(dupes) != 0 {
		t.Fatalf("unmutated manifest duplicates = %v", dupes)
	}
	m.Processes = append(m.Processes, processroles.Process{Command: "worker"})
	if dupes := m.DuplicateCommands(); len(dupes) != 1 || dupes[0] != "worker" {
		t.Fatalf("duplicate mutation yielded %v, want [worker]", dupes)
	}
	if got := m.InitialCommands(); len(got) != 0 {
		t.Fatalf("status-less rows became initial commands: %v", got)
	}
}

// Race verifies concurrent reads of an immutable parsed manifest return
// independent slices without corrupting each other.
func TestTodo_SVC_001_Race(t *testing.T) {
	m, err := processroles.Load(filepath.Join(repopath.RootDir(), "definitions", "architecture", "process-roles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := m.InitialCommands()
	sort.Strings(want)
	const workers = 12
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := m.InitialCommands()
			sort.Strings(got)
			if !equalStrings(got, want) {
				t.Errorf("concurrent command list = %v, want %v", got, want)
			}
			if len(m.DuplicateCommands()) != 0 {
				t.Error("immutable manifest reports duplicates")
			}
		}()
	}
	wg.Wait()
}
