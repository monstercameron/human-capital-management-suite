package productui

import (
	"regexp"
	"strings"
	"testing"
)

var chatDesignFlyoutTrigger = regexp.MustCompile(`<button[^>]*nav-flyout-trigger[^>]*>`)

// #3839: in the icon rail an item with child pages opens a flyout listing its
// children, so Chat's Agents page is reachable without expanding the
// navigation. The trigger announces the popup, the flyout is a list of links,
// and the rule that places it follows the reading direction.
func TestChatDesign_3839_RailItemsWithChildrenOpenAFlyout(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageChat)
		view.Locale = ResolveProductLocalePreference(locale, "")
		document, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		triggers := chatDesignFlyoutTrigger.FindAllString(document, -1)
		if len(triggers) < 2 {
			t.Fatalf("%s: the rail has %d flyout triggers, want one per item with children", locale, len(triggers))
		}
		for _, trigger := range triggers {
			for _, want := range []string{`aria-haspopup="true"`, `aria-expanded="false"`, `aria-controls="nav-flyout-`, `type="button"`} {
				if !strings.Contains(trigger, want) {
					t.Errorf("%s: trigger lacks %s: %s", locale, want, trigger)
				}
			}
		}
		list := regexp.MustCompile(`(?s)<ul[^>]*id="nav-flyout-chat"[^>]*>(.*?)</ul>`).FindStringSubmatch(document)
		if list == nil {
			t.Fatalf("%s: the Chat item has no flyout list", locale)
		}
		if !strings.Contains(list[1], `href="/workspace/app/chat/agents`) || !strings.Contains(list[1], `href="/workspace/app/chat`) {
			t.Errorf("%s: the Chat flyout does not link Conversations and Agents: %s", locale, list[1])
		}
		if strings.Contains(list[1], "nav=collapsed") {
			t.Errorf("%s: the flyout links carry the rail as a collapse choice", locale)
		}
	}

	// The expanded navigation keeps its disclosure groups and has no flyout.
	expanded := testView(PageChat)
	expanded.NavExpanded = true
	document, err := Render(expanded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(document, `class="nav-link nav-flyout-trigger`) {
		t.Error("the expanded navigation renders flyout triggers")
	}

	sheet := chatDesignNavFlyoutStylesheet()
	for _, want := range []string{"inset-inline-start:100%", ".nav-flyout:not(.is-dismissed):is(:hover,:focus-within)>.nav-flyout-list", ".nav-flyout.is-open>.nav-flyout-list", "overflow:visible"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the flyout stylesheet lacks %q", want)
		}
	}
	if strings.Contains(sheet, "inset-inline-start:100%") && strings.Contains(sheet, "left:") {
		t.Error("the flyout is placed with a physical side")
	}
}
