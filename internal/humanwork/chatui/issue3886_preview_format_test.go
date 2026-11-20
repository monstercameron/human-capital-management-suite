package chatui

import (
	"regexp"
	"strings"
	"testing"
)

var issue3886FormatButton = regexp.MustCompile(`<button[^>]*data-action="format"[^>]*>`)

// Issue 3886: the formatting buttons cannot act on a read-only preview, so
// while it is shown they are disabled and say why, in every language.
func TestIssue3886_FormatButtonsDisabledInPreview(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		m.Draft = "**A sample draft**"
		m.Callbacks.SendMessage = func(string, string) {}
		why := composerText(m, keyComposerFormatPreviewOff)
		if why == "" || why == keyComposerFormatPreviewOff {
			t.Fatalf("%s: no copy for the disabled formatting buttons", locale)
		}
		edit := issue3886FormatButton.FindAllString(renderNode(t, composer(m, handlers{})), -1)
		if len(edit) == 0 {
			t.Fatalf("%s: no formatting buttons while editing", locale)
		}
		for _, tag := range edit {
			if strings.Contains(tag, "disabled") {
				t.Errorf("%s: button disabled while editing: %s", locale, tag)
			}
		}
		for name, markup := range map[string]string{
			"channel": renderNode(t, composer(m, handlers{local: localUI{composerPreview: true}})),
			"thread":  issue3886Thread(t, m),
		} {
			tags := issue3886FormatButton.FindAllString(markup, -1)
			if len(tags) != len(edit) {
				t.Fatalf("%s %s: %d buttons in preview, %d editing", locale, name, len(tags), len(edit))
			}
			for _, tag := range tags {
				if !strings.Contains(tag, "disabled") || !strings.Contains(tag, `title="`) || !strings.Contains(tag, why) || !strings.Contains(tag, `aria-label="`) || strings.Count(tag, why) < 2 {
					t.Errorf("%s %s: preview button still active or unexplained: %s", locale, name, tag)
				}
			}
		}
	}
}

func issue3886Thread(t *testing.T, m Model) string {
	t.Helper()
	m.ShowThread, m.ThreadParentID = true, "root"
	m.ThreadDrafts = map[string]string{"root": m.Draft}
	m.Callbacks.ReplyInThread = func(string, string) {}
	return renderNode(t, threadPane(m, handlers{local: localUI{threadComposerPreview: true}}))
}
