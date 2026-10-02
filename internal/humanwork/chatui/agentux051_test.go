package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// AGENTUX-051 clause table (the GREEN line, one row each). The server half of
// each row is named in the last column; this file proves what the reader sees.
//
//	a source is a link to the document in the hub, relative with no origin ..... TestTodo_AGENTUX_051
//	the link carries the section's anchor and reads "title · section · version"  TestTodo_AGENTUX_051
//	plain text with "You cannot open this document" otherwise ................... TestTodo_AGENTUX_051_Security
//	the same linked form replaces the "(title, version 1, section)" text ........ TestTodo_AGENTUX_051
//	one sentence and one next step for every failure class ...................... TestTodo_AGENTUX_051_Browser
//	"Ask again" always for the asker; the next step only where asking cannot help TestTodo_AGENTUX_051_Browser
//	owner told once per cause, a transient failure retried once ................. internal/application (AgentUXQuality_OwnerAttention, _Transient_Fault)
//	opening the section, "A newer version exists", compare ...................... Documents hub (internal/humanwork/productui, lane 14)

// agentux051Direct is the Policy Helper conversation with one answer in it.
func agentux051Direct(body string) (Model, Message) {
	actor := &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}
	post := Message{ID: "answer", Revision: 1, AuthorID: "policy-helper", Author: "Policy Helper", Body: body, SentAt: time.Now(), PersonaActor: actor}
	return Model{Locale: "en-US", State: StateReady, SelectedID: "policy", CurrentUser: "walt", CurrentTenantID: "tenant",
		Conversations: []Conversation{{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true}},
		Messages:      []Message{post}}, post
}

func agentux051Anchors(t *testing.T, markup string) []*xhtml.Node {
	t.Helper()
	return chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "a" && chatPolishHasClass(n, "agent-reply-source-link")
	})
}

func TestTodo_AGENTUX_051(t *testing.T) {
	m, post := agentux051Direct(chatbug021Projected)
	// No origin is configured on this page: the link is relative, so it opens on
	// the page's own site.
	if m.EmbedOrigin != "" {
		t.Fatal("the fixture configures an origin")
	}
	markup := renderNode(t, message(m, handlers{}, post, false))
	links := agentux051Anchors(t, markup)
	if len(links) != 2 {
		t.Fatalf("%d source links, want one per cited document: %s", len(links), markup)
	}
	for _, link := range links {
		if href := chatPolishAttr(link, "href"); !strings.HasPrefix(href, "/workspace/app/docs?document=") || strings.Contains(href, "://") {
			t.Fatalf("a source link is not relative to the hub: %q", href)
		}
	}
	// The first source reads title, section and version, and opens that section.
	if got := strings.Join(strings.Fields(chatbug030Text(links[0])), " "); got != "Paid time off policy · Carryover · v1.0.0" {
		t.Fatalf("the first source reads %q", got)
	}
	if href := chatPolishAttr(links[0], "href"); href != chatbug021Pol || !strings.HasSuffix(href, "#carryover") {
		t.Fatalf("the first source opens %q, want the cited version at its section %q", href, chatbug021Pol)
	}
	if got := strings.Join(strings.Fields(chatbug030Text(links[1])), " "); got != "2026 holiday guide · v1.0.0" {
		t.Fatalf("the second source reads %q", got)
	}

	// The plain "(title, version 1, section)" text is replaced by the linked form:
	// the sentence is shown once, with the title as a link, and the old text is gone.
	visible := chatbug021Visible(markup)
	for _, old := range []string{"version 1", "Carryover)", "(Paid time off policy"} {
		if strings.Contains(visible, old) {
			t.Fatalf("the old plain citation %q is still printed: %s", old, visible)
		}
	}
	if strings.Count(markup, `href="`+chatbug021Attr(chatbug021Pol)+`"`) < 2 {
		t.Fatalf("the title inside the answer is not a link: %s", markup)
	}
	// The stored form of the same answer (titles only) reads the same.
	stored := renderNode(t, message(m, handlers{}, Message{ID: "stored", Revision: 1, AuthorID: "policy-helper", Author: "Policy Helper", Body: chatbug021Stored, SentAt: time.Now(), PersonaActor: post.PersonaActor}, false))
	for _, internal := range []string{"chat-agent-question", "/chat/share/", "readable:"} {
		if strings.Contains(chatbug021Visible(stored), internal) {
			t.Fatalf("the stored answer prints %q", internal)
		}
	}
}

