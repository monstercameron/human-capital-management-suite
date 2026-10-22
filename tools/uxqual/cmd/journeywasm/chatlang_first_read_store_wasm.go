//go:build js && wasm

package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// The first-read translation notice is answered once per browser session through
// the same adapter as the translation bar's dismissal: a viewer-scoped key, a
// closed value, and every read and write checked by productclient before it
// reaches Web Storage.
func init() {
	chatui.FirstReadNoticeStore.Seen = chatlangFirstReadSeenInSession
	chatui.FirstReadNoticeStore.Remember = chatlangRememberFirstReadInSession
}

func chatlangFirstReadKey() string {
	model := chatBrowser.snapshot()
	return productclient.ChatFirstReadStorageKey(chatVisitScopeDigest(model.CurrentTenantID, model.CurrentUser))
}

func chatlangFirstReadSeenInSession() bool {
	key := chatlangFirstReadKey()
	if key == "" {
		return false
	}
	storage, ok := browserSessionStorage()
	if !ok {
		return false
	}
	raw, ok := browserStorageGet(storage, key)
	if !ok {
		return false
	}
	notice, valid := productclient.DecodeChatFirstRead(raw)
	return valid && notice == productclient.ChatNoticeFirstRead
}

func chatlangRememberFirstReadInSession() {
	key := chatlangFirstReadKey()
	value, ok := productclient.EncodeChatFirstRead(productclient.ChatNoticeFirstRead)
	if key == "" || !ok {
		return
	}
	if storage, available := browserSessionStorage(); available {
		browserStorageSet(storage, key, value)
	}
}
