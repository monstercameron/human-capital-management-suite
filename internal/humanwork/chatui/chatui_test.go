package chatui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func render(t *testing.T, m Model) string {
	t.Helper()
	markup, err := ui.RenderToString(Build(m))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestChatSearchGroupsAuthorizedResultsWithoutReplacingConversationState(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "old-room", Search: "launch", Draft: "keep this draft", Messages: []Message{{ID: "existing", Body: "current timeline"}}, SearchChannels: []Conversation{{ID: "room-1", Name: "Launch", Kind: PublicChannel}}, SearchPeople: []SearchPerson{{ID: "worker-1", Name: "Alex Rivera"}}, SearchMessages: []SearchMessage{{ConversationID: "room-1", ConversationName: "Launch", Message: Message{ID: "post-1", Sequence: 42, Author: "Sam Lee", Body: "launch tomorrow"}}},
		Callbacks: Callbacks{Search: func(string) {}, SelectConversation: func(string) {}, OpenPerson: func(string) {}, OpenSearchMessage: func(string, string, uint64) {}, SearchMore: func() {}}}
	markup := render(t, m)
	for _, want := range []string{"Results for “launch”", `aria-label="Results for “launch”"`, "Channels", "People", "Messages", `<mark class="search-hit">Launch</mark>`, "Alex Rivera", `<mark class="search-hit">launch</mark> tomorrow`, `data-action="open-search-message"`, `data-id="room-1"`, `data-extra="post-1"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("search result panel missing %q", want)
		}
	}
	if strings.Contains(markup, "current timeline") || strings.Contains(markup, "keep this draft") {
		t.Fatal("search overlay rendered the current timeline or draft as results")
	}
	if m.SelectedID != "old-room" || m.Draft != "keep this draft" || len(m.Messages) != 1 || m.Messages[0].ID != "existing" {
		t.Fatalf("search rendering changed the open conversation: %+v", m)
	}
}

func TestChatSearchUsesSingularResultStatus(t *testing.T) {
	markup := render(t, Model{State: StateReady, Search: "launch", SearchMessages: []SearchMessage{{ConversationID: "room-1", Message: Message{ID: "post-1"}}}})
	if !strings.Contains(markup, "1 result") || strings.Contains(markup, "1 results") {
		t.Fatalf("singular result status is incorrect: %s", markup)
	}
}

func TestChatSearchMessageActionCarriesConversationPostAndSequence(t *testing.T) {
	var got string
	m := Model{SearchMessages: []SearchMessage{{ConversationID: "room-9", Message: Message{ID: "post-7", Sequence: 777}}}, Callbacks: Callbacks{OpenSearchMessage: func(conversation, post string, sequence uint64) {
		got = conversation + ":" + post + ":" + strconv.FormatUint(sequence, 10)
	}}}
	m.actWith("open-search-message", "room-9", "post-7")
	if got != "room-9:post-7:777" {
		t.Fatalf("search result navigation = %q", got)
	}
}

func TestTodo_CHAT_032(t *testing.T) {
	room := Conversation{ID: "sales", Name: "Sales", Kind: PublicChannel}
	m := Model{State: StateReady, Conversations: []Conversation{room}, Sections: []SidebarSection{{ID: "channels", Name: "Channels"}, {ID: "direct", Name: "Direct messages"}, {ID: "custom-1", Name: "Projects", Collapsed: true, Chats: []Conversation{room}}}, RailMenuID: room.ID,
		Callbacks: Callbacks{CreateSection: func(string) {}, RemoveSection: func(string) {}, MoveConversationSection: func(string, string) {}, ToggleSection: func(string) {}, OpenRailMenu: func(string) {}}}
	markup := render(t, m)
	for _, want := range []string{`<details class="section-create" id="chat-section-create">`, `id="chat-new-section"`, "New section", "Create", "Cancel", `data-action="section-remove" data-id="custom-1"`, `aria-expanded="false"`, `data-action="rail-move-section"`, `data-extra="custom-1"`, "Move to Projects"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(markup, `id="chat-section-create" open`) {
		t.Fatal("new group form should be closed until requested")
	}
	if strings.Contains(markup, `data-action="select" data-id="sales"`) {
		t.Fatal("collapsed group rendered a conversation row")
	}
	m.Sections[2].Collapsed = false
	if !strings.Contains(render(t, m), `data-action="select" data-id="sales"`) {
		t.Fatal("expanded group did not render its conversation")
	}
}

func TestRailMenuRendersSeparateButtonsAndTargetedMode(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "selected", CurrentUser: "owner", RailMenuID: "other", Conversations: []Conversation{{ID: "selected", Name: "Selected"}, {ID: "other", Name: "Other", OwnerID: "owner"}}, Preferences: Preferences{Notifications: map[string]NotificationMode{"other": NotifyMention}}, Callbacks: Callbacks{SelectConversation: func(string) {}, OpenRailMenu: func(string) {}, OpenConversationDetails: func(string) {}, CopyConversationReference: func(string, string) {}, CopyConversationAPICurl: func(string) {}, SetConversationNotification: func(string, NotificationMode) {}}}
	markup := render(t, m)
	for _, want := range []string{`data-action="rail-menu" data-id="other"`, `aria-label="More options for Other"`, `role="menu"`, `data-action="rail-details" data-id="other"`, `data-action="rail-copy-reference" data-id="other"`, "Copy name and link", `data-action="rail-notify-mentions" data-id="other"`, `aria-checked="true"`, "✓"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	// CHAT-08 (retest): the everyday menu never offers the API curl action
	// at all now, for anyone -- it moved to Details' Integrations section.
	if strings.Contains(markup, "rail-copy-api-curl") || strings.Contains(markup, "Copy API curl") {
		t.Error("the rail menu still offers Copy API curl; it must live only in Details' Integrations section")
	}
	if strings.Contains(markup, `<button class="chat-row`) && strings.Contains(markup, `<button class="rail-row-more`) {
		// The row's two buttons must be siblings, not nested interactive controls.
		start := strings.Index(markup, `data-action="select" data-id="other"`)
		end := strings.Index(markup[start:], `data-action="rail-menu" data-id="other"`)
		if start < 0 || end < 0 || !strings.Contains(markup[start:start+end], "</button>") {
			t.Fatal("rail controls are nested")
		}
	}
}

// TestIntegrationsSectionGatedToAdminOrOwner covers CHAT-08's retest: the
// API curl action lives in Details' Integrations section now, described
// with what it needs, and hidden from anyone who is neither a tenant admin
// nor this conversation's owner.
func TestIntegrationsSectionGatedToAdminOrOwner(t *testing.T) {
	base := Model{State: StateReady, SelectedID: "room", ShowDetails: true,
		Conversations: []Conversation{{ID: "room", Name: "Room", Kind: PublicChannel, OwnerID: "owner-1"}},
		Callbacks:     Callbacks{CopyConversationAPICurl: func(string) {}}}

	neither := base
	neither.CurrentUser = "someone-else"
	markup := render(t, neither)
	if strings.Contains(markup, "integrations-section") || strings.Contains(markup, `data-action="copy-conversation-api-curl"`) {
		t.Error("Integrations showed to a member who is neither the owner nor a tenant admin")
	}

	owner := base
	owner.CurrentUser = "owner-1"
	markup = render(t, owner)
	for _, want := range []string{`class="details-section integrations-section"`, "Integrations", "Let an installed app read and post here", `data-action="copy-conversation-api-curl" data-id="room"`, "Copy API curl"} {
		if !strings.Contains(markup, want) {
			t.Errorf("Integrations section (owner) missing %q", want)
		}
	}

	admin := base
	admin.CurrentUser = "someone-else"
	admin.IsTenantAdmin = true
	markup = render(t, admin)
	if !strings.Contains(markup, "integrations-section") {
		t.Error("Integrations did not show to a tenant admin who does not own the conversation")
	}
}

func TestChatProfilePhotosUseAuthorizedURLsAndKeepInitialsFallback(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", CurrentUser: "viewer", PeerIDs: map[string]string{"dm": "peer"}, PhotoURLs: map[string]string{"author": "/workspace/assets/person-author.jpg", "member": "/workspace/assets/person-member.jpg", "peer": "/workspace/assets/person-dm.jpg"}, Conversations: []Conversation{{ID: "room", Name: "Room", Kind: PublicChannel}, {ID: "dm", Name: "Dana Moore", Kind: DirectMessage}, {ID: "group", Name: "Group", Kind: GroupChat}}, Messages: []Message{{ID: "one", AuthorID: "author", Author: "Ari Chen", Body: "Hello"}, {ID: "two", AuthorID: "missing", Author: "No Photo", Body: "Hi"}}, Members: []Member{{ID: "member", Name: "Maya Lee"}}, ShowDetails: true}
	markup := render(t, m)
	for _, want := range []string{"person-author.jpg", "person-member.jpg", "person-dm.jpg", ">NP</span>", `class="chat-avatar-photo"`, `width="64"`, `height="64"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("avatar missing %q", want)
		}
	}
	if strings.Contains(markup, "person-group") || strings.Contains(markup, "person-room") {
		t.Fatal("non-person conversation rendered as a photo")
	}
}

