package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

const (
	chatbug021Token = "eyJsYWJlbCI6IiNnZW5lcmFsIiwidGV4dCI6IkBQb2xpY3kgSGVscGVyIGhvdyBtYW55IFBUTyBob3VycyBjYXJyeSBvdmVyPyIsImF0IjoiMjAyNi0xMC0wMVQxNToxODowOC45Mzc5MTZaIn0"
	chatbug021Share = "/chat/share/aXJvbnJpZGdlLWRlbW8AYThhMGQ4NmYtZTkzYy01MmMzLWE2ZWYtNzQyZTVjM2EyOWU1"
	chatbug021Pol   = "/workspace/app/docs?document=doc-64271829&version=docv-10264426#carryover"
	chatbug021Guide = "/workspace/app/docs?document=doc-10c773e5&version=docv-8d270c40"
)

// The two shapes of the review answer. Stored is what the chat database holds
// (titles only, "(version 1)", the model's own Sources list, the context token
// and the share address). Projected is what the server delivers to Walt Brennan
// after it decided each source for him.
var (
	chatbug021Tail      = "\n\n[chat-agent-question-context:" + chatbug021Token + "](" + chatbug021Share + "#hcm-question=" + chatbug021Token + ")"
	chatbug021Sentence  = "Paid time off policy — Employees may carry over up to 40 hours of unused PTO into the next calendar year (Paid time off policy, version 1, Carryover)."
	chatbug021Stored    = chatbug021Sentence + "\n\nSources\n- Paid time off policy (version 1)\n- 2026 holiday guide" + chatbug021Tail
	chatbug021Projected = chatbug021Sentence + "\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](" + chatbug021Pol + ") <!--chat.agent.source.readable:true-->\n- [2026 holiday guide · v1.0.0](" + chatbug021Guide + ") <!--chat.agent.source.readable:true-->" + chatbug021Tail
)

func chatbug021Printed(t *testing.T, where, text string) {
	t.Helper()
	for _, internal := range []string{"chat-agent-question", "eyJsYWJl", "/chat/share/", "hcm-question", "hcm_agent_announcement", "readable:", "<!--"} {
		if strings.Contains(text, internal) {
			t.Errorf("%s printed internal text %q: %s", where, internal, text)
		}
	}
}

// TestTodo_CHATBUG_021 pins the display-time cleaning: an answer shows its
// statement once and its sources once, and no context token, share address,
// markup tag or data field is printed in a message or a result.
func TestTodo_CHATBUG_021(t *testing.T) {
	for name, body := range map[string]string{"stored": chatbug021Stored, "projected": chatbug021Projected} {
		envelope := parseAgentReplyEnvelope(body)
		chatbug021Printed(t, name+" envelope", envelope.Body)
		if strings.Contains(envelope.Body, "Sources") || strings.Contains(envelope.Body, "holiday guide") || len(envelope.Sources) != 2 || envelope.Backlink == "" || envelope.QuestionText == "" {
			t.Errorf("%s: statement, sources and question context were not separated: %+v", name, envelope)
		}
	}

	// The reader's text can be the raw stored body: the cleaned shape is the same.
	raw := agentAnswerPresentBody(chatbug021Stored, parseAgentReplyEnvelope(chatbug021Projected).Sources)
	if strings.Contains(raw, "Sources") || strings.Contains(raw, "holiday guide") || strings.Contains(raw, "version 1") || !strings.HasPrefix(raw, "[Paid time off policy]("+chatbug021Pol+") — Employees") {
		t.Errorf("raw stored text was not cleaned to the one shape: %s", raw)
	}
	chatbug021Printed(t, "raw stored text", raw)
	if again := agentAnswerPresentBody(raw, parseAgentReplyEnvelope(chatbug021Projected).Sources); again != raw {
		t.Errorf("cleaning is not stable: %q then %q", raw, again)
	}
	// Structured sources absent: the model's own list is the only list, so it stays.
	if kept := agentAnswerPresentBody("Answer\n\nSources\n- Handbook", nil); !strings.Contains(kept, "Handbook") {
		t.Errorf("a Sources list with no structured sources was dropped: %q", kept)
	}

	announcement, err := AnnouncementMessageBody(AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Text: "Upcoming company holidays remaining in 2026"})
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ in, want string }{
		"stored answer":     {chatbug021Stored, strings.Replace(chatbug021Sentence, "version 1", "version 1", 1)},
		"projected answer":  {chatbug021Projected, chatbug021Sentence},
		"token cut off":     {"Carryover is 40 hours.\n\n[chat-agent-question-context:" + chatbug021Token[:30], "Carryover is 40 hours."},
		"share cut off":     {"Carryover is 40 hours. ](" + chatbug021Share[:20], "Carryover is 40 hours."},
		"only the fragment": {"Carryover is 40 hours. #hcm-question=" + chatbug021Token, "Carryover is 40 hours."},
		"open original":     {"Carryover is 40 hours.\n\n[Open the original message](" + chatbug021Share + ")", "Carryover is 40 hours."},
		"announcement v2":   {announcement, "Upcoming company holidays remaining in 2026"},
		"announcement raw":  {"<hcm_agent_announcement>\n{\"AgentName\":\"Agent\",\"OwnerName\":\"Walt Brennan\",\"Text\":\"Upcoming company holidays remaining in 2026:\\n- Labor Day\",\"Scheduled\":false,\"Sources\":[{\"Title\":\"2026 holiday guide\",\"Href\":\"/workspace/app/docs?document=d\"}]}", "Upcoming company holidays remaining in 2026:\n- Labor Day"},
		"plain":             {"Plain text with a [link](https://example.com).", "Plain text with a [link](https://example.com)."},
	} {
		got := chatDisplayText(tc.in)
		chatbug021Printed(t, tc.in[:20]+" result", got)
		if tc.want != "" && !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s: result text %q, want it to start with %q", name, got, tc.want)
		}
		if name != "plain" && strings.Contains(got, "Sources") {
			t.Errorf("%s: a trailing Sources list was printed: %q", name, got)
		}
	}
	if got := chatDisplayText("<hcm_agent_announcement>\n{broken"); got != "" {
		t.Errorf("an undecodable announcement printed %q", got)
	}
}

