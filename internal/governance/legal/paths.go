package legal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// ErrRepoRootNotFound is returned when the checked-in definition tree cannot
// be located.
var ErrRepoRootNotFound = errors.New("legal: cannot locate the repository root holding definitions/legal/packs")

// PackDefinitionDir is the repository-relative directory holding checked-in
// rule-pack definition files. Seed fixtures live under seed/, the fifty-one
// extracted state drafts under states/.
const PackDefinitionDir = "definitions/legal/packs"

// ResearchDir is the repository-relative directory holding the fifty-one state
// research files every citation in a state draft points back to.
const ResearchDir = "planning/research/state-employment-law"

var (
	repoRootOnce sync.Once
	repoRootPath string
	repoRootErr  error
)

// RepoRoot locates the repository root: the nearest ancestor directory that
// holds go.mod. It looks upward from this source file first and from the
// working directory second, so it resolves the same whether the caller is a
// test in this package, a tool run from the repository root, or a tool run
// from a subdirectory.
//
// The rule packs this package ships are fixtures drawn from checked-in
// research (see the package doc), so reading them from the repository tree is
// the correct source of truth. A deployment that ships releases as data
// rather than as fixtures uses [LoadPackDefinitionFile] with its own path.
func RepoRoot() (string, error) {
	repoRootOnce.Do(func() {
		var candidates []string
		if _, thisFile, _, ok := runtime.Caller(0); ok {
			candidates = append(candidates, filepath.Dir(thisFile))
		}
		if wd, err := os.Getwd(); err == nil {
			candidates = append(candidates, wd)
		}
		for _, start := range candidates {
			if root, ok := ascendToGoMod(start); ok {
				repoRootPath = root
				return
			}
		}
		repoRootErr = ErrRepoRootNotFound
	})
	return repoRootPath, repoRootErr
}

func ascendToGoMod(dir string) (string, bool) {
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// PackDefinitionPath returns the absolute path of a checked-in definition
// file, e.g. PackDefinitionPath("seed", "us-ca.json").
func PackDefinitionPath(parts ...string) (string, error) {
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{root, filepath.FromSlash(PackDefinitionDir)}, parts...)...), nil
}

// loadDefinedPack loads one checked-in definition file and returns the
// validated, unsigned pack it defines.
func loadDefinedPack(parts ...string) (RulePack, error) {
	path, err := PackDefinitionPath(parts...)
	if err != nil {
		return RulePack{}, err
	}
	def, err := LoadPackDefinitionFile(path)
	if err != nil {
		return RulePack{}, fmt.Errorf("legal: loading %s: %w", filepath.Join(parts...), err)
	}
	candidate, err := def.Candidate()
	if err != nil {
		return RulePack{}, fmt.Errorf("legal: building %s: %w", filepath.Join(parts...), err)
	}
	return candidate.Pack(), nil
}
