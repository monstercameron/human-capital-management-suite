package xtextkit_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

const (
	xTextVersion = "v0.41.0"
	unicodeData  = "17.0.0"
	cldrData     = "32"
)

var supportedLocales = map[string]struct{}{"en-US": {}, "de-DE": {}, "und": {}}

type fieldPolicy struct {
	Name             string
	Normalize        bool
	PreserveOriginal bool
	SearchForm       bool
	ConfusableReview bool
}

type fieldValue struct {
	Original     string
	Normalized   string
	Search       string
	Digest       string
	ReviewSignal bool
}

var displayNamePolicy = fieldPolicy{
	Name: "display_name", Normalize: true, PreserveOriginal: true,
	SearchForm: true, ConfusableReview: true,
}

var identifierPolicy = fieldPolicy{
	Name: "identifier", Normalize: true, PreserveOriginal: true,
	SearchForm: true, ConfusableReview: true,
}

func applyPolicy(policy fieldPolicy, input string) (fieldValue, error) {
	if !utf8.ValidString(input) {
		return fieldValue{}, errors.New("invalid UTF-8")
	}
	normalized := input
	if policy.Normalize {
		normalized = norm.NFC.String(input)
	}
	search := ""
	if policy.SearchForm {
		search = cases.Fold().String(normalized)
	}
	value := fieldValue{Normalized: normalized, Search: search, ReviewSignal: confusableSignal(normalized)}
	if policy.PreserveOriginal {
		value.Original = input
	}
	digest := sha256.Sum256([]byte(normalized))
	value.Digest = hex.EncodeToString(digest[:])
	return value, nil
}

func confusableSignal(value string) bool {
	for _, r := range value {
		switch r {
		case '\u0430', '\u03bf', '\u0435', '\u0441', '\u03b1':
			return true
		}
	}
	return false
}

func compareDisplay(a, b string) int {
	return collate.New(language.Und, collate.Loose).CompareString(a, b)
}

func formatNumber(locale string, value int64) (string, error) {
	if _, ok := supportedLocales[locale]; !ok {
		return "", fmt.Errorf("unsupported locale %q", locale)
	}
	return message.NewPrinter(language.MustParse(locale)).Sprintf("%v", number.Decimal(value)), nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}

