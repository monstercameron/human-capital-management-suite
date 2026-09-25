package chatui

import (
	"strings"
	"testing"
)

func TestOneMemberDirectMessageUsesSingularCount(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "self", Conversations: []Conversation{{ID: "self", Name: "Rafael Torres", Kind: DirectMessage, MemberCount: 1}}}
	markup := render(t, m)
	if !strings.Contains(markup, "Direct message</span>") || !strings.Contains(markup, " · </span>1 member<") || strings.Contains(markup, "1 members") {
		t.Fatalf("self DM count was not singular: %s", markup)
	}
	m.Conversations[0].MemberCount = 2
	if markup = render(t, m); !strings.Contains(markup, " · </span>2 members<") {
		t.Fatalf("two-person DM lost plural count: %s", markup)
	}
}
