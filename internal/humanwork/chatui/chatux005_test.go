package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	xhtml "golang.org/x/net/html"
)

// chatux005Model is #general with two agents, two pins, a team, a project and a
// status service, as seen by "manager" (a channel manager), "member" (a plain
// member) or "admin".
func chatux005Model(viewer string) Model {
	room := Conversation{ID: "general", Name: "general", Kind: PublicChannel, OwnerID: "walt", MemberCount: 18, Joined: true}
	refs := []ChatReference{
		{Kind: "AGENT_MENTION", ID: "assistant", TenantID: "t", Display: "Assistant", ConversationID: room.ID},
		{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "t", Display: "Policy Helper", ConversationID: room.ID},
	}
	m := Model{State: StateReady, Locale: "en-US", ShowDetails: true, SelectedID: room.ID, CurrentTenantID: "t", Text: func(key string) string { return englishCopy[key] },
		CurrentUser:   map[string]string{"admin": "walt", "manager": "walt", "member": "jake"}[viewer],
		IsTenantAdmin: viewer == "admin",
		Conversations: []Conversation{room},
		Members:       []Member{{ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}, {ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}},
		ChannelTeam: ChannelTeamWidget{Revision: 1, Purpose: "Policy questions", Members: []ChannelTeamMember{
			{HomeTenantID: "t", SubjectID: "jake", Role: "MEMBERSHIP_ROLE_MEMBER"}, {HomeTenantID: "t", SubjectID: "walt", Role: "MEMBERSHIP_ROLE_MANAGER"}}},
		ChannelProject: ChannelProjectWidget{Revision: 1, Title: "Handbook", Milestones: []ChannelProjectMilestone{{ID: "ms1", Text: "Draft", Status: "PLANNED"}}},
		ChannelPins:    []ChannelPin{{PostID: "p1", Author: "Jake Sullivan", Body: "First pin", Sequence: 1}, {PostID: "p2", Author: "Walt Brennan", Body: "Second pin", Sequence: 2}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{
			{Reference: refs[0], Handle: "assistant", Purpose: "Answers everyday questions"},
			{Reference: refs[1], Handle: "policy-helper", Purpose: "Answer policy questions"},
		},
		PersonaLookup: PersonaLookupReady, PersonaLookupConversationID: room.ID,
		ChannelStatuses: map[string]ChannelStatusView{room.ID: {
			Status:      chat.ChannelStatus{TenantID: "t", ConversationID: room.ID, Status: chatpolicy.StatusOpen, Revision: 1},
			Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusLocked, Permission: chatpolicy.PermissionChangeChannelStatus}},
		}},
		ChangeChannelStatus: func(chat.ChangeChannelStatusRequest) {},
		FilterSettings:      func() ui.Node { return html.P(html.Props{Text: "filter settings body"}) },
		Callbacks: Callbacks{SetConversationNotification: func(string, NotificationMode) {}, CopyConversationAPICurl: func(string) {}, ToggleDetails: func(bool) {},
			SendMessageWithReferences: func(string, string, []ChatReference) {}},
	}
	if viewer == "member" {
		// Only a manager is offered the status change.
		m.ChannelStatuses = map[string]ChannelStatusView{room.ID: {Status: chat.ChannelStatus{TenantID: "t", ConversationID: room.ID, Status: chatpolicy.StatusOpen, Revision: 1}}}
		m.ChangeChannelStatus = nil
	}
	return m
}

func chatux005Panel(t *testing.T, m Model, local localUI) string {
	t.Helper()
	return renderNode(t, details(m, handlers{local: local}))
}

func chatux005Before(t *testing.T, markup string, markers ...string) {
	t.Helper()
	last, lastMarker := -1, ""
	for _, marker := range markers {
		at := strings.Index(markup, marker)
		if at < 0 {
			t.Fatalf("the panel has no %q", marker)
		}
		if at < last {
			t.Fatalf("%q comes before %q", marker, lastMarker)
		}
		last, lastMarker = at, marker
	}
}

