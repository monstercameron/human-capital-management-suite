package chatui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	xhtml "golang.org/x/net/html"
)

func chatPolishMatrix(t *testing.T, check func(*testing.T, Model, int, string)) {
	t.Helper()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, width := range []int{1440, 800, 390, 320} {
			for _, theme := range []string{"light", "dark"} {
				t.Run(fmt.Sprintf("%s/%d/%s", locale, width, theme), func(t *testing.T) {
					m := chat4Fixture(locale, "sent", false)
					check(t, m, width, theme)
				})
			}
		}
	}
}

func chatPolishMarkup(t *testing.T, node ui.Node, width int, theme string) string {
	t.Helper()
	markup := renderNode(t, html.Div(html.Props{Data: map[string]string{"viewport": fmt.Sprint(width), "theme": theme}}, node))
	for _, forbidden := range []string{"<details", "<summary", "<style", ` style="`} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("CSP/disclosure regression: %s", forbidden)
		}
	}
	return markup
}

func chatPolishNodes(t *testing.T, markup string, match func(*xhtml.Node) bool) []*xhtml.Node {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var result []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && match(n) {
			result = append(result, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return result
}

func chatPolishAttr(n *xhtml.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func chatPolishHasClass(n *xhtml.Node, class string) bool {
	return strings.Contains(" "+chatPolishAttr(n, "class")+" ", " "+class+" ")
}

func chatPolishAncestor(n *xhtml.Node, class string) bool {
	for n = n.Parent; n != nil; n = n.Parent {
		if chatPolishHasClass(n, class) {
			return true
		}
	}
	return false
}

func TestChatPolish_A_ReactionAndSingleSearch(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.PickerID = "question"
		markup := chatPolishMarkup(t, html.Div(html.Props{}, rail(m, handlers{local: localUI{searchOpen: true}}), chatSearchLayer(m, handlers{local: localUI{searchOpen: true}}), chatEmojiReactionLayer(m, localUI{})), width, theme)
		inputs := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "input" && chatPolishAttr(n, "type") == "search" })
		// The emoji's labelled filter is separate from the one conversation search.
		count := 0
		for _, n := range inputs {
			if !chatPolishAncestor(n, "emoji-pop") {
				count++
				if chatPolishAttr(n, "id") != "chat-search" {
					t.Fatal("duplicate conversation search")
				}
			}
		}
		if count != 1 {
			t.Fatalf("%d conversation search fields", count)
		}
		chat4Require(t, markup, `data-chat-layer="reaction"`, `popover="manual"`)
		chat4Require(t, markup, `data-chat-layer="search"`)
		for _, rtl := range []bool{false, true} {
			anchor := chatLayerRect{float64(width) - 48, 300, float64(width) - 8, 330}
			g := anchoredChatGeometry(anchor, 360, 240, float64(width), 900, true, rtl)
			if g.left < 8 || g.left+g.width > float64(width)-8 || g.top+g.height > anchor.top-4 {
				t.Fatal(g)
			}
		}
	})
}

func TestChatPolish_B_IconMenuAndDestructiveGroup(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.IsTenantAdmin = true
		// CHATBUG-030 changed this test: the viewer's own message ends with Delete
		// message alone, so the entry count is one lower than when it also
		// carried the moderator's Remove.
		markup := chatPolishMarkup(t, html.Div(html.Props{}, chatMessageMenuItems(m, Message{ID: "post", AuthorID: m.CurrentUser, Body: "Text"})...), width, theme)
		entries := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "menuitem" })
		if len(entries) < 7 {
			t.Fatal("missing menu entries")
		}
		for _, n := range entries {
			if n.Data != "button" || chatPolishAttr(n, "title") == "" {
				t.Fatal("menu entry lacks a button or tooltip")
			}
			iconFound := false
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Data == "svg" {
					iconFound = true
				}
			}
			if !iconFound {
				t.Fatalf("menu item lacks icon: %s", chatPolishAttr(n, "data-action"))
			}
		}
		if strings.Index(markup, `role="separator"`) > strings.Index(markup, `data-action="delete"`) {
			t.Fatal("destructive entries precede separator")
		}
		g := anchoredChatGeometry(chatLayerRect{200, 40, 230, 64}, 280, 360, float64(width), 900, true, false)
		if g.height != 360 || g.top < 68 || g.top+g.height > 892 {
			t.Fatal("menu clipped or overlaps bar", g)
		}
	})
}

