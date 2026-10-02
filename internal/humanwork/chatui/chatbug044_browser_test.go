package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The open mention menu rendered with the product's own catalog in the three
// languages: a list followed by a hint line, an agent row that is one grid, and no
// copy key anywhere.
func TestTodo_CHATBUG_044_Browser(t *testing.T) {
	badge := map[string]string{"en-US": "Agent", "de-DE": "Agent", "ar": "وكيل"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		ctx := productui.ResolveProductLocale(locale)
		icon := agenticon.Generate(agenticon.Input{Name: "Policy Helper", Description: "Answer policy questions"})
		agent := func(id, name, purpose string) chatui.ResolvedPersonaMention {
			return chatui.ResolvedPersonaMention{Icon: icon, Reference: chatui.ChatReference{Kind: "AGENT_MENTION", TenantID: "t", ID: id, Display: name, ConversationID: "general"}, Handle: id, Purpose: purpose}
		}
		m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", CurrentUser: "walt", CurrentTenantID: "t",
			Text:                    func(key string) string { return ctx.Text(key) },
			Members:                 []chatui.Member{{ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}},
			ResolvedPersonaMentions: []chatui.ResolvedPersonaMention{agent("policy-helper", "Policy Helper", "Answer policy questions"), agent("assistant", "Assistant", "Answers everyday questions")},
			PersonaLookup:           chatui.PersonaLookupReady, PersonaLookupConversationID: "general",
			Callbacks: chatui.Callbacks{SendMessageWithReferences: func(string, string, []chatui.ChatReference) {}},
		}
		menu := html.UnescapeString(chatui.MentionMenuMarkupForTest(t, m, false))
		if strings.Contains(menu, "⟦") {
			t.Errorf("%s: the mention menu prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(menu))
		}
		if strings.Count(menu, `class="mention-agent-option"`) != 2 || strings.Count(menu, `mention-agent-info`) != 2 {
			t.Errorf("%s: want two agent rows with an information button each: %s", locale, menu)
		}
		if !strings.Contains(menu, ">"+badge[locale]+"<") {
			t.Errorf("%s: the Agent badge %q is missing", locale, badge[locale])
		}
		list, hint := strings.Index(menu, `class="mention-list"`), strings.Index(menu, `class="mention-hint kbd-hint"`)
		if list < 0 || hint < list {
			t.Errorf("%s: the hint line does not follow the list", locale)
		}
		if last := strings.LastIndex(menu, `role="option"`); last > hint {
			t.Errorf("%s: an option comes after the hint line", locale)
		}
	}
}
