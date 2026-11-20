package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func issue3934Model(mode string) Model {
	return Model{State: StateReady, Locale: "en-US", CurrentUser: "alice", SelectedID: "general", ComposerEnter: mode,
		Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}},
		Callbacks:     Callbacks{SendMessage: func(string, string) {}},
	}
}

func TestIssue3934_EnterNewlineKeyMatrix(t *testing.T) {
	draft := "line one"
	cases := []struct {
		name  string
		key   string
		state composerKeyState
		want  composerAction
	}{
		{"default: Enter sends", "Enter", composerKeyState{Draft: draft}, composerSend},
		{"default: Ctrl+Enter also sends", "Enter", composerKeyState{Draft: draft, Command: true}, composerSend},
		{"newline: Enter is a line", "Enter", composerKeyState{Draft: draft, EnterNewline: true}, composerNative},
		{"newline: Enter in an empty box is a line", "Enter", composerKeyState{EnterNewline: true}, composerNative},
		{"newline: Shift+Enter is a line", "Enter", composerKeyState{Draft: draft, EnterNewline: true, Shift: true}, composerNative},
		{"newline: Ctrl+Enter sends", "Enter", composerKeyState{Draft: draft, EnterNewline: true, Command: true}, composerSend},
		{"newline: Ctrl+Enter on a blank draft sends nothing", "Enter", composerKeyState{Draft: "  ", EnterNewline: true, Command: true}, composerEmpty},
		{"newline: composing Ctrl+Enter is the input method's", "Enter", composerKeyState{Draft: draft, EnterNewline: true, Command: true, Composing: true}, composerNative},
		{"newline: Enter still picks a highlighted mention", "Enter", composerKeyState{Draft: "@al", EnterNewline: true, MentionOpen: true, MentionHighlighted: true}, composerPickMention},
		{"newline: Enter still picks a highlighted emoji", "Enter", composerKeyState{Draft: ":sm", EnterNewline: true, EmojiOpen: true, EmojiHighlighted: true}, composerPickEmoji},
		{"newline: Ctrl+Enter holds while mentions load", "Enter", composerKeyState{Draft: "@a", EnterNewline: true, Command: true, MentionOpen: true, MentionLoading: true}, composerHold},
		{"newline: Tab is untouched", "Tab", composerKeyState{Draft: draft, EnterNewline: true}, composerNative},
	}
	for _, tc := range cases {
		if got := composerKeyAction(tc.state, tc.key); got != tc.want {
			t.Errorf("%s: composerKeyAction = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIssue3934_EditBoxSavesWithTheShortcutInNewlineMode(t *testing.T) {
	cases := []struct {
		name                      string
		key                       string
		shift, composing, command bool
		newline, want             bool
	}{
		{"default Enter saves", "Enter", false, false, false, false, true},
		{"newline Enter is a line", "Enter", false, false, false, true, false},
		{"newline Ctrl+Enter saves", "Enter", false, false, true, true, true},
		{"newline composing never saves", "Enter", false, true, true, true, false},
		{"newline Shift+Enter is a line", "Enter", true, false, true, true, false},
		{"other keys never save", "a", false, false, true, true, false},
	}
	for _, tc := range cases {
		if got := issue3934EditSaves(tc.key, tc.shift, tc.composing, tc.command, tc.newline); got != tc.want {
			t.Errorf("%s: saves = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIssue3934_HintsFollowThePreference(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		sendM, lineM := issue3934Model(""), issue3934Model(ComposerEnterNewline)
		sendM.Locale, lineM.Locale = locale, locale
		h := handlers{}
		h.local.sentCount = 9 // the default hint has taught what it says and gone
		sendMain := renderNode(t, composer(sendM, h))
		lineMain := renderNode(t, composer(lineM, h))
		lineThread := renderNode(t, threadPane(lineM, h))
		if strings.Contains(sendMain, `id="composer-help"`) {
			t.Errorf("%s: the default hint should be gone after three sends", locale)
		}
		if !strings.Contains(lineMain, `id="composer-help"`) || !strings.Contains(lineThread, `id="thread-composer-help"`) {
			t.Errorf("%s: Enter-for-newline must keep its hint in the channel and thread composers", locale)
		}
		want := issue3934Text(lineM, keyIssue3934Hint)
		if want == "" || (locale != "en-US" && want == issue3934Text(issue3934Model(""), keyIssue3934Hint)) {
			t.Errorf("%s: no localized hint %q", locale, want)
		}
		if locale == "en-US" && !strings.Contains(lineMain, want) {
			t.Errorf("the channel hint does not name the send shortcut: %s", lineMain)
		}
		if !strings.Contains(lineMain, `aria-describedby="composer-help"`) {
			t.Errorf("%s: the field no longer points at its hint", locale)
		}
	}
	// Default mode keeps the original wording for the first three sends.
	if markup := renderNode(t, composer(issue3934Model(""), handlers{})); !strings.Contains(markup, issue3934Model("").t(KeyComposeHint)) {
		t.Error("the default hint changed")
	}
}

func TestIssue3934_EditAndPreviewHints(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3934Model(ComposerEnterNewline)
		m.Locale = locale
		def := issue3934Model("")
		def.Locale = locale
		if issue3934EditHintText(m) == issue3934EditHintText(def) || issue3934PreviewKeys(m) == issue3934PreviewKeys(def) {
			t.Errorf("%s: the edit or preview hint ignores the preference", locale)
		}
		markup := renderNode(t, chatbug073EditHint(m, "p1"))
		if !strings.Contains(markup, issue3934EditHintText(m)) {
			t.Errorf("%s: the edit hint is %s", locale, markup)
		}
	}
	if got := issue3934EditHintText(issue3934Model("")); got != chatbug073Text(issue3934Model(""), keyChatbug073Hint) {
		t.Errorf("default edit hint changed: %q", got)
	}
}

func TestIssue3934_PreferencesRow(t *testing.T) {
	m := issue3934Model("")
	if rows := renderNode(t, html.Div(html.Props{}, chatbug045PanelRows(m, handlers{})...)); strings.Contains(rows, "composer-enter") {
		t.Fatal("the row is drawn where the page cannot keep the choice")
	}
	var saved []bool
	m.Callbacks.SetComposerEnter = func(newline bool) { saved = append(saved, newline) }
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m.Locale = locale
		rows := renderNode(t, html.Div(html.Props{}, chatbug045PanelRows(m, handlers{})...))
		for _, want := range []string{`data-prefs-section="composer-enter"`, `data-action="composer-enter"`, `data-extra="send"`, `data-extra="newline"`, issue3934Text(m, keyIssue3934Title), issue3934Text(m, keyIssue3934Note)} {
			if !strings.Contains(rows, want) {
				t.Errorf("%s: row lacks %q", locale, want)
			}
		}
		if !strings.Contains(rows, `aria-pressed="true"`) {
			t.Errorf("%s: no choice is shown as current", locale)
		}
	}
	m.Locale = "en-US"
	m.ComposerEnter = ComposerEnterNewline
	rows := renderNode(t, html.Div(html.Props{}, chatbug045PanelRows(m, handlers{})...))
	if strings.Count(rows, `aria-pressed="true"`) != 1 || !strings.Contains(rows, "Enter adds a new line") {
		t.Errorf("exactly one choice should be pressed: %s", rows)
	}

	// Choosing: a known mode reaches the callback once, repeating it does not, junk is refused.
	if !issue3934Choose(m, ComposerEnterSends) || len(saved) != 1 || saved[0] {
		t.Fatalf("choosing Enter sends saved %v", saved)
	}
	if !issue3934Choose(m, ComposerEnterNewline) || len(saved) != 1 {
		t.Fatalf("choosing the current mode wrote again: %v", saved)
	}
	if issue3934Choose(m, "enter") || issue3934Choose(Model{}, ComposerEnterSends) {
		t.Fatal("an unknown mode, or a page that cannot keep it, was accepted")
	}
}
