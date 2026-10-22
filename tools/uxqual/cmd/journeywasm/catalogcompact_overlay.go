//go:build !(js && wasm)

package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// The product's text lives in Go map literals: a message catalog of about 6,500
// entries in three locales, and copy tables of a few hundred entries for each
// screen. Compiled as written, each entry is a static table row (its key, and
// for a localize.Message the message's pointers) and, for most of them, a
// store into the map when the package starts: together a few hundred kilobytes
// of the browser module that carry no text. localize.ExpandCompact and
// ExpandCompactStrings build the same map from one string.
//
// The client build therefore compiles the module's files with each such literal
// replaced by the compact form, through the go command's -overlay, like the
// fetch-only net/http and the stand-in crypto/tls. The literals stay the source
// everyone edits: the replacement is made from the files as they are at the time
// of the build, entry by entry, and an entry that is not a plain "key": "text" or
// "key": {Text: "text"} (a plural or gender form, a constant) stays in the file as
// the literal it was. A file that cannot be read the expected way gets the
// ordinary build and a line saying so; the size gate then decides whether that
// build ships.

// errCatalogCompactUnavailable reports sources the replacement cannot read.
var errCatalogCompactUnavailable = errors.New("the module's text tables could not be read for the compact client build")

const (
	moduleImportPath   = "github.com/monstercameron/human-capital-management-suite/"
	localizeImportPath = moduleImportPath + "internal/experience/localize"
	compactImportAlias = "compactlocalize"
	// compactMinimumLength is the size below which a table is left alone: the
	// compact form's own call costs more than a handful of entries.
	compactMinimumLength = 16
)

// compactCatalogStats says what the replacement did.
type compactCatalogStats struct {
	files, literals, entries int
}

// compactCatalogReplacements returns the overlay entries that swap the module's
// compiled files for their compact forms; the new files are written into dir.
func compactCatalogReplacements(goBin, dir string) (map[string]string, compactCatalogStats, error) {
	var stats compactCatalogStats
	command := exec.Command(goBin, "list", "-deps", "-tags="+wasmBuildTags, "-f", "{{.ImportPath}}|{{.Dir}}|{{join .GoFiles \" \"}}", wasmPackage)
	command.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	listed, err := command.Output()
	if err != nil {
		return nil, stats, fmt.Errorf("%w: listing the client's packages: %v", errCatalogCompactUnavailable, err)
	}
	replace := map[string]string{}
	for _, line := range strings.Split(string(listed), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(fields) != 3 || !strings.HasPrefix(fields[0], moduleImportPath) || strings.Contains(fields[0], "/gen/") {
			continue
		}
		type sourceFile struct {
			path   string
			name   string
			source []byte
		}
		files := make([]sourceFile, 0)
		for _, name := range strings.Fields(fields[2]) {
			original := filepath.Join(fields[1], name)
			source, err := os.ReadFile(original)
			if err != nil {
				return nil, stats, fmt.Errorf("%w: %v", errCatalogCompactUnavailable, err)
			}
			files = append(files, sourceFile{path: original, name: name, source: source})
		}
		packageNames := make(map[string]struct{})
		for _, file := range files {
			identifiers, err := sourceIdentifiers(file.path, file.source)
			if err != nil {
				return nil, stats, err
			}
			for identifier := range identifiers {
				packageNames[identifier] = struct{}{}
			}
		}
		importAlias := uniqueCompactAlias(packageNames)
		for _, file := range files {
			compact, literals, entries, err := compactSourceWithAlias(file.path, file.source, importAlias)
			if err != nil {
				return nil, stats, err
			}
			if literals == 0 {
				continue
			}
			target := filepath.Join(dir, fmt.Sprintf("compact_%04d_%s", len(replace), file.name))
			if err := os.WriteFile(target, compact, 0o644); err != nil {
				return nil, stats, fmt.Errorf("writing %s: %w", target, err)
			}
			replace[file.path] = target
			stats.files++
			stats.literals += literals
			stats.entries += entries
		}
	}
	if stats.literals == 0 {
		return nil, stats, fmt.Errorf("%w: no text table was found", errCatalogCompactUnavailable)
	}
	return replace, stats, nil
}

