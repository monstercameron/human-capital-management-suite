package chatui

import (
	"strings"
	"testing"
)

func issue3865People() []mentionCandidate {
	return []mentionCandidate{{ID: "w-1", Name: "Darius Bennett"}, {ID: "w-2", Name: "Evelyn Morgan"}}
}

func issue3865Local(kind ConversationKind) localStore {
	return localStore{box: &localUI{createKind: kind}}
}

// Switching Group -> Direct -> Group keeps the second recipient, while the
// direct message itself never holds more than one.
func TestIssue3865_RecipientsSurviveDirectRoundTrip(t *testing.T) {
	local := issue3865Local(GroupChat)
	local.box.picked = issue3865People()
	m := Model{Locale: "en-US"}

	chatissue3865Select(m, local, DirectMessage)
	if got := pickedIDs(local.get().picked); got != "w-1" {
		t.Fatalf("direct message recipients = %q, want only the first", got)
	}
	chatissue3865Select(m, local, GroupChat)
	if got := pickedIDs(local.get().picked); got != "w-1,w-2" {
		t.Fatalf("group recipients after returning = %q, want both", got)
	}
}

// A person picked while in Direct message joins the restored group rather
// than replacing it, and nobody is listed twice.
func TestIssue3865_RestoredGroupMergesDirectPick(t *testing.T) {
	local := issue3865Local(GroupChat)
	local.box.picked = issue3865People()
	m := Model{Locale: "en-US"}
	chatissue3865Select(m, local, DirectMessage)
	local.box.picked = []mentionCandidate{{ID: "w-3", Name: "Camila Ruiz"}}
	chatissue3865Select(m, local, GroupChat)
	if got := pickedIDs(local.get().picked); got != "w-3,w-1,w-2" {
		t.Fatalf("merged group recipients = %q", got)
	}
}

// The group name returns after a trip through a public channel, and the
// private group name is never carried into the channel's own name box.
func TestIssue3865_GroupNameSurvivesChannelRoundTrip(t *testing.T) {
	local := issue3865Local(GroupChat)
	local.box.picked = issue3865People()
	m := Model{Locale: "en-US", Callbacks: walkCallbacks()}
	createNameInput(m, local, "planning draft")

	chatissue3865Select(m, local, PublicChannel)
	channel := renderNode(t, createDialog(m, handlers{local: local.get()}))
	if strings.Contains(channel, "planning draft") || strings.Contains(channel, "planning-draft") {
		t.Fatal("the private group name leaked into the public channel box")
	}
	chatissue3865Select(m, local, GroupChat)
	group := renderNode(t, createDialog(m, handlers{local: local.get()}))
	if !strings.Contains(group, `data-chat-value="planning draft"`) {
		t.Fatalf("group name not restored:\n%s", group)
	}
	if got := pickedIDs(local.get().picked); got != "w-1,w-2" {
		t.Fatalf("group recipients = %q", got)
	}
}

// Opening the dialog afresh starts with no leftover group draft.
func TestIssue3865_ResetClearsGroupDraft(t *testing.T) {
	local := issue3865Local(GroupChat)
	local.box.picked = issue3865People()
	createNameInput(Model{}, local, "keep me")
	chatissue3865Select(Model{}, local, DirectMessage)
	local.resetCreate()
	if st := local.get(); st.groupName != "" || st.groupPicked != nil {
		t.Fatalf("draft survived a fresh opening: %+v", st)
	}
}
