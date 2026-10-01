// Package invocationpath enforces INTENT-013's single governed invocation
// path. It is deliberately a policy package: it reads Go syntax and the
// already-built endpoint manifest, but it does not execute application code,
// open a database, or add a second runtime gateway.
package invocationpath

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// FindingKind is the stable class of an INTENT-013 finding.
type FindingKind string

const (
	FindingDirectDomainImport  FindingKind = "direct-domain-import"
	FindingDomainStore         FindingKind = "direct-domain-store"
	FindingProviderAdapter     FindingKind = "direct-provider-adapter"
	FindingOutbox              FindingKind = "direct-outbox"
	FindingQueuePublication    FindingKind = "direct-queue-publication"
	FindingMessagingSink       FindingKind = "direct-messaging-sink"
	FindingFileSink            FindingKind = "direct-file-sink"
	FindingEmailSink           FindingKind = "direct-email-sink"
	FindingWorkflowRuntime     FindingKind = "direct-workflow-runtime"
	FindingRouteManifest       FindingKind = "route-manifest-mismatch"
	FindingRouteManifestShape  FindingKind = "route-manifest-invalid"
	FindingUnscopedHandlerPath FindingKind = "handler-not-governed"
)

// Finding is one typed policy finding. File is always a module-relative
// slash-separated path when a source file is available. AllowlistID is set
// only for the exact BIND-001 admin bypass already present in the tree.
type Finding struct {
	Kind        FindingKind
	File        string
	Importer    string
	Imported    string
	Function    string
	AllowlistID string
	Detail      string
}

// String renders a finding in stable, command-friendly form.
func (f Finding) String() string {
	where := f.File
	if where == "" {
		where = f.Importer
	}
	if f.Function != "" {
		where += "#" + f.Function
	}
	allow := ""
	if f.AllowlistID != "" {
		allow = " allowlist=" + f.AllowlistID
	}
	if f.Imported != "" {
		return fmt.Sprintf("%s: %s imports %s%s", f.Kind, where, f.Imported, allow)
	}
	return fmt.Sprintf("%s: %s%s: %s", f.Kind, where, allow, f.Detail)
}

// Source is one Go source file supplied to the syntax scanner.
type Source struct {
	Package  string
	Filename string
	Content  string
}

// Route is the channel-facing portion of one endpoint-manifest row.
type Route struct {
	EndpointID        string
	Capabilities      []string
	IntentDefinitions []string
}

// Channel is one enabled transport channel's route projection. A disabled
// channel is intentionally not checked; it is not exposed by the release.
type Channel struct {
	Name    string
	Enabled bool
	Routes  []Route
}

// Report is the result of scanning a tree. Findings excludes findings that
// carry the narrowly reviewed BIND-001 exception; Accepted retains those
// findings for evidence and audit output.
type Report struct {
	Module           string
	Packages         int
	ImportEdges      int
	Findings         []Finding
	Accepted         []Finding
	ManifestFindings []Finding
}

// OK reports whether the checked tree has no actionable finding.
func (r Report) OK() bool { return len(r.Findings) == 0 && len(r.ManifestFindings) == 0 }

