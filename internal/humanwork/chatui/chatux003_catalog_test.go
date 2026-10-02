package chatui_test

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	xhtml "golang.org/x/net/html"
)

// chatux003CatalogPage renders the whole chat page with the product's own copy
// catalog, the one that answers a key it does not hold with the key in brackets,
// for one state of an agent's answer.
func chatux003CatalogPage(t *testing.T, locale, state string) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, MemberCount: 18, Joined: true}
	ref := chatui.ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "t", Display: "Policy Helper", ConversationID: room.ID}
	sent := time.Now().Add(-time.Minute)
	body := "Carry over up to 40 hours.\n\nSources\n" +
		"- [Paid time off policy · v1.0.0](/workspace/app/docs?document=pto&version=v1) <!--chat.agent.source.readable:true-->\n" +
		"- Executive succession plan <!--chat.agent.source.readable:false-->\n<!--chat.agent.private:audience-->"
	actor := chatui.PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", InvokerHandle: "alice", Trusted: true}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: "alice", CurrentUserName: "Alice Smith", CurrentTenantID: "t",
		Text:                    func(key string) string { return ctx.Text(key) },
		Conversations:           []chatui.Conversation{room},
		Members:                 []chatui.Member{{ID: "alice", HomeTenantID: "t", Name: "Alice Smith"}},
		Messages:                []chatui.Message{{ID: "question", AuthorID: "alice", Author: "Alice Smith", Body: "@Policy Helper how many PTO hours carry over?", TimeLabel: "9:30", SentAt: sent, PersonaReferences: []chatui.ChatReference{ref}, Replies: 1}},
		ResolvedPersonaMentions: []chatui.ResolvedPersonaMention{{Reference: ref, Handle: "policy-helper", Purpose: "Answer policy questions", Owner: "People Operations"}},
		PersonaLookup:           chatui.PersonaLookupReady, PersonaLookupConversationID: room.ID,
		PersonaPostActors: map[string]chatui.PersonaPostActor{"answer": {Display: "Policy Helper", Actor: actor}},
		Callbacks: chatui.Callbacks{SendMessageWithReferences: func(string, string, []chatui.ChatReference) {}, SendMessage: func(string, string) {}, SelectConversation: func(string) {},
			OpenThread: func(string) {}, CloseThread: func() {}, ToggleDetails: func(bool) {}, SubmitAgentFeedback: func(string, bool) {}, UndoAgentFeedback: func(string) {}, OpenMenu: func(string) {}, ShareAgentAnswer: func(string) {}},
	}
	projection := chatui.PersonaProgressProjection{InvocationID: "run", ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", PrivateReplyHref: chatui.ChannelReferenceURL("policy"), DurablePostID: "answer"}
	switch state {
	case "private", "private menu", "sharing", "shared", "refused", "failed share":
		m.EphemeralMessages = []chatui.EphemeralMessage{{ID: "answer", ThreadID: "question", Body: body, OnlyVisibleToYou: true, CreatedAt: sent, ExpiresAt: sent.Add(time.Hour)}}
		if state == "private menu" {
			m.MenuID = "agent-card:answer"
		}
		if status, ok := map[string]chatui.AgentShareStatus{"sharing": chatui.AgentShareSharing, "shared": chatui.AgentShareShared, "refused": chatui.AgentShareRefused, "failed share": chatui.AgentShareFailed}[state]; ok {
			m.AgentShare = map[string]chatui.AgentShareState{"run": {Status: status, Reason: "audience"}}
		}
	case "private strict agent":
		m.EphemeralMessages = []chatui.EphemeralMessage{{ID: "answer", ThreadID: "question", Body: strings.ReplaceAll(body, "audience", "agent"), OnlyVisibleToYou: true, CreatedAt: sent, ExpiresAt: sent.Add(time.Hour)}}
	case "private asked":
		m.EphemeralMessages = []chatui.EphemeralMessage{{ID: "answer", ThreadID: "question", Body: strings.ReplaceAll(body, "audience", "asked"), OnlyVisibleToYou: true, CreatedAt: sent, ExpiresAt: sent.Add(time.Hour)}}
	case "public":
		answer := chatui.Message{ID: "answer", AuthorID: "policy-helper", Author: "Policy Helper", Body: strings.Split(body, "\n<!--")[0], SentAt: sent, PersonaActor: &actor}
		m.ShowThread, m.ThreadParentID, m.ThreadMessages = true, "question", []chatui.Message{m.Messages[0], answer}
	case "pending":
		projection.Progress = &chatui.PersonaProgressProps{InvocationID: "run", InvokerID: "alice", AgentName: "Policy Helper", Visible: true, ElapsedSeconds: 15}
	case "failed":
		projection.Failure = &chatui.PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: "MODEL_UNAVAILABLE", Retryable: true}
	}
	m.PersonaInvocations = []chatui.PersonaThreadInvocation{{PostID: "question", Projection: projection}}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// chatux003CopyKeys lists every place in the agent's cards and messages where a
// catalog marker or a bare copy key is visible: in text, and in the title,
// aria-label and placeholder of any element.
func chatux003CopyKeys(t *testing.T, page string) []string {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(html.UnescapeString(page)))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	bare := func(value string) bool {
		value = strings.TrimSpace(value)
		return strings.ContainsAny(value, "⟦⟧") || (strings.HasPrefix(value, "chatux003.") || strings.HasPrefix(value, "chat.agent.")) && !strings.ContainsAny(value, " \t")
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		switch n.Type {
		case xhtml.TextNode:
			if bare(n.Data) {
				found = append(found, "text: "+strings.TrimSpace(n.Data))
			}
		case xhtml.ElementNode:
			for _, attr := range n.Attr {
				switch attr.Key {
				case "title", "aria-label", "placeholder", "alt", "aria-description", "aria-roledescription":
					if bare(attr.Val) {
						found = append(found, n.Data+"["+attr.Key+"]: "+attr.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

// TestTodo_CHATUX_003_Catalog_Browser renders the agent's answer in every state
// the owner can see it in (a private answer with Share to channel and its more
// menu open, one that is being shared, was shared or was refused, a strict agent's
// and an asked-for private answer, a public answer, the working card and the
// failed card) with the product's real catalog in all three languages, and
// finds no catalog marker or copy key in any text, title, aria-label or placeholder.
func TestTodo_CHATUX_003_Catalog_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, state := range []string{"private", "private menu", "sharing", "shared", "refused", "failed share", "private strict agent", "private asked", "public", "pending", "failed"} {
			page := chatux003CatalogPage(t, locale, state)
			if found := chatux003CopyKeys(t, page); len(found) != 0 {
				t.Errorf("%s / %s: the page prints copy keys: %q", locale, state, found)
			}
			// The state is on the page: the card, the message or the working row.
			want := map[string]string{"private": "agent-reply-share", "private menu": "agent-reply-open", "sharing": "agent-reply-share", "shared": "agent-reply-share-note", "refused": "agent-reply-share-note", "failed share": "agent-reply-share-note",
				"private strict agent": "agent-reply-why", "private asked": "agent-reply-why", "public": "agent-reply-sources", "pending": "agent-progress-cancel", "failed": "persona-progress-failure"}[state]
			if !strings.Contains(page, want) {
				t.Errorf("%s / %s: the page does not show %s", locale, state, want)
			}
		}
	}
}
