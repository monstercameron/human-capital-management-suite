package chatui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func chat4Fixture(locale, state string, direct bool) Model {
	now := time.Now().Add(-time.Minute)
	room := Conversation{ID: "general", Name: "general", Kind: PublicChannel, MemberCount: 18, Joined: true}
	if direct {
		room = Conversation{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true}
	}
	ref := ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "tenant", Display: "Policy Helper", ConversationID: room.ID}
	model := Model{State: StateReady, Locale: locale, CurrentUser: "alice", CurrentTenantID: "tenant", SelectedID: room.ID,
		Conversations:           []Conversation{room, {ID: "people", Name: "people-ops", Kind: PublicChannel, Joined: true}},
		Messages:                []Message{{ID: "question", AuthorID: "alice", Author: "Alice", Body: "@Policy Helper explain our PTO policy in detail: accrual", TimeLabel: "9:30", SentAt: now, PersonaReferences: []ChatReference{ref}, Replies: 2}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ref, Handle: "policy-helper", Purpose: "Answer policy questions", Owner: "People Operations", Initials: "PH"}},
		PersonaLookup:           PersonaLookupReady, PersonaLookupConversationID: room.ID,
		Callbacks: Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}, SendMessage: func(string, string) {}, ReplyInThread: func(string, string) {}, SelectConversation: func(string) {}, ToggleSidebar: func(bool) {}, OpenThread: func(string) {}, CloseThread: func() {}, ToggleDetails: func(bool) {}, SubmitAgentFeedback: func(string, bool) {}, UndoAgentFeedback: func(string) {}},
	}
	projection := PersonaProgressProjection{InvocationID: "run", ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", PrivateReplyHref: ChannelReferenceURL("policy"), DurablePostID: "answer"}
	switch state {
	case "sent", "working2", "working15":
		seconds := 0
		if state == "working2" {
			seconds = 2
		}
		if state == "working15" {
			seconds = 15
		}
		projection.Progress = &PersonaProgressProps{InvocationID: "run", InvokerID: "alice", AgentName: "Policy Helper", Visible: true, ElapsedSeconds: seconds}
	case "failed", "permission":
		code := "MODEL_UNAVAILABLE"
		if state == "permission" {
			code = "DOCUMENT_PERMISSION_DENIED"
		}
		projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: code, Retryable: state == "failed"}
	case "answered":
		question, _ := json.Marshal(agentQuestionContext{Label: "#general", Text: "What is the PTO carryover policy?", At: now})
		body := "Carry over up to 40 hours.\n\nSources\n- [Paid time off policy](/workspace/app/docs?document=pto)\n\n[chat-agent-question-context:" + base64.RawURLEncoding.EncodeToString(question) + "](/chat/share/signed)"
		if direct {
			model.Messages = append(model.Messages, Message{ID: "answer", AuthorID: "policy-helper", Author: "Policy Helper", Body: body, TimeLabel: "9:31", SentAt: now.Add(time.Second), PersonaActor: &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}})
		} else {
			model.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: body, OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
		}
	}
	model.PersonaInvocations = []PersonaThreadInvocation{{PostID: "question", Projection: projection}}
	return model
}

func chat4Matrix(t *testing.T, check func(*testing.T, string, int)) {
	t.Helper()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, width := range []int{1440, 800, 390, 320} {
			t.Run(fmt.Sprintf("%s/%d", locale, width), func(t *testing.T) { check(t, locale, width) })
		}
	}
}

func chat4Require(t *testing.T, markup string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in rendered tree or stylesheet", want)
		}
	}
}

func TestAgentUXChat4_M1_ComposerColumn(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", false)
		model.Draft = "explain our PTO policy in detail: accrual"
		markup := renderAgentUXChat3Node(t, composer(model, handlers{composerAgentName: "Policy Helper", mentionReplyHint: "privacy"}), width)
		chat4Require(t, markup, `class="composer-draft"`, `class="composer-agent-token"`, `data-chat-value="explain our PTO policy in detail: accrual"`, `class="composer-toolbar"`, `aria-disabled="false"`, `dir="`+agentReplyDirection(locale)+`"`)
		if strings.Contains(markup, ` value="explain`) {
			t.Fatal("typed input is bound to a render-time value")
		}
		if strings.Index(markup, "composer-draft") > strings.Index(markup, "composer-agent-reply-hint") || strings.Index(markup, "composer-agent-reply-hint") > strings.Index(markup, "composer-toolbar") {
			t.Fatal("composer draft/hint/actions order changed")
		}
	})
	chat4Require(t, AgentUXChat4Styles, `.chat-workspace .chat-composer:not(:focus-within){display:flex;flex-direction:column`, `width:100%`, `text-indent:attr(data-agent-indent`, `.composer-agent-token{position:absolute`, `.chat-workspace .chat-composer:not(:focus-within) .format-tools{display:flex}`)
}