// compactSource returns source with each large text table written compactly,
// and how many tables and entries that took. A source with none is returned
// unchanged with a zero count.
func compactSource(name string, source []byte) ([]byte, int, int, error) {
	identifiers, err := sourceIdentifiers(name, source)
	if err != nil {
		return nil, 0, 0, err
	}
	return compactSourceWithAlias(name, source, uniqueCompactAlias(identifiers))
}

func sourceIdentifiers(name string, source []byte) (map[string]struct{}, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCatalogCompactUnavailable, err)
	}
	identifiers := make(map[string]struct{})
	for _, spec := range file.Imports {
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		} else if importPath, err := strconv.Unquote(spec.Path.Value); err == nil {
			name = filepath.Base(importPath)
		}
		if name != "" {
			identifiers[name] = struct{}{}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok {
			identifiers[identifier.Name] = struct{}{}
		}
		return true
	})
	return identifiers, nil
}

func uniqueCompactAlias(identifiers map[string]struct{}) string {
	for candidate, suffix := compactImportAlias, 0; ; suffix++ {
		if _, used := identifiers[candidate]; !used {
			return candidate
		}
		candidate = compactImportAlias + strconv.Itoa(suffix+1)
	}
}

func compactSourceWithAlias(name string, source []byte, importAlias string) ([]byte, int, int, error) {
	// A checkout with CRLF line endings holds the same source.
	source = bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
	if !bytes.Contains(source, []byte("map[string]")) {
		return source, 0, 0, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%w: %v", errCatalogCompactUnavailable, err)
	}
	alias := localizeAlias(file)
	offset := func(n ast.Node) int { return fset.Position(n.Pos()).Offset }
	end := func(n ast.Node) int { return fset.Position(n.End()).Offset }
	text := func(n ast.Node) string { return string(source[offset(n):end(n)]) }

	type edit struct {
		start, end  int
		replacement string
	}
	var edits []edit
	literals, entries := 0, 0
	var parents []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			if len(parents) > 0 {
				parents = parents[:len(parents)-1]
			}
			return false
		}
		parents = append(parents, node)
		literal, ok := node.(*ast.CompositeLit)
		if !ok || literal.Type == nil || len(literal.Elts) == 0 {
			return true
		}
		insideFunction := false
		for index, parent := range parents {
			switch ancestor := parent.(type) {
			case *ast.FuncDecl:
				insideFunction = true
			case *ast.FuncLit:
				// A directly-invoked package initializer literal runs once. Other
				// closures may run repeatedly, so keep their encoded text unzipped.
				if index == 0 {
					insideFunction = true
					continue
				}
				call, immediate := parents[index-1].(*ast.CallExpr)
				if !immediate || call.Fun != ancestor {
					insideFunction = true
				}
			}
		}
		gzipPayload := !insideFunction
		replacement, count := compactLiteral(literal, text, alias, importAlias, gzipPayload)
		if count == 0 {
			return true
		}
		edits = append(edits, edit{offset(literal), end(literal), replacement})
		literals++
		entries += count
		// ast.Inspect does not call back with nil when a visitor returns false.
		parents = parents[:len(parents)-1]
		return false
	})
	if literals == 0 {
		return source, 0, 0, nil
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	position := 0
	for _, e := range edits {
		out.Write(source[position:e.start])
		out.WriteString(e.replacement)
		position = e.end
	}
	out.Write(source[position:])
	spliced := out.Bytes()
	// The replacement calls localize under its own name, which this file has
	// not imported. A further import declaration right after the package clause
	// is always valid, and no edit starts before it.
	clauseEnd := offset(file.Name) + len(file.Name.Name)
	insert := "\n\nimport " + importAlias + " " + strconv.Quote(localizeImportPath) + "\n"
	spliced = append(append(append([]byte{}, spliced[:clauseEnd]...), insert...), spliced[clauseEnd:]...)
	if alias != "" && alias != "_" && alias != "." {
		// The file's own import may have been used only inside the tables.
		spliced = append(spliced, []byte("\n\nvar _ "+alias+".Message\n")...)
	}
	formatted, err := format.Source(spliced)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%w: %s does not parse once replaced: %v", errCatalogCompactUnavailable, name, err)
	}
	return formatted, literals, entries, nil
}