func TestTodo_CHATUX_005(t *testing.T) {
	// The order: About, Pinned, Notifications for me, Members (agents first, then
	// people), then Manage channel holding the rare sections (CHATUX-030 moved the
	// reader's own settings above the member list).
	manager := chatux005Panel(t, chatux005Model("manager"), localUI{})
	chatux005Before(t, manager,
		`id="chat-details-about"`, `id="chat-details-pinned"`, "Notifications for me", `id="chat-details-members"`, `id="chat-agents-here"`, `id="chat-details-people"`,
		`id="chat-details-manage"`, "manage-status", "Role labels", "Project and milestones", "chatfilter-entry", "integrations-section")
	for _, gone := range []string{"Agents here", "chat-agent-here", "Channel team", "Channel project"} {
		if strings.Contains(manager, gone) {
			t.Errorf("the old %q section is still in the panel", gone)
		}
	}
	// About holds the purpose with its Edit and who created the channel; the
	// model carries no creation time, so none is shown. Status is not shown
	// while the channel is open.
	about := manager[strings.Index(manager, `id="chat-details-about"`):strings.Index(manager, `id="chat-details-pinned"`)]
	for _, want := range []string{"Purpose", "Policy questions", "Created by you"} {
		if !strings.Contains(about, want) {
			t.Errorf("About misses %q: %s", want, about)
		}
	}
	if strings.Contains(about, "details-about-status") {
		t.Errorf("an open channel shows a status line: %s", about)
	}

	// Every heading with a count states it.
	for _, want := range []string{"Pinned · 2", "Members · 18", "Agents · 2", "People · 2", "Role labels", "Project and milestones"} {
		if !strings.Contains(manager, want) {
			t.Errorf("heading %q is missing", want)
		}
	}

	// Under a pinned message the button copies its link.
	if strings.Count(manager, ">Copy link<") != 2 || strings.Contains(manager, "Copy reference") {
		t.Errorf("pinned rows do not offer Copy link: %s", manager)
	}

	// Each agent shows its icon, badge, one line of description and an Ask button;
	// the same two agents are not listed a second time anywhere.
	rows := strings.Count(manager, `class="member-row persona-member-row"`)
	if rows != 2 || strings.Count(manager, "agent-badge") < 2 || strings.Count(manager, `data-action="agent-ask-here"`) != 2 {
		t.Errorf("agent rows=%d badges=%d asks=%d", rows, strings.Count(manager, "agent-badge"), strings.Count(manager, `data-action="agent-ask-here"`))
	}
	for _, want := range []string{"Answers everyday questions", "Answer policy questions", `aria-label="Ask Policy Helper"`} {
		if !strings.Contains(manager, want) {
			t.Errorf("agent row misses %q", want)
		}
	}

	// The channel's owner is named to the others as who created it.
	if got := chatux005Panel(t, chatux005Model("member"), localUI{}); !strings.Contains(got, "Created by Walt Brennan") {
		t.Errorf("a reader is not told who created the channel: %s", got)
	}

	// A plain member sees the first four sections and no Manage channel group.
	member := chatux005Panel(t, chatux005Model("member"), localUI{})
	chatux005Before(t, member, `id="chat-details-about"`, `id="chat-details-pinned"`, "Notifications for me", `id="chat-details-members"`)
	for _, hidden := range []string{"chat-details-manage", "Manage channel", "manage-status", "chatfilter-entry", "integrations-section", "Project and milestones", "filter settings body"} {
		if strings.Contains(member, hidden) {
			t.Errorf("a plain member sees %q", hidden)
		}
	}
	// A person who may change the status, and nothing else, still finds it.
	statusOnly := chatux005Model("member")
	statusOnly.ChannelStatuses = chatux005Model("manager").ChannelStatuses
	statusOnly.ChangeChannelStatus = func(chat.ChangeChannelStatusRequest) {}
	if got := chatux005Panel(t, statusOnly, localUI{}); !strings.Contains(got, "manage-status") || !strings.Contains(got, "Manage channel") {
		t.Errorf("a person who may change the status cannot reach it: %s", got)
	}

	// The status line shows once the channel is not open, and also to a reader.
	at := time.Date(2026, 9, 30, 9, 15, 0, 0, time.UTC)
	for _, viewer := range []string{"manager", "member"} {
		locked := chatux005Model(viewer)
		view := locked.ChannelStatuses["general"]
		view.Status.Status, view.Status.ChangedBy, view.Status.ChangedAt, view.Status.Reason = chatpolicy.StatusLocked, "walt", &at, "Incident review"
		locked.ChannelStatuses = map[string]ChannelStatusView{"general": view}
		panel := chatux005Panel(t, locked, localUI{})
		lockedAbout := panel[strings.Index(panel, `id="chat-details-about"`):strings.Index(panel, `id="chat-details-pinned"`)]
		if !strings.Contains(lockedAbout, "details-about-status") || !strings.Contains(lockedAbout, "Locked") || !strings.Contains(lockedAbout, "Incident review") {
			t.Errorf("%s: a locked channel's About has no status line: %s", viewer, lockedAbout)
		}
	}

	// Stable ids for the header buttons, kept with nothing pinned and no agents.
	bare := chatux005Model("member")
	bare.ChannelPins, bare.ResolvedPersonaMentions = nil, nil
	empty := chatux005Panel(t, bare, localUI{})
	for _, id := range []string{`id="chat-details-pinned"`, `id="chat-details-members"`} {
		if !strings.Contains(empty, id) {
			t.Errorf("%s is gone with nothing pinned", id)
		}
	}
	if !strings.Contains(empty, "Nothing is pinned yet") || strings.Contains(empty, `id="chat-agents-here"`) || strings.Contains(empty, `id="chat-details-people"`) {
		t.Errorf("empty panel: %s", empty)
	}
	if !strings.Contains(manager, `class="details-subheading" id="chat-agents-here" tabIndex="-1"`) {
		t.Errorf("the agents subheading is not the focus target the header uses")
	}

	// The panel remembers the groups the person opened: closed at first, open
	// after the click, and still open in a panel drawn afterwards.
	closed := manager[strings.Index(manager, `data-details-group="manage"`):]
	if !strings.Contains(closed, `aria-expanded="false"`) || !strings.Contains(closed[:strings.Index(closed, `manage-sec-body`)+80], "hidden") {
		t.Errorf("Manage channel is not collapsed at first: %s", closed[:300])
	}
	var local localUI
	local.setDetailGroup("manage", true)
	local.setDetailGroup("project", true)
	reopened := chatux005Panel(t, chatux005Model("manager"), local)
	manage := reopened[strings.Index(reopened, `data-details-group="manage"`):]
	if !strings.Contains(manage, `aria-expanded="true"`) || strings.Contains(manage[:strings.Index(manage, "manage-status")], `hidden=""`) {
		t.Errorf("Manage channel was open and is not drawn open: %s", manage[:400])
	}
	local.setDetailGroup("manage", false)
	if local.detailGroups["manage"] || !local.detailGroups["project"] {
		t.Errorf("closing one group changed the other: %v", local.detailGroups)
	}
	if chatux005Click(ui.MouseEvent{}, localStore{}) {
		t.Error("a click with no group button was taken as one")
	}
}

