package processroles

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestManifestCommandsAndDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "process-roles.yaml")
	data := []byte("version: 1\nmodule: example.test/app\nprocesses:\n  - command: web\n    status: initial\n    role: serving\n  - command: worker\n    status: later\n    role: jobs\n  - command: web\n    status: initial\n    role: duplicate\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := manifest.InitialCommands(); !reflect.DeepEqual(got, []string{"web", "web"}) {
		t.Fatalf("InitialCommands = %v", got)
	}
	if got := manifest.LaterCommands(); !reflect.DeepEqual(got, []string{"worker"}) {
		t.Fatalf("LaterCommands = %v", got)
	}
	if got := manifest.DuplicateCommands(); !reflect.DeepEqual(got, []string{"web"}) {
		t.Fatalf("DuplicateCommands = %v", got)
	}
}

func TestListCmdDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"web", "worker"} {
		if err := os.MkdirAll(filepath.Join(root, "cmd", name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "README.md"), []byte("not a command"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ListCmdDirectories(root)
	if err != nil {
		t.Fatalf("ListCmdDirectories: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"web", "worker"}) {
		t.Fatalf("ListCmdDirectories = %v", got)
	}
}
