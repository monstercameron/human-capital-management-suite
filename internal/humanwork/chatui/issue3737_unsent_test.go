package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func html3737(m Model) ui.Node {
	return html.Div(html.Props{}, timelineBody(m, handlers{})...)
}

func renderNodes3737(t *testing.T, nodes []ui.Node) string {
	t.Helper()
	return renderNode(t, html.Div(html.Props{}, nodes...))
}

func issue3737Model(locale string, rows ...UnsentMessage) Model {
	m := chatux007Model(locale)
	m.State = StateReady
	m.Messages = []Message{{ID: "p1", Body: "earlier", Author: "Ana", AuthorID: "ana"}}
	m.Unsent = rows
	return m
}

func TestIssue3737_FailedSendStaysVisibleWithItsTextAndThreeControls(t *testing.T) {
	m := issue3737Model("en-US", UnsentMessage{ID: "u1", ConversationID: "general", Body: "Lunch at noon?", Reason: UnsentOffline})
	markup := renderNode(t, html3737(m))
	for _, want := range []string{"Lunch at noon?", "Not sent", "You are offline. This sends when you reconnect.",
		`data-action="unsent-retry"`, `data-action="unsent-edit"`, `data-action="unsent-discard"`, `data-id="u1"`,
		">Retry<", ">Edit<", ">Discard<", `role="alert"`, `dir="auto"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
}

func TestIssue3737_SendingRowHasNoControlsAndSaysSo(t *testing.T) {
	m := issue3737Model("en-US", UnsentMessage{ID: "u1", ConversationID: "general", Body: "Hello"})
	markup := renderNode(t, html3737(m))
	if !strings.Contains(markup, "Sending") || strings.Contains(markup, "unsent-retry") || strings.Contains(markup, "Not sent") {
		t.Fatalf("a message still going out is Sending with no controls: %s", markup)
	}
	if !strings.Contains(markup, `role="group"`) || !strings.Contains(markup, `aria-busy="true"`) {
		t.Fatalf("the labelled Sending row needs a role to carry its name: %s", markup)
	}
}

func TestIssue3737_OnlyTheOpenRoomsRowsShowAndEmptyRoomStillShowsThem(t *testing.T) {
	m := issue3737Model("en-US",
		UnsentMessage{ID: "u1", ConversationID: "general", Body: "mine here", Reason: UnsentFailed},
		UnsentMessage{ID: "u2", ConversationID: "other", Body: "elsewhere secret", Reason: UnsentFailed})
	markup := renderNode(t, html3737(m))
	if !strings.Contains(markup, "mine here") || strings.Contains(markup, "elsewhere secret") {
		t.Fatalf("rows must follow the open room: %s", markup)
	}
	m.Messages = nil
	body := renderNodes3737(t, timelineBody(m, handlers{}))
	if !strings.Contains(body, "mine here") {
		t.Fatalf("an empty room must not hide the unsent message behind the empty state: %s", body)
	}
}

func TestIssue3737_CopyInEveryLocaleAndRTL(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := issue3737Model(locale, UnsentMessage{ID: "u1", ConversationID: "general", Body: "x", Reason: UnsentOffline})
		markup := renderNode(t, html3737(m))
		if strings.Contains(markup, "Not sent") || strings.Contains(markup, ">Retry<") || strings.Contains(markup, ">Discard<") {
			t.Fatalf("%s fell back to English: %s", locale, markup)
		}
	}
	for _, want := range []string{"Nicht gesendet", "Erneut senden"} {
		m := issue3737Model("de-DE", UnsentMessage{ID: "u1", ConversationID: "general", Body: "x", Reason: UnsentFailed})
		if !strings.Contains(renderNode(t, html3737(m)), want) {
			t.Fatalf("de-DE missing %q", want)
		}
	}
	if !strings.Contains(renderNode(t, html3737(issue3737Model("ar", UnsentMessage{ID: "u1", ConversationID: "general", Body: "x", Reason: UnsentFailed}))), "لم تُرسل") {
		t.Fatal("ar missing the Not sent heading")
	}
}

func TestIssue3737_ControlsNameTheMessageAndStylesShip(t *testing.T) {
	m := issue3737Model("en-US", UnsentMessage{ID: "u1", ConversationID: "general", Body: "Lunch at noon?", Reason: UnsentFailed})
	markup := renderNode(t, html3737(m))
	for _, want := range []string{`aria-label="Retry sending: Lunch at noon?"`, `aria-label="Edit unsent message: Lunch at noon?"`, `aria-label="Discard unsent message: Lunch at noon?"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	if !strings.Contains(Stylesheet, Issue3737Styles) || !strings.Contains(Issue3737Styles, "min-height:44px") {
		t.Fatal("styles must ship in the stylesheet with 44px phone targets")
	}
}

func TestIssue3737_ActionsReachTheRightCallbacks(t *testing.T) {
	var got []string
	m := Model{}
	m.Callbacks.RetryUnsent = func(id string) { got = append(got, "retry:"+id) }
	m.Callbacks.EditUnsent = func(id string) { got = append(got, "edit:"+id) }
	m.Callbacks.DiscardUnsent = func(id string) { got = append(got, "discard:"+id) }
	for _, action := range []string{"unsent-retry", "unsent-edit", "unsent-discard"} {
		if !issue3737Dispatch(m, action, "u9") {
			t.Fatalf("%s not handled", action)
		}
	}
	if issue3737Dispatch(m, "other", "u9") {
		t.Fatal("foreign action consumed")
	}
	if strings.Join(got, ",") != "retry:u9,edit:u9,discard:u9" {
		t.Fatalf("got %v", got)
	}
}

func TestIssue3737_PreviewIsShortened(t *testing.T) {
	long := strings.Repeat("word ", 40)
	if p := issue3737Preview(long); len([]rune(p)) > 64 || !strings.HasSuffix(p, "…") {
		t.Fatalf("preview %q", p)
	}
}
