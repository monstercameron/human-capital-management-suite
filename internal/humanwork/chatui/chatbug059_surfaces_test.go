package chatui

import "testing"

// Chatbug059MoreSurfacesForTest renders the whole-page states Chatbug039SurfacesForTest
// does not reach: the dialogs that open over the workspace, the page's loading,
// failure and empty states, a notice with its retry, the sidebar drawer, a
// person's card, the search results, and an open reaction picker, message menu
// and editor. Like it, the maps are for the external real-catalog tests.
func Chatbug059MoreSurfacesForTest(t *testing.T, catalog Model) map[string]string {
	t.Helper()
	out := map[string]string{}
	base := func() Model {
		m := chat4Fixture(catalog.Locale, "answered", false)
		m.Text, m.Direction = catalog.Text, catalog.Direction
		m.IsTenantAdmin = true
		m.Conversations[0].OwnerID = m.CurrentUser
		m.Members = []Member{{ID: "alice", Name: "Alice", HomeTenantID: "tenant"}, {ID: "bob", Name: "Bob", HomeTenantID: "tenant"}}
		m.Callbacks.CreateConversation = func(ConversationKind, string, []string) {}
		m.Callbacks.AddMembers = func([]string) {}
		m.Callbacks.OpenBrowse, m.Callbacks.OpenCreate = func() {}, func() {}
		m.Callbacks.Search = func(string) {}
		m.Callbacks.SavePreferences = func(Preferences) {}
		return m
	}
	add := func(name string, mutate func(*Model)) {
		m := base()
		mutate(&m)
		out[name] = render(t, m)
	}
	add("create dialog", func(m *Model) { m.ShowCreate, m.NewKind = true, PublicChannel })
	add("create dialog for a private group", func(m *Model) { m.ShowCreate, m.NewKind = true, GroupChat })
	add("add people over details", func(m *Model) { m.ShowDetails, m.ShowAddMembers = true, true })
	add("browse dialog", func(m *Model) {
		m.ShowBrowse = true
		m.Browse = []Conversation{{ID: "b1", Name: "announcements", Kind: PublicChannel, MemberCount: 4}, {ID: "b2", Name: "payroll", Kind: PublicChannel, MemberCount: 1, Joined: true}}
	})
	add("person card", func(m *Model) {
		m.ShowPerson = true
		m.PersonDetails = &PersonDetails{ID: "bob", Name: "Bob", JobTitle: "Engineer", Manager: "Alice", Department: "People", Email: "bob@example.com", Ready: true}
	})
	add("person card unavailable", func(m *Model) {
		m.ShowPerson = true
		m.PersonDetails = &PersonDetails{ID: "bob", Name: "Bob", Unavailable: true}
	})
	add("loading state", func(m *Model) { m.State, m.Conversations, m.Messages, m.SelectedID = StateLoading, nil, nil, "" })
	add("failure state", func(m *Model) {
		m.State, m.Conversations, m.Messages, m.SelectedID, m.Error = StateError, nil, nil, "", "The service did not answer."
	})
	add("notice with retry", func(m *Model) { m.Notice, m.NoticeRetry = "Something went wrong.", true })
	add("no conversations", func(m *Model) { m.Conversations, m.Messages, m.SelectedID = nil, nil, "" })
	add("empty channel", func(m *Model) { m.Messages, m.EphemeralMessages, m.PersonaInvocations = nil, nil, nil })
	add("empty direct message", func(m *Model) {
		m.Conversations = []Conversation{{ID: "dm", Kind: DirectMessage, Name: "Bob", Joined: true}}
		m.SelectedID, m.Messages, m.EphemeralMessages, m.PersonaInvocations = "dm", nil, nil, nil
	})
	add("details of a private channel for a member", func(m *Model) {
		m.IsTenantAdmin, m.ShowDetails = false, true
		m.Conversations[0].Kind, m.Conversations[0].OwnerID = PrivateChannel, "bob"
	})
	add("details of a group", func(m *Model) {
		m.ShowDetails = true
		m.Conversations[0].Kind = GroupChat
	})
	add("sidebar drawer", func(m *Model) { m.SidebarOpen = true })
	add("reaction picker", func(m *Model) { m.PickerID = "question" })
	add("message menu", func(m *Model) { m.MenuID = "question" })
	add("message editor", func(m *Model) { m.EditingID = "question" })
	add("new messages line", func(m *Model) { m.UnreadFromID = "question" })
	add("search results", func(m *Model) {
		m.Search = "policy"
		m.SearchChannels = []Conversation{{ID: "people", Name: "people-ops", Kind: PublicChannel}}
		m.SearchPeople = []SearchPerson{{ID: "bob", Name: "Bob"}}
		m.SearchMessages = []SearchMessage{{ConversationID: "general", ConversationName: "general", Message: Message{ID: "post", Sequence: 4, Author: "Bob", Body: "Check this policy"}}}
	})
	add("search with no results", func(m *Model) { m.Search = "zzzz" })
	return out
}
