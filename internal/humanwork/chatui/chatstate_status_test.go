package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func chatstateRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
func chatstateView() ChannelStatusView {
	return ChannelStatusView{Status: chat.ChannelStatus{TenantID: "tenant", ConversationID: "room", Status: chatpolicy.StatusLocked, Revision: 3, ChangedBy: "worker-opaque", Reason: "Incident review"}, ActorName: "Walt Brennan", Transitions: []chat.StatusTransition{{Status: chatpolicy.StatusOpen, Permission: chatpolicy.PermissionChangeChannelStatus}}}
}

func TestTodo_CHATSTATE_002(t *testing.T) {
	view := chatstateView()
	unnamed := view
	unnamed.ActorName = ""
	if name := chatstateActorName(Model{Members: []Member{{ID: view.Status.ChangedBy, HomeTenantID: view.Status.TenantID, Name: "Walt Brennan"}}}, unnamed); name != "Walt Brennan" {
		t.Fatalf("member attribution = %q", name)
	}
	markup := chatstateRender(t, ChannelStatusPanel(ChannelStatusPanelProps{Model: Model{Locale: "en-US"}, View: view, Change: func(chat.ChangeChannelStatusRequest) {}}))
	// The Status row names the state and holds the change form; who changed it,
	// when and why is About's line (CHATBUG-025), so it reaches people who may
	// not change the status too.
	for _, want := range []string{"Locked", "Change status", "Choose a status", "Confirm status change", "chatstate-reason"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q: %s", want, markup)
		}
	}
	if !strings.Contains(markup, "disabled") || strings.Contains(markup, "chatstate-until") || strings.Contains(markup, "Members with posting permission") {
		t.Fatalf("with nothing chosen Confirm waits and no lock end or effect is shown: %s", markup)
	}
	about := chatstateRender(t, chatux005About(Model{Locale: "en-US", SelectedID: "room", ChannelStatuses: map[string]ChannelStatusView{"room": view}}, handlers{}, Conversation{ID: "room"}, nil))
	for _, want := range []string{"Locked", "Walt Brennan", "Incident review"} {
		if !strings.Contains(about, want) {
			t.Fatalf("About misses %q: %s", want, about)
		}
	}
	if strings.Contains(markup, "worker-opaque") || strings.Contains(markup, ">ACTIVE<") || strings.Contains(about, "worker-opaque") {
		t.Fatal("internal identity rendered")
	}
	request, ok := ChannelStatusFormRequest(view, "ACTIVE", "  Resolved  ", "", time.Now())
	if !ok || request.Reason != "Resolved" || request.ExpectedRevision != 3 || request.ConversationID != "room" {
		t.Fatalf("form = %+v %v", request, ok)
	}
	if _, ok = ChannelStatusFormRequest(view, "ARCHIVED", "reason", "", time.Now()); ok {
		t.Fatal("unlisted transition")
	}
	if _, ok = ChannelStatusFormRequest(view, "ACTIVE", "reason", "2026-10-01T10:00", time.Now()); ok {
		t.Fatal("end time on open")
	}
	view.Transitions = nil
	markup = chatstateRender(t, ChannelStatusPanel(ChannelStatusPanelProps{Model: Model{}, View: view, Change: func(chat.ChangeChannelStatusRequest) {}}))
	if strings.Contains(markup, "Change status") || strings.Contains(markup, "chatstate-reason") {
		t.Fatal("unauthorized controls rendered")
	}
	archived := view
	archived.Status.Status = chatpolicy.StatusArchived
	if len(ArchivedChannelStatuses([]ChannelStatusView{view, archived}, true)) != 1 || len(ArchivedChannelStatuses([]ChannelStatusView{view, archived}, false)) != 1 {
		t.Fatal("archive grouping")
	}
}

func TestTodo_CHATSTATE_002_Accessibility(t *testing.T) {
	view := chatstateView()
	view.Updated = true
	markup := chatstateRender(t, ChannelStatusPanel(ChannelStatusPanelProps{Model: Model{}, View: view, Change: func(chat.ChangeChannelStatusRequest) {}}))
	for _, want := range []string{`aria-live="polite"`, `aria-hidden="true"`, `for="chatstate-reason"`, `aria-describedby="chatstate-form-error"`, `for="chatstate-choice"`, `chatstate-effect`, `Channel status changed: Locked`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing accessible %q", want)
		}
	}
	if !strings.Contains(ChannelStatusStyles, "min-block-size:44px") || !strings.Contains(ChannelStatusStyles, ":focus-visible") || !strings.Contains(ChannelStatusStyles, "prefers-reduced-motion") || strings.Contains(ChannelStatusStyles, "#") {
		t.Fatal("styles violate accessibility or token rules")
	}
}

