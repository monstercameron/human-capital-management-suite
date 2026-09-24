// Package workspace implements the TOOL-001 Go-workspace policy: go.mod
// pinning, single-module layout, layout conformance and the no-Node/npm
// constraint on the Go build path.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/mod/modfile"
)

// GoVersionInfo reports the parsed go.mod version directives.
type GoVersionInfo struct {
	Go        string // the "go" directive, e.g. "1.26.3"
	Toolchain string // the "toolchain" directive, or "" if absent
}

var pinnedGoVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
var toolchainRE = regexp.MustCompile(`^go\d+\.\d+(\.\d+)?$`)

// ParseGoMod reads root/go.mod and reports its version directives.
func ParseGoMod(root string) (GoVersionInfo, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return GoVersionInfo{}, fmt.Errorf("workspace: reading go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return GoVersionInfo{}, fmt.Errorf("workspace: parsing go.mod: %w", err)
	}

	info := GoVersionInfo{}
	if f.Go != nil {
		info.Go = f.Go.Version
	}
	if f.Toolchain != nil {
		info.Toolchain = f.Toolchain.Name
	}
	return info, nil
}

// IsPinnedGoVersion reports whether v looks like a full x.y.z patch version
// rather than a floating "x.y" line.
func IsPinnedGoVersion(v string) bool {
	return pinnedGoVersionRE.MatchString(v)
}

// IsAcceptableToolchain reports whether a toolchain directive value is
// well-formed. An empty string (no toolchain directive) is also acceptable:
// TOOL-001 only requires that a *pinned Go version* exists; a toolchain line
// is optional documentation of which toolchain satisfies it.
func IsAcceptableToolchain(toolchain string) bool {
	if toolchain == "" {
		return true
	}
	return toolchainRE.MatchString(toolchain)
}

// HasGoWork reports whether root contains a go.work file.
func HasGoWork(root string) bool {
	_, err := os.Stat(filepath.Join(root, "go.work"))
	return err == nil
}

// FindNodeExecCalls scans .go files under root (excluding exact relative
// paths and directory names) for a literal os/exec invocation of "npm" or
// "node". It is
// a simple textual scan, not a full parse, per the TOOL-001 GREEN clause:
// "a simple assertion that no Go file imports os/exec of npm is enough".
func FindNodeExecCalls(root string, ignorePaths map[string]bool) ([]string, error) {
	var hits []string

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`exec\.Command\(\s*"npm"`),
		regexp.MustCompile(`exec\.Command\(\s*"node"`),
		regexp.MustCompile(`exec\.CommandContext\([^,]+,\s*"npm"`),
		regexp.MustCompile(`exec\.CommandContext\([^,]+,\s*"node"`),
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if ignorePaths[rel] || ignorePaths[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, p := range patterns {
			if p.Match(data) {
				rel, _ := filepath.Rel(root, path)
				hits = append(hits, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("workspace: scanning for Node/npm exec calls: %w", err)
	}
	return hits, nil
}

// FindGoModules returns every go.mod below root, excluding exact relative
// paths, directory names, and their descendants. Paths use slash separators
// on every platform.
func FindGoModules(root string, ignorePaths map[string]bool) ([]string, error) {
	var modules []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if ignorePaths[rel] || ignorePaths[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			modules = append(modules, filepath.ToSlash(filepath.Dir(rel)))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("workspace: scanning Go modules: %w", err)
	}
	return modules, nil
}
