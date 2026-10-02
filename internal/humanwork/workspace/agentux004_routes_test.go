package workspace

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// cspAdmitsPath reports whether a connect-src list lets a page on host fetch
// path: a source with a trailing slash admits everything beneath it, any other
// source admits exactly its own path.
func cspAdmitsPath(connect []string, host, path string) bool {
	for _, source := range connect {
		allowed, ok := strings.CutPrefix(source, host)
		if !ok || !strings.HasPrefix(allowed, "/") {
			continue
		}
		if allowed == path || strings.HasSuffix(allowed, "/") && strings.HasPrefix(path, allowed) {
			return true
		}
	}
	return false
}

// chatClientRoutes reads the same-origin routes the Chat client code names: the
// string literals that begin "/api/" in the browser build's Chat, agent and
// ambient files, with a query or fragment cut off, plus the route constants of
// the transports it calls.
func chatClientRoutes(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "tools", "uxqual", "cmd", "journeywasm")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{agentcontrols.Path: true, agentcontrols.Path + "/anything": true, personachat.Path: true, ambientagents.Path: true, ambientagents.Path + "/opt-out": true, personachat.Path + "/anything": true}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !strings.HasPrefix(name, "chat") && !strings.HasPrefix(name, "persona_chat") && !strings.HasPrefix(name, "agentux_ambient") && !strings.HasPrefix(name, "agent_controls") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			literal, ok := n.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil || !strings.HasPrefix(value, "/api/") {
				return true
			}
			if cut := strings.IndexAny(value, "?#"); cut >= 0 {
				value = value[:cut]
			}
			if value != "" && !strings.ContainsAny(value, " `{}") {
				seen[value] = true
			}
			return true
		})
	}
	routes := make([]string, 0, len(seen))
	for route := range seen {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
}

// The browser console on Chat stays free of policy violations because the page
// is only ever served a policy that admits every route the Chat client calls:
// each same-origin route its code names is admitted by the served policy, and
// none of them is admitted by widening the policy to the whole origin.
func TestTodo_AGENTUX_004_Browser(t *testing.T) {
	const host = "cell.test:8290"
	connect := web033ParseCSP(t, ProductContentSecurityPolicy(host))["connect-src"]
	routes := chatClientRoutes(t)
	if len(routes) < 10 {
		t.Fatalf("the scan found %d routes, it is not reading the client: %v", len(routes), routes)
	}
	for _, route := range routes {
		if !cspAdmitsPath(connect, host, route) {
			t.Errorf("the Chat client calls %s, which the served policy does not admit: %v", route, connect)
		}
	}
	for _, source := range connect {
		if source == "'self'" || source == host || strings.Contains(source, "*") {
			t.Errorf("the policy was widened to the whole origin: %s", source)
		}
	}
	// A route no client calls is not admitted.
	if cspAdmitsPath(connect, host, "/api/payroll/run") || cspAdmitsPath(connect, host, "/api/chatty") {
		t.Fatal("an unrelated route is admitted")
	}
}