func TestAgentUXChat4_EmojiTrigger(t *testing.T) {
	sentence := "explain our PTO policy in detail: accrual"
	for caret := 0; caret <= len(utf16.Encode([]rune(sentence))); caret++ {
		if q, _, ok := emojiCompletionToken(sentence, caret); ok {
			t.Fatalf("ordinary sentence opened emoji completion at %d: %q", caret, q)
		}
	}
	for _, value := range []string{":", ":s", ": sm", "detail:sm", "https://sm", ":sm ", ": acc"} {
		if _, _, ok := emojiCompletionToken(value, len(utf16.Encode([]rune(value)))); ok {
			t.Errorf("invalid trigger accepted: %q", value)
		}
	}
	for _, value := range []string{":sm", "hello :sm", "😀 :sm"} {
		q, start, ok := emojiCompletionToken(value, len(utf16.Encode([]rune(value))))
		if !ok || q != "sm" {
			t.Fatalf("valid word did not open: %q", value)
		}
		updated, _ := insertEmojiAtUTF16(value, "😀 ", start, len(utf16.Encode([]rune(value))))
		if strings.Contains(updated, ":sm") || !strings.HasSuffix(updated, "😀 ") {
			t.Fatalf("completion corrupted draft: %q", updated)
		}
	}
	local := localStore{box: &localUI{emojiCompletion: emojiCompletion{Target: "chat-composer", Query: "sm", Open: true, Active: -1}}}
	if emojiCompletionKey(local, "Enter", "chat-composer") || local.get().emojiCompletion.Open {
		t.Fatal("unhighlighted completion swallowed Send")
	}
	local.box.emojiCompletion = emojiCompletion{Target: "chat-composer", Query: "sm", Open: true, Active: -1}
	if !emojiCompletionKey(local, "Escape", "chat-composer") || local.get().emojiCompletion.Open {
		t.Fatal("Escape did not close completion")
	}
	local.box.emojiCompletion = emojiCompletion{Target: "chat-composer", Query: "sm", Open: true, Active: -1}
	if !emojiCompletionKey(local, "ArrowDown", "chat-composer") {
		t.Fatal("arrow did not highlight a completion")
	}
	state := local.get().emojiCompletion
	aria := emojiCompletionFieldAria(state, "chat-composer", map[string]string{"describedby": "composer-help"})
	if aria["activedescendant"] != "chat-composer-emoji-option-0" || aria["describedby"] != "composer-help" {
		t.Fatal("highlighted completion has no accessible field association")
	}
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", false)
		markup := renderAgentUXChat3Node(t, emojiCompletionMenu(model, state, "chat-composer"), width)
		chat4Require(t, markup, `role="listbox"`, `aria-selected="true"`, `id="chat-composer-emoji-option-0"`, chatEmojiFormat(model, emojiKeyItem, map[string]string{"emoji": "😀"}))
	})
}

func TestAgentUXChat4_M2_FirstPaintAuthor(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "answered", false)
		model.PersonaInvocations = nil
		model.PersonaPostActors = nil
		markup := renderAgentUXChat3Node(t, html.Div(html.Props{}, personaReplyRowsForPost(model, localUI{}, "question", time.Now())...), width)
		chat4Require(t, markup, `class="agent-reply-name">Policy Helper`, "class=\"agent-icon\"")
		model.Messages[0].PersonaReferences = nil
		markup = renderAgentUXChat3Node(t, html.Div(html.Props{}, personaReplyRowsForPost(model, localUI{}, "question", time.Now())...), width)
		chat4Require(t, markup, `class="agent-reply-name">`+personaProgressText(model, "chat.agent.name", "Agent"))
		if strings.Contains(markup, "identity-pending") {
			t.Fatal("completed answer kept a placeholder")
		}
	})
}

