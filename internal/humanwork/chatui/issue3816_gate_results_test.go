package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func issue3816View(rows ...chatsearch.Row) ChatSearchView {
	groups := []chatsearch.Group{}
	for _, row := range rows {
		groups = append(groups, chatsearch.Group{Kind: row.Kind, Count: 1, Rows: []chatsearch.Row{row}})
	}
	return ChatSearchView{Query: "why", Response: chatsearch.Response{Mode: "keyword", CountScope: "page", Groups: groups}}
}

func TestIssue3816_GateRowsReadInEveryLocaleAndOpenTheGateForm(t *testing.T) {
	question := chatsearch.Row{Kind: chatsearch.GateQuestion, ID: "room-id-7:why", TenantID: "t", Text: "Why do you want to join?", Target: chatsearch.Target{ConversationID: "room-id-7", ItemID: "why"}}
	answer := chatsearch.Row{Kind: chatsearch.GateAnswer, ID: "room-id-7:answer", TenantID: "t", Text: "Why do you want to join?: To help", Private: true, OwnerID: "me", AuthorID: "me", Target: chatsearch.Target{ConversationID: "room-id-7", ItemID: "why"}}
	for _, tc := range []struct{ locale, question, answer, only string }{
		{"en-US", "Gate question", "Your gate answer", "Only you"},
		{"de-DE", "Zugangsfrage", "Deine Zugangsantwort", ""},
		{"ar", "سؤال الوصول", "إجابتك للوصول", ""},
	} {
		markup := renderNode(t, RenderChatSearch(tc.locale, issue3816View(question, answer)))
		for _, want := range []string{tc.question, tc.answer, `data-kind="gate_question"`, `data-kind="gate_answer"`, `data-chatsearch-action="open"`, "chatsearch-private"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s: missing %q in %s", tc.locale, want, markup)
			}
		}
		if tc.only != "" && !strings.Contains(markup, tc.only) {
			t.Fatalf("%s: the answer is not marked private (%q): %s", tc.locale, tc.only, markup)
		}
		if strings.Contains(markup, "room-id-7") {
			// The target travels encoded in the control, never as words.
			for _, line := range strings.Split(markup, ">") {
				if strings.Contains(line, "room-id-7") && !strings.Contains(line, "data-") {
					t.Fatalf("%s: internal id shown as text: %s", tc.locale, line)
				}
			}
		}
	}
}