// localizeAlias is the name the file imports localize under, or "".
func localizeAlias(file *ast.File) string {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != localizeImportPath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		return "localize"
	}
	return ""
}

// compactLiteral writes one text table compactly and says how many entries it
// holds; zero means the literal is not a table this replaces.
func compactLiteral(literal *ast.CompositeLit, text func(ast.Node) string, alias, importAlias string, gzipPayload bool) (string, int) {
	kind := strings.Join(strings.Fields(text(literal.Type)), "")
	switch {
	case kind == "map[string][3]string":
		return compactTriples(literal, importAlias, gzipPayload)
	case kind == "map[string]map[string]string":
		return compactNestedStrings(literal, text, importAlias, gzipPayload)
	case kind == "map[string]string":
		return compactMap(literal, text, "", mapOfStrings, importAlias, gzipPayload)
	case alias != "" && alias != "_" && alias != "." && kind == "map[string]"+alias+".Message":
		return compactMap(literal, text, alias, mapOfMessages, importAlias, gzipPayload)
	case alias != "" && alias != "_" && alias != "." && kind == "map[string]map[string]"+alias+".Message":
		return compactNested(literal, text, alias, importAlias, gzipPayload)
	}
	return "", 0
}

// compactTriples handles map[string][3]string copy tables. Every value must
// provide all three locale strings explicitly; indexed, omitted, dynamic, or
// duplicate entries stay as source literals.
func compactTriples(literal *ast.CompositeLit, importAlias string, gzipPayload bool) (string, int) {
	if len(literal.Elts) < compactMinimumLength {
		return "", 0
	}
	entries := make([][4]string, 0, len(literal.Elts))
	seen := make(map[string]struct{}, len(literal.Elts))
	for _, element := range literal.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return "", 0
		}
		key, ok := plainStringLiteral(entry.Key)
		if !ok || strings.ContainsRune(key, '\x00') {
			return "", 0
		}
		if _, duplicate := seen[key]; duplicate {
			return "", 0
		}
		seen[key] = struct{}{}
		values, ok := entry.Value.(*ast.CompositeLit)
		if !ok || values.Type != nil || len(values.Elts) != 3 {
			return "", 0
		}
		triple := [4]string{key}
		for i, value := range values.Elts {
			triple[i+1], ok = plainStringLiteral(value)
			if !ok || strings.ContainsRune(triple[i+1], '\x00') {
				return "", 0
			}
		}
		entries = append(entries, triple)
	}
	data, ok := localize.EncodeCompactTriples(entries)
	if !ok {
		return "", 0
	}
	return compactPayloadCall(importAlias, "Triples", data, gzipPayload) + ")", len(entries)
}

