package chatui

import (
	"strings"
	"testing"
)

func TestChannelWidgetsPinnedCardsAndAuthorizedRoster(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", ShowDetails: true, Conversations: []Conversation{{ID: "room", Kind: PublicChannel}}, Members: []Member{{ID: "ari", HomeTenantID: "home", Name: "Ari Chen"}}, ChannelTeam: ChannelTeamWidget{Revision: 2, Pinned: true, CanPin: true, Purpose: "Launch readiness", Members: []ChannelTeamMember{{HomeTenantID: "home", SubjectID: "ari", RoleLabel: "Coordinator"}, {HomeTenantID: "home", SubjectID: "revoked", RoleLabel: "Old label"}}}, ChannelProject: ChannelProjectWidget{Revision: 3, Pinned: true, CanPin: false, Title: "Launch", Milestones: []ChannelProjectMilestone{{ID: "ms-1", Text: "Review", Status: "BLOCKED", OwnerHomeTenantID: "home", OwnerSubjectID: "ari", DueDate: "2026-10-01"}}}}
	markup := render(t, m)
	for _, want := range []string{"Launch readiness", "Ari Chen", "Coordinator", "Review", "Blocked", "2026-10-01", `data-action="team-pin"`, `data-action="project-pin"`, `data-chat-select-value="BLOCKED"`, `data-chat-select-value="` + widgetOwnerToken("home", "ari") + `"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(markup, "Old label") || strings.Contains(markup, "revoked") {
		t.Fatal("stale member leaked into widget")
	}
	if strings.Contains(markup, `data-action="project-pin" data-id="" aria-label="Pin widget"`) {
		t.Fatal("project pin should not be enabled")
	}
	m.Conversations[0].Kind = GroupChat
	markup = render(t, m)
	if strings.Contains(markup, "channel-widget-inline-card") || strings.Contains(markup, "channel-widget-form") {
		t.Fatal("non-channel exposed widgets")
	}
}

func TestChannelWidgetOwnerIdentityRequiresCurrentTenantQualifiedMember(t *testing.T) {
	m := Model{Members: []Member{{ID: "same", HomeTenantID: "host", Name: "Host"}, {ID: "same", HomeTenantID: "guest", Name: "Guest"}}}
	if got := widgetOwnerValue(m, "guest", "same"); got != widgetOwnerToken("guest", "same") {
		t.Fatalf("guest picker value=%q", got)
	}
	home, subject := widgetOwnerIdentity(m, widgetOwnerToken("guest", "same"))
	if home != "guest" || subject != "same" {
		t.Fatalf("owner=%q/%q", home, subject)
	}
	if got := widgetOwnerValue(m, "revoked", "same"); got != "__none__" {
		t.Fatalf("stale owner=%q", got)
	}
	m.Members[0], m.Members[1] = m.Members[1], m.Members[0]
	home, subject = widgetOwnerIdentity(m, widgetOwnerToken("guest", "same"))
	if home != "guest" || subject != "same" {
		t.Fatalf("reordered owner=%q/%q", home, subject)
	}
}
