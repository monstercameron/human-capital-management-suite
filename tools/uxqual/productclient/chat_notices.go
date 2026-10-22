package productclient

// A dismissed Chat notice is a presentation hint of this browser session, like
// the history watermark and the chat visit ranking: it records that a reader
// closed an informational bar so the bar is not drawn again, and grants nothing.
// The record is viewer-scoped the way the visit ranking is, and its value is a
// closed list of notice names, so a copied storage object cannot carry anything
// else into the client (CHATLANG-008).
const (
	ChatNoticesStorageKeyPrefix = BrowserStateVersion + ".chat-notices."
	// ChatNoticeTranslationBar is the conversation bar that offers "Show
	// originals" and the language settings.
	ChatNoticeTranslationBar = "translation-bar"
)

// ChatNoticesStorageKey accepts only the SHA-256 digest of the current tenant
// and principal, so the dismissal is not shared between viewers and the key does
// not disclose either identity.
func ChatNoticesStorageKey(scopeDigest string) string {
	if len(scopeDigest) != 64 {
		return ""
	}
	for _, r := range scopeDigest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return ChatNoticesStorageKeyPrefix + scopeDigest
}

// EncodeChatNotices is the only supported dismissed-notice record. One notice is
// defined today, so the record is that notice's name.
func EncodeChatNotices(notice string) (string, bool) {
	if notice != ChatNoticeTranslationBar {
		return "", false
	}
	return notice, true
}

// DecodeChatNotices treats session storage as untrusted and fails closed.
func DecodeChatNotices(raw string) (string, bool) {
	return EncodeChatNotices(raw)
}