func TestPersonDetailsOpensFromChatIdentitiesAndShowsDirectoryFields(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "dm", CurrentUser: "viewer", ShowDetails: true, ShowThread: true, ShowPerson: true,
		Conversations: []Conversation{{ID: "dm", Name: "Ari Chen", Kind: DirectMessage}}, PeerIDs: map[string]string{"dm": "ari"},
		Messages: []Message{{ID: "post", AuthorID: "ari", Author: "Ari Chen", Body: "Hello"}}, Members: []Member{{ID: "ari", Name: "Ari Chen"}},
		PersonDetails: &PersonDetails{ID: "ari", Name: "Ari Chen", JobTitle: "Engineer", Manager: "Maya Lee", Department: "Product", Location: "Boston", Ready: true},
		Callbacks:     Callbacks{OpenPerson: func(string) {}, ClosePerson: func() {}, StartDirectMessage: func(string) {}},
	}
	markup := render(t, m)
	for _, want := range []string{`data-action="open-person" data-id="ari"`, `aria-label="View Ari Chen&#39;s details"`, `class="chat-side person-pane"`, "Ari Chen", "Maya Lee", "Product", "Boston", "Not available", `data-action="start-direct-message" data-id="ari"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(markup, `class="chat-side thread-pane"`) || strings.Contains(markup, `class="chat-side chat-details"`) {
		t.Fatal("person details did not own the side column")
	}
	m.PersonDetails.Ready = false
	if !strings.Contains(render(t, m), `data-action="start-direct-message" data-id="ari" disabled`) {
		t.Fatal("message action enabled before directory authorization")
	}
	m.PersonDetails.Ready = true
	m.CurrentUser = "ari"
	if !strings.Contains(render(t, m), `data-action="start-direct-message" data-id="ari"`) || strings.Contains(render(t, m), `data-action="start-direct-message" data-id="ari" disabled`) {
		t.Fatal("self direct message action was unavailable")
	}
}

func TestPersonDetailsLinksGovernedManagerAndDirectReports(t *testing.T) {
	m := Model{State: StateReady, ShowPerson: true,
		PersonDetails: &PersonDetails{ID: "worker", Name: "Pat Person", Manager: "Morgan Manager", ManagerID: "manager", ManagerPhotoURL: "/photos/manager", OrgChartHref: "/workspace/app/organization?org_view=tree&person=worker", Ready: true,
			DirectReports: []PersonLink{{ID: "report-a", Name: "Alex Rivera", PhotoURL: "/photos/alex"}, {ID: "report-b", Name: "Taylor Jones"}}},
		Callbacks: Callbacks{OpenPerson: func(string) {}, ClosePerson: func() {}},
	}
	markup := render(t, m)
	for _, want := range []string{
		`data-action="open-person" data-id="manager"`, `aria-label="View Morgan Manager&#39;s details"`,
		`data-action="open-person" data-id="report-a"`, `aria-label="View Alex Rivera&#39;s details"`,
		`data-action="open-person" data-id="report-b"`, `Direct reports`,
		`href="/workspace/app/organization?org_view=tree&amp;person=worker"`, `View in org chart`,
		`src="/photos/manager"`, `src="/photos/alex"`, `data-id="report-b" type="button"><span aria-hidden="true" class="avatar small person-detail-avatar">TJ</span>`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing clickable relationship %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, `src=""`) || strings.Contains(markup, `data-base-pay`) {
		t.Fatal("person relationship rendering exposed empty or restricted profile data")
	}
	if strings.Contains(markup, `data-action="open-person" data-id="worker"`) {
		t.Fatal("the current person should not be linked as their own manager or report")
	}
	loading := render(t, Model{ShowPerson: true, PersonDetails: &PersonDetails{ID: "worker"}})
	if !strings.Contains(loading, `role="status"`) || !strings.Contains(loading, "Loading person details") {
		t.Fatal("loading person details are not announced accessibly")
	}
	unavailable := render(t, Model{ShowPerson: true, PersonDetails: &PersonDetails{ID: "worker", Unavailable: true}})
	if !strings.Contains(unavailable, `role="status"`) || !strings.Contains(unavailable, "Person details are unavailable") {
		t.Fatal("unavailable person details are not announced accessibly")
	}
}

func TestTodo_CHAT_031_RenderStatesAndAccessibleWorkspace(t *testing.T) {
	m := Model{State: StateReady, Locale: "en-US", CurrentUser: "morgan", SelectedID: "eng", Conversations: []Conversation{{ID: "eng", Name: "Engineering", Kind: PublicChannel, MemberCount: 12}}, Messages: []Message{{ID: "m1", AuthorID: "ari", Author: "Ari Chen", Body: "Release is ready", Replies: 2}}}
	markup := render(t, m)
	for _, want := range []string{"chat-workspace", "Chat navigation", "Conversation messages", "chat-composer", "Release is ready", "for=\"chat-composer\"", "12 members", "2 replies", "Enter to send"} {
		if !strings.Contains(markup, want) {
			t.Errorf("render missing %q: %s", want, markup)
		}
	}
	for _, state := range []LoadState{StateLoading, StateEmpty, StateError} {
		m.State, m.Error = state, "Network unavailable"
		markup = render(t, m)
		if !strings.Contains(markup, `data-chat-state="`+string(state)+`"`) {
			t.Errorf("state %s not exposed", state)
		}
	}
	if !strings.Contains(markup, "Network unavailable") || !strings.Contains(markup, "Try again") {
		t.Fatal("error state must show the message and a retry")
	}
}

func TestTodo_CHAT_031_NoSelectionInvitesAChoice(t *testing.T) {
	markup := render(t, Model{State: StateReady, Conversations: []Conversation{{ID: "a", Name: "general"}}})
	if !strings.Contains(markup, "Choose a conversation") || !strings.Contains(markup, "Pick a channel or a direct message") {
		t.Fatal("no-selection state missing")
	}
	if strings.Contains(markup, `id="chat-composer"`) {
		t.Fatal("no composer may render without a selection")
	}
	if strings.Count(markup, "<h1") != 1 {
		t.Fatal("exactly one h1 must remain for the document outline")
	}
}

func TestTodo_CHAT_032_033_034_035_PersonalStateIsBoundedAndSealed(t *testing.T) {
	saved := 0
	m := Model{SelectedID: "dm", Preferences: Preferences{}, Pane: PaneSizes{}, Callbacks: Callbacks{SavePreferences: func(Preferences) { saved++ }}}
	m.ResizeRail(100)
	if m.Pane.Rail != 220 {
		t.Fatalf("rail lower bound = %d", m.Pane.Rail)
	}
	m.ResizeDetails(999)
	if m.Pane.Details != 440 {
		t.Fatalf("details upper bound = %d", m.Pane.Details)
	}
	m.SetDraft("private draft")
	if m.Preferences.Drafts["dm"] != "private draft" {
		t.Fatal("draft not keyed by conversation")
	}
	if saved != 2 {
		t.Fatalf("save callbacks = %d", saved)
	}
	if direction("ar") != "rtl" || direction("de-DE") != "ltr" {
		t.Fatal("locale direction")
	}
}

func TestTodo_CHAT_035_SendDoesNotSubmitBlankOrCrossConversationDraft(t *testing.T) {
	var gotID, gotBody string
	m := Model{SelectedID: "one", Draft: "  ", Callbacks: Callbacks{SendMessage: func(id, body string) { gotID, gotBody = id, body }}}
	m.Send()
	if gotID != "" || gotBody != "" {
		t.Fatal("blank draft sent")
	}
	m.SetDraft("hello")
	m.Send()
	if gotID != "one" || gotBody != "hello" || m.Draft != "" {
		t.Fatalf("send = %q %q draft=%q", gotID, gotBody, m.Draft)
	}
}

func TestTodo_CHAT_031_DetailsAndConversationVariants(t *testing.T) {
	m := Model{State: StateReady, Locale: "ar", Direction: "rtl", ShowDetails: true, SelectedID: "g", Conversations: []Conversation{{ID: "g", Name: "Team", Kind: GroupChat}, {ID: "p", Name: "Private", Kind: PrivateChannel}, {ID: "d", Name: "Ari", Kind: DirectMessage}}, Members: []Member{{Name: "Ari Chen", Online: true}, {Name: "Sam Lee", Subtitle: "Payroll"}}, Messages: []Message{{ID: "x", Author: "Sam Lee", Body: "Pinned", Edited: true, Pinned: true, Reactions: 2, Replies: 1, Chips: []ReactionChip{{Emoji: "👍", Count: 2, Mine: true}}}}}
	markup := render(t, m)
	for _, want := range []string{`dir="rtl"`, "Members", "Ari Chen", "Online", "Payroll", "edited", "Pinned", "👍", "1 reply", "Channels", "Direct messages", "notify-mode"} {
		if !strings.Contains(markup, want) {
			t.Errorf("details render missing %q", want)
		}
	}
	if strings.Contains(markup, "Offline") {
		t.Fatal("members without a presence signal must not be labelled offline")
	}
	if got := iconFor(PublicChannel) + iconFor(PrivateChannel) + iconFor(DirectMessage) + iconFor(GroupChat); got == "" {
		t.Fatal("conversation icons empty")
	}
	if initials("") != "?" || initials("Ari Chen") != "AC" || statusText(true) != "Online" || statusText(false) != "Offline" {
		t.Fatal("identity helpers")
	}
	if len(paneStyle(PaneSizes{})) != 0 {
		t.Fatal("empty pane style should be omitted")
	}
}

func TestTodo_CHAT_032_PreferencesAndNoopActionsAreRepresented(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "c", Conversations: []Conversation{{ID: "c", Name: "Channel", Kind: PublicChannel}}, Messages: []Message{{ID: "x", Author: "Ari", Body: "Hello"}}}
	markup := render(t, m)
	for _, want := range []string{"disabled", "data-chat-state=\"ready\"", "Send"} {
		if !strings.Contains(markup, want) {
			t.Errorf("unavailable action missing %q", want)
		}
	}
}

func TestTodo_CHAT_033_035_MobileSidebarAndCreateDialogAreSingleRoot(t *testing.T) {
	m := Model{State: StateReady, ShowCreate: true, SidebarOpen: true, NewKind: GroupChat, SelectedID: "c", Conversations: []Conversation{{ID: "c", Name: "Channel"}}}
	markup := render(t, m)
	if strings.Count(markup, `class="chat-workspace"`) != 1 {
		t.Fatalf("workspace roots = %d", strings.Count(markup, `class="chat-workspace"`))
	}
	for _, want := range []string{`data-sidebar-open="true"`, "new-chat-kind", "new-chat-members", "Create conversation", "chat-scrim"} {
		if !strings.Contains(markup, want) {
			t.Errorf("create/mobile markup missing %q", want)
		}
	}
	if !strings.Contains(markup, `value="group" selected`) && !strings.Contains(markup, `selected`) {
		t.Fatal("chosen kind must be preselected")
	}
}

func TestTodo_CHAT_031_ThreadAndCreateControlsExposeKeyboardNames(t *testing.T) {
	createOpened, createClosed, threadClosed := 0, 0, 0
	m := Model{State: StateReady, ShowCreate: true, ShowThread: true, SidebarOpen: true, ThreadParentID: "root", NewKind: GroupChat, SelectedID: "c", Conversations: []Conversation{{ID: "c", Name: "Channel"}}, Messages: []Message{{ID: "root", Author: "Ari", Body: "Root"}}, ThreadMessages: []Message{{ID: "reply", Author: "Sam", Body: "Reply"}}, Callbacks: Callbacks{OpenCreate: func() { createOpened++ }, CloseCreate: func() { createClosed++ }, CloseThread: func() { threadClosed++ }, ToggleSidebar: func(bool) {}}}
	markup := render(t, m)
	for _, want := range []string{"chat-dialog", "Conversation type", "Add people", "Thread", "Reply", "Close thread", "data-thread-open=\"true\"", "data-sidebar-open=\"true\"", "thread-root", "Root"} {
		if !strings.Contains(markup, want) {
			t.Errorf("interactive surface missing %q", want)
		}
	}
	if createOpened != 0 || createClosed != 0 || threadClosed != 0 {
		t.Fatal("SSR rendered callbacks unexpectedly")
	}
}

func TestTodo_CHAT_032_035_CallbacksAndPreferenceCopies(t *testing.T) {
	selected, draftID, draftValue, sectionID := "", "", "", ""
	saved := Preferences{}
	m := Model{SelectedID: "c", Callbacks: Callbacks{SelectConversation: func(id string) { selected = id }, DraftChanged: func(id, value string) { draftID, draftValue = id, value }, SavePreferences: func(p Preferences) { saved = p }, ToggleSection: func(id string) { sectionID = id }}}
	m.Select("next")
	m.SetDraft("keep this")
	m.ResizeRail(999)
	if selected != "next" || draftID != "next" || draftValue != "keep this" || m.Preferences.Drafts["next"] != "keep this" {
		t.Fatal("selection/draft callbacks")
	}
	if saved.Panes.Rail != 420 || sectionID != "" {
		t.Fatal("bounded preference state")
	}
	m.Callbacks.ToggleSection("people")
	if sectionID != "people" {
		t.Fatal("section toggle callback")
	}
}

func TestTodo_CHAT_031_ScopedStylesheetProtectsShellSelectors(t *testing.T) {
	css := ScopedStylesheet()
	if !strings.HasPrefix(css, "@scope (.chat-workspace){") || !strings.Contains(css, ".chat-details.collapsed{display:none}") || !strings.Contains(css, `:scope[data-sidebar-open="true"]`) {
		t.Fatalf("scoped stylesheet missing isolation or responsive rules")
	}
	if strings.Contains(css, "#635bff") || strings.Contains(css, "Inter,") {
		t.Fatal("chat must use shell tokens, not its own palette or typeface")
	}
	for _, token := range []string{"var(--accent)", "var(--surface)", "var(--canvas)", "var(--ink)", "var(--muted)", "var(--line)", "var(--hcm-radius-control)", "var(--hcm-color-focus)"} {
		if !strings.Contains(css, token) {
			t.Errorf("stylesheet missing shell token %s", token)
		}
	}
}

func TestTodo_CHAT_034_QuietHoursAndThreadFollowControls(t *testing.T) {
	m := Model{State: StateReady, ShowThread: true, ThreadFollowed: true, ThreadParentID: "root", SelectedID: "c", Conversations: []Conversation{{ID: "c", Name: "Channel"}}, Messages: []Message{{ID: "root", Author: "Ari", Body: "Root"}}, Preferences: Preferences{QuietHours: true, QuietTimezone: "America/New_York", QuietStartMinute: 1320, QuietEndMinute: 420}, Callbacks: Callbacks{SetThreadFollow: func(bool) {}, SavePreferences: func(Preferences) {}}}
	markup := render(t, m)
	for _, want := range []string{"Following", "aria-pressed=\"true\"", "quiet-timezone", "America/New_York", "22:00", "07:00"} {
		if !strings.Contains(markup, want) {
			t.Errorf("quiet/follow control missing %q", want)
		}
	}
	if minuteClock(1500) != "23:59" || parseClock("08:30") != 510 || followLabel(false) != "Follow" {
		t.Fatal("quiet/follow helpers")
	}
}

func TestTodo_CHAT_031_AllAuthorizedControlsRender(t *testing.T) {
	m := Model{State: StateReady, ShowCreate: true, ShowDetails: true, CurrentUser: "ari", ThreadParentID: "m", MenuID: "m", PickerID: "m", SelectedID: "c", NewKind: PublicChannel, HasOlder: true, Notice: "Saved", Conversations: []Conversation{{ID: "c", Name: "Channel", Mentions: 2}, {ID: "u", Name: "Unread", Unread: 3}}, Messages: []Message{{ID: "m", AuthorID: "ari", Author: "Ari", Body: "Hello", Reacted: true, Pinned: true}}, Members: []Member{{Name: "Sam", Online: true}}, Callbacks: Callbacks{OpenCreate: func() {}, CloseCreate: func() {}, CreateConversation: func(ConversationKind, string, []string) {}, OpenBrowse: func() {}, SelectConversation: func(string) {}, ToggleSection: func(string) {}, ReorderSection: func(string, int) {}, OpenThread: func(string) {}, CloseThread: func() {}, SetThreadFollow: func(bool) {}, ToggleDetails: func(bool) {}, LoadMembers: func() {}, LoadOlder: func() {}, SendMessage: func(string, string) {}, BeginEdit: func(string) {}, DeleteMessage: func(string, uint64) {}, EditMessage: func(string, string, uint64) {}, React: func(string) {}, RemoveReaction: func(string) {}, Pin: func(string) {}, Unpin: func(string) {}, ResizeDetails: func(int) {}, RestorePanes: func() {}, Retry: func() {}, DismissNotice: func() {}}}
	markup := render(t, m)
	for _, want := range []string{"Remove your reaction", "Unpin message", "Edit message", "Delete message", "Show earlier messages", "chat-notice", "Saved", "chat-badge mention", "Browse channels", `data-details-open="true"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("authorized control missing %q", want)
		}
	}
}

