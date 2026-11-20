package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// A task result opens the page that exists, in the reader's language, on the
// exact task; the old /workspace/app/agents address is never written.
func TestIssue3758_TaskResultOpensTheAgentsPageOnItsTask(t *testing.T) {
	row := chatsearch.Row{Kind: chatsearch.AgentTask, ID: "task 7/a", Target: chatsearch.Target{ItemID: "task 7/a"}}
	for locale, want := range map[string]string{
		"en-US": "/workspace/app/chat/agents?locale=en-US&task=task+7%2Fa#agents-task-title",
		"de-DE": "/workspace/app/chat/agents?locale=de-DE&task=task+7%2Fa#agents-task-title",
		"ar":    "/workspace/app/chat/agents?locale=ar&task=task+7%2Fa#agents-task-title",
	} {
		if got := ChatSearchAgentHrefFor(locale, row); got != want {
			t.Fatalf("%s: href = %q, want %q", locale, got, want)
		}
		if got := ChatSearchRowHrefFor(locale, row); got != want {
			t.Fatalf("%s: workspace href = %q, want %q", locale, got, want)
		}
	}
	for _, row := range []chatsearch.Row{
		row,
		{Kind: chatsearch.Agent, ID: "a"},
		{Kind: chatsearch.AgentTask, ID: "x"},
		{Kind: chatsearch.AgentAnnouncement, ID: "n"},
	} {
		for _, href := range []string{ChatSearchAgentHref(row), ChatSearchAgentHrefFor("en-US", row)} {
			if strings.HasPrefix(href, "/workspace/app/agents") || strings.Contains(href, "agent-operations") {
				t.Fatalf("%s result leads to an address that is not a page: %q", row.Kind, href)
			}
		}
	}
	if got := ChatSearchAgentHrefFor("en-US", chatsearch.Row{Kind: chatsearch.AgentTask}); got != "/workspace/app/chat/agents?locale=en-US" {
		t.Fatalf("a task with no id = %q", got)
	}
	if got := ChatSearchAgentHrefFor("", chatsearch.Row{Kind: chatsearch.Message}); got != "" {
		t.Fatalf("a message result got an agent address %q", got)
	}
}

// A task result says where the task stands in the reader's language and links to
// the real page.
func TestIssue3758_TaskResultShowsStateAndLinks(t *testing.T) {
	response := chatsearch.Response{Mode: "all", Groups: []chatsearch.Group{{Kind: chatsearch.AgentTask, Count: 1, Rows: []chatsearch.Row{
		{Kind: chatsearch.AgentTask, ID: "t1", Text: "What is the parental leave policy?", Private: true, State: "COMPLETED", Target: chatsearch.Target{ItemID: "t1"}},
	}}}}
	for locale, want := range map[string]string{"en-US": "Completed", "de-DE": "Abgeschlossen", "ar": "مكتملة"} {
		markup, err := ui.RenderToString(RenderChatSearch(locale, ChatSearchView{Query: "parental leave", Response: response}))
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{`data-task-state="completed"`, ">" + want + "<", `href="/workspace/app/chat/agents?locale=` + locale + `&amp;task=t1#agents-task-title"`} {
			if !strings.Contains(markup, text) {
				t.Fatalf("%s: result lacks %q", locale, text)
			}
		}
		if strings.Contains(markup, `href="/workspace/app/agents`) || strings.Contains(markup, "COMPLETED") {
			t.Fatalf("%s: old address or raw state code shown", locale)
		}
	}
	unknown := response
	unknown.Groups = []chatsearch.Group{{Kind: chatsearch.AgentTask, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.AgentTask, ID: "t2", Text: "x question", State: "WEIRD_CODE", Target: chatsearch.Target{ItemID: "t2"}}}}}
	markup, err := ui.RenderToString(RenderChatSearch("en-US", ChatSearchView{Query: "question", Response: unknown}))
	if err != nil || strings.Contains(markup, "WEIRD_CODE") || strings.Contains(markup, "weird_code") {
		t.Fatalf("an unknown state code was printed: %v", err)
	}
}
