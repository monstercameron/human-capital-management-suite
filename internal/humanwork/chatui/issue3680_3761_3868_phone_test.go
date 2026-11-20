package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// issue3680Block returns the part of the stylesheet that starts at needle and
// runs to the end of that rule, or fails the test.
func issuePhoneRule(t *testing.T, needle string) string {
	t.Helper()
	at := strings.Index(Stylesheet, needle)
	if at < 0 {
		t.Fatalf("the stylesheet has no rule %q", needle)
	}
	rule := Stylesheet[at:]
	return rule[:strings.Index(rule, "}")]
}

// The loading list (no conversation chosen) is the screen on a phone, so it
// takes no drawer offset and keeps a 16px gutter around its search box and rows.
func TestIssue3680_LoadingListKeepsPhoneGutter(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(Build(Model{State: StateLoading, Locale: locale}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`data-has-selection="false"`, `class="rail-search"`, `class="rail-skeleton"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s: the loading workspace has no %s", locale, want)
			}
		}
	}
	slide := issuePhoneRule(t, `.chat-workspace[data-has-selection="false"] .chat-rail{transform`)
	if !strings.Contains(slide, "transform:none") {
		t.Errorf("the list stays offset by the drawer's slide-in: %s", slide)
	}
	for _, selector := range []string{".chat-workspace .chat-rail .rail-search{", ".chat-workspace .chat-rail .rail-skeleton{"} {
		if rule := issuePhoneRule(t, selector); !strings.Contains(rule, "padding-inline:16px") {
			t.Errorf("%s has no 16px side gutter: %s", selector, rule)
		}
	}
	if rule := issuePhoneRule(t, ".chat-workspace .chat-rail .rail-search .chat-icon{"); !strings.Contains(rule, "inset-inline-start:26px") {
		t.Errorf("the search glyph does not follow the gutter: %s", rule)
	}
}

// The list's title is never broken inside a word, and the results heading
// keeps clear of the bell and the account button.
func TestIssue3761_TitleAndResultsClearOfBell(t *testing.T) {
	head := issuePhoneRule(t, ".chat-workspace .chat-rail .rail-head{")
	if !strings.Contains(head, "flex-wrap:wrap") || !strings.Contains(head, "block-size:auto") {
		t.Errorf("the list bar cannot give its title a row: %s", head)
	}
	if rule := issuePhoneRule(t, ".chat-workspace .chat-rail .rail-head .rail-title-group{"); !strings.Contains(rule, "min-inline-size:max-content") {
		t.Errorf("the title can be squeezed narrower than its longest word: %s", rule)
	}
	if rule := issuePhoneRule(t, ".chat-workspace .chat-rail .rail-head .rail-title{"); !strings.Contains(rule, "overflow-wrap:normal") || !strings.Contains(rule, "word-break:normal") {
		t.Errorf("the title may still break inside a word: %s", rule)
	}
	results := issuePhoneRule(t, `.chat-workspace[data-sidebar-open="true"] .chat-search-results .search-results-head{`)
	if !strings.Contains(results, "padding-inline-end:calc(2 * var(--hcm-control-height)") {
		t.Errorf("the results heading runs under the bell and the account button: %s", results)
	}
	// The padding sits on the row that holds the heading and the count, so
	// both clear the buttons; the row must be the heading's parent.
	panel, err := ui.RenderToString(searchResultsPanel(Model{Search: "deploy", Locale: "en-US"}))
	if err != nil || !strings.Contains(panel, `class="search-results-head"`) || !strings.Contains(panel, `search-head-count`) {
		t.Fatalf("the results heading row is not the class the rule styles (%v): %s", err, panel)
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(Build(Model{State: StateLoading, Locale: locale}))
		if err != nil || !strings.Contains(markup, `class="rail-title"`) {
			t.Fatalf("%s: no list title (%v)", locale, err)
		}
	}
}

// A message link wraps on a phone, so its whole address can be read.
func TestIssue3868_PhoneLinkShowsWholeAddress(t *testing.T) {
	link := issuePhoneRule(t, ".chat-workspace a.chat-link[href]{")
	for _, want := range []string{"white-space:normal", "overflow:visible", "text-overflow:clip", "overflow-wrap:anywhere"} {
		if !strings.Contains(link, want) {
			t.Errorf("a phone link lacks %s: %s", want, link)
		}
	}
	cut := issuePhoneRule(t, ".chat-workspace a.chat-link[href] .chat-link-cut{")
	if !strings.Contains(cut, "max-inline-size:none") || !strings.Contains(cut, "white-space:normal") {
		t.Errorf("a shortened address keeps hiding its middle: %s", cut)
	}
	start := strings.Index(Stylesheet, Issue3868Styles)
	base := strings.Index(Stylesheet, "a.chat-link{display:inline-block")
	if start < 0 || base < 0 || start < base {
		t.Errorf("the phone rule must come after the base rule (%d, %d)", start, base)
	}
	markup := chatbug053Body(t, chatbug037Model("ready"), "claim http://gift-rewards.example/claim now")
	if !strings.Contains(markup, `<a class="chat-link" href="http://gift-rewards.example/claim">http://gift-rewards.example/claim</a>`) {
		t.Errorf("the full address is not in the link: %s", markup)
	}
}
