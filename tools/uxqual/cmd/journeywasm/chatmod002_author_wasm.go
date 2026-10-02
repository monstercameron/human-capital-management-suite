//go:build js && wasm

package main

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatmod002Failed is chatActionFailed for a surface that submits text. A
// language-filter refusal never becomes the generic "We couldn't ..." notice:
//
//   - with a key, the text stays where it is and one line under that field names
//     the words (key is a chatui.ModAuthorKey* value for the field);
//   - without a key, the one sentence goes to the notice bar.
//
// Any other failure takes the usual notice. It reports whether err was a failure.
func chatmod002Failed(action string, err error, conversationID, key, surface, text string) bool {
	if err == nil {
		return false
	}
	// Whatever the failure, a message that did not send keeps its text in its
	// box. The page write comes last, after the render the model change asks for.
	if parent, isBox := chatmod002KeyParent(key); isBox {
		if box := chatBrowser.keepFailedText(conversationID, parent, text); box != "" {
			defer chatui.RestoreFieldValue(box, text)
		}
	}
	blocked, words := chatmod002BlockedWords(err, text)
	if !blocked {
		return chatActionFailed(action, err)
	}
	if key == "" {
		noteChatAction(chatui.ModAuthorSentence(chatBrowser.localeTag(), surface, words))
		return true
	}
	if chatBrowser.setAuthorBlocked(conversationID, key, chatui.AuthorBlocked{Surface: surface, Words: words, Text: text, Stamp: time.Now().UnixNano()}) {
		refreshChatRoute()
	}
	return true
}