// compactNestedStrings handles per-locale plain-string tables. All locales
// and entries must be literal and unique so unsupported source keeps its
// original map behavior without partial conversion.
func compactNestedStrings(literal *ast.CompositeLit, text func(ast.Node) string, importAlias string, gzipPayload bool) (string, int) {
	if len(literal.Elts) == 0 {
		return "", 0
	}
	type localeTable struct {
		locale string
		data   string
	}
	tables := make([]localeTable, 0, len(literal.Elts))
	seenLocales := make(map[string]struct{}, len(literal.Elts))
	total := 0
	for _, element := range literal.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return "", 0
		}
		locale, ok := plainStringLiteral(entry.Key)
		if !ok || strings.ContainsRune(locale, '\x00') {
			return "", 0
		}
		if _, duplicate := seenLocales[locale]; duplicate {
			return "", 0
		}
		seenLocales[locale] = struct{}{}
		inner, ok := entry.Value.(*ast.CompositeLit)
		if !ok || inner.Type != nil {
			return "", 0
		}
		pairs := make([][2]string, 0, len(inner.Elts))
		seenKeys := make(map[string]struct{}, len(inner.Elts))
		for _, innerElement := range inner.Elts {
			pair, ok := innerElement.(*ast.KeyValueExpr)
			if !ok {
				return "", 0
			}
			key, keyOK := plainStringLiteral(pair.Key)
			value, valueOK := plainStringLiteral(pair.Value)
			if !keyOK || !valueOK || strings.ContainsRune(key, '\x00') || strings.ContainsRune(value, '\x00') {
				return "", 0
			}
			if _, duplicate := seenKeys[key]; duplicate {
				return "", 0
			}
			seenKeys[key] = struct{}{}
			pairs = append(pairs, [2]string{key, value})
		}
		data, ok := localize.EncodeCompact(pairs)
		if !ok {
			return "", 0
		}
		tables = append(tables, localeTable{locale: locale, data: data})
		total += len(pairs)
	}
	if total < compactMinimumLength {
		return "", 0
	}
	var out strings.Builder
	out.WriteString(text(literal.Type) + "{")
	for _, table := range tables {
		out.WriteString(strconv.Quote(table.locale) + ": " + compactPayloadCall(importAlias, "Strings", table.data, gzipPayload) + ", nil),")
	}
	out.WriteByte('}')
	return out.String(), total
}

func plainStringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

type mapKind int

const (
	mapOfStrings mapKind = iota
	mapOfMessages
)

// compactMap handles map[string]string and map[string]localize.Message
// literals. Entries that are not plain stay as the literal they were.
func compactMap(literal *ast.CompositeLit, text func(ast.Node) string, alias string, kind mapKind, importAlias string, gzipPayload bool) (string, int) {
	pairs, residue, safe := splitEntries(literal.Elts, text, kind)
	if !safe {
		return "", 0
	}
	if len(pairs) < compactMinimumLength {
		return "", 0
	}
	data, ok := localize.EncodeCompact(pairs)
	if !ok {
		return "", 0
	}
	extra := "nil"
	if len(residue) > 0 {
		extra = text(literal.Type) + "{\n" + strings.Join(residue, ",\n") + ",\n}"
	}
	suffix := "Strings"
	if kind == mapOfMessages {
		suffix = ""
	}
	return compactPayloadCall(importAlias, suffix, data, gzipPayload) + ", " + extra + ")", len(pairs)
}

// compactNested handles map[string]map[string]localize.Message: the product
// catalog, one inner literal per locale.
func compactNested(literal *ast.CompositeLit, text func(ast.Node) string, alias, importAlias string, gzipPayload bool) (string, int) {
	var out strings.Builder
	out.WriteString(text(literal.Type) + "{\n")
	total := 0
	seenLocales := make(map[string]struct{}, len(literal.Elts))
	for _, element := range literal.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return "", 0
		}
		locale, ok := plainStringLiteral(entry.Key)
		if !ok || strings.ContainsRune(locale, '\x00') {
			return "", 0
		}
		if _, duplicate := seenLocales[locale]; duplicate {
			return "", 0
		}
		seenLocales[locale] = struct{}{}
		inner, ok := entry.Value.(*ast.CompositeLit)
		if !ok || inner.Type != nil {
			return "", 0
		}
		pairs, residue, safe := splitEntries(inner.Elts, text, mapOfMessages)
		if !safe {
			return "", 0
		}
		data, ok := localize.EncodeCompact(pairs)
		if !ok {
			return "", 0
		}
		total += len(pairs)
		out.WriteString(text(entry.Key) + ": " + compactPayloadCall(importAlias, "", data, gzipPayload) + ", map[string]" + alias + ".Message{")
		for _, kept := range residue {
			out.WriteString("\n" + kept + ",")
		}
		out.WriteString("}),\n")
	}
	out.WriteString("}")
	if total < compactMinimumLength {
		return "", 0
	}
	return out.String(), total
}

