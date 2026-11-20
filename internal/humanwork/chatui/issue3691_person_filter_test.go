package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
)

func issue3691Model() Model {
	return Model{Locale: "en-US", SearchDirectory: []SearchPerson{
		{ID: "hc-026-julian-miller", Name: "Julian Miller"},
		{ID: "hc-031-julia-ng", Name: "Julia Ng"},
	}}
}

// TestIssue3691_FromFilterAcceptsUnquotedNames: from:@Julian Miller, typed
// without quotes, names that one person like the quoted full name does.
func TestIssue3691_FromFilterAcceptsUnquotedNames(t *testing.T) {
	m := issue3691Model()
	for query, want := range map[string]string{
		`from:@Julian Miller`:         `from:hc-026-julian-miller`,
		`from:@Julian Miller payroll`: `from:hc-026-julian-miller payroll`,
		`from:"Julian Miller"`:        `from:hc-026-julian-miller`,
	} {
		got, err := ResolveChatSearchQuery(m, query)
		if err != nil || got != want {
			t.Fatalf("%q resolved to %q (%v), want %q", query, got, err, want)
		}
	}
}

// TestIssue3691_ChipsAndFieldsShowDisplayNames: the filter chip and the person
// field name the person, never the raw id and never half of the name.
func TestIssue3691_ChipsAndFieldsShowDisplayNames(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3691Model()
		m.Locale = locale
		markup := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: `from:@Julian Miller payroll`, Context: &m}))
		chip := chatsearchText(locale, "remove") + ": " + chatsearchText(locale, "person") + " Julian Miller"
		if !strings.Contains(markup, chip) {
			t.Fatalf("%s: no chip %q in %s", locale, chip, markup)
		}
		if strings.Contains(markup, " hc-026") || strings.Contains(markup, "person @Julian") || strings.Contains(markup, `value="hc-026`) {
			t.Fatalf("%s: a raw id or half a name is shown: %s", locale, markup)
		}
		if v, _ := issue3690Attr(issue3690Element(t, markup, "chatsearch-person"), "value"); v != "Julian Miller" {
			t.Fatalf("%s: person field value %q", locale, v)
		}
	}
}

// TestIssue3691_UnknownFirstNameIsExplained: a first name alone finds nobody,
// and the empty result says to type the full name instead of leaving only
// "No matches".
func TestIssue3691_UnknownFirstNameIsExplained(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3691Model()
		m.Locale = locale
		markup := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: `from:@julian payroll`, Context: &m}))
		hint := chatsearchText(locale, "personhint")
		if hint == "personhint" || !strings.Contains(markup, stdhtml.EscapeString(hint)) {
			t.Fatalf("%s: no full-name hint in %s", locale, markup)
		}
		known := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: `from:@Julian Miller payroll`, Context: &m}))
		if strings.Contains(known, stdhtml.EscapeString(hint)) {
			t.Fatalf("%s: the hint is shown for a person who is known", locale)
		}
	}
}
