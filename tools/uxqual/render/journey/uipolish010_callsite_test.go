package journey

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_010_NoProductionStatusIconBypass(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	for _, name := range []string{"page_components.go", "components.go", "people.go"} {
		contents, err := os.ReadFile(filepath.Join(filepath.Dir(filename), name))
		if err != nil {
			t.Fatal(err)
		}
		for _, direct := range []string{"iconInfo(", "iconWarning(", "iconDanger(", "iconSuccess("} {
			if strings.Contains(string(contents), direct) {
				t.Errorf("%s retains direct production status icon %s", name, direct)
			}
		}
	}
}

// TestTodo_UIPOLISH_010_NoProductionIconBypass closes the file-list hole in
// the status-icon guard above it: the old test names three files, so a new
// production file could embed a governed glyph without tripping it. Every
// non-test Go file in this package must resolve glyphs through RenderIcon
// (or the tone/severity resolvers that route through it); only icons.go may
// define the builders and only icon_registry.go may dispatch them.
func TestTodo_UIPOLISH_010_NoProductionIconBypass(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	entries, err := os.ReadDir(filepath.Dir(filename))
	if err != nil {
		t.Fatal(err)
	}
	definers := map[string]bool{"icons.go": true, "icon_registry.go": true}
	direct := []string{
		"iconCheck(", "iconInfo(", "iconSuccess(", "iconWarning(", "iconDanger(",
		"iconLedger(", "iconEmpty(", "iconClock(", "iconPerson(", "iconSpark(",
		"iconArrowRight(", "iconArrowLeft(", "brandMark(", "brandWordmark(",
		"builtInIcon(",
	}
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if definers[name] {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(filepath.Dir(filename), name))
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		for _, call := range direct {
			if strings.Contains(string(contents), call) {
				t.Errorf("%s bypasses the governed icon registry with %s", name, call)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("icon bypass scan covered no production files")
	}
}