func TestTodo_CHAT_031_MessagesGroupByAuthorAndDay(t *testing.T) {
	base := time.Date(2025, 3, 4, 9, 0, 0, 0, time.UTC)
	m := Model{State: StateReady, SelectedID: "c", Conversations: []Conversation{{ID: "c", Name: "Channel"}}, Messages: []Message{
		{ID: "1", AuthorID: "a", Author: "Ari", Body: "one", SentAt: base, TimeLabel: "9:00"},
		{ID: "2", AuthorID: "a", Author: "Ari", Body: "two", SentAt: base.Add(2 * time.Minute), TimeLabel: "9:02"},
		{ID: "3", AuthorID: "a", Author: "Ari", Body: "three", SentAt: base.Add(20 * time.Minute), TimeLabel: "9:20"},
		{ID: "4", AuthorID: "b", Author: "Sam", Body: "four", SentAt: base.AddDate(0, 0, 1), TimeLabel: "9:00"},
	}}
	markup := render(t, m)
	if got := strings.Count(markup, `class="message continued"`); got != 1 {
		t.Fatalf("continued messages = %d, want 1 (only the reply within five minutes)", got)
	}
	if got := strings.Count(markup, `class="day-divider"`); got != 2 {
		t.Fatalf("day dividers = %d, want 2", got)
	}
	if !strings.Contains(markup, "March 4, 2025") {
		t.Fatal("older days are labelled with the date")
	}
}

