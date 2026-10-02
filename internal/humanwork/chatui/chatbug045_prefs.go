package chatui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// CHATBUG-045: Chat preferences lists, in this order, Quiet hours, Reading
// languages (with "Translate messages into my language"), Writing style and Emoji
// skin tone. Each is one row with its current value and each changes at once, with
// no Save button. The first two rows are built where they always were
// (railPreferences and the reading settings); this file adds the last two, and
// leaves a row out when its feature is not composed.

// chatbug045WritingStyleRow is the Writing style row, supplied by the writing
// style feature (chattone) when it has a per-person style to show and change. The
// feature has no such control yet (its toolbar rewrites a draft; it keeps no
// preference), so nothing sets this and the row is not drawn. Setting it to a
// function that returns a row (chatux002Row builds one) adds the row after Reading
// languages with no other change.
var chatbug045WritingStyleRow func(Model) ui.Node

// chatbug045PanelRows are the rows of the panel, in order, each keyed by what it
// is. The reading row (a component that holds the settings it loaded) and the rows
// around it come and go as the page learns which features the workspace has
// composed; without keys the reconciler matched them by position, so a row that
// appeared above another gave its place, and its state, to a different component,
// and the reading row's answer from the settings service was lost with the
// instance that asked for it.
func chatbug045PanelRows(m Model, h handlers) []ui.Node {
	rows := []ui.Node{}
	add := func(key string, row ui.Node) {
		if row != nil {
			rows = append(rows, html.WithKey(row, "prefs-row:"+key))
		}
	}
	add("quiet-hours", railPreferences(m, h))
	add("voice-messages", chatvoicePersonalRow(m))
	add("reading-languages", integrate2ReadingSettings(m))
	if chatbug045WritingStyleRow != nil {
		add("writing-style", chatbug045WritingStyleRow(m))
	}
	add("emoji-tone", chatbug045EmojiToneRow(m))
	return rows
}

// chatbug045Tone is the person's skin tone: what this page holds when it has
// their key (a choice made here counts at once), else the server's copy.
func chatbug045Tone(m Model) int {
	if chatEmojiHost.prefsKey == emojiPrefsKey(m.CurrentTenantID, m.CurrentUser) && m.CurrentUser != "" {
		return chatEmojiHost.prefs.Tone
	}
	return decodeEmojiPrefs(m.EmojiPrefs).Tone
}

type chatbug045ToneProps struct {
	Locale string
	Tone   int
}

// chatbug045EmojiToneRow is the Emoji skin tone row. It is drawn only where the
// page can save the person's emoji choices (the server keeps them with the rest
// of the sidebar preferences).
func chatbug045EmojiToneRow(m Model) ui.Node {
	if m.Callbacks.SaveEmojiPrefs == nil {
		return nil
	}
	return ui.CreateElement(chatbug045ToneComponent, chatbug045ToneProps{Locale: m.Locale, Tone: chatbug045Tone(m)})
}

// chatbug045SetTone remembers the skin tone through the emoji feature's own
// function, which writes it to the server, and redraws the page so the row shows
// the new choice.
func chatbug045SetTone(tone int) {
	chatEmojiToneSet(tone)
	chatEmojiHost.writeFull(func(*localUI) {})
}

func chatbug045ToneComponent(p chatbug045ToneProps) ui.Node {
	// One handler for each tone, always the same number of hooks.
	picks := make([]ui.Handler, emojiset.Tones+1)
	for tone := range picks {
		tone := tone
		picks[tone] = ui.UseEvent(func() { chatbug045SetTone(tone) })
	}
	return chatbug045ToneView(p.Locale, p.Tone, picks)
}

// chatbug045ToneView draws the row: the title, the current tone's name, and the
// tones as buttons, the current one pressed.
func chatbug045ToneView(locale string, tone int, picks []ui.Handler) ui.Node {
	m := Model{Locale: locale}
	title := chatbug045Text(m, keyChatbug045EmojiTone)
	choices := make([]ui.Node, 0, emojiset.Tones+1)
	for t := 0; t <= emojiset.Tones; t++ {
		name := chatEmojiToneName(m, emojiView{}, t)
		props := html.Props{Class: "chat-prefs-swatch", Type: "button", Title: name,
			Data: map[string]string{"prefs-tone": strconv.Itoa(t)},
			Aria: map[string]string{"label": name, "pressed": boolString(t == tone)}, Text: chatEmojiToneGlyph(t)}
		if t < len(picks) {
			props.OnClick = picks[t]
		}
		choices = append(choices, html.Button(props))
	}
	return chatux002Row("emoji-tone", title, chatEmojiToneName(m, emojiView{}, tone), nil,
		html.Div(html.Props{Class: "chat-prefs-swatches", Role: "group", Dir: "ltr", Aria: map[string]string{"label": title}}, choices...))
}

// ChatBug045Styles is the tone buttons; the rows themselves use the panel's.
const ChatBug045Styles = ChatBug045ReadingStyles + `.chat-prefs-swatches{display:flex;flex-wrap:wrap;gap:6px;padding-block:4px}` +
	`.chat-prefs-swatch{display:inline-flex;align-items:center;justify-content:center;box-sizing:border-box;inline-size:44px;block-size:44px;padding:0;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-size:1.25rem;line-height:1;cursor:pointer}` +
	`.chat-prefs-swatch:hover{background:color-mix(in srgb,var(--ink) 6%,transparent)}` +
	`.chat-prefs-swatch[aria-pressed="true"]{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 14%,transparent);box-shadow:inset 0 0 0 1px var(--accent)}` +
	`.chat-prefs-swatch:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}`
