package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

func TestHTMLRenderersPreserveGWCMarkupAndEscaping(t *testing.T) {
	tests := []struct {
		name string
		got  func() (string, error)
		want func() (string, error)
	}{
		{
			name: "filter panel",
			got: func() (string, error) {
				return RenderModAdminPanelHTML(ModAdminProps{Model: Model{Locale: "en-US"}, CanManage: true, Definitions: []chatfilter.Definition{{ID: "x", Name: "<script>bad()</script>", Kind: "words", Action: "block"}}})
			},
			want: func() (string, error) {
				return ui.RenderToString(ModAdminPanel(ModAdminProps{Model: Model{Locale: "en-US"}, CanManage: true, Definitions: []chatfilter.Definition{{ID: "x", Name: "<script>bad()</script>", Kind: "words", Action: "block"}}}))
			},
		},
		{
			name: "moderation page",
			got: func() (string, error) {
				return RenderModerationPageHTML(ModerationPageModel{Locale: "en-US", State: StateReady, Notices: []chat.ModerationNotice{{Outcome: "remove", Reason: "<script>bad()</script>"}}})
			},
			want: func() (string, error) {
				return ui.RenderToString(ModerationPage(ModerationPageModel{Locale: "en-US", State: StateReady, Notices: []chat.ModerationNotice{{Outcome: "remove", Reason: "<script>bad()</script>"}}}))
			},
		},
		{
			name: "moderation dialog",
			got: func() (string, error) {
				return RenderModerationDialogHTML(ModerationDialogModel{Model: Model{Locale: "en-US", SelectedID: "room"}, Report: true, Target: ModerationTargetView{Body: "<script>bad()</script>"}, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{"post"}}})
			},
			want: func() (string, error) {
				return ui.RenderToString(ModerationDialog(ModerationDialogModel{Model: Model{Locale: "en-US", SelectedID: "room"}, Report: true, Target: ModerationTargetView{Body: "<script>bad()</script>"}, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{"post"}}}))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.got()
			if err != nil {
				t.Fatal(err)
			}
			want, err := tt.want()
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatal("HTML helper changed renderer output")
			}
			if strings.Contains(got, "<script>") || !strings.Contains(got, "&lt;script&gt;") {
				t.Fatal("untrusted text was not escaped by GWC")
			}
		})
	}
}
