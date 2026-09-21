package archrules_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// transportHandlerStatementBudget is the ARCH-GO-023 statement budget: an RPC
// handler method above this many statements is doing more than admit,
// forward and encode, which is this package's whole GREEN condition. The
// real tree's largest handler as of this todo (internal/transport/admin's
// ExplainTransaction, ~34 statements once nested blocks are counted) sits
// comfortably under it; a handler that grows past the budget either belongs
// in a named exception below (with the todo that owns the extra work) or
// needs to push logic down into the domain/engine it forwards to.
const transportHandlerStatementBudget = 60

// TestTransportRejectsBusinessAndPersistenceImports is the ARCH-GO-023
// primary test. It exercises archrules.TransportForbiddenImport and
// archrules.ScanTransportSource against synthetic sources so RED is
// reachable without depending on the live tree's current shape.
func TestTransportRejectsBusinessAndPersistenceImports(t *testing.T) {
	t.Run("forbidden import roots", func(t *testing.T) {
		cases := []struct {
			imported string
			want     bool
		}{
			{"internal/domains/people", true},
			{"internal/domains", true},
			{"internal/engines/wire", true},
			{"internal/data/postgres", true},
			{"internal/data", true},
			// a sibling that merely starts with the same prefix is not under it
			{"internal/domainstuff", false},
			{"internal/kernel/values", false},
			{"internal/transport/envelope", false},
			{"internal/capability", false},
		}
		for _, tc := range cases {
			if got := archrules.TransportForbiddenImport(tc.imported); got != tc.want {
				t.Errorf("TransportForbiddenImport(%q) = %v, want %v", tc.imported, got, tc.want)
			}
		}
	})

	t.Run("business logic import without a declared port is rejected", func(t *testing.T) {
		const src = `package widget

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

func (s *server) GetWorker(ctx context.Context, req *WidgetRequest) (*WidgetResponse, error) {
	_, _ = people.ExplainWorkerState(ctx, nil, people.ExplainWorkerStateRequest{})
	return nil, nil
}
`
		file, err := parseImports(t, "widget.go", src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		found := false
		for _, imp := range file {
			if archrules.TransportForbiddenImport(imp) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a forbidden import to be detected in the synthetic handler source")
		}
	})

	t.Run("declared port import is not a violation", func(t *testing.T) {
		const src = `package widget

import (
	"context"

	widgetv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/widget/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

func (s *server) GetWidget(ctx context.Context, req *widgetv1.GetWidgetRequest) (*widgetv1.GetWidgetResponse, error) {
	_ = transport.IntentHandler(nil)
	return nil, nil
}
`
		file, err := parseImports(t, "widget.go", src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		for _, imp := range file {
			if archrules.TransportForbiddenImport(imp) {
				t.Errorf("import %q should not be flagged: transport.IntentHandler is the declared port", imp)
			}
		}
	})

	t.Run("embedded SQL string is rejected", func(t *testing.T) {
		const src = `package widget

const query = "SELECT id, name FROM widgets WHERE tenant_id = $1"
`
		_, sqlFindings, _, err := archrules.ScanTransportSource("internal/transport/widget", "widget.go", src)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(sqlFindings) != 1 {
			t.Fatalf("expected 1 SQL finding, got %d: %+v", len(sqlFindings), sqlFindings)
		}
	})

	t.Run("a single redaction-marker word is not an embedded SQL statement", func(t *testing.T) {
		const src = `package widget

var unsafeMarkers = []string{"select ", "insert into", "delete from", "update set", "pgx", "pq:"}
`
		_, sqlFindings, _, err := archrules.ScanTransportSource("internal/transport/widget", "widget.go", src)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(sqlFindings) != 0 {
			t.Fatalf("expected 0 SQL findings for standalone marker words, got %d: %+v", len(sqlFindings), sqlFindings)
		}
	})

	t.Run("direct pool import is rejected", func(t *testing.T) {
		const src = `package widget

import "github.com/jackc/pgx/v5/pgxpool"

func lookup(p *pgxpool.Pool) {}
`
		_, _, poolFindings, err := archrules.ScanTransportSource("internal/transport/widget", "widget.go", src)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(poolFindings) == 0 {
			t.Fatalf("expected a pool-use finding for a direct pgxpool import")
		}
	})

	t.Run("database/sql import is rejected", func(t *testing.T) {
		const src = `package widget

import "database/sql"

func lookup(db *sql.DB) {}
`
		_, _, poolFindings, err := archrules.ScanTransportSource("internal/transport/widget", "widget.go", src)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(poolFindings) == 0 {
			t.Fatalf("expected a pool-use finding for a direct database/sql import")
		}
	})

	t.Run("a handler body under budget is not flagged", func(t *testing.T) {
		const src = `package widget

import "context"

func (s *server) GetWidget(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, nil
	}
	return &Response{}, nil
}
`
		handlers, _, _, err := archrules.ScanTransportSource("internal/transport/widget", "widget.go", src)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(handlers) != 1 {
			t.Fatalf("expected exactly 1 handler finding, got %d: %+v", len(handlers), handlers)
		}
		if handlers[0].Statements > transportHandlerStatementBudget {
			t.Fatalf("small handler unexpectedly exceeds the budget: %+v", handlers[0])
		}
	})

	t.Run("a handler body over budget is flagged", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("package widget\n\nimport \"context\"\n\nfunc (s *server) GetWidget(ctx context.Context, req *Request) (*Response, error) {\n")
		for i := 0; i < transportHandlerStatementBudget+10; i++ {
			b.WriteString("\t_ = 1\n")
		}
		b.WriteString("\treturn nil, nil\n}\n")

		handlers, _, _, err := archrules.ScanTransportSource("internal/transport/widget", "widget.go", b.String())
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(handlers) != 1 {
			t.Fatalf("expected exactly 1 handler finding, got %d: %+v", len(handlers), handlers)
		}
		if handlers[0].Statements <= transportHandlerStatementBudget {
			t.Fatalf("oversized handler should exceed the budget: %+v", handlers[0])
		}
	})

	t.Run("a plain function is not mistaken for an RPC handler", func(t *testing.T) {
		const src = `package widget

import "context"

func helper(ctx context.Context, x int) int { return x }

func (s *server) notExported(ctx context.Context, req *Request) (*Response, error) { return nil, nil }
`
		handlers, _, _, err := archrules.ScanTransportSource("internal/transport/widget", "widget.go", src)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(handlers) != 0 {
			t.Fatalf("expected 0 handler findings (free function and unexported method), got %d: %+v", len(handlers), handlers)
		}
	})
}

// transportAdminBusinessImportAllowlist is the ARCH-GO-023 exception list: a
// real importer->imported edge from a transport package into
// internal/domains/* that the checker would otherwise reject. Every entry
// must name the owning todo and the reviewed reason it exists.
//
// internal/transport/admin (semantic owner experience-and-transport, todos
// SVC-011/ADMIN-001 per its own doc.go) calls intelligence.ExplainTransaction
// and people.ExplainWorkerState directly rather than through a declared
// transport port interface. Both are documented "governed, read-only
// passthrough" functions (see explain_transaction.go, worker_state.go): they
// perform no write and delegate every authorization/redaction decision to
// internal/operations/admin and the domain function itself. This is a real,
// reviewed exception to ARCH-GO-023's GREEN condition, not an oversight;
// ARCH-GO-023 owns closing it by giving admin its own narrow read port
// (mirroring transport.IntentHandler) instead of calling the domain package
// by name.
type transportImportException struct {
	OwnerTodo string
	Reason    string
}

var transportAdminBusinessImportAllowlist = map[string]transportImportException{
	"internal/transport/admin->internal/data/ledger": {
		OwnerTodo: "REV-037-01",
		Reason:    "operator ledger explorer receives the composed read store",
	},
	"internal/transport/admin->internal/data/ledger/hashchain": {
		OwnerTodo: "REV-037-01",
		Reason:    "operator ledger verification reports the canonical hash-chain result",
	},
	"internal/transport/admin->internal/domains/intelligence": {
		OwnerTodo: "ARCH-GO-023",
		Reason:    "SVC-011/ADMIN-001 governed read-only passthrough",
	},
	"internal/transport/admin->internal/domains/people": {
		OwnerTodo: "ARCH-GO-023",
		Reason:    "SVC-011/ADMIN-001 governed read-only passthrough",
	},
	"internal/transport/cell->internal/data/workflowdraftstore": {
		OwnerTodo: "WF-UI-004",
		Reason:    "cell is the composition root that constructs the workflow draft adapter",
	},
	"internal/transport/journey->internal/domains/promotion": {
		OwnerTodo: "REV-091-02",
		Reason:    "journey projection maps the governed promotion review value without performing domain work",
	},
}

// transportHandlerStatementBudgetAllowlist is the ARCH-GO-023 exception list
// for a handler method whose statement count exceeds
// transportHandlerStatementBudget. Empty as of this todo: the real tree's
// handlers all fit the budget (see TestTodo_ARCH_GO_023_Integration's logged
// counts). Add an entry only with the owning todo and a reviewed reason.
var transportHandlerStatementBudgetAllowlist = map[string]bool{}

// TestTodo_ARCH_GO_023_Golden pins the ARCH-GO-023 exception lists
// themselves: the checker's job is to make any addition to either allowlist
// a visible, reviewed diff, so the lists' own contents are the golden
// artifact.
func TestTodo_ARCH_GO_023_Golden(t *testing.T) {
	wantImportEdges := []string{
		"internal/transport/admin->internal/data/ledger",
		"internal/transport/admin->internal/data/ledger/hashchain",
		"internal/transport/admin->internal/domains/intelligence",
		"internal/transport/admin->internal/domains/people",
		"internal/transport/cell->internal/data/workflowdraftstore",
		"internal/transport/journey->internal/domains/promotion",
	}
	var gotImportEdges []string
	for edge := range transportAdminBusinessImportAllowlist {
		gotImportEdges = append(gotImportEdges, edge)
	}
	sort.Strings(gotImportEdges)
	sort.Strings(wantImportEdges)
	if strings.Join(gotImportEdges, ",") != strings.Join(wantImportEdges, ",") {
		t.Fatalf("ARCH-GO-023 import allowlist drifted from the pinned set\n got:  %v\n want: %v", gotImportEdges, wantImportEdges)
	}
	if len(transportHandlerStatementBudgetAllowlist) != 0 {
		t.Fatalf("ARCH-GO-023 statement-budget allowlist should be empty as pinned; got %v", transportHandlerStatementBudgetAllowlist)
	}
	if transportHandlerStatementBudget != 60 {
		t.Fatalf("ARCH-GO-023 statement budget drifted from the pinned value 60: got %d", transportHandlerStatementBudget)
	}
}

// TestTodo_ARCH_GO_023_Integration runs every ARCH-GO-023 check against the
// real tree: the import graph (via `go list -json`) for business/persistence
// imports outside declared ports, and a file-level AST scan of every
// internal/transport/**/*.go source for embedded SQL strings, direct
// pool/connection references and oversized RPC handler bodies.
func TestTodo_ARCH_GO_023_Integration(t *testing.T) {
	const (
		module        = "github.com/monstercameron/human-capital-management-suite"
		transportRoot = "internal/transport"
	)

	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var importViolations int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(module, pkg.ImportPath)
		if !ok || !archrules.UnderRoot(rel, transportRoot) {
			continue
		}
		for _, imp := range pkg.Imports {
			impRel, ok := archrules.TrimModule(module, imp)
			if !ok {
				continue
			}
			if !archrules.TransportForbiddenImport(impRel) {
				continue
			}
			// The allowlist keys on the exact imported package (e.g.
			// internal/domains/people), the same edge shape
			// TestTodo_ARCH_GO_022_Integration uses, so review sees precisely
			// which domain/engine/data package a transport package reaches.
			edge := rel + "->" + impRel
			if _, ok := transportAdminBusinessImportAllowlist[edge]; ok {
				continue
			}
			importViolations++
			t.Errorf("ARCH-GO-023 violation: %s imports %s directly (not through a declared port); add a reviewed allowlist entry naming the owning todo, or remove the import", rel, impRel)
		}
	}

	transportDir := filepath.Join(root, filepath.FromSlash(transportRoot))
	var (
		sqlViolations     int
		poolViolations    int
		handlerViolations int
		maxStatements     int
		maxStatementsAt   string
	)
	err = filepath.WalkDir(transportDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		relDir, rerr := filepath.Rel(root, filepath.Dir(path))
		if rerr != nil {
			return rerr
		}
		pkgRel := filepath.ToSlash(relDir)

		handlers, sqlFindings, poolFindings, serr := archrules.ScanTransportSource(pkgRel, name, string(data))
		if serr != nil {
			return serr
		}
		for _, f := range sqlFindings {
			sqlViolations++
			t.Errorf("ARCH-GO-023 violation: %s/%s embeds a SQL-shaped string literal %q; transport must never own SQL", f.Package, f.File, f.Literal)
		}
		for _, f := range poolFindings {
			poolViolations++
			t.Errorf("ARCH-GO-023 violation: %s/%s %s; transport must never hold a concrete pool/connection handle", f.Package, f.File, f.Detail)
		}
		for _, h := range handlers {
			if h.Statements > maxStatements {
				maxStatements = h.Statements
				maxStatementsAt = h.Package + "/" + h.File + ":" + h.Func
			}
			if h.Statements <= transportHandlerStatementBudget {
				continue
			}
			key := h.Package + "." + h.Func
			if transportHandlerStatementBudgetAllowlist[key] {
				continue
			}
			handlerViolations++
			t.Errorf("ARCH-GO-023 violation: %s.%s has %d statements, over the %d-statement budget; add a reviewed allowlist entry naming the owning todo, or shrink the handler", h.Package, h.Func, h.Statements, transportHandlerStatementBudget)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", transportDir, err)
	}

	t.Logf("ARCH-GO-023: %d import violations (+ %d allowlisted edges), %d SQL-string violations, %d pool-use violations, %d oversized-handler violations; largest handler seen: %s (%d statements, budget %d)",
		importViolations, len(transportAdminBusinessImportAllowlist), sqlViolations, poolViolations, handlerViolations, maxStatementsAt, maxStatements, transportHandlerStatementBudget)
}

// TestTodo_ARCH_GO_023_Conformance checks that the checker's own inputs are
// internally consistent: every allowlisted import edge actually starts under
// internal/transport and ends under one of TransportForbiddenRoots (an
// allowlist entry that does not even match the shape the checker looks for
// would silently protect nothing), and the statement budget is a sane
// positive number.
func TestTodo_ARCH_GO_023_Conformance(t *testing.T) {
	if transportHandlerStatementBudget <= 0 {
		t.Fatalf("statement budget must be positive, got %d", transportHandlerStatementBudget)
	}
	for edge := range transportAdminBusinessImportAllowlist {
		parts := strings.SplitN(edge, "->", 2)
		if len(parts) != 2 {
			t.Fatalf("malformed allowlist edge %q: want \"importer->imported\"", edge)
		}
		importer, imported := parts[0], parts[1]
		if !archrules.UnderRoot(importer, "internal/transport") {
			t.Errorf("allowlisted importer %q is not under internal/transport", importer)
		}
		if !archrules.TransportForbiddenImport(imported) {
			t.Errorf("allowlisted imported package %q is not under a forbidden root; the entry protects nothing", imported)
		}
		exception := transportAdminBusinessImportAllowlist[edge]
		if exception.OwnerTodo == "" || exception.Reason == "" {
			t.Errorf("allowlisted edge %q must name its owning todo and reviewed reason", edge)
		}
	}
	for _, r := range archrules.TransportForbiddenRoots {
		if strings.TrimSpace(r) == "" {
			t.Fatalf("TransportForbiddenRoots contains an empty entry")
		}
	}
}

// parseImports is a small test helper: it extracts the module-relative
// import paths from src, so the primary test can assert on
// archrules.TransportForbiddenImport against a synthetic handler file
// without duplicating repopath's real `go list` graph.
func parseImports(t *testing.T, filename, src string) ([]string, error) {
	t.Helper()
	const module = "github.com/monstercameron/human-capital-management-suite/"

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var imports []string
	for _, imp := range file.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil {
			continue
		}
		if strings.HasPrefix(path, module) {
			imports = append(imports, strings.TrimPrefix(path, module))
		}
	}
	return imports, nil
}
