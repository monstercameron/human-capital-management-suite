package chatui

import (
	"strings"
	"testing"
)

// Issue #3707: the group "Start conversation" button rendered greyed out with
// pointer-events:none once the optional group name was left empty, because the
// dialog-form rule keyed on any #new-chat-name:placeholder-shown. That rule is
// for the required channel name only; a group name is optional (CHATBUG-099).
func TestChatbug3707GroupStartButtonIsNotGreyedByEmptyOptionalName(t *testing.T) {
	const oldRule = `.dialog-form:has(#new-chat-name:placeholder-shown) .dialog-actions .button:not(.secondary){`
	const channelRule = `.dialog-form:has(#new-chat-name[required]:placeholder-shown) .dialog-actions .button:not(.secondary){`
	if strings.Contains(Stylesheet, oldRule) {
		t.Fatal("the grey-out rule still matches every empty name field, group included")
	}
	if !strings.Contains(Stylesheet, channelRule+"background:color-mix(in srgb,var(--ink) 10%,transparent);color:var(--muted);opacity:.55;pointer-events:none}") {
		t.Fatal("the grey-out rule must be limited to the required channel name")
	}

	group := renderNode(t, createDialog(Model{Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}, CloseCreate: func() {}}},
		handlers{local: localUI{createKind: GroupChat, picked: []mentionCandidate{{ID: "w-1", Name: "Linh Tran"}, {ID: "w-2", Name: "Thomas Baker"}}}}))
	if !strings.Contains(group, `id="new-chat-name"`) || strings.Contains(group, `id="new-chat-name" `+"required") || strings.Contains(group, "required") {
		t.Fatalf("a group name must be optional and not marked required:\n%s", group)
	}
	if !strings.Contains(group, `<button class="button" type="submit"`) || strings.Contains(group, `disabled type="submit"`) {
		t.Fatalf("two picked people must enable the group submit button:\n%s", group)
	}

	channel := renderNode(t, createDialog(Model{}, handlers{}))
	if !strings.Contains(channel, "required") {
		t.Fatalf("a channel name stays required so its grey-out rule still applies:\n%s", channel)
	}
}
