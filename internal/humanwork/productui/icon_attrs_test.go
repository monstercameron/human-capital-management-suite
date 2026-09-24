package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// productIconPerRenderMaps is productIcon as it was before the shared
// attribute maps: two fresh maps per icon per render.
func productIconPerRenderMaps(name, class string) ui.Node {
	return html.Tag("svg", html.Props{Class: class, Raw: map[string]any{
		"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
		"stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false",
	}}, html.Tag("path", html.Props{Raw: map[string]any{"d": iconPath(name)}}))
}

// Sharing the attribute maps must not change a byte of the markup, for
// registered icons and the fallback ring alike.
func TestProductIconSharedAttrsRenderSameMarkup(t *testing.T) {
	for _, name := range []string{"folder", "favorite", "more", "history-back", "not-a-registered-icon"} {
		for _, class := range []string{"docs-menu-icon", "nav-icon", ""} {
			got := docsFocusRender(t, productIcon(name, class))
			want := docsFocusRender(t, productIconPerRenderMaps(name, class))
			if got != want {
				t.Fatalf("%s/%s markup changed:\n got %s\nwant %s", name, class, got, want)
			}
		}
	}
}

// The shared maps are read-only: rendering many icons, with classes and in
// a whole document list, leaves them exactly as declared.
func TestProductIconSharedAttrsStayUnmodified(t *testing.T) {
	before := len(productIconSVGAttrs)
	for i := 0; i < 3; i++ {
		_ = docsFocusRender(t, productIcon("folder", "a"))
		_ = docsFocusRender(t, productIcon("link", "b"))
	}
	if _, err := Render(docsFocusView()); err != nil {
		t.Fatal(err)
	}
	if len(productIconSVGAttrs) != before || productIconSVGAttrs["class"] != nil || productIconSVGAttrs["xmlns"] != nil {
		t.Fatalf("shared svg attributes were written to: %v", productIconSVGAttrs)
	}
	if got := productIconPathAttr("folder")["d"]; got != iconPath("folder") || len(productIconPathAttr("folder")) != 1 {
		t.Fatalf("shared path attributes were written to: %v", productIconPathAttr("folder"))
	}
	if productIconPathAttr("nope")["d"] != fallbackIconPath {
		t.Fatal("unknown icons must draw the fallback ring")
	}
}

// The search box re-renders the list only when a keystroke changes whether
// a query is typed.
func TestDocsTypedSearching(t *testing.T) {
	for typed, want := range map[string]bool{"": false, "   ": false, "p": true, " policy ": true} {
		if got := docsTypedSearching(typed); got != want {
			t.Fatalf("docsTypedSearching(%q) = %v, want %v", typed, got, want)
		}
	}
	// A run of keystrokes inside one query flips the state once, on the
	// first character, so the list renders once rather than per key.
	flips, was := 0, false
	for _, value := range []string{"p", "po", "pol", "poli", "polic", "policy"} {
		if now := docsTypedSearching(value); now != was {
			flips++
			was = now
		}
	}
	if flips != 1 {
		t.Fatalf("typing one word re-rendered the list %d times, want 1", flips)
	}
}

// The document markup still carries every highlight rule, now bound to the
// reader's text so other pages' elements skip the highlight pseudo styles.
func TestDocsHighlightRulesAreScopedToTheReader(t *testing.T) {
	css := docsStylesheet()
	for _, name := range []string{"docs-anchor", "docs-anchor-active", "docs-anchor-flash", "docs-anchor-draft"} {
		scoped := "#docs-markdown::highlight(" + name + "),#docs-markdown ::highlight(" + name + "){"
		if !strings.Contains(css, scoped) {
			t.Fatalf("highlight %s is not scoped to #docs-markdown", name)
		}
		if strings.Contains(css, "\n::highlight("+name+")") || strings.Contains(css, "}::highlight("+name+")") {
			t.Fatalf("highlight %s still applies to every element on every page", name)
		}
	}
}

func BenchmarkProductIcon(b *testing.B) {
	b.Run("shared-attrs", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = productIcon("folder", "docs-menu-icon")
		}
	})
	b.Run("per-render-maps", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = productIconPerRenderMaps("folder", "docs-menu-icon")
		}
	})
}

// BenchmarkDocsLibraryRender25 renders a full page of 25 documents, each
// row with its star, folder tag, access glyph and row menu icons.
func BenchmarkDocsLibraryRender25(b *testing.B) {
	view := docsFocusView()
	base := view.Documents
	view.Documents = nil
	for i := 0; i < 25; i++ {
		doc := base[i%len(base)]
		doc.ID = doc.ID + "-" + string(rune('a'+i))
		doc.FolderID = "f-1"
		view.Documents = append(view.Documents, doc)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Render(view); err != nil {
			b.Fatal(err)
		}
	}
}
