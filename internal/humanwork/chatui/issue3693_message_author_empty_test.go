package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue #3693: Message the author with an empty note. The note field is
// required, so the browser's own validation stopped the submit before the
// client could show its inline "note required" alert. The form must let the
// client's check run, so the person sees the error in the dialog.
func TestIssue3693MessageAuthorFormLetsClientReportEmptyNote(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		model := chatui.ModerationDialogModel{Action: "message", CaseID: "filter:7"}
		model.Model = chatui.Model{Locale: locale}
		model.Selection = chat.RemovalSelection{ConversationID: "general", PostIDs: []string{"p1"}}
		model.Target = chatui.ModerationTargetView{AuthorID: "jake-internal", HomeTenantID: "t", AuthorName: "Jake Sullivan", Body: "the reported words"}
		rendered, err := ui.RenderToString(chatui.ModerationDialog(model))
		if err != nil {
			t.Fatal(err)
		}
		rendered = html.UnescapeString(rendered)
		form := regexp.MustCompile(`(?s)<form[^>]*data-chatremove="resolve"[^>]*>`).FindString(rendered)
		if form == "" {
			t.Fatalf("%s: the message dialog has no resolve form", locale)
		}
		if !strings.Contains(form, "novalidate") {
			t.Errorf("%s: the form %s lets the browser block an empty note before the inline error can show", locale, form)
		}
		if !strings.Contains(rendered, `id="chatremove-note"`) || !regexp.MustCompile(`<textarea[^>]*required`).MatchString(rendered) {
			t.Errorf("%s: the note field must stay marked required for assistive technology", locale)
		}
	}
}