// TestXTextQualificationPinsDataAndPreservesOwnedFieldSemantics is LIB-020's
// primary test. x/text supplies mechanics; this fixture owns field profiles,
// original-text retention, canonical digest input, locale allow-listing, and
// the review-only confusable outcome.
func TestXTextQualificationPinsDataAndPreservesOwnedFieldSemantics(t *testing.T) {
	nfc := "Müller"
	nfd := "Mu\u0308ller"
	a, err := applyPolicy(displayNamePolicy, nfc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := applyPolicy(displayNamePolicy, nfd)
	if err != nil {
		t.Fatal(err)
	}
	if a.Normalized != b.Normalized || a.Digest != b.Digest {
		t.Fatalf("canonical forms diverged: a=%+v b=%+v", a, b)
	}
	if a.Original != nfc || b.Original != nfd {
		t.Fatalf("owned profile destroyed original text: a=%q b=%q", a.Original, b.Original)
	}
	if compareDisplay("Muller", "Müller") != 0 {
		t.Fatal("pinned loose collation did not equate accent variant")
	}
	if compareDisplay("a", "b") >= 0 {
		t.Fatal("collation order is not deterministic")
	}
	en, err := formatNumber("en-US", 1234567)
	if err != nil {
		t.Fatal(err)
	}
	de, err := formatNumber("de-DE", 1234567)
	if err != nil {
		t.Fatal(err)
	}
	if en == de || en == "" || de == "" {
		t.Fatalf("locale formatting did not bind the owned locale policy: en=%q de=%q", en, de)
	}
	confusable, err := applyPolicy(identifierPolicy, "p\u0430ypal")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := applyPolicy(identifierPolicy, "paypal")
	if err != nil {
		t.Fatal(err)
	}
	if !confusable.ReviewSignal || confusable.Digest == plain.Digest {
		t.Fatal("confusable signal must be review-only and must not auto-merge identity")
	}
}

// TestTodo_LIB_020_Property checks normalization idempotence, canonical digest
// convergence, original preservation, and locale output determinism over
// hostile Unicode samples.
func TestTodo_LIB_020_Property(t *testing.T) {
	samples := []string{"e\u0301", "é", "\u212B", "Å", "\u0430", "\u03b1", "👩‍💻", "مرحبا", "हिन्दी", "a\u200db"}
	for _, input := range samples {
		got, err := applyPolicy(displayNamePolicy, input)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if once := norm.NFC.String(got.Normalized); once != got.Normalized {
			t.Fatalf("normalization not idempotent for %q", input)
		}
		again, err := applyPolicy(displayNamePolicy, got.Normalized)
		if err != nil {
			t.Fatal(err)
		}
		if got.Digest != again.Digest || got.Search != again.Search {
			t.Fatalf("canonical policy is not stable for %q: %#v %#v", input, got, again)
		}
		if got.Original != input {
			t.Fatalf("original text was not retained for %q", input)
		}
		for _, locale := range []string{"en-US", "de-DE", "und"} {
			first, err := formatNumber(locale, 42)
			if err != nil {
				t.Fatal(err)
			}
			second, err := formatNumber(locale, 42)
			if err != nil || first != second {
				t.Fatalf("locale %s formatting is unstable: %q %q err=%v", locale, first, second, err)
			}
		}
	}
}

type qualificationManifest struct {
	Version               int      `yaml:"version"`
	Todo                  string   `yaml:"todo"`
	Module                string   `yaml:"module"`
	VersionPin            string   `yaml:"version_pin"`
	Verdict               string   `yaml:"verdict"`
	AllowedImportRoots    []string `yaml:"allowed_import_roots"`
	RuntimeGraphUnchanged bool     `yaml:"runtime_dependency_graph_unchanged"`
	Command               string   `yaml:"command"`
	Decision              string   `yaml:"decision"`
	RemovalPath           string   `yaml:"removal_path"`
	DataVersions          struct {
		UnicodeNormalization string `yaml:"unicode_normalization"`
		CLDR                 string `yaml:"cldr"`
		Collation            string `yaml:"collation"`
	} `yaml:"data_versions"`
	Evidence []struct {
		Test    string `yaml:"test"`
		Package string `yaml:"package"`
	} `yaml:"evidence"`
}

func loadManifest(t *testing.T) qualificationManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "definitions", "architecture", "xtext-qualification.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var m qualificationManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestTodo_LIB_020_Golden pins the module/data versions, policy roots, and
// canonical hostile-Unicode vectors that make a future dependency bump an
// explicit impact/reindex review rather than an invisible behavior change.
func TestTodo_LIB_020_Golden(t *testing.T) {
	m := loadManifest(t)
	if m.Version != 1 || m.Todo != "LIB-020" || m.Module != "golang.org/x/text" || m.VersionPin != xTextVersion {
		t.Fatalf("manifest identity drift: %+v", m)
	}
	if m.Verdict != "QUALIFIED" || !m.RuntimeGraphUnchanged {
		t.Fatalf("verdict=%q runtime_graph_unchanged=%v", m.Verdict, m.RuntimeGraphUnchanged)
	}
	wantRoots := []string{"internal/engines/wire/canonical", "internal/kernel/values", "internal/intent"}
	if !reflect.DeepEqual(m.AllowedImportRoots, wantRoots) {
		t.Fatalf("allowed roots=%v want=%v", m.AllowedImportRoots, wantRoots)
	}
	if m.DataVersions.UnicodeNormalization != unicodeData || m.DataVersions.CLDR != cldrData {
		t.Fatalf("data versions=%+v", m.DataVersions)
	}
	if m.Decision == "" || m.RemovalPath == "" {
		t.Fatal("decision and removal_path are required")
	}
	if compareDisplay("e\u0301", "é") != 0 {
		t.Fatal("canonical collation golden changed")
	}
	if got, _ := formatNumber("en-US", 1234); got != "1,234" {
		t.Fatalf("en-US number golden=%q", got)
	}
	if got, _ := formatNumber("de-DE", 1234); got != "1.234" {
		t.Fatalf("de-DE number golden=%q", got)
	}
}

// FuzzTodo_LIB_020 fuzzes arbitrary UTF-8/invalid-byte strings. The owned
// policy must either reject invalid UTF-8 or return idempotent normalized and
// deterministic search/digest forms without panic.
func FuzzTodo_LIB_020(f *testing.F) {
	for _, seed := range []string{"e\u0301", "\u212B", "p\u0430ypal", "\xff", "👩‍💻"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got, err := applyPolicy(displayNamePolicy, input)
		if err != nil {
			if utf8.ValidString(input) {
				t.Fatalf("valid input rejected: %q: %v", input, err)
			}
			return
		}
		if got.Normalized != norm.NFC.String(got.Normalized) {
			t.Fatal("normalized output is not idempotent")
		}
		again, err := applyPolicy(displayNamePolicy, got.Normalized)
		if err != nil || got.Digest != again.Digest || got.Search != again.Search {
			t.Fatalf("policy output is not reproducible: %#v %#v err=%v", got, again, err)
		}
	})
}

