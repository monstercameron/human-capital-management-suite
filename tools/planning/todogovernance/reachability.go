// Reachability closure (REV-103-01): a ticked runtime todo must name at
// least one Go package that a shipped binary links. The backlog reports
// features as done that no running process can execute; the plan check
// fails while such ticks stand.
//
// Classification is deliberately narrow so the gate stays actionable:
//
//   - Unticked and retired todos are out of scope for this gate.
//   - A todo declares CAPABILITY=RUNTIME or CAPABILITY=LIBRARY and OWNER in
//     INTENT CONTEXT. Neither value is inferred from the ID, package path,
//     or prose.
//   - Package names come from the backticked Evidence and Refs fields,
//     which is where the corpus records the proving package
//     (e.g. "`TestTodo_LEAVE_001` ... in `internal/domains/leave`").
//
// All functions here are pure: callers inject the binary closure (from
// `go list -deps ./cmd/...`) so tests never shell out.
package todogovernance

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// RuleREV10301 is the governance todo owning the reachability check.
const RuleREV10301 = "REV-103-01"

// CodeUnreachableRuntime marks a ticked runtime todo whose named packages
// are all absent from the binary dependency closure.
const CodeUnreachableRuntime = "UNREACHABLE_RUNTIME"
const CodeUnclassifiedCapability = "UNCLASSIFIED_CAPABILITY"

// modulePrefixes are the import-path prefixes stripped before matching an
// evidence token against the binary closure. Both spellings are accepted:
// the canonical module path and the long-standing `monstercamarin` typo
// the corpus occasionally repeats.
var modulePrefixes = []string{
	"github.com/monstercameron/human-capital-management-suite/",
	"github.com/monstercamarin/human-capital-management-suite/",
}

var (
	backtickRe     = regexp.MustCompile("`([^`]*)`")
	moduleSplitRe  = regexp.MustCompile(`^[A-Za-z0-9_.\-/]+$`)
	packageTokenRe = regexp.MustCompile(`^(internal|tools|pkg|cmd|test|gen)/[A-Za-z0-9_.\-/]+$`)
)

// ReachabilityFinding names one ticked runtime todo no binary reaches.
type ReachabilityFinding struct {
	ID string
	// Kind identifies an unreachable runtime capability or missing explicit
	// capability classification.
	Kind string
	// Detail carries the declared owner and sorted package list.
	Detail string
}

func (f ReachabilityFinding) String() string {
	if f.Kind == CodeUnclassifiedCapability {
		return fmt.Sprintf("%s: %s: %s: ticked todo with unreachable packages lacks explicit CAPABILITY and OWNER (%s)", RuleREV10301, f.ID, f.Kind, f.Detail)
	}
	return fmt.Sprintf("%s: %s: %s: ticked runtime todo names only packages no binary reaches (%s)", RuleREV10301, f.ID, f.Kind, f.Detail)
}

// Key returns a stable identity for the finding suitable for golden
// comparison: rule, todo, code and the package list.
func (f ReachabilityFinding) Key() string {
	return RuleREV10301 + "|" + f.ID + "|" + f.Kind + "|" + f.Detail
}

