//go:build !(js && wasm)

package main

import (
	"compress/gzip"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

func TestCompactSourcePreservesTripleAndNestedStringTables(t *testing.T) {
	source := []byte("package fixture\n" + `
var triples = map[string][3]string{
	"empty": {"", "", ""},
	"line": {"actual\nline", "actual\ttab", ` + "`literal\\n\\t`" + `},
	"quote": {"say \"hello\"", "Grüße ⟦x⟧", "مرحبا"},
	"key-03": {"en-03", "de-03", "ar-03"},
	"key-04": {"en-04", "de-04", "ar-04"},
	"key-05": {"en-05", "de-05", "ar-05"},
	"key-06": {"en-06", "de-06", "ar-06"},
	"key-07": {"en-07", "de-07", "ar-07"},
	"key-08": {"en-08", "de-08", "ar-08"},
	"key-09": {"en-09", "de-09", "ar-09"},
	"key-10": {"en-10", "de-10", "ar-10"},
	"key-11": {"en-11", "de-11", "ar-11"},
	"key-12": {"en-12", "de-12", "ar-12"},
	"key-13": {"en-13", "de-13", "ar-13"},
	"key-14": {"en-14", "de-14", "ar-14"},
	"key-15": {"en-15", "de-15", "ar-15"},
}

var nested = map[string]map[string]string{
	"en-US": {
		"": "", "line": "actual\nline", "tab": "actual\ttab", "literal": ` + "`literal\\n\\t`" + `,
		"unicode": "Grüße ⟦x⟧ العربية", "k-5": "v-5", "k-6": "v-6", "k-7": "v-7",
	},
	"ar": {
		"": "empty-key", "line": "line\nbreak", "tab": "tab\tbyte", "unicode": "مرحبا",
		"k-4": "v-4", "k-5": "v-5", "k-6": "v-6", "k-7": "v-7",
	},
}
`)

	wantSource, err := parser.ParseFile(token.NewFileSet(), "original.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	wantTriples := stringTripleMap(t, findCompositeValue(t, wantSource, "triples"))
	wantNested := nestedStringMap(t, findCompositeValue(t, wantSource, "nested"))

	sibling := []byte("package fixture\nvar compactlocalize = 1\n")
	identifiers, err := sourceIdentifiers("sibling.go", sibling)
	if err != nil {
		t.Fatal(err)
	}
	gotSource, literals, entries, err := compactSourceWithAlias("original.go", source, uniqueCompactAlias(identifiers))
	if err != nil {
		t.Fatal(err)
	}
	if literals != 2 || entries != 16+16 {
		t.Fatalf("compacted %d literals/%d entries, want 2/32", literals, entries)
	}
	gotFile, err := parser.ParseFile(token.NewFileSet(), "compact.go", gotSource, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("compacted source does not parse: %v\n%s", err, gotSource)
	}
	if got := compactTriplesValue(t, findValue(t, gotFile, "triples")); !reflect.DeepEqual(got, wantTriples) {
		t.Fatalf("expanded triples differ from source table\ngot:  %#v\nwant: %#v", got, wantTriples)
	}
	if got := compactNestedStringsValue(t, findValue(t, gotFile, "nested")); !reflect.DeepEqual(got, wantNested) {
		t.Fatalf("expanded nested strings differ from source table\ngot:  %#v\nwant: %#v", got, wantNested)
	}
	if !strings.Contains(string(gotSource), "compactlocalize1") {
		t.Fatalf("injected package alias did not avoid the existing identifier:\n%s", gotSource[:min(len(gotSource), 500)])
	}
	assertCompactedFixtureRuns(t, gotSource, findValue(t, gotFile, "triples"), findValue(t, gotFile, "nested"))
}

func TestCompactSourceCompressesOnlyPackageInitialization(t *testing.T) {
	literal := stringMapLiteral(16)
	source := []byte("package fixture\n" +
		"var packageTable = " + literal + "\n" +
		"var initIIFE = func() map[string]string { return " + literal + " }()\n" +
		"var nestedIIFE = func() map[string]string { return func() map[string]string { return " + literal + " }() }()\n" +
		"func later() map[string]string { return " + literal + " }\n" +
		"var closure = func() func() map[string]string { return func() map[string]string { return " + literal + " } }()\n" +
		"var afterFunction = " + literal + "\n")
	compacted, literals, entries, err := compactSource("fixture.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if literals != 6 || entries != 6*16 {
		t.Fatalf("compacted %d literals/%d entries, want 6/96", literals, entries)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", compacted, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("compacted scope fixture does not parse: %v\n%s", err, compacted)
	}
	for name, want := range map[string]string{
		"packageTable":  "ExpandGzipCompactStrings",
		"initIIFE":      "ExpandGzipCompactStrings",
		"nestedIIFE":    "ExpandGzipCompactStrings",
		"closure":       "ExpandCompactStrings",
		"afterFunction": "ExpandGzipCompactStrings",
	} {
		if got := compactMethodInValue(t, file, name); got != want {
			t.Errorf("%s uses %s, want %s", name, got, want)
		}
	}
	if got := compactMethodInFunction(t, file, "later"); got != "ExpandCompactStrings" {
		t.Errorf("function body uses %s, want ExpandCompactStrings", got)
	}
}

func TestCompactPayloadCompressionIsDeterministic(t *testing.T) {
	data := "empty\x00\x00unicode العربية Grüße\x00line\nvalue\x00"
	first, second := compressCompactPayload(data), compressCompactPayload(data)
	if first != second {
		t.Fatal("same compact payload produced different gzip bytes")
	}
	reader, err := gzip.NewReader(strings.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if !reader.ModTime.IsZero() || reader.Name != "" || reader.Comment != "" || reader.OS != 255 {
		t.Fatalf("gzip header is not stable: %#v", reader.Header)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(decoded) != data {
		t.Fatalf("decoded payload = %q, want %q", decoded, data)
	}
}

func assertCompactedFixtureRuns(t *testing.T, source []byte, triples, nested ast.Expr) {
	t.Helper()
	dir := t.TempDir()
	compactPath, err := filepath.Abs("../../../../internal/experience/localize/compact.go")
	if err != nil {
		t.Fatal(err)
	}
	compactSource, err := os.ReadFile(compactPath)
	if err != nil {
		t.Fatal(err)
	}
	tripleExpression := formatExpression(t, triples)
	nestedExpression := formatExpression(t, nested)
	generatedFile, err := parser.ParseFile(token.NewFileSet(), "generated.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var decoderImport string
	for _, spec := range generatedFile.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err == nil && path == localizeImportPath {
			decoderImport = formatExpression(t, spec)
			break
		}
	}
	if decoderImport == "" {
		t.Fatal("generated source has no localize decoder import")
	}
	files := map[string]string{
		"go.mod":       "module github.com/monstercameron/human-capital-management-suite\n\ngo 1.24\n",
		"factories.go": "package fixture\nimport " + decoderImport + "\nfunc makeTriples() map[string][3]string { return " + tripleExpression + " }\nfunc makeNested() map[string]map[string]string { return " + nestedExpression + " }\n",
		"internal/experience/localize/message.go": "package localize\ntype Message struct { Text string; Plural map[string]string }\n",
		"fixture_test.go": `package fixture
import ("reflect"; "testing")
func TestGeneratedValuesAndFreshMaps(t *testing.T) {
	firstTriples, secondTriples := makeTriples(), makeTriples()
	if len(firstTriples) != 16 || !reflect.DeepEqual(firstTriples["line"], [3]string{"actual\nline", "actual\ttab", "literal\\n\\t"}) { t.Fatalf("triples mismatch: %#v", firstTriples) }
	firstNested, secondNested := makeNested(), makeNested()
	if len(firstNested) != 2 || len(firstNested["en-US"]) != 8 || firstNested["en-US"]["literal"] != "literal\\n\\t" || firstNested["ar"]["line"] != "line\nbreak" { t.Fatalf("nested mismatch: %#v", firstNested) }
	firstTriples["mutation"] = [3]string{"x", "x", "x"}
	delete(firstNested, "ar")
	firstNested["en-US"]["line"] = "changed"
	if len(secondTriples) != 16 || secondTriples["line"][0] != "actual\nline" { t.Fatalf("triple factory shared state: %#v", secondTriples) }
	if len(secondNested) != 2 || secondNested["en-US"]["line"] != "actual\nline" || secondNested["ar"]["line"] != "line\nbreak" { t.Fatalf("nested factory shared outer/inner state: %#v", secondNested) }
}
`,
		"sibling.go": "package fixture\nvar compactlocalize = 1\n",
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal/experience/localize"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal/experience/localize/compact.go"), compactSource, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compacted.go"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("go", "test", "-count=1", "./...")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated compacted source failed compile/evaluation test: %v\n%s", err, output)
	}
}

func formatExpression(t *testing.T, expression ast.Node) string {
	t.Helper()
	var output strings.Builder
	if err := format.Node(&output, token.NewFileSet(), expression); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestCompactSourceLeavesUnsupportedAndDuplicateTablesAlone(t *testing.T) {
	tests := map[string]string{
		"flat decoded duplicate key":              flatSourceWithTail(`"\x6b0": "duplicate"`),
		"flat duplicate compacted and residue":    flatSourceWithTail(`"\x6b0": dynamicValue()`),
		"message duplicate compacted and residue": messageSourceWithTail(`"\x6b0": {Plural: map[string]string{"other": "many"}}`),
		"nested message duplicate locale":         nestedMessageSourceWithTail(`"en-US": {"other": {Text: "duplicate"}}`, false),
		"nested message duplicate key":            nestedMessageSourceWithTail(`"k0": {Plural: map[string]string{"other": "many"}}`, true),
		"decoded duplicate triple key":            tripleSourceWithTail(`"\x6b0": {"duplicate", "duplicate", "duplicate"}`),
		"NUL value":                               tripleSourceWithTail(`"last": {"en", "de\x00bad", "ar"}`),
		"dynamic value":                           tripleSourceWithTail(`"last": {"en", dynamicValue(), "ar"}`),
		"omitted locale":                          tripleSourceWithTail(`"last": {"en", "de"}`),
		"indexed locale":                          tripleSourceWithTail(`"last": {0: "en", 1: "de", 2: "ar"}`),
		"nested duplicate key":                    nestedSourceWithTail(`"\x6b0": "duplicate"`),
		"nested NUL value":                        nestedSourceWithTail(`"last": "bad\x00value"`),
		"nested dynamic value":                    nestedSourceWithTail(`"last": dynamicValue()`),
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			got, literals, entries, err := compactSource("fixture.go", []byte("package fixture\n"+source+"\nfunc dynamicValue() string { return \"dynamic\" }\n"))
			if err != nil {
				t.Fatal(err)
			}
			if literals != 0 || entries != 0 || strings.Contains(string(got), ".ExpandCompactTriples(") || strings.Contains(string(got), ".ExpandCompactStrings(") {
				t.Fatalf("unsupported/invalid table was compacted: literals=%d entries=%d\n%s", literals, entries, got)
			}
		})
	}
}

func flatSourceWithTail(tail string) string {
	var source strings.Builder
	source.WriteString("var candidate = map[string]string{\n")
	for i := 0; i < 16; i++ {
		source.WriteString(strconv.Quote("k"+strconv.Itoa(i)) + `: "value",` + "\n")
	}
	source.WriteString(tail + ",\n}\n")
	return source.String()
}

func messageSourceWithTail(tail string) string {
	var source strings.Builder
	source.WriteString("import localize \"github.com/monstercameron/human-capital-management-suite/internal/experience/localize\"\nvar candidate = map[string]localize.Message{\n")
	for i := 0; i < 16; i++ {
		source.WriteString(strconv.Quote("k"+strconv.Itoa(i)) + `: {Text: "value"},` + "\n")
	}
	source.WriteString(tail + ",\n}\n")
	return source.String()
}

func nestedMessageSourceWithTail(tail string, duplicateInner bool) string {
	var source strings.Builder
	source.WriteString("import localize \"github.com/monstercameron/human-capital-management-suite/internal/experience/localize\"\nvar candidate = map[string]map[string]localize.Message{\"en-US\": {\n")
	for i := 0; i < 16; i++ {
		source.WriteString(strconv.Quote("k"+strconv.Itoa(i)) + `: {Text: "value"},` + "\n")
	}
	if duplicateInner {
		source.WriteString(tail + ",\n},\n}\n")
	} else {
		source.WriteString("},\n" + tail + ",\n}\n")
	}
	return source.String()
}

func TestCompactSourceChoosesAliasAgainstSiblingDeclarations(t *testing.T) {
	current := []byte("package fixture\n" + tripleSourceWithTail(""))
	sibling := []byte("package fixture\nvar compactlocalize = 1\n")
	identifiers, err := sourceIdentifiers("sibling.go", sibling)
	if err != nil {
		t.Fatal(err)
	}
	got, literals, _, err := compactSourceWithAlias("current.go", current, uniqueCompactAlias(identifiers))
	if err != nil {
		t.Fatal(err)
	}
	if literals != 1 || !strings.Contains(string(got), "compactlocalize1") {
		t.Fatalf("sibling declaration was not respected when choosing import alias: literals=%d\n%s", literals, got)
	}
}

func tripleSourceWithTail(tail string) string {
	var source strings.Builder
	source.WriteString("var candidate = map[string][3]string{\n")
	for i := 0; i < 16; i++ {
		source.WriteString(strconv.Quote("k"+strconv.Itoa(i)) + `: {"en", "de", "ar"},` + "\n")
	}
	if tail != "" {
		source.WriteString(tail + ",\n")
	}
	source.WriteString("}\n")
	return source.String()
}

func nestedSourceWithTail(tail string) string {
	var source strings.Builder
	source.WriteString("var candidate = map[string]map[string]string{\"en-US\": {\n")
	for i := 0; i < 16; i++ {
		source.WriteString(strconv.Quote("k"+strconv.Itoa(i)) + `: "value",` + "\n")
	}
	if tail != "" {
		source.WriteString(tail + ",\n")
	}
	source.WriteString("}}\n")
	return source.String()
}

func findValue(t *testing.T, file *ast.File, name string) ast.Expr {
	t.Helper()
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, identifier := range value.Names {
				if identifier.Name == name {
					return value.Values[index]
				}
			}
		}
	}
	t.Fatalf("value %q not found", name)
	return nil
}

