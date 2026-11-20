package chatui

import (
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// TestIssue3403_UnrestrictedChoicesSubmitNoValue renders the search form in
// every shipped locale: the All kinds and Any language choices are selected and
// carry an explicit empty value, so applying the untouched form adds no kind or
// language constraint (the select's value is "", never the label text).
func TestIssue3403_UnrestrictedChoicesSubmitNoValue(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			markup, err := ui.RenderToString(RenderChatSearch(locale, ChatSearchView{Query: "budget"}))
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"chatsearch-kind", "chatsearch-written"} {
				sel := issue3690Element(t, markup, id)
				selected, options := "unset", 0
				for c := sel.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != xhtml.ElementNode || c.Data != "option" {
						continue
					}
					options++
					if _, has := issue3690Attr(c, "value"); !has {
						t.Fatalf("%s: an option has no value attribute", id)
					}
					if _, ok := issue3690Attr(c, "selected"); ok {
						selected, _ = issue3690Attr(c, "value")
					}
				}
				if options < 2 || selected != "" {
					t.Fatalf("%s: options=%d selected value=%q, want the unrestricted empty choice selected", id, options, selected)
				}
			}
		})
	}
}
