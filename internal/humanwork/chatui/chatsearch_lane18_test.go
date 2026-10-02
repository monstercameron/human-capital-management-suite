package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	xhtml "golang.org/x/net/html"
)

func chatsearch003Response() chatsearch.Response {
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	return chatsearch.Response{Mode: "keyword",
		Kinds: []chatsearch.Kind{chatsearch.Message, chatsearch.Agent, chatsearch.AgentTask, chatsearch.AgentAnnouncement},
		Groups: []chatsearch.Group{
			{Kind: chatsearch.Agent, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Agent, ID: "persona.holiday", TenantID: "t", Text: "Holiday Helper\nAnswers questions about the holiday guide.", ByAgent: true, Target: chatsearch.Target{ItemID: "persona.holiday"}}}},
			{Kind: chatsearch.AgentTask, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.AgentTask, ID: "task 1", TenantID: "t", Text: "Summarise the holiday guide", OwnerID: "me", Private: true, At: at, Target: chatsearch.Target{ItemID: "task 1"}}}},
			{Kind: chatsearch.AgentAnnouncement, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.AgentAnnouncement, ID: "announce-1", TenantID: "t", Text: "Post the holiday calendar every Monday", OwnerID: "me", Private: true, At: at, Target: chatsearch.Target{ConversationID: "general", ItemID: "announce-1"}}}},
		}}
}