func findCompositeValue(t *testing.T, file *ast.File, name string) *ast.CompositeLit {
	t.Helper()
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, identifier := range value.Names {
				if identifier.Name == name {
					if literal, ok := value.Values[index].(*ast.CompositeLit); ok {
						return literal
					}
					t.Fatalf("%s is not a composite literal", name)
				}
			}
		}
	}
	t.Fatalf("composite value %q not found", name)
	return nil
}

func stringTripleMap(t *testing.T, literal *ast.CompositeLit) map[string][3]string {
	t.Helper()
	got := make(map[string][3]string, len(literal.Elts))
	for _, element := range literal.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("non-key/value triple entry: %T", element)
		}
		key, ok := plainStringLiteral(entry.Key)
		if !ok {
			t.Fatalf("nonliteral triple key: %T", entry.Key)
		}
		values, ok := entry.Value.(*ast.CompositeLit)
		if !ok || len(values.Elts) != 3 {
			t.Fatalf("triple value has unexpected shape: %T", entry.Value)
		}
		var triple [3]string
		for i, element := range values.Elts {
			triple[i], ok = plainStringLiteral(element)
			if !ok {
				t.Fatalf("nonliteral locale value: %T", element)
			}
		}
		got[key] = triple
	}
	return got
}

func nestedStringMap(t *testing.T, literal *ast.CompositeLit) map[string]map[string]string {
	t.Helper()
	got := make(map[string]map[string]string, len(literal.Elts))
	for _, element := range literal.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("non-key/value locale entry: %T", element)
		}
		locale, ok := plainStringLiteral(entry.Key)
		if !ok {
			t.Fatalf("nonliteral locale key: %T", entry.Key)
		}
		inner, ok := entry.Value.(*ast.CompositeLit)
		if !ok {
			t.Fatalf("nonliteral inner locale map: %T", entry.Value)
		}
		values := make(map[string]string, len(inner.Elts))
		for _, pairExpression := range inner.Elts {
			pair, ok := pairExpression.(*ast.KeyValueExpr)
			if !ok {
				t.Fatalf("non-key/value copy entry: %T", pairExpression)
			}
			key, keyOK := plainStringLiteral(pair.Key)
			value, valueOK := plainStringLiteral(pair.Value)
			if !keyOK || !valueOK {
				t.Fatalf("nonliteral copy entry: %#v", pair)
			}
			values[key] = value
		}
		got[locale] = values
	}
	return got
}

