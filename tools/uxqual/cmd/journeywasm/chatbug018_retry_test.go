package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

type chatbug018Package struct {
	fset  *token.FileSet
	funcs map[string][]*ast.FuncDecl
	files map[*ast.FuncDecl]string
}

func chatbug018Load(t *testing.T) chatbug018Package {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	pkg := chatbug018Package{fset: token.NewFileSet(), funcs: map[string][]*ast.FuncDecl{}, files: map[*ast.FuncDecl]string{}}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Build tags are ignored on purpose: the browser files are the ones that matter.
		file, err := parser.ParseFile(pkg.fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				pkg.funcs[fn.Name.Name] = append(pkg.funcs[fn.Name.Name], fn)
				pkg.files[fn] = name
			}
		}
	}
	return pkg
}

func chatbug018String(n ast.Node) (string, bool) {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

// chatbug018IsClickHandler reports whether lit is the trusted-click closure of
// the Retry button: it checks isTrusted and names the retry selector.
func chatbug018IsClickHandler(lit *ast.FuncLit) bool {
	trusted, selector := false, false
	ast.Inspect(lit, func(n ast.Node) bool {
		if value, ok := chatbug018String(n); ok {
			if value == "isTrusted" {
				trusted = true
			}
			if strings.Contains(value, "data-agent-action='retry'") {
				selector = true
			}
		}
		return true
	})
	return trusted && selector
}

// TestTodo_CHATBUG_018 proves, from the client source, that the retry endpoint
// is reached from one place only: the Retry button's trusted click handler. It
// is not reachable from page load, the stream watch, a reconnect, or the replay
// of a failed invocation, because none of those functions (or anything they
// call) can reach retryPersonaChat or name the endpoint.
func TestTodo_CHATBUG_018(t *testing.T) {
	pkg := chatbug018Load(t)

	t.Run("the endpoint is named in one function", func(t *testing.T) {
		var owners []string
		for name, decls := range pkg.funcs {
			for _, fn := range decls {
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if value, ok := chatbug018String(n); ok && strings.HasSuffix(value, "/retry") {
						owners = append(owners, name+" ("+pkg.files[fn]+")")
					}
					return true
				})
			}
		}
		if len(owners) != 1 || owners[0] != "retryPersonaChat (persona_chat_wasm.go)" {
			t.Fatalf("the retry endpoint must be named only in retryPersonaChat, found %v", owners)
		}
	})

	t.Run("generic invocation actions never carry retry", func(t *testing.T) {
		calls := 0
		for name, decls := range pkg.funcs {
			if name == "personaChatInvocationAction" {
				continue // forwards its caller's action; every caller is checked here
			}
			for _, fn := range decls {
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					ident, ok := call.Fun.(*ast.Ident)
					if !ok || (ident.Name != "personaChatInvocationAction" && ident.Name != "personaChatInvocationRequest") {
						return true
					}
					calls++
					if len(call.Args) < 3 {
						t.Errorf("%s: unexpected call shape", pkg.fset.Position(call.Pos()))
						return true
					}
					action, literal := chatbug018String(call.Args[2])
					if !literal || strings.Contains(action, "retry") {
						t.Errorf("%s: invocation action must be a literal other than retry, got %q", pkg.fset.Position(call.Pos()), action)
					}
					return true
				})
			}
		}
		if calls == 0 {
			t.Fatal("found no invocation action calls; the test is looking at the wrong shape")
		}
	})

	t.Run("retryPersonaChat is called only from the trusted click handler", func(t *testing.T) {
		sites := 0
		for name, decls := range pkg.funcs {
			for _, fn := range decls {
				var stack []ast.Node
				ast.Inspect(fn, func(n ast.Node) bool {
					if n == nil {
						stack = stack[:len(stack)-1]
						return true
					}
					stack = append(stack, n)
					ident, isIdent := n.(*ast.Ident)
					if !isIdent || ident.Name != "retryPersonaChat" || ident == fn.Name {
						return true
					}
					sites++
					inHandler := false
					for _, parent := range stack {
						if lit, ok := parent.(*ast.FuncLit); ok && chatbug018IsClickHandler(lit) {
							inHandler = true
						}
					}
					if name != "configurePersonaChatBrowser" || !inHandler {
						t.Errorf("%s: retryPersonaChat used outside the Retry button's trusted click handler (in %s)", pkg.fset.Position(ident.Pos()), name)
					}
					return true
				})
			}
		}
		if sites != 1 {
			t.Fatalf("want exactly one use of retryPersonaChat, found %d", sites)
		}
	})

	t.Run("load, watch, reconnect and replay cannot reach it", func(t *testing.T) {
		roots := []string{"configurePersonaChatBrowser", "startPersonaChat", "restartPersonaChat", "watchPersonaChat", "failPersonaChatWatch", "tickPersonaChatElapsed", "refreshPersonaChatDirectory", "personaChatInvocations", "reconcileAgentPending", "bindAgentInvocationConversations", "chat5InvocationPosts", "personaWatchTerminal", "personaWatchBackoff"}
		seen := map[string]bool{}
		queue := append([]string(nil), roots...)
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			if seen[name] {
				continue
			}
			seen[name] = true
			decls, ok := pkg.funcs[name]
			if !ok {
				if chatbug018Contains(roots, name) {
					t.Fatalf("root %s no longer exists; update the test with its new name", name)
				}
				continue
			}
			for _, fn := range decls {
				var walk func(n ast.Node) bool
				walk = func(n ast.Node) bool {
					if lit, ok := n.(*ast.FuncLit); ok && chatbug018IsClickHandler(lit) {
						return false // a click closure only runs on a person's click
					}
					if ident, ok := n.(*ast.Ident); ok {
						if _, known := pkg.funcs[ident.Name]; known {
							queue = append(queue, ident.Name)
						}
					}
					return true
				}
				ast.Inspect(fn.Body, walk)
			}
		}
		if seen["retryPersonaChat"] {
			t.Fatal("retryPersonaChat is reachable from page load, the stream watch, a reconnect or a replayed failed invocation")
		}
		if !seen["watchPersonaChat"] || !seen["failPersonaChatWatch"] {
			t.Fatal("the walk did not cover the watch stream")
		}
	})
}

func chatbug018Contains(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}
