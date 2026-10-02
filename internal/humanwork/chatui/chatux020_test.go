package chatui

import (
	"strings"
	"testing"
)

func chatux020Model() Model {
	sales := Conversation{ID: "sales", Name: "sales", Kind: PublicChannel, Joined: true}
	general := Conversation{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}
	random := Conversation{ID: "random", Name: "random", Kind: PublicChannel, Joined: true, Unread: 2}
	quiet := Conversation{ID: "quiet", Name: "quiet", Kind: PublicChannel, Joined: true}
	loretta := Conversation{ID: "dm-loretta", Name: "Loretta Haynes", Kind: DirectMessage, Joined: true}
	huddle := Conversation{ID: "huddle", Name: "Q4 huddle", Kind: GroupChat, Joined: true}
	rooms := []Conversation{general, quiet, random, sales, loretta, huddle}
	return Model{State: StateReady, Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "t", SelectedID: "general", Conversations: rooms,
		Sections: []SidebarSection{
			{ID: "channels", Name: "Channels", Chats: []Conversation{general, quiet, random}},
			{ID: "direct", Name: "Direct messages", Chats: []Conversation{loretta, huddle}},
			{ID: "custom-1", Name: "Projects", Chats: []Conversation{sales}},
		},
		Callbacks: Callbacks{SelectConversation: func(string) {}, ToggleSection: func(string) {}, OpenRailMenu: func(string) {}, OpenConversationDetails: func(string) {}, CopyConversationReference: func(string, string) {},
			SetConversationNotification: func(string, NotificationMode) {}, MoveConversationSection: func(string, string) {}, MoveConversationOrder: func(string, int) {},
			MarkConversationRead: func(string) {}, MarkConversationUnread: func(string) {}, LeaveConversation: func(string) {}},
	}
}

// A collapsed section still shows the open conversation and the unread ones.
func TestTodo_CHATUX_020(t *testing.T) {
	m := chatux020Model()
	m.Sections[0].Collapsed = true
	got := renderNode(t, railSection(m, m.Sections[0], nil))
	for _, shown := range []string{`data-id="general"`, `data-id="random"`} {
		if !strings.Contains(got, shown) {
			t.Errorf("a collapsed section hides %s: %s", shown, got)
		}
	}
	if strings.Contains(got, `data-action="select" data-id="quiet"`) {
		t.Errorf("a collapsed section shows a read, unopened conversation: %s", got)
	}
	m.Sections[0].Collapsed = false
	if open := renderNode(t, railSection(m, m.Sections[0], nil)); !strings.Contains(open, `data-id="quiet"`) {
		t.Errorf("an open section lost a row: %s", open)
	}
	// A mention counts as unread too.
	mention := Conversation{ID: "m", Name: "m", Kind: PublicChannel, Mentions: 1}
	if !chatux020ShownCollapsed(Model{}, mention) || chatux020ShownCollapsed(Model{SelectedID: "x"}, Conversation{ID: "y"}) || !chatux020ShownCollapsed(Model{SelectedID: "x"}, Conversation{ID: "x"}) {
		t.Error("the rule for a collapsed section")
	}
}

func TestTodo_CHATUX_020_DraftMark(t *testing.T) {
	m := chatux020Model()
	m.railDrafts = map[string]bool{"sales": true, "general": true}
	if got := renderNode(t, railRow(m, m.Conversations[3])); !strings.Contains(got, "chat-row-draft") || !strings.Contains(got, `aria-label="Unsent draft"`) || !strings.Contains(got, "icon-edit") {
		t.Errorf("a row with a draft has no pencil: %s", got)
	}
	// The open conversation's draft is in the box beside it: no mark there.
	if got := renderNode(t, railRow(m, m.Conversations[0])); strings.Contains(got, "chat-row-draft") {
		t.Errorf("the open conversation is marked: %s", got)
	}
	if got := renderNode(t, railRow(m, m.Conversations[1])); strings.Contains(got, "chat-row-draft") {
		t.Errorf("a row without a draft is marked: %s", got)
	}
	// The set the workspace passes down: the browser's drafts, the server's
	// projection, and the open box; an emptied draft is not one.
	model := Model{SelectedID: "a", Draft: "typing", Preferences: Preferences{Drafts: map[string]string{"b": "saved", "c": "  ", "d": "gone"}}}
	store := &browserDrafts{values: map[string]string{"e": "browser", "d": ""}}
	got := chatux020Drafts(model, store)
	for id, want := range map[string]bool{"a": true, "b": true, "c": false, "d": false, "e": true} {
		if got[id] != want {
			t.Errorf("draft %s = %v, want %v (%v)", id, got[id], want, got)
		}
	}
}

