package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATSEARCH_002_Workspace: the search the workspace box sends to
// Chat is a small keyword search that is not remembered, and what Chat answers
// becomes rows of the workspace list that say what each is and open it.
func TestTodo_CHATSEARCH_002_Workspace(t *testing.T) {
	model := chatui.Model{Locale: "en-US", Conversations: []chatui.Conversation{{ID: "c-general", Name: "general", Kind: chatui.PublicChannel, Joined: true}}}
	request, err := chatsearchWorkspaceRequest(model, "holiday in:general")
	if err != nil {
		t.Fatal(err)
	}
	if request.Query != "holiday in:c-general" || request.DisplayQuery != "holiday in:general" || request.Mode != "keyword" || !request.Transient || request.Limit <= chatsearchWorkspaceLimit || request.Limit > 50 {
		t.Fatalf("request=%+v", request)
	}
	body, _ := json.Marshal(request)
	if !strings.Contains(string(body), `"Transient":true`) || strings.Contains(string(body), "Actor") {
		t.Fatalf("what is sent: %s", body)
	}
	if _, err = chatsearchWorkspaceRequest(model, `holiday "unfinished`); err == nil {
		t.Fatal("words that cannot be read were sent")
	}

	response := chatsearch.Response{Groups: []chatsearch.Group{
		{Kind: chatsearch.Message, Rows: []chatsearch.Row{
			{Kind: chatsearch.Message, ID: "p1", Text: "The holiday calendar is in the handbook", Target: chatsearch.Target{ConversationID: "c-general", MessageID: "p1", Sequence: 12}},
			{Kind: chatsearch.Message, ID: "p2", Text: "Holiday cover for Friday", Target: chatsearch.Target{ConversationID: "c-general", MessageID: "p2", Sequence: 13}},
		}},
		{Kind: chatsearch.Person, Rows: []chatsearch.Row{{Kind: chatsearch.Person, ID: "t:holly", Text: "Holly Holiday", Target: chatsearch.Target{ConversationID: "c-general", ItemID: "holly"}}}},
		{Kind: chatsearch.Agent, Rows: []chatsearch.Row{{Kind: chatsearch.Agent, ID: "persona.holiday", Text: "Holiday Helper\nAnswers questions about the holiday guide.", Target: chatsearch.Target{ItemID: "persona.holiday"}}}},
		{Kind: chatsearch.AgentTask, Rows: []chatsearch.Row{
			{Kind: chatsearch.AgentTask, ID: "t1", Text: "Summarise the holiday guide", Private: true, Target: chatsearch.Target{ItemID: "t1"}},
			{Kind: chatsearch.AgentTask, ID: "t2", Text: "Holiday rota draft", Private: true, Target: chatsearch.Target{ItemID: "t2"}},
		}},
	}}
	items := chatsearchWorkspaceItems(model, response, "holiday")
	if len(items) != chatsearchWorkspaceLimit {
		t.Fatalf("%d rows, want %d: %+v", len(items), chatsearchWorkspaceLimit, items)
	}
	chat, _ := productui.LookupPage(productui.PageChat)
	agents, _ := productui.LookupPage(productui.PageAgents)
	first, agent, task := items[0], items[2], items[3]
	if first.ID != "chat:message:p1" || first.Kind != "chat" || first.KindLabel != "Message" || first.Label != "The holiday calendar is in the handbook" || first.Description != "#general" || first.Href != "/workspace/app/chat#channel=c-general&at=12&message=p1" || first.Icon != chat.Icon {
		t.Fatalf("message row = %+v", first)
	}
	if agent.KindLabel != "Agent" || agent.Label != "Holiday Helper" || agent.Href != "/workspace/app/agents" || agent.Icon != agents.Icon {
		t.Fatalf("agent row = %+v", agent)
	}
	if task.KindLabel != "Agent task" || task.Href != "/workspace/app/agents?task=t1#agents-task-title" {
		t.Fatalf("task row = %+v", task)
	}
	seen := map[string]bool{}
	for _, item := range items {
		if seen[item.ID] || item.Href == "" || item.Label == "" || item.KindLabel == "" || strings.Contains(item.Label, "Holly") {
			t.Fatalf("row = %+v", item)
		}
		seen[item.ID] = true
	}
	if empty := chatsearchWorkspaceItems(model, chatsearch.Response{}, "holiday"); len(empty) != 0 {
		t.Fatalf("no answer gave rows: %+v", empty)
	}
}

// TestTodo_CHATSEARCH_002_VoiceSeek: only a voice result that names a sentence
// in a message seeks; every other result opens as before.
func TestTodo_CHATSEARCH_002_VoiceSeek(t *testing.T) {
	target := chatsearch.Target{ConversationID: "room", MessageID: "post", ItemID: "voice", Sequence: 9, Sentence: 3}
	if sentence, seek := chatsearchVoiceSentence(chatsearch.Voice, target); !seek || sentence != 3 {
		t.Fatalf("a voice result with a sentence: %d %v", sentence, seek)
	}
	none := target
	none.Sentence = 0
	orphan := target
	orphan.MessageID = ""
	for name, attempt := range map[string]struct {
		kind   chatsearch.Kind
		target chatsearch.Target
	}{"no sentence": {chatsearch.Voice, none}, "a correction": {chatsearch.VoiceCorrection, target}, "a message": {chatsearch.Message, target}, "no message": {chatsearch.Voice, orphan}} {
		if _, seek := chatsearchVoiceSentence(attempt.kind, attempt.target); seek {
			t.Errorf("%s seeks", name)
		}
	}
	// The sentence travels in the result's target, which the page hands back.
	raw, _ := json.Marshal(target)
	if !strings.Contains(string(raw), `"Sentence":3`) {
		t.Fatalf("the target does not carry its sentence: %s", raw)
	}
}
