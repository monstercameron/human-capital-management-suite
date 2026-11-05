package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXBLIND-071: the appearance editor's unsaved guard prompts with a leave
// question, not the inline preview status. The served shell reads
// data-unsaved-message when the admin leaves with a dirty draft.
func TestTodo_UXBLIND_071_AppearanceLeaveMessage(t *testing.T) {
	published := DefaultCustomerTheme()
	draft := published
	draft.Palette = "ocean"
	markup, err := ui.RenderToString(AppearancePage(uxblindVAppearanceProps(draft, published)))
	if err != nil {
		t.Fatal(err)
	}
	want := `data-unsaved-message="You have unsaved appearance changes. Leave without saving?"`
	if !strings.Contains(markup, want) {
		t.Fatalf("appearance guard message = %q, want the leave question", markup)
	}
}