// TodoPackages extracts every normalized repo-relative Go package path
// named in a todo's Evidence and Refs fields. Tokens are backticked
// evidence phrases such as `internal/domains/leave`,
// `./internal/domains/leave`, `internal/domains/leave/...`, or the full
// import path; file paths (*.go, *.md), commands, test names and prose
// are ignored. The result is sorted and deduplicated.
func TodoPackages(td todoregistry.Todo) []string {
	var out []string
	seen := map[string]bool{}
	for _, field := range []string{td.Evidence, td.Refs} {
		for _, tok := range backtickRe.FindAllStringSubmatch(field, -1) {
			if p, ok := normalizePackageToken(strings.TrimSpace(tok[1])); ok && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// normalizePackageToken maps one raw backticked token to a repo-relative
// package path, reporting false for anything that is not a package
// reference under a known root.
func normalizePackageToken(tok string) (string, bool) {
	for _, prefix := range modulePrefixes {
		if strings.HasPrefix(tok, prefix) {
			tok = strings.TrimPrefix(tok, prefix)
			break
		}
	}
	tok = strings.TrimPrefix(tok, "./")
	tok = strings.TrimSuffix(tok, "/...")
	tok = strings.TrimSuffix(tok, "/")
	// Repo-relative package directories never contain a dot; file
	// references (`pkg/x.go`), version strings and prose with dots are
	// not packages.
	if tok == "" || strings.Contains(tok, ".") {
		return "", false
	}
	if !moduleSplitRe.MatchString(tok) || !packageTokenRe.MatchString(tok) {
		return "", false
	}
	return tok, true
}

// TodoRuntimeClass returns only the explicit capability declaration.
// Package location and TODO ID are never classification inputs.
func TodoRuntimeClass(td todoregistry.Todo) string {
	return td.CapabilityClass
}

// NormalizeReachable maps raw `go list -deps ./cmd/...` output lines to
// the repo-relative package set TodoPackages produces, dropping external
// modules and stdlib entries.
func NormalizeReachable(raw []string) map[string]bool {
	reachable := make(map[string]bool, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		for _, prefix := range modulePrefixes {
			if strings.HasPrefix(line, prefix) {
				reachable[strings.TrimPrefix(line, prefix)] = true
				break
			}
		}
	}
	return reachable
}

// CheckReachability returns a finding for a ticked, non-retired todo whose
// explicit runtime packages are all absent from reachable, or whose
// unreachable packages lack explicit capability and owner metadata.
// Findings follow input order. A runtime todo with at least one reachable
// package is served and stays silent, as does every declared library todo.
func CheckReachability(todos []todoregistry.Todo, reachable map[string]bool) []ReachabilityFinding {
	return checkReachability(todos, reachable, nil)
}

// CheckReachabilityInPackages applies the same rule as CheckReachability,
// but first removes references that are not real Go packages in the current
// repository. Evidence often names directory roots such as `cmd` or stale
// package paths; those must not be reported as unreachable runtime code.
func CheckReachabilityInPackages(todos []todoregistry.Todo, reachable, knownPackages map[string]bool) []ReachabilityFinding {
	return checkReachability(todos, reachable, knownPackages)
}

func checkReachability(todos []todoregistry.Todo, reachable, knownPackages map[string]bool) []ReachabilityFinding {
	var findings []ReachabilityFinding
	for _, td := range todos {
		if !td.Done || td.Retired {
			continue
		}
		pkgs := TodoPackages(td)
		if knownPackages != nil {
			filtered := pkgs[:0]
			for _, p := range pkgs {
				if knownPackages[p] {
					filtered = append(filtered, p)
				}
			}
			pkgs = filtered
		}
		class := TodoRuntimeClass(td)
		if class == todoregistry.CapabilityLibrary {
			continue
		}
		if len(pkgs) == 0 {
			continue
		}
		served := false
		for _, p := range pkgs {
			if reachable[p] {
				served = true
				break
			}
		}
		if served {
			continue
		}
		kind := CodeUnreachableRuntime
		owner := td.Owner
		if class != todoregistry.CapabilityRuntime || owner == "" || td.CapabilityConflict || td.OwnerConflict {
			kind = CodeUnclassifiedCapability
			if owner == "" || td.OwnerConflict {
				owner = "MISSING"
			}
		}
		findings = append(findings, ReachabilityFinding{
			ID:     td.ID,
			Kind:   kind,
			Detail: fmt.Sprintf("owner=%s; capability=%s; packages=%s", owner, classOrMissing(class), strings.Join(pkgs, ",")),
		})
	}
	return findings
}

func classOrMissing(class string) string {
	if class == "" {
		return "MISSING"
	}
	return class
}

// RenderReachabilityFindings renders findings in the stable line form
// pinned by the REV-103-01 golden test: one line per finding,
// LF-terminated.
func RenderReachabilityFindings(findings []ReachabilityFinding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString(f.String() + "\n")
	}
	return b.String()
}
