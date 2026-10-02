package chatui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func TestAgentUXChat5_ComposerKeyAction(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		state     composerKeyState
		want      composerAction
	}{
		{"menu highlighted", "Enter", composerKeyState{Draft: "@pol", MentionOpen: true, MentionHighlighted: true}, composerPickMention},
		{"just closed", "Enter", composerKeyState{Draft: "how many PTO hours carry over?"}, composerSend},
		{"emoji highlighted", "Enter", composerKeyState{Draft: ":sm", EmojiOpen: true, EmojiHighlighted: true}, composerPickEmoji},
		{"emoji unhighlighted", "Enter", composerKeyState{Draft: ":sm", EmojiOpen: true}, composerSend},
		{"suggestion", "Enter", composerKeyState{Draft: "@pol how many hours?", SuggestionVisible: true}, composerSend},
		{"empty", "Enter", composerKeyState{}, composerEmpty},
		{"selected draft text", "Enter", composerKeyState{Draft: "how many hours?"}, composerSend},
		{"only detached mention", "Enter", composerKeyState{Draft: " "}, composerEmpty},
		{"shift", "Enter", composerKeyState{Draft: "question", Shift: true, MentionOpen: true, MentionHighlighted: true}, composerNative},
		{"composition", "Enter", composerKeyState{Draft: "question", Composing: true, MentionOpen: true, MentionHighlighted: true}, composerNative},
		{"tab", "Tab", composerKeyState{Draft: "@pol", MentionOpen: true, MentionHighlighted: true}, composerPickMention},
		{"document", "Enter", composerKeyState{Draft: "[[pol", DocumentOpen: true, DocumentHighlighted: true}, composerPickDocument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := composerKeyAction(tc.state, tc.key); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestAgentUXChat5_MentionEnterSequence(t *testing.T) {
	for _, direct := range []bool{false, true} {
		for _, name := range []string{"Assistant", "Policy Helper"} {
			t.Run(fmt.Sprintf("direct=%t/%s", direct, name), func(t *testing.T) {
				model := chat4Fixture("en-US", "sent", direct)
				ref := model.ResolvedPersonaMentions[0].Reference
				ref.Display = name
				store := mentionStore{box: &mentionBox{}}
				store.ForConversation(model.SelectedID)
				state := mentionState{Target: "chat-composer", Query: "pol", Start: 0, End: 4, Open: true}
				store.Set(state)
				if got := composerKeyAction(composerKeyState{Draft: "@pol", MentionOpen: true, MentionHighlighted: true}, "Enter"); got != composerPickMention {
					t.Fatal(got)
				}
				// The chosen agent becomes a detached chip, preserving its canonical reference.
				store.Set(mentionState{})
				value, _ := insertEmojiAtUTF16("@pol", "", 0, 4)
				store.AddPersonaToken("chat-composer", model.SelectedID, ref)
				value += "how many PTO hours carry over?"
				// Even a stale pre-selection snapshot cannot intercept Enter after typing.
				live := liveComposerMention(state, "chat-composer", value, len(utf16.Encode([]rune(value))))
				if live.Open || store.Get().Open {
					t.Fatal("selected mention left its menu open")
				}
				if composerKeyAction(composerKeyState{Draft: value, MentionOpen: live.Open}, "Enter") != composerSend {
					t.Fatal("question swallowed Enter")
				}
				body, refs, ready := composerSendPayload(value, "", store.PersonaReferences("chat-composer", model.SelectedID, value))
				if !ready || len(refs) != 1 || refs[0].ID != ref.ID || body != "how many PTO hours carry over?" {
					t.Fatalf("lost question/reference: %q %+v", body, refs)
				}
				if got := bodyWithAgentMentions(body, refs); got != "@"+name+" how many PTO hours carry over?" {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestAgentUXChat5_HoverGeometry(t *testing.T) {
	css := ScopedStylesheet()
	rules := regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(css, -1)
	row := regexp.MustCompile(`\.(message|chat-ephemeral|chat-row|reaction)(?:[.:][^\s>]*)?$`)
	geometry := regexp.MustCompile(`(?:^|;)\s*(?:padding(?:-[\w-]+)?|margin(?:-[\w-]+)?|border(?:-[\w-]+)?-width|height|min-height|line-height|font-size)\s*:`)
	for _, rule := range rules {
		for _, selector := range strings.Split(rule[1], ",") {
			if (strings.Contains(selector, ":hover") || strings.Contains(selector, ":focus-within")) && row.MatchString(strings.TrimSpace(selector)) && geometry.MatchString(rule[2]) {
				t.Fatalf("row geometry changes in %s {%s}", selector, rule[2])
			}
		}
	}
	chat4Require(t, css, "transform:translateY(-50%)", ".message-list .message:first-of-type .message-actions{top:4px;transform:none}")
}

func TestAgentUXChat5_AnchoredLayers(t *testing.T) {
	for action, kind := range map[string]string{"open-todo": "todo", "tray-poll": "poll", "emoji-toggle": "emoji", "react-pick": "reaction", "menu": "menu", "agents-here": "details", "reply": "thread", "chat-search-open": "search", "close-thread": ""} {
		if got := chatLayerKind(action); got != kind {
			t.Fatalf("%s restores focus to %s instead of %s", action, got, kind)
		}
	}
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		for _, rtl := range []bool{false, true} {
			anchor := chatLayerRect{float64(width) - 48, 300, float64(width) - 8, 344}
			g := anchoredChatGeometry(anchor, 360, 260, float64(width), 800, true, rtl)
			if g.left < 8 || g.left+g.width > float64(width)-8 || g.top+g.height > anchor.top-4 {
				t.Fatalf("layer overlaps text or viewport: %+v", g)
			}
		}
		m := chat4Fixture(locale, "answered", false)
		m.ShowThread, m.ThreadParentID, m.PickerID = true, "question", "question"
		markup := renderAgentUXChat3Node(t, Build(m), width)
		if strings.Count(markup, `data-chat-layer="reaction"`) != 1 {
			t.Fatal("thread root opened more than one picker")
		}
		chat4Require(t, markup, chat5Text(m, "chat.emoji.search"), chatEmojiText(m, emojiKeyGrid), `role="grid"`, `aria-label="`+m.t(KeyCloseThread)+`"`)
		for _, kind := range []string{"todo", "poll"} {
			tray := renderAgentUXChat3Node(t, channelTray(m, handlers{}, kind), width)
			chat4Require(t, tray, `data-chat-layer="`+kind+`"`, `role="dialog"`)
		}
		empty := renderNode(t, channelTodoSection(m, handlers{}))
		chat4Require(t, empty, chat5Text(m, "chat.todo.empty_next"), chat5Text(m, "chat.todo.add_next"))
		own := Message{ID: "own", AuthorID: m.CurrentUser, Body: "Question"}
		m.MenuID = "thread:own"
		menu := renderNode(t, html.Div(html.Props{}, threadMessageMenu(m, own)...))
		chat4Require(t, menu, `data-action="copy-link"`, `data-action="edit"`, `data-action="delete"`, `data-action="pin"`, `data-chat-layer="menu"`)
	})
	chat4Require(t, AgentUXChat5Styles, "position:fixed", ".chat-workspace .jump-newest{position:static", ".timeline-frame{display:flex;flex-direction:column")
}

func TestAgentUXChat5_ComposerHintsAndAnswer(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		m := chat4Fixture(locale, "sent", false)
		m.Draft = "@pol"
		h := handlers{mentionView: mentionState{Target: "chat-composer", Query: "pol", Open: true}}
		markup := renderAgentUXChat3Node(t, composer(m, h), width)
		if strings.Contains(markup, `class="composer-unresolved-mention"`) {
			t.Fatal("menu competes with suggestion")
		}
		m.Draft = ""
		markup = renderAgentUXChat3Node(t, composer(m, handlers{mentionReplyHint: "privacy"}), width)
		if strings.Contains(markup, `class="composer-agent-reply-hint"`) || strings.Contains(markup, `class="composer-unresolved-mention"`) {
			t.Fatal("empty composer has a hint")
		}
		m = chat4Fixture(locale, "answered", false)
		markup = renderAgentUXChat3Node(t, Build(m), width)
		if strings.Contains(markup, "agent-question-context") {
			t.Fatal("channel answer repeats its adjacent question")
		}
		m = chat4Fixture(locale, "answered", true)
		chat4Require(t, renderAgentUXChat3Node(t, Build(m), width), "agent-question-context")
		m = chat4Fixture(locale, "sent", false)
		for _, sec := range []int{4, 5, 12} {
			m.PersonaInvocations[0].Projection.Progress.ElapsedSeconds = sec
			card := renderAgentUXChat3Node(t, agentProgressForPost(m, m.PersonaInvocations[0].Projection, "question"), width)
			if (sec >= 5) != strings.Contains(card, "agent-elapsed-seconds") {
				t.Fatalf("elapsed seconds appeared at wrong threshold %d", sec)
			}
		}
		chat4Require(t, renderAgentUXChat3Node(t, chatSearchLayer(m, handlers{local: localUI{searchOpen: true}}), width), `data-chat-layer="search"`, `id="chat-search"`)

	})
	chat4Require(t, AgentUXChat5Styles, ".composer-hint-slot{position:relative;flex:none;height:44px;min-height:44px", ".persona-progress-status.agent-reply-row,.chat-ephemeral.agent-reply-row{width:100%")
}

func TestAgentUXChat5_AgentsHere(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		m := chat4Fixture(locale, "sent", false)
		m.ShowDetails = true
		m.ResolvedPersonaMentions[0].DocumentScope = "CHANNEL_DOCUMENTS"
		m.Conversations[0].OwnerID = m.CurrentUser
		bad := m.ResolvedPersonaMentions[0]
		bad.Reference.TenantID = "other"
		bad.Reference.Display = "Hidden agent"
		stale := m.ResolvedPersonaMentions[0]
		stale.Reference.ConversationID = "other"
		stale.Reference.Display = "Stale agent"
		m.ResolvedPersonaMentions = append(m.ResolvedPersonaMentions, bad, stale)
		markup := renderAgentUXChat3Node(t, chatAgentsSection(m), width)
		chat4Require(t, markup, chat5Text(m, "chat.agents.here"), "Policy Helper", "Answer policy questions", chat5Text(m, "chat.agents.reads_channel"), chat5Text(m, "chat.agents.manage"), `data-action="agent-ask-here"`)
		if strings.Contains(markup, "Hidden agent") || strings.Contains(markup, "Stale agent") {
			t.Fatal("details leaked unauthorized or stale agent")
		}
		m.ResolvedPersonaMentions = m.ResolvedPersonaMentions[:1]
		chat4Require(t, renderAgentUXChat3Node(t, timeline(m, handlers{}), width), agentCountLabel(m, 1), `data-action="agents-here"`)
		store := mentionStore{box: &mentionBox{}}
		selectChatAgent(m, store, "policy-helper")
		if got := store.PersonaReferences("chat-composer", m.SelectedID, ""); len(got) != 1 || got[0].ID != "policy-helper" {
			t.Fatalf("Ask did not install mention: %+v", got)
		}
	})
}

func TestAgentUXChat5_LightPageAndMotion(t *testing.T) {
	m := chat4Fixture("en-US", "sent", false)
	m.Messages = nil
	m.PersonaInvocations = nil
	for i := 0; i < 111; i++ {
		m.Messages = append(m.Messages, Message{ID: fmt.Sprint(i), AuthorID: "person", Author: "Person", Body: "Message"})
	}
	markup := render(t, m)
	controls := integrate1VisibleControls(markup)
	if controls >= 400 || strings.Contains(markup, `class="message-actions"`) {
		t.Fatalf("idle timeline renders %d controls/action bars", controls)
	}
	active := renderNode(t, message(m, handlers{local: localUI{focusRow: "0"}}, m.Messages[0], false))
	chat4Require(t, active, `class="message-actions"`, `role="toolbar"`)
	if chatRowActionsVisible(localUI{}, "0") {
		t.Fatal("blurred row keeps actions")
	}
	if regexp.MustCompile(`\b\d*\.?\d+(ms|s)\b`).MatchString(ScopedStylesheet()) {
		t.Fatal("literal duration bypasses motion tokens")
	}
	chat4Require(t, ScopedStylesheet(), "min-height:24px", ":scope a{min-width:24px;min-height:24px}", "transition:none!important;animation:none!important", "var(--hcm-motion-fast)")
}

func TestAgentUXChat5_DetailsAndAccessibility(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		m := chat4Fixture(locale, "answered", true)
		m.ShowDetails = true
		markup := renderAgentUXChat3Node(t, Build(m), width)
		if strings.Contains(markup, "agent-reply-pending") || strings.Contains(markup, "agent-reply-identity-pending") {
			t.Fatal("completed load retains permanent placeholders")
		}
		intro := renderNode(t, channelIntro(m))
		if strings.Contains(intro, m.t(KeyIntroDirect)) {
			t.Fatal("DM repeats privacy sentence")
		}
		for _, name := range []string{"Assistant", "Policy Helper"} {
			row := renderNode(t, railRow(m, Conversation{ID: name, Name: name, Agent: true, Kind: DirectMessage}))
			chat4Require(t, row, name, "agent-badge")
		}
		stamp := time.Date(2026, 10, 1, 11, 18, 0, 0, time.Local)
		wantClock := map[string]string{"en-US": "11:18 AM", "de-DE": "11:18", "ar": "١١:١٨"}[locale]
		if got := chat5Clock(locale, stamp); got != wantClock {
			t.Fatalf("card clock %q differs from the message format %q", got, wantClock)
		}
		if locale == "ar" && regexp.MustCompile(`[0-9]|AM|PM`).MatchString(chat5Clock(locale, stamp)) {
			t.Fatal("Arabic card time uses Latin numerals")
		}
		root, err := xhtml.Parse(strings.NewReader(markup))
		if err != nil {
			t.Fatal(err)
		}
		labels := map[string]string{}
		walkChat5HTML(root, func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && n.Data == "label" {
				labels[chat5Attr(n, "for")] = chat5NodeText(n)
			}
		})
		walkChat5HTML(root, func(n *xhtml.Node) {
			if n.Type != xhtml.ElementNode {
				return
			}
			switch n.Data {
			case "button", "input", "textarea", "select", "summary", "a":
			default:
				return
			}
			if chat5Attr(n, "type") == "hidden" {
				return
			}
			name := chat5Attr(n, "aria-label") + chat5Attr(n, "title") + labels[chat5Attr(n, "id")]
			if n.Data != "input" && n.Data != "textarea" && n.Data != "select" {
				name += chat5NodeText(n)
			}
			if strings.TrimSpace(name) == "" {
				t.Errorf("unnamed %s id=%s class=%s", n.Data, chat5Attr(n, "id"), chat5Attr(n, "class"))
			}
		})
	})
}

