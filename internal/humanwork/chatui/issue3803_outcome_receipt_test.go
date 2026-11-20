package chatui

import (
	"strings"
	"testing"
)

func issue3803Model(locale, viewer string, outcome string) Model {
	m := agentUXReplyModel(locale)
	m.CurrentUser, m.CurrentUserName = viewer, "Viewer Name"
	m.PersonaInvocations = nil
	m.Members = []Member{{ID: "alice", Name: "Alice Adams"}, {ID: "agent:policy", Name: "Policy Helper", Agent: true}}
	m.PersonaOutcomes = []PersonaOutcomeReceipt{{ID: "run-1", PostID: "question", InvokerID: "alice", PersonaID: "agent:policy", AgentName: "Policy Helper", Outcome: outcome}}
	return m
}

func TestIssue3803_OutcomeReceipt_EveryOutcomeIsOneSentenceInThreeLanguages(t *testing.T) {
	want := map[string]map[string]string{
		"en-US": {"succeeded": "Policy Helper finished the request from Alice Adams.", "refused": "Policy Helper could not take the request from Alice Adams.", "failed": "Policy Helper could not finish the request from Alice Adams.", "cancelled": "The request from Alice Adams to Policy Helper was stopped before it finished.", "expired": "Policy Helper ran out of time on the request from Alice Adams."},
		"de-DE": {"succeeded": "Policy Helper hat die Anfrage von Alice Adams abgeschlossen.", "refused": "Policy Helper konnte die Anfrage von Alice Adams nicht annehmen.", "failed": "Policy Helper konnte die Anfrage von Alice Adams nicht abschließen.", "cancelled": "Die Anfrage von Alice Adams an Policy Helper wurde vor dem Abschluss gestoppt.", "expired": "Policy Helper hat für die Anfrage von Alice Adams die Zeit überschritten."},
		"ar":    {"succeeded": "أنهى Policy Helper الطلب المقدَّم من Alice Adams.", "refused": "تعذّر على Policy Helper قبول الطلب المقدَّم من Alice Adams.", "failed": "تعذّر على Policy Helper إكمال الطلب المقدَّم من Alice Adams.", "cancelled": "أُوقف طلب Alice Adams إلى Policy Helper قبل اكتماله.", "expired": "انتهت المهلة قبل أن يُكمل Policy Helper الطلب المقدَّم من Alice Adams."},
	}
	for locale, outcomes := range want {
		for outcome, sentence := range outcomes {
			// A teammate of the asker reads it under the asker's message.
			markup := renderAgentUXReplyRows(t, issue3803Model(locale, "bob", outcome), "question", 320)
			for _, text := range []string{sentence, `role="status"`, "agent-badge", `data-agent-outcome="` + outcome + `"`, `dir="auto"`} {
				if !strings.Contains(markup, text) {
					t.Fatalf("%s %s lacks %q: %s", locale, outcome, text, markup)
				}
			}
			if strings.Count(markup, "persona-outcome-receipt") != 1 || strings.Contains(markup, "⟦") || strings.Contains(markup, "issue3803.") || strings.Contains(markup, "run-1") && !strings.Contains(markup, `data-agent-outcome-id="run-1"`) {
				t.Fatalf("%s %s is not exactly one clean receipt: %s", locale, outcome, markup)
			}
			if strings.Contains(markup, "agent:policy") || strings.Contains(markup, ">alice<") {
				t.Fatalf("%s %s shows an identifier: %s", locale, outcome, markup)
			}
		}
	}
}

func TestIssue3803_OutcomeReceipt_SucceededSaysTheResultIsTheAgentsOwn(t *testing.T) {
	succeeded := renderAgentUXReplyRows(t, issue3803Model("en-US", "bob", "succeeded"), "question", 1440)
	if !strings.Contains(succeeded, "Steps that follow elsewhere may still be open.") {
		t.Fatalf("a success must not claim the work behind it is finished: %s", succeeded)
	}
	failed := renderAgentUXReplyRows(t, issue3803Model("en-US", "bob", "failed"), "question", 1440)
	if strings.Contains(failed, "Steps that follow") || strings.Contains(strings.ToLower(failed), "step") {
		t.Fatalf("a failure invents no step: %s", failed)
	}
}

func TestIssue3803_OutcomeReceipt_AskerKeepsTheirOneCardAndNoSecondRow(t *testing.T) {
	m := issue3803Model("en-US", "alice", "failed")
	m.PersonaInvocations = agentUXReplyModel("en-US").PersonaInvocations
	if markup := renderAgentUXReplyRows(t, m, "question", 1440); strings.Contains(markup, "persona-outcome-receipt") {
		t.Fatalf("the asker's own card is the receipt: %s", markup)
	}
	// The same person, with no card of their own for it on this page, still reads it.
	m.PersonaInvocations = nil
	markup := renderAgentUXReplyRows(t, m, "question", 1440)
	if !strings.Contains(markup, "Policy Helper could not finish the request from Viewer Name.") {
		t.Fatalf("the asker without a card sees their own name: %s", markup)
	}
}

func TestIssue3803_OutcomeReceipt_DrawsNothingItCannotStand_Security(t *testing.T) {
	m := issue3803Model("en-US", "bob", "weird-new-word")
	if markup := renderAgentUXReplyRows(t, m, "question", 1440); strings.Contains(markup, "persona-outcome-receipt") {
		t.Fatalf("an unknown outcome word is not drawn: %s", markup)
	}
	m = issue3803Model("en-US", "bob", "failed")
	if markup := renderAgentUXReplyRows(t, m, "another-post", 1440); strings.Contains(markup, "persona-outcome-receipt") {
		t.Fatalf("a receipt appears only under its own message: %s", markup)
	}
	// An asker the page cannot name is "a teammate", never an identifier.
	m = issue3803Model("en-US", "bob", "failed")
	m.PersonaOutcomes[0].InvokerID = "user-7f3a"
	markup := renderAgentUXReplyRows(t, m, "question", 1440)
	if !strings.Contains(markup, "from a teammate.") || strings.Contains(markup, "user-7f3a") {
		t.Fatalf("unnamed asker: %s", markup)
	}
	// A conversation with an agent has its replies as its own messages.
	m = issue3803Model("en-US", "bob", "failed")
	m.Conversations = []Conversation{{ID: "dm", Agent: true, AgentID: "agent:policy"}}
	m.SelectedID = "dm"
	if markup := renderAgentUXReplyRows(t, m, "question", 1440); strings.Contains(markup, "persona-outcome-receipt") {
		t.Fatalf("an agent conversation draws no receipt: %s", markup)
	}
}
