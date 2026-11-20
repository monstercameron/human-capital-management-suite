package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// #3897: "Widen search" removes every filter. With none applied it did nothing
// and the hint told a person to remove a filter that was not there.
func TestIssue3897_NoWidenWithoutAFilter(t *testing.T) {
	removeHint := map[string]string{"en-US": "Remove a filter", "de-DE": "Filter entfernen", "ar": "أزل مرشحاً"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		got := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: "zzqnomatch", Response: chatsearch.Response{}}))
		if strings.Contains(got, `data-chatsearch-action="widen"`) {
			t.Fatalf("%s: an unfiltered search offers a widen action that cannot change anything: %s", locale, got)
		}
		if strings.Contains(got, removeHint[locale]) {
			t.Fatalf("%s: the empty state tells a person to remove a filter that is not applied: %s", locale, got)
		}
		if !strings.Contains(got, "zzqnomatch") || !strings.Contains(got, `role="status"`) {
			t.Fatalf("%s: the empty state must still say nothing matched, announced: %s", locale, got)
		}
		if locale == "en-US" && !strings.Contains(got, "Try different words") {
			t.Fatalf("en-US: a useful next step is expected: %s", got)
		}
	}
}

func TestIssue3897_WidenStaysWhenAFilterCanBeRemoved(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		got := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: "zzqnomatch in:people-ops", Response: chatsearch.Response{}}))
		if !strings.Contains(got, `data-chatsearch-action="widen"`) || !strings.Contains(got, `data-extra="zzqnomatch"`) {
			t.Fatalf("%s: a filtered empty search must offer widening to the bare words: %s", locale, got)
		}
	}
}
