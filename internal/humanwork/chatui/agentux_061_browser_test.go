package chatui_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_AGENTUX_061_Browser draws an agent's conversation with a saved answer
// and its sources, in three languages and with the product's catalog: nothing
// is left as a placeholder once the activity has been read, the privacy of the
// conversation is said once, an agent row always carries its badge, a source
// shows its version as secondary text, the answer's clock and the message clock
// use one numeral system, and no key is printed.
func TestTodo_AGENTUX_061_Browser(t *testing.T) {
	clock := map[string]string{"en-US": "11:18 AM", "de-DE": "11:18", "ar": "١١:١٨"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			sent := time.Date(2026, 10, 1, 11, 18, 0, 0, time.Local)
			body := "Carry over up to 40 hours.\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](/workspace/app/docs?document=doc-64271829&version=docv-10264426#carryover) <!--chat.agent.source.readable:true-->"
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "policy", CurrentUser: "me", CurrentTenantID: "t",
				Text: func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{
					{ID: "policy", Name: "Policy Helper", Kind: chatui.DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true},
					{ID: "assistant", Name: "Assistant", Kind: chatui.DirectMessage, Agent: true, AgentID: "assistant", Joined: true},
					{ID: "benefits", Name: "Benefits Concierge", Kind: chatui.DirectMessage, Agent: true, AgentID: "benefits", Joined: true},
				},
				Messages: []chatui.Message{{ID: "answer", Revision: 1, AuthorID: "policy-helper", Author: "Policy Helper", Body: body, SentAt: sent, TimeLabel: clock[locale],
					PersonaActor: &chatui.PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}}},
				PersonaActivityReady: true,
			}
			page := agentux062Page(t, m)
			for _, leftover := range []string{"agent-reply-pending", "chatbug040-reserve", "chat-skeleton", "is-loading"} {
				if strings.Contains(page, leftover) {
					t.Errorf("a settled conversation keeps %s", leftover)
				}
			}
			if n := strings.Count(page, `agent-badge`); n < 3 {
				t.Errorf("only %d agent badges for three agent rows and a header", n)
			}
			visible := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(page, " ")
			if strings.Contains(visible, ctx.Text(chatui.KeyIntroDirect)) {
				t.Error("the conversation repeats the privacy sentence its header carries")
			}
			if !strings.Contains(page, `class="agent-reply-source-version"`) || !strings.Contains(page, "v1.0.0") {
				t.Errorf("the source's version is not secondary text: %s", regexp.MustCompile(`.{200}Paid time off.{200}`).FindString(page))
			}
			if locale == "ar" && regexp.MustCompile(`\b(?:AM|PM)\b`).MatchString(visible) {
				t.Errorf("an AM/PM time sits among Arabic text: %s", regexp.MustCompile(`.{20}(?:AM|PM).{20}`).FindString(visible))
			}
		})
	}
}
