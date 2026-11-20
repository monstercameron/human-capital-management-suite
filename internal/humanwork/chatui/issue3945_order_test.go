package chatui

import (
	"strings"
	"testing"
)

func TestIssue3945_DefaultIsNewestAndOffersOldest(t *testing.T) {
	markup := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "deadline"}))
	for _, want := range []string{`data-chatsearch-action="order"`, `data-extra="oldest"`, `data-extra="newest"`, "Newest first", "Oldest first", `id="chatsearch-order"`, `name="order"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "Remove filter: Result order") {
		t.Fatalf("the default order showed a chip: %s", markup)
	}
	newest := strings.Index(markup, `data-extra="newest"`)
	if !strings.Contains(markup[newest-120:newest+60], `aria-pressed="true"`) {
		t.Fatalf("Newest first is not marked as the chosen order: %s", markup[newest-120:newest+60])
	}
}

func TestIssue3945_OldestIsMarkedChosenAndRemovable(t *testing.T) {
	markup := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "deadline sort:oldest"}))
	oldest := strings.Index(markup, `data-extra="oldest"`)
	if oldest < 0 || !strings.Contains(markup[oldest-120:oldest+60], `aria-pressed="true"`) {
		t.Fatalf("Oldest first is not marked as the chosen order: %s", markup)
	}
	if !strings.Contains(markup, `data-extra="sort:oldest"`) || !strings.Contains(markup, "Remove filter: Result order Oldest first") {
		t.Fatalf("applied order has no removable chip: %s", markup)
	}
	if !strings.Contains(markup, `<option selected value="oldest">Oldest first`) {
		t.Fatalf("Refine search does not show Oldest first: %s", markup)
	}
}

func TestIssue3945_OrderCopyInEveryLocale(t *testing.T) {
	for _, key := range []string{"order", "newest", "oldest", "ordernote"} {
		en := chatsearchText("en-US", key)
		for _, locale := range []string{"de-DE", "ar"} {
			got := chatsearchText(locale, key)
			if got == "" || got == en {
				t.Fatalf("%s/%s not translated: %q", locale, key, got)
			}
		}
	}
	if markup := renderNode(t, RenderChatSearch("ar", ChatSearchView{Query: "x"})); !strings.Contains(markup, `dir="rtl"`) {
		t.Fatalf("not right to left")
	}
}

func TestIssue3945_NoSwitchWithoutAQuery(t *testing.T) {
	if markup := renderNode(t, RenderChatSearch("en-US", ChatSearchView{})); strings.Contains(markup, `data-chatsearch-action="order"`) {
		t.Fatalf("order switch drawn with nothing searched: %s", markup)
	}
}

func TestIssue3945_OrderAloneIsNotAReasonToWiden(t *testing.T) {
	markup := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "deadline sort:oldest"}))
	if strings.Contains(markup, `data-chatsearch-action="widen"`) {
		t.Fatalf("widen offered for an order only: %s", markup)
	}
	markup = renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "deadline kind:message sort:oldest"}))
	if !strings.Contains(markup, `data-chatsearch-action="widen" data-extra="deadline sort:oldest"`) {
		t.Fatalf("widening dropped the chosen order: %s", markup)
	}
}
