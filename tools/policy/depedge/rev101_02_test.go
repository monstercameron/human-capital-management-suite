package depedge_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/importgraph"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
)

// wantUpwardRule mirrors depedge.RuleLayerUpward (asserted equal in
// TestTodo_REV_101_02) so tables below read without a package qualifier.
const wantUpwardRule = "layer-must-not-import-upward"

// rev10102LayerOf resolves a module-relative path to its ranked layer,
// mirroring the checker's own classification over the policy's exported
// fields.
func rev10102LayerOf(p *depedge.Policy, rel string) (depedge.Layer, bool) {
	for _, l := range p.Layers {
		for _, root := range l.Roots {
			if rel == root || strings.HasPrefix(rel, root+"/") {
				return l, true
			}
		}
	}
	return depedge.Layer{}, false
}

// rev10102ExpectedRanks is the REV-101-02 GREEN contract: every spine root
// from the RED clause carries a rank, including the three the review found
// missing (humanwork, application, platform).
func rev10102ExpectedRanks() map[string]struct {
	layer string
	rank  int
} {
	return map[string]struct {
		layer string
		rank  int
	}{
		"internal/kernel/values":      {"kernel", 0},
		"internal/engines/transform":  {"engines", 1},
		"internal/domains/people":     {"domains", 2},
		"internal/capability/gateway": {"capabilities", 3},
		"internal/workflow/step":      {"workflow", 4},
		"internal/platform/execution": {"platform", 5},
		"internal/humanwork/workitem": {"humanwork", 5},
		"internal/transport/grpc":     {"transport", 5},
		"internal/application":        {"application", 6},
	}
}

// rev10102ExpectedExceptions is the exact set of dated, owned waivers the
// policy carries for live-graph edges REV-101-02 may not move (workflow
// step adapters and the sandbox composer live in packages this todo does
// not own). The leave -> workitem edge is deliberately absent: it is
// fixed, not waived.
func rev10102ExpectedExceptions() [][2]string {
	return [][2]string{
		{"internal/platform/sandbox", "internal/application"},
		{"internal/workflow/cancellation", "internal/humanwork/workitem"},
		{"internal/workflow/execute", "internal/humanwork"},
		{"internal/workflow/execute", "internal/humanwork/workitem"},
		{"internal/workflow/execute/effects", "internal/humanwork/workitem"},
		{"internal/workflow/inspect", "internal/humanwork/workitem"},
		{"internal/workflow/migrate/artifacts", "internal/humanwork/workitem"},
		{"internal/workflow/promotionexec", "internal/humanwork"},
		{"internal/workflow/promotionexec", "internal/humanwork/approverclass"},
		{"internal/workflow/promotionexec", "internal/humanwork/workitem"},
		{"internal/workflow/prototype", "internal/humanwork"},
		{"internal/workflow/simulate", "internal/humanwork"},
		{"internal/workflow/simulate", "internal/platform/timeauth"},
		{"internal/workflow/steps/approval", "internal/humanwork"},
		{"internal/workflow/steps/approval", "internal/humanwork/workitem"},
		{"internal/workflow/steps/task", "internal/humanwork"},
		{"internal/workflow/steps/task", "internal/humanwork/workitem"},
		{"internal/workflow/steps/task", "internal/humanwork/formcontinuity"},
	}
}