func compactTriplesValue(t *testing.T, expression ast.Expr) map[string][3]string {
	t.Helper()
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		t.Fatalf("triple replacement is not a call: %T", expression)
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("triple replacement is not a selector call: %T", call.Fun)
	}
	data := unquoteCallData(t, call, selector.Sel.Name)
	if selector.Sel.Name == "ExpandGzipCompactTriples" {
		return localize.ExpandGzipCompactTriples(data, compactCallLength(t, call))
	}
	return localize.ExpandCompactTriples(data)
}

func compactNestedStringsValue(t *testing.T, expression ast.Expr) map[string]map[string]string {
	t.Helper()
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("nested replacement is not a composite literal: %T", expression)
	}
	got := make(map[string]map[string]string, len(literal.Elts))
	for _, element := range literal.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("non-key/value compact locale: %T", element)
		}
		locale, ok := plainStringLiteral(entry.Key)
		if !ok {
			t.Fatalf("nonliteral compact locale: %T", entry.Key)
		}
		call, ok := entry.Value.(*ast.CallExpr)
		if !ok {
			t.Fatalf("compact locale is not a call: %T", entry.Value)
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			t.Fatalf("compact locale is not a selector call: %T", call.Fun)
		}
		data := unquoteCallData(t, call, selector.Sel.Name)
		if selector.Sel.Name == "ExpandGzipCompactStrings" {
			got[locale] = localize.ExpandGzipCompactStrings(data, compactCallLength(t, call), nil)
		} else {
			got[locale] = localize.ExpandCompactStrings(data, nil)
		}
	}
	return got
}