// Explain renders all findings deterministically.
func (r Report) Explain() string {
	findings := append([]Finding(nil), r.Findings...)
	findings = append(findings, r.ManifestFindings...)
	accepted := append([]Finding(nil), r.Accepted...)
	sort.Slice(findings, func(i, j int) bool { return findingKey(findings[i]) < findingKey(findings[j]) })
	sort.Slice(accepted, func(i, j int) bool { return findingKey(accepted[i]) < findingKey(accepted[j]) })
	var b strings.Builder
	fmt.Fprintf(&b, "invocation path: %d findings, %d accepted, packages=%d edges=%d\n", len(findings), len(accepted), r.Packages, r.ImportEdges)
	for _, f := range findings {
		b.WriteString("FINDING ")
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	for _, f := range accepted {
		b.WriteString("ACCEPTED ")
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// Analyze builds the module import graph with go list and runs the AST
// scanner over every non-test Go file under internal/transport and cmd.
func Analyze(root string) (Report, error) {
	packages, err := repopath.ListPackages(root)
	if err != nil {
		// Policy must still be able to report the offending source when an
		// unrelated package is temporarily incomplete (for example while a
		// generated package is landing). The fallback is itself an AST import
		// graph and preserves the fail-closed behavior for parse errors.
		packages, err = filesystemPackages(root)
		if err != nil {
			return Report{}, fmt.Errorf("invocationpath: go list failed: %v; filesystem graph failed: %w", err, err)
		}
	}
	module := repopath.ModulePath(root)
	var sources []Source
	for _, pkg := range packages {
		rel, ok := trimModule(module, pkg.ImportPath)
		if !ok || (!isHandlerRoot(rel) && rel != "internal/intent/app") {
			continue
		}
		entries, readErr := os.ReadDir(pkg.Dir)
		if readErr != nil {
			return Report{}, fmt.Errorf("invocationpath: reading %s: %w", pkg.Dir, readErr)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			filename := filepath.Join(pkg.Dir, entry.Name())
			content, readErr := os.ReadFile(filename)
			if readErr != nil {
				return Report{}, fmt.Errorf("invocationpath: reading %s: %w", filename, readErr)
			}
			relFile, relErr := filepath.Rel(root, filename)
			if relErr != nil {
				return Report{}, fmt.Errorf("invocationpath: locating %s: %w", filename, relErr)
			}
			sources = append(sources, Source{Package: rel, Filename: filepath.ToSlash(relFile), Content: string(content)})
		}
	}
	findings, accepted, err := ScanSources(module, sources)
	if err != nil {
		return Report{}, err
	}
	contractFindings, contractErr := CheckIntentServiceContract(module, sources)
	if contractErr != nil {
		return Report{}, contractErr
	}
	findings = append(findings, contractFindings...)
	sort.Slice(findings, func(i, j int) bool { return findingKey(findings[i]) < findingKey(findings[j]) })
	manifestFindings, manifestErr := CheckDeclaredManifest(root)
	if manifestErr != nil {
		return Report{}, manifestErr
	}
	report := Report{Module: module, Packages: len(packages), Findings: findings, Accepted: accepted, ManifestFindings: manifestFindings}
	for _, pkg := range packages {
		for _, imported := range pkg.Imports {
			if _, ok := trimModule(module, imported); ok {
				report.ImportEdges++
			}
		}
	}
	return report, nil
}

// CheckIntentServiceContract proves the application half of INTENT-013's
// invocation path from the live source tree. Import scanning protects the
// trusted boundary; this check protects the composition point that makes the
// gateway mandatory and the only route from IntentService to a capability
// handler. It is intentionally syntax-only so policy checks cannot create a
// second runtime invocation path.
func CheckIntentServiceContract(module string, sources []Source) ([]Finding, error) {
	service, ok := contractSource(module, sources, "internal/intent/app", "service.go")
	if !ok {
		return []Finding{{Kind: FindingUnscopedHandlerPath, File: "internal/intent/app/service.go", Detail: "IntentService source is missing"}}, nil
	}
	cell, ok := contractSource(module, sources, "internal/intent/app", "cell.go")
	if !ok {
		return []Finding{{Kind: FindingUnscopedHandlerPath, File: "internal/intent/app/cell.go", Detail: "IntentService composition source is missing"}}, nil
	}
	serviceFile, err := parseContractSource(service)
	if err != nil {
		return nil, err
	}
	cellFile, err := parseContractSource(cell)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	if !hasImport(serviceFile, module+"/internal/capability") {
		findings = append(findings, contractFinding(service, "IntentService", "service.go must import internal/capability for the governed gateway"))
	}
	if !hasStructField(serviceFile, "Options", "Gateway", "*capability.Gateway") {
		findings = append(findings, contractFinding(service, "Options.Gateway", "Options must require a capability gateway"))
	}
	if !hasStructField(serviceFile, "IntentService", "gateway", "*capability.Gateway") {
		findings = append(findings, contractFinding(service, "IntentService.gateway", "IntentService must retain the governed gateway"))
	}
	constructor := findFunction(serviceFile, "NewIntentService", "")
	if constructor == nil || !hasNilGuard(constructor, "opts.Gateway") {
		findings = append(findings, contractFinding(service, "NewIntentService", "constructor must refuse a nil capability gateway"))
	}
	invoke := findFunction(serviceFile, "invoke", "IntentService")
	if invoke == nil || !hasMethodCall(invoke, "s.gateway", "Invoke") {
		findings = append(findings, contractFinding(service, "IntentService.invoke", "domain capability dispatch must call the gateway"))
	}
	if !hasOptionsGatewayComposition(cellFile) {
		findings = append(findings, contractFinding(cell, "NewCell", "production composition must pass Gateway: gateway to NewIntentService"))
	}
	sort.Slice(findings, func(i, j int) bool { return findingKey(findings[i]) < findingKey(findings[j]) })
	return findings, nil
}

func contractSource(module string, sources []Source, pkg, filename string) (Source, bool) {
	for _, source := range sources {
		if normalizePackage(module, source.Package) == pkg && path.Base(filepath.ToSlash(source.Filename)) == filename {
			return source, true
		}
	}
	return Source{}, false
}

func parseContractSource(source Source) (*ast.File, error) {
	file, err := parser.ParseFile(token.NewFileSet(), source.Filename, source.Content, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("invocationpath: parsing %s: %w", source.Filename, err)
	}
	return file, nil
}

func hasImport(file *ast.File, want string) bool {
	for _, spec := range file.Imports {
		if imported, err := strconv.Unquote(spec.Path.Value); err == nil && imported == want {
			return true
		}
	}
	return false
}

func hasStructField(file *ast.File, typeName, fieldName, typeNameWant string) bool {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != typeName {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range structType.Fields.List {
				if exprPath(field.Type) != typeNameWant {
					continue
				}
				for _, name := range field.Names {
					if name.Name == fieldName {
						return true
					}
				}
			}
		}
	}
	return false
}

func findFunction(file *ast.File, name, receiver string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || receiver == "" && fn.Recv != nil || receiver != "" && fn.Recv == nil {
			continue
		}
		if receiver == "" {
			return fn
		}
		for _, field := range fn.Recv.List {
			if strings.TrimPrefix(exprPath(field.Type), "*") == receiver {
				return fn
			}
		}
	}
	return nil
}

func hasNilGuard(fn *ast.FuncDecl, want string) bool {
	if fn == nil || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		binary, ok := node.(*ast.BinaryExpr)
		if !ok || binary.Op != token.EQL {
			return true
		}
		left, right := exprPath(binary.X), exprPath(binary.Y)
		if (left == want && right == "nil") || (left == "nil" && right == want) {
			found = true
		}
		return true
	})
	return found
}

