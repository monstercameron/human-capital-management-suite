package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestDocsSuggestTrigger(t *testing.T) {
	for _, tc := range []struct {
		before, kind, query string
		start               int
		ok                  bool
	}{
		{"Ask @raf", DocsSuggestPeople, "raf", 4, true},
		{"@", DocsSuggestPeople, "", 0, true},
		{"(#peo", DocsSuggestChannels, "peo", 1, true},
		{"See [[onb", DocsSuggestDocs, "onb", 4, true},
		{"See [[Leave policy", DocsSuggestDocs, "Leave policy", 4, true},
		{"mail rafael@exa", "", "", 0, false},
		{"issue a#3", "", "", 0, false},
		{"done [[x]] then", "", "", 0, false},
		{"@raf ", "", "", 0, false},
	} {
		kind, query, start, ok := docsSuggestTrigger(tc.before)
		if kind != tc.kind || query != tc.query || start != tc.start || ok != tc.ok {
			t.Fatalf("trigger(%q) = %q %q %d %v", tc.before, kind, query, start, ok)
		}
	}
}

func TestDocsSuggestApply(t *testing.T) {
	next, at, ok := docsSuggestApply("Ask @raf now", 8, DocsSuggestPeople, "@rafael.torres")
	if !ok || next != "Ask @rafael.torres now" || at != len("Ask @rafael.torres ") {
		t.Fatalf("person = %q %d %v", next, at, ok)
	}
	// The formatted pane's Markdown escapes the trigger.
	md := `See \[\[onb`
	next, at, ok = docsSuggestApply(md, len(md), DocsSuggestDocs, DocsSuggestDocInsert("doc-1", "Onboarding [v2]"))
	if !ok || next != `See [Onboarding \[v2\]](doc:doc-1) ` || at != len(next) {
		t.Fatalf("doc = %q %d %v", next, at, ok)
	}
	md = `\#peo`
	next, _, ok = docsSuggestApply(md, len(md), DocsSuggestChannels, DocsSuggestChannelInsert("conv-1", "people-ops"))
	if !ok || next != "[#people-ops](channel:conv-1) " {
		t.Fatalf("channel = %q %v", next, ok)
	}
	if _, _, ok := docsSuggestApply("no trigger", 10, DocsSuggestPeople, "@x"); ok {
		t.Fatal("applied without a trigger")
	}
	if got := DocsSuggestPersonInsert("hc-9", "Sam Lee", true); got != "@hc-9" {
		t.Fatalf("shared handle = %q", got)
	}
	if got := DocsSuggestPersonInsert("hc-8", "Sam Lee", false); got != "@sam.lee" {
		t.Fatalf("handle = %q", got)
	}
}

func TestDocsSuggestControllerKeys(t *testing.T) {
	var published []docsSuggestState
	var asked []string
	var answer func([]DocsReferenceSuggestion)
	var picked DocsReferenceSuggestion
	s := &docsSuggestController{
		fetch: func(kind, query string, done func([]DocsReferenceSuggestion)) {
			asked = append(asked, kind+":"+query)
			answer = done
		},
		publish: func(state docsSuggestState) { published = append(published, state) },
		onPick:  func(item DocsReferenceSuggestion, kind string) { picked = item },
	}
	if s.key("ArrowDown") {
		t.Fatal("closed list took a key")
	}
	s.update(DocsSuggestPeople, "ra", true)
	if !s.state.Open || !s.state.Loading || len(asked) != 1 {
		t.Fatalf("open = %+v, asked %v", s.state, asked)
	}
	stale := answer
	s.update(DocsSuggestPeople, "raf", true)
	stale([]DocsReferenceSuggestion{{ID: "stale"}})
	if len(s.state.Items) != 0 {
		t.Fatal("a stale answer landed")
	}
	answer([]DocsReferenceSuggestion{{ID: "a", Insert: "@a"}, {ID: "b", Insert: "@b"}})
	if s.state.Loading || len(s.state.Items) != 2 {
		t.Fatalf("answered = %+v", s.state)
	}
	if !s.key("ArrowUp") || s.state.Active != 1 || !s.key("ArrowDown") || s.state.Active != 0 || !s.key("ArrowDown") {
		t.Fatalf("arrows = %+v", s.state)
	}
	if !s.key("Enter") || picked.ID != "b" || s.state.Open {
		t.Fatalf("enter picked %+v, state %+v", picked, s.state)
	}
	s.update(DocsSuggestDocs, "x", true)
	if !s.key("Escape") || s.state.Open || s.key("Escape") {
		t.Fatalf("escape = %+v", s.state)
	}
	s.update("", "", false)
	if len(published) == 0 || published[len(published)-1].Open {
		t.Fatalf("published = %+v", published)
	}
}

func TestDocsSuggestListRendersListbox(t *testing.T) {
	s := &docsSuggestController{}
	out, err := ui.RenderToString(docsSuggestList(docsSuggestListProps{Locale: "en-US", Controller: s}))
	if err != nil || !strings.Contains(out, `role="listbox"`) || !strings.Contains(out, "hidden") || strings.Contains(out, "style=") {
		t.Fatalf("closed list = %s, %v", out, err)
	}
	if !strings.Contains(docsSuggestStylesheet(), "inset-inline-start:var(--docs-suggest-start") || !strings.Contains(docsSuggestStylesheet(), "--hcm-color-focus") {
		t.Fatal("list is not placed by its custom properties")
	}
	if !strings.Contains(docsChatRefsStylesheet(), ".docs-suggest{") {
		t.Fatal("suggest styles not in the chat-reference sheet")
	}
}

// The reference syntax survives the formatted pane: Markdown to HTML and
// back gives the same text, so an edit in either pane keeps every chip.
func TestDocsChatReferencesRoundTripThroughEditor(t *testing.T) {
	for _, markdown := range []string{
		"Ask in #people-ops and @rafael.torres or @hc-050-rafael-torres.\n",
		"See [#leads](channel:conv-2) and [@Ana Lopez](person:hc-7).\n",
		"Decision: /workspace/app/chat#share=tok_A1\n",
		"Read [Onboarding guide](doc:doc-1).\n",
	} {
		got, rendered := docsEditorRoundTrip(t, markdown)
		if got != markdown {
			t.Fatalf("round trip changed %q to %q (html %s)", markdown, got, rendered)
		}
	}
}