// TestTodo_REV_101_02 is the REV-101-02 primary test: every declared spine
// root has a rank, the checker rejects any edge to a higher rank (not only
// the five named rules), and the waiver set is exactly the dated, owned
// list above.
func TestTodo_REV_101_02(t *testing.T) {
	p := loadPolicy(t)
	mod := p.Module

	t.Run("every declared spine root has a rank", func(t *testing.T) {
		for rel, want := range rev10102ExpectedRanks() {
			got, ok := rev10102LayerOf(p, rel)
			if !ok {
				t.Errorf("no ranked layer covers %q: humanwork, application and platform must carry ranks", rel)
				continue
			}
			if got.Name != want.layer || got.Rank != want.rank {
				t.Errorf("%q resolves to layer %q rank %d, want layer %q rank %d", rel, got.Name, got.Rank, want.layer, want.rank)
			}
		}
	})

	t.Run("the generic upward rule rejects edges the named rules miss", func(t *testing.T) {
		// The exception-free view: what the checker itself flags before
		// any waiver applies.
		bare := &depedge.Policy{Module: p.Module, Layers: p.Layers}
		cases := []struct {
			name     string
			importer string
			imported string
			wantRule string
		}{
			{
				name:     "promotion domain importing transport (PROMOUX-011 history)",
				importer: mod + "/internal/domains/promotion",
				imported: mod + "/internal/transport/productquery",
				wantRule: wantUpwardRule,
			},
			{
				name:     "leave domain importing workitem",
				importer: mod + "/internal/domains/leave",
				imported: mod + "/internal/humanwork/workitem",
				wantRule: wantUpwardRule,
			},
			{
				name:     "workflow importing workitem",
				importer: mod + "/internal/workflow/execute",
				imported: mod + "/internal/humanwork/workitem",
				wantRule: wantUpwardRule,
			},
			{
				name:     "sandbox composer importing the application root",
				importer: mod + "/internal/platform/sandbox",
				imported: mod + "/internal/application",
				wantRule: wantUpwardRule,
			},
			{
				name:     "transport importing domains stays downward and allowed",
				importer: mod + "/internal/transport/journey",
				imported: mod + "/internal/domains/promotion",
				wantRule: "",
			},
			{
				name:     "equal-rank humanwork to transport stays allowed",
				importer: mod + "/internal/humanwork/journeyinvalidation",
				imported: mod + "/internal/transport/productquery",
				wantRule: "",
			},
			{
				name:     "unranked importer stays out of the spine policy",
				importer: mod + "/internal/intent/app",
				imported: mod + "/internal/domains/promotion",
				wantRule: "",
			},
			{
				name:     "unranked imported stays out of the spine policy",
				importer: mod + "/internal/domains/people",
				imported: mod + "/internal/intent/model",
				wantRule: "",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				v := bare.CheckEdge(tc.importer, tc.imported)
				if tc.wantRule == "" {
					if v != nil {
						t.Fatalf("CheckEdge(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
					}
					return
				}
				if v == nil {
					t.Fatalf("CheckEdge(%q, %q) = nil, want rule %q", tc.importer, tc.imported, tc.wantRule)
				}
				if v.Rule != tc.wantRule {
					t.Errorf("violation.Rule = %q, want %q", v.Rule, tc.wantRule)
				}
			})
		}
	})

	t.Run("named rules keep precedence over the generic rank rule", func(t *testing.T) {
		bare := &depedge.Policy{Module: p.Module, Layers: p.Layers}
		for _, tc := range []struct {
			importer, imported, want string
		}{
			{mod + "/internal/kernel/values", mod + "/internal/engines/transform", depedge.RuleKernelUpward},
			{mod + "/internal/engines/eligibility", mod + "/internal/domains/people/aggregate", depedge.RuleEngineImportsDomain},
		} {
			if v := bare.CheckEdge(tc.importer, tc.imported); v == nil || v.Rule != tc.want {
				t.Errorf("CheckEdge(%q, %q) = %v, want rule %q", tc.importer, tc.imported, v, tc.want)
			}
		}
	})

	t.Run("the generic rule is exported under its yaml name", func(t *testing.T) {
		if depedge.RuleLayerUpward != wantUpwardRule {
			t.Errorf("depedge.RuleLayerUpward = %q, want %q", depedge.RuleLayerUpward, wantUpwardRule)
		}
		found := false
		for _, r := range rev10102RuleNames(t) {
			if r == wantUpwardRule {
				found = true
			}
		}
		if !found {
			t.Errorf("policy yaml names no %q rule; the checker and the manifest must share one vocabulary", wantUpwardRule)
		}
	})

	t.Run("waivers are exactly the dated owned set", func(t *testing.T) {
		want := rev10102ExpectedExceptions()
		if len(p.Exceptions) != len(want) {
			t.Fatalf("policy carries %d exceptions, want exactly %d (%v)", len(p.Exceptions), len(want), want)
		}
		today := time.Now().Format("2006-01-02")
		seen := map[[2]string]bool{}
		for _, e := range p.Exceptions {
			seen[[2]string{e.Importer, e.Imported}] = true
			if e.Importer == "" || e.Imported == "" {
				t.Errorf("exception names an empty edge: %+v", e)
			}
			if strings.Contains(e.Importer, "://") || strings.HasPrefix(e.Importer, mod) {
				t.Errorf("exception importer %q must be module-relative", e.Importer)
			}
			if e.Owner == "" || e.Rationale == "" {
				t.Errorf("exception %s -> %s carries no owner/rationale", e.Importer, e.Imported)
			}
			if _, err := time.Parse("2006-01-02", e.Expiry); err != nil {
				t.Errorf("exception %s -> %s has unparsable expiry %q (want YYYY-MM-DD)", e.Importer, e.Imported, e.Expiry)
			} else if e.Expiry < today {
				t.Errorf("exception %s -> %s expired on %s: renew or remove it", e.Importer, e.Imported, e.Expiry)
			}
		}
		for _, w := range want {
			if !seen[w] {
				t.Errorf("expected waiver missing: %s -> %s", w[0], w[1])
			}
		}
	})
}

