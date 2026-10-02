package chatui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	xhtml "golang.org/x/net/html"
)

func TestIntegrate1ComposerSidebarAndAgentBadge_Browser(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		for _, theme := range []string{"light", "dark"} {
			t.Run(theme, func(t *testing.T) {
				m := chat4Fixture(locale, "sent", true)
				m.Conversations[0].Icon = AgentIconFixture(agenticon.Input{Name: "Policy Helper"})
				voice := RenderVoiceComposer(VoiceComposerProps{Locale: locale, ConversationID: m.SelectedID, TenantID: "tenant", Enabled: true})
				markup := renderNode(t, html.Div(html.Props{Dir: direction(locale), Data: map[string]string{"theme": theme, "viewport": fmt.Sprint(width)}}, voice, chattoneToolbar(m, "chat-composer", false), chatsaveSidebar(m), railRow(m, m.Conversations[0])))
				for _, want := range []string{`class="tool-button format-button"`, `title="` + VoiceCopy(locale, "record") + `"`, `aria-label="` + VoiceCopy(locale, "record") + `"`, `aria-haspopup="dialog"`, `data-chat-layer="voice"`, `data-chatvoice-action="toggle"`, `class="chat-row chatsave-sidebar-row"`, `data-saved-count="true"`, `class="chat-row-name"`, `class="agent-icon"`, `class="agent-badge agent-badge-label"`} {
					if !strings.Contains(markup, want) {
						t.Fatalf("missing %s", want)
					}
				}
				if strings.Contains(markup, "<style") || strings.Contains(markup, ` style="`) || strings.Contains(markup, ">"+VoiceCopy(locale, "record")+"</summary>") || !strings.Contains(markup, `data-chat-layer="writing-style"`) {
					t.Fatal("CSP offender or disclosure toolbar", markup)
				}
				for _, want := range []string{`.chat-rail-row .chat-row .agent-badge{position:static;flex:none`, `.chat-row-name{flex:1 1 0%}`, `.chatsave-sidebar[open]>summary`, `.chatsave-sidebar>summary:hover`, `.chatsave-sidebar>summary:focus-visible`, `.chatvoice-panel{z-index:1500;overflow:auto`} {
					if !strings.Contains(Stylesheet, want) {
						t.Fatalf("missing responsive token style %s", want)
					}
				}
				name := strings.Index(markup, `title="Policy Helper"`)
				badge := strings.LastIndex(markup, `class="agent-badge agent-badge-label"`)
				if name < 0 || badge < name {
					t.Fatal("agent badge precedes name")
				}
			})
		}
	})
}

func TestIntegrate1MenusAndWidgetNotice(t *testing.T) {
	m := chat4Fixture("de-DE", "sent", false)
	m.IsTenantAdmin = true
	m.MenuID = "question"
	msg := m.Messages[0]
	// CHATBUG-030 changed this line: the moderation entries are for somebody
	// else's message; on the viewer's own the one destructive command is Delete.
	msg.AuthorID, msg.Author = "bob", "Bob"
	main := renderNode(t, message(m, handlers{}, msg, false))
	m.MenuID = "thread:" + msg.ID
	thread := renderNode(t, html.Div(html.Props{}, threadMessageMenu(m, msg)...))
	for _, markup := range []string{main, thread} {
		if strings.Count(markup, `role="menuitem"`) < 1 || !strings.Contains(markup, `data-saved-action="save"`) || !strings.Contains(markup, `/api/chat/moderation/page?`) {
			t.Fatal("missing canonical menu actions", markup)
		}
	}
	m.ChannelWidgetsError = "unavailable"
	m.Callbacks.RetryChannelWidgets = func() {}
	markup := renderNode(t, integrate1InlineWidgets(m))
	for _, want := range []string{`class="channel-tray"`, `class="widget-inline-notice"`, `role="alert"`, `data-action="widget-retry"`, m.t(KeyWidgetError)} {
		if !strings.Contains(markup, want) {
			t.Fatal(want, markup)
		}
	}
	m.ChannelWidgetsError = ""
	if strings.Contains(renderNode(t, integrate1InlineWidgets(m)), "widget-inline-notice") {
		t.Fatal("successful tray keeps error")
	}
}

