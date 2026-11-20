package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func TestIssue3997_ReplyAndChannelCopyBoundToSubmitConversation(t *testing.T) {
	type call struct {
		conversation, parent, body string
		also                       bool
	}
	var calls []call
	m := Model{SelectedID: "room-a", ThreadParentID: "post-1"}
	m.Callbacks.ReplyInThreadIn = func(conversationID, parentID, body string, also bool) {
		calls = append(calls, call{conversationID, parentID, body, also})
	}
	m.Callbacks.ReplyInThread = func(string, string) { t.Fatal("legacy reply used although ReplyInThreadIn is set") }
	m.Callbacks.SendMessage = func(string, string) { t.Fatal("separate channel send used although ReplyInThreadIn is set") }

	if !dispatchThreadReply(m, "hello", nil, true) {
		t.Fatal("reply was not dispatched")
	}
	// The reader switches rooms right after pressing Send; the dispatched call
	// already carries the room and parent from submit time.
	m.SelectedID = "room-b"
	if len(calls) != 1 || calls[0] != (call{"room-a", "post-1", "hello", true}) {
		t.Fatalf("calls = %+v, want one call bound to room-a/post-1 with the channel copy", calls)
	}
}

func TestIssue3997_LegacyCallbacksStillBindSubmitConversation(t *testing.T) {
	var sentTo, repliedParent string
	m := Model{SelectedID: "room-a", ThreadParentID: "post-1"}
	m.Callbacks.ReplyInThread = func(parent, _ string) { repliedParent = parent }
	m.Callbacks.SendMessage = func(conversationID, _ string) { sentTo = conversationID }
	dispatchThreadReply(m, "hi", nil, true)
	if repliedParent != "post-1" || sentTo != "room-a" {
		t.Fatalf("reply parent %q, channel copy to %q", repliedParent, sentTo)
	}
	sentTo = ""
	dispatchThreadReply(m, "hi", nil, false)
	if sentTo != "" {
		t.Fatalf("channel copy sent without the choice: %q", sentTo)
	}
}

func TestIssue3997_AgentRequestIsNeverCopiedToChannel(t *testing.T) {
	var refsCalls int
	m := Model{SelectedID: "room-a", ThreadParentID: "post-1"}
	m.Callbacks.ReplyInThreadWithReferences = func(string, string, []ChatReference) { refsCalls++ }
	m.Callbacks.ReplyInThreadIn = func(string, string, string, bool) { t.Fatal("agent request took the plain path") }
	m.Callbacks.SendMessage = func(string, string) { t.Fatal("agent request copied to the channel") }
	if !dispatchThreadReply(m, "@bot do it", []ChatReference{{}}, true) || refsCalls != 1 {
		t.Fatalf("references path calls = %d", refsCalls)
	}
}

func TestIssue3997_NoCallbackDispatchesNothing(t *testing.T) {
	if dispatchThreadReply(Model{SelectedID: "r", ThreadParentID: "p"}, "x", nil, true) {
		t.Fatal("dispatched with no callbacks")
	}
}

func TestIssue3997_AgentReplyChoiceIsOffAndSaysWhy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		plain := renderNode(t, html.Div(html.Props{}, chatux014ThreadTools(m, handlers{}, false)...))
		if strings.Contains(plain, "thread-also-agent") || strings.Contains(plain, issue3997AgentNote(locale)) {
			t.Fatalf("%s: an ordinary reply got the agent note", locale)
		}
		h := handlers{threadRefs: []ChatReference{{}}}
		got := renderNode(t, html.Div(html.Props{}, chatux014ThreadTools(m, h, false)...))
		note := issue3997AgentNote(locale)
		if note == "" || !strings.Contains(got, note) || !strings.Contains(got, "thread-also-agent") || !strings.Contains(got, "disabled") {
			t.Fatalf("%s: the choice for an agent request is not off with its reason: %s", locale, got)
		}
	}
}

// The two thread notices (#3997, #3965) read in every locale and are not the
// English line reused.
func TestIssue3997_ThreadNoticesAreLocalized(t *testing.T) {
	for name, notice := range map[string]func(string) string{"copy failed": ThreadChannelCopyFailedNotice, "thread unavailable": ThreadUnavailableNotice} {
		en, de, ar := notice("en-US"), notice("de-DE"), notice("ar")
		if en == "" || de == "" || ar == "" || en == de || en == ar || de == ar {
			t.Fatalf("%s: en %q de %q ar %q are not three distinct lines", name, en, de, ar)
		}
	}
	if !strings.Contains(ThreadChannelCopyFailedNotice("en-US"), "was posted") {
		t.Fatal("the copy-failed notice does not say the reply was posted")
	}
}
