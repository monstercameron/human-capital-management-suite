package chatui

import (
	"strings"
	"testing"
)

func issue3875Model() Model {
	return Model{Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}}}
}

// Issue 3875: after a disallowed character is removed and the name that is
// left is valid, the box says what was removed politely and is not invalid.
func TestIssue3875_RemovedCharacterLeavesValidNameNotInvalid(t *testing.T) {
	got := renderNode(t, createDialog(issue3875Model(), handlers{local: localUI{nameRefused: true, nameTyped: "hello-worldx"}}))
	if strings.Contains(got, `aria-invalid`) || strings.Contains(got, "field-error") || strings.Contains(got, `role="alert"`) {
		t.Fatalf("a valid corrected name is marked invalid: %s", got)
	}
	for _, want := range []string{`role="status"`, "removed", `aria-describedby="new-chat-name-hint"`, `id="new-chat-name-hint"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %s", want, got)
		}
	}
}

// A name that is still unusable stays invalid, and so does a service refusal.
func TestIssue3875_UnusableNameStaysInvalid(t *testing.T) {
	got := renderNode(t, createDialog(issue3875Model(), handlers{local: localUI{nameRefused: true, nameTyped: ""}}))
	if !strings.Contains(got, `aria-invalid="true"`) || !strings.Contains(got, "field-error") {
		t.Fatalf("an empty name after a refusal is not invalid: %s", got)
	}
	m := issue3875Model()
	m.NewNameRefused = "ab-"
	got = renderNode(t, createDialog(m, handlers{local: localUI{nameTyped: "ab-"}}))
	if !strings.Contains(got, `aria-invalid="true"`) {
		t.Fatalf("the service's refusal is not invalid: %s", got)
	}
}

func TestIssue3875_NoticeLanguages(t *testing.T) {
	seen := map[string]bool{}
	for _, loc := range []string{"en-US", "de-DE", "ar"} {
		m := issue3875Model()
		m.Locale = loc
		got := renderNode(t, createDialog(m, handlers{local: localUI{nameRefused: true, nameTyped: "hello-worldx"}}))
		if strings.Contains(got, "chat.issue3875") || strings.Contains(got, `aria-invalid`) {
			t.Fatalf("%s: %s", loc, got)
		}
		note := laneText(m, chatissue3875Copy, keyChatissue3875Removed)
		if note == "" || seen[note] && loc != "en-US" {
			t.Fatalf("%s: notice missing or untranslated: %q", loc, note)
		}
		seen[note] = true
		if !strings.Contains(got, note) {
			t.Errorf("%s: notice not rendered: %s", loc, got)
		}
	}
}
