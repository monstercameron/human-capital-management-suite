package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatbug045Model is a ready Chat page rendered with the product's own catalog,
// the one that answers an unknown key with a bracketed marker.
func chatbug045Model(locale string, withEmoji bool) chatui.Model {
	ctx := productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, OwnerID: "owner", MemberCount: 18, Joined: true}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: "walt", CurrentTenantID: "t",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room},
		Members:       []chatui.Member{{ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}},
		Callbacks:     chatui.Callbacks{SavePreferences: func(chatui.Preferences) {}},
	}
	if withEmoji {
		m.Callbacks.SaveEmojiPrefs = func(string) {}
		m.EmojiPrefs = `{"v":1,"tone":3,"usage":[],"seq":0}`
	}
	return m
}

func chatbug045Panel(t *testing.T, locale string, withEmoji bool) string {
	t.Helper()
	page, err := ui.RenderToString(chatui.Build(chatbug045Model(locale, withEmoji)))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(page)
}

func TestTodo_CHATBUG_045_Browser(t *testing.T) {
	// The words each row needs, from the feature's own table in each language.
	want := map[string][]string{
		"en-US": {">Reading language<", "Emoji skin tone", "Medium skin tone", "Translate messages into my language"},
		"de-DE": {">Lesesprache<", "Hautfarbe der Emoji", "Mittlere Hautfarbe", "Nachrichten in meine Sprache übersetzen"},
		"ar":    {">لغة القراءة<", "لون بشرة الرموز التعبيرية", "بشرة متوسطة", "ترجمة الرسائل إلى لغتي"},
	}
	for locale, words := range want {
		page := chatbug045Panel(t, locale, true)
		if strings.Contains(page, "⟦") {
			t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
		}
		for _, text := range words {
			if !strings.Contains(page, text) {
				t.Errorf("%s: the preferences panel lacks %q", locale, text)
			}
		}
		// Quiet hours' own title differs by catalog; the section is what matters.
		if !strings.Contains(page, `data-prefs-section="quiet-hours"`) {
			t.Errorf("%s: no Quiet hours row", locale)
		}
	}
}

func TestTodo_CHATBUG_045(t *testing.T) {
	page := chatbug045Panel(t, "en-US", true)
	// The rows, in order, one section each.
	order := []string{`data-prefs-section="quiet-hours"`, `data-prefs-section="reading-languages"`, `data-prefs-section="emoji-tone"`}
	last := -1
	for _, section := range order {
		at := strings.Index(page, section)
		if at < 0 {
			t.Fatalf("the preferences panel has no %s", section)
		}
		if at < last {
			t.Fatalf("%s is out of order", section)
		}
		last = at
	}
	if strings.Count(page, `data-prefs-section="`) != len(order) {
		t.Errorf("the panel holds %d rows, want %d (Writing style stays out until its feature supplies one)", strings.Count(page, `data-prefs-section="`), len(order))
	}

	// The reading language row changes at once: no Save button anywhere in the
	// panel, and the translate choice is a switch.
	if strings.Contains(page, chatui.RenderingText("en-US", "save")) {
		t.Error("the reading language settings still have a Save button")
	}
	panel := page[strings.Index(page, `data-prefs-section="quiet-hours"`):]
	if strings.Contains(panel[:strings.Index(panel, `data-prefs-section="emoji-tone"`)], `type="submit"`) {
		t.Error("a submit button is inside the reading language settings")
	}
	box := regexp.MustCompile(`<input[^>]*id="chatrender-translate"[^>]*>`).FindString(page)
	if box == "" || !strings.Contains(box, `role="switch"`) || !strings.Contains(box, `class="switch"`) {
		t.Errorf("the translate choice is not a switch: %q", box)
	}
	if !strings.Contains(page, "Translate messages into my language") {
		t.Error("the reading row does not offer Translate messages into my language")
	}
	// The stand-alone form keeps its Save button.
	form, err := ui.RenderToString(chatui.RenderingSettings(chatui.RenderingSettingsModel{Locale: "en-US"}))
	if err != nil || !strings.Contains(form, chatui.RenderingText("en-US", "save")) {
		t.Error("the stand-alone language settings lost their Save button")
	}

	// Emoji skin tone: the current tone's name beside the title, six buttons, the
	// saved one pressed.
	tone := page[strings.Index(page, `data-prefs-section="emoji-tone"`):]
	if !strings.Contains(tone, "Emoji skin tone") || !regexp.MustCompile(`data-prefs-value="emoji-tone"[^>]*>Medium skin tone<`).MatchString(tone) {
		t.Error("the emoji row does not show its title and the current tone")
	}
	buttons := regexp.MustCompile(`<button[^>]*data-prefs-tone="(\d)"[^>]*>`).FindAllStringSubmatch(tone, -1)
	if len(buttons) != 6 {
		t.Fatalf("%d tone buttons, want 6", len(buttons))
	}
	for _, b := range buttons {
		pressed := strings.Contains(b[0], `aria-pressed="true"`)
		if pressed != (b[1] == "3") {
			t.Errorf("tone %s pressed = %v", b[1], pressed)
		}
	}

	// A workspace whose emoji preferences cannot be saved has no such row.
	if without := chatbug045Panel(t, "en-US", false); strings.Contains(without, `data-prefs-section="emoji-tone"`) {
		t.Error("an Emoji skin tone row is drawn where the choice cannot be saved")
	}
	// Reading languages and Quiet hours stay when the emoji row is absent.
	if without := chatbug045Panel(t, "en-US", false); !strings.Contains(without, `data-prefs-section="reading-languages"`) || !strings.Contains(without, `data-prefs-section="quiet-hours"`) {
		t.Error("the other rows disappeared with the emoji row")
	}
}

