package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

// Issue #3615: every value-bearing control of the Board settings and New task
// forms must carry the value it was rendered with, so Cancel can restore it.
var projectFormControlTag = regexp.MustCompile(`<(input|select|textarea)\b[^>]*>`)

func projectFormControlsWithoutSavedValue(markup string) []string {
	missing := []string{}
	for _, tag := range projectFormControlTag.FindAllString(markup, -1) {
		if strings.Contains(tag, `type="hidden"`) || strings.Contains(tag, `type="submit"`) || strings.Contains(tag, `type="button"`) {
			continue
		}
		if !strings.Contains(tag, "data-saved-value=") && !strings.Contains(tag, "data-saved-checked=") {
			missing = append(missing, tag)
		}
	}
	return missing
}

func TestTodo_ISSUE3615_ProjectFormsRenderSavedValueForEveryControl(t *testing.T) {
	copy := projectPageCopy(LocaleContext{Resolved: "en-US"})
	board := projectui.Model{
		Mode: projectui.ViewBoard, Title: "Bögen — Ärger ✓", ViewID: "view-1", ViewRevision: 3,
		SwimlaneGrouping: "BOARD_SWIMLANE_GROUPING_PRIORITY",
		Columns: []projectui.Column{
			{ID: "todo", Label: "To do", Statuses: []projectui.Status{{ID: "todo", Label: "To do"}}},
			{ID: "doing", Label: "In progress", Statuses: []projectui.Status{{ID: "doing", Label: "In progress"}}},
		},
	}

	settings := renderProjectTest(t, projectBoardSettingsForm(copy, "p-7", board, true))
	if !strings.Contains(settings, `data-saved-value="Bögen — Ärger ✓"`) {
		t.Fatalf("board name must carry its non-ASCII saved value: %s", settings)
	}
	if !strings.Contains(settings, `data-saved-value="BOARD_SWIMLANE_GROUPING_PRIORITY"`) {
		t.Fatalf("grouping select must carry its saved option: %s", settings)
	}
	if !strings.Contains(settings, `data-saved-checked="true"`) || !strings.Contains(settings, `data-saved-checked="false"`) {
		t.Fatalf("column status checkboxes must carry their saved checked state: %s", settings)
	}
	if missing := projectFormControlsWithoutSavedValue(settings); len(missing) > 0 {
		t.Fatalf("board settings controls without a saved value: %v", missing)
	}

	create := renderProjectTest(t, projectTaskCreateForm(copy, "p-7", board, true))
	for _, want := range []string{
		`data-saved-value="todo"`,
		`data-saved-value="TASK_PRIORITY_NORMAL"`,
		`data-saved-value=""`,
	} {
		if !strings.Contains(create, want) {
			t.Fatalf("new task form must carry %s: %s", want, create)
		}
	}
	if missing := projectFormControlsWithoutSavedValue(create); len(missing) > 0 {
		t.Fatalf("new task controls without a saved value: %v", missing)
	}
}