func TestAgentUXChat4_M3_AtomicAnswerMetadata(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "answered", false)
		markup := renderAgentUXChat3Node(t, html.Div(html.Props{}, personaReplyRowsForPost(model, localUI{}, "question", time.Now())...), width)
		// CHATUX-003: the link to the saved copy is in the card's more menu.
		chat4Require(t, markup, "agent-reply-sources", "agent-feedback-button", "agent-reply-more", "agent-follow-up", "Policy Helper", "<time")
		if strings.Count(markup, `data-agent-reply-state="answered-private"`) != 1 {
			t.Fatal("answer duplicated its working row")
		}
		model.PersonaInvocations = nil
		markup = renderAgentUXChat3Node(t, html.Div(html.Props{}, personaReplyRowsForPost(model, localUI{}, "question", time.Now())...), width)
		chat4Require(t, markup)
		direct := chat4Fixture(locale, "answered", true)
		direct.PersonaInvocations = nil
		if strings.Contains(renderAgentUXChat3Node(t, Build(direct), width), "agent-reply-pending-feedback") {
			t.Fatal("saved answer keeps pending feedback")
		}
	})
}

func TestAgentUXChat4_M5_StateJourney(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		for _, direct := range []bool{false, true} {
			for _, state := range []string{"sent", "working2", "working15", "answered", "failed"} {
				model := chat4Fixture(locale, state, direct)
				markup := renderAgentUXChat3Node(t, Build(model), width)
				chat4Require(t, markup, "Policy Helper", `class="send-button" disabled`, `data-chat-value=""`)
				if strings.Contains(markup, `data-thread-open="true"`) {
					t.Fatal("send opened a thread")
				}
				switch state {
				case "sent", "working2", "working15":
					chat4Require(t, markup, `data-agent-reply-state="working"`, "agent-working-dots", `role="status"`)
					if state == "working15" {
						chat4Require(t, markup, ">"+chatNumeral(locale, "0:15")+"<")
					}
				case "answered":
					chat4Require(t, markup, "Carry over up to 40 hours.", "agent-feedback-button")
					if direct {
						chat4Require(t, markup, "agent-question-context", "What is the PTO carryover policy?")
					} else if strings.Contains(markup, "agent-question-context") {
						t.Fatal("channel repeated question")
					}
					if strings.Contains(markup, `data-agent-reply-state="working"`) {
						t.Fatal("answer left a working card behind")
					}
				case "failed":
					chat4Require(t, markup, `data-agent-reply-state="failed"`, `data-agent-action="retry"`)
				}
			}
		}
	})
}

func TestAgentUXChat4_M6_AccessRequest(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "permission", false)
		markup := renderAgentUXChat3Node(t, html.Div(html.Props{}, personaReplyRowsForPost(model, localUI{}, "question", time.Now())...), width)
		chat4Require(t, markup, `data-action="agent-request-access"`, agentUXChat4Text(model, "chat.agent.request_access"), "9:30")
		// CHATBUG-054: a failed card has the answered card's header line, with who
		// can see it at its end and nothing on a line of its own above the agent.
		if strings.Index(markup, "agent-reply-private") < strings.Index(markup, "agent-reply-identity") {
			t.Fatal("the privacy note is on a line of its own above the agent")
		}
		var selected, drafted, body string
		model.Callbacks.SelectConversation = func(id string) { selected = id }
		model.Callbacks.DraftChanged = func(id, value string) { drafted, body = id, value }
		requestAgentDocumentAccess(model)
		if selected != "people" || drafted != "people" || body != agentUXChat4Text(model, "chat.agent.access_draft") {
			t.Fatalf("access request lost destination/draft: %q %q %q", selected, drafted, body)
		}
	})
}