// The reading settings keep what a person reaches for first in view (the language
// and the translate switch) and put the two lists of languages, as chips, with the
// "apply to this conversation" choice, under one hidden More options disclosure.
func TestTodo_CHATBUG_045_MoreOptions(t *testing.T) {
	for locale, more := range map[string]string{"en-US": "More options", "de-DE": "Weitere Optionen", "ar": "خيارات إضافية"} {
		page := chatbug045Panel(t, locale, true)
		form := page[strings.Index(page, `class="chatrender-settings"`):]
		form = form[:strings.Index(form, "</form>")]
		body := regexp.MustCompile(`<div[^>]*chatrender-more-body[^>]*>`).FindStringIndex(form)
		if body == nil || !strings.Contains(form[body[0]:body[1]], "hidden") {
			t.Fatalf("%s: no hidden More options body", locale)
		}
		if !strings.Contains(form[:body[0]], more) {
			t.Errorf("%s: the More options toggle %q is missing", locale, more)
		}
		before, inside := form[:body[0]], form[body[0]:]
		for _, shown := range []string{`id="chatrender-reading"`, `id="chatrender-translate"`} {
			if !strings.Contains(before, shown) {
				t.Errorf("%s: %s is not in view", locale, shown)
			}
		}
		if strings.Count(inside, `class="chatrender-chip"`) != 16 || !strings.Contains(inside, `name="further"`) || !strings.Contains(inside, `name="never"`) {
			t.Errorf("%s: the two language lists are not 16 chips under More options", locale)
		}
		if strings.Contains(before, `type="checkbox" value=`) {
			t.Errorf("%s: a language checkbox is outside More options", locale)
		}
	}
	// Never "Not specified" to a person.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		out, err := ui.RenderToString(chatui.RenderingLanguageStatusIndicator(locale, map[string]int{"en": 1, "und": 17}, false, false))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, chatui.RenderingText(locale, "und")) || strings.Contains(out, ": 17") {
			t.Errorf("%s: the language list shows the unknown language: %s", locale, out)
		}
		if !strings.Contains(out, chatui.RenderingText(locale, "en")+": 1") {
			t.Errorf("%s: the known language is missing: %s", locale, out)
		}
	}
	// The row sits in a host that never changes, so it keeps its place among the
	// rows after it when it replaces its placeholder.
	page := chatbug045Panel(t, "en-US", true)
	host := strings.Index(page, `class="chatrender-personal-host"`)
	reading, emoji := strings.Index(page, `data-prefs-section="reading-languages"`), strings.Index(page, `data-prefs-section="emoji-tone"`)
	if host < 0 || reading < host || emoji < reading {
		t.Error("the reading row is not inside its host before the emoji row")
	}
	// A title, its value and its control wrap rather than overlap.
	for _, want := range []string{".chat-prefs-head{flex-wrap:wrap", ".chat-prefs-title{flex:1 1 auto;white-space:normal"} {
		if !strings.Contains(chatui.ChatBug045Styles, want) {
			t.Errorf("the row heads do not wrap: missing %q", want)
		}
	}
}
