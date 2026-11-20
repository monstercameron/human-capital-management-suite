//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatmapSyncRetention rewrites the duration hint for the chosen duration and
// the channel's longest keeping time (#3863). Live choices hide it: the live
// note explains them.
func chatmapSyncRetention(sheet js.Value) {
	if !sheet.Truthy() {
		return
	}
	hint := sheet.Call("querySelector", "[data-chatmap-retention]")
	if !hint.Truthy() {
		return
	}
	locale := sheet.Get("dataset").Get("locale").String()
	text := chatui.ChatmapDurationHint(locale, chatmapValue(sheet, "duration"), chatmapPolicy.MaxRetentionSeconds)
	hint.Set("textContent", text)
	hint.Set("hidden", text == "")
}

// chatmapLookupUnavailable reports whether address lookup has failed here.
func chatmapLookupUnavailable(sheet js.Value) bool {
	button := sheet.Call("querySelector", "[data-chatmap-action=lookup]")
	return button.Truthy() && button.Get("disabled").Bool()
}

// chatmapInvalidateAddress drops the point an earlier lookup resolved when the
// typed address changes: the edited address is unresolved until it is found
// again (#3862).
func (b *chatmapBrowser) chatmapInvalidateAddress(sheet js.Value) {
	for _, key := range []string{"lat", "lon", "accuracy", "label"} {
		chatmapSetValue(sheet, key, "")
	}
	b.places = nil
	if selector := chatmapField(sheet, "lookup"); selector.Truthy() {
		selector.Set("hidden", true)
		selector.Set("textContent", "")
	}
}

// closeStaleSheet closes and clears a Location sheet that is still open after
// the person moved to another conversation (#3908). Its audience, precision and
// note were chosen for the conversation it was opened in.
func (b *chatmapBrowser) closeStaleSheet() {
	if b.openedFor == "" {
		return
	}
	if !ChatmapSheetStale(b.openedFor, chatBrowser.snapshot().SelectedID) {
		return
	}
	b.openedFor = ""
	sheet := js.Global().Get("document").Call("querySelector", "[data-chatmap-sheet]:not([hidden])")
	if !sheet.Truthy() {
		return
	}
	b.resetSheet(sheet)
	chatui.CloseLocationComposer(sheet, false)
}

// resetSheet returns the sheet to how it first opens: the device source,
// approximate precision, the shortest duration, and no note, address or point.
func (b *chatmapBrowser) resetSheet(sheet js.Value) {
	for _, key := range []string{"lat", "lon", "accuracy", "label", "readable", "note", "siteid"} {
		chatmapSetValue(sheet, key, "")
	}
	chatmapSetValue(sheet, "precision", "approximate")
	chatmapSetValue(sheet, "duration", "hour")
	b.draft = ChatmapDraft{}
	b.places = nil
	if selector := chatmapField(sheet, "lookup"); selector.Truthy() {
		selector.Set("hidden", true)
		selector.Set("textContent", "")
	}
	choices := sheet.Call("querySelectorAll", "[data-chatmap-choice]")
	for i := 0; i < choices.Length(); i++ {
		choices.Index(i).Set("hidden", choices.Index(i).Get("dataset").Get("chatmapChoice").String() != "device")
	}
	buttons := sheet.Call("querySelectorAll", "[data-chatmap-action=source]")
	for i := 0; i < buttons.Length(); i++ {
		buttons.Index(i).Call("setAttribute", "aria-pressed", boolText(buttons.Index(i).Get("dataset").Get("source").String() == "device"))
	}
	chatmapSetValue(sheet, "source", "device")
	if send := sheet.Call("querySelector", "[data-chatmap-action=send]"); send.Truthy() {
		send.Set("disabled", true)
	}
	if image := sheet.Call("querySelector", "[data-chatmap-preview]"); image.Truthy() {
		image.Set("hidden", true)
	}
	b.status(sheet, "")
	chatmapSyncRetention(sheet)
}
