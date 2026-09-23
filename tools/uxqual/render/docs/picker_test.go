package docs

import (
	"strings"
	"testing"
)

// TestTodo_HUB_035 is the PRIMARY test for HUB-035: the picker submits
// stable document IDs and the backlinks view shows safe target states
// with restricted sources title-free.
func TestTodo_HUB_035(t *testing.T) {
	picker, err := RenderPicker(PickerPage{
		Locale: "en-US", Title: "Guide",
		Docs: []PickerDoc{{ID: "doc-b", Title: "Bee"}, {ID: "doc-c", Title: "Sea"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`value="doc-b"`, `value="doc-c"`, "Bee", `name="target_doc"`, `name="target_block"`, "doc:"} {
		if !strings.Contains(picker, want) {
			t.Fatalf("picker missing %q", want)
		}
	}
	if strings.Contains(picker, "[[Bee]]") {
		t.Fatal("picker offers title-only links")
	}
	back, err := RenderBacklinks(BacklinksPage{
		Locale: "en-US", Title: "Bee", TargetDocID: "doc-b",
		Links: []BacklinkEntry{
			{SourceDocID: "doc-a", SourceTitle: "Ay", Label: "bee", State: "valid"},
			{SourceDocID: "doc-x", Label: "bee", State: "stale", Restricted: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"doc-a", "Ay", "bee", "valid", "stale", "Restricted"} {
		if !strings.Contains(back, want) {
			t.Fatalf("backlinks missing %q", want)
		}
	}
	if strings.Contains(back, "doc-x-title") {
		t.Fatal("restricted source title leaked")
	}
}

// TestTodo_HUB_035_Accessibility is the ACCESSIBILITY test for HUB-035:
// the picker is a labelled native control with instructions, and states
// are text, not color alone.
func TestTodo_HUB_035_Accessibility(t *testing.T) {
	picker, err := RenderPicker(PickerPage{Locale: "de-DE", Title: "Anleitung", Docs: []PickerDoc{{ID: "doc-b", Title: "Biene"}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := parseDoc(t, picker)
	sel := findByID(doc, "target-doc")
	if sel == nil || sel.Data != "select" {
		t.Fatal("native select#target-doc missing")
	}
	if attr(sel, "aria-describedby") == "" {
		t.Fatal("picker instructions not associated")
	}
	if !strings.Contains(picker, "<h1") || !strings.Contains(picker, "<label") {
		t.Fatal("picker heading or label missing")
	}
	back, err := RenderBacklinks(BacklinksPage{Locale: "en-US", Title: "Bee", TargetDocID: "doc-b", Links: []BacklinkEntry{{SourceDocID: "doc-a", SourceTitle: "Ay", Label: "bee", State: "broken"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(back, "broken") || !strings.Contains(back, "<ul") {
		t.Fatal("state text or list missing")
	}
}