// A source the reader may not open is text, with the one sentence that says so,
// and nothing of the document's address reaches the page; an address that is not
// of the Documents hub is never made a link, whatever the flag says.
func TestTodo_AGENTUX_051_Security(t *testing.T) {
	denied := strings.Replace(chatbug021Projected, "[2026 holiday guide · v1.0.0]("+chatbug021Guide+") <!--chat.agent.source.readable:true-->", "2026 holiday guide <!--chat.agent.source.readable:false-->", 1)
	m, post := agentux051Direct(denied)
	post.Body = denied
	markup := renderNode(t, message(m, handlers{}, post, false))
	if len(agentux051Anchors(t, markup)) != 1 || strings.Count(markup, "You cannot open this document") != 1 {
		t.Fatalf("the unreadable source is not one text chip with its note: %s", markup)
	}
	if strings.Contains(markup, "doc-10c773e5") || strings.Contains(markup, "docv-8d270c40") {
		t.Fatalf("the address of a document the reader cannot open reached the page: %s", markup)
	}
	locked := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-reply-source-locked") })
	if len(locked) != 1 || !strings.Contains(chatbug030Text(locked[0]), "2026 holiday guide") {
		t.Fatalf("the unreadable source is not drawn as the locked chip: %d", len(locked))
	}

	for name, href := range map[string]string{
		"another site":        "https://evil.example/workspace/app/docs?document=doc-1",
		"a script":            "javascript:alert(1)",
		"a backslash":         "/workspace/app/docs?document=doc-1&x=\\evil",
		"the wrong page":      "/workspace/app/people?document=doc-1",
		"a protocol-relative": "//evil.example/workspace/app/docs?document=doc-1",
	} {
		body := "Answer.\n\nSources\n- [Handbook](" + href + ") <!--chat.agent.source.readable:true-->"
		model, forged := agentux051Direct(body)
		forged.Body = body
		page := renderNode(t, message(model, handlers{}, forged, false))
		if len(agentux051Anchors(t, page)) != 0 || strings.Contains(page, "evil.example") || strings.Contains(page, "javascript:") {
			t.Fatalf("%s was made a source link: %s", name, page)
		}
	}
}

// Every way an answer can fail is one plain sentence, and what to do next only
// where asking again cannot help; the person who asked can always ask again.
func TestTodo_AGENTUX_051_Browser(t *testing.T) {
	want := map[string][2]string{
		"ADMISSION_REFUSED":           {"Policy Helper cannot answer this request here.", "Ask about a document it can read in this conversation."},
		"NO_RESULTS":                  {"Policy Helper found nothing about this in the documents it can read here.", "Try naming the document."},
		"PERSONA_SUSPENDED":           {"Policy Helper is not available in this conversation right now.", "Ask the person who looks after it to check its setup."},
		"OUTPUT_REJECTED":             {"Policy Helper wrote an answer that did not pass its checks, so it is not shown.", "Ask again, or ask about one thing at a time."},
		chat.AgentAnswerTitleOnlyCode: {"Policy Helper answered with only the name of a document, so the answer is not shown.", "Ask again, or ask what the document says."},
		"MODEL_TIMEOUT":               {"Policy Helper took too long to answer.", ""},
		"LIMIT_REACHED":               {"Policy Helper has reached the limit for this conversation.", "Try again after the limit resets."},
		"ANSWER_INTERRUPTED":          {"Policy Helper's answer was interrupted.", ""},
		"CANCELLED":                   {"Policy Helper's answer was stopped.", "Send a new question when you are ready."},
		"MODEL_UNAVAILABLE":           {"Policy Helper could not answer because the service had a problem.", ""},
	}
	for code, sentence := range want {
		for _, direct := range []bool{false, true} {
			m := chat4Fixture("en-US", "failed", direct)
			m.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: code, Message: "provider said: internal detail 503"}
			card := chatbug054Card(t, m)
			text := chatbug030Text(card)
			if !strings.Contains(text, sentence[0]) {
				t.Fatalf("direct=%v %s: the card reads %q, want %q", direct, code, text, sentence[0])
			}
			next := chatPolishNodesIn(card, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-failure-next") })
			if sentence[1] == "" && len(next) != 0 || sentence[1] != "" && (len(next) != 1 || strings.TrimSpace(chatbug030Text(next[0])) != sentence[1]) {
				t.Fatalf("direct=%v %s: next step %v, want %q", direct, code, len(next), sentence[1])
			}
			retry := chatPolishNodesIn(card, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-agent-action") == "retry" })
			if len(retry) != 1 {
				t.Fatalf("direct=%v %s: the asker cannot ask again", direct, code)
			}
			for _, internal := range []string{code, "internal detail", "503", "invocation", "persona"} {
				if strings.Contains(strings.ToLower(text), strings.ToLower(internal)) {
					t.Fatalf("direct=%v %s: the card shows %q: %s", direct, code, internal, text)
				}
			}
		}
	}
	// A code with no sentence of its own is still one sentence, never the code.
	m := chat4Fixture("en-US", "failed", false)
	m.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: "SOME_NEW_CODE"}
	if text := chatbug030Text(chatbug054Card(t, m)); !strings.Contains(text, "Policy Helper is not available in this conversation right now.") || strings.Contains(text, "SOME_NEW_CODE") {
		t.Fatalf("an unknown code reads %q", text)
	}
}
