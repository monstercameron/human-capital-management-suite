package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATUX_031: the reading settings are one row (a label and the select
// that holds the language, with a tick after a save), the translate switch is
// its own row, there is no Change button and no second labelled select, and the
// counts and skin tones are laid out as plain text and one row of six.
func TestTodo_CHATUX_031(t *testing.T) {
	render := func(saved bool) string {
		node := RenderingSettings(RenderingSettingsModel{Locale: "en-US", Preference: chatrender.DefaultPreference("en"), AutoSave: true, Saved: saved})
		out, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	page := render(false)
	for _, want := range []string{`class="chat-prefs-head chatux031-row"`, `id="chatrender-reading"`, `Reading language`, `id="chatrender-translate"`, `data-state="help"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the settings do not carry %s", want)
		}
	}
	if strings.Contains(page, "Read messages in") || strings.Contains(page, "chat-prefs-change") || strings.Contains(page, "chatux031-tick") {
		t.Error("the form keeps the second label, the Change button or a tick before any save")
	}
	// The tick's slot is always there, so the select does not move when it appears.
	if !strings.Contains(page, `class="chatux031-end"`) {
		t.Error("the row has no fixed slot for the saved tick")
	}
	if saved := render(true); !strings.Contains(saved, `chatux031-tick`) || !strings.Contains(saved, `data-state="saved"`) {
		t.Error("a saved change draws no tick")
	}
	for _, want := range []string{"min-inline-size:min(220px,calc(100vw - 16px))", "container-type:inline-size", "@container(max-width:250px){.chat-workspace .chat-prefs-head .chat-prefs-title{flex:1 0 100%}", "flex-wrap:wrap;align-items:center", "min-block-size:44px;padding:0 12px", "border-block-start:1px solid var(--line)", "line-height:16px", "@keyframes chatux031-tick{0%,83%{opacity:1}100%{opacity:0}}", "repeat(6,minmax(0,1fr))", ".chatrender-language-count{margin:0;border:0"} {
		if !strings.Contains(ChatUX031Styles, want) {
			t.Errorf("the preferences styles lack %s", want)
		}
	}
}

func TestCHATUX031PrefsMinimumHonorsSidebarPlacement(t *testing.T) {
	column := chatLayerRect{left: 496, top: 0, right: 728, bottom: 900}
	opener := chatLayerRect{left: 688, top: 8, right: 720, bottom: 44}
	placement := chatSidebarLayerPlacement(opener, column, 800, 900, true)
	if placement.width != 220 || placement.left+placement.width > column.right {
		t.Fatalf("800px RTL sidebar placement is %+v, want a 220px panel inside its rail", placement)
	}
	if !strings.Contains(ChatUX031Styles, "min-inline-size:min(220px,calc(100vw - 16px))") || !strings.Contains(ChatUX031Styles, ".chat-prefs-head{display:flex;flex-wrap:wrap") || !strings.Contains(ChatUX031Styles, "@container(max-width:250px)") {
		t.Fatal("Chat preferences can exceed their measured rail width or cannot wrap their rows")
	}
}

// chatux031Selected reports whether an option carries the selected attribute,
// which an HTML parser keeps with an empty value.
func chatux031Selected(n *xhtml.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "selected" {
			return true
		}
	}
	return false
}

// TestTodo_CHATUX_031_DocSourceLine: a linked document's byline names the
// document's source (its collection, or "Policy document" when none is sent) and
// the date, never the person who owns it or who asked.
func TestTodo_CHATUX_031_DocSourceLine(t *testing.T) {
	cases := []struct {
		locale string
		in     DocPreview
		want   string
	}{
		{"en-US", DocPreview{Owner: "Walt Brennan", UpdatedAt: "Oct 1"}, "Policy document · Oct 1"},
		{"en-US", DocPreview{Owner: "Walt Brennan", UpdatedAt: "Oct 1", Collection: "Benefits"}, "Benefits · Oct 1"},
		{"en-US", DocPreview{Owner: "Walt Brennan"}, "Policy document"},
		{"de-DE", DocPreview{UpdatedAt: "1. Okt."}, "Richtliniendokument · 1. Okt."},
		{"ar", DocPreview{UpdatedAt: "1 أكتوبر"}, "مستند سياسة · 1 أكتوبر"},
	}
	for _, c := range cases {
		got := chatux031DocSourceLine(Model{Locale: c.locale}, c.in)
		if got != c.want {
			t.Errorf("%s %+v: byline %q, want %q", c.locale, c.in, got, c.want)
		}
		if strings.Contains(got, "Walt Brennan") {
			t.Errorf("%s: the byline names the owner: %q", c.locale, got)
		}
	}
}

// TestTodo_CHATUX_039: the sources a message copies as text are chips, one per
// source, not a bulleted list; any other list is still a list; a linked-document
// card is neutral and keeps a floor width; the citation marker keeps no
// minimum box that would push the full stop away from it.
func TestTodo_CHATUX_039(t *testing.T) {
	m := Model{Locale: "en-US"}
	link := func(title, id string) string { return "[" + title + "](/workspace/app/docs?document=" + id + ")" }
	body := "Paid time off policy.\n\nSources\n\n- " + link("Paid time off policy · Carryover · v1.0.0", "pto") + "\n- " + link("2026 holiday guide · v1.0.0", "hol")
	markup := renderMarkdownBody(t, m, body)
	if got := strings.Count(markup, `class="agent-reply-source"`); got != 2 || !strings.Contains(markup, `class="md-source-chips"`) {
		t.Errorf("want two source chips in one chip list, got %d: %s", got, markup)
	}
	if strings.Contains(markup, "<ul>") {
		t.Errorf("the sources are still a bulleted list: %s", markup)
	}
	for _, heading := range []string{"Quellen", "المصادر", "**Sources:**"} {
		if !strings.Contains(renderMarkdownBody(t, m, heading+"\n\n- "+link("A", "a")+"\n"), "md-source-chips") {
			t.Errorf("a %q heading does not give chips", heading)
		}
	}
	for _, plain := range []string{
		"Documents\n\n- " + link("A", "a") + "\n- " + link("B", "b"),
		"Sources\n\n- " + link("A", "a") + " and more\n- " + link("B", "b"),
		"Sources\n\n- plain item\n- " + link("B", "b"),
		"- " + link("A", "a"),
	} {
		if markup := renderMarkdownBody(t, m, plain); strings.Contains(markup, "md-source-chips") || !strings.Contains(markup, "<ul>") {
			t.Errorf("%q was turned into chips: %s", plain, markup)
		}
	}
	for _, want := range []string{".chat-workspace .chat-doc-embed,.chat-workspace .chat-doc-embed-wrap>.chat-embed-more{border-inline-start:1px solid var(--line);background:var(--surface)}", "{min-inline-size:min(240px,calc(100vw - 88px))}", ".message-body a:not([class]){display:inline"} {
		if !strings.Contains(ChatUX031Styles, want) {
			t.Errorf("the styles lack %s", want)
		}
	}
	if !strings.Contains(AgentUX075Styles, ".chat-workspace .agent-cite{display:inline;min-width:0;min-height:0}") {
		t.Error("the citation marker can still be given the workspace's 24 px link size")
	}
}