func TestTodo_CHAT_031_BrowseDialogOffersJoin(t *testing.T) {
	joined := ""
	m := Model{State: StateReady, ShowBrowse: true, Browse: []Conversation{{ID: "x", Name: "announcements", Kind: PublicChannel, MemberCount: 40}, {ID: "y", Name: "design", Kind: PublicChannel, Joined: true}}, Callbacks: Callbacks{JoinConversation: func(id string) { joined = id }, CloseBrowse: func() {}}}
	markup := render(t, m)
	for _, want := range []string{"browse-dialog", "announcements", "40 members", ">Join<", "Joined"} {
		if !strings.Contains(markup, want) {
			t.Errorf("browse dialog missing %q", want)
		}
	}
	if joined != "" {
		t.Fatal("SSR must not invoke join")
	}
	empty := render(t, Model{State: StateReady, ShowBrowse: true})
	if !strings.Contains(empty, "No channels to join yet.") {
		t.Fatal("empty browse copy")
	}
}

func TestTodo_CHAT_031_CopyResolvesThroughTextHook(t *testing.T) {
	m := Model{State: StateReady, Locale: "de-DE", Text: func(key string) string {
		if key == KeyRailTitle {
			return "Unterhaltungen"
		}
		return ""
	}}
	markup := render(t, m)
	if !strings.Contains(markup, "Unterhaltungen") {
		t.Fatal("translated key not used")
	}
	if !strings.Contains(markup, "Browse channels") {
		t.Fatal("missing translation must fall back to English, never a raw key")
	}
	if strings.Contains(markup, "chat.") {
		t.Fatalf("raw key leaked into markup: %s", markup)
	}
	for key := range englishCopy {
		if !strings.HasPrefix(key, "chat.") {
			t.Errorf("key %q outside the chat namespace", key)
		}
	}
	if got := splitMembers(" ari, sam ;; lee ,"); len(got) != 3 || got[2] != "lee" {
		t.Fatalf("splitMembers = %v", got)
	}
	if splitMembers("  ") != nil {
		t.Fatal("blank members must be nil")
	}
}

