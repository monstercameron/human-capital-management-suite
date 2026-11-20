package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestIssue3896_RowTokenMatchesTheOpenControl(t *testing.T) {
	view := ChatSearchView{Query: "png", Response: chatsearch.Response{Groups: issue3900Rows(3)}}
	got := renderNode(t, RenderChatSearch("en-US", view))
	for _, row := range view.Response.Groups[0].Rows {
		encoded, kind, _ := strings.Cut(Issue3896RowToken(row), "\x00")
		if !strings.Contains(got, `data-target="`+encoded+`"`) || !strings.Contains(got, `data-kind="`+kind+`"`) {
			t.Fatalf("token %q does not match the rendered control: %s", encoded, got)
		}
	}
}
