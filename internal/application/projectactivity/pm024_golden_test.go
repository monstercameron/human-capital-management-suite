package projectactivity

import (
	"context"
	"testing"
)

func TestTodo_PM_024_Golden(t *testing.T) {
	got, err := NewPlainTextSanitizer().Sanitize(context.Background(), `<p><strong>Launch</strong> @owner</p><a href="javascript:alert(1)">unsafe</a><a href="https://example.test/guide">guide</a>`)
	if err != nil {
		t.Fatal(err)
	}
	want := `<p><strong>Launch</strong> @owner</p>unsafe<a href="https://example.test/guide">guide</a>`
	if got.HTML != want {
		t.Fatalf("sanitized golden=%q want=%q", got.HTML, want)
	}
	if len(got.MentionHandles) != 1 || got.MentionHandles[0] != "owner" {
		t.Fatalf("mention golden=%#v", got.MentionHandles)
	}
}
