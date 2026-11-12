package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestProjectPreviewEmbedSettledStatesDoNotLoadForever3750 pins that a missing,
// archived or failed project read renders a settled locked card with its own
// embed-state, never the loading skeleton.
func TestProjectPreviewEmbedSettledStatesDoNotLoadForever3750(t *testing.T) {
	origin := "https://hcm.example"
	body := "https://hcm.example/workspace/app/project?project=proj-7&task=task-19"
	for _, state := range []string{"missing", "archived", "failed"} {
		m := Model{EmbedOrigin: origin, ProjectTaskPreviews: map[string]ProjectTaskPreview{
			ProjectTaskPreviewKey("proj-7", "task-19"): {ProjectID: "proj-7", TaskID: "task-19", State: state},
		}}
		markup, err := ui.RenderToString(ui.Fragment(projectPreviewEmbeds(m, body)...))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, `data-embed-state="`+state+`"`) || !strings.Contains(markup, "is-locked") {
			t.Errorf("state %s: want a settled locked card, got:\n%s", state, markup)
		}
		if strings.Contains(markup, "is-loading") {
			t.Errorf("state %s: settled answer still renders the loading skeleton:\n%s", state, markup)
		}
	}
}
