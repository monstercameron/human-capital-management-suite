package chatui

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestDocTokenAt(t *testing.T) {
	for _, tc := range []struct {
		value, query string
		start        int
		ok           bool
	}{
		{"see [[onb", "onb", 4, true},
		{"[[", "", 0, true},
		{"see doc:hand", "hand", 4, true},
		{"(doc:x", "x", 1, true},
		{"héllo [[plan", "plan", 6, true},
		{"see [[a]] now", "", 0, false},
		{"mydoc:x", "", 0, false},
		{"doc:a b", "", 0, false},
		{"plain text", "", 0, false},
	} {
		caret := len(utf16.Encode([]rune(tc.value)))
		query, start, ok := docTokenAt(tc.value, caret)
		if query != tc.query || start != tc.start || ok != tc.ok {
			t.Fatalf("docTokenAt(%q) = %q %d %v", tc.value, query, start, ok)
		}
	}
	value := "see [[onb"
	start := 4
	got, caret := applyDocSuggestion(value, start, len(utf16.Encode([]rune(value))), "doc-1")
	if got != "see doc:doc-1 " || caret != len(utf16.Encode([]rune(got))) {
		t.Fatalf("applied = %q %d", got, caret)
	}
	if refs := DocReferences(got, "https://hcm.example"); len(refs) != 1 || refs[0].ID != "doc-1" {
		t.Fatalf("inserted reference is not recognized: %+v", refs)
	}
}

func TestDocSuggestMenuListbox(t *testing.T) {
	m := Model{}
	if closed := renderNode(t, docSuggestMenu(m, docSuggestState{}, "chat-composer")); closed != `<div class="doc-suggest-slot"></div>` {
		t.Fatalf("closed = %s", closed)
	}
	state := docSuggestState{Target: "chat-composer", Open: true, Active: 1, Items: []DocSuggestion{{ID: "d1", Title: "Onboarding", Detail: "Ana"}, {ID: "d2", Title: "Leave policy"}}}
	out := renderNode(t, docSuggestMenu(m, state, "chat-composer"))
	for _, want := range []string{`role="listbox"`, `id="chat-composer-docs"`, `id="chat-composer-doc-2"`, `aria-selected="true"`, `data-action="doc-suggest-pick"`, `>Onboarding<`, `>Leave policy<`} {
		if !strings.Contains(out, want) {
			t.Fatalf("menu omitted %q: %s", want, out)
		}
	}
	if strings.Count(out, `aria-selected="true"`) != 1 || strings.Contains(out, "style=") {
		t.Fatalf("menu = %s", out)
	}
	if other := renderNode(t, docSuggestMenu(m, state, "thread-composer")); !strings.Contains(other, "doc-suggest-slot") {
		t.Fatalf("another composer drew the list: %s", other)
	}
	empty := renderNode(t, docSuggestMenu(m, docSuggestState{Target: "chat-composer", Open: true}, "chat-composer"))
	if !strings.Contains(empty, `role="status"`) {
		t.Fatalf("empty = %s", empty)
	}
	aria := docSuggestFieldAria(state, "chat-composer", map[string]string{"describedby": "composer-help"})
	if aria["activedescendant"] != "chat-composer-doc-2" || aria["controls"] != "chat-composer-docs" || aria["describedby"] != "composer-help" {
		t.Fatalf("aria = %v", aria)
	}
	if plain := docSuggestFieldAria(docSuggestState{}, "chat-composer", map[string]string{"x": "y"}); len(plain) != 1 {
		t.Fatalf("closed aria = %v", plain)
	}
}

func TestDocReferenceNavigatesInApp(t *testing.T) {
	var went string
	m := Model{Callbacks: Callbacks{Navigate: func(href string) { went = href }}}
	if !docReferenceNavigate(m, "doc-1", true) || went != "/workspace/app/docs?document=doc-1" {
		t.Fatalf("navigated to %q", went)
	}
	went = ""
	if docReferenceNavigate(m, "doc-1", false) || docReferenceNavigate(m, "../x", true) || docReferenceNavigate(Model{}, "doc-1", true) || went != "" {
		t.Fatalf("modified click, bad ID or no router navigated: %q", went)
	}
}
