package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The first event of a conversation's agent activity holds the stored private
// answers. The page takes them in the same step as the activity, so a finished
// question has its answer the first time its card is drawn; the answer the
// event stream delivers afterwards is the same one, not a second.
func TestTodo_CHATBUG_040_Browser(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	// What the server sends, encoded by its own types.
	wire, err := json.Marshal(personachat.Progress{
		Invocations: []personachat.Invocation{{InvocationID: "run", PostID: "question", ThreadID: "question", ConversationID: "general", InvokerID: "alice", AgentName: "Policy Helper", Status: "COMPLETED", PrivateConversationID: "policy", PrivatePostID: "copy"}},
		Answers: []personachat.PrivateAnswer{
			{ID: "answer", ThreadID: "question", Body: "Carry over up to 40 hours.", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour), ThreadLink: "/chat/share/signed"},
			{ID: "expired", ThreadID: "older", Body: "Gone.", CreatedAt: now.Add(-30 * time.Hour), ExpiresAt: now.Add(-6 * time.Hour)},
			{ID: "", ThreadID: "question", Body: "No id.", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
			{ID: "no-thread", Body: "No question.", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
			{ID: "empty", ThreadID: "question", Body: "  ", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var event personaChatActivity
	if err = json.Unmarshal(wire, &event); err != nil || len(event.Invocations) != 1 || len(event.Answers) != 5 {
		t.Fatalf("the page does not read the server's event: %+v %v", event, err)
	}

	model := chatui.Model{SelectedID: "general", CurrentUser: "alice"}
	before := model.EphemeralMessages
	if !chatbug040ApplyAnswers(&model, event.Answers, now) {
		t.Fatal("the stored answers changed nothing")
	}
	model.PersonaInvocations = chatbug079StoredAnswers(nil, personaChatInvocations(event.Invocations, journeyclient.Config{Tenant: "tenant", Subject: "alice"}, "general"), now)
	if len(model.EphemeralMessages) != 1 || len(before) != 0 {
		t.Fatalf("answers on the page = %+v, want the one that is whole and not expired", model.EphemeralMessages)
	}
	answer := model.EphemeralMessages[0]
	if answer.ID != "answer" || answer.ThreadID != "question" || answer.Body != "Carry over up to 40 hours." || !answer.OnlyVisibleToYou || answer.ThreadLink != "/chat/share/signed" || !answer.ExpiresAt.Equal(now.Add(23*time.Hour)) {
		t.Fatalf("answer = %+v", answer)
	}
	// The run is finished and its answer is held under the same question: the
	// card has what it needs on its first draw.
	if len(model.PersonaInvocations) != 1 || !model.PersonaInvocations[0].Projection.AnswerStored || model.PersonaInvocations[0].PostID != answer.ThreadID {
		t.Fatalf("activity = %+v", model.PersonaInvocations)
	}
	if visible := chatui.VisibleEphemeralMessages(model.EphemeralMessages, now); len(visible) != 1 {
		t.Fatalf("the answer is not drawn: %+v", visible)
	}

	// The same answer again (the next connection of the watch, or the stream):
	// nothing changes and nothing is added.
	held := model.EphemeralMessages
	if chatbug040ApplyAnswers(&model, event.Answers, now) || len(model.EphemeralMessages) != 1 || &held[0] != &model.EphemeralMessages[0] {
		t.Fatal("a repeated answer changed the page")
	}
	// A later event without answers leaves the ones on the page alone.
	if chatbug040ApplyAnswers(&model, nil, now) || len(model.EphemeralMessages) != 1 {
		t.Fatal("an event without answers removed the answers on the page")
	}
	// A changed body for a held id replaces it in a new slice.
	changed := []personaChatAnswer{{ID: "answer", ThreadID: "question", Body: "Carry over up to 40 hours [source].", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour)}}
	if !chatbug040ApplyAnswers(&model, changed, now) || len(model.EphemeralMessages) != 1 || model.EphemeralMessages[0].Body != changed[0].Body || held[0].Body != "Carry over up to 40 hours." {
		t.Fatalf("a changed answer: %+v (the slice a render held: %+v)", model.EphemeralMessages, held)
	}

	// The watch applies the answers before the activity, inside one change of the model.
	source, err := os.ReadFile("persona_chat_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	answers, activity := strings.Index(text, "chatbug040ApplyAnswers(model, payload.Answers"), strings.Index(text, "model.PersonaInvocations = chatbug079StoredAnswers(")
	if answers < 0 || activity < 0 || answers > activity || !strings.Contains(text, "var payload personaChatActivity") {
		t.Fatal("the watch does not put the stored answers on the page with the activity")
	}
}
