package productui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_WEB_018_NoExplicitSmoothScroll is the #1934 adversarial-review
// regression: an explicit smooth scrollIntoView behavior overrides the CSS
// scroll-behavior kill-switch, so shipped product WASM must never declare
// one. Scroll sites use behavior auto, which defers to the platform
// reduced-motion contract.
func TestTodo_WEB_018_NoExplicitSmoothScroll(t *testing.T) {
	files, err := filepath.Glob("*_wasm.go")
	if err != nil {
		t.Fatalf("glob wasm sources: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no shipped wasm sources found; the scan is vacuous")
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, banned := range []string{`"behavior": "smooth"`, `"behavior":"smooth"`} {
			if strings.Contains(string(body), banned) {
				t.Errorf("%s declares explicit smooth scrolling (%s), bypassing the reduced-motion kill-switch; use behavior auto", file, banned)
			}
		}
	}
}
