package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agentUX044View(t *testing.T, language string, favorites ...PageID) View {
	t.Helper()
	view := ApplyLocale(NewView(PageHome, "tenant-1", "principal-1", ""), ResolveProductLocale(language))
	view = ApplyPagePermissions(view, []RolePagePermission{
		{RoleID: "r", Page: PageHome, View: true}, {RoleID: "r", Page: PageChat, View: true}, {RoleID: "r", Page: PageAgents, View: true},
		{RoleID: "r", Page: PageAdmin, View: true}, {RoleID: "r", Page: PagePersonaAdmin, View: true}, {RoleID: "r", Page: PageSettings, View: true},
		{RoleID: "r", Page: PageChatSettings, View: true, Update: true},
	})
	view = ApplyAgentsAvailability(view, AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable}})
	view = ApplyRequest(view, PageRequest{FavoritePages: favorites})
	return ApplyLocale(view, ResolveProductLocale(language))
}

// A favorite is named by its page. Chat's first child is the page "Chat"; the
// favorites list called it "Overview", as it would every other section's.
func TestTodo_AGENTUX_044(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		view := agentUX044View(t, language, PageChat, PageAdmin)
		favorites, items := projectNavigation(view)

		// Every section whose first child is its own overview is checked: the
		// favorite of that child carries the section's name.
		sections := 0
		for _, item := range view.Navigation {
			if len(item.Children) == 0 || !navigationOverviewChild(item, item.Children[0]) {
				continue
			}
			sections++
			leaves := map[PageID]NavItem{}
			collectNavigationLeaves(item, NavItem{}, leaves)
			leaf, ok := leaves[item.Page]
			if !ok || leaf.Label != item.Label || leaf.LabelKey != item.LabelKey {
				t.Errorf("%s: the favorite of %s's overview is named %q, want the section name %q", language, item.Page, leaf.Label, item.Label)
			}
			if leaf.Label == locale.Text("nav.overview") {
				t.Errorf("%s: a favorite of %s is still named by its position in the menu", language, item.Page)
			}
		}
		if sections == 0 {
			t.Fatalf("%s: the navigation under test has no section with an overview child", language)
		}

		// The Favorites list shows the two sections by two different names.
		labels := map[string]bool{}
		for _, favorite := range favorites {
			if favorite.Label == locale.Text("nav.overview") {
				t.Errorf("%s: Favorites lists %s as %q", language, favorite.Page, favorite.Label)
			}
			labels[favorite.Label] = true
		}
		if len(favorites) == 0 || len(labels) != len(favorites) {
			t.Fatalf("%s: favorites are missing or share a name: %+v", language, favorites)
		}
		chatFavorite := false
		for _, favorite := range favorites {
			if favorite.Page == PageChat {
				chatFavorite = favorite.Label == locale.Text("page.chat.label")
			}
		}
		if !chatFavorite {
			t.Errorf("%s: the Chat favorite is not labelled %q: %+v", language, locale.Text("page.chat.label"), favorites)
		}

		// Under Chat, the first child says what it is: the conversations.
		chat, ok := projectedNavigationItem(items, PageChat)
		if !ok || len(chat.Children) == 0 {
			t.Fatalf("%s: Chat has no children in the navigation: %+v", language, chat)
		}
		if chat.Children[0].Label != locale.Text("nav.conversations") || chat.Children[0].Page != PageChat {
			t.Errorf("%s: Chat's first child is %q, want %q", language, chat.Children[0].Label, locale.Text("nav.conversations"))
		}
		for _, child := range chat.Children {
			if child.Label == locale.Text("nav.overview") {
				t.Errorf("%s: a child of Chat is still labelled %q", language, child.Label)
			}
		}
	}
	// The three names are translated, not English everywhere.
	if ResolveProductLocale("de-DE").Text("nav.conversations") == "Conversations" || ResolveProductLocale("ar").Text("nav.conversations") == "Conversations" {
		t.Fatal("Conversations is not localized")
	}
}

// The rendered sidebar says the same: the favorite reads "Chat" and the child
// under Chat reads "Conversations".
func TestTodo_AGENTUX_044_Browser(t *testing.T) {
	view := agentUX044View(t, "en-US", PageChat)
	view.Page = PageChat
	markup, err := ui.RenderToString(ui.CreateElement(NavigationSidebar, navigationSidebarProps(view)))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(agentUX055Text(markup)), " ")
	english := ResolveProductLocale("en-US")
	favoritesHeading, allHeading := english.Text("nav.favorites"), english.Text("nav.all")
	start, end := strings.Index(text, favoritesHeading), strings.Index(text, allHeading)
	if favoritesHeading == "" || allHeading == "" || start < 0 || end < start {
		t.Fatalf("the sidebar has no Favorites section before the full navigation: %s", text)
	}
	if favorites := text[start:end]; !strings.Contains(favorites, "Chat") || strings.Contains(favorites, "Overview") {
		t.Fatalf("the Favorites section names the Chat favorite by its menu position: %q", favorites)
	}
	if !strings.Contains(text[end:], "Chat Conversations Agents") {
		t.Fatalf("under Chat the sidebar does not read Conversations then Agents: %s", text[end:])
	}
}
