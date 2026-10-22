package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_043: in an Arabic page every mixed-direction run is its own
// isolate, a paragraph and the list under it share one edge, and a left-to-right
// page is untouched.
func TestTodo_CHATUX_043(t *testing.T) {
	// A skill the server describes in English, beside an Arabic tier.
	skills := []PersonaMentionSkill{{Name: "Search records the requesting user may read. More detail.", Tier: "t0"}}
	rows := personaSkillRows("ar", skills)
	fsi, pdi := string(chatux043FSI), string(chatux043PDI)
	if len(rows) != 1 || !strings.HasPrefix(rows[0], fsi+"Search records the requesting user may read."+pdi+" — "+fsi) || !strings.HasSuffix(rows[0], pdi) {
		t.Errorf("an Arabic skill row is %q, want the sentence and the tier each in an isolate", rows)
	}
	if got := personaSkillRows("en-US", skills); len(got) != 1 || strings.ContainsAny(got[0], fsi+pdi) {
		t.Errorf("an English skill row is %q, want no isolates", got)
	}

	// Key names in the hint are left-to-right units; an English hint is one text.
	if markup := renderNode(t, spanOf(chatux043Hint("ar", "Enter للإرسال، Shift+Enter لسطر جديد"))); !strings.Contains(markup, `<bdi dir="ltr">Enter</bdi>`) || !strings.Contains(markup, `<bdi dir="ltr">Shift+Enter</bdi>`) {
		t.Errorf("the Arabic hint is %s, want each key name isolated", markup)
	}
	if markup := renderNode(t, spanOf(chatux043Hint("en-US", "Enter to send, Shift+Enter for a new line"))); strings.Contains(markup, "bdi") {
		t.Errorf("the English hint is %s, want plain text", markup)
	}

	// The agent card: facts written by someone else are isolated in Arabic only.
	persona := ResolvedPersonaMention{Purpose: "Answer policy questions.", Owner: "People Operations", Version: "6"}
	if markup := renderNode(t, personaProfileCard("ar", persona)); !strings.Contains(markup, `<bdi dir="auto">Answer policy questions.</bdi>`) || !strings.Contains(markup, `<bdi dir="auto">6</bdi>`) {
		t.Errorf("the Arabic agent card is %s, want its facts isolated", markup)
	}
	if markup := renderNode(t, personaProfileCard("en-US", persona)); strings.Contains(markup, "<bdi") {
		t.Errorf("the English agent card is %s, want no isolates", markup)
	}

	// A paragraph and its bullets share the page's edge, a citation marker and
	// a bottom sheet keep their own rules.
	body := func(sel string) bool {
		return strings.Contains(sel, `[dir="rtl"]`) && strings.Contains(sel, ".message-body") && strings.Contains(sel, ":is(p") && !strings.Contains(sel, "thread")
	}
	if got := s11Value(t, s11Env{width: 1280}, "text-align", body); got != "match-parent" {
		t.Errorf("an Arabic page's paragraph text-align is %q, want match-parent (the page's own start edge, where its bullets are, with no physical right)", got)
	}
	for _, want := range []string{".agent-cite{unicode-bidi:isolate;direction:ltr}", "env(safe-area-inset-bottom,0px) + 12px"} {
		if !strings.Contains(ChatUX043Styles, want) {
			t.Errorf("ChatUX043Styles lacks %s", want)
		}
	}
}

// TestTodo_CHATUX_043_Browser is the cascade of the Arabic message body at a
// phone width and a laptop width.
func TestTodo_CHATUX_043_Browser(t *testing.T) {
	body := func(sel string) bool {
		return strings.Contains(sel, `[dir="rtl"]`) && strings.Contains(sel, ".agent-reply-answer") && strings.Contains(sel, ":is(p") && !strings.Contains(sel, "thread")
	}
	for _, width := range []int{390, 1280} {
		if got := s11Value(t, s11Env{width: width}, "text-align", body); got != "match-parent" {
			t.Errorf("%dpx: an Arabic agent answer's paragraph text-align is %q, want match-parent", width, got)
		}
	}
}
