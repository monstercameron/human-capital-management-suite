package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// The page keeps the message an answer was shared as, learns it again when the
// conversation is opened later, and forgets it when the copy is gone.
func TestTodo_CHATUX_026(t *testing.T) {
	// The server's answer to "Share to channel", as its own type encodes it.
	ok, _ := json.Marshal(personachat.ShareResult{InvocationID: "run", PostID: "copy"})
	if got := agentShareResult(http.StatusOK, ok); got != (chatui.AgentShareState{Status: chatui.AgentShareShared, PostID: "copy"}) {
		t.Fatalf("a shared answer is read as %+v", got)
	}
	if got := agentShareResult(http.StatusConflict, []byte(`{"error":"conflict","state":"audience","detail":"2026 holiday guide"}`)); got != (chatui.AgentShareState{Status: chatui.AgentShareRefused, Reason: "audience", Source: "2026 holiday guide"}) {
		t.Fatalf("a refusal is read as %+v", got)
	}
	// A refusal never carries a message id, whatever the body says.
	if got := agentShareResult(http.StatusForbidden, []byte(`{"post_id":"copy"}`)); got != (chatui.AgentShareState{Status: chatui.AgentShareRefused, Reason: "denied"}) {
		t.Fatalf("a forbidden share is read as %+v", got)
	}
	for _, body := range [][]byte{nil, []byte("not json"), []byte(`{}`)} {
		if got := agentShareResult(http.StatusServiceUnavailable, body); got != (chatui.AgentShareState{Status: chatui.AgentShareFailed}) {
			t.Fatalf("a failed share with body %q is read as %+v", body, got)
		}
	}

	// Opening the conversation: what the server says about each stored answer.
	var none map[string]chatui.AgentShareState
	opened := []personaChatAnswer{{ID: "a1", InvocationID: "run-1", SharedPostID: "copy-1"}, {ID: "a2", InvocationID: "run-2"}, {ID: "a3"}}
	share, changed := chatux026SharedOnOpen(none, opened)
	if !changed || len(share) != 1 || share["run-1"] != (chatui.AgentShareState{Status: chatui.AgentShareShared, PostID: "copy-1"}) {
		t.Fatalf("on opening the page holds %+v", share)
	}
	if again, changed := chatux026SharedOnOpen(share, opened); changed || len(again) != 1 {
		t.Fatalf("the same answer again changed the page: %+v", again)
	}
	// The copy was removed elsewhere: private again. The earlier map is untouched.
	gone, changed := chatux026SharedOnOpen(share, []personaChatAnswer{{ID: "a1", InvocationID: "run-1"}})
	if _, held := gone["run-1"]; !changed || held || share["run-1"].Status != chatui.AgentShareShared {
		t.Fatalf("a removed copy is still held as shared: %+v (earlier map %+v)", gone, share)
	}
	// A request on its way, a refusal and a failure are not overwritten by an
	// answer the server holds as private; a request on its way is never touched.
	for _, status := range []chatui.AgentShareStatus{chatui.AgentShareSharing, chatui.AgentShareRemoving, chatui.AgentShareRefused, chatui.AgentShareFailed} {
		held := map[string]chatui.AgentShareState{"run-2": {Status: status, PostID: "p"}}
		if next, changed := chatux026SharedOnOpen(held, opened[1:2]); changed || next["run-2"].Status != status {
			t.Fatalf("%s was changed by a private answer: %+v", status, next)
		}
	}
	for _, status := range []chatui.AgentShareStatus{chatui.AgentShareSharing, chatui.AgentShareRemoving} {
		held := map[string]chatui.AgentShareState{"run-1": {Status: status, PostID: "copy-1"}}
		if next, changed := chatux026SharedOnOpen(held, opened[:1]); changed || next["run-1"].Status != status {
			t.Fatalf("%s was changed while its request is on its way: %+v", status, next)
		}
	}
	// A failed removal the server still holds as shared stays shared.
	held := map[string]chatui.AgentShareState{"run-1": {Status: chatui.AgentShareShared, PostID: "copy-1", RemoveFailed: true}}
	if next, changed := chatux026SharedOnOpen(held, opened[:1]); changed || !next["run-1"].RemoveFailed {
		t.Fatalf("a shared answer was rewritten for nothing: %+v", next)
	}

	// The removal deletes the copy at the revision the page holds, or the first.
	model := chatui.Model{ThreadMessages: []chatui.Message{{ID: "copy", Revision: 3}}, Messages: []chatui.Message{{ID: "question", Revision: 1}}}
	if chatux026CopyRevision(model, "copy") != 3 || chatux026CopyRevision(model, "question") != 1 || chatux026CopyRevision(model, "not-held") != 1 {
		t.Fatal("the copy's revision is not the one the page holds")
	}
}

// The page is wired: the first read of the activity sets the share states, the
// card's Remove is connected, and a removal deletes the copy the state names.
func TestTodo_CHATUX_026_Browser(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(source)
	}
	watch := read("persona_chat_wasm.go")
	answers, shares, activity := strings.Index(watch, "chatbug040ApplyAnswers(model, payload.Answers"), strings.Index(watch, "chatux026SharedOnOpen(model.AgentShare, payload.Answers)"), strings.Index(watch, "model.PersonaInvocations = chatbug079StoredAnswers(")
	if answers < 0 || shares < answers || activity < shares {
		t.Fatal("the watch does not set the share states with the stored answers, before the activity")
	}
	if !strings.Contains(read("chat_wasm.go"), "RemoveSharedAgentAnswer: func(invocationID string) {") {
		t.Fatal("Remove shared answer is not connected")
	}
	share := read("agentux070_share_wasm.go")
	for _, want := range []string{"agentShareResult(response.StatusCode, body)", "chatui.AgentShareRemoving", "client.DeletePost(", "PostId: shared.PostID", "RemoveFailed: true", "setAgentShare(invocation, chatui.AgentShareState{})"} {
		if !strings.Contains(share, want) {
			t.Fatalf("the share glue is missing %q", want)
		}
	}
}