func TestChatPolish_C_ThreadSaveAndBoundedComposer(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.ShowThread = true
		m.ThreadParentID = "question"
		m.ThreadMessages = []Message{{ID: "reply", Author: "Alex", Body: "Reply"}}
		markup := chatPolishMarkup(t, threadPane(m, handlers{}), width, theme)
		buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return chatPolishAttr(n, "data-saved-post") == "reply" && chatPolishAttr(n, "data-saved-action") == "save"
		})
		if len(buttons) != 1 || !chatPolishAncestor(buttons[0], "message-actions") {
			t.Fatal("reply save is outside hover bar")
		}
		chat4Require(t, ChatPolishStyles, ".thread-composer{flex:none", "max-block-size:120px", ".thread-message:hover .message-actions")
	})
}

func TestChatPolish_D_WritingStylesStayInToolbar(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		props := ChattoneToolbarProps{Locale: m.Locale, Target: "chat-composer", Draft: "one two three", Styles: chatrewrite.DefaultStyles(), Enabled: true}
		markup := chatPolishMarkup(t, RenderChattoneToolbar(props), width, theme)
		styles := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chattone-action") == "rewrite" })
		if len(styles) != 3 {
			t.Fatal("three style controls missing")
		}
		for _, n := range styles {
			if !chatPolishAncestor(n, "chattone-toolbar-choices") || !chatPolishHasClass(n, "tool-button") || chatPolishAttr(n, "title") == "" {
				t.Fatal("style tool differs from its neighbours")
			}
		}
		notices := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return chatPolishHasClass(n, "chattone-notice") || chatPolishHasClass(n, "chattone-status")
		})
		for _, n := range notices {
			if !chatPolishAncestor(n, "chattone-options") {
				t.Fatal("notice occupies toolbar")
			}
		}
		props.Pending = true
		pending := chatPolishMarkup(t, RenderChattoneToolbar(props), width, theme)
		roots := chatPolishNodes(t, pending, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chattone") == "toolbar" })
		if len(roots) != 1 || !hasChatPolishAttribute(roots[0], "hidden") {
			t.Fatal("unavailable styles are visible")
		}
	})
}

func hasChatPolishAttribute(n *xhtml.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func TestChatPolish_E_LocationSheet(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		markup := chatPolishMarkup(t, ChatmapShareSheet(m.Locale, "chat-composer", "tenant", "room", false), width, theme)
		chat4Require(t, markup, `data-chat-layer="location"`, `class="tool-button"`, `data-chatmap-preview-region="true"`, `data-chatmap-field="note"`, `data-chatmap-field="duration"`)
		for _, key := range []string{"lat", "lon", "accuracy"} {
			if strings.Contains(markup, `data-chatmap-field="`+key+`"`) {
				t.Fatal("coordinates rendered as fields")
			}
		}
		choices := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chatmap-action") == "source" })
		if len(choices) != 3 {
			t.Fatal("missing source choice")
		}
		for _, key := range []string{"north", "south", "east", "west"} {
			buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chatmap-action") == key })
			if len(buttons) != 1 || !chatPolishAncestor(buttons[0], "chatmap-preview-frame") {
				t.Fatal("nudge outside preview")
			}
		}
		images := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "img" })
		if len(images) != 1 || !hasChatPolishAttribute(images[0], "hidden") {
			t.Fatal("broken image is visible")
		}
		if strings.Count(markup, `data-chatmap-action="send"`) != 1 {
			t.Fatal("multiple primary actions")
		}
	})
}

