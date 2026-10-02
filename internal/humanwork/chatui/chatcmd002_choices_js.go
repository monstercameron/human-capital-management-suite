//go:build js && wasm

package chatui

import "syscall/js"

// chatcmd002CheckedChoices reads which options of an anonymous poll are chosen
// on the page. The choice lives in the native radio or checkbox until Cast vote
// is pressed, because a ballot that cannot be changed should not be sent by the
// press that only selects it. The same message can be drawn twice (the timeline
// and its thread), so an option is reported once.
func chatcmd002CheckedChoices(post string) []string {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil
	}
	boxes := doc.Call("querySelectorAll", "input[data-chatcmd002-choice]")
	seen := map[string]bool{}
	var out []string
	for i := 0; i < boxes.Length(); i++ {
		box := boxes.Index(i)
		if box.Get("dataset").Get("chatcmd002Choice").String() != post || !box.Get("checked").Truthy() {
			continue
		}
		if id := box.Get("value").String(); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// chatcmd002RevealMessage brings a card's message into view and puts the
// keyboard on its first control. A message outside the loaded part of the
// conversation is not on the page, and the press then does nothing.
func chatcmd002RevealMessage(post string) {
	// CHATBUG-014: a windowed timeline may not have drawn the row; it is asked
	// to, and the row is looked for again once it has (virtual_js.go).
	revealTimelineRow(post, func() bool { return chatcmd002RevealNow(post) })
}

// chatcmd002RevealNow does the reveal when the message's row is on the page and
// reports whether it was.
func chatcmd002RevealNow(post string) bool {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return false
	}
	rows := doc.Call("querySelectorAll", ".chat-main [data-message-id]")
	for i := 0; i < rows.Length(); i++ {
		row := rows.Index(i)
		if row.Get("dataset").Get("messageId").String() != post {
			continue
		}
		row.Call("scrollIntoView", map[string]any{"block": "center"})
		if control := row.Call("querySelector", ".chatcmd002-card button:not([disabled]),.chatcmd002-card input:not([disabled])"); control.Truthy() {
			control.Call("focus", map[string]any{"preventScroll": true})
		}
		return true
	}
	return false
}