// The row menu: Mark as read or unread, the notification group under its heading,
// Move to a section only where it makes sense, and Leave for channels.
func TestTodo_CHATUX_020_RowMenu(t *testing.T) {
	m := chatux020Model()
	menu := func(id string, h handlers) string {
		m.RailMenuID = id
		return renderNode(t, chatux020RailMenu(m, h))
	}
	// An unread channel offers Mark as read, a read one Mark as unread.
	if got := menu("random", handlers{}); !strings.Contains(got, `data-action="rail-mark-read"`) || strings.Contains(got, "rail-mark-unread") || !strings.Contains(got, ">Mark as read<") {
		t.Errorf("an unread row: %s", got)
	}
	got := menu("quiet", handlers{})
	if !strings.Contains(got, `data-action="rail-mark-unread"`) || strings.Contains(got, "rail-mark-read") || !strings.Contains(got, ">Mark as unread<") {
		t.Errorf("a read row: %s", got)
	}
	// The notification choice is a group with a heading naming it.
	if !strings.Contains(got, `role="group"`) || !strings.Contains(got, `aria-labelledby="rail-menu-notify-heading"`) || !strings.Contains(got, `id="rail-menu-notify-heading"`) || !strings.Contains(got, ">Notify me about<") || strings.Count(got, `role="menuitemradio"`) != 3 {
		t.Errorf("the notification group: %s", got)
	}
	// A channel in Channels moves to the sections it is not in and that fit it:
	// Projects, and not Direct messages, and not Channels again.
	if !strings.Contains(got, ">Move to Projects<") || strings.Contains(got, "Move to Direct messages") || strings.Contains(got, "Move to Channels") {
		t.Errorf("the moves of a channel in Channels: %s", got)
	}
	// A direct message moves to Projects, not to Channels; a channel in Projects
	// moves to Channels.
	if dm := menu("dm-loretta", handlers{}); !strings.Contains(dm, ">Move to Projects<") || strings.Contains(dm, "Move to Channels") || strings.Contains(dm, "Move to Direct messages") {
		t.Errorf("the moves of a direct message: %s", dm)
	}
	if moved := menu("sales", handlers{}); !strings.Contains(moved, ">Move to Channels<") || strings.Contains(moved, "Move to Projects") || strings.Contains(moved, "Move to Direct messages") {
		t.Errorf("the moves of a channel in Projects: %s", moved)
	}
	// Up and Down only where the row can go that way.
	first, middle, last := menu("general", handlers{}), menu("quiet", handlers{}), menu("random", handlers{})
	if strings.Contains(first, "rail-chat-up") || !strings.Contains(first, "rail-chat-down") || !strings.Contains(middle, "rail-chat-up") || !strings.Contains(middle, "rail-chat-down") || strings.Contains(last, "rail-chat-down") || !strings.Contains(last, "rail-chat-up") {
		t.Error("a move with nowhere to go is offered")
	}
	// Leave is for channels only, takes two presses, and names what it leaves.
	if !strings.Contains(got, `data-action="rail-leave"`) || !strings.Contains(got, ">Leave channel<") {
		t.Errorf("a channel has no Leave: %s", got)
	}
	for _, id := range []string{"dm-loretta", "huddle"} {
		if strings.Contains(menu(id, handlers{}), "rail-leave") {
			t.Errorf("%s offers Leave", id)
		}
	}
	armed := menu("quiet", handlers{local: localUI{railLeave: "quiet"}})
	if !strings.Contains(armed, `data-action="rail-leave-confirm"`) || !strings.Contains(armed, "Leave quiet? Press again to confirm") || strings.Contains(armed, `data-action="rail-leave"`) {
		t.Errorf("the confirming Leave: %s", armed)
	}
	if other := menu("sales", handlers{local: localUI{railLeave: "quiet"}}); strings.Contains(other, "rail-leave-confirm") {
		t.Errorf("another row's menu asks to confirm: %s", other)
	}
	// The items rest without their callbacks.
	m.Callbacks.LeaveConversation, m.Callbacks.MarkConversationUnread = nil, nil
	rest := menu("quiet", handlers{})
	for _, action := range []string{"rail-leave", "rail-mark-unread"} {
		item := rest[strings.LastIndex(rest[:strings.Index(rest, `data-action="`+action+`"`)], "<button"):]
		if !strings.Contains(item[:strings.Index(item, ">")], "disabled") {
			t.Errorf("%s is live without its callback", action)
		}
	}
}

func TestTodo_CHATUX_020_SectionFit(t *testing.T) {
	channel := Conversation{ID: "c", Kind: PublicChannel}
	private := Conversation{ID: "p", Kind: PrivateChannel}
	direct := Conversation{ID: "d", Kind: DirectMessage}
	group := Conversation{ID: "g", Kind: GroupChat}
	for _, tc := range []struct {
		section string
		c       Conversation
		want    bool
	}{{"channels", channel, true}, {"channels", private, true}, {"channels", direct, false}, {"channels", group, false}, {"direct", direct, true}, {"direct", group, true}, {"direct", channel, false}, {"custom-1", channel, true}, {"custom-1", direct, true}} {
		if got := chatux020SectionFits(SidebarSection{ID: tc.section}, tc.c); got != tc.want {
			t.Errorf("%s holds %s = %v, want %v", tc.section, tc.c.Kind, got, tc.want)
		}
	}
}

