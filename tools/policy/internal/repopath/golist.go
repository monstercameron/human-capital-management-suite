package repopath

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Package mirrors the subset of `go list -json` output tools/policy needs:
// a package's own import path and its direct imports (both standard
// library/third-party and within-module).
type Package struct {
	ImportPath string
	Dir        string
	Imports    []string
	Deps       []string
}

// ListPackages runs `go list -json ./...` from root and decodes the
// concatenated JSON object stream `go list` prints (one object per
// package, no enclosing array or separators).
func ListPackages(root string) ([]Package, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = absRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list -json ./... failed: %w\nstderr:\n%s", err, stderr.String())
	}

	var packages []Package
	decoder := json.NewDecoder(&stdout)
	for decoder.More() {
		var pkg Package
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		if !isRepositoryPackageDir(absRoot, pkg.Dir) {
			continue
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// isRepositoryPackageDir keeps policy inventories independent from local
// dependency caches and ignored scratch worktrees. `go list ./...` can include
// Go sources shipped inside JavaScript dependencies (for example,
// node_modules/flatted/golang), even though those sources are not part of the
// repository architecture being governed.
func isRepositoryPackageDir(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	for _, segment := range strings.Split(rel, "/") {
		switch segment {
		case ".artifacts", ".git", "node_modules":
			return false
		}
	}
	return true
}
