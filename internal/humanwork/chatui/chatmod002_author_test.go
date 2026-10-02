package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatmod002Page renders the whole page with the product's own catalog, the one
// that answers a missing key with a bracketed key. The viewer, Jake, has a draft
// a filter refused, one of his messages that a filter masked, and a reader's
// message that was masked too.
func chatmod002Page(t *testing.T, locale string, edit bool) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	draft := "you damn fool"
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", CurrentUser: "jake", CurrentTenantID: "t",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
		Draft:         draft,
		Messages: []chatui.Message{
			{ID: "own", AuthorID: "jake", Author: "Jake", SentAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), Sequence: 1, Body: "you are a [removed word]"},
			{ID: "reader", AuthorID: "walt", Author: "Walt", SentAt: time.Date(2026, 10, 1, 9, 1, 0, 0, time.UTC), Sequence: 2, Body: "he is a [removed word] too"},
			{ID: "plain", AuthorID: "jake", Author: "Jake", SentAt: time.Date(2026, 10, 1, 9, 2, 0, 0, time.UTC), Sequence: 3, Body: "a message nothing happened to"},
		},
		AuthorBlocked: map[string]chatui.AuthorBlocked{
			chatui.ModAuthorKeyComposer("general"): {Surface: chatui.ModAuthorSurfaceMessage, Words: []string{"damn"}, Text: draft, Stamp: 1},
			chatui.ModAuthorKeyEdit("own"):         {Surface: chatui.ModAuthorSurfaceMessage, Text: "you are a damn", Stamp: 2},
		},
		Callbacks: chatui.Callbacks{SendMessage: func(string, string) {}, EditMessage: func(string, string, uint64) {}, BeginEdit: func(string) {}},
	}
	if edit {
		m.EditingID = "own"
	}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(page)
}

// chatmod002Tag is the opening tag that carries marker.
func chatmod002Tag(t *testing.T, page, marker string) string {
	t.Helper()
	at := strings.Index(page, marker)
	if at < 0 {
		t.Fatalf("no element carries %q", marker)
	}
	start := strings.LastIndex(page[:at], "<")
	end := strings.Index(page[at:], ">")
	return page[start : at+end+1]
}