func TestChatPolish_F_SavedPanelAndEditors(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		markup := chatPolishMarkup(t, Build(m), width, theme)
		panels := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chat-layer") == "saved" })
		if len(panels) != 1 || chatPolishAncestor(panels[0], "chat-rail") {
			t.Fatal("saved panel is trapped inside rail")
		}
		row := SavedMessageRow{Author: "Alex", Body: "Draft", Availability: "readable", PostID: "private-post"}
		list := chatPolishMarkup(t, RenderSavedMessages(SavedMessagesView{Locale: m.Locale, Rows: []SavedMessageRow{row}}), width, theme)
		if len(chatPolishNodes(t, list, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "tab" })) != 3 {
			t.Fatal("tabs missing")
		}
		// CHATSAVE-002 changed this assertion: the Note and Remind me disclosure rows
		// under every item are gone; an item has four icon buttons in one bar instead.
		if len(chatPolishNodes(t, list, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-disclosure-button") })) != 0 {
			t.Fatal("the Saved panel still draws disclosure rows")
		}
		if len(chatPolishNodes(t, list, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chatsave-act") })) != 4 {
			t.Fatal("an item does not have its four icon buttons")
		}
	})
}

func TestChatPolish_G_QuietSettingsAvailability(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.Callbacks.SavePreferences = nil
		markup := chatPolishMarkup(t, html.Div(html.Props{}, railPreferencesForPolish(m), renderingPersonalView(renderingPersonalProps{m.Locale, "room"}, renderingPersonalState{Unavailable: true}, ui.Handler{}, ui.Handler{}, nil)), width, theme)
		buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-disclosure-button") })
		// CHATBUG-026: an unavailable language service draws no row at all (it used
		// to draw a greyed one); the quiet-hours row keeps its explained disabled state.
		// CHATUX-002 changed these assertions: the explained disabled state is now the
		// switch inside the Quiet hours section (it used to be the disclosure button of
		// the footer row), and nothing else in the panel is a disclosure.
		if len(buttons) != 0 {
			t.Fatal("a row for the unavailable language service is drawn")
		}
		switches := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "quiet-hours" })
		if len(switches) != 1 || !hasChatPolishAttribute(switches[0], "disabled") || chatPolishAttr(switches[0], "title") != chatPolishUnavailable(m.Locale) || !strings.Contains(markup, chatPolishUnavailable(m.Locale)) {
			t.Fatal("missing quiet unavailable state")
		}
		for _, bad := range []string{RenderingText(m.Locale, "error"), RenderingText(m.Locale, "languages_error")} {
			if strings.Contains(markup, bad) {
				t.Fatal("error announced before action")
			}
		}
		chat4Require(t, markup, `data-prefs-section="quiet-hours"`)
		available := chatPolishMarkup(t, renderingPersonalView(renderingPersonalProps{m.Locale, "room"}, renderingPersonalState{}, ui.Handler{}, ui.Handler{}, nil), width, theme)
		chat4Require(t, available, `data-prefs-section="reading-languages"`)
		chat4Require(t, Stylesheet, RenderingStyles, `.chatrender-check{display:flex;align-items:center;gap:8px`)
	})
}

func railPreferencesForPolish(m Model) ui.Node { return railPreferences(m, handlers{}) }

func TestChatPolish_H_SearchFailurePreservesResults(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		v := ChatSearchView{Query: "policy", Error: "unavailable"}
		markup := chatPolishMarkup(t, RenderChatSearch(m.Locale, v), width, theme)
		if strings.Contains(markup, chatsearchText(m.Locale, "filters")) || strings.Contains(markup, chatsearchText(m.Locale, "keyword")) || strings.Contains(markup, `data-chatsearch-action="retry"`) {
			t.Fatal("empty headings/retry dead end")
		}
		v.Response = chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, Text: "previous readable result"}}}}}
		markup = chatPolishMarkup(t, RenderChatSearch(m.Locale, v), width, theme)
		chat4Require(t, markup, "previous readable result", chatsearchText(m.Locale, "unavailable"))
	})
}

