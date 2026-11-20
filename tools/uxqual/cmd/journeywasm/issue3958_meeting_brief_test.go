package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func issue3958Page() chat.SavedPage {
	post := func(id, body string) *chat.Post {
		return &chat.Post{ID: id, TenantID: "t", ConversationID: "c1", AuthorID: "u2", AuthorHomeTenantID: "t", Body: body, CreatedAt: time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)}
	}
	return chat.SavedPage{Items: []chat.SavedItem{
		{TenantID: "t", HomeTenantID: "t", PersonID: "me", ConversationID: "c1", PostID: "p1", Note: "ask HR", Availability: "readable", Post: post("p1", "Who owns the laptop order?")},
		{TenantID: "t", HomeTenantID: "t", PersonID: "me", ConversationID: "c1", PostID: "p2", Availability: "readable", Post: post("p2", "Other")},
		{TenantID: "t", HomeTenantID: "t", PersonID: "me", ConversationID: "c9", PostID: "p3", Availability: "no_access"},
	}}
}

func TestIssue3958_ToggleSelectionNeverMutatesTheOldMap(t *testing.T) {
	first := chatsaveToggleSelection(nil, "k1")
	second := chatsaveToggleSelection(first, "k2")
	third := chatsaveToggleSelection(second, "k1")
	if !reflect.DeepEqual(first, map[string]bool{"k1": true}) || !reflect.DeepEqual(second, map[string]bool{"k1": true, "k2": true}) || !reflect.DeepEqual(third, map[string]bool{"k2": true}) {
		t.Fatalf("first %v second %v third %v", first, second, third)
	}
}

func TestIssue3958_BriefRowsAreReadFreshAndMissingOnesAreNamed(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "t", Subject: "me", Locale: "en-US"}
	selected := map[string]bool{
		chatui.SavedItemKey("t", "c1", "p1"):  true,
		chatui.SavedItemKey("t", "c9", "p3"):  true,
		chatui.SavedItemKey("t", "c1", "zzz"): true, // taken off Saved since
		chatui.SavedItemKey("t", "c1", "p2"):  false,
	}
	rows := chatsaveBriefRows(issue3958Page(), selected, cfg, chatui.Model{})
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].PostID != "p1" || rows[0].Availability != "readable" || rows[0].Body != "Who owns the laptop order?" || rows[0].Note != "ask HR" {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[1].PostID != "p3" || rows[1].Availability != "no_access" || rows[1].Body != "" {
		t.Fatalf("an unreadable message must carry no text: %+v", rows[1])
	}
	if rows[2].PostID != "zzz" || rows[2].Availability != "missing" || rows[2].ConversationID != "c1" || rows[2].TenantID != "t" {
		t.Fatalf("missing row = %+v", rows[2])
	}
	sources, used := chatsaveBriefSources(rows)
	if used != 1 || len(sources) != 3 || sources[1].Reason != "no_access" || sources[2].Reason != "missing" {
		t.Fatalf("sources %+v used %d", sources, used)
	}
}

func TestIssue3958_BriefViewKeepsTheSelectionAndOffersOnlyJoinedPeopleConversations(t *testing.T) {
	rows := chatsaveBriefRows(issue3958Page(), map[string]bool{chatui.SavedItemKey("t", "c1", "p1"): true}, journeyclient.Config{Tenant: "t", Subject: "me"}, chatui.Model{})
	state := &chatsaveBriefState{stage: "setup", err: "unavailable", purpose: "Onboarding"}
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: "c1", Name: "onboarding", Joined: true}, {ID: "c2", Name: "elsewhere"}}}
	view := state.view(rows, model)
	if view.Error != "unavailable" || view.UsedCount != 1 || view.Purpose != "Onboarding" || len(view.Targets) != 1 || view.Targets[0].ID != "c1" {
		t.Fatalf("view = %+v", view)
	}
}

func TestIssue3958_DraftTextKeepsWhatWasAlreadyTyped(t *testing.T) {
	if got := chatsaveBriefDraftText("", "Meeting brief\n"); got != "Meeting brief" {
		t.Fatalf("empty composer: %q", got)
	}
	got := chatsaveBriefDraftText("Hi all,\n", "Meeting brief\nPurpose: x\n")
	if got != "Hi all,\n\nMeeting brief\nPurpose: x" {
		t.Fatalf("existing text must stay first and never be replaced: %q", got)
	}
	if got := chatsaveBriefDraftText("   \n", "Brief"); got != "Brief" {
		t.Fatalf("blank composer: %q", got)
	}
}

func TestIssue3958_TargetNameRefusesAnythingNotOffered_Security(t *testing.T) {
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: "c1", Name: "onboarding", Joined: true}, {ID: "c2", Name: "other", Joined: false}, {ID: "c3", Name: "bot", Joined: true, Agent: true}}}
	if chatsaveBriefTargetName(model, "c1") != "onboarding" {
		t.Fatal("a joined conversation must be a target")
	}
	for _, id := range []string{"c2", "c3", "", "forged"} {
		if chatsaveBriefTargetName(model, id) != "" {
			t.Fatalf("%q must not be a target", id)
		}
	}
}

func TestIssue3958_KeyPartsRoundTrip(t *testing.T) {
	host, conversation, post := chatsaveKeyParts(chatui.SavedItemKey("t", "c1", "p|1"))
	if host != "t" || conversation != "c1" || post != "p|1" {
		t.Fatalf("%q %q %q", host, conversation, post)
	}
	if _, _, post := chatsaveKeyParts("loose"); !strings.Contains(post, "loose") {
		t.Fatalf("a malformed key must not panic")
	}
}