func hasMethodCall(fn *ast.FuncDecl, receiver, method string) bool {
	if fn == nil || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == method && exprPath(selector.X) == receiver {
			found = true
		}
		return true
	})
	return found
}

func hasOptionsGatewayComposition(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok || exprPath(literal.Type) != "Options" {
			return true
		}
		for _, element := range literal.Elts {
			keyValue, ok := element.(*ast.KeyValueExpr)
			if ok && exprPath(keyValue.Key) == "Gateway" && exprPath(keyValue.Value) == "gateway" {
				found = true
			}
		}
		return true
	})
	return found
}

func exprPath(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		prefix := exprPath(value.X)
		if prefix == "" {
			return value.Sel.Name
		}
		return prefix + "." + value.Sel.Name
	case *ast.StarExpr:
		return "*" + exprPath(value.X)
	default:
		return ""
	}
}

func contractFinding(source Source, function, detail string) Finding {
	return Finding{Kind: FindingUnscopedHandlerPath, File: filepath.ToSlash(source.Filename), Function: function, Detail: detail}
}

// CheckDeclaredManifest compares the live generated endpoint manifest with
// definitions/api/endpoint-manifest.json. The comparison is intentionally
// limited to the route-facing identity, capability, and intent-definition
// sets; the transport manifest package remains the owner of all other row
// fields.
func CheckDeclaredManifest(root string) ([]Finding, error) {
	live, err := manifest.Build()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, "definitions", "api", "endpoint-manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("invocationpath: reading declared endpoint manifest: %w", err)
	}
	var declared manifest.EndpointManifest
	if err := json.Unmarshal(data, &declared); err != nil {
		return nil, fmt.Errorf("invocationpath: parsing declared endpoint manifest: %w", err)
	}
	if got, want := ManifestGolden(live), ManifestGolden(&declared); got != want {
		return []Finding{{Kind: FindingRouteManifest, File: "definitions/api/endpoint-manifest.json", Detail: "declared route capability or intent-definition sets differ from the live manifest"}}, nil
	}
	return nil, nil
}

