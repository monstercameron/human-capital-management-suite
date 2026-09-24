package chatui

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDelegatedActionsDispatchOnlyTheirIntendedCallback(t *testing.T) {
	for _, tc := range []struct {
		action string
		want   []string
	}{
		{"select", []string{"select:target", "sidebar:false"}},
		{"toggle-section", []string{"section:target"}},
		{"section-up", []string{"reorder:target:-1"}}, {"section-down", []string{"reorder:target:1"}},
		{"open-create", []string{"create"}}, {"close-create", []string{"close-create"}},
		{"open-browse", []string{"browse"}}, {"close-browse", []string{"close-browse"}},
		{"browse-to-create", []string{"close-browse", "create"}}, {"join", []string{"request-join:target"}},
		{"open-rail", []string{"sidebar:true"}}, {"close-rail", []string{"sidebar:false"}},
		{"details", []string{"details:true"}}, {"close-details", []string{"details:false"}},
		{"open-person", []string{"person:target"}}, {"close-person", []string{"close-person"}}, {"start-direct-message", []string{"dm:target"}},
		{"refresh-members", []string{"members"}}, {"load-older", []string{"older"}}, {"retry", []string{"retry"}},
		{"reply", []string{"thread:target"}}, {"stats", []string{"thread:target"}},
		{"close-thread", []string{"close-thread"}}, {"follow", []string{"follow:true"}},
		{"react-pick", []string{"picker:target"}}, {"menu", []string{"menu:target"}},
		{"copy-link", []string{"copy:target"}}, {"jump-newest", []string{"newest"}},
		{"react", []string{"react:target"}}, {"unreact", []string{"unreact:target"}},
		{"pin", []string{"pin:target"}}, {"unpin", []string{"unpin:target"}},
		{"edit", []string{"edit:target"}}, {"cancel-edit", []string{"cancel-edit"}},
		{"delete", []string{"delete:target:7"}}, {"dismiss-notice", []string{"dismiss"}}, {"unknown", nil},
	} {
		t.Run(tc.action, func(t *testing.T) {
			var calls []string
			add := func(s string) { calls = append(calls, s) }
			plain := func(s string) func() { return func() { add(s) } }
			id := func(s string) func(string) { return func(v string) { add(s + ":" + v) } }
			flag := func(s string) func(bool) { return func(v bool) { add(fmt.Sprintf("%s:%t", s, v)) } }
			m := Model{SidebarOpen: true, Messages: []Message{{ID: "target", Revision: 7}}, Callbacks: Callbacks{
				SelectConversation: id("select"), ToggleSection: id("section"), ReorderSection: func(s string, d int) { add(fmt.Sprintf("reorder:%s:%d", s, d)) },
				OpenCreate: plain("create"), CloseCreate: plain("close-create"), OpenBrowse: plain("browse"), CloseBrowse: plain("close-browse"), RequestJoinConversation: id("request-join"), JoinConversation: id("join"),
				ToggleSidebar: flag("sidebar"), ToggleDetails: flag("details"), LoadMembers: plain("members"), LoadOlder: plain("older"), Retry: plain("retry"),
				OpenThread: id("thread"), CloseThread: plain("close-thread"), SetThreadFollow: flag("follow"), OpenPicker: id("picker"), OpenMenu: id("menu"),
				CopyLink: id("copy"), JumpToNewest: plain("newest"), React: id("react"), RemoveReaction: id("unreact"), Pin: id("pin"), Unpin: id("unpin"),
				BeginEdit: id("edit"), CancelEdit: plain("cancel-edit"), DeleteMessage: func(s string, v uint64) { add(fmt.Sprintf("delete:%s:%d", s, v)) }, DismissNotice: plain("dismiss"),
				OpenPerson: id("person"), ClosePerson: plain("close-person"), StartDirectMessage: id("dm"),
			}}
			m.act(tc.action, "target")
			if !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("calls=%v, want %v", calls, tc.want)
			}
			// A disconnected/render-only component must never panic or invoke
			// a mutation just because a control's callback is absent.
			calls = nil
			m.Callbacks = Callbacks{}
			m.act(tc.action, "target")
			if len(calls) != 0 {
				t.Fatalf("disconnected control emitted %v", calls)
			}
		})
	}
}

func TestReactionAndMenuDispatchPreservesMessageIdentity(t *testing.T) {
	var calls []string
	m := Model{MenuID: "open", Messages: []Message{{ID: "one", Chips: []ReactionChip{{Emoji: "thumb", Mine: true}}}, {ID: "two"}}, Callbacks: Callbacks{
		ReactWith:          func(id, emoji string) { calls = append(calls, "add:"+id+":"+emoji) },
		RemoveReactionWith: func(id, emoji string) { calls = append(calls, "remove:"+id+":"+emoji) },
		OpenPicker:         func(id string) { calls = append(calls, "picker:"+id) },
		OpenMenu:           func(id string) { calls = append(calls, "menu:"+id) },
		CopyLink:           func(id string) { calls = append(calls, "copy:"+id) },
	}}
	m.actWith("toggle-reaction", "one", "thumb")
	m.actWith("toggle-reaction", "two", "thumb")
	m.actWith("react-with", "two", "heart")
	m.actWith("react-with", "two", "")
	m.actWith("copy-link", "two", "")
	m.actWith("menu", "open", "")
	want := []string{"remove:one:thumb", "add:two:thumb", "add:two:heart", "picker:", "picker:", "copy:two", "menu:", "menu:"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v, want %v", calls, want)
	}
	// Unknown message IDs must not borrow another row's delete revision.
	m.Callbacks.DeleteMessage = func(string, uint64) { t.Fatal("deleted an absent message") }
	m.act("delete", "absent")
}

