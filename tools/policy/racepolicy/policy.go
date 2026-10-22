package racepolicy

import (
	"go/build"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is one concurrent package's race-coverage verdict.
type Finding struct {
	Package ConcurrentPackage
	// HasLinuxRaceableTests is true when go/build, targeting linux/amd64
	// with the race build tag and cgo enabled (the context TOOL-012's CI job
	// uses), resolves at least one test file for this package's directory.
	// A file gated behind `!race` is excluded from that job even though it
	// may run in ordinary tests — which is exactly the gap this check finds.
	HasLinuxRaceableTests bool
	// Detail explains a false HasLinuxRaceableTests: what go/build reported
	// (no Go files at all, or Go files but zero test files) for this
	// directory under the Linux context.
	Detail string
}

// Violation reports whether this finding fails the policy.
func (f Finding) Violation() bool { return !f.HasLinuxRaceableTests }

// Report is the full policy evaluation over a directory tree.
type Report struct {
	Findings []Finding
}

// Violations returns the subset of Findings that fail the policy, in the
// same (import-path-sorted) order Evaluate produced them.
func (r Report) Violations() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Violation() {
			out = append(out, f)
		}
	}
	return out
}

// linuxRaceBuildContext is a copy of go/build.Default retargeted at the
// context TOOL-012's actual `go test -race` CI job uses: linux/amd64 with
// cgo enabled and the race build tag. It does not depend on the host
// platform running this policy check (this repository's development host
// is windows/arm64, which has no race detector; see doc.go).
func linuxRaceBuildContext() build.Context {
	ctx := build.Default
	ctx.GOOS = "linux"
	ctx.GOARCH = "amd64"
	ctx.CgoEnabled = true
	// Keep toolchain experiment tags but replace host architecture feature
	// tags with the default CI target level (GOAMD64=v1). In particular, a
	// policy run on arm64 must not select arm64-only source files.
	toolTags := make([]string, 0, len(ctx.ToolTags)+1)
	for _, tag := range ctx.ToolTags {
		if strings.HasPrefix(tag, "goexperiment.") {
			toolTags = append(toolTags, tag)
		}
	}
	ctx.ToolTags = append(toolTags, "amd64.v1")
	// The go command adds this build tag whenever -race is enabled.
	ctx.BuildTags = []string{"race"}
	return ctx
}

// packageHasLinuxRaceableTests reports whether dir (an absolute path)
// resolves at least one test file (TestGoFiles or XTestGoFiles) under the
// Linux build context.
func packageHasLinuxRaceableTests(dir string) (bool, string) {
	ctx := linuxRaceBuildContext()
	pkg, err := ctx.ImportDir(dir, 0)
	if err != nil {
		if _, ok := err.(*build.NoGoError); ok {
			return false, "no Go source files resolve for this directory under the linux/amd64 race build context"
		}
		// build.MultiplePackageError and similar still populate pkg with
		// what it found; fall through and judge on the file lists.
		if pkg == nil {
			return false, "go/build.ImportDir failed: " + err.Error()
		}
	}
	if len(pkg.TestGoFiles) > 0 || len(pkg.XTestGoFiles) > 0 {
		return true, ""
	}
	return false, "Go source files resolve under linux/amd64 with -race, but no test file does (either none exist, or the only ones present are excluded by a build constraint)"
}

// Evaluate finds every concurrent package under root (module path
// modulePath) and checks each one against packageHasLinuxRaceableTests.
func Evaluate(root, modulePath string) (Report, error) {
	packages, err := FindConcurrentPackages(root, modulePath)
	if err != nil {
		return Report{}, err
	}

	findings := make([]Finding, 0, len(packages))
	for _, pkg := range packages {
		dir := filepath.Join(root, filepath.FromSlash(pkg.Dir))
		has, detail := packageHasLinuxRaceableTests(dir)
		findings = append(findings, Finding{Package: pkg, HasLinuxRaceableTests: has, Detail: detail})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Package.ImportPath < findings[j].Package.ImportPath })
	return Report{Findings: findings}, nil
}
