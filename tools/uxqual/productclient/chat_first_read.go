package productclient

// The first-read notice ("Messages can be translated for you ...") is answered
// once per signed-in viewer for the browser session. Like the translation bar's
// dismissal it is a presentation hint that grants nothing: a viewer-scoped key
// and one closed value, so a copied storage object cannot carry anything else
// into the client.
const (
	ChatFirstReadStorageKeyPrefix = BrowserStateVersion + ".chat-first-read."
	// ChatNoticeFirstRead is the value of the one record under that key.
	ChatNoticeFirstRead = "first-read"
)

// ChatFirstReadStorageKey accepts only the SHA-256 digest of the current tenant
// and principal, the same digest the dismissed-notice key takes.
func ChatFirstReadStorageKey(scopeDigest string) string {
	if ChatNoticesStorageKey(scopeDigest) == "" {
		return ""
	}
	return ChatFirstReadStorageKeyPrefix + scopeDigest
}

// EncodeChatFirstRead is the only supported first-read record.
func EncodeChatFirstRead(notice string) (string, bool) {
	if notice != ChatNoticeFirstRead {
		return "", false
	}
	return notice, true
}

// DecodeChatFirstRead treats session storage as untrusted and fails closed.
func DecodeChatFirstRead(raw string) (string, bool) {
	return EncodeChatFirstRead(raw)
}