func filesystemPackages(root string) ([]repopath.Package, error) {
	module := repopath.ModulePath(root)
	byDir := map[string]*repopath.Package{}
	visit := func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || strings.HasPrefix(name, ".gocache") || name == "test-results" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		dir := filepath.Dir(filename)
		pkg := byDir[dir]
		if pkg == nil {
			relDir, relErr := filepath.Rel(root, dir)
			if relErr != nil {
				return relErr
			}
			importPath := module
			if relDir != "." {
				importPath += "/" + filepath.ToSlash(relDir)
			}
			pkg = &repopath.Package{ImportPath: importPath, Dir: dir}
			byDir[dir] = pkg
		}
		content, readErr := os.ReadFile(filename)
		if readErr != nil {
			return readErr
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), filename, content, parser.ImportsOnly)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", filename, parseErr)
		}
		for _, spec := range parsed.Imports {
			imported, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr == nil && !contains(pkg.Imports, imported) {
				pkg.Imports = append(pkg.Imports, imported)
			}
		}
		return nil
	}
	for _, target := range []string{filepath.Join(root, "internal", "transport"), filepath.Join(root, "cmd")} {
		if _, statErr := os.Stat(target); statErr != nil {
			return nil, statErr
		}
		if err := filepath.WalkDir(target, visit); err != nil {
			return nil, err
		}
	}
	out := make([]repopath.Package, 0, len(byDir))
	for _, pkg := range byDir {
		sort.Strings(pkg.Imports)
		out = append(out, *pkg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImportPath < out[j].ImportPath })
	return out, nil
}

// ScanSources runs the pure AST/import check over caller-supplied sources.
// It is the mutation-test seam: a fixture handler can be denied without a
// filesystem, process, database, or application service.
func ScanSources(module string, sources []Source) (findings, accepted []Finding, err error) {
	sorted := append([]Source(nil), sources...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Filename != sorted[j].Filename {
			return sorted[i].Filename < sorted[j].Filename
		}
		return sorted[i].Package < sorted[j].Package
	})
	for _, source := range sorted {
		if !isHandlerRoot(source.Package) || strings.HasSuffix(source.Filename, "_test.go") {
			continue
		}
		fileFindings, scanErr := scanSource(module, source)
		if scanErr != nil {
			return nil, nil, scanErr
		}
		for _, finding := range fileFindings {
			if finding.AllowlistID != "" {
				accepted = append(accepted, finding)
			} else {
				findings = append(findings, finding)
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findingKey(findings[i]) < findingKey(findings[j]) })
	sort.Slice(accepted, func(i, j int) bool { return findingKey(accepted[i]) < findingKey(accepted[j]) })
	return findings, accepted, nil
}

// ScanSource scans one source file. The package may be module-relative or a
// full module import path.
func ScanSource(module string, source Source) ([]Finding, error) {
	if !isHandlerRoot(normalizePackage(module, source.Package)) {
		return nil, nil
	}
	return scanSource(module, source)
}

func scanSource(module string, source Source) ([]Finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, source.Filename, source.Content, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("invocationpath: parsing %s: %w", source.Filename, err)
	}
	pkgRel := normalizePackage(module, source.Package)
	fileName := filepath.ToSlash(source.Filename)
	var findings []Finding
	for _, spec := range file.Imports {
		imported, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			continue
		}
		importedRel, ok := trimModule(module, imported)
		if !ok {
			continue
		}
		kind := classifyImport(importedRel)
		if kind == "" {
			continue
		}
		finding := Finding{
			Kind:     kind,
			File:     fileName,
			Importer: pkgRel,
			Imported: imported,
			Detail:   "material handlers must enter CapabilityGateway then IntentService before any effect",
		}
		if isBind001AdminBypass(pkgRel, fileName, importedRel, kind) {
			finding.AllowlistID = "BIND-001"
		}
		findings = append(findings, finding)
	}

	return findings, nil
}