func TestChatPolish_I_ChannelDetailsIdentityAndPressedState(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.ShowDetails = true
		m.ResolvedPersonaMentions[0].DataClasses = []string{"POLICY_DOCUMENT", "UNKNOWN_INTERNAL_KEY"}
		markup := chatPolishMarkup(t, Build(m), width, theme)
		if strings.Contains(markup, ">POLICY_DOCUMENT<") || strings.Contains(markup, ">UNKNOWN_INTERNAL_KEY<") {
			t.Fatal("data key leaked")
		}
		chat4Require(t, markup, chatPolishPolicyScope(m.Locale), "agent-icon")
		pressed := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return chatPolishAttr(n, "aria-pressed") == "true" && chatPolishAncestor(n, "conversation-actions")
		})
		if len(pressed) != 1 || chatPolishAttr(pressed[0], "data-action") != "details" {
			t.Fatal("multiple header pressed states")
		}
	})
}

func TestChatPolish_J_TodoOptionsDisclosure(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		markup := chatPolishMarkup(t, channelTodoSection(m, handlers{}), width, theme)
		buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return chatPolishHasClass(n, "chat-disclosure-button") && chatPolishAncestor(n, "channel-todo-options")
		})
		if len(buttons) != 1 || chatPolishAttr(buttons[0], "aria-expanded") != "false" {
			t.Fatal("todo options not styled/closed")
		}
	})
}

func TestChatPolish_K_StoredAgentIconEverywhere(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		value := agenticon.Generate(agenticon.Input{Name: "Policy Helper"})
		m.ResolvedPersonaMentions[0].Icon = value
		m.Conversations[0].Kind = DirectMessage
		m.Conversations[0].Agent = true
		m.Conversations[0].AgentID = "policy"
		m.Conversations[0].Icon = value
		actor := &PersonaActor{Trusted: true, PersonaID: m.ResolvedPersonaMentions[0].Reference.ID, AgentID: "policy"}
		want := renderNode(t, agenticon.Node(value))
		for _, node := range []ui.Node{integrate1MessageAvatar(m, Message{Author: "Policy Helper", PersonaActor: actor}, "avatar"), personaMentionAvatar(m.ResolvedPersonaMentions[0]), kindGlyph(m, m.selected(), "Agent")} {
			markup := chatPolishMarkup(t, node, width, theme)
			if !strings.Contains(markup, want) {
				t.Fatal("stored icon not used")
			}
		}
	})
}

func TestChatPolish_L_AnswerFooterAndSourceNote(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		answer := renderPersonaPrivateAnswer(m, localUI{}, EphemeralMessage{ID: "answer", ThreadID: "question", Body: "Answer", CreatedAt: time.Now()}, PersonaProgressProjection{AgentName: "Policy Helper", PrivateReplyHref: "/workspace/app/chat#agent-dm"})
		sources := renderAgentReplySources(m, agentReplyEnvelope{Sources: []agentReplySource{{Title: "PTO policy"}}})
		markup := chatPolishMarkup(t, html.Div(html.Props{}, append([]ui.Node{answer}, sources...)...), width, theme)
		footer := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-reply-footer") })
		if len(footer) != 1 {
			t.Fatal("missing spaced footer")
		}
		notes := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-reply-source-unavailable") })
		if len(notes) != 1 || !chatPolishAncestor(notes[0], "agent-reply-source") {
			t.Fatal("access note missing")
		}
		chat4Require(t, ChatPolishStyles, "gap:8px 12px", ".agent-reply-source-unavailable{grid-column:2;display:block")
	})
}

func TestChatPolish_M_MentionSuppressesPlaceholder(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		markup := chatPolishMarkup(t, composer(m, handlers{composerAgentName: "Policy Helper"}), width, theme)
		fields := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "chat-composer" })
		if len(fields) != 1 || chatPolishAttr(fields[0], "placeholder") != "" {
			t.Fatal("placeholder overlaps chip")
		}
	})
}

