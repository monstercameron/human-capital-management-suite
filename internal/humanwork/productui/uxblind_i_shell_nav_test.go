package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_016(t *testing.T) {
	view := testView(PagePeople)
	view.ContentLoading = true
	doc, err := ui.RenderToString(BuildContentLoading(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `aria-busy="true"`) || !strings.Contains(doc, `data-network-state="pending"`) {
		t.Fatalf("navigation loading state has no immediate progress semantics: %s", doc)
	}
}

func TestTodo_UXBLIND_016_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.ContentLoading = true
	doc, err := ui.RenderToString(BuildContentLoading(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `network-stage-pending`) == false || !strings.Contains(doc, `aria-busy="true"`) {
		t.Fatal("old page content remained the interactive navigation surface")
	}
}

func TestTodo_UXBLIND_027(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := ResolveProductLocale(locale)
		for _, definition := range registeredPages() {
			if definition.LabelKey == "" || definition.TitleKey == "" {
				continue
			}
			if label, title := copy.Text(definition.LabelKey), copy.Text(definition.TitleKey); label != title {
				t.Fatalf("%s %s has sidebar %q but title %q", locale, definition.ID, label, title)
			}
		}
	}
}

func TestTodo_UXBLIND_027_Browser(t *testing.T) {
	view := ApplyLocale(testView(PageHistory), ResolveProductLocale("en-US"))
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Work History", `aria-current="page"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("canonical page identity missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_027_I18n(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := ResolveProductLocale(locale)
		if copy.Text("page.docs.label") != copy.Text("page.docs.title") || copy.Text("page.worker_ids.label") != copy.Text("page.worker_ids.title") {
			t.Fatalf("%s has divergent catalog page names", locale)
		}
	}
}

func TestTodo_UXBLIND_028(t *testing.T) {
	items := navigationFor(ResolveProductLocale("en-US"), nil)
	seen := map[string]PageID{}
	for _, item := range items {
		if previous, exists := seen[item.Icon]; exists {
			t.Fatalf("top-level pages %s and %s share icon %q", previous, item.Page, item.Icon)
		}
		seen[item.Icon] = item.Page
	}
}

func TestTodo_UXBLIND_028_Browser(t *testing.T) {
	view := testView(PageHistory)
	view.NavigationGroupOpen = map[PageID]bool{PageWork: true, PageAdmin: true}
	_, items := projectNavigation(view)
	open := 0
	for _, item := range items {
		if item.Expanded && len(item.Children) > 0 {
			open++
		}
	}
	if open > 1 {
		t.Fatalf("multiple primary groups remained open: %d", open)
	}
}

func TestTodo_UXBLIND_028_Accessibility(t *testing.T) {
	props := navigationLeafProps(testView(PageHistory), NavItem{Page: PageHistory, Label: "Work History", Icon: "history"}, true)
	markup, err := ui.RenderToString(NavigationItem(props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-label="Remove Work History from favorites"`) || !strings.Contains(markup, `title="Remove Work History from favorites"`) {
		t.Fatalf("favorite control is not independently labelled: %s", markup)
	}
}

func TestTodo_UXBLIND_040(t *testing.T) {
	css := Stylesheet()
	if strings.Contains(css, `.role-access-preview .organization-scope-unit{min-width:0;overflow-wrap:anywhere`) {
		t.Fatal("organization identifiers still allow arbitrary mid-word wrapping")
	}
	if !strings.Contains(css, `.role-access-preview .organization-scope-unit{min-width:0;overflow-wrap:normal;word-break:normal;}`) {
		t.Fatal("organization identifiers do not use word-boundary wrapping")
	}
}

func TestTodo_UXBLIND_040_Browser(t *testing.T) {
	css := Stylesheet()
	for _, width := range []string{"min-width:761px", "max-width:760px"} {
		if !strings.Contains(css, width) {
			t.Fatalf("responsive label rules omit %s", width)
		}
	}
}
