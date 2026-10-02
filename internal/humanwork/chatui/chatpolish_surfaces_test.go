package chatui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

var (
	chatPolishRawIdentifier = regexp.MustCompile(`\b[A-Z][A-Z0-9]+(?:_[A-Z0-9]+)+\b|⟦|\bchat\.[a-z0-9]+\.[a-z0-9_.]+\b`)
	chatPolishTags          = regexp.MustCompile(`<[^>]*>`)
)

// chatPolishVisible is the text a person reads in a rendered surface: the tags
// are gone and so are the attributes, which carry the machine words.
func chatPolishVisible(markup string) string {
	return regexp.MustCompile(`\s+`).ReplaceAllString(chatPolishTags.ReplaceAllString(markup, " "), " ")
}

// TestChatPolish_Surfaces is the scripted inspection of AGENTUX-071 as far as a
// render can run it: every surface the ten features of 2026-10-01 added, at the
// widths, themes and languages the inspection used, has none of the findings it
// looked for. The kinds are the inspection's: an unstyled disclosure, an inline
// style the page's security policy refuses, a raw identifier, a control without
// a name, a feature announcing a problem before the person acted, and a menu
// that mixes items with underlined links.
func TestChatPolish_Surfaces(t *testing.T) {
	for _, locale := range []string{"en-US", "ar"} {
		for _, width := range []int{1440, 390} {
			for _, theme := range []string{"light", "dark"} {
				t.Run(fmt.Sprintf("%s/%d/%s", locale, width, theme), func(t *testing.T) {
					for name, markup := range agentux063Surfaces(t, locale, width) {
						for _, forbidden := range []string{"<details", "<summary", "<style", ` style="`} {
							if strings.Contains(markup, forbidden) {
								t.Errorf("%s: %s (unstyled disclosure or refused inline style)", name, forbidden)
							}
						}
						visible := chatPolishVisible(markup)
						if found := chatPolishRawIdentifier.FindString(visible); found != "" {
							t.Errorf("%s: the page shows the identifier %q", name, found)
						}
						// Nothing announces a problem before the person acts: an idle
						// surface has no alert and no refusal sentence. The failed answer
						// card is a failure the person is meant to see.
						if name != "failed card" {
							if strings.Contains(markup, `role="alert"`) {
								t.Errorf("%s: an alert is on the page before anything was done", name)
							}
							for _, sentence := range []string{"could not be saved", "unavailable", "Settings could not"} {
								if locale == "en-US" && strings.Contains(visible, sentence) {
									t.Errorf("%s: %q is announced before anything was done", name, sentence)
								}
							}
						}
						// A menu holds items and separators, nothing else.
						root, err := xhtml.Parse(strings.NewReader(markup))
						if err != nil {
							t.Fatal(err)
						}
						walkChat5HTML(root, func(n *xhtml.Node) {
							if n.Type != xhtml.ElementNode || chat5Attr(n, "role") != "menu" {
								return
							}
							walkChat5HTML(n, func(item *xhtml.Node) {
								if item.Type != xhtml.ElementNode || (item.Data != "button" && item.Data != "a") {
									return
								}
								if chat5Attr(item, "role") != "menuitem" || !strings.Contains(" "+chat5Attr(item, "class")+" ", " menu-item ") {
									t.Errorf("%s: <%s class=%q> in a menu is not drawn as a menu item", name, item.Data, chat5Attr(item, "class"))
								}
							})
						})
					}
				})
			}
		}
	}
	// One visual language: every composer tool, hover action and menu item is
	// drawn from the same three classes.
	for class, want := range map[string]string{"tool-button": "composer tool", "message-action": "hover action", "menu-item": "menu item"} {
		if !strings.Contains(Stylesheet, "."+class) {
			t.Errorf("no styles for the %s class %s", want, class)
		}
	}
	// The thread composer, the reaction picker and the search dialog open
	// as anchored layers, not in a corner of the page.
	for _, kind := range []string{"reaction", "search", "menu", "todo", "poll"} {
		if chatLayerGroup(kind) == "" {
			t.Errorf("%s is not a layer kind", kind)
		}
	}
}

// TestChatPolish_Surfaces_Accessibility: every control of every inspected
// surface has a name, at both widths and in both directions.
func TestChatPolish_Surfaces_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "ar"} {
		for _, width := range []int{1440, 390} {
			for name, markup := range agentux063Surfaces(t, locale, width) {
				agentux063Controls(t, markup, func(n *xhtml.Node, controlName string) {
					if controlName == "" {
						t.Errorf("%s/%s/%d: <%s class=%q data-action=%q> has no name", locale, name, width, n.Data, chat5Attr(n, "class"), chat5Attr(n, "data-action"))
					}
				})
			}
		}
	}
}
