package productui

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// clientForbiddenFamilies are the server-side package families the browser
// client must not link, each with everything below it. They arrived through a
// handful of type references (a plan type, an inspector view, a mutation
// interface) and, with their own imports, put the workflow engine, the
// transaction coordinator, the ledger and outbox with their PostgreSQL driver
// types, tenancy with its YAML reader and the intent and governance engines
// into a module that cannot run any of them.
var clientForbiddenFamilies = []string{
	"internal/data",
	"internal/ledger",
	"internal/transaction",
	"internal/workflow/inspect",
	"internal/workflow/execute",
	"internal/workflow/runtime",
	"internal/workflow/timer",
	"internal/workflow/lease",
	"internal/workflow/observe",
	"internal/workflow/cancellation",
	"internal/workflow/workload",
	"internal/workflow/version",
	"internal/workflow/testprofile",
	"internal/workflow/steps/task",
	"internal/workflow/steps/approval",
	"internal/intent",
	"internal/governance",
	"internal/documents",
	"internal/documentsecurity",
	"internal/platform",
	"internal/operations/reconcile",
	"internal/operations/repair",
	"internal/operations/export",
	"internal/domains/repair",
	"internal/domains/dataops",
	"internal/domains/tenant",
	"internal/domains/audience",
	"internal/engines/schedule",
	"internal/engines/replan",
	"internal/engines/fielddiff",
	"internal/engines/population",
	"internal/messaging",
	// The machine-token verifier carries the ECDSA and RSA key types; only the
	// server verifies those tokens (internal/trust/served_verifier.go).
	"internal/trust/machine",
	"internal/forms",
	"internal/connectivity/observe",
	"github.com/jackc/pgx",
	"gopkg.in/yaml",
}

// clientAllowedInFamilies are the small leaf packages inside a forbidden family
// that the client does use: ports and value types with no engine behind them.
var clientAllowedInFamilies = []string{
	"internal/data/dbport",
	"internal/data/pageledger",
	"internal/data/demoworkforce/unitnames",
	"internal/intent/lifecycle",
	"internal/governance/candidate",
}

func forbiddenInClient(path string) bool {
	for _, allowed := range clientAllowedInFamilies {
		if path == allowed {
			return false
		}
	}
	for _, family := range clientForbiddenFamilies {
		if path == family || strings.HasPrefix(path, family+"/") {
			return true
		}
	}
	return false
}

// assertNoServerFamiliesInClient lists the package's dependencies as the
// browser build sees them and reports any forbidden package with the packages
// that import it, so the next edit knows which import to cut.
func assertNoServerFamiliesInClient(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("lists the js/wasm build graph")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}
	cmd := exec.Command(goBin, "list", "-deps", "-tags=grpcnotrace,nethttpomithttp2", "-f", `{{.ImportPath}} {{join .Imports " "}}`, ".")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps (js/wasm): %v\n%s", err, out)
	}
	const module = "github.com/monstercameron/human-capital-management-suite/"
	importers := map[string][]string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for _, imported := range fields[min(1, len(fields)):] {
			importers[strings.TrimPrefix(imported, module)] = append(importers[strings.TrimPrefix(imported, module)], strings.TrimPrefix(fields[0], module))
		}
	}
	if len(importers) < 50 {
		t.Fatalf("read %d imports; the build graph was not read", len(importers))
	}
	var linked []string
	for path := range importers {
		if forbiddenInClient(path) {
			linked = append(linked, path)
		}
	}
	sort.Strings(linked)
	for _, path := range linked {
		by := importers[path]
		sort.Strings(by)
		t.Errorf("the browser client links %s (imported by %s)", path, strings.Join(by, ", "))
	}
}