func TestTodo_CHAT_032_035_ModelMutationsCoverBoundsAndCallbacks(t *testing.T) {
	selected, sent := "", ""
	m := Model{SelectedID: "c", Draft: "hello", Preferences: Preferences{Drafts: map[string]string{"d": "draft for d"}}, Callbacks: Callbacks{SelectConversation: func(id string) { selected = id }, SendMessage: func(id, body string) { sent = id + ":" + body }, SavePreferences: func(Preferences) {}}}
	m.Select("d")
	m.Send()
	m.SetDraft("again")
	m.ResizeRail(-1)
	m.ResizeDetails(999)
	if selected != "d" || sent != "d:draft for d" || m.Preferences.Drafts["c"] != "hello" || m.Pane.Rail != 220 || m.Pane.Details != 440 {
		t.Fatal("model mutation callbacks/bounds")
	}
	m.Send()
	if sent != "d:again" {
		t.Fatal("second send")
	}
	if got := paneStyle(PaneSizes{Rail: 280, Details: 300}); got["--chat-rail"] != "280px" {
		t.Fatal("pane style")
	}
}

func TestTodo_CHAT_034_QuietTimeInputValidation(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{{"", 0}, {"25:99", 1439}, {"-1:-2", 0}, {"09:x", 540}} {
		if got := parseClock(tc.raw); got != tc.want {
			t.Errorf("parseClock(%q)=%d want %d", tc.raw, got, tc.want)
		}
	}
	if minuteClock(-5) != "00:00" {
		t.Fatal("negative minute clamp")
	}
}

