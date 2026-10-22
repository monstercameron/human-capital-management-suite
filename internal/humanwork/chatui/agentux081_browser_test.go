package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_AGENTUX_081_Browser pins FIX 1 at page level: on the composed chat
// page an agent's name routes to the agent profile while a person's name keeps
// the person panel. Opening the dialog and asking from it are live browser
// behaviors specified in tools/uxqual/browser/agentux081_agent_surfaces.spec.mjs,
// which needs a running server and browser and is not executed here.
func TestTodo_AGENTUX_081_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			persona := chatui.ResolvedPersonaMention{
				Reference: chatui.ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "agent-helper", Display: "Helper", ConversationID: "general"},
				Handle:    "helper", Purpose: "Answer tenant-wide policy questions.", Owner: "Walt Brennan", Version: "6",
				DataClasses: []string{"policy"}, CannotDo: []string{"record"},
				Skills: []chatui.PersonaMentionSkill{{Name: "Search", Tier: "T0"}},
			}
			m := chatui.Model{State: chatui.StateReady, SelectedID: "general", CurrentTenantID: "tenant", CurrentUser: "alice", Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
				Members:       []chatui.Member{{ID: "alice", Name: "Alice"}},
				Messages: []chatui.Message{
					{ID: "m1", Author: "Helper", AuthorID: "agent-helper", Body: "Here is the policy."},
					{ID: "m2", Author: "Alice", AuthorID: "alice", Body: "Thanks!"},
				},
				ResolvedPersonaMentions: []chatui.ResolvedPersonaMention{persona},
				Callbacks:               chatui.Callbacks{SendMessage: func(string, string) {}, OpenPerson: func(string) {}},
			}
			page, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			agent := page[strings.Index(page, `data-message-id="m1"`):strings.Index(page, `data-message-id="m2"`)]
			if !strings.Contains(agent, `data-action="agent-profile-open"`) || strings.Contains(agent, `data-action="open-person"`) {
				t.Fatalf("composed page: agent name does not open the agent profile: %s", agent)
			}
			person := page[strings.Index(page, `data-message-id="m2"`):]
			if !strings.Contains(person, `data-action="open-person"`) {
				t.Fatalf("composed page: person name lost the person panel: %s", person)
			}
		})
	}
}