// rev10102RuleNames reads the rule vocabulary back from the loaded policy
// file. The Policy type does not retain rule entries (it implements them),
// so this re-reads the manifest's rules[].name list directly.
func rev10102RuleNames(t *testing.T) []string {
	t.Helper()
	root := repopath.RootDir()
	data, err := os.ReadFile(filepath.Join(root, "definitions", "architecture", "package-dependency-policy.yaml"))
	if err != nil {
		t.Fatalf("reading package-dependency-policy manifest: %v", err)
	}
	var names []string
	inRules := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "rules:") {
			inRules = true
			continue
		}
		if inRules && strings.HasPrefix(trimmed, "exceptions:") {
			break
		}
		if inRules && strings.HasPrefix(trimmed, "- name:") {
			names = append(names, strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:")))
		}
	}
	return names
}

// rev10102GoldenTable is the fixed edge set whose verdicts
// TestTodo_REV_101_02_Golden pins byte-exact. Module-relative paths keep
// the golden stable across renames of the module itself.
func rev10102GoldenTable() [][2]string {
	return [][2]string{
		{"internal/domains/leave", "internal/humanwork/workitem"},
		{"internal/domains/promotion", "internal/transport/productquery"},
		{"internal/humanwork/journeyinvalidation", "internal/transport/productquery"},
		{"internal/platform/sandbox", "internal/application"},
		{"internal/transport/journey", "internal/domains/promotion"},
		{"internal/workflow/execute", "internal/humanwork/workitem"},
		{"internal/workflow/simulate", "internal/platform/timeauth"},
	}
}

// TestTodo_REV_101_02_Golden pins the checker's exact verdict bytes over
// the fixed edge set so a future refactor cannot silently change what the
// gate rejects.
func TestTodo_REV_101_02_Golden(t *testing.T) {
	p := loadPolicy(t)
	bare := &depedge.Policy{Module: p.Module, Layers: p.Layers}
	var b strings.Builder
	for _, e := range rev10102GoldenTable() {
		v := bare.CheckEdge(p.Module+"/"+e[0], p.Module+"/"+e[1])
		if v == nil {
			b.WriteString(e[0] + " -> " + e[1] + ": OK\n")
		} else {
			b.WriteString(e[0] + " -> " + e[1] + ": " + v.Rule + "\n")
		}
	}
	goldenPath := filepath.Join(repopath.RootDir(), "tools", "policy", "depedge", "testdata", "rev101_02.golden.txt")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	if b.String() != string(want) {
		t.Errorf("checker verdicts mismatch:\n got:\n%s\nwant:\n%s", b.String(), string(want))
	}
}

// TestTodo_REV_101_02_Integration drives the real module graph: the live
// import graph carries zero unwaived violations, the fixed leave edge is
// gone, the promotion edge stays gone, and every waiver names an edge
// that actually exists (a waiver for a dead edge fails instead of
// lingering).
func TestTodo_REV_101_02_Integration(t *testing.T) {
	root := repopath.RootDir()
	layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		t.Fatalf("loading repository-layout manifest: %v", err)
	}
	policy, err := depedge.Load(filepath.Join(root, "definitions", "architecture", "package-dependency-policy.yaml"))
	if err != nil {
		t.Fatalf("loading package-dependency-policy manifest: %v", err)
	}
	graph, err := importgraph.Build(root, layoutManifest, policy)
	if err != nil {
		t.Fatalf("building import graph: %v", err)
	}
	if len(graph.Packages) == 0 {
		t.Fatal("import graph has no packages")
	}
	for _, v := range graph.PolicyViolations {
		t.Errorf("forbidden dependency edge: %s -> %s violates %s", v.Importer, v.Imported, v.Rule)
	}

	edges := map[[2]string]bool{}
	for _, e := range graph.Edges {
		edges[[2]string{e.Importer, e.Imported}] = true
	}
	present := func(importerRel, importedRel string) bool {
		return edges[[2]string{policy.Module + "/" + importerRel, policy.Module + "/" + importedRel}]
	}

	for _, fixed := range [][2]string{
		{"internal/domains/leave", "internal/humanwork/workitem"},
		{"internal/domains/promotion", "internal/transport/productquery"},
	} {
		if present(fixed[0], fixed[1]) {
			t.Errorf("live graph still carries the fixed edge %s -> %s", fixed[0], fixed[1])
		}
	}
	for _, w := range rev10102ExpectedExceptions() {
		if !present(w[0], w[1]) {
			t.Errorf("waiver %s -> %s names a dead edge: remove it instead of waiving it", w[0], w[1])
		}
	}
}