// Saved and Moderation stay fixed above the list, the section heading is opaque,
// and the sheet that ships carries both.
func TestTodo_CHATUX_020_FixedRows(t *testing.T) {
	m := chatux020Model()
	m.Moderation = ModerationState{Ready: true, Moderator: true, Open: 1}
	markup := renderNode(t, rail(m, handlers{}))
	fixed, scroll := strings.Index(markup, `class="rail-fixed"`), strings.Index(markup, `class="rail-scroll"`)
	if fixed < 0 || scroll < fixed {
		t.Fatalf("the fixed rows do not come before the list: %d %d", fixed, scroll)
	}
	for _, id := range []string{`id="chatsave-sidebar"`, `id="chatmod005-sidebar"`} {
		at := strings.Index(markup, id)
		if at < fixed || at > scroll {
			t.Errorf("%s is not in the fixed rows (%d, fixed %d, list %d)", id, at, fixed, scroll)
		}
	}
	if !strings.Contains(ChatUX020Styles, ".rail-scroll>.sidebar-section>.section-controls{margin:0;padding-block:14px 4px;background:var(--canvas)}") || !strings.Contains(Stylesheet, ChatUX020Styles) {
		t.Error("the section heading is not opaque in the stylesheet that ships")
	}
}

// Quiet hours starts on the device zone, not on UTC.
func TestTodo_CHATUX_020_QuietZone(t *testing.T) {
	for _, tc := range []struct{ saved, device, want string }{
		{"", "Europe/Berlin", "Europe/Berlin"}, {"  ", "Asia/Tokyo", "Asia/Tokyo"}, {"America/Denver", "Europe/Berlin", "America/Denver"}, {"", "", "UTC"}, {"", " ", "UTC"},
	} {
		if got := quietZone(tc.saved, tc.device); got != tc.want {
			t.Errorf("quietZone(%q, %q) = %q, want %q", tc.saved, tc.device, got, tc.want)
		}
	}
}

// CHATSIDE-001 moved the manual ordering to the foot of the row menu: after a
// divider, as a Reorder pair, and not at all where the order is automatic.
func TestTodo_CHATUX_020_Reorder(t *testing.T) {
	m := chatux020Model()
	m.RailMenuID = "quiet"
	got := renderNode(t, chatux020RailMenu(m, handlers{}))
	separator, heading := strings.Index(got, `role="separator"`), strings.Index(got, ">Reorder<")
	up, down, leave, move := strings.Index(got, `data-action="rail-chat-up"`), strings.Index(got, `data-action="rail-chat-down"`), strings.Index(got, `data-action="rail-leave"`), strings.Index(got, `data-action="rail-move-section"`)
	if separator < 0 || heading < separator || up < heading || down < up {
		t.Fatalf("the Reorder pair is not a headed pair after a divider (separator %d, heading %d, up %d, down %d): %s", separator, heading, up, down, got)
	}
	if leave < 0 || move < 0 || separator < leave || separator < move {
		t.Errorf("Reorder is not the last thing in the menu (leave %d, move %d, divider %d)", leave, move, separator)
	}
	if !strings.Contains(got, `aria-labelledby="rail-menu-reorder-heading"`) || !strings.Contains(got, `id="rail-menu-reorder-heading"`) {
		t.Errorf("the Reorder group is not named by its heading: %s", got)
	}
	// A row with nowhere to go either way has no Reorder at all.
	m.Sections = []SidebarSection{{ID: "channels", Name: "Channels", Chats: []Conversation{{ID: "quiet", Name: "quiet", Kind: PublicChannel, Joined: true}}}}
	if alone := renderNode(t, chatux020RailMenu(m, handlers{})); strings.Contains(alone, "Reorder") || strings.Contains(alone, "menu-separator") {
		t.Errorf("a lone row offers Reorder: %s", alone)
	}
	// A section the page sorts itself (no saved layout) and Favorites offer none.
	m = chatux020Model()
	m.RailMenuID = "quiet"
	m.Sections = nil
	if auto := renderNode(t, chatux020RailMenu(m, handlers{})); strings.Contains(auto, "rail-chat-up") || strings.Contains(auto, "rail-chat-down") || strings.Contains(auto, "Reorder") {
		t.Errorf("an automatically sorted section offers Reorder: %s", auto)
	}
	m = chatux020Model()
	m.RailMenuID = "quiet"
	for i := range m.Conversations {
		if m.Conversations[i].ID == "quiet" {
			m.Conversations[i].Starred = true
		}
	}
	if fav := renderNode(t, chatux020RailMenu(m, handlers{})); strings.Contains(fav, "rail-chat-up") || strings.Contains(fav, "rail-chat-down") {
		t.Errorf("a favorite offers Reorder: %s", fav)
	}
}
