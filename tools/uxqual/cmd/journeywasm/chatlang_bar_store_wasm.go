//go:build js && wasm

package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// The translation bar's dismissal (CHATLANG-008) is a presentation hint kept for
// this browser session through the same adapter as the history watermark and the
// chat visit ranking: a viewer-scoped key, a closed value, and every read and
// write checked by productclient before it reaches Web Storage.
func init() {
	chatui.BarDismissalStore.Dismissed = chatlangBarDismissedInSession
	chatui.BarDismissalStore.Remember = chatlangRememberBarDismissedInSession
}

// chatlangBarNoticeKey is the storage key of the signed-in viewer, or "" when
// the viewer is not known yet.
func chatlangBarNoticeKey() string {
	model := chatBrowser.snapshot()
	return productclient.ChatNoticesStorageKey(chatVisitScopeDigest(model.CurrentTenantID, model.CurrentUser))
}

func chatlangBarDismissedInSession() bool {
	key := chatlangBarNoticeKey()
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
	notice, valid := productclient.DecodeChatNotices(raw)
	return valid && notice == productclient.ChatNoticeTranslationBar
}

func chatlangRememberBarDismissedInSession() {
	key := chatlangBarNoticeKey()
	value, ok := productclient.EncodeChatNotices(productclient.ChatNoticeTranslationBar)
	if key == "" || !ok {
		return
	}
	if storage, available := browserSessionStorage(); available {
		browserStorageSet(storage, key, value)
	}
}
