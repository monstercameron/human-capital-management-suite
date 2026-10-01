package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_066(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{FavoritePages: []PageID{PagePeople}})
	props := navigationSidebarProps(view)
	if len(props.Favorites) != 1 || props.Favorites[0].Page != PagePeople {
		t.Fatalf("favorite projection = %+v", props.Favorites)
	}
	if item, ok := navigationItemByPage(props.Items, PagePeople); !ok || item.Page != PagePeople {
		t.Fatal("a favorited page was removed from its original navigation position")
	}
	if strings.Contains(favoriteToggleHref(view, PagePeople), "favorites=") {
		t.Fatal("favorite toggle placed the account preference in the URL")
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "favorites=") {
		t.Fatal("a generated product link leaked favorites into the address")
	}
}

func TestTodo_UXBLIND_066_Browser(t *testing.T) {
	view := ApplyRequest(testView(PagePeople), PageRequest{FavoritePages: []PageID{PagePeople}})
	props := navigationSidebarProps(view)
	markup, err := ui.RenderToString(NavigationSidebar(props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="nav-favorite is-favorite"`) || strings.Contains(markup, "favorites=") {
		t.Fatalf("favorite navigation is not an in-place, URL-free control: %s", markup)
	}
	css := Stylesheet()
	if !strings.Contains(css, `.nav-favorite{`) || !strings.Contains(css, `min-height:44px`) {
		t.Fatal("favorite control does not retain a 24px-plus keyboard target")
	}
}

func TestTodo_UXBLIND_066_Accessibility(t *testing.T) {
	props := navigationLeafProps(ApplyRequest(testView(PageHome), PageRequest{}), NavItem{Page: PagePeople, Label: "People", Icon: "people"}, false)
	markup, err := ui.RenderToString(NavigationItem(props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-label="Add People to favorites"`) || !strings.Contains(markup, `class="nav-favorite"`) {
		t.Fatalf("favorite control is not keyboard-labelled: %s", markup)
	}
}

func TestTodo_UXBLIND_067(t *testing.T) {
	view := testView(PageSettings)
	view.Accessibility = AccessibilityPreferences{TextSize: "larger", Contrast: "more", Motion: "reduce", Links: "underlined"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-text-size="larger"`, `data-hcm-contrast="more"`, `data-hcm-motion-preference="reduce"`, `data-hcm-links="underlined"`,
		`aria-label="Larger"`, `aria-label="More contrast"`, `aria-label="Reduce motion"`, `aria-label="Always underline links"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("settings document missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_067_Browser(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{`:root[data-hcm-text-size="larger"]{font-size:125%;}`, `.locale-choice-copy,.locale-choice-copy strong`, `overflow-wrap:normal`, `word-break:normal`} {
		if !strings.Contains(css, want) {
			t.Errorf("accessibility reflow CSS missing %q", want)
		}
	}
	if strings.Contains(css, `.locale-choice-copy{overflow-wrap:anywhere`) {
		t.Fatal("language labels can still break mid-word at larger text")
	}
}

func TestTodo_UXBLIND_067_Accessibility(t *testing.T) {
	props := AccessibilityPreferencesProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Value: DefaultAccessibilityPreferences(),
		TextSizes: AccessibilityTextSizeOptions(), Contrasts: AccessibilityContrastOptions(), Motions: AccessibilityMotionOptions(), LinkStyles: AccessibilityLinkOptions(),
	}
	markup, err := ui.RenderToString(AccessibilityPreferencesPanel(props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-labelledby="accessibility-title"`, `aria-describedby="accessibility-text-size-help"`, `aria-label="Standard"`, `aria-label="Larger"`, `type="radio"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("accessibility control missing %q", want)
		}
	}
	if strings.Contains(markup, `aria-label="standard"`) || strings.Contains(markup, `aria-label="larger"`) {
		t.Fatal("radio accessible names fell back to raw preference identifiers")
	}
}

func navigationItemByPage(items []NavigationItemProps, page PageID) (NavigationItemProps, bool) {
	for _, item := range items {
		if item.Page == page && len(item.Children) == 0 {
			return item, true
		}
		if found, ok := navigationItemByPage(item.Children, page); ok {
			return found, ok
		}
	}
	return NavigationItemProps{}, false
}