// ExpectedRoutes projects the exact route-facing fields of the live endpoint
// manifest into channel manifests.
func ExpectedRoutes(m *manifest.EndpointManifest) []Route {
	if m == nil {
		return nil
	}
	routes := make([]Route, 0, len(m.Endpoints))
	for _, endpoint := range m.Endpoints {
		routes = append(routes, Route{
			EndpointID:        endpoint.EndpointID,
			Capabilities:      append([]string(nil), endpoint.CapabilityRefs...),
			IntentDefinitions: append([]string(nil), endpoint.AcceptedIntentDefinitionRefs...),
		})
	}
	return routes
}

// CheckManifestConformance proves that every enabled channel exposes exactly
// the endpoint, capability, and intent-definition sets declared by m.
func CheckManifestConformance(m *manifest.EndpointManifest, channels []Channel) []Finding {
	if m == nil {
		return []Finding{{Kind: FindingRouteManifestShape, Detail: "endpoint manifest is nil"}}
	}
	expected := make(map[string]Route, len(m.Endpoints))
	var findings []Finding
	for _, route := range ExpectedRoutes(m) {
		if _, exists := expected[route.EndpointID]; exists {
			findings = append(findings, Finding{Kind: FindingRouteManifestShape, Detail: "manifest declares duplicate endpoint " + route.EndpointID})
		}
		expected[route.EndpointID] = route
	}
	enabled := 0
	seenChannels := map[string]bool{}
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		enabled++
		if channel.Name == "" || seenChannels[channel.Name] {
			findings = append(findings, Finding{Kind: FindingRouteManifestShape, File: channel.Name, Detail: "enabled channel name is empty or duplicated"})
		}
		seenChannels[channel.Name] = true
		seen := map[string]bool{}
		for _, route := range channel.Routes {
			if seen[route.EndpointID] {
				findings = append(findings, routeFinding(channel.Name, route.EndpointID, "route is duplicated"))
				continue
			}
			seen[route.EndpointID] = true
			want, ok := expected[route.EndpointID]
			if !ok {
				findings = append(findings, routeFinding(channel.Name, route.EndpointID, "route is absent from the endpoint manifest"))
				continue
			}
			if !sameStrings(route.Capabilities, want.Capabilities) {
				findings = append(findings, routeFinding(channel.Name, route.EndpointID, "capability set differs from the endpoint manifest"))
			}
			if !sameStrings(route.IntentDefinitions, want.IntentDefinitions) {
				findings = append(findings, routeFinding(channel.Name, route.EndpointID, "intent-definition set differs from the endpoint manifest"))
			}
		}
		for endpointID := range expected {
			if !seen[endpointID] {
				findings = append(findings, routeFinding(channel.Name, endpointID, "enabled channel omits endpoint manifest route"))
			}
		}
	}
	if enabled == 0 {
		findings = append(findings, Finding{Kind: FindingRouteManifestShape, Detail: "no enabled channel was supplied"})
	}
	sort.Slice(findings, func(i, j int) bool { return findingKey(findings[i]) < findingKey(findings[j]) })
	return findings
}

// ManifestGolden renders a stable, line-oriented golden representation.
func ManifestGolden(m *manifest.EndpointManifest) string {
	routes := ExpectedRoutes(m)
	var b strings.Builder
	for _, route := range routes {
		fmt.Fprintf(&b, "%s|caps=%s|intents=%s\n", route.EndpointID,
			strings.Join(sortedUnique(route.Capabilities), ","),
			strings.Join(sortedUnique(route.IntentDefinitions), ","))
	}
	return b.String()
}

// Run executes the policy command core. A command wrapper under cmd can bind
// os.Args and standard streams without duplicating policy logic.
func Run(root string, stdout, stderr io.Writer) error {
	report, err := Analyze(root)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(stdout, report.Explain())
	if !report.OK() {
		return fmt.Errorf("invocationpath: %d actionable findings", len(report.Findings)+len(report.ManifestFindings))
	}
	return nil
}