// TestTodo_CHATBUG_021_Browser renders the three places the owner saw it: the
// answer in the agent conversation when the reader's text is the raw stored
// body, the private answer card, and the search results.
func TestTodo_CHATBUG_021_Browser(t *testing.T) {
	now := time.Now()
	actor := &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}
	post := Message{ID: "answer", Revision: 1, AuthorID: "policy-helper", Author: "Policy Helper", Body: chatbug021Projected, SentAt: now, PersonaActor: actor}
	m := Model{Locale: "en-US", State: StateReady, SelectedID: "policy", CurrentUser: "walt", CurrentTenantID: "tenant",
		Conversations: []Conversation{{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true}},
		Messages:      []Message{post},
		// The reading request answered with the stored body, not the projected one.
		ReaderSelections: map[string]ReaderSelection{"answer": {Revision: 1, Rendering: chatrender.Rendering{Message: "answer", Revision: 1, Text: chatbug021Stored, Tone: chatrender.AsWritten}, Mark: chatrender.Mark{State: "ready"}}}}
	markup := renderNode(t, message(m, handlers{}, post, false))
	chatbug021Printed(t, "agent conversation", chatbug021Visible(markup))
	if strings.Count(markup, "agent-reply-sources") != 1 || strings.Count(markup, "2026 holiday guide") != 1 || strings.Contains(markup, ">Sources</") && strings.Count(markup, ">Sources</") != 1 {
		t.Errorf("the answer printed its sources twice or lost its row: %s", markup)
	}
	if !strings.Contains(markup, "Employees may carry over up to 40 hours") {
		t.Errorf("the statement is gone: %s", markup)
	}

	card := renderNode(t, renderPersonaPrivateAnswer(m, localUI{}, EphemeralMessage{ID: "card", ThreadID: "question", Body: chatbug021Projected, OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), ThreadLink: chatbug021Share + "#hcm-question=" + chatbug021Token}, PersonaProgressProjection{AgentName: "Policy Helper"}))
	chatbug021Printed(t, "private answer card", chatbug021Visible(card))

	view := ChatSearchView{Query: "holiday", Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.AgentAnswer, Count: 2, Rows: []chatsearch.Row{
		{Kind: chatsearch.AgentAnswer, ID: "a", Text: chatbug021Stored},
		{Kind: chatsearch.Message, ID: "b", Text: "<hcm_agent_announcement>\n{\"AgentName\":\"Agent\",\"OwnerName\":\"Walt Brennan\",\"Text\":\"Upcoming company holidays remaining in 2026\",\"Sources\":[]}"}}}}}}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		results := renderNode(t, RenderChatSearch(locale, view))
		chatbug021Printed(t, locale+" search results", chatbug021Visible(results))
		if !strings.Contains(results, "Employees may carry over up to 40 hours") || !strings.Contains(results, "Upcoming company ") {
			t.Errorf("%s: a result lost its text: %s", locale, results)
		}
	}
}

