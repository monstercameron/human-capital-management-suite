package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// chatEmojiHost is the picker's live connection to the workspace: where its state
// is read and written, the model whose callbacks add a reaction, and the person's
// preferences. The workspace refreshes it on every render, so a handler that runs
// between renders still reads the latest model. Tests set the read and apply
// fields directly.
type chatEmojiHostT struct {
	read     func() localUI
	apply    func(func(*localUI))
	quiet    func(func(*localUI)) // writes without asking for a render
	model    Model
	prefs    emojiPrefs
	prefsKey string
	// dirty is whether prefs holds a change the server has not been given;
	// serverSeen is the encoded copy last exchanged with it (read from the page
	// model or written back), so a render of that same copy changes nothing.
	dirty      bool
	serverSeen string
	gate       emojiSaveGate
	// layerRender redraws the emoji layers component alone; nil until it is on the page.
	layerRender func()
	layerSeq    uint64
	// clock and after are the time and the one-shot timer; tests replace them.
	clock func() time.Time
	after func(time.Duration, func())
	room  string
	// lastPicker is the message the reaction picker was last open for, so a
	// newly opened one starts a load that an earlier failure left undone.
	lastPicker string
}

var chatEmojiHost chatEmojiHostT

// bind refreshes the host from the current render.
func (h *chatEmojiHostT) bind(model Model, local localStore) {
	h.read, h.apply, h.model = local.get, local.update, model
	h.quiet = func(change func(*localUI)) { change(local.box) }
	if key := emojiPrefsKey(model.CurrentTenantID, model.CurrentUser); key != h.prefsKey {
		h.prefsKey = key
		h.prefs, h.dirty, h.serverSeen, h.gate = emojiPrefs{}, false, "", emojiSaveGate{}
	}
	// The server's copy arrives with the page model (and again if another device
	// changed it): it joins what this page already holds.
	// An empty or unreadable copy says nothing and changes nothing.
	if raw := model.EmojiPrefs; raw != "" && raw != h.serverSeen {
		h.serverSeen = raw
		if server, ok := parseEmojiPrefs(raw); ok {
			h.prefs = mergeEmojiPrefs(server, h.prefs, h.dirty)
		}
	}
	// A picker belongs to the conversation it was opened in, and a reaction picker
	// belongs to the model's PickerID; neither survives the thing it belongs to.
	stored := local.get().emoji
	if h.room != model.SelectedID {
		h.room = model.SelectedID
		if stored.Open && !stored.Reaction {
			local.box.emoji = emojiPickerState{}
		}
	}
	if stored.Open && stored.Reaction && model.PickerID != stored.Target {
		local.box.emoji = emojiPickerState{}
	}
	if model.PickerID != h.lastPicker {
		h.lastPicker = model.PickerID
		if model.PickerID != "" {
			// A reaction picker reads the screen once when it opens, like the
			// composer's: touch cells, and a sheet at phone width.
			touch, sheet := chatEmojiEnvironment()
			local.box.emoji = emojiOpenState(model.PickerID, true, touch, sheet)
			chatEmojiEnsureData(true)
		}
	} else if model.PickerID != "" {
		chatEmojiEnsureData(false)
	}
}

func (h *chatEmojiHostT) state() localUI {
	if h.read == nil {
		return localUI{}
	}
	return h.read()
}

// write changes the picker's state and redraws the emoji layers alone: a typed
// letter, a hover, a scroll or the arrival of the data is not a reason to draw
// the whole workspace again.
func (h *chatEmojiHostT) write(change func(*localUI)) {
	if h.layerRender != nil && h.quiet != nil {
		h.quiet(change)
		h.layerRender()
		return
	}
	h.writeFull(change)
}

// writeFull also redraws the workspace, whose emoji buttons show whether their
// picker is open. Opening and closing use it; the layers are redrawn first, so
// the picker does not wait for the page.
func (h *chatEmojiHostT) writeFull(change func(*localUI)) {
	if h.apply == nil {
		return
	}
	if h.layerRender != nil && h.quiet != nil {
		h.quiet(change)
		h.layerRender()
		h.apply(func(*localUI) {})
		return
	}
	h.apply(change)
}

// current is the picker that is open, if any: the reaction picker when the
// message model has one open, else the composer's.
func (h *chatEmojiHostT) current() (emojiPickerState, bool) {
	stored := h.state().emoji
	if h.model.PickerID != "" {
		return emojiStateFor(stored, h.model.PickerID, true), true
	}
	if stored.Open && !stored.Reaction {
		return stored, true
	}
	return emojiPickerState{}, false
}

func (h *chatEmojiHostT) view(st emojiPickerState) emojiView {
	return buildEmojiView(st, chatEmojiData.index, h.prefs)
}

// mutate applies a state transition to the open picker.
func (h *chatEmojiHostT) mutate(change func(emojiPickerState, emojiView) emojiPickerState) {
	st, ok := h.current()
	if !ok {
		return
	}
	next := change(st, h.view(st))
	h.write(func(u *localUI) { u.emoji = next })
}