// splitEntries preserves residue values but refuses any map whose keys cannot
// be proven literal and unique. Otherwise a compacted entry could collide with
// a retained expression and change the source map's compile-time duplicate
// check or last-write behavior.
func splitEntries(elements []ast.Expr, text func(ast.Node) string, kind mapKind) (pairs [][2]string, residue []string, safe bool) {
	seen := make(map[string]struct{}, len(elements))
	safe = true
	for _, element := range elements {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return nil, nil, false
		}
		key, ok := plainStringLiteral(entry.Key)
		if !ok || strings.ContainsRune(key, '\x00') {
			return nil, nil, false
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, nil, false
		}
		seen[key] = struct{}{}
		if key, value, plain := plainEntry(element, kind); plain {
			if _, ok := localize.EncodeCompact([][2]string{{key, value}}); ok {
				pairs = append(pairs, [2]string{key, value})
				continue
			}
		}
		residue = append(residue, text(element))
	}
	return pairs, residue, safe
}

// plainEntry reads "key": "text" (for a string table) or "key": {Text: "text"}
// (for a message table).
func plainEntry(element ast.Expr, kind mapKind) (key, value string, ok bool) {
	entry, isEntry := element.(*ast.KeyValueExpr)
	if !isEntry {
		return "", "", false
	}
	keyLiteral, isLiteral := entry.Key.(*ast.BasicLit)
	if !isLiteral || keyLiteral.Kind != token.STRING {
		return "", "", false
	}
	var valueLiteral *ast.BasicLit
	if kind == mapOfStrings {
		valueLiteral, _ = entry.Value.(*ast.BasicLit)
	} else {
		message, isMessage := entry.Value.(*ast.CompositeLit)
		if !isMessage || message.Type != nil || len(message.Elts) != 1 {
			return "", "", false
		}
		field, isField := message.Elts[0].(*ast.KeyValueExpr)
		if !isField {
			return "", "", false
		}
		if name, isName := field.Key.(*ast.Ident); !isName || name.Name != "Text" {
			return "", "", false
		}
		valueLiteral, _ = field.Value.(*ast.BasicLit)
	}
	if valueLiteral == nil || valueLiteral.Kind != token.STRING {
		return "", "", false
	}
	unquotedKey, keyErr := strconv.Unquote(keyLiteral.Value)
	unquotedValue, valueErr := strconv.Unquote(valueLiteral.Value)
	if keyErr != nil || valueErr != nil {
		return "", "", false
	}
	return unquotedKey, unquotedValue, true
}

func compressCompactPayload(data string) string {
	var encoded bytes.Buffer
	writer, err := gzip.NewWriterLevel(&encoded, gzip.BestCompression)
	if err != nil {
		panic(err)
	}
	writer.Header.ModTime = time.Time{}
	writer.Header.Name = ""
	writer.Header.Comment = ""
	writer.Header.Extra = nil
	writer.Header.OS = 255
	if _, err := writer.Write([]byte(data)); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return encoded.String()
}

func compactPayloadCall(importAlias, suffix, data string, gzipPayload bool) string {
	method := "ExpandCompact" + suffix
	payload := data
	if gzipPayload {
		method = "ExpandGzipCompact" + suffix
		payload = compressCompactPayload(data)
	}
	call := importAlias + "." + method + "(" + strconv.Quote(payload)
	if gzipPayload {
		call += ", " + strconv.Itoa(len(data))
	}
	return call
}
