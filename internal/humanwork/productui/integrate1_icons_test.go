package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

func TestIntegrate1AgentSetupIcons_Browser(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		icon := agenticon.Generate(agenticon.Input{Name: "Policy Helper"})
		for _, width := range []int{1440, 800, 390, 320} {
			for _, theme := range []string{"light", "dark"} {
				t.Logf("locale=%s width=%d theme=%s", language, width, theme)
				persona := PersonaAdminPersona{ID: "policy", Name: "Policy Helper", Icon: icon, IconRevision: 7}
				markup := personaAdminRender(t, integrate1AgentIconControls(locale, persona))
				for _, action := range []string{"regenerate", "shuffle", "reset", "apply", "undo", "cancel"} {
					if !strings.Contains(markup, `data-agent-icon-action="`+action+`"`) || !strings.Contains(markup, Integrate1IconText(locale, action)) {
						t.Fatal("missing localized control", action, markup)
					}
				}
				if !strings.Contains(markup, `data-agent-icon-revision="7"`) || !strings.Contains(markup, `role="status"`) || !strings.Contains(markup, `role="group"`) || !strings.Contains(markup, `hidden`) {
					t.Fatal("preview/CAS contract missing", markup)
				}
				if strings.Contains(markup, `style="`) || strings.Contains(markup, "<style") {
					t.Fatal("inline style")
				}
				choice := personaAdminRender(t, agentChoice(locale, "choice", "policy", "Policy Helper", "Read policies", true, icon))
				if !strings.Contains(choice, `class="agent-icon"`) || !strings.Contains(choice, `M3 4h5q4 0 4 3`) {
					t.Fatal("choice lost icon", choice)
				}
				line := personaAdminRender(t, agentTaskDetailAgentLine(locale, AgentTask{Icon: icon, AnsweringAgentDisplayName: "Policy Helper", State: AgentTaskCompleted}))
				if !strings.Contains(line, `class="agent-icon"`) || !strings.Contains(line, "Policy Helper") {
					t.Fatal("task identity missing", line)
				}
			}
		}
	}
}
