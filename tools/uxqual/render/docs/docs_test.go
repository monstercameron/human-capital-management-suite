package docs

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseDoc(t *testing.T, page string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func findByID(n *html.Node, id string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if found != nil {
			return
		}
		if x.Type == html.ElementNode {
			for _, a := range x.Attr {
				if a.Key == "id" && a.Val == id {
					found = x
					return
				}
			}
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return found
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// TestTodo_HUB_033 is the PRIMARY test for HUB-033: the editor submits new
// candidates with their expected base, shows a base conflict, and the
// compare flow renders two immutable versions with no editing surface.
func TestTodo_HUB_033(t *testing.T) {
	editor, err := RenderEditor(EditorPage{
		Locale: "en-US", Title: "Guide", DocumentID: "doc-1",
		BaseVersionID: "docv-1", BaseHash: "9e1e",
		Draft: "# Guide\n\nNew words.\n", Action: "/docs/doc-1/candidates",
		Conflict: &BaseConflict{ExpectedVersionID: "docv-1", LiveVersionID: "docv-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`action="/docs/doc-1/candidates"`, `name="markdown"`, `name="base_version" value="docv-1"`,
		`name="base_hash" value="9e1e"`, `role="alert"`, "docv-2", "# Guide",
	} {
		if !strings.Contains(editor, want) {
			t.Fatalf("editor missing %q", want)
		}
	}
	compare, err := RenderCompare(ComparePage{
		Locale: "en-US", Title: "Guide",
		Base:  CompareVersion{VersionID: "docv-1", ShortHash: "9e1e", Title: "Guide", BodyHTML: "<h1>Guide</h1>"},
		Other: CompareVersion{VersionID: "docv-2", ShortHash: "a71f", Title: "Guide", BodyHTML: "<h1>Guide</h1><p>More.</p>"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"docv-1", "docv-2", "9e1e", "a71f", "More."} {
		if !strings.Contains(compare, want) {
			t.Fatalf("compare missing %q", want)
		}
	}
	for _, forbidden := range []string{"<form", "<textarea", "base_version"} {
		if strings.Contains(compare, forbidden) {
			t.Fatalf("compare exposes editing surface %q", forbidden)
		}
	}
}

// TestTodo_HUB_033_Accessibility is the ACCESSIBILITY test for HUB-033:
// labels bind controls, errors associate, conflict announces, locales set
// language and direction, and motion stays optional.
func TestTodo_HUB_033_Accessibility(t *testing.T) {
	editor, err := RenderEditor(EditorPage{
		Locale: "de-DE", Title: "Anleitung", DocumentID: "doc-1",
		BaseVersionID: "docv-1", BaseHash: "9e1e",
		Error:  "Text ist erforderlich.",
		Action: "/docs/doc-1/candidates",
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := parseDoc(t, editor)
	var lang string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "html" {
			lang = attr(n, "lang")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if lang != "de-DE" {
		t.Fatalf("document lang wrong: %q", lang)
	}
	area := findByID(doc, "markdown")
	if area == nil || area.Data != "textarea" {
		t.Fatal("textarea#markdown missing")
	}
	described := attr(area, "aria-describedby")
	if described == "" {
		t.Fatal("error not associated with textarea")
	}
	if findByID(doc, described) == nil {
		t.Fatalf("aria-describedby target %q missing", described)
	}
	var labelFor string
	var findLabel func(*html.Node)
	findLabel = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "label" && attr(n, "for") == "markdown" {
			labelFor = "markdown"
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findLabel(c)
		}
	}
	findLabel(doc)
	if labelFor == "" {
		t.Fatal("label for markdown missing")
	}
	if !strings.Contains(editor, "Text ist erforderlich.") {
		t.Fatal("german error string missing")
	}
	rtl, err := RenderEditor(EditorPage{Locale: "ar", Title: "دليل", DocumentID: "doc-1", Action: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rtl, `dir="rtl"`) || !strings.Contains(rtl, `lang="ar"`) {
		t.Fatal("rtl direction missing")
	}
	for _, want := range []string{"prefers-reduced-motion", ":focus-visible", "label"} {
		if !strings.Contains(editor, want) {
			t.Fatalf("editor missing %q", want)
		}
	}
	if !strings.Contains(editor, "<h1") {
		t.Fatal("editor has no heading")
	}
}
