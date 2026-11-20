package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func issue3929Model(locale string) Model {
	m := chatux002Model(locale)
	m.SearchSaved = []SavedSearchItem{
		{ID: "s-1", Name: "Onboarding questions", Query: `visa in:general has:file`},
		{ID: "s-2", Name: "Ana review", Query: `from:agent sort:oldest`},
	}
	return m
}

func issue3929Popover(t *testing.T, m Model) string {
	t.Helper()
	return renderNode(t, chatsearchRecentList(m))
}

func TestIssue3929_SavedSearchesAreListedAboveRecentWithTheirScope(t *testing.T) {
	m := issue3929Model("en-US")
	m.SearchRecent = []string{"payroll close"}
	got := issue3929Popover(t, m)
	for _, want := range []string{"Saved searches", "Onboarding questions", "Ana review", `data-action="search-saved"`, `data-id="s-1"`, "Recent searches", "payroll close"} {
		if !strings.Contains(got, want) {
			t.Errorf("popover lacks %q: %s", want, got)
		}
	}
	// The scope is stated before anything runs: words and where they are searched.
	if !strings.Contains(got, "“visa”") || !strings.Contains(got, "In a channel: ") || !strings.Contains(got, "With a file") || !strings.Contains(got, "Result order: Oldest first") {
		t.Errorf("scope is not shown: %s", got)
	}
	if strings.Index(got, "Saved searches") > strings.Index(got, "Recent searches") {
		t.Errorf("saved searches come after the recent ones: %s", got)
	}
	if !strings.Contains(got, "Only you can see these") || !strings.Contains(got, "Clearing them keeps your saved searches") {
		t.Errorf("the distinction between saved and recent is not explained: %s", got)
	}
}

