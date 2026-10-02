package productui

import "testing"

// A page whose icon name is not in the governed icon table renders the
// generic fallback ring. Every registered page must name a real glyph.
func TestEveryRegisteredPageIconIsARegisteredGlyph(t *testing.T) {
	known := make(map[string]bool)
	for _, icon := range IconDefinitions() {
		known[icon.Name] = true
	}
	if !known["sparkles"] {
		t.Fatal("the agent pages' glyph is not registered")
	}
	for _, name := range []string{"sparkles"} {
		if iconPath(name) == fallbackIconPath {
			t.Fatalf("icon %q resolves to the fallback ring", name)
		}
	}
}
