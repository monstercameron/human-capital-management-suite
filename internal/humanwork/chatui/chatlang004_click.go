package chatui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// The page-session state of this feature changes through these pure steps, so
// each press is testable without a browser.

func (l chatlangLocal) toggleOriginal(id string) chatlangLocal {
	next := map[string]bool{}
	for k, v := range l.original {
		next[k] = v
	}
	next[id] = !next[id]
	l.original = next
	return l
}

func (l chatlangLocal) toggleAll(room string) chatlangLocal {
	next := map[string]bool{}
	for k, v := range l.all {
		next[k] = v
	}
	next[room] = !next[room]
	l.all = next
	return l
}

func (l chatlangLocal) openFixer(id string) chatlangLocal {
	l.fixing, l.busy, l.failed = id, false, false
	return l
}

func (l chatlangLocal) saving() chatlangLocal {
	l.busy, l.failed = true, false
	return l
}

// saved ends a correction: the picker closes when the server accepted it and
// stays open, with a sentence, when it did not.
func (l chatlangLocal) saved(err error) chatlangLocal {
	l.busy, l.failed = false, err != nil
	if err == nil {
		l.fixing = ""
	}
	return l
}

// chatlangMessageRevision finds the revision of a message the page shows, for
// the writer's correction of its language.
func chatlangMessageRevision(m Model, id string) (uint64, bool) {
	for _, list := range [][]Message{m.Messages, m.ThreadMessages} {
		for _, msg := range list {
			if msg.ID == id {
				return msg.Revision, true
			}
		}
	}
	if m.ThreadParent != nil && m.ThreadParent.ID == id {
		return m.ThreadParent.Revision, true
	}
	return 0, false
}

// chatlangClick handles the data-action buttons of this feature and reports
// whether the action was one of them.
func chatlangClick(m Model, local localStore, action, id, extra string) bool {
	if !strings.HasPrefix(action, "chatlang-") {
		return false
	}
	switch action {
	case "chatlang-original":
		local.update(func(u *localUI) { u.chatlang = u.chatlang.toggleOriginal(id) })
	case "chatlang-originals":
		local.update(func(u *localUI) { u.chatlang = u.chatlang.toggleAll(id) })
	case "chatlang-settings":
		chatlangOpenSettings()
	case "chatlang-bar-dismiss":
		chatlangRememberBarDismissed()
		local.update(func(u *localUI) { u.chatlang.barDismissed = true })
	case "chatlang-fix":
		if m.Callbacks.OpenMenu != nil {
			m.Callbacks.OpenMenu("")
		}
		local.update(func(u *localUI) { u.chatlang = u.chatlang.openFixer(id) })
		// The menu item that opened the editor is gone with its menu, so focus
		// would fall to the page; move it into the editor.
		focusChatLanguageEditor(id)
	case "chatlang-reword-edit", "chatlang-reword-send":
		chattoneRewordClick(m, local, action, id)
	case "chatlang-report-open", "chatlang-report-cancel", "chatlang-report-send", "chatlang-notice-show", "chatlang-notice-ok":
		chatlangW10Click(m, local, action, id)
	case "chatlang-fix-cancel":
		local.update(func(u *localUI) { u.chatlang = u.chatlang.openFixer("") })
		// The editor leaves the page; focus returns to the message's More actions.
		focusMessageAfterEdit(id)
	case "chatlang-correct":
		revision, ok := chatlangMessageRevision(m, id)
		if !ok || !chatrender.Supported(extra) {
			return true
		}
		local.update(func(u *localUI) { u.chatlang = u.chatlang.saving() })
		chatlangCorrect(m.SelectedID, id, revision, extra, func(err error) {
			ui.PostAsync(func() {
				local.update(func(u *localUI) { u.chatlang = u.chatlang.saved(err) })
				if err == nil {
					// The editor closes on success; focus returns to the message.
					focusMessageAfterEdit(id)
				}
			})
		})
	}
	return true
}

// CorrectLanguage tells the server which language the writer says their own
// message is in. The server accepts it only from the message's author and then
// drops the translations made from the wrong guess.
func (c ReadingSettingsClient) CorrectLanguage(ctx context.Context, room, post string, revision uint64, language string) error {
	body, err := json.Marshal(struct {
		Message  string `json:"message"`
		Revision uint64 `json:"revision"`
		Language string `json:"language"`
	}{post, revision, language})
	if err != nil {
		return err
	}
	response, err := c.requestEndpoint(ctx, http.MethodPost, "correct-language", room, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return chatrender.ErrUnavailable
	}
	return nil
}

// chatlangEditorEscape reports whether a key press closes the language editor:
// Escape, pressed inside an editor that is open.
func chatlangEditorEscape(l chatlangLocal, key string, inEditor bool) bool {
	return key == "Escape" && l.fixing != "" && inEditor
}

// chatlangEditorKey closes an open language editor on Escape, the same way
// Cancel does, and returns focus to the message. It reports whether it handled
// the press.
func chatlangEditorKey(local localStore, e ui.KeyboardEvent) bool {
	state := local.get().chatlang
	if !chatlangEditorEscape(state, e.GetKey(), chatlangEventInEditor(e)) {
		return false
	}
	id := state.fixing
	local.update(func(u *localUI) { u.chatlang = u.chatlang.openFixer("") })
	focusMessageAfterEdit(id)
	return true
}
