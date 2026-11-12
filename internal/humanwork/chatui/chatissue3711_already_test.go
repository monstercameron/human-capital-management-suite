package chatui

import (
	"strings"
	"testing"
)

// TestIssue3711_PickedPersonSaysAlreadyAdded: a name the create dialog already
// holds as a chip is left out of the options, so typing it again must say the
// person is already added, not that no one matches.
func TestIssue3711_PickedPersonSaysAlreadyAdded(t *testing.T) {
	m := chatux002Model("en-US")
	m.ShowAddMembers = false
	h := handlers{local: localUI{
		pickQuery: "Darius",
		picked:    []mentionCandidate{{ID: "darius-bennett", Name: "Darius Bennett"}},
	}}
	markup := renderNode(t, memberPicker(m, h, GroupChat))
	if strings.Contains(markup, "No one matches") {
		t.Fatalf("an already-added person is reported as no match: %s", markup)
	}
	if !strings.Contains(markup, "“Darius” is already added or is a member of this conversation.") {
		t.Fatalf("no already-added message for the picked person: %s", markup)
	}
}

// TestIssue3711_MemberSaysAlreadyInConversation: when the details panel adds
// people to a conversation, a name that is already a member gets the same answer.
func TestIssue3711_MemberSaysAlreadyInConversation(t *testing.T) {
	m := chatux002Model("en-US")
	m.ShowAddMembers = true
	m.Members = []Member{{ID: "darius-bennett", Name: "Darius Bennett"}}
	h := handlers{local: localUI{pickQuery: "darius"}}
	markup := renderNode(t, memberPicker(m, h, PublicChannel))
	if strings.Contains(markup, "No one matches") {
		t.Fatalf("an existing member is reported as no match: %s", markup)
	}
	if !strings.Contains(markup, "“darius” is already added or is a member of this conversation.") {
		t.Fatalf("no already-in-conversation message for the member: %s", markup)
	}
}

// TestIssue3711_UnknownNameStillNoMatch: a name nobody holds keeps the
// no-match message, so the new answer is only for people who are already in.
func TestIssue3711_UnknownNameStillNoMatch(t *testing.T) {
	m := chatux002Model("en-US")
	m.ShowAddMembers = true
	m.Members = []Member{{ID: "darius-bennett", Name: "Darius Bennett"}}
	h := handlers{local: localUI{pickQuery: "zzqx"}}
	markup := renderNode(t, memberPicker(m, h, PublicChannel))
	if !strings.Contains(markup, "No one matches “zzqx”.") || strings.Contains(markup, "already added") {
		t.Fatalf("an unknown name lost its no-match message: %s", markup)
	}
}

// TestIssue3711_LocalizedAlreadyText: the already-added answer is written in
// de-DE and ar as well as en-US, with the typed query kept verbatim.
func TestIssue3711_LocalizedAlreadyText(t *testing.T) {
	cases := map[string]string{
		"en-US": "“Darius” is already added or is a member of this conversation.",
		"de-DE": "„Darius“ ist bereits hinzugefügt oder Mitglied dieser Unterhaltung.",
		"ar":    "«Darius» مضاف بالفعل أو عضو في هذه المحادثة.",
	}
	for locale, want := range cases {
		if got := chatissue3711AlreadyText(locale, "Darius"); got != want {
			t.Fatalf("%s: got %q want %q", locale, got, want)
		}
	}
}
