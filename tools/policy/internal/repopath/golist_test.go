package repopath

import (
	"path/filepath"
	"testing"
)

func TestGolistSmoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestGolistNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestRepositoryPackageDirExcludesLocalDependencyAndArtifactTrees(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{name: "module root", dir: root, want: true},
		{name: "tracked package", dir: filepath.Join(root, "internal", "workflow"), want: true},
		{name: "node dependency", dir: filepath.Join(root, "node_modules", "flatted", "golang"), want: false},
		{name: "nested node dependency", dir: filepath.Join(root, "tools", "node_modules", "example"), want: false},
		{name: "artifact worktree", dir: filepath.Join(root, ".artifacts", "worktrees", "other"), want: false},
		{name: "git metadata", dir: filepath.Join(root, ".git", "objects"), want: false},
		{name: "outside repository", dir: filepath.Join(filepath.Dir(root), "other"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRepositoryPackageDir(root, tt.dir); got != tt.want {
				t.Fatalf("isRepositoryPackageDir(%q, %q) = %t, want %t", root, tt.dir, got, tt.want)
			}
		})
	}
}
