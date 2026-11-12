package chatui

import (
	"strings"
	"testing"
)

// Issue #3709: the group message form marked Name as required (*) although
// the field is optional. The asterisk must follow the input's own required
// state, not the class on its wrapper, which the group dialog shares.
func TestIssue3709_GroupNameShowsNoRequiredMark(t *testing.T) {
	const gated = `.create-name:has(input[required])>label::after{content:" *"`
	if !strings.Contains(Stylesheet, gated) {
		t.Fatalf("the required asterisk is not gated on the input's required attribute; want %s", gated)
	}
	if strings.Contains(Stylesheet, `.create-name>label::after`) {
		t.Error("the required asterisk is applied to every create-name label, including the optional group name")
	}

	people := []mentionCandidate{{ID: "ann", Name: "Ann Lee"}, {ID: "bob", Name: "Bob Ray"}}
	m := Model{Locale: "en-US", ShowCreate: true, NewKind: GroupChat, CurrentUser: "walt",
		Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
		Callbacks:     walkCallbacks()}
	dialog := renderNode(t, createDialog(m, handlers{local: localUI{picked: people, createKind: GroupChat}}))
	nameAt := strings.Index(dialog, `id="new-chat-name"`)
	if nameAt < 0 {
		t.Fatal("the group dialog has no name field")
	}
	field := dialog[strings.LastIndex(dialog[:nameAt], "<input"):]
	field = field[:strings.Index(field, ">")+1]
	if strings.Contains(field, "required") {
		t.Errorf("the group name input is marked required: %s", field)
	}
}
