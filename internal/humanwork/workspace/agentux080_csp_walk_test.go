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
)

// agentux080ClientDirs are the packages whose code issues the page's own
// requests. A path they name that the document's connect-src does not admit is
// refused by the browser, and the surface that asked can only say it could not
// read, which is how the Agents pages lost their limits and cost reads.
var agentux080ClientDirs = []string{
	"../../../tools/uxqual/cmd/journeywasm",
	"../../../tools/uxqual/productclient",
	"../chatui",
	"../productui",
}

// agentux080NotFetched names string constants that begin like an API path but
// are never requested by the page: the integration snippet shows a curl line
// for an external caller to run elsewhere.
var agentux080NotFetched = map[string]string{
	"integration_curl.go": "/v1/conversations/",
}

// agentux080Source is one API path literal and where it was found.
type agentux080Source struct {
	path, where string
}

func agentux080Literals(t *testing.T) []agentux080Source {
	t.Helper()
	var found []agentux080Source
	seen := map[string]bool{}
	for _, dir := range agentux080ClientDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", dir, name, err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil || !(strings.HasPrefix(value, "/api/") || strings.HasPrefix(value, "/v1/")) {
					return true
				}
				if agentux080NotFetched[name] == value {
					return true
				}
				// The query string is not part of what connect-src matches.
				if cut := strings.IndexAny(value, "?#"); cut >= 0 {
					value = value[:cut]
				}
				key := value + "|" + filepath.Base(dir) + "/" + name
				if !seen[key] {
					seen[key] = true
					found = append(found, agentux080Source{path: value, where: filepath.Base(dir) + "/" + name})
				}
				return true
			})
		}
	}
	return found
}

// agentux080Admits applies the CSP path rule: a source path ending in a slash
// admits everything beneath it, any other source path admits only itself.
func agentux080Admits(connect []string, host, path string) bool {
	for _, source := range connect {
		source = strings.TrimPrefix(strings.TrimPrefix(source, "https://"), "http://")
		if !strings.HasPrefix(source, host+"/") {
			continue
		}
		allowed := strings.TrimPrefix(source, host)
		if strings.HasSuffix(allowed, "/") {
			if strings.HasPrefix(path, allowed) {
				return true
			}
		} else if path == allowed {
			return true
		}
	}
	return false
}

func TestTodo_AGENTUX_080(t *testing.T) {
	const host = "localhost:8290"
	connect := web033ParseCSP(t, ProductContentSecurityPolicy(host))["connect-src"]
	for _, source := range connect {
		if source == "'self'" || strings.Contains(source, "*") {
			t.Fatalf("connect-src admits more than exact prefixes: %s", source)
		}
	}

	literals := agentux080Literals(t)
	if len(literals) < 20 {
		t.Fatalf("walked only %d API path literals; the client directories moved or the walk broke", len(literals))
	}
	var missing []string
	agents := 0
	for _, lit := range literals {
		if strings.HasPrefix(lit.path, "/api/agent-") || strings.HasPrefix(lit.path, "/api/agents/") {
			agents++
		}
		if !agentux080Admits(connect, host, lit.path) {
			missing = append(missing, lit.path+"  ("+lit.where+")")
		}
	}
	if agents < 5 {
		t.Fatalf("walk found %d agent API literals, want the access, cost, task and control routes among them", agents)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("client code calls paths the product document's connect-src does not admit; the browser refuses them and the page shows a failed read:\n%s", strings.Join(missing, "\n"))
	}

	// The three prefixes this todo found refused on the live page, by name.
	for _, path := range []string{"/api/agent-access/v1", "/api/agent-cost/v1", "/api/agent-tasks/v1/extend-budget"} {
		if !agentux080Admits(connect, host, path) {
			t.Errorf("%s is not admitted by connect-src", path)
		}
	}
	// A path nobody asked for stays refused.
	if agentux080Admits(connect, host, "/api/unlisted/v1") {
		t.Error("connect-src admits a path no client code calls")
	}
}