func TestIntegrate1IconAvatars(t *testing.T) {
	m := chat4Fixture("ar", "sent", true)
	value := AgentIconFixture(agenticon.Input{Name: "Policy Helper"})
	m.Conversations[0].Icon = value
	expected := renderNode(t, agenticon.Node(value))
	for _, node := range []ui.Node{kindGlyph(m, m.selected(), ""), conversationAvatar(m, m.selected()), conversationHeaderAvatar(m, m.selected()), personaMentionAvatar(ResolvedPersonaMention{Icon: value})} {
		markup := renderNode(t, node)
		if !strings.Contains(markup, expected) || strings.Contains(markup, "<style") || strings.Contains(markup, `style="`) {
			t.Fatal("canonical icon omitted or violates CSP", markup)
		}
	}
	other := AgentIconFixture(agenticon.Input{Name: "Birthday Helper"})
	m.ResolvedPersonaMentions[0].Icon = other
	m.ResolvedPersonaMentions[0].Reference.Display = "Policy Helper"
	actor := PersonaActor{Trusted: true, PersonaID: "policy-helper", AgentID: "policy-helper", Icon: value}
	message := Message{Author: "Policy Helper", PersonaActor: &actor}
	for _, node := range []ui.Node{integrate1MessageAvatar(m, message, "avatar"), renderAgentReplyIdentity(m, "Policy Helper", value)} {
		if markup := renderNode(t, node); !strings.Contains(markup, expected) {
			t.Fatal("display-name collision chose another agent's icon", markup)
		}
	}
	actor.Icon = agenticon.Value{}
	if markup := renderNode(t, integrate1MessageAvatar(m, message, "avatar")); !strings.Contains(markup, expected) {
		t.Fatal("legacy direct actor omitted its admitted conversation icon", markup)
	}
	actor.AgentID = "another-agent"
	if markup := renderNode(t, integrate1MessageAvatar(m, message, "avatar")); strings.Contains(markup, expected) {
		t.Fatal("different actor borrowed the selected agent's icon", markup)
	}
	m.PersonaPostActors = map[string]PersonaPostActor{"answer": {Actor: PersonaActor{Trusted: true, PersonaID: "policy-helper", AgentID: "policy-helper", Icon: value}, Display: "Policy Helper"}}
	if markup := renderNode(t, renderPersonaPrivateAnswer(m, localUI{}, EphemeralMessage{ID: "answer", ThreadID: "question", Body: "A private answer."}, PersonaProgressProjection{})); !strings.Contains(markup, expected) {
		t.Fatal("private answer ignored its admitted actor icon", markup)
	}
}

func TestIntegrate1ModerationShellStyles(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, node := range []ui.Node{ModerationPage(ModerationPageModel{Locale: locale, State: StateReady}), ModerationDialog(ModerationDialogModel{Model: Model{Locale: locale, SelectedID: "room"}, Action: "remove"})} {
			markup := renderNode(t, node)
			if strings.Contains(markup, "<style") || strings.Contains(markup, ` style="`) {
				t.Fatal("moderation bypassed admitted shell CSS", markup)
			}
		}
	}
	if !strings.Contains(Stylesheet, ChatremoveStyles) {
		t.Fatal("moderation shell styles not composed")
	}
}

func TestIntegrate1WaveStylesReachAdmittedShell(t *testing.T) {
	for _, css := range []string{ChatVoiceStyles, ChatremoveStyles, ChatsaveStyles, ChattoneStyles, ChatSearchStyles, ChannelStatusStyles, ChatgateStyles, ChatmapStyles} {
		if !strings.Contains(Stylesheet, css) {
			t.Fatal("wave component depends on a refused inline stylesheet")
		}
	}
}

// Hidden panels and the bodies of closed disclosures are not idle focus targets.
func integrate1VisibleControls(markup string) int {
	doc, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		return 0
	}
	count := 0
	var visit func(*xhtml.Node)
	visit = func(node *xhtml.Node) {
		hidden, closed := false, false
		for _, a := range node.Attr {
			if a.Key == "hidden" || (a.Key == "aria-hidden" && a.Val == "true") {
				hidden = true
			}
		}
		if hidden {
			return
		}
		if node.Type == xhtml.ElementNode {
			switch node.Data {
			case "button", "input", "textarea", "select":
				count++
			}
			for _, a := range node.Attr {
				if a.Key == "tabindex" && a.Val == "0" {
					count++
				}
			}
			if node.Data == "details" {
				closed = true
				for _, a := range node.Attr {
					if a.Key == "open" {
						closed = false
					}
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if !closed || c.Data == "summary" {
				visit(c)
			}
		}
	}
	visit(doc)
	return count
}
