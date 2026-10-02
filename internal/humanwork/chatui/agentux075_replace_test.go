package chatui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentUX075KeyOf is the key a node carries, whichever way it was keyed.
func agentUX075KeyOf(node ui.Node) string {
	if node == nil {
		return ""
	}
	if node.Key != "" {
		return node.Key
	}
	return fmt.Sprint(node.Props["key"])
}

// The working message and the answer (or the failure) are one element in one
// place: same key, no row inserted or removed around it, in a channel and in the
// person's own conversation with an agent, and after a reload (AGENTUX-075).
func TestTodo_AGENTUX_075_Replace(t *testing.T) {
	now := time.Now()
	for _, direct := range []bool{false, true} {
		name := "channel"
		if direct {
			name = "direct"
		}
		t.Run(name, func(t *testing.T) {
			slot := func(state string) (string, int) {
				model := chat4Fixture("en-US", state, direct)
				model.PersonaActivityReady = true
				rows := personaReplyRowsForPost(model, localUI{}, "question", now)
				if direct && state == "answered" {
					// The answer is the agent's own message; it takes the working row's key.
					if len(rows) != 0 {
						t.Fatalf("the working row stayed beside the answer: %d rows", len(rows))
					}
					key, ok := agentUX075AnswerSlot(model, "answer")
					if !ok {
						t.Fatal("the answer message does not know its slot")
					}
					if got := agentUX075MessageKey(model, "answer"); got != key {
						t.Fatalf("the answer message is keyed %q, its slot is %q", got, key)
					}
					return key, 1
				}
				if len(rows) != 1 {
					t.Fatalf("%s: %d rows under the question, want 1", state, len(rows))
				}
				return agentUX075KeyOf(rows[0]), len(rows)
			}
			working, _ := slot("working2")
			if working == "" {
				t.Fatal("the working message has no key")
			}
			for _, state := range []string{"working15", "answered", "failed"} {
				if got, _ := slot(state); got != working {
					t.Errorf("%s is keyed %q, the working message %q: the page would insert and remove a row", state, got, working)
				}
			}
		})
	}

	// A waiting card the page drew before the server reported ("pending:<question>")
	// and the run's own row are the same slot, so the hand-over moves nothing.
	model := chat4Fixture("en-US", "working2", false)
	before := agentUX075KeyOf(personaReplyRowsForPost(model, localUI{}, "question", now)[0])
	model.PersonaInvocations[0].Projection.InvocationID = "pending:question"
	pending := agentUX075KeyOf(personaReplyRowsForPost(model, localUI{}, "question", now)[0])
	if before != pending {
		t.Errorf("the provisional card is keyed %q and the run's row %q", pending, before)
	}
	// Two agents asked in one message are two slots.
	two := chat4Fixture("en-US", "working2", false)
	second := two.PersonaInvocations[0]
	second.Projection.InvocationID, second.Projection.AgentName = "run-2", "Policy Helper"
	two.PersonaInvocations = append(two.PersonaInvocations, second)
	rows := personaReplyRowsForPost(two, localUI{}, "question", now)
	if len(rows) != 2 || agentUX075KeyOf(rows[0]) == agentUX075KeyOf(rows[1]) {
		t.Errorf("two runs of one question share a slot: %d rows", len(rows))
	}
	// A message that is not an answer keeps its own key.
	if got := agentUX075MessageKey(chat4Fixture("en-US", "answered", true), "question"); got != "message:question" {
		t.Errorf("an ordinary message is keyed %q", got)
	}
}

// AGENTP-020: when long work is handed to the task view, the task card takes the
// working message's place in the thread (same slot) and updates in it, so the
// person does not lose the place.
func TestTodo_AGENTP_020_TaskCardKeepsTheSlot(t *testing.T) {
	now := time.Now()
	model := chat4Fixture("en-US", "working15", false)
	before := personaReplyRowsForPost(model, localUI{}, "question", now)
	if len(before) != 1 {
		t.Fatalf("%d rows while working", len(before))
	}
	model.RenderPersonaTask = func(task PersonaTaskCardProps) ui.Node {
		return html.A(html.Props{Class: "persona-task-open", Href: task.OpenTaskHref, Text: task.Title + " · " + task.State})
	}
	for _, state := range []string{"running", "awaiting_approval"} {
		progress := model.PersonaInvocations[0].Projection.Progress
		model.PersonaInvocations[0].Projection.Task = &PersonaTaskCardProps{ID: "task-1", Title: "Review the holiday guide", State: state, OpenTaskHref: "/workspace/app/agents?task=task-1", AwaitingApproval: state == "awaiting_approval"}
		model.PersonaInvocations[0].Projection.Progress = progress
		rows := personaReplyRowsForPost(model, localUI{}, "question", now)
		if len(rows) != 1 || agentUX075KeyOf(rows[0]) != agentUX075KeyOf(before[0]) {
			t.Fatalf("%s: the task card is not in the working message's slot: %d rows", state, len(rows))
		}
		markup := renderNode(t, rows[0])
		if !strings.Contains(markup, "/workspace/app/agents?task=task-1") || !strings.Contains(markup, "Review the holiday guide · "+state) {
			t.Fatalf("%s: the task card or its link is missing: %s", state, markup)
		}
	}
}