func TestTodo_CHAT_035_BlankAndUnselectedSendIsSafe(t *testing.T) {
	m := Model{Draft: "  ", Callbacks: Callbacks{SendMessage: func(string, string) { t.Fatal("blank send callback") }}}
	m.Send()
	m.Draft = "message"
	m.Send()
	if m.selected().ID != "" {
		t.Fatal("zero selected conversation")
	}
}

func TestTodo_CHAT_031_AttachmentsRenderInlineOrAsChips(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "c", Conversations: []Conversation{{ID: "c", Name: "design"}}, Messages: []Message{{ID: "m", AuthorID: "a", Author: "Ari", Body: "Latest mock", Attachments: []Attachment{
		{ID: "img1", Name: "mock.png", ContentType: "image/png", URL: "blob:cached-thumbnail", Width: 640, Height: 400, Bytes: 12345},
		{ID: "gif1", Name: "party.gif", ContentType: "image/gif"},
		{ID: "img2", Name: "unmeasured.png", ContentType: "image/png", URL: "/v1/chat/media/img2?grant=x"},
		{ID: "img3", Name: "loading.png", ContentType: "image/png", Width: 640, Height: 400},
		{ID: "doc1", Name: "bands.xlsx", ContentType: "application/vnd.ms-excel", URL: "/v1/chat/media/doc1?grant=y", Bytes: 48 * 1024},
	}}}}
	markup := render(t, m)
	for _, want := range []string{`class="attachment-image measured"`, `data-media-thumb="thumbnail"`, `data-media-display="display"`, `data-media-original="original"`, `data-media-width="640"`, `data-media-height="400"`, `data-media-bytes="12345"`, `data-media-animated="false"`, `data-media-name="mock.png"`, `alt="mock.png"`, `width="640"`, `attachment-image unmeasured gif`, `attachment-image unmeasured`, `>GIF<`, `class="attachment-chip"`, "bands.xlsx", "48 KB"} {
		if !strings.Contains(markup, want) {
			t.Errorf("attachment markup missing %q", want)
		}
	}
	if strings.Contains(markup, `src="/v1/chat/media/img1`) || strings.Contains(markup, `src="/v1/chat/media/img2`) {
		t.Fatal("timeline image rendered a protected URL as an eager browser source")
	}
	if strings.Contains(markup, `src="blob:cached-thumbnail"`) {
		t.Fatal("timeline reused a stale original Blob instead of starting from its rendition placeholder")
	}
	if got := strings.Count(markup, `data-action="view-image"`); got != 4 {
		t.Errorf("mounted image attachments should expose lazy viewer controls before bytes arrive: got %d controls", got)
	}
	if !strings.Contains(markup, `aria-label="Open image: mock.png"`) || !strings.Contains(markup, `data-action="view-image" data-id="img3"`) {
		t.Fatal("image viewer controls must be named while their authenticated grant is pending")
	}
	if !strings.Contains(ScopedStylesheet(), `.chat-image-viewer img{`) || !strings.Contains(ScopedStylesheet(), `.chat-image-viewer-original.chat-image-original-ready{opacity:1}`) || !strings.Contains(ScopedStylesheet(), `object-fit:contain`) {
		t.Fatal("viewer must fit the full image inside the viewport")
	}
	// The frame rides as data attributes (the product CSP blocks style
	// attributes); both the pending and the loaded image carry the same one.
	if got := min(strings.Count(markup, `data-frame-width="360"`), strings.Count(markup, `data-frame-w="640"`), strings.Count(markup, `data-frame-h="400"`)); got != 2 {
		t.Errorf("known-size pending and loaded images have different frames: got %d shared frames", got)
	}
	if humanBytes(3*1024*1024) != "3 MB" || humanBytes(512) != "512 B" {
		t.Fatal("humanBytes")
	}
	if !(Attachment{ContentType: "IMAGE/GIF"}).IsGIF() || (Attachment{ContentType: "text/plain"}).IsImage() {
		t.Fatal("attachment kind helpers")
	}
}