func TestAgentUXChat4_M7_UnresolvedMention(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", false)
		model.Draft = "@pol explain our PTO policy in detail"
		markup := renderAgentUXChat3Node(t, composerUnresolvedMention(model, ""), width)
		chat4Require(t, markup, agentUXChat4Format(model, "chat.agent.suggest_mention", map[string]string{"name": "Policy Helper"}), agentUXChat4Text(model, "chat.agent.unresolved_plain"))
		// CHATUX-006: the hint is for a message that was just sent.
		message := Message{ID: "unresolved", AuthorID: "alice", Body: model.Draft, SentAt: time.Now()}
		chat4Require(t, renderNode(t, unresolvedMessageRecovery(model, message)), `data-recipient-only="true"`, `data-action="agent-suggest-mention"`)
		message.AuthorID = "bob"
		if unresolvedMessageRecovery(model, message) != nil {
			t.Fatal("second member saw private recovery")
		}
	})
	model := chat4Fixture("en-US", "sent", false)
	model.Members = []Member{{ID: "pol", Name: "Pol"}}
	if _, _, _, found := unresolvedAgent(model, "@Pol hello"); found {
		t.Fatal("exact person match triggered agent suggestion")
	}
	for _, body := range []string{"@Policy Helper hello", "@policy-helper hello", "hello a@pol.example"} {
		if _, _, _, found := unresolvedAgent(model, body); found {
			t.Errorf("resolved mention/email triggered recovery: %q", body)
		}
	}
	model.Members = nil
	model.Messages = append(model.Messages, Message{ID: "unresolved", AuthorID: "alice", Body: "@pol explain PTO"})
	var sent string
	var references []ChatReference
	model.Callbacks.SendMessageWithReferences = func(_, body string, refs []ChatReference) { sent, references = body, refs }
	selectUnresolvedAgent(model, mentionStore{}, "policy-helper", "unresolved")
	if sent != "@Policy Helper explain PTO" || len(references) != 1 || references[0].ID != "policy-helper" {
		t.Fatalf("recovery did not send canonical mention: %q %+v", sent, references)
	}
	if refs := (mentionStore{box: &mentionBox{}}).PersonaReferences("chat-composer", "general", "@pol explain PTO"); len(refs) != 0 {
		t.Fatal("unselected partial mention became an invocation")
	}
	other := model.ResolvedPersonaMentions[0]
	other.Reference.ID, other.Reference.Display, other.Handle = "policy-other", "Policy Guide", "policy-guide"
	model.ResolvedPersonaMentions = append(model.ResolvedPersonaMentions, other)
	if unresolvedMessageRecovery(model, model.Messages[len(model.Messages)-1]) != nil {
		t.Fatal("ambiguous sent fragment offered an unintended agent")
	}
	sent = ""
	selectUnresolvedAgent(model, mentionStore{}, "policy-helper", "unresolved")
	if sent != "" {
		t.Fatal("ambiguous recovery sent a new public message")
	}
}

func TestAgentUXChat4_M8_PrivacyHint(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", false)
		store := mentionStore{box: &mentionBox{personas: []personaDraftMention{{Target: "chat-composer", ConversationID: "general", Reference: model.ResolvedPersonaMentions[0].Reference}}}}
		hint := mentionReplyHint(model, store, "chat-composer")
		// AGENTUX-070: the answer is posted to the channel unless kept private.
		want := chatux003Format(model, "chatux003.hint.public", map[string]string{"channel": "\u2068#general\u2069", "name": "Policy Helper"})
		if hint != want || want == "" || strings.Contains(hint, "reply here") {
			t.Fatalf("privacy hint = %q", hint)
		}
		chat4Require(t, renderAgentUXChat3Node(t, composerAgentReplyHint(hint), width), "#general", "Policy Helper")
	})
}

func TestAgentUXChat4_M9_DirectFailure(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "failed", true)
		markup := renderAgentUXChat3Node(t, Build(model), width)
		chat4Require(t, markup, `class="message agent-direct-state"`, `class="message-author">Policy Helper`, `class="message-time">9:30`, `data-agent-action="retry"`, "agent-dm-avatar")
		if strings.Contains(markup, `class="message-stats"`) || strings.Contains(markup, "agent-reply-private") {
			t.Fatal("direct failure retained channel count/privacy")
		}
	})
}

