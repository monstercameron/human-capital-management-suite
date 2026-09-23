// Reachability closure (REV-103-01): a ticked runtime todo must name at
// least one Go package that a shipped binary links. The backlog reports
// features as done that no running process can execute; the plan check
// fails while such ticks stand.
//
// Classification is deliberately narrow so the gate stays actionable:
//
//   - Unticked, retired, and UI/chat-surface todos (CHAT-, UXLIVE-,
//     WF-UI-, WEB-) are out of scope for this gate.
//   - A todo whose GREEN field carries a backticked `LIBRARY` token is a
//     declared library; a backticked `RUNTIME` token is a declared runtime
//     capability. The marker is the per-todo declaration the GREEN clause
//     requires; undeclared todos fall back to their package roots.
//   - Undeclared todos naming only tooling roots (tools/, test/, gen/)
//     are library by location: those trees never link into ./cmd/...
//     binaries by design. Naming any of internal/, cmd/ or pkg/ makes the
//     todo a runtime todo, and every named package must then be absent
//     from the binary closure for the finding to fire.
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

// reachabilityExemptPrefixes are ticked-todo ID families this gate never
// flags: conversational/chat surfaces and browser/UI work whose serving
// path is not a ./cmd/... binary dependency.
var reachabilityExemptPrefixes = []string{"CHAT-", "UXLIVE-", "WF-UI-", "WEB-"}

// runtimeRoots are package roots whose code ships inside a ./cmd/...
// binary when linked. Every other known root (tools/, test/, gen/) never
// links into a shipped binary, so todos naming only those roots are
// library by location.
var runtimeRoots = map[string]bool{"internal": true, "cmd": true, "pkg": true}

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
	declarationRe  = regexp.MustCompile("`(RUNTIME|LIBRARY)`")
	moduleSplitRe  = regexp.MustCompile(`^[A-Za-z0-9_.\-/]+$`)
	packageTokenRe = regexp.MustCompile(`^(internal|tools|pkg|cmd|test|gen)(/[A-Za-z0-9_.\-]+)*$`)
)

// ReachabilityFinding names one ticked runtime todo no binary reaches.
type ReachabilityFinding struct {
	ID string
	// Kind is always CodeUnreachableRuntime; kept as a field so the
	// renderer and any future kind share one shape.
	Kind string
	// Detail carries the re-tagged metadata: the owning family derived
	// from the todo ID and the sorted unreachable package list, e.g.
	// "owner=LEAVE; packages=internal/domains/leave".
	Detail string
}

func (f ReachabilityFinding) String() string {
	return fmt.Sprintf("%s: %s: %s: ticked runtime todo names only packages no binary reaches (%s)", RuleREV10301, f.ID, f.Kind, f.Detail)
}

// Key returns a stable identity for the finding suitable for golden
// comparison: rule, todo, code and the package list.
func (f ReachabilityFinding) Key() string {
	return RuleREV10301 + "|" + f.ID + "|" + f.Kind + "|" + f.Detail
}

// ReachabilityOwner derives the owning family from a todo ID: the text
// before the first "-". It is the owner tag the GREEN clause requires on
// every affected tick.
func ReachabilityOwner(id string) string {
	if cut, _, ok := strings.Cut(id, "-"); ok && cut != "" {
		return cut
	}
	return id
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

// TodoRuntimeClass reports whether a todo is a "runtime" capability whose
// packages must be reachable from a binary, or a "library" that needs no
// binary consumer. An explicit backticked `RUNTIME`/`LIBRARY` token in
// GREEN wins; otherwise any internal/, cmd/ or pkg/ package makes the
// todo runtime, while tools/-only, test/-only, gen/-only, or package-less
// todos are library.
func TodoRuntimeClass(td todoregistry.Todo, pkgs []string) string {
	if m := declarationRe.FindStringSubmatch(td.Green); m != nil {
		return m[1]
	}
	for _, p := range pkgs {
		root, _, _ := strings.Cut(p, "/")
		if runtimeRoots[root] {
			return "RUNTIME"
		}
	}
	return "LIBRARY"
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

// CheckReachability returns one finding for every ticked, non-retired,
// non-exempt runtime todo whose named packages are all absent from
// reachable. Findings follow input order. A runtime todo with at least
// one reachable package is served and stays silent, as does every
// library todo.
func CheckReachability(todos []todoregistry.Todo, reachable map[string]bool) []ReachabilityFinding {
	var findings []ReachabilityFinding
	for _, td := range todos {
		if !td.Done || td.Retired {
			continue
		}
		if isReachabilityExempt(td.ID) {
			continue
		}
		pkgs := TodoPackages(td)
		if TodoRuntimeClass(td, pkgs) != "RUNTIME" {
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
		findings = append(findings, ReachabilityFinding{
			ID:     td.ID,
			Kind:   CodeUnreachableRuntime,
			Detail: fmt.Sprintf("owner=%s; packages=%s", ReachabilityOwner(td.ID), strings.Join(pkgs, ",")),
		})
	}
	return findings
}

// isReachabilityExempt reports whether id belongs to a UI/chat family
// this gate never flags.
func isReachabilityExempt(id string) bool {
	for _, prefix := range reachabilityExemptPrefixes {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
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
