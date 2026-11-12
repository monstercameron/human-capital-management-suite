package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestIssue3680_ChatLoadingDrawsNoShellHeading pins the phone defect in
// #3680. Chat owns its heading (OwnsHeading), so the ready page draws no shell
// page-head ("Chat" and its subtitle). The loading shells (BuildLoading,
// BuildContentLoading, BuildFailure) used to draw one anyway, above the chat
// room, inside the full-bleed main region that has no side padding under 760px,
// so the heading sat flush at x=0. Every chat shell must match the ready page.
func TestIssue3680_ChatLoadingDrawsNoShellHeading(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			view := agentux024View(locale)
			ready := agentux024Render(t, Build(view))
			if strings.Contains(ready, `data-hcm-page="chat"`) || strings.Contains(ready, `id="page-title"`) {
				t.Fatal("the ready Chat page draws a shell heading; the comparison is invalid")
			}
			states := map[string]ui.Node{
				"loading":         BuildLoading(view),
				"content loading": BuildContentLoading(view),
				"failure":         BuildFailure(view, "The service did not answer."),
			}
			for name, node := range states {
				page := agentux024Render(t, node)
				if !strings.Contains(page, `class="chat-workspace`) {
					t.Fatalf("%s: the loading proxy has no Chat workspace", name)
				}
				if strings.Contains(page, `data-hcm-page="chat"`) {
					t.Errorf("%s: the shell draws a page-head for Chat above the chat room", name)
				}
				if strings.Contains(page, `id="page-title"`) {
					t.Errorf("%s: the shell draws the page title for Chat above the chat room", name)
				}
			}
		})
	}
}