func TestRailMenuActionsTargetRowIndependentOfSelection(t *testing.T) {
	var calls []string
	m := Model{SelectedID: "selected", RailMenuID: "other", Conversations: []Conversation{{ID: "selected", Name: "Selected"}, {ID: "other", Name: "Q4 hiring huddle", Kind: PublicChannel}}, Callbacks: Callbacks{
		OpenRailMenu:                func(id string) { calls = append(calls, "menu:"+id) },
		OpenConversationDetails:     func(id string) { calls = append(calls, "details:"+id) },
		CopyConversationReference:   func(id, label string) { calls = append(calls, "copy:"+id+":"+label) },
		CopyConversationAPICurl:     func(id string) { calls = append(calls, "api-curl:"+id) },
		SetConversationNotification: func(id string, mode NotificationMode) { calls = append(calls, "notify:"+id+":"+string(mode)) },
	}}
	m.act("rail-menu", "other")
	m.actWith("rail-details", "other", "")
	m.actWith("rail-copy-reference", "other", "")
	m.actWith("rail-copy-api-curl", "other", "")
	m.actWith("rail-notify-all", "other", "")
	m.actWith("rail-notify-mentions", "other", "")
	m.actWith("rail-notify-mute", "other", "")
	want := []string{"menu:", "details:other", "menu:", "copy:other:#Q4_hiring_huddle", "menu:", "api-curl:other", "menu:", "notify:other:all", "menu:", "notify:other:mentions", "menu:", "notify:other:mute", "menu:"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v, want %v", calls, want)
	}
	m.actWith("rail-copy-reference", "unknown", "")
	if !reflect.DeepEqual(calls, append(want, "menu:")) {
		t.Fatal("unknown conversation copied a reference")
	}
	m.actWith("rail-copy-api-curl", "unknown", "")
	if !reflect.DeepEqual(calls, append(want, "menu:", "menu:")) {
		t.Fatal("unknown conversation copied an API curl")
	}
}

func TestChatChannelReferenceDelegatesOnlyAdmittedRooms(t *testing.T) {
	selected := []string{}
	m := Model{Conversations: []Conversation{{ID: "room", Joined: true}, {ID: "hidden"}}, Callbacks: Callbacks{SelectConversation: func(id string) { selected = append(selected, id) }}}
	m.act("open-channel-reference", "room")
	m.act("open-channel-reference", "hidden")
	m.act("open-channel-reference", "unknown")
	if !reflect.DeepEqual(selected, []string{"room"}) {
		t.Fatalf("direct selections = %#v, want admitted room only", selected)
	}
}

func TestPaneKeyboardDirectionsAndDefaults(t *testing.T) {
	for _, direction := range []string{"ltr", "rtl"} {
		for _, pane := range []string{"rail", "details"} {
			for _, key := range []string{"ArrowLeft", "ArrowRight"} {
				t.Run(direction+"/"+pane+"/"+key, func(t *testing.T) {
					got := 0
					calls := 0
					save := func(v int) { got = v; calls++ }
					m := Model{Direction: direction, Callbacks: Callbacks{ResizeRail: save, ResizeDetails: save}}
					delta := 16
					if key == "ArrowLeft" {
						delta = -delta
					}
					if direction == "rtl" {
						delta = -delta
					}
					if pane == "details" {
						delta = -delta
					}
					base := RailDefault
					if pane == "details" {
						base = DetailsDefault
					}
					if !m.resizeFromKey(pane, key) || calls != 1 || got != base+delta {
						t.Fatalf("resize=%d calls=%d, want %d", got, calls, base+delta)
					}
					m.Pane = PaneSizes{Rail: 300, Details: 300}
					m.resizeFromKey(pane, key)
					if got != 300+delta || calls != 2 {
						t.Fatalf("existing resize=%d calls=%d", got, calls)
					}
				})
			}
		}
	}
	resets := 0
	m := Model{Callbacks: Callbacks{RestorePanes: func() { resets++ }}}
	if !m.resizeFromKey("rail", "Home") || resets != 1 {
		t.Fatal("Home did not restore panes")
	}
	if m.resizeFromKey("rail", "Enter") || m.resizeFromKey("rail", "ArrowLeft") || m.resizeFromKey("details", "ArrowRight") {
		t.Fatal("unhandled key swallowed")
	}
}

func TestEditDraftCopiesDoNotAliasPriorState(t *testing.T) {
	before := map[string]string{"one": "draft"}
	after := copyMap(before)
	after["one"] = "changed"
	if before["one"] != "draft" || after["one"] != "changed" {
		t.Fatal("draft copy aliased prior state")
	}
	if copyMap[string, string](nil) == nil {
		t.Fatal("nil input must yield writable map")
	}
}
