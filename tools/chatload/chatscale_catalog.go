// Package chatload measures the existing chat schema in disposable test databases.
package chatload

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Query struct {
	headSQL    string
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Function   string   `json:"function"`
	SQL        string   `json:"sql"`
	Arguments  []string `json:"arguments"`
	Unresolved string   `json:"unresolved,omitempty"`
}

// Inventory retains unresolved call sites rather than silently omitting SQL.
// It resolves package constants and all locally assigned string alternatives.
func Inventory(root string) ([]Query, error) {
	files, err := filepath.Glob(filepath.Join(root, "internal/data/chatstore/*.go"))
	if err != nil {
		return nil, err
	}
	fs := token.NewFileSet()
	parsed := []*ast.File{}
	globals := map[string][]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, e := parser.ParseFile(fs, name, nil, 0)
		if e != nil {
			return nil, e
		}
		parsed = append(parsed, f)
	}
	for pass := 0; pass < 8; pass++ {
		for _, f := range parsed {
			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, s := range g.Specs {
					v, ok := s.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, n := range v.Names {
						if i < len(v.Values) {
							globals[n.Name] = resolve(v.Values[i], globals)
							if n.Name == "seedPurgeOrder" {
								if composite, ok := v.Values[i].(*ast.CompositeLit); ok {
									for _, element := range composite.Elts {
										if row, ok := element.(*ast.CompositeLit); ok && len(row.Elts) == 2 {
											a, b := resolve(row.Elts[0], globals), resolve(row.Elts[1], globals)
											if len(a) == 1 && len(b) == 1 {
												globals["seedPurgeOrder.rows"] = unique(append(globals["seedPurgeOrder.rows"], a[0]+"|"+b[0]))
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	for pass := 0; pass < 3; pass++ {
		for _, f := range parsed {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				local := map[string][]string{}
				for k, v := range globals {
					local[k] = v
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					a, ok := n.(*ast.AssignStmt)
					if ok {
						for i, l := range a.Lhs {
							if i < len(a.Rhs) {
								if id, ok := l.(*ast.Ident); ok {
									local[id.Name] = resolve(a.Rhs[i], local)
								}
							}
						}
					}
					return true
				})
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if _, ok := n.(*ast.FuncLit); ok {
						return false
					}
					ret, ok := n.(*ast.ReturnStmt)
					if ok && len(ret.Results) > 0 {
						for _, value := range resolve(ret.Results[0], local) {
							if value != "" {
								globals[fn.Name.Name+"()"] = unique(append(globals[fn.Name.Name+"()"], value))
							}
						}
					}
					return true
				})
			}
		}
	}
	var out []Query
	for _, f := range parsed {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			env := map[string][]string{}
			for k, v := range globals {
				env[k] = v
			}
			// Resolve assignments once in source order; retain branch alternatives.
			for pass := 0; pass < 1; pass++ {
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					a, ok := n.(*ast.AssignStmt)
					if !ok {
						return true
					}
					for i, l := range a.Lhs {
						if i >= len(a.Rhs) {
							continue
						}
						id, ok := l.(*ast.Ident)
						if ok {
							values := resolve(a.Rhs[i], env)
							if a.Tok == token.ADD_ASSIGN {
								values = combine(env[id.Name], values)
							}
							env[id.Name] = unique(append(env[id.Name], values...))
						}
					}
					return true
				})
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := c.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				idx := 1
				switch sel.Sel.Name {
				case "Query", "QueryRow", "Exec":
				case "execTenant":
					idx = 2
				default:
					return true
				}
				if len(c.Args) <= idx {
					return true
				}
				p := fs.Position(c.Pos())
				rel, _ := filepath.Rel(root, p.Filename)
				q := Query{File: filepath.ToSlash(rel), Line: p.Line, Function: fn.Name.Name}
				for _, a := range c.Args[idx+1:] {
					if id, ok := a.(*ast.Ident); ok && c.Ellipsis != token.NoPos {
						var exprs []string
						ast.Inspect(fn.Body, func(n ast.Node) bool {
							assignment, ok := n.(*ast.AssignStmt)
							if !ok {
								return true
							}
							for i, l := range assignment.Lhs {
								if ident, ok := l.(*ast.Ident); ok && ident.Name == id.Name && i < len(assignment.Rhs) {
									switch rhs := assignment.Rhs[i].(type) {
									case *ast.CompositeLit:
										for _, v := range rhs.Elts {
											exprs = append(exprs, expression(fs, v))
										}
									case *ast.CallExpr:
										if fun, ok := rhs.Fun.(*ast.Ident); ok && fun.Name == "append" {
											for _, v := range rhs.Args[1:] {
												exprs = append(exprs, expression(fs, v))
											}
										}
									}
								}
							}
							return true
						})
						if len(exprs) > 0 {
							q.Arguments = append(q.Arguments, exprs...)
							continue
						}
					}
					q.Arguments = append(q.Arguments, expression(fs, a))
				}
				sqls := resolve(c.Args[idx], env)
				if fn.Name.Name == "execTenant" {
					return true
				} // SQL is catalogued at each caller.
				if fn.Name.Name == "PurgeConversations" {
					for _, row := range globals["seedPurgeOrder.rows"] {
						parts := strings.Split(row, "|")
						if parts[1] == "post_id_in_conversation" {
							sqls = append(sqls, fmt.Sprintf("DELETE FROM %s WHERE tenant_id=$1 AND post_id IN (SELECT id FROM chat_post WHERE tenant_id=$1 AND conversation_id=ANY($2))", parts[0]))
						} else {
							sqls = append(sqls, fmt.Sprintf("DELETE FROM %s WHERE tenant_id=$1 AND %s=ANY($2)", parts[0], parts[1]))
						}
					}
				}
				if len(sqls) == 0 {
					q.Unresolved = expression(fs, c.Args[idx])
					out = append(out, q)
				} else {
					for _, sql := range sqls {
						if !possibleSQL(sql) {
							continue
						}
						q.SQL = sql
						out = append(out, q)
					}
				}
				return true
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].SQL < out[j].SQL
	})
	return out, nil
}
func expression(fs *token.FileSet, e ast.Expr) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, fs, e)
	return b.String()
}
func resolve(e ast.Expr, env map[string][]string) []string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			s, err := strconv.Unquote(x.Value)
			if err == nil {
				return []string{s}
			}
		}
	case *ast.Ident:
		return env[x.Name]
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			var out []string
			for _, a := range resolve(x.X, env) {
				for _, b := range resolve(x.Y, env) {
					if len(out) < 64 {
						out = append(out, a+b)
					}
				}
			}
			return unique(out)
		}
	case *ast.FuncLit:
		local := map[string][]string{}
		for k, v := range env {
			local[k] = v
		}
		i := 0
		for _, f := range x.Type.Params.List {
			for _, n := range f.Names {
				local[n.Name] = []string{fmt.Sprintf("@arg%d@", i)}
				i++
			}
		}
		var out []string
		ast.Inspect(x.Body, func(n ast.Node) bool {
			if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) > 0 {
				out = append(out, resolve(ret.Results[0], local)...)
			}
			return true
		})
		return out
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok {
			templates := env[id.Name]
			if len(templates) == 0 {
				templates = env[id.Name+"()"]
			}
			for i, arg := range x.Args {
				var next []string
				for _, t := range templates {
					values := resolve(arg, env)
					if len(values) == 0 {
						next = append(next, t)
					} else {
						for _, v := range values {
							next = append(next, strings.ReplaceAll(t, fmt.Sprintf("@arg%d@", i), v))
						}
					}
				}
				templates = next
			}
			return templates
		}
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && len(x.Args) > 0 {
			switch sel.Sel.Name {
			case "Replace", "ReplaceAll":
				if len(x.Args) >= 3 {
					var out []string
					for _, s := range resolve(x.Args[0], env) {
						for _, old := range resolve(x.Args[1], env) {
							for _, v := range resolve(x.Args[2], env) {
								out = append(out, strings.ReplaceAll(s, old, v))
							}
						}
					}
					return unique(out)
				}
			}
		}
	case *ast.SliceExpr:
		if x.Low == nil {
			if call, ok := x.High.(*ast.CallExpr); ok && len(call.Args) == 2 {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Index" {
					var out []string
					for _, s := range resolve(x.X, env) {
						for _, needle := range resolve(call.Args[1], env) {
							if i := strings.Index(s, needle); i >= 0 {
								out = append(out, s[:i])
							}
						}
					}
					return out
				}
			}
		}
	case *ast.ParenExpr:
		return resolve(x.X, env)
	}
	return nil
}
func unique(xs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
func queryBy(qs []Query, function, fragment string) (Query, error) {
	for _, q := range qs {
		if q.Function == function && strings.Contains(q.SQL, fragment) {
			return q, nil
		}
	}
	return Query{}, fmt.Errorf("missing source query %s %s", function, fragment)
}

func combine(a, b []string) []string {
	var out []string
	for _, x := range a {
		for _, y := range b {
			if len(out) < 128 {
				out = append(out, x+y)
			}
		}
	}
	return unique(out)
}

// Discard impossible cross-products of branch strings: a lock must name an
// alias in its own branch, and positional parameters cannot have holes.
func possibleSQL(sql string) bool {
	if strings.Contains(sql, "FOR UPDATE OF p,i") && !strings.Contains(sql, "chat_app_installation i") {
		return false
	}
	if strings.Contains(sql, "FOR UPDATE OF p,m") && !strings.Contains(sql, "chat_membership m") {
		return false
	}
	seen := map[int]bool{}
	largest := 0
	for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(sql, -1) {
		n, _ := strconv.Atoi(m[1])
		seen[n] = true
		largest = max(largest, n)
	}
	for n := 1; n <= largest; n++ {
		if !seen[n] {
			return false
		}
	}
	return true
}