func TestAgentUXChat4_M10_M13_G82_G86_M15_Layout(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "answered", false)
		model.ShowThread, model.ThreadParentID = true, "question"
		markup := renderAgentUXChat3Node(t, threadPane(model, handlers{}), width)
		chat4Require(t, markup, "agent-reply-name", "Carry over", `dir="`+agentReplyDirection(locale)+`"`, "thread-scroll")
	})
	chat4Require(t, AgentUXChat4Styles, `--chat-measure:72ch`, `.message-content,.message-content>.agent-reply-row`, `max-width:72ch`, `margin-inline:0;text-align:match-parent`, `.message-meta{flex-wrap:nowrap`, `.message-meta .agent-badge,.message-time{flex:none;white-space:nowrap}`, `.thread-scroll{overflow-x:hidden}`, `.agent-reply-name{font-weight:700}`)
}

func TestAgentUXChat4_M11_FollowUp(t *testing.T) {
	model := chat4Fixture("en-US", "answered", false)
	var selected string
	model.Callbacks.SelectConversation = func(id string) { selected = id }
	openAgentFollowUp(model, ChannelReferenceURL("policy"))
	if selected != "policy" {
		t.Fatal("follow-up did not route to the agent conversation")
	}
	selected = ""
	for _, href := range []string{"https://evil.invalid/workspace/app/chat#channel=policy", "/other#channel=policy", "//evil.invalid/workspace/app/chat#channel=policy"} {
		openAgentFollowUp(model, href)
	}
	if selected != "" {
		t.Fatal("follow-up accepted an external route")
	}
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "answered", false)
		model.ShowThread, model.ThreadParentID = true, "question"
		markup := renderAgentUXChat3Node(t, threadPane(model, handlers{}), width)
		chat4Require(t, markup, agentUXChat4Text(model, "chat.agent.ask_follow_up"), agentThreadFollowUpHint(model), `placeholder="`+agentThreadPlaceholder(model)+`"`)
	})
}

func TestAgentUXChat4_M12_NoReceipt(t *testing.T) {
	model := chat4Fixture("en-US", "answered", false)
	model.ShowThread, model.ThreadParentID = true, "question"
	model.ThreadMessages = []Message{{ID: "legacy", Body: legacyPrivateAnswerReceiptBody, Author: "Alice", TimeLabel: "9:31"}}
	markup := renderNode(t, threadPane(model, handlers{}))
	if strings.Contains(markup, "agent-legacy-private") || strings.Contains(markup, "The answer was sent privately") {
		t.Fatal("thread duplicated privacy with a legacy receipt")
	}
}

func TestAgentUXChat4_M14_M21_Header(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", true)
		markup := renderAgentUXChat3Node(t, timeline(model, handlers{}), width)
		chat4Require(t, markup, chatux016Text(locale, "private"), "agent-header-private", "agent-header-purpose", `data-action="details"`)
		model = chat4Fixture(locale, "sent", false)
		model.ShowThread = true
		chat4Require(t, renderAgentUXChat3Node(t, timeline(model, handlers{}), width), "topic-kind", "18")
	})
	chat4Require(t, AgentUXChat4Styles, `.conversation-topic-long{display:none}`, `.chat-workspace .conversation-topic-short{display:block`, `.conversation-topic .topic-kind{display:inline}`, `.chat-workspace .conversation-header{min-height:56px;height:56px`)
}

func TestAgentUXChat4_M16_J16_G87_TouchControls(t *testing.T) {
	chat4Require(t, AgentUXChat4Styles, `.chat-workspace .message-actions{display:none}`, `.chat-workspace .thread-more{display:none}`, `.chat-rail-row .rail-row-more{visibility:hidden;pointer-events:none}`, `.chat-rail-row .agent-badge{margin-inline-end:8px}`, `.message-stats{color:var(--hcm-color-brand-primary);min-height:44px}`)
	if bindChatMessageLongPress(Model{}) != nil {
		t.Fatal("native render installed browser-only listeners")
	}
	focusAgentChatComposer()
	pinAgentChatAfterSend("chat-composer")
}

