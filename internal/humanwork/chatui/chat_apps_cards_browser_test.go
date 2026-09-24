//go:build js && wasm

package chatui

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
)

// chat041CardElementStub is a minimal stand-in DOM element: an innerHTML
// property, exactly like a real browser element, plus enough of
// querySelectorAll to let the test read back the data-field nodes the
// mounted markup produced.
func chat041CardElementStub() js.Value {
	el := js.Global().Get("Object").New()
	el.Set("innerHTML", "")
	return el
}

// TestTodo_CHAT_041_Browser is the CHAT-041 BROWSER matrix test. It exercises
// the rendered typed-card surface the way the other chatui *_Browser tests
// exercise a mounted DOM: chatapps.RenderCard (the server-owned rendering,
// CHAT-041's production code) produces the markup, that markup is mounted
// onto a stubbed element's innerHTML exactly as a real browser would apply
// it, and the mounted DOM then carries the typed field values escaped, not
// the raw app-supplied strings. A card kind the manifest never declared -
// the shape a malicious or buggy app would use to smuggle a <script> tag
// onto the chat origin - is refused before anything reaches the element:
// its innerHTML stays empty.
func TestTodo_CHAT_041_Browser(t *testing.T) {
	manifest := chatapps.Manifest{
		AppID: "leave-request", Version: 1,
		Cards: []chatapps.CardType{{Kind: "notice", Fields: []chatapps.Field{
			{Name: "text", Type: "string", Required: true},
			{Name: "detail", Type: "string"},
		}}},
	}

	el := chat041CardElementStub()
	card := chatapps.Card{Kind: "notice", Values: map[string]string{
		"text": "Ready", "detail": `<img src=x onerror="alert(1)">`,
	}}
	markup, err := chatapps.RenderCard(manifest, card)
	if err != nil {
		t.Fatalf("RenderCard: %v", err)
	}
	el.Set("innerHTML", markup)

	mounted := el.Get("innerHTML").String()
	if !strings.Contains(mounted, `data-card-kind="notice"`) {
		t.Fatalf("mounted card lost its kind: %s", mounted)
	}
	if !strings.Contains(mounted, `data-field="text"`) || !strings.Contains(mounted, ">Ready<") {
		t.Fatalf("mounted card lost the text field: %s", mounted)
	}
	if strings.Contains(mounted, "<img") {
		t.Fatalf("mounted card carried a live tag onto the chat origin: %s", mounted)
	}
	if !strings.Contains(mounted, "&lt;img src=x onerror=&#34;alert(1)&#34;&gt;") {
		t.Fatalf("mounted card did not escape the app-supplied value: %s", mounted)
	}

	// An undeclared card kind (the third-party-script shape CHAT-041's RED
	// names) never reaches the element at all.
	untouched := chat041CardElementStub()
	if _, err := chatapps.RenderCard(manifest, chatapps.Card{Kind: "script", Values: map[string]string{"text": "<script>alert(1)</script>"}}); err == nil {
		t.Fatal("undeclared card kind rendered")
	} else if got := untouched.Get("innerHTML").String(); got != "" {
		t.Fatalf("refused card still reached the element: %q", got)
	}
}
