package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// #3857: after a filter is added or removed, or "Try again" is pressed, the
// control that was activated is replaced and focus fell to the page. Every
// settled outcome now carries one labelled, focusable summary the browser
// client moves focus to.
func TestIssue3857_EverySettledOutcomeHasOneFocusableSummary(t *testing.T) {
	groups := issue3900Rows(2)
	unavailable := map[string]string{"en-US": "Search is not available right now", "de-DE": "", "ar": ""}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		cases := map[string]ChatSearchView{
			"words":         {Query: "Portland", Response: chatsearch.Response{Groups: groups}},
			"filter only":   {Query: "in:general", Response: chatsearch.Response{Groups: groups}},
			"no results":    {Query: "Portland in:general"},
			"error":         {Query: "Portland", Error: "unavailable"},
			"error w/ rows": {Query: "Portland", Error: "unavailable", Response: chatsearch.Response{Groups: groups}},
		}
		for name, view := range cases {
			got := renderNode(t, RenderChatSearch(locale, view))
			if n := strings.Count(got, `id="`+Issue3857SummaryID+`"`); n != 1 {
				t.Fatalf("%s/%s: want exactly one focus target, got %d: %s", locale, name, n, got)
			}
			at := strings.Index(got, `id="`+Issue3857SummaryID+`"`)
			open := strings.LastIndex(got[:at], "<")
			tag := got[open:]
			tag = tag[:strings.Index(tag, ">")]
			if !strings.Contains(tag, `tabIndex="-1"`) {
				t.Fatalf("%s/%s: the summary must be focusable by script only: %s", locale, name, tag)
			}
		}
		if got := renderNode(t, RenderChatSearch(locale, cases["error"])); !strings.Contains(got, `data-chatsearch-action="retry"`) {
			t.Fatalf("%s: the failure keeps its retry control", locale)
		}
	}
	got := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "Portland", Error: "unavailable"}))
	if !strings.Contains(got, unavailable["en-US"]) {
		t.Fatalf("the failure summary must say what happened: %s", got)
	}
	// The summary names the scope, so a filter change is heard when focus lands.
	scoped := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "Portland in:general", Response: chatsearch.Response{Groups: groups}}))
	if !strings.Contains(scoped, "Results for") || !strings.Contains(scoped, "Portland") {
		t.Fatalf("the summary must name the words searched: %s", scoped)
	}
}