func TestTodo_CHATSTATE_002_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		view := chatstateView()
		for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusOpen, chatpolicy.StatusAnnouncements, chatpolicy.StatusLocked, chatpolicy.StatusArchived} {
			view.Status.Status = status
			markup := chatstateRender(t, ChannelStatusBadge(m, view))
			if !strings.Contains(markup, chatstateText(m, chatstateKey(status))) {
				t.Fatal("localized badge missing")
			}
			if status != chatpolicy.StatusOpen {
				notice := chatstateRender(t, ChannelStatusComposerNotice(m, view))
				if notice == "" || strings.Contains(notice, "textarea") {
					t.Fatal("composer explanation missing")
				}
			}
		}
		view.Status.Status = chatpolicy.StatusArchived
		view.Transitions = []chat.StatusTransition{{Status: chatpolicy.StatusOpen}}
		markup := chatstateRender(t, ChannelStatusPanel(ChannelStatusPanelProps{Model: m, View: view, Change: func(chat.ChangeChannelStatusRequest) {}}))
		if !strings.Contains(markup, chatstateText(m, "restore")) {
			t.Fatal("restore missing")
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("RTL missing")
		}
		view.Status.Name = "Incident channel"
		markup = chatstateRender(t, ChannelStatusDirectory(ChannelStatusDirectoryProps{Model: m, Views: []ChannelStatusView{view}, Archived: true, Open: func(string) {}, Restore: func(ChannelStatusView) {}}))
		if !strings.Contains(markup, "Incident channel") || !strings.Contains(markup, chatstateText(m, "restore")) {
			t.Fatal("archive directory missing channel or restore")
		}
		markup = chatstateRender(t, ChannelStatusSystemLine(m, view))
		if !strings.Contains(markup, "Walt Brennan") || !strings.Contains(markup, "Incident review") || !strings.Contains(markup, chatstateText(m, "archived")) {
			t.Fatal("localized system line missing")
		}
		view.Unavailable = true
		markup = chatstateRender(t, ChannelStatusPanel(ChannelStatusPanelProps{Model: m, View: view, Retry: func() {}}))
		if !strings.Contains(markup, chatstateText(m, "retry")) || !strings.Contains(markup, chatstateText(m, "error")) {
			t.Fatal("error recovery missing")
		}
	}
}

// A channel's status shows wherever the channel appears: its header, its row in
// the sidebar, the channel list (Browse channels) and the search results, in
// words and an icon.
func TestTodo_CHATSTATE_002_ShownWhereverTheChannelAppears(t *testing.T) {
	room := Conversation{ID: "announce", Name: "announcements", Kind: PublicChannel, Joined: true, MemberCount: 40}
	locked := ChannelStatusView{Status: chat.ChannelStatus{TenantID: "t", ConversationID: "announce", Status: chatpolicy.StatusLocked, Revision: 2}}
	m := Model{Locale: "en-US", State: StateReady, SelectedID: "announce", Conversations: []Conversation{room}, Browse: nil,
		SearchChannels:  []Conversation{room},
		ChannelStatuses: map[string]ChannelStatusView{"announce": locked},
		Callbacks:       Callbacks{SelectConversation: func(string) {}, JoinConversation: func(string) {}, CloseBrowse: func() {}, Search: func(string) {}},
	}
	surfaces := map[string]string{
		"header":  renderNode(t, timeline(m, handlers{})),
		"sidebar": renderNode(t, railRow(m, room)),
		"browse":  renderNode(t, browseDialog(m, handlers{})),
	}
	m.Search = "announce"
	surfaces["search results"] = renderNode(t, searchResultsPanel(m))
	for name, markup := range surfaces {
		if !strings.Contains(markup, "chatstate-badge") || !strings.Contains(markup, ">Locked<") || !strings.Contains(markup, `aria-hidden="true">🔒<`) {
			t.Errorf("%s does not show the status in words and an icon: %s", name, markup)
		}
	}
	// An Open channel is the ordinary case and shows nothing anywhere.
	open := m
	open.ChannelStatuses = map[string]ChannelStatusView{"announce": {Status: chat.ChannelStatus{TenantID: "t", ConversationID: "announce", Status: chatpolicy.StatusOpen, Revision: 1}}}
	if strings.Contains(renderNode(t, railRow(open, room)), "chatstate-badge") || strings.Contains(renderNode(t, browseDialog(open, handlers{})), "chatstate-badge") {
		t.Error("an Open channel shows a status badge")
	}
}