// Main is a command-friendly exit-code adapter for a future cmd entrypoint.
func Main(args []string, stdout, stderr io.Writer) int {
	root := repopath.RootDir()
	if len(args) > 0 && args[0] != "" {
		root = args[0]
	}
	if err := Run(root, stdout, stderr); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func classifyImport(rel string) FindingKind {
	segments := strings.Split(strings.Trim(rel, "/"), "/")
	if len(segments) >= 2 && segments[0] == "internal" && segments[1] == "domains" {
		if hasSegment(segments, "store") || hasSegment(segments, "persistence") || hasSegment(segments, "repository") {
			return FindingDomainStore
		}
		return FindingDirectDomainImport
	}
	if under(rel, "internal/data") {
		if rel == "internal/data/dbport" || rel == "internal/data/pgtest" {
			return ""
		}
		if under(rel, "internal/data/outbox") {
			return FindingOutbox
		}
		if under(rel, "internal/data/runtimestate") {
			return FindingQueuePublication
		}
		return FindingProviderAdapter
	}
	if hasSegment(segments, "provider") || hasSegment(segments, "providers") || hasSegment(segments, "adapters") {
		return FindingProviderAdapter
	}
	if under(rel, "internal/workflow/runtime") || under(rel, "internal/workflow/execute") || under(rel, "internal/workflow/lease") || under(rel, "internal/platform/execution") {
		return FindingWorkflowRuntime
	}
	if under(rel, "internal/email") {
		return FindingEmailSink
	}
	if under(rel, "internal/files") || under(rel, "internal/file") {
		return FindingFileSink
	}
	if under(rel, "internal/messaging") || under(rel, "internal/notification") {
		return FindingMessagingSink
	}
	return ""
}

func isBind001AdminBypass(pkgRel, filename, importedRel string, kind FindingKind) bool {
	if !under(pkgRel, "internal/transport/admin") || kind != FindingDirectDomainImport || !under(importedRel, "internal/domains") {
		return false
	}
	base := path.Base(filepath.ToSlash(filename))
	return base == "server.go" || base == "worker_state.go" || base == "explain_transaction.go"
}

func isHandlerRoot(rel string) bool { return under(rel, "internal/transport") || under(rel, "cmd") }

func normalizePackage(module, pkg string) string {
	if rel, ok := trimModule(module, pkg); ok {
		return rel
	}
	return strings.Trim(strings.ReplaceAll(pkg, "\\", "/"), "/")
}

func trimModule(module, importPath string) (string, bool) {
	if importPath == module {
		return "", true
	}
	prefix := module + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(importPath, prefix), true
}

func under(rel, root string) bool { return rel == root || strings.HasPrefix(rel, root+"/") }

func hasSegment(segments []string, want string) bool {
	for _, segment := range segments {
		if segment == want {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func routeFinding(channel, endpoint, detail string) Finding {
	return Finding{Kind: FindingRouteManifest, File: channel, Function: endpoint, Detail: detail}
}

func sortedUnique(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func sameStrings(a, b []string) bool {
	aa, bb := sortedUnique(a), sortedUnique(b)
	return strings.Join(aa, "\x00") == strings.Join(bb, "\x00")
}

func findingKey(f Finding) string {
	return string(f.Kind) + "|" + f.File + "|" + f.Function + "|" + f.Imported + "|" + f.Detail
}

// digestLines is retained as a small pure helper for callers that want to
// pin a report without depending on map or filesystem order.
func digestLines(h hash.Hash, lines []string) string {
	for _, line := range lines {
		_, _ = h.Write([]byte(line))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Digest returns a stable digest of the actionable findings.
func (r Report) Digest() string {
	lines := make([]string, 0, len(r.Findings)+len(r.ManifestFindings))
	for _, finding := range append(append([]Finding(nil), r.Findings...), r.ManifestFindings...) {
		lines = append(lines, findingKey(finding))
	}
	sort.Strings(lines)
	return digestLines(sha256Hash(), lines)
}

func sha256Hash() hash.Hash {
	return sha256.New()
}