func TestChatPolish_N_TabOrderAndLiveSend(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.Draft = "Send these words"
		m.Callbacks.SendMessage = func(string, string) {}
		markup := chatPolishMarkup(t, composer(m, handlers{}), width, theme)
		buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && !chatPolishAncestor(n, "chat-settings-layer") && !chatPolishAncestor(n, "chatmap-sheet") && !chatPolishAncestor(n, "chattone-options") && !chatPolishAncestor(n, "emoji-picker") && !chatPolishAncestor(n, "giphy-picker") && !chatPolishAncestor(n, "chatvoice-panel")
		})
		if len(buttons) == 0 || !chatPolishHasClass(buttons[len(buttons)-1], "send-button") || hasChatPolishAttribute(buttons[len(buttons)-1], "disabled") {
			t.Fatal("Send is absent from end of tab order")
		}
		for _, n := range buttons {
			// CHATUX-004: the location and voice buttons are now only the openers
			// the Add menu presses; the menu items are the tab stops, so these two
			// leave the tab order and nothing else may.
			opener := chatPolishAttr(n, "data-chatmap-action") == "toggle" || chatPolishAttr(n, "data-chatvoice-action") == "toggle"
			if chatPolishAttr(n, "tabindex") == "-1" && !opener {
				t.Fatal("tool removed from tab order")
			}
		}
		if !chatPolishSendReady(true, "live text") || chatPolishSendReady(false, "live text") || chatPolishSendReady(true, " \n ") {
			t.Fatal("send ignores draft/capability")
		}
	})
}

func TestChatPolish_O_SidebarAvatarAlignment(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		c := Conversation{ID: "agent-dm", Name: "Policy Helper", Kind: DirectMessage, Agent: true, Icon: agenticon.Generate(agenticon.Input{Name: "Policy Helper"})}
		markup := chatPolishMarkup(t, kindGlyph(m, c, "Agent"), width, theme)
		chat4Require(t, markup, `class="avatar tiny agent-dm-avatar"`, `class="agent-icon"`)
		chat4Require(t, ChatPolishStyles, ".chat-row .avatar.tiny{inline-size:24px;block-size:24px}", ".chat-row .agent-dm-avatar{align-self:center}")
	})
}

func TestChatPolish_A_LayerEscapeOpeningOrder(t *testing.T) {
	layers := []chatPolishLayer{{"saved", 4, true}, {"menu", 2, true}, {"location", 8, false}, {"writing-style", 5, true}}
	if top := chatPolishTopLayer(layers); top != 3 {
		t.Fatal(top)
	}
	layers[3].visible = false
	if top := chatPolishTopLayer(layers); top != 0 || !chatPolishManagedLayer(layers[top].kind) {
		t.Fatal("Escape did not return to Saved", top)
	}
	if chatPolishManagedLayer("menu") || chatPolishTopLayer(nil) != -1 {
		t.Fatal("state-owned menu intercepted")
	}
}

func TestChatPolish_C_TranscriptDisclosure(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		markup := chatPolishMarkup(t, RenderVoicePlayer(VoicePlayerProps{Locale: m.Locale, ID: "voice", Transcript: chat.VoiceTranscript{State: chat.TranscriptReady}}), width, theme)
		chat4Require(t, markup, `data-chatvoice-expand`, `data-chat-disclosure-toggle`, `aria-expanded="false"`)
	})
}

func TestChatPolish_H_SearchFailureKeepsConversation(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.Messages = []Message{{ID: "visible", Body: "Keep the visible conversation", Author: "Alex"}}
		markup := chatPolishMarkup(t, RenderChatSearch(m.Locale, ChatSearchView{Error: "unavailable", Conversation: &m}), width, theme)
		chat4Require(t, markup, "Keep the visible conversation", chatsearchText(m.Locale, "unavailable"))
		if strings.Contains(markup, "<h3") || strings.Contains(markup, `data-chatsearch-action="retry"`) {
			t.Fatal("fallback draws empty headings or retry controls")
		}
	})
}
