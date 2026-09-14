package productui

import (
	"net/url"
	"testing"
)

func TestTodo_UXAUDIT_015_PromotionShortcutClearsStaleDirectoryFilters(t *testing.T) {
	view := NewView(PageHome, "HarborCare Demo", "worker-rafael", "scope")
	view.NavCollapsed = true
	view = ApplyPagePermissions(view, []RolePagePermission{
		{Page: PagePeople, View: true},
		{Page: PageJourneys, View: true, Create: true},
	})

	var promotionHref string
	peopleShortcuts := 0
	for _, action := range ResolveHomeQuickActions(view) {
		parsedAction, err := url.Parse(action.Href)
		if err != nil {
			t.Fatal(err)
		}
		if parsedAction.Path == pageHref(PagePeople) {
			peopleShortcuts++
		}
		if action.Label == view.Locale.Text("home.action_promote") {
			promotionHref = action.Href
			break
		}
	}
	if promotionHref == "" {
		t.Fatal("authorized promotion start is absent")
	}
	if peopleShortcuts != 1 {
		t.Fatalf("promotion start must not duplicate a generic directory action: %d", peopleShortcuts)
	}
	parsed, err := url.Parse(promotionHref)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != pageHref(PagePeople) {
		t.Fatalf("promotion shortcut destination = %q", parsed.Path)
	}
	query := parsed.Query()
	for _, key := range []string{"q", "team", "location"} {
		values, present := query[key]
		if !present || len(values) != 1 || values[0] != "" {
			t.Fatalf("%s must explicitly clear the persisted filter: %q", key, promotionHref)
		}
	}
	if query.Get("eligible") != "1" || query.Get("page") != "1" || query.Get("nav") != "collapsed" {
		t.Fatalf("promotion shortcut must show eligible workers on page one and retain shell state: %q", promotionHref)
	}
}
