package chatui

import (
	"regexp"
	"strings"
	"testing"
)

func chatux009LoadingFixture(locale string) Model {
	m := chat4Fixture(locale, "sent", false)
	m.State = StateLoading
	m.Messages = nil
	m.PersonaInvocations = nil
	return m
}

// The copy of the five-second line exists in en-US, de-DE and ar, differs between
// them, and is never a copy key; the skeleton draws a fixed number of rows.
func TestTodo_CHATUX_009(t *testing.T) {
	seen := map[string]bool{}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		text := chatux009Text(m, chatux009StillLoadingKey)
		if text == "" || text == chatux009StillLoadingKey || strings.HasPrefix(text, "chat.") || strings.Contains(text, "⟦") {
			t.Fatalf("%s: the still-loading line is %q", locale, text)
		}
		if seen[text] {
			t.Fatalf("%s: the still-loading line is not translated: %q", locale, text)
		}
		seen[text] = true
	}
	if ChatSlowLoadAfter.Seconds() != 5 {
		t.Fatalf("the line appears after %v, want five seconds", ChatSlowLoadAfter)
	}
	nodes := chatux009LoadingBody(chatux009LoadingFixture("en-US"))
	if len(nodes) != 1 {
		t.Fatalf("the loading body is %d nodes, want one status region", len(nodes))
	}
	markup := renderNode(t, nodes[0])
	if got := strings.Count(markup, `class="chatux009-skel-row"`); got != chatux009SkeletonRows {
		t.Fatalf("skeleton rows = %d, want %d: %s", got, chatux009SkeletonRows, markup)
	}
	// The five-second rule is a rule on the document element's attribute, set by
	// the client, and uses no literal duration (the motion tokens own those).
	chat4Require(t, ChatUX009Styles, ".chatux009-slow{", "visibility:hidden", `:root[`+ChatSlowLoadAttribute+`] .chat-workspace .chatux009-slow{visibility:visible}`)
	if regexp.MustCompile(`\b\d*\.?\d+(ms|s)\b`).MatchString(ChatUX009Styles) {
		t.Fatal("the loading styles carry a literal duration")
	}
	if !strings.Contains(Stylesheet, ChatUX009Styles) {
		t.Fatal("the loading styles are not part of the page stylesheet")
	}
}

// The first paint of Chat while the open conversation's messages are still being
// read: the conversation list, the header and the composer are there, and the
// timeline is a skeleton with no loading sentence a person can see.
func TestTodo_CHATUX_009_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chatux009LoadingFixture(locale)
		markup := render(t, m)
		for _, want := range []string{"general", "people-ops", `id="chat-composer"`, "chatux009-skeleton", "chatux009-skel-row", "chatux009-slow"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s: the loading page is missing %q", locale, want)
			}
		}
		// The sentence a screen reader hears is out of sight, and the five-second
		// line is hidden until the client reveals it.
		loading := m.t(KeyLoading)
		if loading == "" || loading == KeyLoading {
			t.Fatalf("%s: the loading copy is missing", locale)
		}
		visible := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(regexp.MustCompile(`<span class="sr-only">[^<]*</span>`).ReplaceAllString(markup, ""), "|")
		if strings.Contains(visible, loading) {
			t.Fatalf("%s: the page prints %q as visible text while loading", locale, loading)
		}
		if strings.Contains(markup, `class="state-panel"`) && strings.Contains(markup, loading) {
			t.Fatalf("%s: the old loading panel is still drawn", locale)
		}
	}
	// The conversation list comes from the list read alone: with no rooms yet the
	// rail itself is a skeleton.
	empty := Model{State: StateLoading, Locale: "en-US"}
	if markup := render(t, empty); !strings.Contains(markup, "rail-skeleton") || !strings.Contains(markup, "chatux009-skeleton") {
		t.Fatal("a page with nothing read yet must draw skeletons for the list and the timeline")
	}
}

// Assistive technology hears one busy status region, and the drawn rows are not
// read out as content.
func TestTodo_CHATUX_009_Accessibility(t *testing.T) {
	markup := renderNode(t, chatux009LoadingBody(chatux009LoadingFixture("en-US"))[0])
	for _, want := range []string{`role="status"`, `aria-busy="true"`, `class="sr-only"`, `aria-hidden="true"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("the loading region is missing %s: %s", want, markup)
		}
	}
	rows := regexp.MustCompile(`<div[^>]*chatux009-skel-rows[^>]*>`).FindString(markup)
	if !strings.Contains(rows, `aria-hidden="true"`) {
		t.Fatalf("the skeleton rows are exposed to assistive technology: %s", rows)
	}
}