// TestTodo_CHATSEARCH_003_Browser: agents, a person's own tasks and their own
// announcements are drawn in the Chat results under their own headings, each
// as one link to the page that holds the record, with "Only you" on what only
// its owner finds; and the kind filter offers the three kinds only where the
// service answers them.
func TestTodo_CHATSEARCH_003_Browser(t *testing.T) {
	markup, err := ui.RenderToString(RenderChatSearch("en-US", ChatSearchView{Query: "holiday", Response: chatsearch003Response()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		">Agents</h3>", ">Your agent tasks</h3>", ">Your scheduled announcements</h3>",
		`href="/workspace/app/agents"`, `href="/workspace/app/agents?task=task+1#agents-task-title"`, `href="/workspace/app/agent-operations"`,
		`data-chatsearch-action="visit"`, `data-kind="agent"`, `data-kind="agent_task"`, `data-kind="agent_announcement"`,
		"Answers questions about the ", "Open result: Agent · Holiday Helper",
		`value="agent"`, `value="agent_task"`, `value="agent_announcement"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("the results lack %s", want)
		}
	}
	results := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chatsearch-agent-result") })
	if len(results) != 3 {
		t.Fatalf("%d agent results are drawn, want 3", len(results))
	}
	if strings.Count(markup, "Only you") != 2 {
		t.Errorf("\"Only you\" is on %d results, want the task and the announcement", strings.Count(markup, "Only you"))
	}
	// The searched word is tinted in the agent's name and in its purpose.
	if strings.Count(markup, "<mark") < 4 {
		t.Errorf("the searched word is tinted %d times, want once in each line that holds it", strings.Count(markup, "<mark"))
	}
	// An agent result is not opened as a message: it carries no target.
	for _, absent := range []string{`data-chatsearch-action="open" data-kind="agent`, "persona.holiday"} {
		if strings.Contains(markup, absent) {
			t.Errorf("the results carry %s", absent)
		}
	}
	for locale, heading := range map[string]string{"de-DE": "Deine Agentenaufgaben", "ar": "مهام الوكلاء الخاصة بك"} {
		localized, err := ui.RenderToString(RenderChatSearch(locale, ChatSearchView{Query: "holiday", Response: chatsearch003Response()}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(localized, heading) || strings.Contains(localized, "Your agent tasks") || strings.Contains(localized, "agent_task<") {
			t.Errorf("%s: the task heading is not %q", locale, heading)
		}
	}
	// Where the service does not answer them, the kinds are not offered.
	plain, err := ui.RenderToString(RenderChatSearch("en-US", ChatSearchView{Query: "holiday", Response: chatsearch.Response{Kinds: []chatsearch.Kind{chatsearch.Message}}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, `value="agent"`) || strings.Contains(plain, `value="agent_task"`) {
		t.Error("the kind filter offers agent kinds nothing here can find")
	}
	if all := chatsearchKindChoices(chatsearch.Response{}); len(all) != len(chatsearch.Declarations()) {
		t.Errorf("with no answer %d kinds are offered, want the %d Chat kinds", len(all), len(chatsearch.Declarations()))
	}
	for _, rule := range []string{".chatsearch-agent-link", "min-height:44px"} {
		if !strings.Contains(ChatSearchStyles, rule) {
			t.Errorf("the styles lack %s", rule)
		}
	}
}

// TestTodo_CHATSEARCH_002_Browser_Workspace: what Chat answers the workspace
// search box with becomes rows that say what each is and where, and an address
// that opens the conversation at the message, the thread or the sentence.
func TestTodo_CHATSEARCH_002_Browser_Workspace(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	m := Model{Locale: "en-US", Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}}}
	response := chatsearch003Response()
	response.Groups = append([]chatsearch.Group{
		{Kind: chatsearch.Message, Rows: []chatsearch.Row{{Kind: chatsearch.Message, ID: "p1", Text: "The holiday calendar is in the handbook", AuthorID: "bob", At: at, Target: chatsearch.Target{ConversationID: "general", MessageID: "p1", Sequence: 12}}}},
		{Kind: chatsearch.Thread, Rows: []chatsearch.Row{{Kind: chatsearch.Thread, ID: "p3", Text: "Holiday pay is in section 4", At: at, InThread: true, Target: chatsearch.Target{ConversationID: "unlisted room", MessageID: "p3", Sequence: 15, ThreadID: "p2", ThreadSequence: 14}}}},
		{Kind: chatsearch.Voice, Rows: []chatsearch.Row{{Kind: chatsearch.Voice, ID: "p4:v", Text: "Morning. The holiday rota is late.", HasVoice: true, At: at, Target: chatsearch.Target{ConversationID: "general", MessageID: "p4", ItemID: "v", Sequence: 16, Sentence: 2}}}},
		{Kind: chatsearch.Person, Rows: []chatsearch.Row{{Kind: chatsearch.Person, ID: "t:holly", Text: "Holly Holiday", Target: chatsearch.Target{ConversationID: "general", ItemID: "holly"}}}},
		{Kind: chatsearch.FilterDefinition, Rows: []chatsearch.Row{{Kind: chatsearch.FilterDefinition, ID: "f1", Text: "Holiday words", Target: chatsearch.Target{ItemID: "f1"}}}},
	}, response.Groups...)
	rows := ChatSearchWorkspaceResults(m, response, "holiday", 10)
	want := []ChatSearchWorkspaceResult{
		{ID: "message:p1", Kind: chatsearch.Message, KindLabel: "Message", Text: "The holiday calendar is in the handbook", Where: "#general", Href: "/workspace/app/chat#channel=general&at=12&message=p1"},
		{ID: "thread:p3", Kind: chatsearch.Thread, KindLabel: "Thread reply", Text: "Holiday pay is in section 4", Where: "Thread replies", Href: "/workspace/app/chat#channel=unlisted+room&at=14&message=p2&thread=1"},
		{ID: "voice:p4:v", Kind: chatsearch.Voice, KindLabel: "Voice message", Text: "Morning. The holiday rota is late.", Where: "#general", Href: "/workspace/app/chat#channel=general&at=16&message=p4&sentence=2"},
		{ID: "agent:persona.holiday", Kind: chatsearch.Agent, KindLabel: "Agent", Text: "Holiday Helper", Where: "Agents", Href: "/workspace/app/agents"},
		{ID: "agent_task:task 1", Kind: chatsearch.AgentTask, KindLabel: "Agent task", Text: "Summarise the holiday guide", Where: "Your agent tasks", Href: "/workspace/app/agents?task=task+1#agents-task-title"},
		{ID: "agent_announcement:announce-1", Kind: chatsearch.AgentAnnouncement, KindLabel: "Scheduled announcement", Text: "Post the holiday calendar every Monday", Where: "#general", Href: "/workspace/app/agent-operations"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows=%+v", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
	// The limit is a limit, and an identifier is never shown as a place.
	if short := ChatSearchWorkspaceResults(m, response, "holiday", 2); len(short) != 2 {
		t.Fatalf("limit 2 gave %d rows", len(short))
	}
	for _, row := range rows {
		if strings.Contains(row.Where, "unlisted room") || strings.Contains(row.Text, "persona.") {
			t.Errorf("an identifier is shown: %+v", row)
		}
	}

	// The address is read back as it was written.
	for _, row := range rows[:3] {
		_, fragment, _ := strings.Cut(row.Href, "#")
		address, ok := ParseChatSearchAddress("#" + fragment)
		if !ok {
			t.Fatalf("%s names no message", row.Href)
		}
		switch row.Kind {
		case chatsearch.Message:
			if address != (ChatSearchAddress{MessageID: "p1", Sequence: 12}) {
				t.Errorf("message address = %+v", address)
			}
		case chatsearch.Thread:
			if address != (ChatSearchAddress{MessageID: "p2", Sequence: 14, Thread: true}) {
				t.Errorf("thread address = %+v", address)
			}
		case chatsearch.Voice:
			if address != (ChatSearchAddress{MessageID: "p4", Sequence: 16, Sentence: 2}) {
				t.Errorf("voice address = %+v", address)
			}
		}
	}
	for _, hash := range []string{"", "#channel=general", "#channel=general&tab=docs", "#channel=general&message=p1", "#channel=general&message=p1&at=0", "#channel=general&message=p1&at=x", "#channel=general&at=4", "#channel=general&message=p1&at=4&sentence=0", "#search=general&message=p1&at=4", "#channel=general&message=" + strings.Repeat("p", 300) + "&at=4"} {
		if address, ok := ParseChatSearchAddress(hash); ok {
			t.Errorf("%q was read as a message address: %+v", hash, address)
		}
	}
	// A conversation result opens the conversation, and one that names
	// nothing an address can open has none.
	if href := ChatSearchRowHref(chatsearch.Row{Kind: chatsearch.Conversation, ID: "general", Target: chatsearch.Target{ConversationID: "general"}}); href != "/workspace/app/chat#channel=general" {
		t.Errorf("conversation href = %q", href)
	}
	if href := ChatSearchRowHref(chatsearch.Row{Kind: chatsearch.FilterDefinition, ID: "f1", Target: chatsearch.Target{ItemID: "f1"}}); href != "" {
		t.Errorf("a workspace filter has the address %q", href)
	}
}
