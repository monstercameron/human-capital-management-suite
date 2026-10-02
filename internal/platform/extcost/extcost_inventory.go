package extcost

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type NetworkSite struct{ File, Function, Kind string }
type InventoryEntry struct {
	Package, Provider, Operation, Purpose, PriceSource, UsageToday, Attribution, Reason string
	Gateway, CommonLedger                                                               bool
	Units                                                                               []Unit
	Planned                                                                             bool
}
type AllowedSite struct {
	Site   NetworkSite
	Reason string
	Defect bool
}
type Inventory struct {
	Operations []InventoryEntry
	Allowed    []AllowedSite
}

func ScanNetwork(root string) ([]NetworkSite, error) {
	var out []NetworkSite
	for _, dir := range []string{"internal", "cmd", "tools"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, e fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			aliases := map[string]string{}
			for _, imp := range file.Imports {
				p, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				name := filepath.Base(p)
				if imp.Name != nil {
					name = imp.Name.Name
				}
				aliases[name] = p
			}
			for _, decl := range file.Decls {
				function := "package"
				if fn, ok := decl.(*ast.FuncDecl); ok {
					function = fn.Name.Name
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					kind := networkKind(n, aliases)
					if kind != "" {
						out = append(out, NetworkSite{filepath.ToSlash(rel), function, kind})
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := json.Marshal(out[i])
		b, _ := json.Marshal(out[j])
		return string(a) < string(b)
	})
	return out, nil
}
func networkKind(n ast.Node, aliases map[string]string) string {
	construction := func(expr ast.Expr) string {
		sel, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		owner, ok := sel.X.(*ast.Ident)
		if !ok {
			return ""
		}
		p := aliases[owner.Name]
		if p == "net/http" && (sel.Sel.Name == "Client" || sel.Sel.Name == "Transport") || p == "net" && sel.Sel.Name == "Dialer" {
			return p + "." + sel.Sel.Name
		}
		return ""
	}
	switch v := n.(type) {
	case *ast.CompositeLit:
		return construction(v.Type)
	case *ast.SelectorExpr:
		if owner, ok := v.X.(*ast.Ident); ok && aliases[owner.Name] == "net/http" && (v.Sel.Name == "DefaultClient" || v.Sel.Name == "DefaultTransport") {
			return "net/http." + v.Sel.Name
		}
	case *ast.CallExpr:
		if name, ok := v.Fun.(*ast.Ident); ok && name.Name == "new" && len(v.Args) == 1 {
			return construction(v.Args[0])
		}
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			if owner, ok := sel.X.(*ast.Ident); ok {
				p := aliases[owner.Name]
				switch p {
				case "net", "crypto/tls":
					if sel.Sel.Name == "Dial" || sel.Sel.Name == "DialTimeout" || sel.Sel.Name == "DialWithDialer" {
						return p + "." + sel.Sel.Name
					}
				case "net/http":
					switch sel.Sel.Name {
					case "Get", "Post", "PostForm", "Head":
						return p + "." + sel.Sel.Name
					}
				case "google.golang.org/grpc":
					switch sel.Sel.Name {
					case "Dial", "DialContext", "NewClient":
						return p + "." + sel.Sel.Name
					}
				}
			}
			if sel.Sel.Name == "DialContext" {
				return "dial_context"
			}
		}
	}
	return ""
}

// Baseline exceptions retain known defects as findings. A new construction or
// changed inventory fails; it cannot acquire permission through an import.
func CheckInventory(root string, i Inventory) ([]string, error) {
	sites, err := ScanNetwork(root)
	if err != nil {
		return nil, err
	}
	allowed := map[NetworkSite]int{}
	var findings []string
	for _, a := range i.Allowed {
		if strings.TrimSpace(a.Reason) == "" {
			return nil, ErrInvalid
		}
		allowed[a.Site]++
		if a.Defect {
			findings = append(findings, fmt.Sprintf("%s:%s: outbound gateway bypass (%s)", a.Site.File, a.Site.Function, a.Reason))
		}
	}
	for _, s := range sites {
		if allowed[s] == 0 {
			return findings, fmt.Errorf("external usage: unreviewed outbound construction %s:%s (%s)", s.File, s.Function, s.Kind)
		}
		allowed[s]--
	}
	for s, count := range allowed {
		if count != 0 {
			return findings, fmt.Errorf("external usage: inventory drift %s:%s", s.File, s.Function)
		}
	}
	for _, r := range i.Operations {
		if r.Package == "" || r.Provider == "" || r.Operation == "" || r.Purpose == "" || r.Attribution == "" || (len(r.Units) == 0 && r.Reason == "") {
			return nil, ErrInvalid
		}
		if !r.CommonLedger {
			findings = append(findings, r.Package+":"+r.Operation+": no external usage record")
		}
		if !r.Gateway && !r.Planned {
			findings = append(findings, r.Package+":"+r.Operation+": outside outbound gateway")
		}
	}
	return findings, nil
}
func CheckGatewaySource(source []byte) error {
	file, err := parser.ParseFile(token.NewFileSet(), "gateway.go", source, 0)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Run" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			owner, ok := sel.X.(*ast.SelectorExpr)
			if ok && owner.Sel.Name == "journal" {
				found[sel.Sel.Name] = true
			}
			return true
		})
	}
	for _, name := range []string{"Reserve", "MarkSent", "Settle"} {
		if !found[name] {
			return fmt.Errorf("external usage: gateway missing %s", name)
		}
	}
	return nil
}
func LoadInventory(path string) (Inventory, error) {
	var out Inventory
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}