func TestIssue3929_EachSavedSearchCanBeRenamedEditedAndDeletedByName(t *testing.T) {
	m := issue3929Model("en-US")
	got := issue3929Popover(t, m)
	for _, want := range []string{`aria-label="Edit saved search: Onboarding questions"`, `aria-label="Delete saved search: Onboarding questions"`, `data-action="search-saved-edit"`, `data-action="search-saved-delete"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	m.SavedSearchEditing = "s-1"
	got = issue3929Popover(t, m)
	for _, want := range []string{`id="chatsearch-saved-name"`, `value="Onboarding questions"`, `id="chatsearch-saved-query"`, `value="visa in:general has:file"`, `data-action="search-saved-update"`, `data-action="search-saved-cancel"`, "Save changes", "Cancel"} {
		if !strings.Contains(got, want) {
			t.Errorf("editor lacks %q: %s", want, got)
		}
	}
	if !strings.Contains(got, "Ana review") {
		t.Errorf("editing one hid the others: %s", got)
	}
}

func TestIssue3929_SavedSearchesShowWithoutAnyRecentAndClearDoesNotAppear(t *testing.T) {
	m := issue3929Model("en-US")
	m.SearchRecent = nil
	got := issue3929Popover(t, m)
	if !strings.Contains(got, "Onboarding questions") {
		t.Fatalf("saved searches hide when there is no recent history: %s", got)
	}
	// Clear belongs to the automatic history only.
	if strings.Contains(got, "search-clear-recent") || strings.Contains(got, "Clear recent searches") {
		t.Errorf("Clear is offered with nothing to clear: %s", got)
	}
}

func TestIssue3929_ClearingRecentLeavesSavedSearchesInTheList(t *testing.T) {
	m := issue3929Model("en-US")
	m.SearchRecent = nil
	m.SearchRecentStatus = "cleared"
	got := issue3929Popover(t, m)
	if !strings.Contains(got, "Onboarding questions") {
		t.Fatalf("a cleared history took the saved searches with it: %s", got)
	}
	m.SearchSaved = nil
	if got := issue3929Popover(t, m); strings.Contains(got, "Onboarding") {
		t.Errorf("nothing saved but something shown: %s", got)
	}
}

func TestIssue3929_NothingSavedLooksLikeBefore(t *testing.T) {
	m := chatux002Model("en-US")
	m.SearchRecent = []string{"payroll close"}
	got := issue3929Popover(t, m)
	if strings.Contains(got, "Saved searches") || strings.Contains(got, "keeps your saved") {
		t.Errorf("saved-search wording with nothing saved: %s", got)
	}
	if !strings.Contains(got, `aria-label="Recent searches"`) {
		t.Errorf("popover label changed: %s", got)
	}
}

func TestIssue3929_ASearchThatNoLongerParsesIsNotRunAndSaysWhy(t *testing.T) {
	m := issue3929Model("en-US")
	m.SearchSaved = []SavedSearchItem{{ID: "s-9", Name: "Old filter", Query: "budget before:not-a-date"}}
	got := issue3929Popover(t, m)
	if !strings.Contains(got, "no longer works") || !strings.Contains(got, "disabled") {
		t.Errorf("a broken saved search runs or is silent: %s", got)
	}
	if !strings.Contains(got, `data-action="search-saved-edit"`) || !strings.Contains(got, `data-action="search-saved-delete"`) {
		t.Errorf("a broken saved search cannot be fixed or removed: %s", got)
	}
}

func TestIssue3929_AChannelThatIsGoneIsSaidAndNeverWidened(t *testing.T) {
	m := issue3929Model("en-US")
	m.SearchSaved = []SavedSearchItem{{ID: "s-3", Name: "Retired", Query: "kickoff in:retired-channel"}}
	got := issue3929Popover(t, m)
	if !strings.Contains(got, "retired-channel") || !strings.Contains(got, "not in your conversations now") {
		t.Errorf("the missing channel is not named: %s", got)
	}
	// The query that runs still carries the filter, so it cannot widen to all rooms.
	resolved, err := ResolveChatSearchQuery(m, m.SearchSaved[0].Query)
	if err != nil || !strings.Contains(resolved, "in:retired-channel") {
		t.Errorf("resolved query = %q err=%v", resolved, err)
	}
}

func TestIssue3929_PressingASavedSearchRunsItsQueryTheOrdinaryWay(t *testing.T) {
	m := issue3929Model("en-US")
	var ran string
	var edited, deleted, updatedName string
	m.Callbacks.Search = func(q string) { ran = q }
	m.Callbacks.SavedSearchEdit = func(id string) { edited = id }
	m.Callbacks.SavedSearchDelete = func(id string) { deleted = id }
	m.Callbacks.SavedSearchUpdate = func(id, name, query string) { updatedName = id + name + query }
	m.act("search-saved", "s-1")
	if ran != "visa in:general has:file" {
		t.Errorf("ran %q", ran)
	}
	m.act("search-saved-edit", "s-2")
	m.act("search-saved-delete", "s-2")
	m.act("search-saved-update", "s-2")
	m.act("search-saved", "missing")
	if edited != "s-2" || deleted != "s-2" || updatedName != "s-2" || ran != "visa in:general has:file" {
		t.Errorf("edited=%q deleted=%q updated=%q ran=%q", edited, deleted, updatedName, ran)
	}
}

func TestIssue3929_ResultsPageOffersToSaveTheSearchAndSaysWhenItIs(t *testing.T) {
	m := issue3929Model("en-US")
	m.Callbacks.SavedSearchSave = func(string, string) {}
	m.ChatSearch = &ChatSearchView{Query: "payroll close in:general"}
	got := renderNode(t, ChatSearchMount(m))
	for _, want := range []string{`data-chatsearch-form="save"`, "Save this search", `id="chatsearch-save-name"`, "Save search", "Only you can see it"} {
		if !strings.Contains(got, want) {
			t.Errorf("save control lacks %q: %s", want, got)
		}
	}
	// A search already kept says so instead of offering a second copy.
	m.ChatSearch = &ChatSearchView{Query: "visa in:general has:file"}
	got = renderNode(t, ChatSearchMount(m))
	if strings.Contains(got, `data-chatsearch-form="save"`) || !strings.Contains(got, "Already saved as “Onboarding questions”") {
		t.Errorf("a kept search is offered again: %s", got)
	}
	m.SavedSearchStatus, m.SavedSearchName = "saved", "Onboarding questions"
	if got = renderNode(t, ChatSearchMount(m)); !strings.Contains(got, "Saved as “Onboarding questions”. Find it under the search box.") {
		t.Errorf("a finished save is not announced: %s", got)
	}
	// Nothing to save while loading, on a bad query or with no words.
	for _, v := range []*ChatSearchView{{Query: "x", Loading: true}, {Query: "x before:nope"}, {Query: "  "}, {Query: "x", Error: "invalid"}} {
		m.ChatSearch = v
		if got := renderNode(t, ChatSearchMount(m)); strings.Contains(got, "chatsearch-save-name") {
			t.Errorf("save offered for %+v", v)
		}
	}
}

func TestIssue3929_SaveFailuresAreAnnouncedAsAlerts(t *testing.T) {
	m := issue3929Model("en-US")
	m.Callbacks.SavedSearchSave = func(string, string) {}
	m.ChatSearch = &ChatSearchView{Query: "payroll"}
	for status, want := range map[string]string{"nameTaken": "already have a saved search with that name", "limit": "up to 50 saved searches", "invalid": "Give the search a name", "failed": "Nothing was changed"} {
		m.SavedSearchStatus = status
		got := renderNode(t, ChatSearchMount(m))
		if !strings.Contains(got, want) || !strings.Contains(got, `role="alert"`) {
			t.Errorf("%s: %s", status, got)
		}
	}
}

func TestIssue3929_CopyInEveryLocale(t *testing.T) {
	keys := []string{"title", "group", "private", "keeps", "edit", "delete", "editName", "delName", "editTitle", "name", "query", "update", "cancel", "saveLabel", "saveHint", "savePlace", "save", "saved", "already", "updated", "deleted", "failed", "nameTaken", "limit", "invalid", "broken", "noChannel"}
	for _, key := range keys {
		en := issue3929Text("en-US", key)
		if strings.TrimSpace(en) == "" {
			t.Fatalf("no English copy for %s", key)
		}
		for _, locale := range []string{"de-DE", "ar"} {
			got := issue3929Text(locale, key)
			if strings.TrimSpace(got) == "" || (got == en && key != "name") {
				t.Errorf("%s has no %s copy: %q", key, locale, got)
			}
		}
	}
	// A German popover reads German, and an Arabic one is right to left in its fields.
	m := issue3929Model("de-DE")
	if got := issue3929Popover(t, m); !strings.Contains(got, "Gespeicherte Suchen") || !strings.Contains(got, "Gespeicherte Suche bearbeiten: Onboarding questions") {
		t.Errorf("German popover: %s", got)
	}
	m = issue3929Model("ar")
	m.SavedSearchEditing = "s-1"
	if got := issue3929Popover(t, m); !strings.Contains(got, "عمليات البحث المحفوظة") || !strings.Contains(got, `dir="auto"`) {
		t.Errorf("Arabic popover: %s", got)
	}
}

func TestIssue3929_LayoutHoldsOnPhonesAndRightToLeft(t *testing.T) {
	css := issue3929Styles + issue3929ResultStyles
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "left:", "right:", "text-align:left", "text-align:right"} {
		if strings.Contains(css, physical) {
			t.Errorf("physical property %q breaks right-to-left", physical)
		}
	}
	for _, want := range []string{"min-block-size:44px", "(pointer:coarse)", "max-width:560px"} {
		if !strings.Contains(css, want) {
			t.Errorf("styles lack %q", want)
		}
	}
	if !strings.Contains(chatlane9Styles, issue3929Styles) || !strings.Contains(ChatSearchStyles, issue3929ResultStyles) {
		t.Error("the saved-search styles are not part of the page styles")
	}
}

func TestIssue3929_ParseBacksTheScopeLine(t *testing.T) {
	m := chatux002Model("en-US")
	parts, parsed, err := issue3929ScopeParts(m, `"parental leave" from:agent kind:message`)
	if err != nil || parsed.Text != "parental leave" || len(parts) != 3 || parts[0] != "“parental leave”" {
		t.Errorf("parts = %v parsed=%+v err=%v", parts, parsed, err)
	}
	if _, _, err := issue3929ScopeParts(m, "x before:nope"); err == nil {
		t.Error("an unsupported filter parsed")
	}
	if chatsearch.SavedNameMax != 80 {
		t.Error("the name limit moved without the field and migration")
	}
}

func TestIssue3929_EnterSavesAndEscapeClosesTheEditor(t *testing.T) {
	m := issue3929Model("en-US")
	if got := issue3929KeyAction(m, "Enter", issue3929NameField, false); got != "" {
		t.Errorf("Enter with no editor open did something: %q", got)
	}
	m.SavedSearchEditing = "s-1"
	for _, field := range []string{issue3929NameField, issue3929QueryField} {
		if got := issue3929KeyAction(m, "Enter", field, false); got != "search-saved-update" {
			t.Errorf("Enter in %s = %q, want the update", field, got)
		}
		if got := issue3929KeyAction(m, "Escape", field, false); got != "search-saved-cancel" {
			t.Errorf("Escape in %s = %q, want the cancel", field, got)
		}
	}
	if got := issue3929KeyAction(m, "Enter", issue3929NameField, true); got != "" {
		t.Errorf("Enter that only confirms an input method composition submitted: %q", got)
	}
	if got := issue3929KeyAction(m, "Enter", "chat-search", false); got != "" {
		t.Errorf("Enter in another field changed the saved search: %q", got)
	}
	if got := issue3929KeyAction(m, "a", issue3929NameField, false); got != "" {
		t.Errorf("a letter did something: %q", got)
	}
}