func TestAgentUXChat4_G76_J16_G66_M19_Drawer(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "answered", false)
		agent := Conversation{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Unread: 1}
		model.Conversations = append(model.Conversations, agent)
		model.Sections = []SidebarSection{{ID: "direct", Name: "Direct messages", Chats: []Conversation{agent}}}
		model.SidebarOpen = true
		markup := renderAgentUXChat3Node(t, Build(model), width)
		chat4Require(t, markup, `data-sidebar-open="true"`, `class="chat-row unread"`, `data-action="select" data-id="policy"`, `class="agent-badge agent-badge-label"`, model.tf(KeyUnreadCount, map[string]string{"n": model.n(1)}))
		model.SelectedID, model.SidebarOpen = "policy", false
		model.Messages = nil
		selected := renderAgentUXChat3Node(t, Build(model), width)
		chat4Require(t, selected, `data-sidebar-open="false"`, `class="chat-row selected unread"`, "Policy Helper")
	})
	chat4Require(t, AgentUXChat3Styles, `.chat-workspace[data-sidebar-open="true"] .chat-main`, `display:flex;position:absolute;inset:0;width:100%;max-width:none;z-index:8`)
}

func TestAgentUXChat4_J9_RenderedRetry(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		for _, direct := range []bool{false, true} {
			model := chat4Fixture(locale, "failed", direct)
			chat4Require(t, renderAgentUXChat3Node(t, Build(model), width), `data-agent-reply-state="failed"`, `data-agent-action="retry"`)
			model.PersonaInvocations = chat4Fixture(locale, "working2", direct).PersonaInvocations
			working := renderAgentUXChat3Node(t, Build(model), width)
			chat4Require(t, working, `data-agent-reply-state="working"`)
			if strings.Contains(working, `data-agent-reply-state="failed"`) {
				t.Fatal("retry retained the old failure")
			}
			model = chat4Fixture(locale, "answered", direct)
			answer := renderAgentUXChat3Node(t, Build(model), width)
			chat4Require(t, answer, "Carry over up to 40 hours.")
			if strings.Contains(answer, `data-agent-reply-state="failed"`) || strings.Contains(answer, `data-agent-reply-state="working"`) {
				t.Fatal("successful retry did not replace the old state")
			}
		}
	})
}

func TestAgentUXChat4_G72_MenuDetails(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", false)
		markup := renderAgentUXChat3Node(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "pol", Open: true, Active: 0, Details: true}, "chat-composer"), width)
		chat4Require(t, markup, "mention-profile-card", "People Operations", `aria-expanded="true"`)
	})
}

func TestAgentUXChat4_J3_J4_G72_InlineDetails(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", false)
		model.renderReferences = model.Messages[0].PersonaReferences
		markup := renderAgentUXChat3Node(t, html.P(html.Props{}, mentionReferenceBody(model, "@Policy Helper explain PTO")...), width)
		chat4Require(t, markup, `<span class="mention-chip-details"><button`, `data-action="agent-profile-open"`, "@Policy Helper", " explain PTO")
		if strings.Contains(markup, "<details") || strings.Contains(markup, "<article") {
			t.Fatal("block content split the paragraph")
		}
		chat4Require(t, renderNode(t, agentProfileDialog(model, "policy-helper")), `role="dialog"`, `aria-modal="true"`, `data-action="agent-profile-close"`, "People Operations", `class="agent-summary"`)
	})
}

func TestAgentUXChat4_J8_FeedbackResult(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "answered", true)
		markup := renderAgentUXChat3Node(t, renderAgentFeedback(model, localUI{agentFeedback: map[string]string{"run": "not-right"}}, "answer"), width)
		chat4Require(t, markup, `aria-pressed="true"`, `data-action="agent-feedback-undo"`, "Policy Helper", agentReplyFallback(locale, "chat.agent.undo", "Undo"))
		undone := renderNode(t, renderAgentFeedback(model, localUI{}, "answer"))
		if strings.Contains(undone, `aria-pressed="true"`) || strings.Contains(undone, "agent-feedback-sent") {
			t.Fatal("undo left feedback selected")
		}
	})
}

func TestAgentUXChat4_J14_G73_MenuFiltering(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", false)
		model.Members = []Member{{ID: "ana", Name: "Ana Flores"}, {ID: "ben", Name: "Ben Whitaker"}, {ID: "alice", Name: "Alice"}}
		markup := renderAgentUXChat3Node(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "pol", Open: true}, "chat-composer"), width)
		chat4Require(t, markup, "Policy Helper", "mention-hint")
		if strings.Contains(markup, "Ana Flores") || strings.Contains(markup, "Ben Whitaker") {
			t.Fatal("unrelated people appeared for @pol")
		}
		bare := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
		chat4Require(t, bare, "Ana Flores", "Ben Whitaker", "Policy Helper")
		if strings.Count(bare, `class="mention-agent-row"`) != 1 {
			t.Fatal("bare mention duplicated the agent")
		}
		if !strings.Contains(markup, ">Esc ") || !strings.Contains(markup, ">Tab ") {
			t.Fatal("keyboard shortcut segments disappeared")
		}
	})
}