func compactCallLength(t *testing.T, call *ast.CallExpr) int {
	t.Helper()
	if len(call.Args) < 2 {
		t.Fatalf("compressed compact call has no expected length: %#v", call.Args)
	}
	literal, ok := call.Args[1].(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		t.Fatalf("expected compact length is not an integer literal: %T", call.Args[1])
	}
	length, err := strconv.Atoi(literal.Value)
	if err != nil {
		t.Fatal(err)
	}
	return length
}

func stringMapLiteral(entries int) string {
	var source strings.Builder
	source.WriteString("map[string]string{")
	for index := 0; index < entries; index++ {
		source.WriteString(strconv.Quote("key-"+strconv.Itoa(index)) + ": " + strconv.Quote("value-"+strconv.Itoa(index)) + ",")
	}
	source.WriteByte('}')
	return source.String()
}

func compactMethodInValue(t *testing.T, file *ast.File, name string) string {
	t.Helper()
	return compactMethodInExpression(t, findValue(t, file, name))
}

func compactMethodInFunction(t *testing.T, file *ast.File, name string) string {
	t.Helper()
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return compactMethodInExpression(t, function.Body)
		}
	}
	t.Fatalf("function %q not found", name)
	return ""
}

func compactMethodInExpression(t *testing.T, expression ast.Node) string {
	t.Helper()
	method := ""
	ast.Inspect(expression, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && strings.Contains(selector.Sel.Name, "Compact") {
			if method != "" {
				method = "MULTIPLE"
			} else {
				method = selector.Sel.Name
			}
		}
		return true
	})
	return method
}

func unquoteCallData(t *testing.T, call *ast.CallExpr, method string) string {
	t.Helper()
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != method || len(call.Args) == 0 {
		t.Fatalf("unexpected compact call: %#v", call.Fun)
	}
	data, ok := call.Args[0].(*ast.BasicLit)
	if !ok || data.Kind != token.STRING {
		t.Fatalf("compact data is not a string literal: %T", call.Args[0])
	}
	decoded, err := strconv.Unquote(data.Value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
