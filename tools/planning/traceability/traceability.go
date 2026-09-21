// Package traceability builds the requirement -> todo -> test -> evidence
// crosswalk and rejects orphaned claims (GOV-003): every completed todo's
// Evidence line must name at least one Test/Fuzz/Benchmark function that
// actually exists in the repository's *_test.go sources.
//
// REFACTOR note: this crosswalk is derived by scanning todos.md and the
// repository test sources directly. Once SchemaFlux emits dependency data
// for the delivery manifest (GOV-001), generate the crosswalk from that
// instead of re-deriving it from prose.
package traceability

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// Orphan names one evidence claim that does not resolve to a real test, or
// a completed todo with no usable evidence at all.
type Orphan struct {
	ID     string
	Reason string
}

func (o Orphan) String() string {
	return fmt.Sprintf("%s: %s", o.ID, o.Reason)
}

var (
	tickRe = regexp.MustCompile("`([^`]*)`")
	nameRe = regexp.MustCompile(`^(Test|Fuzz|Benchmark)[A-Za-z0-9_]*$`)
)

// ExtractEvidenceTestNames extracts every Test/Fuzz/Benchmark function name
// referenced in an Evidence field's prose. It recognizes three forms, all
// observed in planning/todos.md:
//
//   - a plain backtick-quoted name: `TestFoo`
//   - a brace-expansion group: `TestTodo_ID_{Golden,Race}` expands to
//     TestTodo_ID_Golden and TestTodo_ID_Race
//   - a shorthand suffix continuation: `TestTodo_ID`, `_Golden`, `_Race`
//     expands to TestTodo_ID, TestTodo_ID_Golden, TestTodo_ID_Race by
//     appending each `_suffix` token to the most recently seen full name
//
// Backtick tokens that are not Test/Fuzz/Benchmark identifiers (package
// paths, shell commands, prose) are ignored. Names may also be comma
// separated within a single backtick pair.
func ExtractEvidenceTestNames(evidence string) []string {
	var names []string
	lastBase := ""

	for _, tok := range tickRe.FindAllStringSubmatch(evidence, -1) {
		for _, part := range splitOutsideBraces(tok[1]) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			if open := strings.Index(part, "{"); open != -1 {
				shut := strings.Index(part, "}")
				if shut == -1 || shut < open {
					continue
				}
				prefix := part[:open]
				inside := part[open+1 : shut]
				base := strings.TrimSuffix(prefix, "_")
				// Only a Test/Fuzz/Benchmark-shaped prefix triggers brace
				// expansion - "definitions/telemetry/{a,b,c}.yaml" or
				// "planning/research/state-employment-law/{ca,ny}.md" are
				// backticked file-path lists, not test names, and must be
				// ignored rather than expanded into fake test names.
				if !nameRe.MatchString(base) {
					continue
				}
				lastBase = base
				for _, suffix := range strings.Split(inside, ",") {
					suffix = strings.TrimSpace(suffix)
					if suffix == "" {
						continue
					}
					names = append(names, prefix+suffix)
				}
				continue
			}

			if strings.HasPrefix(part, "_") {
				if lastBase != "" {
					names = append(names, lastBase+part)
				}
				continue
			}

			if nameRe.MatchString(part) {
				names = append(names, part)
				// Only a "Test..." name can become (or extend) the root
				// that later "_suffix" shorthand tokens attach to - a
				// sibling "FuzzTodo_..."/"BenchmarkTodo_..." entry never
				// takes over that role, since no corpus example attaches a
				// shorthand suffix to one. And a "Test..." name that is
				// itself an extension of the current root (e.g.
				// "TestTodo_DATA_005_Integration" following
				// "TestTodo_DATA_005") does not replace it: a later
				// shorthand suffix like "_Fault_Integration" still means
				// "root + suffix", not "extension + suffix".
				if strings.HasPrefix(part, "Test") && (lastBase == "" || !strings.HasPrefix(part, lastBase)) {
					lastBase = part
				}
			}
		}
	}

	return names
}

// splitOutsideBraces splits s on commas that are not inside a {...} group,
// so "TestTodo_ID_{Golden,Race}" survives as one part while
// "TestFoo, FuzzFoo" still splits into two.
func splitOutsideBraces(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// ScanTestNames walks root for *_test.go files (skipping any directory
// named "testdata", ".git" or "vendor") and returns every top-level
// Test/Fuzz/Benchmark function name declared in them.
func ScanTestNames(root string) (map[string]bool, error) {
	names := make(map[string]bool)
	funcRe := regexp.MustCompile(`^func\s+(Test|Fuzz|Benchmark)[A-Za-z0-9_]*\s*\(`)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Dot-prefixed directories hold caches and embedded-server runtimes
			// (.artifacts, .git, .gocache) whose files appear and vanish while a
			// scan runs; they never hold repository tests.
			if name := info.Name(); name == "testdata" || name == "vendor" || name == "node_modules" || (strings.HasPrefix(name, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(content), "\n") {
			m := funcRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			// Recover the full identifier (funcRe stops at "(").
			rest := strings.TrimPrefix(line, "func")
			rest = strings.TrimSpace(rest)
			name, _, _ := strings.Cut(rest, "(")
			names[strings.TrimSpace(name)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return names, nil
}

// CheckTraceability returns an Orphan for every completed (Done) todo whose
// Evidence field is empty, names no recognizable test, or names no test that
// exists in existingTests. Evidence often names one top-level test followed
// by shorthand subtest labels, so one resolved top-level function satisfies
// the crosswalk. Todos that are not Done are ignored: their evidence, if any,
// may legitimately describe remaining/future work.
func CheckTraceability(todos []todoregistry.Todo, existingTests map[string]bool) []Orphan {
	var orphans []Orphan

	for _, td := range todos {
		if !td.Done {
			continue
		}
		if strings.TrimSpace(td.Evidence) == "" {
			orphans = append(orphans, Orphan{ID: td.ID, Reason: "completed todo has no Evidence field"})
			continue
		}

		names := ExtractEvidenceTestNames(td.Evidence)
		if len(names) == 0 {
			orphans = append(orphans, Orphan{ID: td.ID, Reason: "Evidence field names no recognizable Test/Fuzz/Benchmark function"})
			continue
		}

		resolved := false
		for _, n := range names {
			if existingTests[n] {
				resolved = true
				break
			}
		}
		if !resolved {
			orphans = append(orphans, Orphan{ID: td.ID, Reason: fmt.Sprintf("Evidence names no repository test; first unresolved name is %s", names[0])})
		}
	}

	return orphans
}
