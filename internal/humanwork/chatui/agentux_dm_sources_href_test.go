package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// A Sources line becomes a link only when it is a relative address of the
// Documents hub on the page's own origin. Every address here would take the
// reader somewhere else, or is read differently by a browser than by Go.
func TestTodo_AGENTUX_032_Security(t *testing.T) {
	hostile := []string{
		// Another host, with the path the check used to look at alone.
		"https://evil.example/workspace/app/docs?document=pto",
		"http://evil.example/workspace/app/docs?document=pto",
		"HTTPS://evil.example/workspace/app/docs?document=pto",
		// Scheme-relative: the browser keeps the page's scheme and goes to the host.
		"//evil.example/workspace/app/docs?document=pto",
		"///evil.example/workspace/app/docs?document=pto",
		// Backslashes, which browsers read as slashes.
		`/\evil.example/workspace/app/docs?document=pto`,
		`\\evil.example\workspace\app\docs?document=pto`,
		`/workspace/app/docs\..\..\evil?document=pto`,
		`/workspace/app/docs?document=pto\`,
		// Encoded separators and traversal in the path.
		"/%2Fevil.example/workspace/app/docs?document=pto",
		"%2F%2Fevil.example/workspace/app/docs?document=pto",
		"/workspace/app/docs%2F..%2F..%2Fadmin?document=pto",
		"/workspace/app/docs/../../admin?document=pto",
		"/workspace/app/../app/docs?document=pto",
		"/workspace/app/docs%3Fdocument=pto",
		// Other schemes.
		"javascript:alert(1)",
		"javascript:/workspace/app/docs?document=pto",
		"JaVaScRiPt:alert(1)//workspace/app/docs?document=pto",
		"data:text/html,/workspace/app/docs?document=pto",
		"vbscript:msgbox(1)",
		"ftp://files.example/workspace/app/docs?document=pto",
		// Credentials, and characters a browser strips before it reads the address.
		"https://user:pw@evil.example/workspace/app/docs?document=pto",
		"/\t/evil.example/workspace/app/docs?document=pto",
		"/workspace/app/docs?document=pto\n",
		" /workspace/app/docs?document=pto",
		"/workspace/app/docs?document=pto x",
		"/workspace/app/docs?document=pto\x00",
		// The right place with no usable document.
		"/workspace/app/docs",
		"/workspace/app/docs?",
		"/workspace/app/docs?document=",
		"/workspace/app/docs?version=3",
		"/workspace/app/docs?document=../../secret",
		"/workspace/app/docs?document=%2F%2Fevil.example",
		"/workspace/app/docs?document=a%00b",
		"/workspace/app/docs?document=pto;x=%zz",
		// Another page of the product.
		"/workspace/app/people?document=pto",
		"workspace/app/docs?document=pto",
		"",
	}
	for _, href := range hostile {
		if validAgentDocumentHref(href) {
			t.Errorf("%q passes as a document address", href)
		}
		envelope := parseAgentReplyEnvelope("Answer.\n\nSources\n- [Paid time off policy](" + href + ")")
		for _, source := range envelope.Sources {
			if source.Href != "" || source.Readable {
				t.Errorf("%q became a source link: %+v", href, source)
			}
		}
		message := EphemeralMessage{ID: "answer", ThreadID: "question", Body: "Answer.\n\nSources\n- [Paid time off policy](" + href + ")", OnlyVisibleToYou: true, CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
		markup, err := ui.RenderToString(html.Div(html.Props{}, renderPersonaPrivateAnswer(Model{Locale: "en-US"}, localUI{}, message, PersonaProgressProjection{AgentName: "Policy Helper"})))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, "agent-reply-source-link") || strings.Contains(markup, "evil.example\"") || strings.Contains(markup, `href="javascript:`) {
			t.Errorf("%q is rendered as a link: %s", href, markup)
		}
		// An announcement's sources go through the same check. Its address is a
		// field of its own, read with the space around it removed, so an address
		// that is whole once trimmed is the document it names and nothing else.
		if validAgentDocumentHref(strings.TrimSpace(href)) {
			continue
		}
		announced, err := ui.RenderToString(html.Div(html.Props{}, RenderAgentAnnouncementMessage(Model{Locale: "en-US"}, AgentAnnouncementMessage{AgentName: "Assistant", Text: "Thanksgiving is next.", Sources: []AgentAnnouncementSource{{Title: "2026 holiday guide", Href: href}}})))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(announced, "agent-reply-source-link") {
			t.Errorf("%q is a link in an announcement: %s", href, announced)
		}
	}

	// What the server writes still opens the document, at its version and section.
	for _, href := range []string{
		"/workspace/app/docs?document=pto",
		"/workspace/app/docs?document=doc-64271829&version=docv-10264426#carryover",
		"/workspace/app/docs?document=673214ec-4402-5f09-bf93-0d42e691712f&version=3",
	} {
		if !validAgentDocumentHref(href) {
			t.Errorf("%q is refused", href)
		}
		envelope := parseAgentReplyEnvelope("Answer.\n\nSources\n- [Paid time off policy](" + href + ")")
		if len(envelope.Sources) != 1 || envelope.Sources[0].Href != href || !envelope.Sources[0].Readable {
			t.Errorf("%q is not a source link: %+v", href, envelope.Sources)
		}
	}
}
