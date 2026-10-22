package chatui_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

var chatux047Tags = regexp.MustCompile(`(?s)<[^>]*>`)

// TestTodo_CHATUX_047_Browser renders the first frame of a conversation whose
// names are not known yet, and the same frame once the list answer has named
// them, in the three languages the product ships. A name that is not known is
// the shared loading bar: no "...", no "Conversation" as a stand-in for a person
// in the sidebar, the phone header or the author line.
func TestTodo_CHATUX_047_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		ctx := productui.ResolveProductLocale(locale)
		word := ctx.Text(chatui.KeyConversation)
		icon := agenticon.Value{Glyph: "calendar", Shape: "circle", Foreground: "--hcm-color-info", Background: "--hcm-color-info-surface"}
		base := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) },
			CurrentUser: "walt", CurrentTenantID: "tenant", SelectedID: "dm-1", PersonaLookup: chatui.PersonaLookupIdle,
			Conversations: []chatui.Conversation{
				{ID: "dm-1", Name: "dm-1", Kind: chatui.DirectMessage, Joined: true},
				{ID: "dm-2", Name: "dm-2", Kind: chatui.DirectMessage, Joined: true},
				{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true},
			},
			Messages: []chatui.Message{{ID: "m1", AuthorID: "a-1", Author: "", Body: "hello", SentAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
				PersonaActor: &chatui.PersonaActor{PersonaID: "a-1", AgentID: "a-1", Trusted: true}}},
		}
		page := func(m chatui.Model) string {
			markup, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatalf("%s: %v", locale, err)
			}
			return markup
		}

		// Names unknown: the shared loading bar stands in, in the sidebar rows, the
		// header and the author line.
		unknown := page(base)
		for _, author := range regexp.MustCompile(`(?s)<strong[^>]*class="message-author[^"]*"[^>]*>.*?</strong>`).FindAllString(unknown, -1) {
			if text := strings.TrimSpace(chatux047Tags.ReplaceAllString(author, "")); text != "" {
				t.Errorf("%s: an author line prints %q while the name is unknown", locale, text)
			}
		}
		for _, row := range regexp.MustCompile(`(?s)<span class="chat-row-name[^"]*"[^>]*>.*?</span>`).FindAllString(unknown, -1) {
			if strings.Contains(row, ">"+word+"<") || strings.Contains(row, ">…<") {
				t.Errorf("%s: a sidebar row prints %q for an unknown name: %s", locale, word, row)
			}
		}
		for _, heading := range regexp.MustCompile(`(?s)<h1[^>]*>.*?</h1>`).FindAllString(unknown, -1) {
			if strings.Contains(heading, ">"+word+"<") || strings.Contains(heading, ">…<") {
				t.Errorf("%s: a heading prints %q for an unknown name: %s", locale, word, heading)
			}
		}
		if got := strings.Count(unknown, "chatux037-bar chatux047-name"); got < 3 {
			t.Errorf("%s: %d loading bars stand in for the unknown names, want the two rows, the header and the author line", locale, got)
		}

		// The list answer named everyone: the same frame reads every name and draws
		// the agent's own icon.
		named := base
		named.Conversations = []chatui.Conversation{
			{ID: "dm-1", Name: "Assistant", Kind: chatui.DirectMessage, Joined: true, Agent: true, AgentID: "a-1", Icon: icon},
			{ID: "dm-2", Name: "Loretta Haynes", Kind: chatui.DirectMessage, Joined: true},
			base.Conversations[2],
		}
		named.PeerIDs = map[string]string{"dm-1": "a-1", "dm-2": "loretta"}
		named.PhotoURLs = map[string]string{"loretta": "/photo/loretta.png"}
		named.Messages = []chatui.Message{{ID: "m1", AuthorID: "a-1", Author: "Assistant", Body: "hello", SentAt: base.Messages[0].SentAt,
			PersonaActor: &chatui.PersonaActor{PersonaID: "a-1", AgentID: "a-1", Trusted: true, Icon: icon}}}
		ready := page(named)
		for _, name := range []string{"Assistant", "Loretta Haynes"} {
			if !strings.Contains(ready, ">"+name+"<") {
				t.Errorf("%s: the named frame does not show %q", locale, name)
			}
		}
		if strings.Contains(ready, "chatux037-bar chatux047-name") || strings.Contains(ready, "agent-icon-pending") {
			t.Errorf("%s: the named frame still draws a placeholder", locale)
		}
	}
}
