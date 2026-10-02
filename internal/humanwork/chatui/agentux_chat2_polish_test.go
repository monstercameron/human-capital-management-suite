package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func TestAgentUXChat2_CanonicalMentionChips(t *testing.T) {
	persona := ResolvedPersonaMention{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy", Display: "Policy Helper", ConversationID: "general"}, Purpose: "Answer policy questions"}
	model := Model{Locale: "en-US", ResolvedPersonaMentions: []ResolvedPersonaMention{persona}}
	plain := renderNode(t, html.Span(html.Props{}, mentionReferenceBody(model, "Policy Helper and @Policy Helper")...))
	if strings.Contains(plain, "mention-chip") {
		t.Fatalf("plain display-name text gained identity: %s", plain)
	}
	model.renderReferences = []ChatReference{persona.Reference}
	chip := renderNode(t, html.Span(html.Props{}, mentionReferenceBody(model, "Ask @Policy Helper now")...))
	for _, want := range []string{`class="mention-chip mention-chip-agent"`, "@Policy Helper", "mention-chip-agent-badge", `data-action="agent-profile-open"`} {
		if !strings.Contains(chip, want) {
			t.Fatalf("agent mention chip missing %q: %s", want, chip)
		}
	}
	css := AgentUXChat2Styles
	ruleStart := strings.Index(css, ".mention-chip{")
	if ruleStart < 0 {
		t.Fatal("message mention chip rule is missing")
	}
	ruleEnd := strings.Index(css[ruleStart:], "}")
	if ruleEnd < 0 {
		t.Fatal("message mention chip rule is unterminated")
	}
	rule := css[ruleStart : ruleStart+ruleEnd]
	for _, forbidden := range []string{"border-radius:50%", "display:block", "width:140px", "height:140px", "width:44px", "height:44px"} {
		if strings.Contains(rule, forbidden) {
			t.Errorf("message mention chip rule inherited fixed avatar geometry %q: %s", forbidden, rule)
		}
	}
	for _, want := range []string{"display:inline", "padding:2px 4px", "border-radius:4px", "font-size:inherit", "line-height:inherit", "width:auto", "height:auto", "text-decoration:none"} {
		if !strings.Contains(rule, want) {
			t.Errorf("message mention chip rule missing %q: %s", want, rule)
		}
	}
}

func TestAgentUXChat2_QuestionCardAndDocumentSources(t *testing.T) {
	body := "Answer\n\nSources\n- [Paid time off policy](/workspace/app/docs?document=pto)\n\n[chat-agent-question:#general](/chat/share/signed)"
	envelope := parseAgentReplyEnvelope(body)
	if envelope.Body != "Answer" || envelope.QuestionLabel != "#general" || len(envelope.Sources) != 1 {
		t.Fatalf("parsed reply envelope = %+v", envelope)
	}
	for _, locale := range []struct{ code, asked, view, sources string }{{"en-US", "You asked in", "View in #general", "Sources"}, {"de-DE", "Sie fragten in", "In #general ansehen", "Quellen"}, {"ar", "طرحت سؤالًا في", "عرض في #general", "المصادر"}} {
		model := Model{Locale: locale.code}
		markup := renderNode(t, html.Div(html.Props{}, renderAgentQuestionContext(model, envelope, ""), html.Div(html.Props{}, renderAgentReplySources(model, envelope)...)))
		if !strings.Contains(markup, locale.asked) || !strings.Contains(markup, locale.view) || !strings.Contains(markup, locale.sources) || !strings.Contains(markup, "Paid time off policy") || !strings.Contains(markup, "icon-document") {
			t.Fatalf("%s reply sources/card = %s", locale.code, markup)
		}
	}
}

func TestAgentUXChat2_AgentIdentityAndGrouping(t *testing.T) {
	conversation := Conversation{ID: "agent-dm", Name: "Policy Helper", AgentID: "policy", Agent: true, Kind: DirectMessage}
	model := Model{SelectedID: conversation.ID, Conversations: []Conversation{conversation}}
	message := personaTrustedMessage(model, Message{ID: "answer", AuthorID: "policy", Author: "internal", SentAt: time.Now()})
	if message.Author != "Policy Helper" || !messageIsAgent(model, message) {
		t.Fatalf("first-paint agent attribution = %+v", message)
	}
	if got := personaMessageBadge(model, message.PersonaActor); !strings.Contains(renderNode(t, got), ">Agent<") {
		t.Fatal("agent direct message omitted compact Agent badge")
	}
}

func TestAgentUXChat2_FeedbackAndResponsiveContract(t *testing.T) {
	model := Model{Locale: "en-US", PersonaInvocations: []PersonaThreadInvocation{{Projection: PersonaProgressProjection{InvocationID: "run-1", DurablePostID: "answer"}}}, Callbacks: Callbacks{SubmitAgentFeedback: func(string, bool) {}}}
	markup := renderNode(t, renderAgentFeedback(model, localUI{}, "answer"))
	for _, want := range []string{"Helpful", "Not right", `data-id="run-1"`, `data-action="agent-feedback"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("feedback controls missing %q: %s", want, markup)
		}
	}
	for _, want := range []string{"max-width:767px", "grid-template-columns:56px", ".message-actions{top:-38px}", "min-height:44px", "text-overflow:ellipsis", ".mention-menu{inset-inline:0", "safe-area-inset-bottom"} {
		if !strings.Contains(AgentUXChat2Styles, want) {
			t.Fatalf("responsive chat contract missing %q", want)
		}
	}
}

func TestAgentUXChat2_DirectFollowUpReferenceAndCopy(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{{"en-US", "Ask Policy Helper a follow-up"}, {"de-DE", "Policy Helper eine Folgefrage stellen"}, {"ar", "اطرح سؤال متابعة على Policy Helper"}} {
		if got := strings.ReplaceAll(agentReplyFallback(tc.locale, "chat.agent.follow_up", ""), "{name}", "Policy Helper"); got != tc.want {
			t.Fatalf("%s follow-up placeholder = %q", tc.locale, got)
		}
	}
	model := Model{SelectedID: "agent-dm", CurrentTenantID: "tenant", Conversations: []Conversation{{ID: "agent-dm", Name: "Policy Helper", Agent: true, AgentID: "policy", Kind: DirectMessage}}, ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy", Display: "Policy Helper", ConversationID: "agent-dm"}}}}
	ref, ok := selectedAgentReference(model)
	if !ok || ref.ID != "policy" || ref.ConversationID != "agent-dm" {
		t.Fatalf("implicit direct-agent reference = %+v, ok=%v", ref, ok)
	}
}