// savePrefs notes a change to the person's choices and has it written to the
// server: at once after a quiet spell, otherwise as one write when emojiSaveEvery
// has passed since the last.
func (h *chatEmojiHostT) savePrefs() {
	h.dirty = true
	writeNow, wait := h.gate.request(h.now())
	switch {
	case writeNow:
		h.flushPrefs()
	case wait > 0:
		h.later(wait, func() {
			h.gate.fired(h.now())
			h.flushPrefs()
		})
	}
}

// flushPrefs gives the server the page's choices if it has not got them. Without
// a save callback the choices stay on the page (and stay marked unsaved).
func (h *chatEmojiHostT) flushPrefs() {
	save := h.model.Callbacks.SaveEmojiPrefs
	if !h.dirty || save == nil {
		return
	}
	encoded := h.prefs.encode()
	if encoded == "" {
		return
	}
	h.dirty, h.serverSeen = false, encoded
	save(encoded)
}

func (h *chatEmojiHostT) now() time.Time {
	if h.clock != nil {
		return h.clock()
	}
	return time.Now()
}

func (h *chatEmojiHostT) later(wait time.Duration, run func()) {
	if h.after != nil {
		h.after(wait, run)
		return
	}
	time.AfterFunc(wait, run)
}

// record counts one use of an emoji. It is what makes "Frequently used" the
// person's own, and what the message bar's one-click reactions follow.
func (h *chatEmojiHostT) record(glyph string) {
	h.prefs = h.prefs.bump(glyph)
	h.savePrefs()
}

// chatEmojiToggle opens the composer's picker, or closes it when it is open.
func chatEmojiToggle(target string) {
	h := &chatEmojiHost
	stored := h.state().emoji
	if stored.Open && !stored.Reaction && stored.Target == target {
		chatEmojiClose(true)
		return
	}
	// One floating layer at a time: the reaction picker and the message menu give way.
	if h.model.PickerID != "" && h.model.Callbacks.OpenPicker != nil {
		h.model.Callbacks.OpenPicker("")
	}
	if h.model.MenuID != "" && h.model.Callbacks.OpenMenu != nil {
		h.model.Callbacks.OpenMenu("")
	}
	touch, sheet := chatEmojiEnvironment()
	h.writeFull(func(u *localUI) { u.emoji = emojiOpenState(target, false, touch, sheet) })
	chatEmojiEnsureData(true)
}

// chatEmojiClose closes the open picker. restoreFocus returns the cursor to the
// button that opened it.
func chatEmojiClose(restoreFocus bool) {
	h := &chatEmojiHost
	st, ok := h.current()
	if !ok {
		return
	}
	h.writeFull(func(u *localUI) { u.emoji = emojiPickerState{} })
	if st.Reaction {
		if h.model.PickerID != "" && h.model.Callbacks.OpenPicker != nil {
			h.model.Callbacks.OpenPicker("")
		}
		if restoreFocus {
			restoreChatLayerFocus("reaction")
		}
		return
	}
	if restoreFocus {
		restoreChatLayerFocus("emoji")
	}
}

// chatEmojiSetQuery follows the search field.
func chatEmojiSetQuery(query string) {
	// Typing into a picker whose data failed to load tries the load again.
	if chatEmojiData.status == emojiDataFailed {
		chatEmojiEnsureData(true)
	}
	chatEmojiHost.mutate(func(st emojiPickerState, _ emojiView) emojiPickerState { return emojiQuery(st, query) })
}

// chatEmojiHover records the emoji under the pointer (1-based; 0 for none).
func chatEmojiHover(pos int) {
	st, ok := chatEmojiHost.current()
	if !ok || st.Hover == pos {
		return
	}
	chatEmojiHost.mutate(func(st emojiPickerState, _ emojiView) emojiPickerState { st.Hover = pos; return st })
}

func chatEmojiTab(group int) {
	chatEmojiHost.mutate(func(st emojiPickerState, v emojiView) emojiPickerState { return emojiCategory(st, v, group) })
}

func chatEmojiToneToggle() {
	chatEmojiHost.mutate(func(st emojiPickerState, _ emojiView) emojiPickerState { st.ToneOpen = !st.ToneOpen; return st })
}

// chatEmojiToneSet remembers a skin tone for the person; it applies to every
// emoji that has tones.
func chatEmojiToneSet(tone int) {
	h := &chatEmojiHost
	if tone < 0 || tone > emojiset.Tones {
		return
	}
	h.prefs.Tone = tone
	h.savePrefs()
	h.mutate(func(st emojiPickerState, _ emojiView) emojiPickerState { return emojiTone(st) })
}

// chatEmojiScrolled follows the grid's scroll. A render happens only when the
// rows in the page change; otherwise the position is only remembered.
func chatEmojiScrolled(scroll, viewH float64) {
	h := &chatEmojiHost
	st, ok := h.current()
	if !ok {
		return
	}
	next, render := emojiScrolled(st, h.view(st), scroll, viewH)
	next.ScrollSet = st.ScrollSet && !render
	if !render {
		// Remembered silently: the DOM is already where the state says.
		h.silent(func(box *localUI) { box.emoji = next })
		return
	}
	h.write(func(u *localUI) { u.emoji = next })
}