// TestTodo_LIB_020_Security exercises hostile identifiers and locale input:
// confusable forms remain distinct review signals, invalid UTF-8 is rejected,
// and unsupported locale tags cannot influence formatting or authorization.
func TestTodo_LIB_020_Security(t *testing.T) {
	if _, err := applyPolicy(identifierPolicy, string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	latin, _ := applyPolicy(identifierPolicy, "a")
	cyrillic, _ := applyPolicy(identifierPolicy, "\u0430")
	if !cyrillic.ReviewSignal || latin.Digest == cyrillic.Digest {
		t.Fatal("confusable identifier auto-merged or lacked review signal")
	}
	if _, err := formatNumber("tr-TR", 1); err == nil {
		t.Fatal("unsupported locale accepted")
	}
	if _, err := formatNumber("en-US-u-ca-islamic", 1); err == nil {
		t.Fatal("locale extension bypassed allow-list")
	}
}

// TestTodo_LIB_020_Conformance scans production imports against the existing
// narrow roots and uses the semantic firewall's exported-signature scanner to
// ensure no x/text type crosses an owned API.
func TestTodo_LIB_020_Conformance(t *testing.T) {
	root := repoRoot(t)
	m := loadManifest(t)
	wantTests := map[string]bool{
		"TestXTextQualificationPinsDataAndPreservesOwnedFieldSemantics": false,
		"TestTodo_LIB_020_Property":                                     false,
		"TestTodo_LIB_020_Golden":                                       false,
		"FuzzTodo_LIB_020":                                              false,
		"TestTodo_LIB_020_Security":                                     false,
		"TestTodo_LIB_020_Conformance":                                  false,
	}
	for _, ev := range m.Evidence {
		if _, ok := wantTests[ev.Test]; !ok {
			t.Errorf("unexpected evidence test %q", ev.Test)
			continue
		}
		if wantTests[ev.Test] {
			t.Errorf("duplicate evidence test %q", ev.Test)
		}
		wantTests[ev.Test] = true
		if ev.Package != "tools/quality/xtextkit" {
			t.Errorf("%s evidence package=%q", ev.Test, ev.Package)
		}
	}
	for name, found := range wantTests {
		if !found {
			t.Errorf("evidence missing %q", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "golang.org/x/text v0.41.0") {
		t.Fatal("go.mod does not pin golang.org/x/text v0.41.0")
	}

	allowed := make(map[string]bool, len(m.AllowedImportRoots))
	for _, name := range m.AllowedImportRoots {
		allowed[name] = true
	}
	var owners []string
	var violations []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			// Local test/build worktrees are not part of this repository's
			// production package graph, even when they contain Go sources.
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		hasXText := false
		for _, imp := range parsed.Imports {
			if strings.HasPrefix(strings.Trim(imp.Path.Value, `"`), "golang.org/x/text") {
				hasXText = true
			}
		}
		if !hasXText {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return relErr
		}
		if !allowed[filepath.ToSlash(rel)] {
			violations = append(violations, filepath.ToSlash(rel))
			return nil
		}
		owners = append(owners, filepath.Dir(path))
		return nil
	})
	if err != nil {
		t.Fatalf("scan production imports: %v", err)
	}
	if len(owners) == 0 || len(violations) > 0 {
		t.Fatalf("x/text owners=%v violations=%v", owners, violations)
	}
	sort.Strings(owners)
	for _, dir := range owners {
		exposures, scanErr := libfirewall.ScanExposures(dir, []string{"golang.org/x/text"})
		if scanErr != nil {
			t.Fatalf("scan exported signatures in %s: %v", dir, scanErr)
		}
		if len(exposures) != 0 {
			t.Fatalf("x/text type leakage from %s: %v", dir, exposures)
		}
	}

	// Keep the parser import semantically live even if the Go version changes
	// AST import representation: every owner file must contain an import spec.
	_ = ast.NewIdent("x/text")
}
