package chatui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderAgentUXChat3Node(t *testing.T, node ui.Node, width int) string {
	t.Helper()
	markup, err := ui.RenderToString(html.Div(html.Props{Data: map[string]string{"test-viewport": fmt.Sprint(width)}}, node))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestAgentUXChat3_WorkingStatusStagesAndCancel(t *testing.T) {
	base := PersonaProgressProjection{ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper"}
	for _, tc := range []struct {
		seconds int
		want    []string
		absent  []string
	}{
		// AGENTUX-075: the elapsed time and Stop appear after ten seconds, not five.
		{seconds: 9, want: []string{"Finding an answer in your policy documents…", "agent-working-dots"}, absent: []string{"agent-reply-counter", "agent-progress-cancel"}},
		{seconds: 10, want: []string{"Finding an answer in your policy documents…", ">0:10<", "agent-progress-cancel", ">Stop<"}},
		{seconds: 20, want: []string{"Still working…", ">0:20<", "agent-progress-cancel", ">Stop<"}},
	} {
		t.Run(fmt.Sprint(tc.seconds), func(t *testing.T) {
			projection := base
			projection.Progress = &PersonaProgressProps{InvocationID: "run-1", InvokerID: "alice", AgentName: "Policy Helper", Visible: true, ElapsedSeconds: tc.seconds}
			model := Model{Locale: "en-US", Callbacks: Callbacks{CancelPersonaInvocation: func(string) {}}}
			markup := renderAgentUXChat3Node(t, RenderPersonaProgress(model, projection), 390)
			for _, want := range tc.want {
				if !strings.Contains(markup, want) {
					t.Errorf("%ds status missing %q: %s", tc.seconds, want, markup)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(markup, absent) {
					t.Errorf("%ds status unexpectedly contains %q: %s", tc.seconds, absent, markup)
				}
			}
			if strings.Count(markup, `role="status"`) != 1 || strings.Count(markup, `aria-live="polite"`) != 1 {
				t.Fatalf("%ds status is not announced exactly once: %s", tc.seconds, markup)
			}
		})
	}
	for _, locale := range []string{"de-DE", "ar"} {
		projection := base
		projection.Progress = &PersonaProgressProps{InvocationID: "run-1", InvokerID: "alice", AgentName: "Policy Helper", Visible: true, ElapsedSeconds: 20}
		markup := renderAgentUXChat3Node(t, RenderPersonaProgress(Model{Locale: locale, Callbacks: Callbacks{CancelPersonaInvocation: func(string) {}}}, projection), 320)
		rtl := strings.Contains(markup, `dir="rtl"`)
		if strings.Contains(markup, "chat.agent.") || !strings.Contains(markup, ">"+chatNumeral(locale, "0:20")+"<") || rtl != strings.HasPrefix(locale, "ar") {
			t.Fatalf("%s 320px status lost localization/direction: %s", locale, markup)
		}
	}
}

func TestAgentUXChat3_ComposerTokenAndDirectAgentSemantics(t *testing.T) {
	reference := ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy", Display: "Policy Helper", ConversationID: "room"}
	store := mentionStore{box: &mentionBox{conversationID: "room"}}
	store.AddPersonaToken("chat-composer", "room", reference)
	store.ReconcilePersonas("chat-composer", "what is the PTO policy?")
	refs := store.PersonaReferences("chat-composer", "room", "what is the PTO policy?")
	if len(refs) != 1 || refs[0] != reference {
		t.Fatalf("visual composer token lost canonical reference: %+v", refs)
	}
	if got := bodyWithAgentMentions("what is the PTO policy?", refs); got != "@Policy Helper what is the PTO policy?" {
		t.Fatalf("channel payload = %q", got)
	}
	markup := renderAgentUXChat3Node(t, composerAgentToken("Policy Helper"), 390)
	if !strings.Contains(markup, `class="composer-agent-token"`) || !strings.Contains(markup, "@Policy Helper") {
		t.Fatalf("composer token is not visible: %s", markup)
	}
	for _, body := range []string{"", "   ", "@", "  @  "} {
		if composerMessageReady(body) {
			t.Errorf("composer accepted non-message %q", body)
		}
	}
	if !composerMessageReady("@ policy") {
		t.Fatal("composer rejected a substantive message")
	}

	model := agentUXMentionModel(PersonaLookupReady)
	model.Conversations = []Conversation{{ID: "room", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper"}}
	model.Members = []Member{{ID: "ana", Name: "Ana Flores"}}
	options, _ := mentionOptionsForState(model, "", "chat-composer", false)
	if len(options) != 1 || options[0].persona == nil || options[0].person != nil {
		t.Fatalf("direct-agent mention menu offered people: %+v", options)
	}
	model.renderReferences = []ChatReference{reference}
	question := renderAgentUXChat3Node(t, html.Div(html.Props{}, mentionReferenceBody(model, "@Policy Helper and for part-time staff?")...), 390)
	if strings.Contains(question, "mention-chip") || !strings.Contains(question, "and for part-time staff?") || strings.Contains(question, "@Policy Helper") {
		t.Fatalf("direct-agent question retained a redundant mention: %s", question)
	}
}

func TestAgentUXChat3_InlineMentionIsStableAndKeyboardOperable(t *testing.T) {
	model := agentUXMentionModel(PersonaLookupReady)
	model.renderReferences = []ChatReference{model.ResolvedPersonaMentions[0].Reference}
	markup := renderAgentUXChat3Node(t, html.P(html.Props{}, mentionReferenceBody(model, "Ask @Policy Helper about PTO")...), 1440)
	for _, want := range []string{`<span class="mention-chip-details">`, `aria-label="Policy Helper, Agent"`, `data-action="agent-profile-open"`, `class="sr-only mention-chip-agent-badge"`, " about PTO"} {
		if !strings.Contains(markup, want) {
			t.Errorf("inline mention missing %q: %s", want, markup)
		}
	}
	for _, want := range []string{".mention-chip-details{display:inline", ".mention-chip{display:inline", "padding:2px 4px", "border-radius:4px", "text-decoration:none"} {
		if !strings.Contains(AgentUXChat2Styles, want) {
			t.Errorf("inline mention CSS missing %q", want)
		}
	}
	if strings.Contains(AgentUXChat2Styles, ".mention-chip:hover .mention-chip-agent-badge") {
		t.Fatal("mention still changes visible copy on hover")
	}
	if mentionActionForKey("Tab") != mentionKeyDetails || mentionActionForKey("ArrowLeft") != mentionKeyNone || mentionActionForKey("ArrowRight") != mentionKeyNone {
		t.Fatal("mention keyboard contract still steals text-navigation arrows")
	}
	if got := mentionHintText("de-DE", false); got != "↑↓ bewegen · Enter zum Einfügen · Tab für Details · Esc zum Schließen" {
		t.Fatalf("German menu hint = %q", got)
	}
	if got := mentionHintText("ar", true); got != "Esc رجوع" {
		t.Fatalf("Arabic details hint = %q", got)
	}
}

func TestAgentUXChat3_SourcesAndQuestionContext(t *testing.T) {
	body := "Carry over up to five days.\n\nSources\n- [Paid time off policy](/workspace/app/docs?document=pto)\n- Employee handbook\n\n[chat-agent-question:#general](/chat/share/signed)"
	envelope := parseAgentReplyEnvelope(body)
	if envelope.Body != "Carry over up to five days." || len(envelope.Sources) != 2 {
		t.Fatalf("answer envelope left metadata in the paragraph: %+v", envelope)
	}
	markup := renderAgentUXChat3Node(t, html.Div(html.Props{}, append([]ui.Node{renderAgentQuestionContext(Model{Locale: "ar"}, envelope, "5:25 AM")}, renderAgentReplySources(Model{Locale: "ar"}, envelope)...)...), 1440)
	for _, want := range []string{`class="agent-question-context"`, `<bdi dir="ltr">#general</bdi>`, "5:25 AM", `href="/chat/share/signed"`, `href="/workspace/app/docs?document=pto"`, "Employee handbook", "icon-document"} {
		if !strings.Contains(markup, want) {
			t.Errorf("answer provenance missing %q: %s", want, markup)
		}
	}
	if strings.Contains(envelope.Body, "Sources") || strings.Contains(envelope.Body, "chat-agent-question") {
		t.Fatalf("answer paragraph exposed provenance markup: %q", envelope.Body)
	}
}

func TestAgentUXR5Srv_OriginalQuestionContext(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 25, 0, 0, time.UTC)
	contextJSON, _ := json.Marshal(struct {
		Label string    `json:"label"`
		Text  string    `json:"text"`
		At    time.Time `json:"at"`
	}{Label: "#general", Text: "How much leave can I carry over?", At: at})
	body := "Carry over is limited to five days.\n\n[chat-agent-question-context:" + base64.RawURLEncoding.EncodeToString(contextJSON) + "](/chat/share/signed)"
	envelope := parseAgentReplyEnvelope(body)
	if envelope.Body != "Carry over is limited to five days." || envelope.QuestionText != "How much leave can I carry over?" || !envelope.QuestionAt.Equal(at) {
		t.Fatalf("question envelope=%+v", envelope)
	}
	markup := renderAgentUXChat3Node(t, renderAgentQuestionContext(Model{Locale: "en-US"}, envelope, ""), 390)
	for _, want := range []string{"#general", "How much leave can I carry over?", at.Local().Format("3:04 PM"), `href="/chat/share/signed"`, "<blockquote"} {
		if !strings.Contains(markup, want) {
			t.Errorf("question context missing %q: %s", want, markup)
		}
	}
	linked := bindAgentQuestionThreadLink(agentReplyEnvelope{Body: "Carry over is limited to five days."}, "/chat/share/signed#hcm-question="+base64.RawURLEncoding.EncodeToString(contextJSON))
	if linked.QuestionText != envelope.QuestionText || !linked.QuestionAt.Equal(at) || linked.Backlink != "/chat/share/signed" {
		t.Fatalf("thread-link question context=%+v", linked)
	}
}

func TestAgentUXChat3_FeedbackStateFailureAndLegacyReceipt(t *testing.T) {
	model := Model{Locale: "en-US", PersonaInvocations: []PersonaThreadInvocation{{Projection: PersonaProgressProjection{InvocationID: "run-1", AgentName: "Policy Helper", DurablePostID: "answer"}}}, Callbacks: Callbacks{SubmitAgentFeedback: func(string, bool) {}, UndoAgentFeedback: func(string) {}}}
	markup := renderAgentUXChat3Node(t, renderAgentFeedback(model, localUI{agentFeedback: map[string]string{"run-1": "not-right"}}, "answer"), 800)
	for _, want := range []string{"icon-thumb-up", "icon-thumb-down", `aria-pressed="true"`, "Thanks. Policy Helper&#39;s owner has been told.", ">Undo<"} {
		if !strings.Contains(markup, want) {
			t.Errorf("feedback state missing %q: %s", want, markup)
		}
	}

	failure := PersonaProgressProjection{ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", Failure: &PersonaProgressFailure{InvocationID: "run-1", InvokerID: "alice", Code: "MODEL_UNAVAILABLE", Retryable: true}}
	markup = renderAgentUXChat3Node(t, RenderPersonaProgress(Model{Locale: "en-US"}, failure), 390)
	for _, want := range []string{"Policy Helper could not answer because the service had a problem.", "icon-warning", "Only visible to you", ">Ask again<", ">Dismiss<", "persona-progress-failure"} {
		if !strings.Contains(markup, want) {
			t.Errorf("failure card missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Try again") {
		t.Errorf("failure card repeats its action in the sentence: %s", markup)
	}

	for _, tc := range []struct{ locale, want string }{{"en-US", "The answer was sent privately."}, {"de-DE", "Die Antwort wurde privat gesendet."}, {"ar", "تم إرسال الإجابة بشكل خاص."}} {
		node, ok := legacyPrivateAnswerReceipt(Model{Locale: tc.locale}, Message{ID: "old", Author: "Walt Brennan", Body: legacyPrivateAnswerReceiptBody, TimeLabel: "1:44 AM"})
		if !ok {
			t.Fatalf("%s legacy receipt was not recognized", tc.locale)
		}
		legacy := renderAgentUXChat3Node(t, node, 320)
		if !strings.Contains(legacy, tc.want) || strings.Contains(legacy, legacyPrivateAnswerReceiptBody) || strings.Contains(legacy, "Walt Brennan") || strings.Contains(legacy, "avatar") {
			t.Fatalf("%s legacy receipt leaked old authorship/copy: %s", tc.locale, legacy)
		}
	}
}

func TestAgentUXChat3_OnePanePhoneAndStableComposer(t *testing.T) {
	for _, width := range []int{390, 320} {
		selected := Model{State: StateReady, Locale: "en-US", SelectedID: "policy", Conversations: []Conversation{{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy"}}, Callbacks: Callbacks{SendMessage: func(string, string) {}, SelectConversation: func(string) {}, ToggleSidebar: func(bool) {}}}
		markup := renderAgentUXChat3Node(t, Build(selected), width)
		if !strings.Contains(markup, `data-has-selection="true"`) || !strings.Contains(markup, `placeholder="Ask Policy Helper"`) || !strings.Contains(markup, "Private to you") || !strings.Contains(markup, "Only you can see this conversation") || strings.Contains(markup, "Answers from policy documents") {
			t.Fatalf("%dpx selected phone did not default to the conversation: %s", width, markup)
		}
		empty := selected
		empty.SelectedID = ""
		markup = renderAgentUXChat3Node(t, Build(empty), width)
		if !strings.Contains(markup, `data-has-selection="false"`) {
			t.Fatalf("%dpx no-selection phone did not default to the list: %s", width, markup)
		}
	}
	for _, want := range []string{"@media(max-width:767px)", `.chat-workspace[data-has-selection="false"] .chat-rail{display:flex`, ".chat-rail{display:none}", ".chat-side.thread-pane{position:fixed;inset:0;width:100%", ".thread-back-label{display:inline}", ".chat-composer{position:sticky;bottom:0", ".mention-menu{position:absolute;inset-inline:0", "max-width:none", ".composer-toolbar{display:flex", ".jump-newest{inset-inline:auto;left:50%;bottom:8px", ".rail-row-more[aria-expanded=\"true\"]"} {
		if !strings.Contains(AgentUXChat3Styles, want) {
			t.Errorf("phone/composer contract missing %q", want)
		}
	}
	if strings.Contains(AgentUXChat3Styles, "#fff") || strings.Contains(AgentUXChat3Styles, "font-family") {
		t.Fatal("round-three styles bypassed shell theme or typography tokens")
	}
}

func TestAgentUXChat3_DirectAgentHeaderAndRestoredDraft(t *testing.T) {
	// CHATUX-016 and CHATUX-024: the description is a line of its own, the
	// privacy mark sits beside the name, and the box says "Ask <name>" until
	// the agent has answered.
	for _, tc := range []struct {
		locale, subtitle, placeholder string
	}{
		{"en-US", "Answers from policy documents · Only you can see this conversation", "Ask Policy Helper"},
		{"de-DE", "Antworten aus Richtliniendokumenten · Nur Sie können diese Unterhaltung sehen", "Policy Helper fragen"},
		{"ar", "إجابات من مستندات السياسات · يمكنك وحدك رؤية هذه المحادثة", "اسأل Policy Helper"},
	} {
		for _, width := range []int{1440, 800, 390, 320} {
			model := Model{State: StateReady, Locale: tc.locale, Direction: agentReplyDirection(tc.locale), CurrentUser: "alice", SelectedID: "policy", Draft: "@pol", ShowThread: true, ThreadParentID: "question", Conversations: []Conversation{{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy", AgentPurpose: strings.SplitN(tc.subtitle, " · ", 2)[0]}}, Callbacks: Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}}}
			markup := renderAgentUXChat3Node(t, Build(model), width)
			parts := strings.SplitN(tc.subtitle, " · ", 2)
			for _, want := range []string{"Policy Helper", `class="conversation-topic agent-header-purpose"`, parts[0], `class="agent-header-private"`, chatux016Text(tc.locale, "private"), parts[1], `placeholder="` + tc.placeholder + `"`, `class="agent-badge agent-badge-label"`} {
				if !strings.Contains(markup, want) {
					t.Errorf("%s at %dpx direct-agent header missing %q: %s", tc.locale, width, want, markup)
				}
			}
			if strings.Contains(markup, `class="mention-menu"`) || strings.Contains(markup, `role="listbox"`) {
				t.Fatalf("%s at %dpx restored draft opened the mention menu: %s", tc.locale, width, markup)
			}
			if strings.Contains(markup, `class="chat-side thread-pane"`) || strings.Contains(markup, ">Follow<") {
				t.Fatalf("%s at %dpx direct-agent conversation exposed a thread pane: %s", tc.locale, width, markup)
			}
		}
	}
}

func TestAgentUXChat3_ThreadShowsAgentAnswerInsteadOfEmptyState(t *testing.T) {
	now := time.Now()
	model := Model{
		State: StateReady, Locale: "en-US", CurrentUser: "alice", SelectedID: "general", ShowThread: true, ThreadParentID: "question",
		Conversations:      []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}},
		Messages:           []Message{{ID: "question", AuthorID: "alice", Author: "Alice", Body: "@Policy Helper What is the PTO policy?", TimeLabel: "5:24 AM", PersonaReferences: []ChatReference{{Kind: "AGENT_MENTION", ID: "policy-helper", Display: "Policy Helper", ConversationID: "general"}}}},
		PersonaInvocations: []PersonaThreadInvocation{{PostID: "question", Projection: PersonaProgressProjection{InvocationID: "run-1", ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", PrivateReplyHref: "/workspace/app/chat#policy"}}},
		EphemeralMessages:  []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: "Carry over up to five days.", OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}},
		Callbacks:          Callbacks{CloseThread: func() {}, ReplyInThread: func(string, string) {}},
	}
	markup := render(t, model)
	for _, want := range []string{"Carry over up to five days.", "To ask Policy Helper more, use Ask a follow-up.", `class="thread-back-label"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("answered thread missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "No replies yet") {
		t.Fatalf("answered thread retained its empty sentence: %s", markup)
	}
}

func TestAgentUXChat3_UnreadAgentRowsRemainDistinctFromSelection(t *testing.T) {
	model := Model{SelectedID: "general", Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}, {ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, Unread: 2}}, Callbacks: Callbacks{SelectConversation: func(string) {}}}
	selected := renderAgentUXChat3Node(t, railRow(model, model.Conversations[0]), 390)
	unread := renderAgentUXChat3Node(t, railRow(model, model.Conversations[1]), 390)
	if !strings.Contains(selected, `class="chat-row selected"`) || strings.Contains(unread, `class="chat-row selected`) || !strings.Contains(unread, `class="chat-row unread"`) || !strings.Contains(unread, `class="chat-badge"`) || !strings.Contains(unread, ">2<") {
		t.Fatalf("selection/unread state is ambiguous: selected=%s unread=%s", selected, unread)
	}
}