func TestChatImageViewerKeepsSmallImagesAtNaturalSizeAndOffersZoom(t *testing.T) {
	css := ScopedStylesheet()
	for _, want := range []string{
		`.chat-image-viewer img{display:block;position:absolute;top:50%;left:50%;width:auto;height:auto;max-width:100%;max-height:100%`,
		`.chat-image-viewer-media.chat-image-viewer-zoomed{display:grid;place-items:center;overflow:auto}`,
		`.chat-image-viewer-zoomed .chat-image-viewer-original{position:relative;inset:auto;top:auto;left:auto;width:auto;height:auto;max-width:none;max-height:none`,
		`transition:opacity var(--hcm-motion-normal) var(--hcm-motion-easing)`,
		`@media(prefers-reduced-motion:reduce){.chat-image-viewer-full,.chat-image-viewer-original{transition:none!important}}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("image viewer stylesheet missing %q", want)
		}
	}
	if strings.Contains(css, `.chat-image-viewer img{display:block;position:absolute;inset:0;width:100%;height:100%`) {
		t.Fatal("viewer scales small originals to fill the viewport")
	}
}

func TestChatEmptyReactionPickerDoesNotCreateAChipRow(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", PickerID: "post", Conversations: []Conversation{{ID: "room", Name: "Room"}}, Messages: []Message{{ID: "post", Author: "Ari", Body: "Hello"}}, Callbacks: Callbacks{OpenPicker: func(string) {}, ReactWith: func(string, string) {}}}
	markup := render(t, m)
	if !strings.Contains(markup, `class="reaction-row picker-only"`) || !strings.Contains(markup, `class="reaction-picker"`) {
		t.Fatal("empty picker lacks its anchored popup")
	}
	if strings.Contains(markup, `class="reaction add"`) {
		t.Fatal("empty picker inserted a redundant chip into the message flow")
	}
	if !strings.Contains(Stylesheet, `.reaction-row.picker-only{position:absolute;inset:0;margin:0;pointer-events:none}`) {
		t.Fatal("empty picker wrapper is not removed from message layout")
	}
	m.Messages[0].Chips = []ReactionChip{{Emoji: "👍", Count: 1}}
	withChip := render(t, m)
	if !strings.Contains(withChip, `class="reaction-row"`) || !strings.Contains(withChip, `class="reaction add"`) {
		t.Fatal("existing reaction row lost its add control")
	}
}
