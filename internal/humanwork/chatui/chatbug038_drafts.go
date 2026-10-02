package chatui

import "sync"

// CHATBUG-038. The composer's previews are computed from the draft, so a draft
// that comes back after it was sent or cleared brings its previews back with it
// while the textarea (which the field sync keeps empty after a send) shows
// nothing, and the send button stays disabled.
//
// It came back through browserDrafts.prepare. A send clears the draft store and
// then asks the workspace to render again; that render is made from the model
// the page last passed in, which still carries the text as of the page's last
// render (the sent text, or an earlier part of it), and prepare adopted a model
// draft the store did not hold. The store now keeps a tombstone of what was
// sent or cleared, and prepare refuses those texts until a render shows the
// draft empty or the reader types something else.

var clearedDrafts = struct {
	sync.Mutex
	byConversation map[string][]string
}{byConversation: map[string][]string{}}

// rememberClearedDraft records the texts a conversation's draft was known by
// when it was sent or cleared: the store's own copy and the model's copy of
// the last render.
func rememberClearedDraft(conversationID string, bodies ...string) {
	if conversationID == "" {
		return
	}
	var kept []string
	for _, body := range bodies {
		if body != "" {
			kept = append(kept, body)
		}
	}
	if len(kept) == 0 {
		return
	}
	clearedDrafts.Lock()
	clearedDrafts.byConversation[conversationID] = kept
	clearedDrafts.Unlock()
}

// ForgetClearedDraft lets a draft come back on purpose: a send that failed
// puts its text back in the composer, and that text is not stale.
func ForgetClearedDraft(conversationID string) {
	clearedDrafts.Lock()
	delete(clearedDrafts.byConversation, conversationID)
	clearedDrafts.Unlock()
}

func forgetAllClearedDrafts() {
	clearedDrafts.Lock()
	clear(clearedDrafts.byConversation)
	clearedDrafts.Unlock()
}

// staleClearedDraft reports whether draft is one of the texts this
// conversation had cleared. An empty draft, or a different one, ends the
// tombstone.
func staleClearedDraft(conversationID, draft string) bool {
	clearedDrafts.Lock()
	defer clearedDrafts.Unlock()
	cleared, ok := clearedDrafts.byConversation[conversationID]
	if !ok {
		return false
	}
	for _, body := range cleared {
		if draft == body {
			return true
		}
	}
	delete(clearedDrafts.byConversation, conversationID)
	return false
}