func walkChat5HTML(n *xhtml.Node, visit func(*xhtml.Node)) {
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walkChat5HTML(child, visit)
	}
}
func chat5Attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func chat5NodeText(n *xhtml.Node) string {
	var text strings.Builder
	walkChat5HTML(n, func(child *xhtml.Node) {
		if child.Type == xhtml.TextNode {
			text.WriteString(child.Data)
		}
	})
	return text.String()
}

func TestAgentUXChat5_NativePlatform(t *testing.T) {
	if composerIsComposing(ui.KeyboardEvent{}) || bindChatActiveRows(localStore{}) != nil {
		t.Fatal("native renderer installed browser handlers")
	}
	rememberChatLayerOpener(ui.MouseEvent{})
	restoreChatLayerFocus()
	syncChatAnchoredLayers()
	focusChatAgentsSection()
	focusChatLayerField("field")
	if chatLayerContainsEvent(ui.MouseEvent{}) {
		t.Fatal("native event claims a DOM layer")
	}
	if got := chat5RecentEmoji([]string{"a", "b", "c", "d", "e", "f"}, "b"); strings.Join(got, ",") != "b,a,c,d,e,f" {
		t.Fatal(got)
	}
	if got := chat5RecentEmoji([]string{"a", "b", "c", "d", "e", "f"}, "g"); len(got) != 6 || got[0] != "g" {
		t.Fatal(got)
	}
	if got := renderNode(t, anchoredChatLayer(html.Props{Class: "test"}, "menu", ui.Text("Action"))); !strings.Contains(got, `data-chat-layer="menu"`) {
		t.Fatal(got)
	}
}
