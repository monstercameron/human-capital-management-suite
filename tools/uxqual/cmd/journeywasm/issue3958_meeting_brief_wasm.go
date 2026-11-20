//go:build js && wasm

package main

import (
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3958. The Saved panel's select mode and meeting brief. State is held on
// the panel's object only; closing the panel forgets all of it.

func (b *chatsaveBrowser) briefDoc() js.Value { return js.Global().Get("document") }

func (b *chatsaveBrowser) briefField(id string) string {
	if field := b.briefDoc().Call("getElementById", id); field.Truthy() {
		return field.Get("value").String()
	}
	return ""
}

// briefReset forgets the selection and any open brief.
func (b *chatsaveBrowser) briefReset() {
	b.selecting, b.selected, b.brief, b.afterSync = false, nil, nil, nil
}

// briefDecorate adds the select mode and the open brief to the panel's view.
func (b *chatsaveBrowser) briefDecorate(view *chatui.SavedMessagesView, snapshot chatui.Model, cfg journeyclient.Config) {
	count := 0
	for _, on := range b.selected {
		if on {
			count++
		}
	}
	view.Select = chatui.SavedSelectView{On: b.selecting, Selected: b.selected, Count: count}
	if b.brief == nil {
		return
	}
	rows := b.brief.rows
	if b.brief.err == "" {
		rows = chatsaveBriefRows(b.page, b.selected, cfg, snapshot)
		b.brief.rows = rows
	}
	view.Brief = b.brief.view(rows, snapshot)
}

// briefClick handles the select and brief actions of a press and says whether
// it did.
func (b *chatsaveBrowser) briefClick(action string, host, conversationID, postID string) bool {
	switch action {
	case "select-start":
		b.selecting, b.selected = true, map[string]bool{}
		b.reminderMenu, b.editingNote, b.editingLabels, b.pickingDate = "", "", "", false
		b.focusID = "chatsave-select-stop"
		b.render()
	case "select-stop":
		b.selecting, b.selected = false, nil
		b.focusID = "chatsave-select-start"
		b.render()
	case "select-toggle":
		b.selected = chatsaveToggleSelection(b.selected, chatui.SavedItemKey(host, conversationID, postID))
		b.render()
	case "brief-open":
		b.briefOpen()
	case "brief-back":
		b.briefBack()
	case "brief-retry":
		if b.brief == nil {
			return true
		}
		b.refresh = chatsaveRefresh{}
		b.errorCode, b.commandFailed, b.listDown = "", false, false
		b.brief.err = ""
		b.briefRecheck(nil)
	case "brief-setup":
		if b.brief == nil {
			return true
		}
		b.brief.stage, b.brief.problems, b.brief.status = "setup", nil, ""
		b.focusID = "chatsave-brief-purpose"
		b.render()
	case "brief-notes":
		if b.brief != nil {
			b.briefRemember()
			b.brief.includeNotes = !b.brief.includeNotes
			b.render()
		}
	case "brief-sharenotes":
		if b.brief != nil {
			b.brief.text = b.briefField("chatsave-brief-text")
			b.brief.shareNotes = !b.brief.shareNotes
			b.brief.problems, b.brief.status = nil, ""
			b.render()
		}
	case "brief-copy":
		b.briefCopy()
	case "brief-draft":
		b.briefDraft()
	default:
		return false
	}
	return true
}

// briefRemember keeps what is typed in the setup form.
func (b *chatsaveBrowser) briefRemember() {
	if b.brief == nil || b.brief.stage != "setup" {
		return
	}
	b.brief.purpose = b.briefField("chatsave-brief-purpose")
	b.brief.when = b.briefField("chatsave-brief-when")
}

// briefInput keeps the brief's fields as they change and reports whether the
// event was one of them.
func (b *chatsaveBrowser) briefInput(target js.Value) bool {
	if b.brief == nil {
		return false
	}
	switch target.Get("id").String() {
	case "chatsave-brief-purpose":
		b.brief.purpose = target.Get("value").String()
	case "chatsave-brief-when":
		b.brief.when = target.Get("value").String()
	case "chatsave-brief-text":
		b.brief.text = target.Get("value").String()
	case "chatsave-brief-target":
		b.brief.target = target.Get("value").String()
		if len(b.brief.problems) > 0 || b.brief.status != "" {
			// What was said about the last choice no longer applies.
			b.brief.problems, b.brief.status = nil, ""
			b.render()
		}
	default:
		return false
	}
	return true
}

func (b *chatsaveBrowser) briefConfig() (chatui.Model, []chatui.SavedMessageRow) {
	cfg := b.cfg
	cfg.Locale = b.locale()
	snapshot := chatBrowser.snapshot()
	return snapshot, chatsaveBriefRows(b.page, b.selected, cfg, snapshot)
}

func (b *chatsaveBrowser) briefOpen() {
	count := 0
	for _, on := range b.selected {
		if on {
			count++
		}
	}
	if count == 0 || count > chatui.BriefMaxSources {
		return
	}
	_, rows := b.briefConfig()
	b.brief = &chatsaveBriefState{stage: "setup", rows: rows}
	b.focusID = "chatsave-brief-purpose"
	b.briefRecheck(nil)
}

func (b *chatsaveBrowser) briefBack() {
	b.brief, b.afterSync = nil, nil
	b.focusID = "chatsave-prepare"
	b.render()
}

// briefRecheck reads the whole Saved list again, so the service decides what
// the person can still open, and then runs then. A failed read keeps the
// selection and reports the failure on the page.
func (b *chatsaveBrowser) briefRecheck(then func()) {
	state := b.brief
	if state == nil {
		return
	}
	state.checking, state.err = true, ""
	b.render()
	finish := func(err error) {
		if b.brief != state {
			return
		}
		state.checking = false
		if err != nil {
			state.err = chatsaveErrorCode(err)
		} else if then != nil {
			then()
		}
		b.render()
	}
	if b.listDown {
		finish(chatsaveError("unavailable"))
		return
	}
	b.refresh = chatsaveRefresh{}
	b.afterSync = finish
	b.sync()
}

// briefSubmit builds the agenda from the messages as they are now.
func (b *chatsaveBrowser) briefSubmit() {
	state := b.brief
	if state == nil || state.checking {
		return
	}
	b.briefRemember()
	b.briefRecheck(func() {
		snapshot, rows := b.briefConfig()
		state.rows = rows
		origin := ""
		if location := js.Global().Get("location"); location.Truthy() {
			origin = location.Get("origin").String()
		}
		result := chatui.BuildMeetingBrief(chatui.MeetingBriefInput{Locale: b.locale(), Purpose: state.purpose, When: state.when, Origin: origin, IncludeNotes: state.includeNotes, Rows: rows, Now: time.Now()})
		if len(result.Included) == 0 {
			return
		}
		state.stage, state.text, state.included, state.notesInText = "draft", result.Text, result.Included, result.NotesInText
		state.problems, state.status, state.shareNotes = nil, "", false
		if state.target == "" && snapshot.SelectedID != "" && chatsaveBriefTargetName(snapshot, snapshot.SelectedID) != "" {
			state.target = snapshot.SelectedID
		}
		b.focusID = "chatsave-brief-text"
	})
}

func (b *chatsaveBrowser) briefCopy() {
	state := b.brief
	if state == nil {
		return
	}
	state.text = b.briefField("chatsave-brief-text")
	navigator := js.Global().Get("navigator")
	if !navigator.Truthy() || !navigator.Get("clipboard").Truthy() || navigator.Get("clipboard").Get("writeText").Type() != js.TypeFunction {
		state.status = "copy_failed"
		b.render()
		return
	}
	promise := navigator.Get("clipboard").Call("writeText", state.text)
	var done, failed js.Func
	done = js.FuncOf(func(js.Value, []js.Value) any {
		done.Release()
		failed.Release()
		ui.PostAsync(func() {
			if !b.disposed && b.brief == state {
				state.status = "copied"
				b.render()
			}
		})
		return nil
	})
	failed = js.FuncOf(func(js.Value, []js.Value) any {
		done.Release()
		failed.Release()
		ui.PostAsync(func() {
			if !b.disposed && b.brief == state {
				state.status = "copy_failed"
				b.render()
			}
		})
		return nil
	})
	promise.Call("then", done, failed)
}

// briefDraft puts the agenda in the message box of the chosen conversation. It
// never sends. Access to every cited message is read again first, each cited
// message is checked against the chosen audience, and private notes go only if
// the person said so.
func (b *chatsaveBrowser) briefDraft() {
	state := b.brief
	if state == nil || state.checking {
		return
	}
	state.text = b.briefField("chatsave-brief-text")
	if target := b.briefField("chatsave-brief-target"); target != "" || state.target == "" {
		state.target = target
	}
	state.status, state.problems = "", nil
	snapshot := chatBrowser.snapshot()
	name := chatsaveBriefTargetName(snapshot, state.target)
	if name == "" {
		state.status = "choose"
		b.focusID = "chatsave-brief-target"
		b.render()
		return
	}
	target, text := state.target, state.text
	b.briefRecheck(func() {
		gone := make([]bool, len(state.included))
		for i, row := range state.included {
			item, ok := chatsaveFind(b.page, row.TenantID, row.ConversationID, row.PostID)
			gone[i] = !ok || item.Availability != "readable"
		}
		problems := chatui.MeetingBriefShareCheck(b.locale(), text, state.included, gone, target, state.shareNotes)
		if len(problems) > 0 {
			state.problems = problems
			b.focusID = "chatsave-brief-problems"
			return
		}
		callbacks := chatBrowser.snapshot().Callbacks
		if callbacks.DraftChanged != nil {
			callbacks.DraftChanged(target, chatsaveBriefDraftText(chatBrowser.draft(target), text))
		}
		state.status, state.draftedIn = "drafted", name
		if callbacks.SelectConversation != nil {
			if chatsaveNarrow() {
				b.close()
			}
			callbacks.SelectConversation(target)
		}
		noteChatAction(chatui.SavedBriefDraftedText(b.locale(), name))
	})
}
