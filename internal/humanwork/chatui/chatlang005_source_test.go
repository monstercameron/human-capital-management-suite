package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATLANG_005_SourceLanguage: a cited document in another language
// than the reader's is named with its language, in the reader's words; one in the
// reader's language, or with no language on its line, is shown as before.
func TestTodo_CHATLANG_005_SourceLanguage(t *testing.T) {
	body := "Employees may carry over 40 hours.\n\nSources\n" +
		"- [Paid time off policy](/workspace/app/docs?document=d1&version=v1) <!--chat.agent.source.readable:true--> <!--chat.agent.source.language:en-->\n" +
		"- [Handbuch](/workspace/app/docs?document=d2&version=v1) <!--chat.agent.source.readable:true--> <!--chat.agent.source.language:de-->\n" +
		"- Locked policy <!--chat.agent.source.readable:false--> <!--chat.agent.source.language:fr-->\n" +
		"- [Plain](/workspace/app/docs?document=d3&version=v1) <!--chat.agent.source.readable:true-->"
	envelope := parseAgentReplyEnvelope(body)
	if len(envelope.Sources) != 4 {
		t.Fatalf("sources: %+v", envelope.Sources)
	}
	for i, want := range []string{"en", "de", "fr", ""} {
		if envelope.Sources[i].Language != want {
			t.Errorf("source %d language %q, want %q", i, envelope.Sources[i].Language, want)
		}
	}
	if strings.Contains(envelope.Sources[2].Title, "<!--") || strings.Contains(envelope.Sources[0].Title, "<!--") {
		t.Fatalf("a marker leaked into a title: %+v", envelope.Sources)
	}
	if envelope.Sources[0].Href == "" || envelope.Sources[2].Readable || envelope.Sources[2].Href != "" {
		t.Fatalf("the readable marker was lost: %+v", envelope.Sources)
	}
	render := func(locale string) string {
		var out []string
		for _, node := range renderAgentReplySources(Model{Locale: locale}, envelope) {
			out = append(out, renderNode(t, ui.Node(node)))
		}
		return strings.Join(out, "")
	}
	english := render("en-US")
	if !strings.Contains(english, "Paid time off policy") || strings.Contains(english, ", in English") || !strings.Contains(english, ", in German") || !strings.Contains(english, ", in French") {
		t.Fatalf("English reader: %s", english)
	}
	german := render("de-DE")
	if !strings.Contains(german, ", auf Englisch") || strings.Contains(german, ", auf Deutsch") {
		t.Fatalf("German reader: %s", german)
	}
	arabic := render("ar")
	if !strings.Contains(arabic, "، باللغة الإنجليزية") || !strings.Contains(arabic, "، باللغة الألمانية") {
		t.Fatalf("Arabic reader: %s", arabic)
	}
}
