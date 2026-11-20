package chatui

import (
	"strings"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issues 3953 and 3950. A draft is saved to the person's own account, so it
// can be written from two tabs, and a write can fail while the backend is away.
// Both used to be silent: the later write replaced the earlier text for good,
// and edits made during an outage stayed on screen but never reached the
// account. The page keeps the other version beside the composer so the person
// chooses (nothing is sent or replaced on its own), and says plainly when the
// latest edits are not saved yet, with a way to try again.

// ChatIssue3953Styles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatIssue3953Styles = `
.chat-workspace .composer-draft-sync{display:flex;flex-direction:column;gap:6px;margin:0;padding:6px 8px;font-size:.8125rem;color:var(--hcm-color-text-muted)}
.chat-workspace .composer-draft-sync p{margin:0;min-width:0;overflow-wrap:anywhere}
.chat-workspace .composer-draft-other{max-height:6.5em;overflow:auto;padding:4px 8px;border-inline-start:3px solid var(--hcm-color-border);color:var(--hcm-color-text);white-space:pre-wrap}
.chat-workspace .composer-draft-sync-actions{display:flex;flex-wrap:wrap;gap:6px}
.chat-workspace .composer-draft-sync-actions .button{min-height:32px}
@media (pointer:coarse){.chat-workspace .composer-draft-sync-actions .button{min-height:44px;min-width:44px}}
`

const (
	actionDraftConflictKeep = "draft-conflict-keep"
	actionDraftConflictUse  = "draft-conflict-use"
	actionDraftConflictBoth = "draft-conflict-both"
	actionDraftSaveRetry    = "draft-save-retry"

	keyIssue3953Conflict = "chat.issue3953.conflict"
	keyIssue3953Other    = "chat.issue3953.other_label"
	keyIssue3953Keep     = "chat.issue3953.keep"
	keyIssue3953Use      = "chat.issue3953.use"
	keyIssue3953Both     = "chat.issue3953.both"
	keyIssue3950Unsaved  = "chat.issue3950.unsaved"
	keyIssue3950Retry    = "chat.issue3950.retry"

	// issue3953PreviewRunes bounds how much of the other version is drawn.
	issue3953PreviewRunes = 600
)

var issue3953Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3953Conflict: "Another tab or device saved a different draft for this conversation. Nothing was replaced. Choose which to keep.",
		keyIssue3953Other:    "The other draft",
		keyIssue3953Keep:     "Keep mine",
		keyIssue3953Use:      "Use the other draft",
		keyIssue3953Both:     "Keep both",
		keyIssue3950Unsaved:  "Your latest changes are not saved yet. We will keep trying. Please stay on this page.",
		keyIssue3950Retry:    "Save now",
	},
	"de-DE": {
		keyIssue3953Conflict: "Ein anderer Tab oder ein anderes Gerät hat einen anderen Entwurf für diese Unterhaltung gespeichert. Nichts wurde ersetzt. Wähle, welchen du behalten möchtest.",
		keyIssue3953Other:    "Der andere Entwurf",
		keyIssue3953Keep:     "Meinen behalten",
		keyIssue3953Use:      "Anderen Entwurf verwenden",
		keyIssue3953Both:     "Beide behalten",
		keyIssue3950Unsaved:  "Deine letzten Änderungen sind noch nicht gespeichert. Wir versuchen es weiter. Bitte bleibe auf dieser Seite.",
		keyIssue3950Retry:    "Jetzt speichern",
	},
	"ar": {
		keyIssue3953Conflict: "حفظ تبويب أو جهاز آخر مسودة مختلفة لهذه المحادثة. لم يتم استبدال أي شيء. اختر المسودة التي تريد الاحتفاظ بها.",
		keyIssue3953Other:    "المسودة الأخرى",
		keyIssue3953Keep:     "الاحتفاظ بمسودتي",
		keyIssue3953Use:      "استخدام المسودة الأخرى",
		keyIssue3953Both:     "الاحتفاظ بالاثنتين",
		keyIssue3950Unsaved:  "لم تُحفظ آخر تغييراتك بعد. سنواصل المحاولة. يرجى البقاء في هذه الصفحة.",
		keyIssue3950Retry:    "احفظ الآن",
	},
}

// issue3953Resolution is the composer text after the person's choice. Keeping
// mine changes nothing; using the other replaces; keeping both puts the other
// version below mine, so neither is lost.
func issue3953Resolution(action, mine, other string) (string, bool) {
	switch action {
	case actionDraftConflictKeep:
		return mine, true
	case actionDraftConflictUse:
		return other, true
	case actionDraftConflictBoth:
		if strings.TrimSpace(mine) == "" {
			return other, true
		}
		return strings.TrimRight(mine, " \t\r\n") + "\n\n" + other, true
	}
	return mine, false
}

// draftSyncAction handles the buttons of the notes below. It never sends.
func draftSyncAction(m Model, action string) bool {
	cb := m.Callbacks
	switch action {
	case actionDraftSaveRetry:
		if cb.RetryDraftSave != nil {
			cb.RetryDraftSave()
		}
		// The note leaves once the write succeeds; focus must not fall to the page.
		focusField("chat-composer")
		return true
	case actionDraftConflictKeep, actionDraftConflictUse, actionDraftConflictBoth:
		other := m.DraftConflicts[m.SelectedID]
		if m.SelectedID == "" || strings.TrimSpace(other) == "" {
			return true
		}
		if text, _ := issue3953Resolution(action, m.Draft, other); text != m.Draft {
			if cb.DraftChanged != nil {
				cb.DraftChanged(m.SelectedID, text)
			}
			replaceComposerText("chat-composer", text, len(utf16.Encode([]rune(text))))
		}
		if cb.DraftConflictResolved != nil {
			cb.DraftConflictResolved(m.SelectedID)
		}
		// The note closes under the pressed button: focus goes back to the draft.
		focusField("chat-composer")
		return true
	}
	return false
}

func issue3953Preview(text string) string {
	runes := []rune(text)
	if len(runes) <= issue3953PreviewRunes {
		return text
	}
	return string(runes[:issue3953PreviewRunes]) + "…"
}

// draftSyncNote is the line (or two) under the main composer for a different
// draft saved elsewhere and for edits that are not saved yet. It is nil when
// neither applies.
func draftSyncNote(m Model, target string) ui.Node {
	if target != "chat-composer" {
		return nil
	}
	var notes []ui.Node
	if other := m.DraftConflicts[m.SelectedID]; m.SelectedID != "" && strings.TrimSpace(other) != "" && other != m.Draft {
		button := func(action, key string) ui.Node {
			return html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": action}, Text: laneText(m, issue3953Copy, key)})
		}
		notes = append(notes, html.Div(html.Props{Class: "composer-draft-sync conflict", Role: "group", Aria: map[string]string{"label": laneText(m, issue3953Copy, keyIssue3953Other)}},
			html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: laneText(m, issue3953Copy, keyIssue3953Conflict)}),
			html.P(html.Props{Class: "composer-draft-other", Dir: "auto", Text: issue3953Preview(other)}),
			html.Div(html.Props{Class: "composer-draft-sync-actions"},
				button(actionDraftConflictKeep, keyIssue3953Keep), button(actionDraftConflictUse, keyIssue3953Use), button(actionDraftConflictBoth, keyIssue3953Both))))
	}
	if m.DraftUnsaved {
		notes = append(notes, html.Div(html.Props{Class: "composer-draft-sync unsaved"},
			html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: laneText(m, issue3953Copy, keyIssue3950Unsaved)}),
			html.Div(html.Props{Class: "composer-draft-sync-actions"},
				html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": actionDraftSaveRetry}, Text: laneText(m, issue3953Copy, keyIssue3950Retry)}))))
	}
	if len(notes) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "composer-draft-sync-slot"}, notes...)
}