// TestTodo_CHATBUG_020_Browser renders the answer the way the review account
// saw it: the sources the reader may open are links, on the channel card and in
// the agent conversation, and the title inside the answer sentence is a link.
func TestTodo_CHATBUG_020_Browser(t *testing.T) {
	now := time.Now()
	actor := &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}
	post := Message{ID: "answer", Revision: 1, AuthorID: "policy-helper", Author: "Policy Helper", Body: chatbug021Projected, SentAt: now, PersonaActor: actor}
	m := Model{Locale: "en-US", State: StateReady, SelectedID: "policy", CurrentUser: "walt", CurrentTenantID: "tenant",
		Conversations: []Conversation{{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true}}, Messages: []Message{post}}
	card := renderNode(t, renderPersonaPrivateAnswer(m, localUI{}, EphemeralMessage{ID: "card", ThreadID: "question", Body: chatbug021Projected, OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), ThreadLink: chatbug021Share}, PersonaProgressProjection{AgentName: "Policy Helper"}))
	direct := renderNode(t, message(m, handlers{}, post, false))
	for where, markup := range map[string]string{"channel card": card, "agent conversation": direct} {
		if strings.Contains(markup, "You cannot open this document") {
			t.Errorf("%s: a source the reader may open carries the access note: %s", where, markup)
		}
		if strings.Count(markup, `href="`+chatbug021Attr(chatbug021Pol)+`"`) < 2 {
			t.Errorf("%s: the policy is not linked both in the sentence and in Sources: %s", where, markup)
		}
		if strings.Count(markup, `href="`+chatbug021Attr(chatbug021Guide)+`"`) != 1 {
			t.Errorf("%s: the holiday guide is not a link in Sources: %s", where, markup)
		}
	}

	// A reader who truly may not open the second document sees its note, once,
	// and the first document stays a link.
	denied := strings.Replace(chatbug021Projected, "[2026 holiday guide · v1.0.0]("+chatbug021Guide+") <!--chat.agent.source.readable:true-->", "2026 holiday guide <!--chat.agent.source.readable:false-->", 1)
	noted := renderNode(t, renderPersonaPrivateAnswer(m, localUI{}, EphemeralMessage{ID: "card", ThreadID: "question", Body: denied, OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), ThreadLink: chatbug021Share}, PersonaProgressProjection{AgentName: "Policy Helper"}))
	if strings.Count(noted, "You cannot open this document") != 1 || strings.Contains(noted, chatbug021Attr(chatbug021Guide)) || !strings.Contains(noted, `href="`+chatbug021Attr(chatbug021Pol)+`"`) {
		t.Errorf("the access note is not limited to the document the reader may not open: %s", noted)
	}

	// The Sources row is the reference: an agent answer whose sentence links a
	// document in its Sources gets no "Linked document" card for it, while a
	// person's ordinary message with a document link still does.
	withPreviews := m
	withPreviews.EmbedOrigin = "https://hcm.example"
	withPreviews.DocPreviews = map[string]DocPreview{
		"doc-64271829": {ID: "doc-64271829", State: "ready", Readable: true, Title: "Paid time off policy", Owner: "Walt Brennan", Snippet: "Employees may carry over"},
		"doc-10c773e5": {ID: "doc-10c773e5", State: "ready", Readable: true, Title: "2026 holiday guide", Owner: "Walt Brennan", Snippet: "Offices are closed"},
		"doc-other":    {ID: "doc-other", State: "ready", Readable: true, Title: "Benefits handbook", Owner: "Walt Brennan", Snippet: "Open enrollment"},
	}
	label := withPreviews.t(KeyDocEmbedTitle)
	answer := renderNode(t, message(withPreviews, handlers{}, post, false))
	if label == "" || strings.Contains(answer, label) || strings.Contains(answer, "chat-embed-source") {
		t.Errorf("an agent answer got a document preview card for a document in its Sources: %s", answer)
	}
	ordinary := Message{ID: "plain", Revision: 1, AuthorID: "alice", Author: "Alice", Body: "Read https://hcm.example" + chatbug021Guide, SentAt: now}
	if markup := renderNode(t, message(withPreviews, handlers{}, ordinary, false)); strings.Count(markup, label) != 1 || !strings.Contains(markup, "2026 holiday guide") {
		t.Errorf("an ordinary message lost the preview card for its document link: %s", markup)
	}
	// A document the answer links that is not in its Sources keeps its card.
	other := post
	other.Body = strings.Replace(chatbug021Projected, "Employees may carry", "See [Benefits handbook](/workspace/app/docs?document=doc-other). Employees may carry", 1)
	if markup := renderNode(t, message(withPreviews, handlers{}, other, false)); strings.Count(markup, label) != 1 || !strings.Contains(markup, "Benefits handbook") {
		t.Errorf("an agent answer's link to a document outside its Sources lost its card: %s", markup)
	}
}

// chatbug021Visible is what a reader sees of rendered markup: its text, not the
// attribute values of its links.
func chatbug021Visible(markup string) string {
	return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(markup, " ")
}

// chatbug021Attr is an address as rendered markup spells it.
func chatbug021Attr(href string) string { return strings.ReplaceAll(href, "&", "&amp;") }
