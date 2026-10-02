package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestIntegrate2AddressSelection(t *testing.T) {
	rooms := []chatui.Conversation{{ID: "direct", Name: "Policy Helper", Kind: chatui.DirectMessage}, {ID: "channel-id", Name: "general", Kind: chatui.PublicChannel}}
	for _, test := range []struct{ address, previous, want string }{{"general", "direct", "channel-id"}, {"direct", "channel-id", "direct"}, {"cannot-open", "direct", "cannot-open"}, {"", "direct", "direct"}, {"", "removed", "direct"}} {
		if got := integrate2AddressSelection(rooms, test.address, test.previous); got != test.want {
			t.Fatalf("address %q retained %q: got %q want %q", test.address, test.previous, got, test.want)
		}
	}
	if got := integrate2AddressSelection(nil, "denied", "direct"); got != "denied" {
		t.Fatal("unreadable address silently became a different conversation", got)
	}
}

func TestIntegrate2RetryRequiresPersonClick(t *testing.T) {
	for _, test := range []struct {
		trusted        bool
		action, id     string
		disabled, want bool
	}{{false, "retry", "invocation", false, false}, {true, "retry", "invocation", false, true}, {true, "retry", "", false, false}, {true, "load", "invocation", false, false}, {true, "retry", "invocation", true, false}} {
		if got := integrate2RetryAllowed(test.trusted, test.action, test.id, test.disabled); got != test.want {
			t.Fatalf("retry permission %+v: %v", test, got)
		}
	}
}

func TestIntegrate2RetryOnlyHasClickCallsite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		allowed := map[*ast.CallExpr]bool{}
		ast.Inspect(file, func(node ast.Node) bool {
			closure, ok := node.(*ast.FuncLit)
			if !ok {
				return true
			}
			trusted, selector, guard := false, false, false
			var retryCalls []*ast.CallExpr
			ast.Inspect(closure.Body, func(node ast.Node) bool {
				if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					value, _ := strconv.Unquote(literal.Value)
					trusted = trusted || value == "isTrusted"
					selector = selector || strings.Contains(value, "data-agent-action='retry'")
				}
				if call, ok := node.(*ast.CallExpr); ok {
					if function, ok := call.Fun.(*ast.Ident); ok {
						guard = guard || function.Name == "integrate2RetryAllowed"
						if function.Name == "retryPersonaChat" {
							retryCalls = append(retryCalls, call)
						}
					}
				}
				return true
			})
			if trusted && selector && guard {
				for _, call := range retryCalls {
					allowed[call] = true
				}
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			function, ok := call.Fun.(*ast.Ident)
			if !ok || function.Name != "retryPersonaChat" {
				return true
			}
			calls++
			if !allowed[call] {
				t.Errorf("retry outside guarded person click at %s", fset.Position(call.Pos()))
			}
			return true
		})
	}
	if calls != 1 {
		t.Fatalf("retry must have one click callsite, got %d", calls)
	}
}