func TestAgentUXChat4_G68_SecondMember(t *testing.T) {
	model := chat4Fixture("en-US", "answered", false)
	model.CurrentUser = "bob"
	model.PersonaInvocations[0].Projection.ViewerID = "bob"
	model.EphemeralMessages = nil
	markup := renderNode(t, html.Div(html.Props{}, personaReplyRowsForPost(model, localUI{}, "question", time.Now())...))
	if strings.Contains(markup, "agent-reply-state") || strings.Contains(markup, "Carry over") {
		t.Fatal("second member received private answer/status")
	}
}

func TestAgentUXChat4_J19_J20_G83_CardAndActionsStyles(t *testing.T) {
	chat4Require(t, AgentUXChat3Styles, `background:var(--surface)`, `border-inline-start:3px solid var(--accent)`, `.message.search-target .agent-reply-row,.message.thread-active .agent-reply-row{background:var(--surface)}`)
	chat4Require(t, AgentUXChat4Styles, `.mention-menu .agent-badge{border-color:var(--hcm-color-brand-primary)`, `.agent-reply-identity .avatar,.agent-dm-avatar{background:var(--hcm-color-surface);color:var(--hcm-color-text)`, `.message-actions{top:0;transform:translateY(-50%)`, `.timeline-frame.away .message-list{padding-bottom:60px}`, `.jump-newest{bottom:8px`)
}

func TestAgentUXChat4_J10_CardPrivacyGeometry(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		for _, state := range []string{"sent", "working15", "answered", "failed"} {
			model := chat4Fixture(locale, state, false)
			markup := renderAgentUXChat3Node(t, html.Div(html.Props{}, personaReplyRowsForPost(model, localUI{}, "question", time.Now())...), width)
			chat4Require(t, markup, "agent-reply-private", "<time", "agent-reply-identity")
			// CHATUX-003 and CHATBUG-054: an answered and a failed card have one
			// header line with who can see it at its end; the working card keeps
			// the privacy line first.
			privateFirst := strings.Index(markup, "agent-reply-private") < strings.Index(markup, "agent-reply-identity")
			if privateFirst == (state == "answered" || state == "failed") {
				t.Fatalf("%s card placed the privacy note first=%v", state, privateFirst)
			}
		}
	})
	chat4Require(t, AgentUXChat4Styles, `.message-content{font-size:.9375rem}`, `.agent-question-context,.agent-reply-sources{width:100%;max-width:none}`, `.agent-reply-pending-conversation{min-height:80px}`)
}

func TestAgentUXChat4_Section5_ThemesAndThread(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		for _, theme := range []string{"light", "dark"} {
			for _, state := range []string{"sent", "working2", "working15", "answered", "permission"} {
				model := chat4Fixture(locale, state, false)
				model.Draft = "explain our PTO policy in detail: accrual"
				model.ShowThread, model.ThreadParentID = true, "question"
				markup := renderAgentUXChat3Node(t, html.Div(html.Props{Data: map[string]string{"hcm-theme": theme}}, Build(model)), width)
				chat4Require(t, markup, `data-hcm-theme="`+theme+`"`, `data-thread-open="true"`, "thread-scroll", `data-chat-value="explain our PTO policy in detail: accrual"`, "Policy Helper", `dir="`+agentReplyDirection(locale)+`"`)
			}
		}
	})
	if strings.Contains(AgentUXChat4Styles, "#") || strings.Contains(AgentUXChat4Styles, "font-family") {
		t.Fatal("chat layout bypasses customer theme tokens")
	}
}

func TestAgentUXChat4_M12_LegacyReceipt(t *testing.T) {
	if !IsLegacyPrivateAnswerReceipt("  "+legacyPrivateAnswerReceiptBody+"\n") || IsLegacyPrivateAnswerReceipt("The team replied publicly.") {
		t.Fatal("legacy receipt filter suppressed an ordinary reply or missed an old private notice")
	}
}