func TestTodo_CHATUX_005_Accessibility(t *testing.T) {
	for _, viewer := range []string{"manager", "member"} {
		for _, open := range []bool{false, true} {
			var local localUI
			if open {
				local.setDetailGroup("manage", true)
			}
			markup := chatux005Panel(t, chatux005Model(viewer), local)
			root, err := xhtml.Parse(strings.NewReader(markup))
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]*xhtml.Node{}
			var headings []string
			walkChat5HTML(root, func(n *xhtml.Node) {
				if n.Type != xhtml.ElementNode {
					return
				}
				if id := chat5Attr(n, "id"); id != "" {
					if ids[id] != nil {
						t.Errorf("%s/%v: duplicate id %q", viewer, open, id)
					}
					ids[id] = n
				}
				if len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
					headings = append(headings, n.Data)
				}
			})
			// Every reference points at something in the panel.
			walkChat5HTML(root, func(n *xhtml.Node) {
				if n.Type != xhtml.ElementNode {
					return
				}
				for _, attr := range []string{"aria-controls", "aria-labelledby"} {
					if target := chat5Attr(n, attr); target != "" && ids[target] == nil {
						t.Errorf("%s/%v: %s=%q points at nothing", viewer, open, attr, target)
					}
				}
				// Every button is named, by its text or its label.
				if n.Data == "button" && strings.TrimSpace(chat5NodeText(n)) == "" && chat5Attr(n, "aria-label") == "" && chat5Attr(n, "title") == "" {
					t.Errorf("%s/%v: a button has no name", viewer, open)
				}
			})
			// One page-level heading, sections are h3, subheadings h4, in that order.
			if strings.Join(headings, " ")[:5] != "h2 h3" {
				t.Errorf("%s/%v: headings begin %v", viewer, open, headings[:2])
			}
			firstH4 := -1
			for i, h := range headings {
				if h == "h4" && firstH4 < 0 {
					firstH4 = i
				}
			}
			if firstH4 < 0 || headings[firstH4-1] == "h2" {
				t.Errorf("%s/%v: a subheading has no section heading before it: %v", viewer, open, headings)
			}
			if viewer == "member" {
				if ids["chat-details-manage"] != nil {
					t.Errorf("a plain member has the Manage channel group")
				}
				continue
			}
			// The group button states and controls its body, and what is inside a
			// closed group is not reachable.
			group := ids["chat-details-group-manage"]
			if group == nil {
				t.Fatalf("%s/%v: no Manage channel body", viewer, open)
			}
			var toggle *xhtml.Node
			walkChat5HTML(root, func(n *xhtml.Node) {
				if n.Type == xhtml.ElementNode && n.Data == "button" && chat5Attr(n, "data-id") == "manage" && chat5Attr(n, "data-action") == "details-group" {
					toggle = n
				}
			})
			if toggle == nil || chat5Attr(toggle, "aria-controls") != "chat-details-group-manage" || chat5Attr(toggle, "aria-expanded") != map[bool]string{true: "true", false: "false"}[open] {
				t.Fatalf("%s/%v: the group button does not state its body: %v", viewer, open, toggle)
			}
			hidden := false
			for _, a := range group.Attr {
				hidden = hidden || a.Key == "hidden"
			}
			if hidden == open {
				t.Errorf("%s: group body hidden=%v with the group open=%v", viewer, hidden, open)
			}
			// The Ask buttons name the agent they ask.
			for _, name := range []string{"Ask Assistant", "Ask Policy Helper"} {
				if !strings.Contains(markup, `aria-label="`+name+`"`) {
					t.Errorf("no button named %q", name)
				}
			}
		}
	}
}
