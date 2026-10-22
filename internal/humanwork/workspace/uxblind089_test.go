package workspace

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTodo_UXBLIND_089 pins the loader half of the cold-load fix. The bundle
// is fetched and compiled as soon as the manifest answers, alongside the
// runtime shim, instead of after the shim has been fetched and imported; only
// instantiation waits for the shim. Measured before the fix, the serial chain
// left the bundle request idle behind the shim import for up to 1.3 s in the
// browser pane. Every loader phase is marked so the budget can be measured.
func TestTodo_UXBLIND_089(t *testing.T) {
	source := journeyLoaderSource
	compile := strings.Index(source, `window.WebAssembly.compileStreaming(k("`+PathJourneyWasm+`"`)
	shim := strings.Index(source, `k("`+PathWasmExec+`"`)
	importShim := strings.Index(source, "import(u)")
	instantiate := strings.Index(source, "window.WebAssembly.instantiate(b,g.importObject)")
	if compile < 0 || shim < 0 || importShim < 0 || instantiate < 0 {
		t.Fatalf("loader is missing a stage: compile=%d shim=%d import=%d instantiate=%d", compile, shim, importShim, instantiate)
	}
	if compile > shim {
		t.Fatalf("the bundle compile starts after the runtime shim request (compile at %d, shim at %d); the two must overlap", compile, shim)
	}
	if !(importShim < instantiate) {
		t.Fatal("instantiation must wait for the shim's import object")
	}
	if strings.Contains(source, "instantiateStreaming(") {
		t.Fatal("the loader still streams the bundle only after the shim has loaded")
	}
	// A compile failure is observed by the chain, not reported unhandled.
	if !strings.Contains(source, "w.catch(function(){})") || !strings.Contains(source, "return w.then(") {
		t.Fatal("the compile promise is not both observed and awaited")
	}
	// Integrity and authentication still guard the bundle bytes.
	if !strings.Contains(source, `v(a),a.integrity)`) {
		t.Fatal("the bundle compile dropped its subresource integrity pin")
	}
	last := -1
	for _, phase := range ColdStartLoaderPhases() {
		at := strings.Index(source, `p("`+phase+`")`)
		if at < 0 {
			t.Fatalf("loader does not mark phase %q", phase)
		}
		if at < last {
			t.Fatalf("loader marks phase %q out of order", phase)
		}
		last = at
	}
	if !strings.Contains(source, `var h="`+ColdStartMarkPrefix+`";function p(n){try{performance.mark(h+n)}`) {
		t.Fatalf("loader marks are not under the %q prefix", ColdStartMarkPrefix)
	}
}

// TestTodo_UXBLIND_089_Browser ties the Go matrix to the real-browser proof:
// the served product shell carries the marked, overlapped loader under a
// policy that allows it, and the live Playwright spec enforces the same
// budget and phase names this package declares.
func TestTodo_UXBLIND_089_Browser(t *testing.T) {
	doc, err := productShellDocument(JourneyConfig{TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token", Roles: []string{}, JourneysPath: PathJourney}, true)
	if err != nil {
		t.Fatalf("productShellDocument: %v", err)
	}
	if !strings.Contains(doc, "<script>"+journeyLoaderSource+"</script>") {
		t.Fatal("the product shell does not serve the cold-start loader")
	}
	policy := ProductContentSecurityPolicy("cell.test")
	if !strings.Contains(policy, "'"+sha256Source(journeyLoaderSource)+"'") || !strings.Contains(policy, "'wasm-unsafe-eval'") {
		t.Fatalf("the product policy does not allow the loader to compile the bundle: %s", policy)
	}

	specPath := filepath.Join("..", "..", "..", "tools", "uxqual", "browser", "uxblind089_cold_load.spec.mjs")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read live spec: %v", err)
	}
	spec := string(raw)
	budget := regexp.MustCompile(`const COLD_CONTENT_BUDGET_MS = (\d+);`).FindStringSubmatch(spec)
	if budget == nil {
		t.Fatal("the live spec declares no cold-content budget")
	}
	ms, _ := strconv.Atoi(budget[1])
	if time.Duration(ms)*time.Millisecond != ColdContentBudget {
		t.Fatalf("live spec budget %d ms, Go budget %s", ms, ColdContentBudget)
	}
	if ColdContentBudget > 3*time.Second {
		t.Fatalf("cold-content budget %s exceeds the 3 s the todo requires", ColdContentBudget)
	}
	for name, phases := range map[string][]string{"LOADER_PHASES": ColdStartLoaderPhases(), "CLIENT_PHASES": ColdStartClientPhases()} {
		if !jsStringArrayDeclarationMatches(spec, name, phases) {
			t.Errorf("live spec %s differs from the Go declaration; want ordered phases %s", name, strings.Join(phases, ", "))
		}
	}
	if !strings.Contains(spec, "browser.newContext()") {
		t.Error("the live spec does not start from an empty cache")
	}
}

func jsStringArrayDeclarationMatches(source, name string, values []string) bool {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = `"` + regexp.QuoteMeta(value) + `"`
	}
	pattern := `const\s+` + regexp.QuoteMeta(name) + `\s*=\s*\[\s*` + strings.Join(quoted, `\s*,\s*`) + `\s*,?\s*\]\s*;`
	return regexp.MustCompile(pattern).MatchString(source)
}

func TestUXBLIND089PhaseDeclarationMatchesFormattedArray(t *testing.T) {
	want := []string{"go-main", "dial", "hydrate-start"}
	tests := []struct {
		name   string
		source string
		match  bool
	}{
		{
			name:   "single-line array",
			source: `const CLIENT_PHASES = ["go-main", "dial", "hydrate-start"];`,
			match:  true,
		},
		{
			name: "Prettier multiline array with trailing comma",
			source: `const CLIENT_PHASES = [
  "go-main",
  "dial",
  "hydrate-start",
];`,
			match: true,
		},
		{
			name:   "missing phase",
			source: `const CLIENT_PHASES = ["go-main", "dial"];`,
		},
		{
			name:   "reordered phases",
			source: `const CLIENT_PHASES = ["dial", "go-main", "hydrate-start"];`,
		},
		{
			name:   "duplicate phase",
			source: `const CLIENT_PHASES = ["go-main", "dial", "dial"];`,
		},
		{
			name:   "whitespace inside phase name",
			source: `const CLIENT_PHASES = ["go-main", "di al", "hydrate-start"];`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsStringArrayDeclarationMatches(tt.source, "CLIENT_PHASES", want); got != tt.match {
				t.Errorf("jsStringArrayDeclarationMatches() = %t, want %t", got, tt.match)
			}
		})
	}
}