// silent changes the stored state without asking for a render.
func (h *chatEmojiHostT) silent(change func(*localUI)) {
	if h.quiet != nil {
		h.quiet(change)
	}
}

// chatEmojiKey is the keyboard model. It reports whether the key was handled.
// inInput is whether the key went to the search field, where Left and Right are
// the text cursor's unless the field is empty.
func chatEmojiKey(key string, shift, inInput, rtl bool) bool {
	h := &chatEmojiHost
	st, ok := h.current()
	if !ok {
		return false
	}
	switch key {
	case "ArrowDown", "ArrowUp", "PageDown", "PageUp":
	case "ArrowLeft", "ArrowRight":
		if inInput && strings.TrimSpace(st.Query) != "" {
			return false
		}
	case "Enter":
		v := h.view(st)
		cell, ok := v.cell(max(0, min(st.Active, v.seq.len()-1)))
		if !ok {
			return true
		}
		chatEmojiChoose(cell.Glyph, shift)
		return true
	default:
		return false
	}
	h.mutate(func(st emojiPickerState, v emojiView) emojiPickerState { return emojiNavigate(st, v, key, rtl) })
	return true
}

// chatEmojiChoose is a choice made from the keyboard: a reaction is added and the
// picker closes; in the composer the emoji goes in at the caret and the picker
// closes, unless Shift keeps it open for more.
func chatEmojiChoose(glyph string, keepOpen bool) {
	h := &chatEmojiHost
	st, ok := h.current()
	if !ok || glyph == "" {
		return
	}
	h.record(glyph)
	if st.Reaction {
		if h.model.Callbacks.ReactWith != nil {
			h.model.Callbacks.ReactWith(st.Target, glyph)
		}
		chatEmojiClose(false)
		return
	}
	if chatEmojiInsert != nil {
		chatEmojiInsert(st.Target, glyph, keepOpen)
	} else {
		insertComposerEmoji(st.Target, glyph, keepOpen)
	}
	if !keepOpen {
		h.writeFull(func(u *localUI) { u.emoji = emojiPickerState{} })
	}
}

// chatEmojiInsert, when set, takes the place of putting the emoji into the
// composer's field. Tests set it to see what would have been inserted.
var chatEmojiInsert func(target, glyph string, keepOpen bool)

// chatEmojiEscape is Escape: a typed query is cleared first, the picker closes
// second. It reports whether the search field itself must be emptied.
func chatEmojiEscape() (clearedQuery bool) {
	st, ok := chatEmojiHost.current()
	if !ok {
		return false
	}
	if strings.TrimSpace(st.Query) != "" {
		chatEmojiSetQuery("")
		return true
	}
	chatEmojiClose(true)
	return false
}

// chatEmojiAction handles the picker's own delegated actions (the composer
// button, a choice, a category, the skin-tone control). It reports whether the
// action was one of them.
func chatEmojiAction(action, id, extra string, shift bool) bool {
	switch action {
	case "emoji-toggle":
		chatEmojiToggle(id)
	case "emoji-insert":
		chatEmojiChoose(extra, shift)
	case "emoji-tab":
		if group, ok := atoiOK(extra); ok {
			chatEmojiTab(group)
		}
	case "emoji-tone-toggle":
		chatEmojiToneToggle()
	case "emoji-tone":
		if tone, ok := atoiOK(extra); ok {
			chatEmojiToneSet(tone)
		}
	default:
		return false
	}
	return true
}

func atoiOK(s string) (int, bool) {
	n, neg := 0, false
	for i, r := range s {
		switch {
		case i == 0 && r == '-':
			neg = true
		case r >= '0' && r <= '9':
			n = n*10 + int(r-'0')
		default:
			return 0, false
		}
	}
	if s == "" || s == "-" {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}

// chatEmojiEnsureData loads the reader's language (and English) the first time a
// picker opens, and again if the language changed.
func chatEmojiEnsureData(retry bool) {
	lang := chatEmojiLang(chatEmojiHost.model.Locale)
	d := &chatEmojiData
	// A failed load is tried again when a picker is opened, never on every render.
	if d.status == emojiDataLoading || (d.status == emojiDataReady && d.lang == lang) || (d.status == emojiDataFailed && !retry) {
		return
	}
	d.status = emojiDataLoading
	chatEmojiStartLoad(lang)
}

// chatEmojiLoaded is called when a load finishes.
func chatEmojiLoaded(lang string, ix *emojiset.Index, err error) {
	if err != nil || ix == nil {
		// A load that fails while another already gave the data changes nothing.
		if chatEmojiData.index == nil {
			chatEmojiData.status = emojiDataFailed
		} else if chatEmojiData.status == emojiDataLoading {
			chatEmojiData.status = emojiDataReady
		}
	} else {
		chatEmojiData = chatEmojiDataStore{status: emojiDataReady, lang: lang, index: ix}
	}
	chatEmojiHost.write(func(*localUI) {})
}