func TestTodo_CHATMOD_002_Accessibility(t *testing.T) {
	page := chatmod002Page(t, "en-US", true)
	// The blocked line: an alert in the composer region, auto direction, and the
	// field points at it.
	tag := chatmod002Tag(t, page, `id="chatmod002-blocked"`)
	if !strings.Contains(tag, `role="alert"`) || !strings.Contains(tag, `dir="auto"`) {
		t.Errorf("the blocked line is not an alert with dir=auto: %s", tag)
	}
	formStart := regexp.MustCompile(`<form[^>]*class="chat-composer"`).FindStringIndex(page)
	if formStart == nil {
		t.Fatal("no composer form")
	}
	form := page[formStart[0]:]
	form = form[:strings.Index(form, "</form>")]
	if !strings.Contains(form, `id="chatmod002-blocked"`) {
		t.Error("the blocked line is not inside the composer")
	}
	field := chatmod002Tag(t, form, `id="chat-composer"`)
	if !regexp.MustCompile(`aria-describedby="[^"]*chatmod002-blocked`).MatchString(field) {
		t.Errorf("the composer field is not associated with the line: %s", field)
	}
	if !strings.Contains(form, `This message was not sent: it contains a word this workspace does not allow: "damn".`) {
		t.Errorf("the composer line is missing its sentence: %s", form)
	}
	if strings.Contains(page, "We couldn't send this message") {
		t.Error("the generic send failure is on a blocked page")
	}
	// The edit form carries its own line, tied to its own field.
	editLine := chatmod002Tag(t, page, `id="chatmod002-blocked-edit"`)
	if !strings.Contains(editLine, `role="alert"`) || !strings.Contains(editLine, `dir="auto"`) {
		t.Errorf("the edit line: %s", editLine)
	}
	if edit := chatmod002Tag(t, page, `id="edit-own"`); !strings.Contains(edit, "chatmod002-blocked-edit") {
		t.Errorf("the edit field is not associated with its line: %s", edit)
	}
	// The masked note is a quiet line, not an alert, and only the author's own
	// masked message has one.
	note := chatmod002Tag(t, page, "chatmod002-masked-note")
	if strings.Contains(note, "alert") || strings.Contains(note, "status") || !strings.Contains(note, `dir="auto"`) {
		t.Errorf("the masked note is not a plain line: %s", note)
	}
	if got := strings.Count(page, "chatmod002-masked-note"); got != 1 {
		t.Errorf("%d masked notes, want 1 (the author's own masked message only)", got)
	}
	// The chip has accessible text in every language and is never blank.
	for locale, want := range map[string]string{"en-US": "removed word", "de-DE": "entferntes Wort", "ar": "كلمة محذوفة"} {
		p := chatmod002Page(t, locale, false)
		chips := regexp.MustCompile(`<span class="chatfilter-removed">([^<]*)</span>`).FindAllStringSubmatch(p, -1)
		if len(chips) != 2 {
			t.Fatalf("%s: %d removed-word chips, want 2 (own and reader's)", locale, len(chips))
		}
		for _, chip := range chips {
			if chip[1] != want {
				t.Errorf("%s: chip text %q, want %q", locale, chip[1], want)
			}
		}
		if strings.Contains(p, "[removed word]") {
			t.Errorf("%s: the raw token is on the page", locale)
		}
	}
	// The styles give the chip a visible themed look from the product's tokens.
	if !strings.Contains(chatui.Stylesheet, ".chatfilter-removed{display:inline-block;") || strings.Contains(chatui.ChatMod002Styles, "style=") {
		t.Error("the removed-word chip has no chip style")
	}
	if regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgb\(`).MatchString(chatui.ChatMod002Styles) {
		t.Error("the CHATMOD-002 styles use a colour that is not a token")
	}
}

func TestTodo_CHATMOD_002_Browser(t *testing.T) {
	for locale, want := range map[string]struct{ blocked, note, edit string }{
		"en-US": {`This message was not sent: it contains a word this workspace does not allow: "damn".`, "Readers see this message with a word hidden.", "This message was not sent: it contains a word this workspace does not allow."},
		"de-DE": {"Diese Nachricht wurde nicht gesendet: Sie enthält ein Wort, das dieser Arbeitsbereich nicht erlaubt: „damn“.", "Leser sehen diese Nachricht mit einem verborgenen Wort.", "Diese Nachricht wurde nicht gesendet: Sie enthält ein Wort, das dieser Arbeitsbereich nicht erlaubt."},
		"ar":    {"لم تُرسل هذه الرسالة: فهي تحتوي على كلمة لا تسمح بها مساحة العمل هذه: ⁨\"damn\"⁩.", "يرى القراء هذه الرسالة مع إخفاء كلمة.", "لم تُرسل هذه الرسالة: فهي تحتوي على كلمة لا تسمح بها مساحة العمل هذه."},
	} {
		page := chatmod002Page(t, locale, true)
		if strings.Contains(page, "⟦") {
			t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
		}
		for what, sentence := range map[string]string{"composer line": want.blocked, "masked note": want.note, "edit line": want.edit} {
			if !strings.Contains(page, sentence) {
				t.Errorf("%s: the %s does not read %q", locale, what, sentence)
			}
		}
		if locale == "ar" && !strings.Contains(page, `dir="rtl"`) {
			t.Error("ar: the page is not right to left")
		}
		// A masked message reads the same to its author as to a reader, and nothing
		// marks a message a filter only flagged.
		if strings.Count(chatmod002Page(t, locale, false), `class="chatfilter-removed"`) != 2 {
			t.Errorf("%s: want a chip in the author's and in the reader's message", locale)
		}
		if strings.Contains(strings.ToLower(page), "flagged") {
			t.Errorf("%s: something marks a flagged message", locale)
		}
		// The line is gone once the author has nothing refused: no entry, no line.
	}
	// With no refusal in the model there is no line and no description.
	ctx := productui.ResolveProductLocale("en-US")
	clean := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", CurrentUser: "jake", CurrentTenantID: "t",
		Text: func(key string) string { return ctx.Text(key) }, Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
		Draft: "you damn fool", Callbacks: chatui.Callbacks{SendMessage: func(string, string) {}}}
	rendered, err := ui.RenderToString(chatui.Build(clean))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "chatmod002-blocked") || strings.Contains(rendered, "role=\"alert\" dir=\"auto\"") && strings.Contains(rendered, "was not sent") {
		t.Error("a line is drawn with no refusal")
	}
}
